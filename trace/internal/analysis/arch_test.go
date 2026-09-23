package analysis_test

import (
	"encoding/binary"
	"testing"

	"github.com/laststate/trace/internal/analysis"
	"github.com/laststate/trace/internal/decode"
	"github.com/laststate/trace/internal/lep"
)

func TestRISCVDetailed(t *testing.T) {
	v := make([]byte, 28)
	binary.LittleEndian.PutUint32(v[0:4], 2) // illegal instruction
	binary.LittleEndian.PutUint32(v[4:8], 0x80001234)
	binary.LittleEndian.PutUint32(v[8:12], 0xdead)
	binary.LittleEndian.PutUint32(v[12:16], 0x1800) // mstatus
	binary.LittleEndian.PutUint32(v[16:20], 0x8000ff00)
	binary.LittleEndian.PutUint32(v[20:24], 0x80005678) // ra
	d := decode.Decoded{
		Header: lep.Header{Type: lep.TypeCrash, Architecture: 2},
		TLVs:   []lep.TLV{{Type: decode.TLVFault, Value: v}},
	}
	r := analysis.FromDecoded(d)
	if r.ProbableCause == "" || r.MEPC == 0 {
		t.Fatalf("%+v", r)
	}
	if r.FaultDetails == nil {
		t.Fatal("details")
	}
	if len(r.Frames) < 1 {
		t.Fatal("frames")
	}
}

func TestXtensaDetailed(t *testing.T) {
	v := make([]byte, 28)
	binary.LittleEndian.PutUint32(v[0:4], 0) // illegal
	binary.LittleEndian.PutUint32(v[4:8], 0x3ff00000)
	binary.LittleEndian.PutUint32(v[8:12], 0x400d1234) // epc1
	binary.LittleEndian.PutUint32(v[12:16], 0x20)
	binary.LittleEndian.PutUint32(v[24:28], 0x400d5678) // a0
	d := decode.Decoded{
		Header: lep.Header{Type: lep.TypeCrash, Architecture: 3},
		TLVs:   []lep.TLV{{Type: decode.TLVFault, Value: v}},
	}
	r := analysis.FromDecoded(d)
	if r.ProbableCause == "" {
		t.Fatal(r)
	}
	if r.PC == 0 {
		t.Fatal("epc")
	}
}

func TestLinuxSIGSEGV(t *testing.T) {
	v := make([]byte, 32)
	binary.LittleEndian.PutUint32(v[0:4], 11) // SIGSEGV
	binary.LittleEndian.PutUint32(v[4:8], 1)  // MAPERR
	binary.LittleEndian.PutUint64(v[8:16], 0)
	binary.LittleEndian.PutUint64(v[16:24], 0x0000555555551234)
	binary.LittleEndian.PutUint64(v[24:32], 0x00007fffffffe000)
	d := decode.Decoded{
		Header: lep.Header{Type: lep.TypeCrash, Architecture: 4},
		TLVs:   []lep.TLV{{Type: decode.TLVFault, Value: v}},
	}
	r := analysis.FromDecoded(d)
	if r.ProbableCause == "" || r.FaultDetails == nil {
		t.Fatal(r)
	}
	if len(r.Frames) < 1 {
		t.Fatal("pc frame")
	}
	if r.AnalyzerVersion != analysis.AnalyzerVersion {
		t.Fatal()
	}
}
