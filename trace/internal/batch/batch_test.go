package batch_test

import (
	"encoding/binary"
	"testing"

	"github.com/laststate/trace/internal/batch"
)

func TestRoundTrip(t *testing.T) {
	in := []batch.Event{
		{EventID: "evt_a", Payload: []byte("LSTP...")},
		{EventID: "evt_b", Payload: []byte{1, 2, 3}},
	}
	raw, err := batch.Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := batch.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].EventID != "evt_a" || string(out[1].Payload) != "\x01\x02\x03" {
		t.Fatalf("%+v", out)
	}
}

func TestDecodeRejectsHugeCount(t *testing.T) {
	raw := make([]byte, 12)
	copy(raw[0:4], batch.Magic)
	raw[4] = batch.Version
	binary.LittleEndian.PutUint32(raw[8:12], 0xffffffff)
	if _, err := batch.Decode(raw); err == nil {
		t.Fatal("expected error for huge count")
	}
}

func TestDecodeRejectsHugePayloadLen(t *testing.T) {
	raw := make([]byte, 20)
	copy(raw[0:4], batch.Magic)
	raw[4] = batch.Version
	binary.LittleEndian.PutUint32(raw[8:12], 1)
	binary.LittleEndian.PutUint16(raw[12:14], 0)
	binary.LittleEndian.PutUint32(raw[14:18], 0xffffffff)
	if _, err := batch.DecodeLimited(raw, batch.DefaultLimits()); err == nil {
		t.Fatal("expected payload bound error")
	}
}

func TestDecodeRejectsBadMagic(t *testing.T) {
	if _, err := batch.Decode([]byte("XXXX........")); err == nil {
		t.Fatal()
	}
}

func TestDecodeRejectsBadVersion(t *testing.T) {
	raw := make([]byte, 12)
	copy(raw[0:4], batch.Magic)
	raw[4] = 99
	if _, err := batch.Decode(raw); err == nil {
		t.Fatal()
	}
}

func TestDecodeRejectsZstdFlag(t *testing.T) {
	raw := make([]byte, 12)
	copy(raw[0:4], batch.Magic)
	raw[4] = batch.Version
	raw[5] = batch.FlagZstd
	if _, err := batch.Decode(raw); err == nil {
		t.Fatal()
	}
}

func TestDecodeRejectsTotalSize(t *testing.T) {
	lim := batch.Limits{MaxEvents: 10, MaxEventID: 100, MaxPayload: 100, MaxTotal: 5}
	raw := make([]byte, 12)
	copy(raw[0:4], batch.Magic)
	raw[4] = batch.Version
	if _, err := batch.DecodeLimited(raw, lim); err == nil {
		t.Fatal()
	}
}

func TestDecodeRejectsLongEventID(t *testing.T) {
	// one event with id len > max
	id := make([]byte, 600)
	for i := range id {
		id[i] = 'a'
	}
	// build manually
	raw := make([]byte, 12+2+len(id)+4)
	copy(raw[0:4], batch.Magic)
	raw[4] = batch.Version
	binary.LittleEndian.PutUint32(raw[8:12], 1)
	binary.LittleEndian.PutUint16(raw[12:14], uint16(len(id)))
	copy(raw[14:], id)
	if _, err := batch.DecodeLimited(raw, batch.DefaultLimits()); err == nil {
		t.Fatal()
	}
}

func TestEncodeRejectsTooMany(t *testing.T) {
	evs := make([]batch.Event, batch.DefaultMaxEvents+1)
	for i := range evs {
		evs[i] = batch.Event{EventID: "x", Payload: []byte{1}}
	}
	if _, err := batch.Encode(evs); err == nil {
		t.Fatal()
	}
}

func TestEncodeRejectsLongID(t *testing.T) {
	id := string(make([]byte, batch.DefaultMaxEventID+1))
	if _, err := batch.Encode([]batch.Event{{EventID: id}}); err == nil {
		t.Fatal()
	}
}

func TestEmptyBatch(t *testing.T) {
	raw, err := batch.Encode(nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := batch.Decode(raw)
	if err != nil || len(out) != 0 {
		t.Fatalf("%v %d", err, len(out))
	}
}

func TestTruncatedFrames(t *testing.T) {
	raw, _ := batch.Encode([]batch.Event{{EventID: "a", Payload: []byte{1, 2, 3}}})
	// truncate mid-payload
	if _, err := batch.Decode(raw[:len(raw)-2]); err == nil {
		t.Fatal()
	}
}

func TestDefaultLimits(t *testing.T) {
	l := batch.DefaultLimits()
	if l.MaxEvents == 0 || l.MaxPayload == 0 {
		t.Fatal(l)
	}
}
