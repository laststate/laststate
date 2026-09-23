// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// Package analysis decodes crash TLVs into readable reports.
package analysis

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/laststate/relay/internal/lep"
	"github.com/laststate/relay/internal/symbolicate"
)

// Report is the structured offline analysis result for one LEP incident.
type Report struct {
	ProtocolVersion  uint8               `json:"protocol_version"`
	EventID          uint32              `json:"event_id"`
	Sequence         uint32              `json:"sequence"`
	EventType        uint8               `json:"event_type"`
	Architecture     uint8               `json:"architecture"`
	ArchitectureName string              `json:"architecture_name,omitempty"`
	CPU              *CPU                `json:"cpu,omitempty"`
	CPU64            *CPU64              `json:"cpu64,omitempty"`
	Fault            *Fault              `json:"fault,omitempty"`
	Frames           []symbolicate.Frame `json:"frames,omitempty"`
	Confidence       float64             `json:"confidence,omitempty"`
	MemoryClass      string              `json:"memory_class,omitempty"`
	Warnings         []string            `json:"warnings,omitempty"`
}

type CPU struct {
	Architecture uint8  `json:"architecture"`
	Fault        uint8  `json:"fault"`
	LR           uint32 `json:"lr"`
	PC           uint32 `json:"pc"`
	SP           uint32 `json:"sp,omitempty"`
}

// CPU64 decodes a LEP TLV 16 CPU64 capability descriptor (Latch riscv64).
type CPU64 struct {
	Encoding     uint8    `json:"encoding"`
	Flags        uint8    `json:"flags"`
	Architecture uint8    `json:"architecture"`
	WordSize     uint8    `json:"word_size"`
	Complete     bool     `json:"complete"`
	X            []uint64 `json:"x,omitempty"`
	MSTATUS      uint64   `json:"mstatus,omitempty"`
	MCAUSE       uint64   `json:"mcause,omitempty"`
	MTVAL        uint64   `json:"mtval,omitempty"`
	MEPC         uint64   `json:"mepc,omitempty"`
}

type Fault struct {
	CFSR  uint32 `json:"cfsr,omitempty"`
	HFSR  uint32 `json:"hfsr,omitempty"`
	MMFAR uint32 `json:"mmfar,omitempty"`
	BFAR  uint32 `json:"bfar,omitempty"`
	// RISC-V style
	MCAUSE     uint32   `json:"mcause,omitempty"`
	MEPC       uint32   `json:"mepc,omitempty"`
	MTVAL      uint32   `json:"mtval,omitempty"`
	Causes     []string `json:"causes,omitempty"`
	Probable   string   `json:"probable_cause,omitempty"`
	MMFARValid bool     `json:"mmfar_valid,omitempty"`
	BFARValid  bool     `json:"bfar_valid,omitempty"`
}

type AnalyzeOptions struct {
	ArtifactPath string
	MemoryMap    []MemoryRegion
	PathMap      symbolicate.PathMap
	Cache        *symbolicate.Cache
	ArtifactSHA  string
	Keyring      lep.Keyring
}

func Analyze(raw []byte) (Report, error) { return AnalyzeWithArtifact(raw, "") }

func AnalyzeWithArtifact(raw []byte, artifactPath string) (Report, error) {
	return AnalyzeWithOptions(raw, AnalyzeOptions{ArtifactPath: artifactPath})
}

