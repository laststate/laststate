// Package analysis turns CPU/FAULT TLVs into a readable report.
package analysis

import (
	"encoding/binary"
	"fmt"

	"github.com/laststate/trace/internal/decode"
	"github.com/laststate/trace/internal/lep"
)

type Frame struct {
	Address  uint64 `json:"address"`
	Function string `json:"function,omitempty"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Inline   bool   `json:"inline,omitempty"`
}

// AnalyzerVersion bumps when analysis logic changes (triggers reprocess candidates).
const AnalyzerVersion = 6

type Report struct {
	Architecture     uint8          `json:"architecture"`
	ArchitectureName string         `json:"architecture_name"`
	EventType        uint8          `json:"event_type"`
	PC               uint32         `json:"pc,omitempty"`
	LR               uint32         `json:"lr,omitempty"`
	SP               uint32         `json:"sp,omitempty"`
	CFSR             uint32         `json:"cfsr,omitempty"`
	HFSR             uint32         `json:"hfsr,omitempty"`
	MMFAR            uint32         `json:"mmfar,omitempty"`
	BFAR             uint32         `json:"bfar,omitempty"`
	MCAUSE           uint32         `json:"mcause,omitempty"`
	MEPC             uint32         `json:"mepc,omitempty"`
	MTVAL            uint32         `json:"mtval,omitempty"`
	Causes           []string       `json:"causes,omitempty"`
	ProbableCause    string         `json:"probable_cause,omitempty"`
	Frames           []Frame        `json:"frames,omitempty"`
	Confidence       float64        `json:"confidence"`
	Summary          string         `json:"summary"`
	Warnings         []string       `json:"warnings,omitempty"`
	UnwindMethod     string         `json:"unwind_method,omitempty"`
	RTOS             *RTOSInfo      `json:"rtos,omitempty"`
	Probe            *ProbeInfo     `json:"probe,omitempty"`
	AnalyzerVersion  int            `json:"analyzer_version"`
	Stacked          *StackedFrame  `json:"stacked_frame,omitempty"`
	FaultDetails     map[string]any `json:"fault_details,omitempty"`
}

func FromDecoded(d decode.Decoded) Report {
	r := Report{
		Architecture:     d.Header.Architecture,
		ArchitectureName: lep.ArchName(d.Header.Architecture),
		EventType:        d.Header.Type,
		PC:               d.PC,
		LR:               d.LR,
		SP:               d.SP,
		Confidence:       0.4,
		AnalyzerVersion:  AnalyzerVersion,
	}
	for _, t := range d.TLVs {
		if t.Type == decode.TLVFault {
			applyFault(&r, t.Value, d.Header.Architecture)
		}
		if t.Type == decode.TLVStack {
			frames, method := unwindStack(t.Value, d.Header.Architecture, r.SP)
			if len(frames) > 0 {
				// prefer arch-specific frames first; append unique
				existing := map[uint64]bool{}
				for _, f := range r.Frames {
					existing[f.Address] = true
				}
				for _, f := range frames {
					if !existing[f.Address] {
						r.Frames = append(r.Frames, f)
					}
				}
				if r.UnwindMethod == "" {
					r.UnwindMethod = method
				}
				r.Confidence += 0.15
			}
		}
	}
	if len(r.Frames) == 0 {
		if r.PC != 0 {
			r.Frames = append(r.Frames, Frame{Address: uint64(r.PC &^ 1)})
			r.Confidence += 0.2
		}
		if r.LR != 0 {
			r.Frames = append(r.Frames, Frame{Address: uint64(r.LR &^ 1)})
			if r.UnwindMethod == "" {
				r.UnwindMethod = "lr-chain"
			}
		}
	}
	if r.ProbableCause != "" {
		r.Confidence += 0.2
	}
	if r.AssertHint(d) {
		r.Confidence += 0.1
	}
	EnrichRTOS(&r, d)
	if r.Confidence > 1 {
		r.Confidence = 1
	}
	r.Summary = buildSummary(r, d)
	return r
}

func unwindStack(stack []byte, arch uint8, sp uint32) ([]Frame, string) {
	if len(stack) < 4 {
		return nil, ""
	}
	// DWARF CFI stream (Relay / Trace dump format or standard CIE magic)
	if len(stack) >= 4 && string(stack[0:4]) == "CIE\x00" {
		frames, method := unwindDWARFCFI(stack, arch)
		return markInlines(frames), method
	}
	// ARM EHABI index + stack memory
	if len(stack) >= 4 && binary.LittleEndian.Uint32(stack[0:4]) == 0xFFFFFFFF {
		frames, method := unwindARMEHABI(stack, arch)
		return markInlines(frames), method
	}
	switch arch {
	case 1: // Cortex-M: exception stacked frame walk when raw stack given
		if f := walkWithRAOffsets(stack, []int32{20, 24, 0, 4, 8, 12, 16}, arch); len(f) > 0 {
			return markInlines(f), "cortexm-stacked"
		}
	case 2:
		if f := unwindRISCV(stack); len(f) > 0 {
			return markInlines(f), "riscv-stack"
		}
	case 3:
		if f := unwindXtensa(stack); len(f) > 0 {
			return markInlines(f), "xtensa-stack"
		}
	case 4:
		if f := unwindLinux(stack); len(f) > 0 {
			return markInlines(f), "linux-stack"
		}
	}
	_ = sp
	return markInlines(scanCodePointers(stack, arch)), "stack-scan"
}

func scanCodePointers(stack []byte, arch uint8) []Frame {
	var frames []Frame
	seen := map[uint64]bool{}
	step := 4
	if arch == 4 {
		step = 8
	}
	for i := 0; i+step <= len(stack) && len(frames) < 32; i += step {
		var addr uint64
		if step == 8 {
			addr = binary.LittleEndian.Uint64(stack[i : i+8])
		} else {
			a := binary.LittleEndian.Uint32(stack[i : i+4])
			if arch == 1 {
				a &^= 1
			}
			addr = uint64(a)
		}
		if !plausibleCode(addr, arch) || seen[addr] {
			continue
		}
		seen[addr] = true
		frames = append(frames, Frame{Address: addr})
	}
	return frames
}

func plausibleCode(addr uint64, arch uint8) bool {
	switch arch {
	case 1: // cortex-m
		if addr < 0x200 {
			return false
		}
		// flash, SRAM code, ITCM
		return (addr >= 0x00000000 && addr < 0x00100000) ||
			(addr >= 0x08000000 && addr < 0x08200000) ||
			(addr >= 0x10000000 && addr < 0x10100000) ||
			(addr >= 0x20000000 && addr < 0x20100000)
	case 2:
		return (addr >= 0x00010000 && addr < 0x00200000) ||
			(addr >= 0x20000000 && addr < 0x30000000) ||
			(addr >= 0x80000000 && addr < 0x90000000)
	case 3:
		return (addr >= 0x40000000 && addr < 0x40400000) ||
			(addr >= 0x40200000 && addr < 0x40800000)
	case 4:
		return plausibleLinux(addr)
	default:
		return addr > 0x200 && addr < 0xE0000000
	}
}

func (r *Report) AssertHint(d decode.Decoded) bool {
	if d.Assert == "" {
		return false
	}
	if r.ProbableCause == "" {
		r.ProbableCause = "assert: " + d.Assert
		r.Causes = append(r.Causes, r.ProbableCause)
	}
	return true
}

func applyFault(r *Report, value []byte, arch uint8) {
	switch arch {
	case 2:
		applyRISCVFault(r, value)
	case 3:
		applyXtensaFault(r, value)
	case 4:
		applyLinuxFault(r, value)
	default: // cortex-m
		if len(value) < 24 {
			r.Warnings = append(r.Warnings, "fault TLV truncated")
			return
		}
		r.CFSR = binary.LittleEndian.Uint32(value[0:4])
		r.HFSR = binary.LittleEndian.Uint32(value[4:8])
		r.MMFAR = binary.LittleEndian.Uint32(value[16:20])
		r.BFAR = binary.LittleEndian.Uint32(value[20:24])
		var details map[string]any
		r.Causes, r.ProbableCause, details = decodeCortexMDetailed(r.CFSR, r.HFSR, r.MMFAR, r.BFAR)
		r.FaultDetails = details
		if len(value) >= 56 {
			if sf := parseStackedFrame(value[24:56]); sf != nil {
				r.Stacked = sf
				if r.PC == 0 && sf.PC != 0 {
					r.PC = sf.PC &^ 1
				}
				if r.LR == 0 && sf.LR != 0 {
					r.LR = sf.LR
				}
				r.Frames = append([]Frame{{Address: uint64(sf.PC &^ 1)}}, r.Frames...)
				if sf.LR != 0 {
					r.Frames = append(r.Frames, Frame{Address: uint64(sf.LR &^ 1)})
				}
				r.UnwindMethod = "stacked-frame"
				r.Confidence += 0.1
			}
		}
	}
}

func decodeCortexM(cfsr, hfsr uint32) (causes []string, probable string) {
	causes, probable, _ = decodeCortexMDetailed(cfsr, hfsr, 0, 0)
	return
}

func decodeRISCV(mcause uint32) (causes []string, probable string) {
	causes, probable, _ = decodeRISCVDetailed(mcause, 0)
	return
}

func decodeXtensa(exccause uint32) (causes []string, probable string) {
	causes, probable, _ = decodeXtensaDetailed(exccause, 0)
	return
}

func buildSummary(r Report, d decode.Decoded) string {
	etype := lep.EventTypeName(d.Header.Type)
	if r.ProbableCause != "" {
		return fmt.Sprintf("%s on %s: %s", etype, r.ArchitectureName, r.ProbableCause)
	}
	if r.PC != 0 {
		return fmt.Sprintf("%s on %s at 0x%08x", etype, r.ArchitectureName, r.PC)
	}
	return fmt.Sprintf("%s on %s", etype, r.ArchitectureName)
}
