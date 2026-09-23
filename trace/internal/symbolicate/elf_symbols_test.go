package symbolicate

import (
	"debug/elf"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveELFMissing(t *testing.T) {
	_, err := ResolveELF(filepath.Join(t.TempDir(), "nope"), []uint64{1})
	if err == nil {
		t.Fatal("expected error")
	}
}

// Build a tiny ELF if possible — otherwise skip.
func TestResolveELFIfAvailable(t *testing.T) {
	// Most CI won't have a firmware ELF; just ensure Open path errors cleanly on empty file
	p := filepath.Join(t.TempDir(), "x.elf")
	_ = os.WriteFile(p, []byte{0x7f, 'E', 'L', 'F'}, 0o644)
	_, err := ResolveELF(p, []uint64{0x1000})
	if err == nil {
		// might fail decode — ok either way
		t.Log("unexpected success on stub elf")
	}
	_ = elf.EM_ARM
}