func AnalyzeWithOptions(raw []byte, opts AnalyzeOptions) (Report, error) {
	envelope, err := lep.Validate(raw)
	if err != nil {
		return Report{}, err
	}
	report := Report{
		ProtocolVersion:  envelope.Version,
		EventID:          envelope.EventID,
		Sequence:         envelope.Sequence,
		EventType:        envelope.Type,
		Architecture:     envelope.Architecture,
		ArchitectureName: ArchitectureName(envelope.Architecture),
		Confidence:       0.5,
	}
	fields, err := lep.DecodeTLVs(raw, opts.Keyring)
	if err != nil {
		// Fall back to plain TLVs for unauthenticated envelopes.
		fields, err = lep.TLVs(raw)
		if err != nil {
			report.Warnings = append(report.Warnings, err.Error())
			return report, nil
		}
	}
	arch := envelope.Architecture
	for _, field := range fields {
		switch field.Type {
		case 4: // LS_TLV_CPU_CONTEXT
			cpu, warn := decodeCPU(field.Value, arch)
			if warn != "" {
				report.Warnings = append(report.Warnings, warn)
			}
			if cpu != nil {
				report.CPU = cpu
				if cpu.Architecture != 0 {
					arch = cpu.Architecture
					report.ArchitectureName = ArchitectureName(arch)
				}
			}
		case 5: // LS_TLV_FAULT_REGISTERS
			fault, warn := decodeFault(field.Value, arch)
			if warn != "" {
				report.Warnings = append(report.Warnings, warn)
			}
			report.Fault = fault
		case 16: // LS_TLV_CPU64
			if cpu64, warn := decodeCPU64(field.Value); cpu64 != nil {
				report.CPU64 = cpu64
				if cpu64.Architecture != 0 {
					arch = cpu64.Architecture
					report.Architecture = cpu64.Architecture
					report.ArchitectureName = ArchitectureName(arch)
				}
				if warn != "" {
					report.Warnings = append(report.Warnings, warn)
				}
			}
		}
	}
	if report.CPU != nil && len(opts.MemoryMap) > 0 {
		report.MemoryClass = ClassifyAddress(uint64(report.CPU.PC), opts.MemoryMap)
	}
	report.Confidence = scoreConfidence(report)

	if opts.ArtifactPath != "" && report.CPU != nil {
		addresses := []uint64{uint64(report.CPU.PC &^ 1)}
		if report.CPU.LR != 0 {
			addresses = append(addresses, uint64(report.CPU.LR&^1))
		}
		symOpts := symbolicate.Options{PathMap: opts.PathMap, Cache: opts.Cache, ArtifactSHA: opts.ArtifactSHA}
		frames, warns, err := symbolicate.ResolveDebugFile(opts.ArtifactPath, addresses, symOpts)
		report.Warnings = append(report.Warnings, warns...)
		if err != nil {
			report.Warnings = append(report.Warnings, "symbolication: "+err.Error())
		} else {
			report.Frames = frames
		}
	}
	return report, nil
}

func decodeCPU(value []byte, arch uint8) (*CPU, string) {
	if len(value) < 2 {
		return nil, fmt.Sprintf("CPU context TLV is truncated (%d bytes)", len(value))
	}
	cpuArch := value[0]
	if cpuArch == 0 {
		cpuArch = arch
	}
	cpu := &CPU{Architecture: cpuArch, Fault: value[1]}
	switch cpuArch {
	case ArchRISCV:
		// proposal: lr@8, pc@12, sp@16 when enough bytes; else fall through to cortex offsets
		if len(value) >= 20 {
			cpu.LR = binary.LittleEndian.Uint32(value[8:12])
			cpu.PC = binary.LittleEndian.Uint32(value[12:16])
			cpu.SP = binary.LittleEndian.Uint32(value[16:20])
			return cpu, ""
		}
	}
	// Default Latch multi-arch CPU TLV (Cortex-M / RISC-V / Xtensa / Linux).
	if len(value) >= 138 {
		cpu.LR = binary.LittleEndian.Uint32(value[130:134])
		cpu.PC = binary.LittleEndian.Uint32(value[134:138])
		if len(value) >= 130 {
			cpu.SP = binary.LittleEndian.Uint32(value[126:130])
		}
		return cpu, ""
	}
	if len(value) >= 12 {
		cpu.PC = binary.LittleEndian.Uint32(value[4:8])
		cpu.LR = binary.LittleEndian.Uint32(value[8:12])
		return cpu, fmt.Sprintf("CPU context used short layout (%d bytes)", len(value))
	}
	return nil, fmt.Sprintf("CPU context TLV is truncated (%d bytes)", len(value))
}

