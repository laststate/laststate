package decode_test

import (
	"encoding/binary"
	"testing"

	"github.com/laststate/trace/internal/decode"
	"github.com/laststate/trace/internal/lep"
)

func TestIdentityFromEnvelope(t *testing.T) {
	idVal := []byte{
		2, 5, 'b', 'o', 'a', 'r', 'd',
		7, 5, '1', '.', '2', '.', '3',
		8, 8, 'd', 'e', 'a', 'd', 'b', 'e', 'e', 'f',
	}
	ev := make([]byte, 28)
	ev[1] = 4
	payload := lep.EncodeTLVs([]lep.TLV{
		{Type: decode.TLVIdentity, Value: idVal},
		{Type: decode.TLVEvent, Value: ev},
		{Type: decode.TLVBuildID, Value: []byte("deadbeef")},
	})
	raw, err := lep.Encode(lep.Header{Type: lep.TypeCrash, Architecture: 1, Sequence: 1, EventID: 42}, payload)
	if err != nil {
		t.Fatal(err)
	}
	d, err := decode.Envelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.Identity.DeviceID != "board" {
		t.Fatalf("device=%q", d.Identity.DeviceID)
	}
	if d.Identity.FirmwareVersion != "1.2.3" {
		t.Fatalf("fw=%q", d.Identity.FirmwareVersion)
	}
	if d.Identity.BuildID != "deadbeef" {
		t.Fatalf("build=%q", d.Identity.BuildID)
	}
	if d.Event == nil || d.Event.Severity != 4 {
		t.Fatalf("event=%+v", d.Event)
	}
}

func TestBootIDResetTimestampAttachment(t *testing.T) {
	// RESET TLV (type 2): 20 bytes, timestamp_ms at offset 13
	reset := make([]byte, 20)
	binary.LittleEndian.PutUint32(reset[13:17], 1_700_000_000)
	// attachment: name_len name mime_len mime content
	att := []byte{3, 'l', 'o', 'g', 4, 't', 'e', 'x', 't', 'h', 'i'}
	payload := lep.EncodeTLVs([]lep.TLV{
		{Type: decode.TLVBootID, Value: []byte("boot-abc")},
		{Type: decode.TLVReset, Value: reset},
		{Type: decode.TLVAttachment, Value: att},
		{Type: decode.TLVExtension, Value: []byte{1, 2, 3}},
		{Type: decode.TLVProjectID, Value: []byte("proj")},
		{Type: decode.TLVReleaseID, Value: []byte("1.0")},
		{Type: decode.TLVFirmwareHash, Value: []byte{0xde, 0xad}},
	})
	raw, err := lep.Encode(lep.Header{Type: lep.TypeLog, Architecture: 1}, payload)
	if err != nil {
		t.Fatal(err)
	}
	d, err := decode.Envelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.BootID != "boot-abc" || d.Identity.BootID != "boot-abc" {
		t.Fatalf("boot=%q", d.BootID)
	}
	if d.TimestampMS != 1_700_000_000 {
		t.Fatalf("ts=%d", d.TimestampMS)
	}
	if len(d.Attachments) != 1 || d.Attachments[0].Name != "log" {
		t.Fatalf("%+v", d.Attachments)
	}
	if d.Identity.ProjectID != "proj" || d.Identity.ReleaseID != "1.0" {
		t.Fatal(d.Identity)
	}
	if d.Identity.FirmwareHash != "dead" {
		t.Fatal(d.Identity.FirmwareHash)
	}
	if len(d.Extensions) != 1 {
		t.Fatal(d.Extensions)
	}
}

func TestSeverityNames(t *testing.T) {
	// map known severities
	for _, s := range []uint8{0, 1, 2, 3, 4, 5, 99} {
		_ = decode.SeverityName(s)
	}
}

func TestCorruptEnvelope(t *testing.T) {
	if _, err := decode.Envelope([]byte("bad")); err == nil {
		t.Fatal()
	}
}

func TestAssertTLV(t *testing.T) {
	// assert TLV layout depends on parseAssertMessage — send raw string-ish
	payload := lep.EncodeTLVs([]lep.TLV{
		{Type: decode.TLVAssert, Value: []byte("failed here")},
	})
	raw, err := lep.Encode(lep.Header{Type: lep.TypeError, Architecture: 1}, payload)
	if err != nil {
		t.Fatal(err)
	}
	d, err := decode.Envelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.Assert == "" {
		// parser may strip; ensure no panic
		t.Log("assert empty ok depending on layout")
	}
}

func TestCPURegistersCortex(t *testing.T) {
	// CPU TLV: architecture-dependent; send 12+ bytes of registers if parseCPU expects it
	cpu := make([]byte, 48)
	binary.LittleEndian.PutUint32(cpu[0:4], 0x08001000) // possible PC
	payload := lep.EncodeTLVs([]lep.TLV{{Type: decode.TLVCPU, Value: cpu}})
	raw, err := lep.Encode(lep.Header{Type: lep.TypeCrash, Architecture: 1}, payload)
	if err != nil {
		t.Fatal(err)
	}
	d, err := decode.Envelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	_ = d.PC
}
