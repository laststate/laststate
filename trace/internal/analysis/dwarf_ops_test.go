package analysis

import "testing"

func TestInterpretCFIOpsOffset(t *testing.T) {
	// DW_CFA_def_cfa r13 + 32, DW_CFA_offset r14 at 1 (-4*1 with dataAlign -4)
	ops := []byte{
		0x0c, 13, 32, // def_cfa
		0x80 | 14, 1, // offset r14
	}
	off, ok := interpretCFIOps(ops, -4, 14)
	if !ok {
		t.Fatal("expected ra offset")
	}
	if off != -4 {
		t.Fatalf("got %d", off)
	}
}
