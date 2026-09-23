// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package analysis

// Architecture codes - frozen with LEP v1 / Latch producer table.
// See protocol registry/architecture-codes.md.
const (
	ArchUnknown uint8 = 0
	ArchCortexM uint8 = 1
	ArchRISCV   uint8 = 2
	ArchXtensa  uint8 = 3
	ArchLinux   uint8 = 4
	ArchRISCV64 uint8 = 5
	// Codes >= 6 are unallocated; allocate via protocol RFC.
)

func ArchitectureName(code uint8) string {
	switch code {
	case ArchCortexM:
		return "cortex-m"
	case ArchRISCV:
		return "riscv"
	case ArchXtensa:
		return "xtensa"
	case ArchLinux:
		return "linux"
	case ArchRISCV64:
		return "riscv64"
	default:
		return "unknown"
	}
}

// MemoryRegion classifies addresses for fault analysis.
type MemoryRegion struct {
	Name  string
	Start uint64
	End   uint64 // exclusive
	Class string // flash, ram, peripheral, invalid, ...
}

func ClassifyAddress(address uint64, regions []MemoryRegion) string {
	for _, region := range regions {
		if address >= region.Start && address < region.End {
			if region.Class != "" {
				return region.Class
			}
			return region.Name
		}
	}
	return "unknown"
}
