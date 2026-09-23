package analysis_test

import (
	"encoding/binary"
	"testing"

	"github.com/laststate/trace/internal/analysis"
	"github.com/laststate/trace/internal/decode"
	"github.com/laststate/trace/internal/lep"
)

func TestCortexMDetailedFaultAndStacked(t *testing.T) {
	// CFSR div0 + HFSR forced + stacked frame after 24 bytes
	value := make([]byte, 56)
	binary.LittleEndian.PutUint32(value[0:4], 1<<25) // DIVBYZERO in UFSR region bit 25 of CFSR
	binary.LittleEndian.PutUint32(value[4:8], 1<<30) // FORCED
	// stacked PC at offset 24+24=48
	binary.LittleEndian.PutUint32(value[24+24:24+28], 0x0800ABCD)
	binary.LittleEndian.PutUint32(value[24+20:24+24], 0x0800FFFF) // LR
	d := decode.Decoded{
		Header: lep.Header{Type: lep.TypeCrash, Architecture: 1},
		TLVs:   []lep.TLV{{Type: decode.TLVFault, Value: value}},
	}
	r := analysis.FromDecoded(d)
	if r.ProbableCause == "" {
		t.Fatal("probable")
	}
	if r.FaultDetails == nil {
		t.Fatal("details")
	}
	if r.Stacked == nil {
		t.Fatal("stacked")
	}
	if r.UnwindMethod != "stacked-frame" {
		t.Fatal(r.UnwindMethod)
	}
	if r.AnalyzerVersion != analysis.AnalyzerVersion {
		t.Fatal()
	}
}
