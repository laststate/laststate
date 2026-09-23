// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package lep

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
)

// LEP v1/v2 crypto (device path):
//
//	AEAD meta: nonce[24] || key_id u32 LE
//	AAD: header[24] || meta[28]
//	Key: HKDF-SHA256(salt=key_id||seq||event_id, ikm, info="laststate/latch/envelope/v{version}")
//	HMAC: over header||payload||payload_crc

const (
	AEADMetadataSize = 28
	AEADTagSize      = 16
	HMACSize         = 32
	KeyIDSize        = 8
	XChaChaNonceSize = 24
)

var (
	envelopeHKDFInfoV1 = []byte("laststate/latch/envelope/v1")
	envelopeHKDFInfoV2 = []byte("laststate/latch/envelope/v2")
)

// envelopeHKDFInfo returns the version-bound HKDF info label. Unknown versions
// fall back to the v2 label so forward decryption is deterministic.
func envelopeHKDFInfo(version uint8) []byte {
	if version == Version1 {
		return envelopeHKDFInfoV1
	}
	return envelopeHKDFInfoV2
}

// Key is a 32-byte IKM with a numeric device key id (Latch u32) and optional 8-byte label.
type Key struct {
	NumericID uint32
	ID        [KeyIDSize]byte // optional display / config
	Key       []byte          // 32 bytes
}

// Keyring resolves keys for Open and selects an active key for Seal.
type Keyring interface {
	GetNumeric(id uint32) (Key, bool)
	// Active returns the key used for newly sealed envelopes.
	Active() (Key, bool)
	// AuthKey returns the HMAC key for auth-only envelopes.
	AuthKey() (Key, bool)
}

// MemoryKeyring is a simple in-process key set.
type MemoryKeyring struct {
	ByNumeric  map[uint32]Key
	ActiveID   uint32
	AuthID     uint32
	HaveActive bool
	HaveAuth   bool
}

// NewMemoryKeyring indexes keys by NumericID.
func NewMemoryKeyring(keys []Key, activeID, authID uint32) *MemoryKeyring {
	ring := &MemoryKeyring{ByNumeric: make(map[uint32]Key, len(keys))}
	for _, key := range keys {
		if len(key.Key) != chacha20poly1305.KeySize {
			continue
		}
		ring.ByNumeric[key.NumericID] = key
	}
	if _, ok := ring.ByNumeric[activeID]; ok {
		ring.ActiveID = activeID
		ring.HaveActive = true
	}
	if _, ok := ring.ByNumeric[authID]; ok {
		ring.AuthID = authID
		ring.HaveAuth = true
	} else if ring.HaveActive {
		ring.AuthID = ring.ActiveID
		ring.HaveAuth = true
	}
	return ring
}

func (ring *MemoryKeyring) GetNumeric(id uint32) (Key, bool) {
	key, ok := ring.ByNumeric[id]
	return key, ok
}

func (ring *MemoryKeyring) Active() (Key, bool) {
	if !ring.HaveActive {
		return Key{}, false
	}
	return ring.GetNumeric(ring.ActiveID)
}

func (ring *MemoryKeyring) AuthKey() (Key, bool) {
	if !ring.HaveAuth {
		return Key{}, false
	}
	return ring.GetNumeric(ring.AuthID)
}

// ParseKeyID copies up to 8 bytes into a fixed key ID (zero-padded).
func ParseKeyID(raw []byte) [KeyIDSize]byte {
	var id [KeyIDSize]byte
	copy(id[:], raw)
	return id
}

// NumericIDFromKeyID interprets the first 4 LE bytes as a Latch key_id.
func NumericIDFromKeyID(id [KeyIDSize]byte) uint32 {
	return binary.LittleEndian.Uint32(id[:4])
}

// KeyIDFromNumeric stores u32 LE in the first 4 bytes of an 8-byte id.
func KeyIDFromNumeric(id uint32) [KeyIDSize]byte {
	var out [KeyIDSize]byte
	binary.LittleEndian.PutUint32(out[:4], id)
	return out
}

func hkdfSHA256(salt, ikm, info []byte, length int) ([]byte, error) {
	// Match Latch ls_hkdf_sha256: Extract then Expand.
	if len(salt) == 0 {
		salt = make([]byte, 32)
	}
	prkMac := hmac.New(sha256.New, salt)
	prkMac.Write(ikm)
	prk := prkMac.Sum(nil)

	var out []byte
	var previous []byte
	counter := byte(1)
	for len(out) < length {
		mac := hmac.New(sha256.New, prk)
		mac.Write(previous)
		mac.Write(info)
		mac.Write([]byte{counter})
		previous = mac.Sum(nil)
		counter++
		need := length - len(out)
		if need > len(previous) {
			need = len(previous)
		}
		out = append(out, previous[:need]...)
	}
	return out, nil
}

