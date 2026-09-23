// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// Package lep implements LEP v1/v2 validation, encoding, and crypto.
package lep

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

const (
	HeaderSize      = 24
	MaxEnvelopeSize = 4 << 20
	Magic           = "LSTP"
	Version1        = 1
	Version2        = 2

	// Flags match Latch / protocol registry (v1 and v2).
	FlagAuthenticated uint8 = 1 << 0
	FlagEncrypted     uint8 = 1 << 1
	FlagAEAD          uint8 = 1 << 2
	FlagTruncated     uint8 = 1 << 3 // Latch: optional fields omitted
	FlagCompressed    uint8 = 1 << 4 // Gateway zstd of payload; devices do not set this
	KnownFlags              = FlagAuthenticated | FlagEncrypted | FlagAEAD | FlagTruncated | FlagCompressed

	// CRC32IEEEPolynomial is the normal representation of CRC-32/ISO-HDLC.
	CRC32IEEEPolynomial          uint32 = 0x04C11DB7
	CRC32IEEEReflectedPolynomial uint32 = 0xEDB88320
)

type ErrorKind string

const (
	ErrorCorrupt     ErrorKind = "corrupt"
	ErrorUnsupported ErrorKind = "unsupported"
	ErrorTooLarge    ErrorKind = "too_large"
)

type ValidationError struct {
	Kind   ErrorKind
	Field  string
	Reason string
}

func (err *ValidationError) Error() string {
	if err.Field == "" {
		return err.Reason
	}
	return err.Field + ": " + err.Reason
}

type Envelope struct {
	Version       uint8  `json:"version"`
	Type          uint8  `json:"type"`
	Architecture  uint8  `json:"architecture"`
	Flags         uint8  `json:"flags"`
	Sequence      uint32 `json:"sequence"`
	EventID       uint32 `json:"event_id"`
	PayloadLength uint32 `json:"payload_length"`
}

type TLV struct {
	Type  uint16 `json:"type"`
	Value []byte `json:"-"`
}

func Validate(data []byte) (Envelope, error) {
	var envelope Envelope
	if len(data) > MaxEnvelopeSize {
		return envelope, &ValidationError{Kind: ErrorTooLarge, Field: "envelope", Reason: fmt.Sprintf("exceeds %d bytes", MaxEnvelopeSize)}
	}
	if len(data) < HeaderSize+4 {
		return envelope, &ValidationError{Kind: ErrorCorrupt, Field: "envelope", Reason: "too short"}
	}
	if string(data[:4]) != Magic {
		return envelope, &ValidationError{Kind: ErrorCorrupt, Field: "magic", Reason: "expected LSTP"}
	}
	envelope = Envelope{
		Version: data[4], Type: data[5], Architecture: data[6], Flags: data[7],
		Sequence: binary.LittleEndian.Uint32(data[8:12]), EventID: binary.LittleEndian.Uint32(data[12:16]),
		PayloadLength: binary.LittleEndian.Uint32(data[16:20]),
	}
	if envelope.Version != Version1 && envelope.Version != Version2 {
		return envelope, &ValidationError{Kind: ErrorUnsupported, Field: "version", Reason: fmt.Sprintf("unsupported LEP version %d", envelope.Version)}
	}
	if envelope.Flags&^uint8(KnownFlags) != 0 {
		return envelope, &ValidationError{Kind: ErrorUnsupported, Field: "flags", Reason: fmt.Sprintf("unknown flags 0x%02x", envelope.Flags&^uint8(KnownFlags))}
	}
	if (envelope.Flags&FlagEncrypted != 0) != (envelope.Flags&FlagAEAD != 0) {
		return envelope, &ValidationError{Kind: ErrorCorrupt, Field: "flags", Reason: "encrypted and AEAD flags must be used together"}
	}
	if envelope.Flags&FlagAEAD != 0 && envelope.Flags&FlagAuthenticated == 0 {
		return envelope, &ValidationError{Kind: ErrorCorrupt, Field: "flags", Reason: "AEAD requires authenticated flag"}
	}

	metadataLength, authLength := 0, 0
	if envelope.Flags&FlagAEAD != 0 {
		metadataLength, authLength = 28, 16
	} else if envelope.Flags&FlagAuthenticated != 0 {
		authLength = 32
	}
	overhead := HeaderSize + metadataLength + 4 + authLength
	if len(data) < overhead || uint64(envelope.PayloadLength) != uint64(len(data)-overhead) {
		return envelope, &ValidationError{Kind: ErrorCorrupt, Field: "payload_length", Reason: "does not match envelope size"}
	}
	if binary.LittleEndian.Uint32(data[20:24]) != crc32.ChecksumIEEE(data[:20]) {
		return envelope, &ValidationError{Kind: ErrorCorrupt, Field: "header_crc", Reason: "CRC-32/IEEE mismatch"}
	}
	crcOffset := HeaderSize + metadataLength + int(envelope.PayloadLength)
	if crcOffset+4 > len(data) {
		return envelope, &ValidationError{Kind: ErrorCorrupt, Field: "payload_crc", Reason: "missing checksum"}
	}
	if binary.LittleEndian.Uint32(data[crcOffset:crcOffset+4]) != crc32.ChecksumIEEE(data[HeaderSize:crcOffset]) {
		return envelope, &ValidationError{Kind: ErrorCorrupt, Field: "payload_crc", Reason: "CRC-32/IEEE mismatch"}
	}
	// Plaintext TLVs only when not encrypted and not compressed.
	if envelope.Flags&FlagEncrypted == 0 && envelope.Flags&FlagCompressed == 0 {
		if err := validateTLVs(data[HeaderSize:crcOffset]); err != nil {
			return envelope, err
		}
	}
	return envelope, nil
}

