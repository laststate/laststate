package analysis

import (
	"encoding/binary"
	"fmt"
)

// Cortex-M stacked frame (exception entry) layout when present after fault TLV
// or as part of stack dump prefix (8x uint32: r0 r1 r2 r3 r12 lr pc xpsr).

type StackedFrame struct {
	R0   uint32 `json:"r0"`
	R1   uint32 `json:"r1"`
	R2   uint32 `json:"r2"`
	R3   uint32 `json:"r3"`
	R12  uint32 `json:"r12"`
	LR   uint32 `json:"lr"`
	PC   uint32 `json:"pc"`
	XPSR uint32 `json:"xpsr"`
}

func parseStackedFrame(v []byte) *StackedFrame {
	if len(v) < 32 {
		return nil
	}
	return &StackedFrame{
		R0:   binary.LittleEndian.Uint32(v[0:4]),
		R1:   binary.LittleEndian.Uint32(v[4:8]),
		R2:   binary.LittleEndian.Uint32(v[8:12]),
		R3:   binary.LittleEndian.Uint32(v[12:16]),
		R12:  binary.LittleEndian.Uint32(v[16:20]),
		LR:   binary.LittleEndian.Uint32(v[20:24]),
		PC:   binary.LittleEndian.Uint32(v[24:28]),
		XPSR: binary.LittleEndian.Uint32(v[28:32]),
	}
}

func decodeCortexMDetailed(cfsr, hfsr, mmfar, bfar uint32) (causes []string, probable string, details map[string]any) {
	details = map[string]any{}
	mmfsr := cfsr & 0xFF
	bfsr := (cfsr >> 8) & 0xFF
	ufsr := (cfsr >> 16) & 0xFFFF

	add := func(cond bool, text string) {
		if cond {
			causes = append(causes, text)
		}
	}
	// MMFSR
	add(mmfsr&(1<<0) != 0, "IACCVIOL: instruction access violation")
	add(mmfsr&(1<<1) != 0, "DACCVIOL: data access violation")
	add(mmfsr&(1<<3) != 0, "MUNSTKERR: MemManage unstacking error")
	add(mmfsr&(1<<4) != 0, "MSTKERR: MemManage stacking error")
	add(mmfsr&(1<<5) != 0, "MLSPERR: MemManage lazy FP stacking")
	add(mmfsr&(1<<7) != 0, "MMARVALID: MMFAR valid")
	if mmfsr&(1<<7) != 0 {
		details["mmfar"] = fmt.Sprintf("0x%08x", mmfar)
	}
	// BFSR
	add(bfsr&(1<<0) != 0, "IBUSERR: instruction bus error")
	add(bfsr&(1<<1) != 0, "PRECISERR: precise data bus error")
	add(bfsr&(1<<2) != 0, "IMPRECISERR: imprecise data bus error")
	add(bfsr&(1<<3) != 0, "UNSTKERR: bus fault on unstacking")
	add(bfsr&(1<<4) != 0, "STKERR: bus fault on stacking")
	add(bfsr&(1<<5) != 0, "LSPERR: bus fault lazy FP stacking")
	add(bfsr&(1<<7) != 0, "BFARVALID: BFAR valid")
	if bfsr&(1<<7) != 0 {
		details["bfar"] = fmt.Sprintf("0x%08x", bfar)
	}
	// UFSR
	add(ufsr&(1<<0) != 0, "UNDEFINSTR: undefined instruction")
	add(ufsr&(1<<1) != 0, "INVSTATE: invalid EPSR/execution state")
	add(ufsr&(1<<2) != 0, "INVPC: invalid PC load / EXC_RETURN")
	add(ufsr&(1<<3) != 0, "NOCP: no coprocessor")
	add(ufsr&(1<<8) != 0, "UNALIGNED: unaligned access")
	add(ufsr&(1<<9) != 0, "DIVBYZERO: division by zero")
	// HFSR
	add(hfsr&(1<<1) != 0, "VECTTBL: vector table read fault")
	add(hfsr&(1<<30) != 0, "FORCED: configurable fault escalated to HardFault")
	add(hfsr&(1<<31) != 0, "DEBUGEVT: debug event")

	details["mmfsr"] = fmt.Sprintf("0x%02x", mmfsr)
	details["bfsr"] = fmt.Sprintf("0x%02x", bfsr)
	details["ufsr"] = fmt.Sprintf("0x%04x", ufsr)
	details["hfsr"] = fmt.Sprintf("0x%08x", hfsr)

	if len(causes) > 0 {
		probable = causes[0]
	}
	return
}
