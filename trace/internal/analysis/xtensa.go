package analysis

import (
	"encoding/binary"
	"fmt"
)

// Xtensa fault TLV:
//  v1 (8B):  exccause u32, excvaddr u32
//  v2 (16B): + epc1 u32, ps u32
//  v3 (24B+): + depc, excsave1, optional a0-a15 window

type XtensaRegs struct {
	EPC1    uint32   `json:"epc1,omitempty"`
	PS      uint32   `json:"ps,omitempty"`
	DEPC    uint32   `json:"depc,omitempty"`
	EXCSAVE uint32   `json:"excsave1,omitempty"`
	A       []uint32 `json:"a,omitempty"`
}

func applyXtensaFault(r *Report, value []byte) {
	if len(value) < 8 {
		r.Warnings = append(r.Warnings, "xtensa fault TLV truncated")
		return
	}
	exccause := binary.LittleEndian.Uint32(value[0:4])
	excvaddr := binary.LittleEndian.Uint32(value[4:8])
	causes, probable, details := decodeXtensaDetailed(exccause, excvaddr)
	r.Causes, r.ProbableCause = causes, probable
	r.FaultDetails = details

	regs := &XtensaRegs{}
	if len(value) >= 16 {
		regs.EPC1 = binary.LittleEndian.Uint32(value[8:12])
		regs.PS = binary.LittleEndian.Uint32(value[12:16])
		r.PC = regs.EPC1
		details["epc1"] = fmt.Sprintf("0x%08x", regs.EPC1)
		details["ps"] = fmt.Sprintf("0x%08x", regs.PS)
		details["intlevel"] = (regs.PS >> 0) & 0xF
		details["um"] = (regs.PS>>5)&1 == 1
		details["ring"] = (regs.PS >> 6) & 3
	}
	if len(value) >= 24 {
		regs.DEPC = binary.LittleEndian.Uint32(value[16:20])
		regs.EXCSAVE = binary.LittleEndian.Uint32(value[20:24])
	}
	for off := 24; off+4 <= len(value) && len(regs.A) < 16; off += 4 {
		regs.A = append(regs.A, binary.LittleEndian.Uint32(value[off:off+4]))
	}
	// a0 often holds return address on Xtensa windowed ABI
	if len(regs.A) > 0 && regs.A[0] != 0 {
		r.LR = regs.A[0]
	}
	if regs.EPC1 != 0 || len(regs.A) > 0 {
		details["regs"] = regs
	}
	if r.PC != 0 {
		r.Frames = append(r.Frames, Frame{Address: uint64(r.PC)})
	}
	if r.LR != 0 {
		r.Frames = append(r.Frames, Frame{Address: uint64(r.LR)})
		r.UnwindMethod = "xtensa-a0"
	}
	r.Confidence += 0.1
}

func decodeXtensaDetailed(exccause, excvaddr uint32) (causes []string, probable string, details map[string]any) {
	details = map[string]any{
		"exccause": exccause,
		"excvaddr": fmt.Sprintf("0x%08x", excvaddr),
	}
	names := map[uint32]string{
		0:  "IllegalInstructionCause",
		1:  "SyscallCause",
		2:  "InstructionFetchErrorCause",
		3:  "LoadStoreErrorCause",
		4:  "Level1InterruptCause",
		5:  "AllocaCause",
		6:  "IntegerDivideByZeroCause",
		7:  "PCValueErrorCause", // reserved/variant
		8:  "PrivilegedCause",
		9:  "LoadStoreAlignmentCause",
		12: "InstrPIFDataErrorCause",
		13: "LoadStorePIFDataErrorCause",
		14: "InstrPIFAddrErrorCause",
		15: "LoadStorePIFAddrErrorCause",
		16: "InstTLBMissCause",
		17: "InstTLBMultiHitCause",
		18: "InstFetchPrivilegeCause",
		20: "InstFetchProhibitedCause",
		24: "LoadStoreTLBMissCause",
		25: "LoadStoreTLBMultiHitCause",
		26: "LoadStorePrivilegeCause",
		28: "LoadProhibitedCause",
		29: "StoreProhibitedCause",
		32: "Coprocessor0Disabled",
		33: "Coprocessor1Disabled",
	}
	if name, ok := names[exccause]; ok {
		probable = name
		causes = append(causes, name)
	} else {
		probable = fmt.Sprintf("EXCCAUSE=%d", exccause)
		causes = append(causes, probable)
	}
	// address-related causes
	switch exccause {
	case 2, 3, 9, 12, 13, 14, 15, 16, 20, 24, 28, 29:
		if excvaddr != 0 {
			causes = append(causes, fmt.Sprintf("EXCVADDR=0x%08x", excvaddr))
		}
	}
	return
}

func unwindXtensa(stack []byte) []Frame {
	var frames []Frame
	seen := map[uint64]bool{}
	for i := 0; i+4 <= len(stack) && len(frames) < 32; i += 4 {
		a := uint64(binary.LittleEndian.Uint32(stack[i : i+4]))
		// ESP8266/ESP32 IRAM/IROM windows
		if seen[a] {
			continue
		}
		if (a >= 0x40000000 && a < 0x40400000) || (a >= 0x3ff00000 && a < 0x40000000) ||
			(a >= 0x40200000 && a < 0x40800000) {
			seen[a] = true
			frames = append(frames, Frame{Address: a})
		}
	}
	return frames
}
