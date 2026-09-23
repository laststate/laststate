package analysis

import "encoding/binary"

// Minimal DWARF CFI opcode interpreter for CFA-relative return address recovery.
// Covers the ops most firmware toolchains emit for Cortex-M / RISC-V.

const (
	dwCFANop              = 0x00
	dwCFASetLoc           = 0x01
	dwCFAAdvanceLoc1      = 0x02
	dwCFAAdvanceLoc2      = 0x03
	dwCFAAdvanceLoc4      = 0x04
	dwCFAOffsetExtended   = 0x05
	dwCFARestoreExtended  = 0x06
	dwCFAUndefined        = 0x07
	dwCFASameValue        = 0x08
	dwCFARegister         = 0x09
	dwCFARememberState    = 0x0a
	dwCFARestoreState     = 0x0b
	dwCFADefCFA           = 0x0c
	dwCFADefCFARegister   = 0x0d
	dwCFADefCFAOffset     = 0x0e
	dwCFADefCFAExpression = 0x0f
	dwCFAExpression       = 0x10
	dwCFAOffsetExtendedSf = 0x11
	dwCFADefCFASf         = 0x12
	dwCFADefCFAOffsetSf   = 0x13
	dwCFAValOffset        = 0x14
	dwCFAValOffsetSf      = 0x15
	// high 2 bits encode primary ops
	dwCFAAdvanceLoc = 0x40 // 01xxxxxx
	dwCFAOffset     = 0x80 // 10xxxxxx
	dwCFARestore    = 0xc0 // 11xxxxxx
)

type cfaState struct {
	cfaReg    uint64
	cfaOffset int64
	// reg -> offset from CFA (for DW_CFA_offset)
	saved map[uint64]int64
	raReg uint64
}

func newCFA(raReg uint64) *cfaState {
	return &cfaState{cfaReg: 13 /* sp-ish */, saved: map[uint64]int64{}, raReg: raReg}
}

// interpretCFIOps runs CFI ops and returns CFA-relative RA offset if known.
func interpretCFIOps(ops []byte, dataAlign int64, raReg uint64) (raOff int64, ok bool) {
	st := newCFA(raReg)
	if dataAlign == 0 {
		dataAlign = -4
	}
	i := 0
	for i < len(ops) {
		op := ops[i]
		i++
		switch {
		case op == dwCFANop:
			continue
		case op == dwCFADefCFA:
			reg, n := uleb(ops[i:])
			i += n
			off, n2 := uleb(ops[i:])
			i += n2
			st.cfaReg = reg
			st.cfaOffset = int64(off)
		case op == dwCFADefCFAOffset:
			off, n := uleb(ops[i:])
			i += n
			st.cfaOffset = int64(off)
		case op == dwCFADefCFARegister:
			reg, n := uleb(ops[i:])
			i += n
			st.cfaReg = reg
		case op == dwCFAOffsetExtended:
			reg, n := uleb(ops[i:])
			i += n
			off, n2 := uleb(ops[i:])
			i += n2
			st.saved[reg] = int64(off) * dataAlign
		case op&0xc0 == dwCFAOffset:
			reg := uint64(op & 0x3f)
			off, n := uleb(ops[i:])
			i += n
			st.saved[reg] = int64(off) * dataAlign
		case op&0xc0 == dwCFAAdvanceLoc:
			// location advance — ignore for offline dump
			continue
		case op == dwCFAAdvanceLoc1:
			if i < len(ops) {
				i++
			}
		case op == dwCFAAdvanceLoc2:
			i += 2
		case op == dwCFAAdvanceLoc4:
			i += 4
		case op == dwCFARestore, op&0xc0 == dwCFARestore:
			// ignore restore
			if op == dwCFARestoreExtended {
				_, n := uleb(ops[i:])
				i += n
			}
		case op == dwCFAUndefined, op == dwCFASameValue:
			_, n := uleb(ops[i:])
			i += n
		case op == dwCFARegister:
			_, n := uleb(ops[i:])
			i += n
			_, n2 := uleb(ops[i:])
			i += n2
		case op == dwCFARememberState, op == dwCFARestoreState:
			continue
		case op == dwCFADefCFASf:
			reg, n := uleb(ops[i:])
			i += n
			off, n2 := sleb(ops[i:])
			i += n2
			st.cfaReg = reg
			st.cfaOffset = off * dataAlign
		case op == dwCFADefCFAOffsetSf:
			off, n := sleb(ops[i:])
			i += n
			st.cfaOffset = off * dataAlign
		case op == dwCFAOffsetExtendedSf:
			reg, n := uleb(ops[i:])
			i += n
			off, n2 := sleb(ops[i:])
			i += n2
			st.saved[reg] = off * dataAlign
		default:
			// unknown — stop rather than desync
			if i >= len(ops) {
				break
			}
			// try skip one uleb
			_, n := uleb(ops[i:])
			if n == 0 {
				break
			}
			i += n
		}
	}
	if off, has := st.saved[st.raReg]; has {
		return off, true
	}
	// ARM LR often reg 14; RISC-V ra reg 1
	for _, r := range []uint64{st.raReg, 14, 1, 30} {
		if off, has := st.saved[r]; has {
			return off, true
		}
	}
	return st.cfaOffset, st.cfaOffset != 0
}

func uleb(b []byte) (uint64, int) {
	var r uint64
	var s uint
	for i := 0; i < len(b); i++ {
		r |= uint64(b[i]&0x7f) << s
		if b[i]&0x80 == 0 {
			return r, i + 1
		}
		s += 7
		if s > 63 {
			return r, i + 1
		}
	}
	return r, len(b)
}

func sleb(b []byte) (int64, int) {
	var r int64
	var s uint
	var i int
	for i = 0; i < len(b); i++ {
		r |= int64(b[i]&0x7f) << s
		s += 7
		if b[i]&0x80 == 0 {
			if s < 64 && b[i]&0x40 != 0 {
				r |= -1 << s
			}
			return r, i + 1
		}
	}
	return r, i
}

// extractRAFromStack reads a word at CFA+raOff from stack memory.
func extractRAFromStack(mem []byte, cfaIndex int, raOff int64, arch uint8) (uint64, bool) {
	idx := cfaIndex + int(raOff)
	step := 4
	if arch == 4 {
		step = 8
	}
	if idx < 0 || idx+step > len(mem) {
		return 0, false
	}
	var addr uint64
	if step == 8 {
		addr = binary.LittleEndian.Uint64(mem[idx : idx+8])
	} else {
		a := binary.LittleEndian.Uint32(mem[idx : idx+4])
		if arch == 1 {
			a &^= 1
		}
		addr = uint64(a)
	}
	return addr, plausibleCode(addr, arch)
}
