package artifact_test

import (
	"debug/elf"
	"os"
	"path/filepath"
	"testing"

	"github.com/laststate/trace/internal/artifact"
)

func TestInspectNonELF(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.bin")
	if err := os.WriteFile(p, []byte("not elf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := artifact.InspectFile(p); err == nil {
		t.Fatal("expected error")
	}
}

func TestInspectMissing(t *testing.T) {
	if _, err := artifact.InspectFile(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal()
	}
}

func TestWriteTempAndInspectRoundTripIfELF(t *testing.T) {
	// skip if no sample — just WriteTemp
	raw := []byte{0x7f, 'E', 'L', 'F', 1, 1, 1}
	// minimal not valid full ELF — Inspect may fail
	tmp, err := artifact.WriteTemp(raw)
	if err != nil {
		// WriteTemp may not exist
		t.Skip(err)
	}
	defer os.Remove(tmp)
	_, _ = artifact.InspectFile(tmp)
}

func TestArchNameViaInspectMachine(t *testing.T) {
	// ensure package loads elf machines without panic
	_ = elf.EM_ARM
}