func decodeCPU64(value []byte) (*CPU64, string) {
	const (
		flagComplete  = 0x01
		flagUnavail   = 0x02
		registerCount = 32
		csrCount      = 4
		fullLength    = 4 + (registerCount+csrCount)*8
	)
	if len(value) < 4 {
		return nil, fmt.Sprintf("CPU64 TLV is truncated (%d bytes)", len(value))
	}
	cpu64 := &CPU64{
		Encoding:     value[0],
		Flags:        value[1],
		Architecture: value[2],
		WordSize:     value[3],
	}
	switch cpu64.Flags {
	case flagComplete:
		cpu64.Complete = true
	case flagUnavail:
		cpu64.Complete = false
	default:
		return cpu64, fmt.Sprintf("CPU64 flags 0x%02x unrecognized", cpu64.Flags)
	}
	if !cpu64.Complete {
		return cpu64, ""
	}
	if len(value) < fullLength {
		return cpu64, fmt.Sprintf("CPU64 complete value truncated (%d bytes, want %d)", len(value), fullLength)
	}
	if cpu64.WordSize != 8 {
		return cpu64, fmt.Sprintf("CPU64 word size %d unsupported", cpu64.WordSize)
	}
	// Registers followed by four CSRs (mstatus, mcause, mtval, mepc), each 8
	// bytes. fullLength (292) was validated above so these bounds are safe.
	regs := value[4 : 4+registerCount*8]
	csr := value[4+registerCount*8 : fullLength] //nolint:gosec // bounds validated by len(value) < fullLength above
	cpu64.X = make([]uint64, registerCount)
	for i := 0; i < registerCount; i++ {
		cpu64.X[i] = binary.LittleEndian.Uint64(regs[i*8 : (i+1)*8]) //nolint:gosec // regs is exactly registerCount*8 bytes
	}
	cpu64.MSTATUS = binary.LittleEndian.Uint64(csr[0:8]) //nolint:gosec // csr is a fixed 32-byte slice (validated length)
	cpu64.MCAUSE = binary.LittleEndian.Uint64(csr[8:16]) //nolint:gosec // csr is a fixed 32-byte slice (validated length)
	cpu64.MTVAL = binary.LittleEndian.Uint64(csr[16:24]) //nolint:gosec // csr is a fixed 32-byte slice (validated length)
	cpu64.MEPC = binary.LittleEndian.Uint64(csr[24:32])  //nolint:gosec // csr is a fixed 32-byte slice (validated length)
	return cpu64, ""
}

func decodeFault(value []byte, arch uint8) (*Fault, string) {
	switch arch {
	case ArchRISCV:
		if len(value) >= 12 {
			fault := &Fault{
				MCAUSE: binary.LittleEndian.Uint32(value[0:4]),
				MEPC:   binary.LittleEndian.Uint32(value[4:8]),
				MTVAL:  binary.LittleEndian.Uint32(value[8:12]),
			}
			fault.Causes, fault.Probable = decodeRISCVFault(fault.MCAUSE)
			return fault, ""
		}
		return nil, fmt.Sprintf("RISC-V fault TLV truncated (%d bytes)", len(value))
	default:
		if len(value) >= 24 {
			fault := &Fault{
				CFSR:  binary.LittleEndian.Uint32(value[0:4]),
				HFSR:  binary.LittleEndian.Uint32(value[4:8]),
				MMFAR: binary.LittleEndian.Uint32(value[16:20]),
				BFAR:  binary.LittleEndian.Uint32(value[20:24]),
			}
			fault.Causes, fault.Probable, fault.MMFARValid, fault.BFARValid = decodeCortexMFault(fault.CFSR, fault.HFSR)
			return fault, ""
		}
		return nil, fmt.Sprintf("fault-register TLV is truncated (%d bytes)", len(value))
	}
}

func decodeRISCVFault(mcause uint32) (causes []string, probable string) {
	interrupt := mcause>>31 != 0
	code := mcause & 0x7fffffff
	if interrupt {
		probable = fmt.Sprintf("interrupt cause %d", code)
		causes = []string{probable}
		return causes, probable
	}
	names := map[uint32]string{
		0:  "instruction address misaligned",
		1:  "instruction access fault",
		2:  "illegal instruction",
		3:  "breakpoint",
		4:  "load address misaligned",
		5:  "load access fault",
		6:  "store/AMO address misaligned",
		7:  "store/AMO access fault",
		8:  "environment call from U-mode",
		11: "environment call from M-mode",
	}
	if name, ok := names[code]; ok {
		return []string{name}, name
	}
	probable = fmt.Sprintf("exception cause %d", code)
	return []string{probable}, probable
}

