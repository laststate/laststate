// SPDX-License-Identifier: Apache-2.0
package analysis

import "testing"

func TestArchitectureName(t *testing.T) {
	tests := []struct {
		code uint8
		name string
	}{
		{ArchUnknown, "unknown"},
		{ArchCortexM, "cortex-m"},
		{ArchRISCV, "riscv"},
		{ArchXtensa, "xtensa"},
		{ArchLinux, "linux"},
		{ArchRISCV64, "riscv64"},
		{255, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ArchitectureName(tt.code); got != tt.name {
				t.Errorf("ArchitectureName(%d) = %q, want %q", tt.code, got, tt.name)
			}
		})
	}
}

func TestClassifyAddress(t *testing.T) {
	regions := []MemoryRegion{
		{Name: "flash", Start: 0x08000000, End: 0x08100000, Class: "flash"},
		{Name: "ram", Start: 0x20000000, End: 0x20010000, Class: "ram"},
		{Name: "periph", Start: 0x40000000, End: 0x40010000, Class: "peripheral"},
	}

	tests := []struct {
		addr uint64
		want string
	}{
		{0x08000000, "flash"},
		{0x08050000, "flash"},
		{0x08100000, "unknown"},
		{0x20000000, "ram"},
		{0x2000FFFF, "ram"},
		{0x20010000, "unknown"},
		{0x40000000, "peripheral"},
		{0x10000000, "unknown"},
	}
	for _, tt := range tests {
		t.Run(string(rune(tt.addr)), func(t *testing.T) {
			if got := ClassifyAddress(tt.addr, regions); got != tt.want {
				t.Errorf("ClassifyAddress(0x%x) = %q, want %q", tt.addr, got, tt.want)
			}
		})
	}
}

func TestClassifyAddressNoRegions(t *testing.T) {
	if got := ClassifyAddress(0x12345678, nil); got != "unknown" {
		t.Errorf("expected unknown, got %q", got)
	}
	if got := ClassifyAddress(0x12345678, []MemoryRegion{}); got != "unknown" {
		t.Errorf("expected unknown, got %q", got)
	}
}
