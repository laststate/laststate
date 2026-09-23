package analysis

import (
	"encoding/binary"
	"fmt"
)

// RISC-V fault TLV layouts supported:
//  v1 (12B): mcause u32, mepc u32, mtval u32
//  v2 (16B): + mstatus u32
//  v3 (20B+): + sp u32, optional ra/gp/tp and a0-a7 (up to 12 more words)

type RISCVRegs struct {
	MStatus uint32   `json:"mstatus,omitempty"`
	SP      uint32   `json:"sp,omitempty"`
	RA      uint32   `json:"ra,omitempty"`
	GP      uint32   `json:"gp,omitempty"`
	TP      uint32   `json:"tp,omitempty"`
	A       []uint32 `json:"a,omitempty"` // a0-a7
}

func applyRISCVFault(r *Report, value []byte) {
	if len(value) < 12 {
		r.Warnings = append(r.Warnings, "riscv fault TLV truncated")
		return
	}
	r.MCAUSE = binary.LittleEndian.Uint32(value[0:4])
	r.MEPC = binary.LittleEndian.Uint32(value[4:8])
	r.MTVAL = binary.LittleEndian.Uint32(value[8:12])
	causes, probable, details := decodeRISCVDetailed(r.MCAUSE, r.MTVAL)
	r.Causes, r.ProbableCause = causes, probable
	r.FaultDetails = details
	if r.MEPC != 0 {
		r.PC = r.MEPC
	}
	regs := &RISCVRegs{}
	if len(value) >= 16 {
		regs.MStatus = binary.LittleEndian.Uint32(value[12:16])
		details["mstatus"] = fmt.Sprintf("0x%08x", regs.MStatus)
		details["mpp"] = riscvMPP(regs.MStatus)
		details["mie"] = regs.MStatus&(1<<3) != 0
	}
	if len(value) >= 20 {
		regs.SP = binary.LittleEndian.Uint32(value[16:20])
		r.SP = regs.SP
	}
	if len(value) >= 24 {
		regs.RA = binary.LittleEndian.Uint32(value[20:24])
		r.LR = regs.RA // reuse LR field as link/return
	}
	if len(value) >= 28 {
		regs.GP = binary.LittleEndian.Uint32(value[24:28])
	}
	if len(value) >= 32 {
		regs.TP = binary.LittleEndian.Uint32(value[28:32])
	}
	for off := 32; off+4 <= len(value) && len(regs.A) < 8; off += 4 {
		regs.A = append(regs.A, binary.LittleEndian.Uint32(value[off:off+4]))
	}
	if regs.MStatus != 0 || regs.SP != 0 || regs.RA != 0 {
		details["regs"] = regs
	}
	// frames from mepc + ra
	if r.MEPC != 0 {
		r.Frames = append(r.Frames, Frame{Address: uint64(r.MEPC)})
	}
	if regs.RA != 0 && regs.RA != r.MEPC {
		r.Frames = append(r.Frames, Frame{Address: uint64(regs.RA)})
		r.UnwindMethod = "riscv-ra"
	}
	r.Confidence += 0.1
}

func riscvMPP(mstatus uint32) string {
	switch (mstatus >> 11) & 3 {
	case 0:
		return "U"
	case 1:
		return "S"
	case 3:
		return "M"
	default:
		return "?"
	}
}

func decodeRISCVDetailed(mcause, mtval uint32) (causes []string, probable string, details map[string]any) {
	details = map[string]any{
		"mcause": fmt.Sprintf("0x%08x", mcause),
		"mtval":  fmt.Sprintf("0x%08x", mtval),
	}
	interrupt := mcause>>31 != 0
	code := mcause & 0x7fffffff
	details["interrupt"] = interrupt
	details["code"] = code

	if interrupt {
		iname := map[uint32]string{
			1: "Supervisor software interrupt", 3: "Machine software interrupt",
			5: "Supervisor timer interrupt", 7: "Machine timer interrupt",
			9: "Supervisor external interrupt", 11: "Machine external interrupt",
		}
		if n, ok := iname[code]; ok {
			probable = n
		} else {
			probable = fmt.Sprintf("interrupt %d", code)
		}
		return []string{probable}, probable, details
	}

	names := map[uint32]string{
		0:  "Instruction address misaligned",
		1:  "Instruction access fault",
		2:  "Illegal instruction",
		3:  "Breakpoint",
		4:  "Load address misaligned",
		5:  "Load access fault",
		6:  "Store/AMO address misaligned",
		7:  "Store/AMO access fault",
		8:  "Environment call from U-mode",
		9:  "Environment call from S-mode",
		11: "Environment call from M-mode",
		12: "Instruction page fault",
		13: "Load page fault",
		15: "Store/AMO page fault",
	}
	if name, ok := names[code]; ok {
		causes = append(causes, name)
		probable = name
	} else {
		probable = fmt.Sprintf("exception cause %d", code)
		causes = append(causes, probable)
	}
	// context from mtval
	switch code {
	case 2:
		if mtval != 0 {
			causes = append(causes, fmt.Sprintf("bad instr encoding mtval=0x%08x", mtval))
		}
	case 1, 5, 7, 12, 13, 15:
		if mtval != 0 {
			causes = append(causes, fmt.Sprintf("faulting address 0x%08x", mtval))
		}
	case 0, 4, 6:
		if mtval != 0 {
			causes = append(causes, fmt.Sprintf("misaligned address 0x%08x", mtval))
		}
	}
	return
}

func unwindRISCV(stack []byte) []Frame {
	// Prefer aligned return addresses in typical embedded ranges
	var frames []Frame
	seen := map[uint64]bool{}
	for i := 0; i+4 <= len(stack) && len(frames) < 32; i += 4 {
		a := uint64(binary.LittleEndian.Uint32(stack[i : i+4]))
		if a < 0x1000 || a%2 != 0 || seen[a] {
			continue
		}
		// common flash/ROM/ilp32
		if (a >= 0x20000000 && a < 0x30000000) || (a >= 0x80000000 && a < 0x90000000) ||
			(a >= 0x00010000 && a < 0x00100000) || (a >= 0x20400000 && a < 0x21000000) {
			seen[a] = true
			frames = append(frames, Frame{Address: a})
		}
	}
	return frames
}