func deriveEnvelopeKey(ikm []byte, keyID, sequence, eventID uint32, version uint8) ([]byte, error) {
	salt := make([]byte, 12)
	binary.LittleEndian.PutUint32(salt[0:4], keyID)
	binary.LittleEndian.PutUint32(salt[4:8], sequence)
	binary.LittleEndian.PutUint32(salt[8:12], eventID)
	return hkdfSHA256(salt, ikm, envelopeHKDFInfo(version), 32)
}

// Seal encrypts and/or authenticates a plain validated envelope (Latch device path).
// plain must be a plain envelope (flags only TRUNCATED and/or COMPRESSED allowed).
func Seal(plain []byte, ring Keyring, encrypt bool) ([]byte, error) {
	envelope, err := Validate(plain)
	if err != nil {
		return nil, err
	}
	if envelope.Flags&^(FlagTruncated|FlagCompressed) != 0 {
		return nil, errors.New("seal requires a non-authenticated envelope")
	}
	payload := plain[HeaderSize : HeaderSize+int(envelope.PayloadLength)]
	preserve := envelope.Flags & (FlagTruncated | FlagCompressed)

	if encrypt {
		key, ok := ring.Active()
		if !ok {
			return nil, errors.New("no active encryption key")
		}
		nonce := make([]byte, XChaChaNonceSize)
		if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
			return nil, err
		}

		header := make([]byte, HeaderSize)
		copy(header[:20], plain[:20])
		header[7] = FlagAuthenticated | FlagEncrypted | FlagAEAD | preserve
		binary.LittleEndian.PutUint32(header[16:20], uint32(len(payload)))
		binary.LittleEndian.PutUint32(header[20:24], crc32.ChecksumIEEE(header[:20]))

		meta := make([]byte, AEADMetadataSize)
		copy(meta[0:24], nonce)
		binary.LittleEndian.PutUint32(meta[24:28], key.NumericID)

		derived, err := deriveEnvelopeKey(key.Key, key.NumericID, envelope.Sequence, envelope.EventID, envelope.Version)
		if err != nil {
			return nil, err
		}
		aead, err := chacha20poly1305.NewX(derived)
		if err != nil {
			return nil, err
		}
		aad := append(append([]byte{}, header...), meta...)
		sealed := aead.Seal(nil, nonce, payload, aad)
		ct, tag := sealed[:len(sealed)-AEADTagSize], sealed[len(sealed)-AEADTagSize:]

		out := make([]byte, HeaderSize+AEADMetadataSize+len(ct)+4+AEADTagSize)
		copy(out[:HeaderSize], header)
		copy(out[HeaderSize:], meta)
		copy(out[HeaderSize+AEADMetadataSize:], ct)
		crcRegion := out[HeaderSize : HeaderSize+AEADMetadataSize+len(ct)]
		binary.LittleEndian.PutUint32(out[HeaderSize+AEADMetadataSize+len(ct):], crc32.ChecksumIEEE(crcRegion))
		copy(out[len(out)-AEADTagSize:], tag)
		return out, nil
	}

	key, ok := ring.AuthKey()
	if !ok {
		return nil, errors.New("no auth key")
	}
	header := make([]byte, HeaderSize)
	copy(header[:20], plain[:20])
	header[7] = FlagAuthenticated | preserve
	binary.LittleEndian.PutUint32(header[16:20], uint32(len(payload)))
	binary.LittleEndian.PutUint32(header[20:24], crc32.ChecksumIEEE(header[:20]))

	out := make([]byte, HeaderSize+len(payload)+4+HMACSize)
	copy(out[:HeaderSize], header)
	copy(out[HeaderSize:], payload)
	binary.LittleEndian.PutUint32(out[HeaderSize+len(payload):], crc32.ChecksumIEEE(payload))
	mac := hmac.New(sha256.New, key.Key)
	mac.Write(out[:HeaderSize+len(payload)+4])
	copy(out[len(out)-HMACSize:], mac.Sum(nil))
	return out, nil
}

