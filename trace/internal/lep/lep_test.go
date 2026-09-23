package lep_test

import (
	"encoding/binary"
	"hash/crc32"
	"testing"

	"github.com/laststate/trace/internal/lep"
)

func TestValidateAndRoundTrip(t *testing.T) {
	idVal := []byte{2, 4, 'd', 'e', 'v', '1'}
	payload := lep.EncodeTLVs([]lep.TLV{{Type: 1, Value: idVal}})
	raw, err := lep.Encode(lep.Header{Type: lep.TypeCrash, Architecture: 1, Sequence: 7, EventID: 9}, payload)
	if err != nil {
		t.Fatal(err)
	}
	h, err := lep.Validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	if h.Type != lep.TypeCrash || h.Sequence != 7 || h.EventID != 9 {
		t.Fatalf("header %+v", h)
	}
	p, err := lep.Payload(raw)
	if err != nil {
		t.Fatal(err)
	}
	tlvs, err := lep.ParseTLVs(p)
	if err != nil || len(tlvs) != 1 || tlvs[0].Type != 1 {
		t.Fatalf("tlvs=%v err=%v", tlvs, err)
	}
}

func TestRejectBadMagic(t *testing.T) {
	raw := make([]byte, 32)
	copy(raw, []byte("XXXX"))
	binary.LittleEndian.PutUint32(raw[20:24], crc32.ChecksumIEEE(raw[:20]))
	if _, err := lep.Validate(raw); err == nil {
		t.Fatal("expected error")
	}
}

func TestRejectTooShort(t *testing.T) {
	if _, err := lep.Validate([]byte("LSTP")); err == nil {
		t.Fatal("expected")
	}
}

func TestRejectTooLarge(t *testing.T) {
	raw := make([]byte, lep.MaxEnvelopeSize+1)
	copy(raw[:4], lep.Magic)
	_, err := lep.Validate(raw)
	if err == nil {
		t.Fatal("expected too large")
	}
	var ve *lep.ValidationError
	if !asValidation(err, &ve) || ve.Kind != lep.ErrorTooLarge {
		t.Fatalf("kind=%v", err)
	}
}

func asValidation(err error, out **lep.ValidationError) bool {
	if e, ok := err.(*lep.ValidationError); ok {
		*out = e
		return true
	}
	return false
}

func TestRejectBadHeaderCRC(t *testing.T) {
	payload := lep.EncodeTLVs([]lep.TLV{{Type: 1, Value: []byte{1}}})
	raw, err := lep.Encode(lep.Header{Type: lep.TypeError, Architecture: 1}, payload)
	if err != nil {
		t.Fatal(err)
	}
	raw[20] ^= 0xff
	if _, err := lep.Validate(raw); err == nil {
		t.Fatal("expected header crc fail")
	}
}

func TestRejectBadPayloadCRC(t *testing.T) {
	payload := lep.EncodeTLVs([]lep.TLV{{Type: 1, Value: []byte{1, 2, 3}}})
	raw, err := lep.Encode(lep.Header{Type: lep.TypeError, Architecture: 1}, payload)
	if err != nil {
		t.Fatal(err)
	}
	// flip last payload crc byte
	raw[len(raw)-1] ^= 0xff
	if _, err := lep.Validate(raw); err == nil {
		t.Fatal("expected payload crc fail")
	}
}

func TestRejectUnknownVersion(t *testing.T) {
	payload := lep.EncodeTLVs(nil)
	raw, err := lep.Encode(lep.Header{Type: lep.TypeLog, Architecture: 1}, payload)
	if err != nil {
		t.Fatal(err)
	}
	raw[4] = 99
	binary.LittleEndian.PutUint32(raw[20:24], crc32.ChecksumIEEE(raw[:20]))
	// also fix payload length area already ok
	if _, err := lep.Validate(raw); err == nil {
		t.Fatal("expected unsupported version")
	}
}

func TestRejectUnknownFlags(t *testing.T) {
	payload := lep.EncodeTLVs(nil)
	raw, err := lep.Encode(lep.Header{Type: lep.TypeLog, Architecture: 1}, payload)
	if err != nil {
		t.Fatal(err)
	}
	raw[7] = 0x80 // unknown flag
	binary.LittleEndian.PutUint32(raw[20:24], crc32.ChecksumIEEE(raw[:20]))
	if _, err := lep.Validate(raw); err == nil {
		t.Fatal("expected unsupported flags")
	}
}

