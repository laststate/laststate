package analysis_test

import (
	"encoding/binary"
	"testing"

	"github.com/laststate/trace/internal/analysis"
	"github.com/laststate/trace/internal/decode"
	"github.com/laststate/trace/internal/lep"
)

func TestFromDecodedCortexM(t *testing.T) {
	d := decode.Decoded{
		Header: lep.Header{Type: lep.TypeCrash, Architecture: 1},
		PC:     0x08001234,
		LR:     0x08005678,
	}
	r := analysis.FromDecoded(d)
	if len(r.Frames) < 1 {
		t.Fatal("expected frames")
	}
	if r.AnalyzerVersion != analysis.AnalyzerVersion {
		t.Fatal("analyzer version")
	}
	if r.Summary == "" {
		t.Fatal("summary")
	}
}

func TestXtensaFault(t *testing.T) {
	payload := make([]byte, 8)
	payload[0] = 2
	d := decode.Decoded{
		Header: lep.Header{Type: lep.TypeCrash, Architecture: 3},
		TLVs:   []lep.TLV{{Type: decode.TLVFault, Value: payload}},
	}
	r := analysis.FromDecoded(d)
	if r.ProbableCause == "" {
		t.Fatal("expected xtensa cause")
	}
}

func TestRISCVFault(t *testing.T) {
	payload := make([]byte, 12)
	binary.LittleEndian.PutUint32(payload[0:4], 2) // illegal instruction
	binary.LittleEndian.PutUint32(payload[4:8], 0x80001234)
	d := decode.Decoded{
		Header: lep.Header{Type: lep.TypeCrash, Architecture: 2},
		TLVs:   []lep.TLV{{Type: decode.TLVFault, Value: payload}},
	}
	r := analysis.FromDecoded(d)
	if r.MCAUSE != 2 || r.ProbableCause == "" {
		t.Fatalf("%+v", r)
	}
	if r.PC == 0 {
		t.Fatal("mepc should set pc")
	}
}

func TestCortexMFaultBits(t *testing.T) {
	payload := make([]byte, 24)
	// CFSR div by zero bit 25
	binary.LittleEndian.PutUint32(payload[0:4], 1<<25)
	d := decode.Decoded{
		Header: lep.Header{Type: lep.TypeCrash, Architecture: 1},
		TLVs:   []lep.TLV{{Type: decode.TLVFault, Value: payload}},
	}
	r := analysis.FromDecoded(d)
	if r.ProbableCause == "" {
		t.Fatal("cfsr")
	}
}

func TestStackUnwind(t *testing.T) {
	stack := make([]byte, 16)
	binary.LittleEndian.PutUint32(stack[0:4], 0x08001001) // thumb bit
	binary.LittleEndian.PutUint32(stack[4:8], 0x08002000)
	d := decode.Decoded{
		Header: lep.Header{Type: lep.TypeCrash, Architecture: 1},
		TLVs:   []lep.TLV{{Type: decode.TLVStack, Value: stack}},
	}
	r := analysis.FromDecoded(d)
	if len(r.Frames) == 0 {
		t.Fatal("expected stack frames")
	}
	if r.UnwindMethod == "" {
		t.Fatal("method")
	}
}

func TestRTOSEnrichment(t *testing.T) {
	// kind freertos=1, count=1, name "t1", prio, state running=0, stack, pc
	name := "t1"
	buf := []byte{1, 1, byte(len(name))}
	buf = append(buf, name...)
	buf = append(buf, 5, 0) // prio, state running
	rest := make([]byte, 12)
	binary.LittleEndian.PutUint32(rest[8:12], 0x08009999)
	buf = append(buf, rest...)
	d := decode.Decoded{
		Header: lep.Header{Type: lep.TypeCrash, Architecture: 1},
		PC:     1,
		TLVs:   []lep.TLV{{Type: analysis.TLVRTOS, Value: buf}},
	}
	r := analysis.FromDecoded(d)
	if r.RTOS == nil || r.RTOS.Kind != "freertos" || len(r.RTOS.Tasks) != 1 {
		t.Fatalf("%+v", r.RTOS)
	}
}

func TestProbeEnrichment(t *testing.T) {
	v := append([]byte("SN123\x00jlink\x00nrf52"), 0)
	d := decode.Decoded{
		Header: lep.Header{Type: lep.TypeCrash, Architecture: 1},
		TLVs:   []lep.TLV{{Type: analysis.TLVProbe, Value: v}},
	}
	r := analysis.FromDecoded(d)
	if r.Probe == nil || r.Probe.Serial == "" {
		t.Fatalf("%+v", r.Probe)
	}
}

func TestAssertHint(t *testing.T) {
	d := decode.Decoded{
		Header: lep.Header{Type: lep.TypeError, Architecture: 1},
		Assert: "x > 0",
	}
	r := analysis.FromDecoded(d)
	if r.ProbableCause == "" && r.Confidence < 0.4 {
		t.Fatal(r)
	}
}

func TestTruncatedFaultWarnings(t *testing.T) {
	d := decode.Decoded{
		Header: lep.Header{Type: lep.TypeCrash, Architecture: 1},
		TLVs:   []lep.TLV{{Type: decode.TLVFault, Value: []byte{1}}},
	}
	r := analysis.FromDecoded(d)
	if len(r.Warnings) == 0 {
		t.Fatal("expected warning")
	}
}

func TestDWARFCFIUnwind(t *testing.T) {
	// CIE\0 + header with n_fde=0 + stack mem with code pointer
	stack := append([]byte("CIE\x00"), make([]byte, 8)...)
	stack[4] = 1 // ver
	stack[8] = 0 // will be after 8-byte header in body — rebuild properly
	body := make([]byte, 8+16)
	body[0] = 1
	body[4] = 0 // n_fde
	binary.LittleEndian.PutUint32(body[8:12], 0x08001234)
	stack = append([]byte("CIE\x00"), body...)
	d := decode.Decoded{
		Header: lep.Header{Type: lep.TypeCrash, Architecture: 1},
		TLVs:   []lep.TLV{{Type: decode.TLVStack, Value: stack}},
	}
	r := analysis.FromDecoded(d)
	if len(r.Frames) == 0 {
		t.Fatal("expected frames from dwarf cfi")
	}
	if r.UnwindMethod == "dwarf-cfi-stub" {
		t.Fatal("should not use stub method anymore:", r.UnwindMethod)
	}
	t.Log(r.UnwindMethod, len(r.Frames))
}

func TestARMEHABIUnwind(t *testing.T) {
	stack := make([]byte, 8+8+16)
	binary.LittleEndian.PutUint32(stack[0:4], 0xFFFFFFFF)
	binary.LittleEndian.PutUint16(stack[4:6], 1) // n entries
	binary.LittleEndian.PutUint32(stack[8:12], 0x0800ABCD)
	binary.LittleEndian.PutUint32(stack[12:16], 0)
	binary.LittleEndian.PutUint32(stack[16:20], 0x08001100)
	d := decode.Decoded{
		Header: lep.Header{Type: lep.TypeCrash, Architecture: 1},
		TLVs:   []lep.TLV{{Type: decode.TLVStack, Value: stack}},
	}
	r := analysis.FromDecoded(d)
	if len(r.Frames) == 0 {
		t.Fatal("expected ehabi frames")
	}
	if r.UnwindMethod == "arm-ehabi-stub" {
		t.Fatal(r.UnwindMethod)
	}
}
