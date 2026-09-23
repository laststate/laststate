// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package lep

import (
	"bytes"
	"hash/crc32"
	"testing"
)

func testKey(numeric uint32, seed byte) Key {
	return Key{
		NumericID: numeric,
		ID:        KeyIDFromNumeric(numeric),
		Key:       bytes.Repeat([]byte{seed}, 32),
	}
}

func TestSealOpenAEADRoundTrip(t *testing.T) {
	plain, err := Encode(Version1, 2, 1, 7, 9, []byte{1, 0, 2, 0, 0xaa, 0xbb})
	if err != nil {
		t.Fatal(err)
	}
	key := testKey(1, 0x42)
	ring := NewMemoryKeyring([]Key{key}, key.NumericID, key.NumericID)
	sealed, err := Seal(plain, ring, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(sealed); err != nil {
		t.Fatalf("sealed validation: %v", err)
	}
	if sealed[HeaderSize+24] != 1 || sealed[HeaderSize+25] != 0 {
		t.Fatalf("expected key_id 1 in metadata, got %x", sealed[HeaderSize+24:HeaderSize+28])
	}
	opened, err := Open(sealed, ring)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(opened, plain) {
		t.Fatalf("round-trip mismatch\nplain=%x\nopened=%x", plain, opened)
	}
	fields, err := TLVs(opened)
	if err != nil || len(fields) != 1 || fields[0].Type != 1 {
		t.Fatalf("tlvs: %v %#v", err, fields)
	}
}

func TestSealOpenHMACRoundTrip(t *testing.T) {
	plain, err := Encode(Version1, 1, 0, 1, 2, []byte{2, 0, 1, 0, 0xcc})
	if err != nil {
		t.Fatal(err)
	}
	key := testKey(7, 0x11)
	ring := NewMemoryKeyring([]Key{key}, key.NumericID, key.NumericID)
	sealed, err := Seal(plain, ring, false)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := Open(sealed, ring)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(opened, plain) {
		t.Fatal("hmac round-trip mismatch")
	}
}

func TestOpenRejectsWrongKey(t *testing.T) {
	plain, _ := Encode(Version1, 1, 0, 1, 2, []byte{1, 0, 1, 0, 9})
	key := testKey(1, 1)
	ring := NewMemoryKeyring([]Key{key}, key.NumericID, key.NumericID)
	sealed, err := Seal(plain, ring, true)
	if err != nil {
		t.Fatal(err)
	}
	other := testKey(1, 2)
	bad := NewMemoryKeyring([]Key{other}, other.NumericID, other.NumericID)
	if _, err := Open(sealed, bad); err == nil {
		t.Fatal("expected authentication failure")
	}
}

func TestTruncatedFlagPreserved(t *testing.T) {
	plain, err := Encode(Version1, 1, 0, 1, 2, []byte{1, 0, 1, 0, 9})
	if err != nil {
		t.Fatal(err)
	}
	plain[7] = FlagTruncated
	sum := crc32.ChecksumIEEE(plain[:20])
	plain[20] = byte(sum)
	plain[21] = byte(sum >> 8)
	plain[22] = byte(sum >> 16)
	plain[23] = byte(sum >> 24)
	if _, err := Validate(plain); err != nil {
		t.Fatal(err)
	}
	key := testKey(3, 9)
	ring := NewMemoryKeyring([]Key{key}, 3, 3)
	sealed, err := Seal(plain, ring, false)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := Open(sealed, ring)
	if err != nil {
		t.Fatal(err)
	}
	if opened[7]&FlagTruncated == 0 {
		t.Fatal("truncated flag lost")
	}
}

func TestCompressRoundTrip(t *testing.T) {
	fields := []TLV{{Type: 1, Value: bytes.Repeat([]byte("x"), 200)}}
	plain, err := EncodeTLVs(Version1, 1, 0, 1, 1, fields)
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := CompressPayload(plain)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Validate(compressed); err != nil {
		t.Fatal(err)
	}
	if compressed[7]&FlagCompressed == 0 {
		t.Fatal("missing compressed flag")
	}
	if compressed[7]&FlagTruncated != 0 {
		t.Fatal("compressed must not set truncated bit")
	}
	out, err := DecompressPayload(compressed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, plain) {
		t.Fatal("compress round-trip mismatch")
	}
}

func TestReplayCache(t *testing.T) {
	cache := NewReplayCache(2)
	payload := []byte("same")
	if cache.Seen("s", 1) {
		t.Fatal("unexpected seen")
	}
	cache.Record("s", 1, payload)
	if !cache.Seen("s", 1) {
		t.Fatal("expected seen")
	}
	same, conflict := cache.Check("s", 1, payload)
	if !same || conflict {
		t.Fatalf("same=%v conflict=%v", same, conflict)
	}
	_, conflict = cache.Check("s", 1, []byte("other"))
	if !conflict {
		t.Fatal("expected conflict on different payload")
	}
	cache.Record("s", 2, payload)
	cache.Record("s", 3, payload)
	if cache.Seen("s", 1) {
		t.Fatal("expected eviction of oldest")
	}
}

func TestEncodeMatchesValidate(t *testing.T) {
	raw, err := Encode(Version1, 2, 0, 7, 9, []byte{1, 0, 2, 0, 0xaa, 0xbb})
	if err != nil {
		t.Fatal(err)
	}
	env, err := Validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	if env.Sequence != 7 || env.EventID != 9 || env.PayloadLength != 6 {
		t.Fatalf("%#v", env)
	}
}

func TestFlagBits(t *testing.T) {
	if FlagTruncated != 1<<3 {
		t.Fatalf("FlagTruncated=%d", FlagTruncated)
	}
	if FlagCompressed != 1<<4 {
		t.Fatalf("FlagCompressed=%d", FlagCompressed)
	}
}