func validateTLVs(payload []byte) error {
	for offset := 0; offset < len(payload); {
		if len(payload)-offset < 4 {
			return &ValidationError{Kind: ErrorCorrupt, Field: "payload", Reason: "incomplete TLV header"}
		}
		typeID := binary.LittleEndian.Uint16(payload[offset : offset+2])
		length := int(binary.LittleEndian.Uint16(payload[offset+2 : offset+4]))
		offset += 4
		if typeID == 0 {
			return &ValidationError{Kind: ErrorCorrupt, Field: "payload", Reason: "TLV type zero is reserved"}
		}
		if length > len(payload)-offset {
			return &ValidationError{Kind: ErrorCorrupt, Field: "payload", Reason: "TLV length exceeds payload"}
		}
		offset += length
	}
	return nil
}

// TLVs returns the unencrypted, uncompressed payload fields after validation.
func TLVs(data []byte) ([]TLV, error) {
	envelope, err := Validate(data)
	if err != nil {
		return nil, err
	}
	if envelope.Flags&FlagEncrypted != 0 {
		return nil, errors.New("encrypted LEP payload cannot be decoded without a key")
	}
	if envelope.Flags&FlagAuthenticated != 0 {
		return nil, errors.New("authenticated LEP payload cannot be decoded without a key")
	}
	if envelope.Flags&FlagCompressed != 0 {
		return nil, errors.New("compressed LEP payload cannot be decoded without a configured codec")
	}
	payload := data[HeaderSize : HeaderSize+int(envelope.PayloadLength)]
	return parseTLVs(payload)
}

func parseTLVs(payload []byte) ([]TLV, error) {
	fields := make([]TLV, 0)
	for offset := 0; offset < len(payload); {
		if len(payload)-offset < 4 {
			return nil, &ValidationError{Kind: ErrorCorrupt, Field: "payload", Reason: "incomplete TLV"}
		}
		fieldType := binary.LittleEndian.Uint16(payload[offset : offset+2])
		length := int(binary.LittleEndian.Uint16(payload[offset+2 : offset+4]))
		offset += 4
		if length > len(payload)-offset {
			return nil, &ValidationError{Kind: ErrorCorrupt, Field: "payload", Reason: "TLV length exceeds payload"}
		}
		fields = append(fields, TLV{Type: fieldType, Value: append([]byte(nil), payload[offset:offset+length]...)})
		offset += length
	}
	return fields, nil
}