// Open verifies authentication and decrypts when needed (Latch device path).
func Open(raw []byte, ring Keyring) ([]byte, error) {
	envelope, err := Validate(raw)
	if err != nil {
		return nil, err
	}
	if envelope.Flags&FlagAuthenticated == 0 {
		return append([]byte(nil), raw...), nil
	}
	if envelope.Flags&FlagAEAD != 0 {
		return openAEAD(raw, envelope, ring)
	}
	return openHMAC(raw, envelope, ring)
}

func openAEAD(raw []byte, envelope Envelope, ring Keyring) ([]byte, error) {
	if len(raw) < HeaderSize+AEADMetadataSize+4+AEADTagSize {
		return nil, &ValidationError{Kind: ErrorCorrupt, Field: "aead", Reason: "envelope too short for AEAD"}
	}
	meta := raw[HeaderSize : HeaderSize+AEADMetadataSize]
	nonce := meta[0:24]
	keyID := binary.LittleEndian.Uint32(meta[24:28])
	key, ok := ring.GetNumeric(keyID)
	if !ok {
		return nil, fmt.Errorf("unknown key id %d", keyID)
	}
	ctLen := int(envelope.PayloadLength)
	ctStart := HeaderSize + AEADMetadataSize
	ctEnd := ctStart + ctLen
	if ctEnd+4+AEADTagSize != len(raw) {
		return nil, &ValidationError{Kind: ErrorCorrupt, Field: "aead", Reason: "length mismatch"}
	}
	ciphertext := raw[ctStart:ctEnd]
	tag := raw[len(raw)-AEADTagSize:]

	derived, err := deriveEnvelopeKey(key.Key, keyID, envelope.Sequence, envelope.EventID, envelope.Version)
	if err != nil {
		return nil, err
	}
	aead, err := chacha20poly1305.NewX(derived)
	if err != nil {
		return nil, err
	}
	aad := raw[:HeaderSize+AEADMetadataSize]
	sealed := append(append([]byte(nil), ciphertext...), tag...)
	plaintext, err := aead.Open(nil, nonce, sealed, aad)
	if err != nil {
		return nil, &ValidationError{Kind: ErrorCorrupt, Field: "aead", Reason: "authentication failed"}
	}
	return rebuildPlain(raw, plaintext)
}

func openHMAC(raw []byte, envelope Envelope, ring Keyring) ([]byte, error) {
	if len(raw) < HeaderSize+4+HMACSize {
		return nil, &ValidationError{Kind: ErrorCorrupt, Field: "hmac", Reason: "envelope too short"}
	}
	body := raw[:len(raw)-HMACSize]
	mac := raw[len(raw)-HMACSize:]
	payload := raw[HeaderSize : HeaderSize+int(envelope.PayloadLength)]

	if key, ok := ring.AuthKey(); ok && verifyHMAC(key.Key, body, mac) {
		return rebuildPlain(raw, payload)
	}
	for _, candidate := range allKeys(ring) {
		if verifyHMAC(candidate.Key, body, mac) {
			return rebuildPlain(raw, payload)
		}
	}
	if _, ok := ring.AuthKey(); !ok && len(allKeys(ring)) == 0 {
		return nil, errors.New("no auth key configured")
	}
	return nil, &ValidationError{Kind: ErrorCorrupt, Field: "hmac", Reason: "authentication failed"}
}

func verifyHMAC(key, body, mac []byte) bool {
	h := hmac.New(sha256.New, key)
	h.Write(body)
	return hmac.Equal(h.Sum(nil), mac)
}

func rebuildPlain(original, payload []byte) ([]byte, error) {
	out := make([]byte, HeaderSize+len(payload)+4)
	copy(out[:20], original[:20])
	out[7] = original[7] & (FlagTruncated | FlagCompressed)
	binary.LittleEndian.PutUint32(out[16:20], uint32(len(payload)))
	binary.LittleEndian.PutUint32(out[20:24], crc32.ChecksumIEEE(out[:20]))
	copy(out[HeaderSize:], payload)
	binary.LittleEndian.PutUint32(out[HeaderSize+len(payload):], crc32.ChecksumIEEE(payload))
	if _, err := Validate(out); err != nil {
		return nil, err
	}
	return out, nil
}

func allKeys(ring Keyring) []Key {
	if mem, ok := ring.(*MemoryKeyring); ok {
		out := make([]Key, 0, len(mem.ByNumeric))
		for _, key := range mem.ByNumeric {
			out = append(out, key)
		}
		return out
	}
	if key, ok := ring.Active(); ok {
		return []Key{key}
	}
	return nil
}