func decodeCortexMFault(cfsr, hfsr uint32) (causes []string, probable string, mmfarValid, bfarValid bool) {
	bits := []struct {
		mask uint32
		text string
	}{
		{1 << 0, "instruction access violation"}, {1 << 1, "data access violation"},
		{1 << 3, "MemManage unstacking error"}, {1 << 4, "MemManage stacking error"},
		{1 << 5, "MemManage lazy floating-point preservation error"},
		{1 << 8, "instruction bus error"}, {1 << 9, "precise data bus error"},
		{1 << 10, "imprecise data bus error"}, {1 << 11, "BusFault unstacking error"},
		{1 << 12, "BusFault stacking error"}, {1 << 13, "BusFault lazy floating-point preservation error"},
		{1 << 16, "undefined instruction"}, {1 << 17, "invalid execution state"},
		{1 << 18, "invalid exception return"}, {1 << 19, "coprocessor access error"},
		{1 << 24, "unaligned access"}, {1 << 25, "division by zero"},
	}
	for _, bit := range bits {
		if cfsr&bit.mask != 0 {
			causes = append(causes, bit.text)
		}
	}
	mmfarValid = cfsr&(1<<7) != 0
	bfarValid = cfsr&(1<<15) != 0
	if hfsr&(1<<30) != 0 {
		causes = append(causes, "configurable fault escalated to HardFault")
	}
	if hfsr&(1<<1) != 0 {
		causes = append(causes, "vector table read fault")
	}
	if hfsr&(1<<31) != 0 {
		causes = append(causes, "debug event generated HardFault")
	}
	if len(causes) > 0 {
		probable = causes[0]
	}
	return causes, probable, mmfarValid, bfarValid
}

func scoreConfidence(report Report) float64 {
	score := 0.4
	if report.CPU != nil && report.CPU.PC != 0 {
		score += 0.2
	}
	if report.Fault != nil && report.Fault.Probable != "" {
		score += 0.2
	}
	if len(report.Frames) > 0 && report.Frames[0].Function != "" {
		score += 0.15
	}
	if report.MemoryClass != "" && report.MemoryClass != "unknown" {
		score += 0.05
	}
	if score > 1 {
		score = 1
	}
	return score
}

func (report Report) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Last State incident\n\nEvent ID: %d\nSequence: %d\nArchitecture: %s (%d)\nConfidence: %.2f\n",
		report.EventID, report.Sequence, report.ArchitectureName, report.Architecture, report.Confidence)
	if report.CPU != nil {
		fmt.Fprintf(&b, "CPU PC: 0x%08x\nCPU LR: 0x%08x\n", report.CPU.PC, report.CPU.LR)
	}
	if report.MemoryClass != "" {
		fmt.Fprintf(&b, "PC memory class: %s\n", report.MemoryClass)
	}
	if report.Fault != nil {
		if report.Fault.CFSR != 0 || report.Fault.HFSR != 0 {
			fmt.Fprintf(&b, "Fault CFSR: 0x%08x\nFault HFSR: 0x%08x\n", report.Fault.CFSR, report.Fault.HFSR)
		}
		if report.Fault.MCAUSE != 0 || report.Fault.MEPC != 0 {
			fmt.Fprintf(&b, "Fault MCAUSE: 0x%08x\nFault MEPC: 0x%08x\nFault MTVAL: 0x%08x\n", report.Fault.MCAUSE, report.Fault.MEPC, report.Fault.MTVAL)
		}
		if report.Fault.Probable != "" {
			fmt.Fprintf(&b, "Probable cause: %s\n", report.Fault.Probable)
		}
		for _, cause := range report.Fault.Causes {
			fmt.Fprintf(&b, "  - %s\n", cause)
		}
		if report.Fault.MMFARValid {
			fmt.Fprintf(&b, "MMFAR: 0x%08x\n", report.Fault.MMFAR)
		}
		if report.Fault.BFARValid {
			fmt.Fprintf(&b, "BFAR: 0x%08x\n", report.Fault.BFAR)
		}
	}
	if report.CPU64 != nil {
		fmt.Fprintf(&b, "CPU64: complete=%t word_size=%d\n", report.CPU64.Complete, report.CPU64.WordSize)
		if report.CPU64.Complete && report.CPU64.MEPC != 0 {
			fmt.Fprintf(&b, "CPU64 PC (mepc): 0x%016x\nCPU64 MCAUSE: 0x%016x\nCPU64 MTVAL: 0x%016x\n",
				report.CPU64.MEPC, report.CPU64.MCAUSE, report.CPU64.MTVAL)
		}
	}
	if len(report.Frames) > 0 {
		b.WriteString("\nSymbolication\n")
		for _, frame := range report.Frames {
			location := frame.File
			if frame.Line > 0 {
				location = fmt.Sprintf("%s:%d", location, frame.Line)
			}
			inline := ""
			if frame.Inline {
				inline = " [inline]"
			}
			fmt.Fprintf(&b, "  0x%08x  %s%s  %s\n", frame.Address, defaultString(frame.Function, "<unknown>"), inline, location)
		}
	}
	for _, warning := range report.Warnings {
		fmt.Fprintf(&b, "Warning: %s\n", warning)
	}
	return b.String()
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
