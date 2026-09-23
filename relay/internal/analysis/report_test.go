// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package analysis

import (
	"encoding/binary"
	"hash/crc32"
	"testing"
)

func TestAnalyzeCortexMFields(t *testing.T) {
	cpu := make([]byte, 138)
	cpu[0] = 1
	cpu[1] = 3
	binary.LittleEndian.PutUint32(cpu[130:134], 0x08001234)
	binary.LittleEndian.PutUint32(cpu[134:138], 0x08005678)
	payload := append([]byte{4, 0, byte(len(cpu)), 0}, cpu...)
	raw := makeEnvelope(payload)
	report, err := Analyze(raw)
	if err != nil {
		t.Fatal(err)
	}
	if report.CPU == nil || report.CPU.PC != 0x08005678 || report.CPU.LR != 0x08001234 {
		t.Fatalf("report: %#v", report)
	}
}

func TestDecodeCPU64Complete(t *testing.T) {
	value := make([]byte, 4+36*8)
	value[0] = 1    // encoding
	value[1] = 0x01 // COMPLETE
	value[2] = ArchRISCV64
	value[3] = 8                                                      // word size
	binary.LittleEndian.PutUint64(value[4+2*8:], 0xdeadbeef)          // x2
	binary.LittleEndian.PutUint64(value[4+31*8:], 0x1234567890abcdef) // x31 (sp)
	binary.LittleEndian.PutUint64(value[4+32*8:], 0x00000000f0000000) // mstatus
	binary.LittleEndian.PutUint64(value[4+33*8:], 15)                 // mcause
	binary.LittleEndian.PutUint64(value[4+34*8:], 0x1234)             // mtval
	binary.LittleEndian.PutUint64(value[4+35*8:], 0x80001234)         // mepc

	cpu64, warn := decodeCPU64(value)
	if cpu64 == nil {
		t.Fatal("expected CPU64 decode")
	}
	if warn != "" {
		t.Errorf("unexpected warning: %s", warn)
	}
	if !cpu64.Complete || cpu64.Architecture != ArchRISCV64 || cpu64.WordSize != 8 {
		t.Fatalf("cpu64: %#v", cpu64)
	}
	if cpu64.X[2] != 0xdeadbeef || cpu64.X[31] != 0x1234567890abcdef {
		t.Errorf("registers wrong: %#v", cpu64.X)
	}
	if cpu64.MSTATUS != 0x00000000f0000000 || cpu64.MCAUSE != 15 ||
		cpu64.MTVAL != 0x1234 || cpu64.MEPC != 0x80001234 {
		t.Errorf("csr wrong: %#v", cpu64)
	}
}

func TestDecodeCPU64Unavailable(t *testing.T) {
	value := []byte{1, 0x02, ArchRISCV64, 8}
	cpu64, warn := decodeCPU64(value)
	if cpu64 == nil {
		t.Fatal("expected CPU64 decode")
	}
	if warn != "" {
		t.Errorf("unexpected warning: %s", warn)
	}
	if cpu64.Complete {
		t.Error("expected incomplete CPU64")
	}
	if len(cpu64.X) != 0 {
		t.Errorf("unexpected registers: %#v", cpu64.X)
	}
}

func TestDecodeCPU64Truncated(t *testing.T) {
	if _, warn := decodeCPU64([]byte{1, 0x01}); warn == "" {
		t.Error("expected warning for truncated CPU64")
	}
}

func makeEnvelope(payload []byte) []byte {
	raw := make([]byte, 24+len(payload)+4)
	copy(raw, "LSTP")
	raw[4], raw[5] = 1, 2
	binary.LittleEndian.PutUint32(raw[8:12], 7)
	binary.LittleEndian.PutUint32(raw[12:16], 9)
	binary.LittleEndian.PutUint32(raw[16:20], uint32(len(payload)))
	binary.LittleEndian.PutUint32(raw[20:24], crc32.ChecksumIEEE(raw[:20]))
	copy(raw[24:], payload)
	binary.LittleEndian.PutUint32(raw[24+len(payload):], crc32.ChecksumIEEE(payload))
	return raw
}
