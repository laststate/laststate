package lep_test

import (
	"encoding/binary"
	"hash/crc32"
	"testing"

	"github.com/laststate/trace/internal/lep"
)

func TestCompressedPayloadDecode(t *testing.T) {
	plain := lep.EncodeTLVs([]lep.TLV{{Type: 1, Value: []byte("hello")}})
	gz, err := lep.CompressGzip(plain)
	if err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, lep.HeaderSize+len(gz)+4)
	copy(raw[0:4], lep.Magic)
	raw[4] = lep.Version1
	raw[5] = lep.TypeLog
	raw[6] = 1
	raw[7] = lep.FlagCompressed
	binary.LittleEndian.PutUint32(raw[16:20], uint32(len(gz)))
	binary.LittleEndian.PutUint32(raw[20:24], crc32.ChecksumIEEE(raw[:20]))
	copy(raw[lep.HeaderSize:], gz)
	binary.LittleEndian.PutUint32(raw[lep.HeaderSize+len(gz):], crc32.ChecksumIEEE(raw[lep.HeaderSize:lep.HeaderSize+len(gz)]))

	// Validate skips TLV check when compressed
	if _, err := lep.Validate(raw); err != nil {
		t.Fatal(err)
	}
	p, err := lep.Payload(raw)
	if err != nil {
		t.Fatal(err)
	}
	tlvs, err := lep.ParseTLVs(p)
	if err != nil || len(tlvs) != 1 || string(tlvs[0].Value) != "hello" {
		t.Fatalf("%v %+v", err, tlvs)
	}
}

func TestEncryptedPayloadRejected(t *testing.T) {
	plain := []byte{1, 0, 0, 0}
	// encrypted+AEAD+auth flags for valid flag combo: AEAD requires auth; encrypted requires AEAD
	raw := make([]byte, lep.HeaderSize+28+len(plain)+4+16)
	copy(raw[0:4], lep.Magic)
	raw[4] = 1
	raw[5] = lep.TypeLog
	raw[7] = lep.FlagEncrypted | lep.FlagAEAD | lep.FlagAuthenticated
	binary.LittleEndian.PutUint32(raw[16:20], uint32(len(plain)))
	// metadata 28 + payload + crc + auth 16
	// size: Header 24 + 28 meta + payload + 4 crc + 16 auth
	// fix length of buffer and CRCs roughly — Validate size check
	meta, auth := 28, 16
	overhead := lep.HeaderSize + meta + 4 + auth
	raw = make([]byte, overhead+len(plain))
	copy(raw[0:4], lep.Magic)
	raw[4] = 1
	raw[5] = lep.TypeLog
	raw[7] = lep.FlagEncrypted | lep.FlagAEAD | lep.FlagAuthenticated
	binary.LittleEndian.PutUint32(raw[16:20], uint32(len(plain)))
	binary.LittleEndian.PutUint32(raw[20:24], crc32.ChecksumIEEE(raw[:20]))
	copy(raw[lep.HeaderSize+meta:], plain)
	crcOff := lep.HeaderSize + meta + len(plain)
	binary.LittleEndian.PutUint32(raw[crcOff:], crc32.ChecksumIEEE(raw[lep.HeaderSize:crcOff]))
	if _, err := lep.Payload(raw); err == nil {
		t.Fatal("expected encrypted unsupported")
	}
}
