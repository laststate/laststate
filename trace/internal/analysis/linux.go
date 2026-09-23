package analysis

import (
	"encoding/binary"
	"fmt"
)

// Linux / userspace fault TLV (arch code 4):
//  sig u32, si_code i32, fault_addr u64, pc u64, sp u64
//  optional: 8x u64 general regs (aarch64/x86_64 truncated)

type LinuxSignal struct {
	Signal    uint32 `json:"signal"`
	Code      int32  `json:"si_code"`
	FaultAddr uint64 `json:"fault_addr,omitempty"`
	PC64      uint64 `json:"pc,omitempty"`
	SP64      uint64 `json:"sp,omitempty"`
}

func applyLinuxFault(r *Report, value []byte) {
	if len(value) < 8 {
		r.Warnings = append(r.Warnings, "linux fault TLV truncated")
		return
	}
	sig := binary.LittleEndian.Uint32(value[0:4])
	code := int32(binary.LittleEndian.Uint32(value[4:8]))
	ls := LinuxSignal{Signal: sig, Code: code}
	if len(value) >= 16 {
		ls.FaultAddr = binary.LittleEndian.Uint64(value[8:16])
	}
	if len(value) >= 24 {
		ls.PC64 = binary.LittleEndian.Uint64(value[16:24])
		r.PC = uint32(ls.PC64) // low 32 for legacy field; full in details
	}
	if len(value) >= 32 {
		ls.SP64 = binary.LittleEndian.Uint64(value[24:32])
		r.SP = uint32(ls.SP64)
	}
	causes, probable, details := decodeLinuxSignal(ls)
	r.Causes, r.ProbableCause = causes, probable
	r.FaultDetails = details
	if ls.PC64 != 0 {
		r.Frames = append(r.Frames, Frame{Address: ls.PC64})
	}
	// optional regs as return addresses candidates
	for off := 32; off+8 <= len(value) && len(r.Frames) < 16; off += 8 {
		a := binary.LittleEndian.Uint64(value[off : off+8])
		if plausibleLinux(a) {
			r.Frames = append(r.Frames, Frame{Address: a})
		}
	}
	r.UnwindMethod = "linux-signal"
	r.Confidence += 0.15
}

func decodeLinuxSignal(ls LinuxSignal) (causes []string, probable string, details map[string]any) {
	details = map[string]any{
		"signal": ls.Signal, "si_code": ls.Code,
		"fault_addr": fmt.Sprintf("0x%x", ls.FaultAddr),
		"pc":         fmt.Sprintf("0x%x", ls.PC64),
		"sp":         fmt.Sprintf("0x%x", ls.SP64),
	}
	sigName := map[uint32]string{
		4: "SIGILL", 5: "SIGTRAP", 6: "SIGABRT", 7: "SIGBUS",
		8: "SIGFPE", 11: "SIGSEGV", 31: "SIGSYS",
	}
	name := sigName[ls.Signal]
	if name == "" {
		name = fmt.Sprintf("signal %d", ls.Signal)
	}
	details["signal_name"] = name

	codeName := linuxSiCode(ls.Signal, ls.Code)
	probable = name
	if codeName != "" {
		probable = name + ": " + codeName
		causes = append(causes, probable)
	} else {
		causes = append(causes, name)
	}
	if ls.FaultAddr != 0 {
		causes = append(causes, fmt.Sprintf("address 0x%x", ls.FaultAddr))
	}
	return
}

func linuxSiCode(sig uint32, code int32) string {
	// common si_code values
	switch sig {
	case 11: // SEGV
		switch code {
		case 1:
			return "SEGV_MAPERR (address not mapped)"
		case 2:
			return "SEGV_ACCERR (invalid permissions)"
		case 3:
			return "SEGV_BNDERR (bounds check)"
		case 4:
			return "SEGV_PKUERR (protection key)"
		}
	case 7: // BUS
		switch code {
		case 1:
			return "BUS_ADRALN (invalid alignment)"
		case 2:
			return "BUS_ADRERR (non-existent physical)"
		case 3:
			return "BUS_OBJERR (object-specific)"
		}
	case 8: // FPE
		switch code {
		case 1:
			return "FPE_INTDIV"
		case 2:
			return "FPE_INTOVF"
		case 3:
			return "FPE_FLTDIV"
		case 4:
			return "FPE_FLTOVF"
		case 5:
			return "FPE_FLTUND"
		case 6:
			return "FPE_FLTRES"
		case 7:
			return "FPE_FLTINV"
		}
	case 4: // ILL
		switch code {
		case 1:
			return "ILL_ILLOPC"
		case 2:
			return "ILL_ILLOPN"
		case 3:
			return "ILL_ILLADR"
		case 4:
			return "ILL_ILLTRP"
		}
	}
	if code > 0 {
		return fmt.Sprintf("si_code=%d", code)
	}
	return ""
}

func plausibleLinux(a uint64) bool {
	// userspace typical
	if a < 0x10000 {
		return false
	}
	if a >= 0x0000550000000000 && a < 0x0000570000000000 {
		return true // x86_64 PIE
	}
	if a >= 0x0000aaaa00000000 && a < 0x0000ab0000000000 {
		return true // aarch64
	}
	if a >= 0x400000 && a < 0x80000000 {
		return true // 32-bit
	}
	return false
}

func unwindLinux(stack []byte) []Frame {
	var frames []Frame
	seen := map[uint64]bool{}
	// try 8-byte then 4-byte
	for step := 8; step >= 4; step -= 4 {
		for i := 0; i+step <= len(stack) && len(frames) < 32; i += step {
			var a uint64
			if step == 8 {
				a = binary.LittleEndian.Uint64(stack[i : i+8])
			} else {
				a = uint64(binary.LittleEndian.Uint32(stack[i : i+4]))
			}
			if seen[a] || !plausibleLinux(a) {
				continue
			}
			seen[a] = true
			frames = append(frames, Frame{Address: a})
		}
		if len(frames) > 0 {
			break
		}
	}
	return frames
}