func TestRejectEncryptedAEADMismatch(t *testing.T) {
	payload := lep.EncodeTLVs(nil)
	raw, err := lep.Encode(lep.Header{Type: lep.TypeLog, Architecture: 1}, payload)
	if err != nil {
		t.Fatal(err)
	}
	raw[7] = lep.FlagEncrypted // encrypted without AEAD
	binary.LittleEndian.PutUint32(raw[20:24], crc32.ChecksumIEEE(raw[:20]))
	// size will be wrong too; either corrupt or unsupported is fine
	if _, err := lep.Validate(raw); err == nil {
		t.Fatal("expected error")
	}
}

func TestRejectTLVZeroType(t *testing.T) {
	payload := make([]byte, 4)
	// type 0, len 0
	raw, err := lep.Encode(lep.Header{Type: lep.TypeMessage, Architecture: 1}, payload)
	if err != nil {
		// encode may reject invalid TLVs
		return
	}
	if _, err := lep.Validate(raw); err == nil {
		t.Fatal("expected tlv type zero")
	}
}

func TestRejectTLVOverrun(t *testing.T) {
	// craft payload: type=1 len=100 but only 1 byte
	payload := []byte{1, 0, 100, 0, 0xab}
	raw := make([]byte, lep.HeaderSize+len(payload)+4)
	copy(raw[0:4], lep.Magic)
	raw[4] = lep.Version1
	raw[5] = lep.TypeLog
	binary.LittleEndian.PutUint32(raw[16:20], uint32(len(payload)))
	binary.LittleEndian.PutUint32(raw[20:24], crc32.ChecksumIEEE(raw[:20]))
	copy(raw[lep.HeaderSize:], payload)
	binary.LittleEndian.PutUint32(raw[lep.HeaderSize+len(payload):], crc32.ChecksumIEEE(payload))
	if _, err := lep.Validate(raw); err == nil {
		t.Fatal("expected overrun")
	}
}

func TestEventTypeAndArchNames(t *testing.T) {
	if lep.EventTypeName(lep.TypeCrash) != "crash" {
		t.Fatal()
	}
	if lep.EventTypeName(255) != "unknown" {
		t.Fatal()
	}
	if lep.ArchName(1) != "cortex-m" || lep.ArchName(2) != "riscv" || lep.ArchName(3) != "xtensa" || lep.ArchName(4) != "linux" {
		t.Fatal(lep.ArchName(1), lep.ArchName(2))
	}
	if lep.ArchName(99) != "unknown" {
		t.Fatal()
	}
}

func TestValidationErrorString(t *testing.T) {
	e := &lep.ValidationError{Kind: lep.ErrorCorrupt, Field: "magic", Reason: "bad"}
	if e.Error() != "magic: bad" {
		t.Fatal(e.Error())
	}
	e2 := &lep.ValidationError{Reason: "only"}
	if e2.Error() != "only" {
		t.Fatal(e2.Error())
	}
}

func TestEncodeRejectsBadVersion(t *testing.T) {
	_, err := lep.Encode(lep.Header{Version: 99}, nil)
	if err == nil {
		t.Fatal("expected")
	}
}

func TestEmptyPayloadRoundTrip(t *testing.T) {
	raw, err := lep.Encode(lep.Header{Type: lep.TypeHealth, Architecture: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lep.Validate(raw); err != nil {
		t.Fatal(err)
	}
	p, err := lep.Payload(raw)
	if err != nil || len(p) != 0 {
		t.Fatalf("%v %d", err, len(p))
	}
}

func TestEncodeDefaultsToCurrentVersion(t *testing.T) {
	payload := lep.EncodeTLVs([]lep.TLV{{Type: 1, Value: []byte("v")}})
	raw, err := lep.Encode(lep.Header{Type: lep.TypeMessage, Architecture: 1}, payload)
	if err != nil {
		t.Fatal(err)
	}
	if raw[4] != lep.CurrentVersion {
		t.Fatalf("encode version=%d, want %d", raw[4], lep.CurrentVersion)
	}
	if _, err := lep.Validate(raw); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAcceptsV1AndV2(t *testing.T) {
	for _, version := range []uint8{lep.Version1, lep.Version2} {
		raw, err := lep.Encode(lep.Header{Type: lep.TypeLog, Architecture: 1, Version: version}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := lep.Validate(raw); err != nil {
			t.Fatalf("version %d rejected: %v", version, err)
		}
	}
}

func TestMultipleTLVs(t *testing.T) {
	payload := lep.EncodeTLVs([]lep.TLV{
		{Type: 1, Value: []byte("a")},
		{Type: 3, Value: []byte{1, 2, 3, 4}},
		{Type: 0x10, Value: []byte("deadbeef")},
	})
	raw, err := lep.Encode(lep.Header{Type: lep.TypeError, Architecture: 2}, payload)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := lep.Payload(raw)
	tlvs, err := lep.ParseTLVs(p)
	if err != nil || len(tlvs) != 3 {
		t.Fatalf("%v %d", err, len(tlvs))
	}
}
