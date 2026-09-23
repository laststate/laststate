package symbolicate_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/laststate/trace/internal/symbolicate"
)

func TestResolveMissingArtifact(t *testing.T) {
	frames, warns, err := symbolicate.Resolve(filepath.Join(t.TempDir(), "nope.elf"), []uint64{0x1000, 0x2000})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 {
		t.Fatal(frames)
	}
	if len(warns) == 0 {
		t.Fatal("expected warning")
	}
}

func TestResolveEmpty(t *testing.T) {
	f, w, err := symbolicate.Resolve("/x", nil)
	if err != nil || f != nil || w != nil {
		t.Fatal(err, f, w)
	}
}

func TestResolveCapsAddresses(t *testing.T) {
	addrs := make([]uint64, 100)
	for i := range addrs {
		addrs[i] = uint64(i + 1)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(path, []byte{0x7f, 'E', 'L', 'F'}, 0o644); err != nil {
		t.Fatal(err)
	}
	frames, _, err := symbolicate.Resolve(path, addrs)
	if err != nil {
		t.Fatal(err)
	}
	// may return capped or raw
	if len(frames) > 64 {
		t.Fatalf("cap %d", len(frames))
	}
}

func TestResolveDirectoryRejected(t *testing.T) {
	frames, warns, err := symbolicate.Resolve(t.TempDir(), []uint64{1})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || len(warns) == 0 {
		t.Fatal(frames, warns)
	}
}
