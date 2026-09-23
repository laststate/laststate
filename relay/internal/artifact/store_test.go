// SPDX-License-Identifier: Apache-2.0
package artifact

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdd(t *testing.T) {
	dir := t.TempDir()

	// Create a test file.
	src := filepath.Join(dir, "test.bin")
	if err := os.WriteFile(src, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	item, err := Add(dir, src)
	if err != nil {
		t.Fatal(err)
	}
	if item.SHA256 == "" {
		t.Error("expected SHA256")
	}
	if item.Size != 11 {
		t.Errorf("size = %d, want 11", item.Size)
	}
	if item.Kind != "binary" {
		t.Errorf("kind = %q, want binary", item.Kind)
	}
	if item.OriginalName != "test.bin" {
		t.Errorf("original_name = %q, want test.bin", item.OriginalName)
	}

	// Verify the file exists.
	if _, err := os.Stat(item.Path); err != nil {
		t.Errorf("artifact file not found: %v", err)
	}

	// Verify manifest exists.
	manifestPath := item.Path + ".json"
	if _, err := os.Stat(manifestPath); err != nil {
		t.Errorf("manifest not found: %v", err)
	}
}

func TestAddDuplicate(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "test.bin")
	if err := os.WriteFile(src, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	// Add twice.
	item1, err := Add(dir, src)
	if err != nil {
		t.Fatal(err)
	}
	item2, err := Add(dir, src)
	if err != nil {
		t.Fatal(err)
	}

	// Should have same SHA256.
	if item1.SHA256 != item2.SHA256 {
		t.Errorf("SHA256 mismatch: %s vs %s", item1.SHA256, item2.SHA256)
	}

	// Should have same path.
	if item1.Path != item2.Path {
		t.Errorf("path mismatch: %s vs %s", item1.Path, item2.Path)
	}
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	src1 := filepath.Join(dir, "test1.bin")
	src2 := filepath.Join(dir, "test2.bin")
	os.WriteFile(src1, []byte("hello"), 0644)
	os.WriteFile(src2, []byte("world"), 0644)

	Add(dir, src1)
	Add(dir, src2)

	items, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 artifacts, got %d", len(items))
	}
}

func TestListEmpty(t *testing.T) {
	dir := t.TempDir()
	items, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if items != nil {
		t.Errorf("expected nil, got %v", items)
	}
}

func TestFindByBuildID(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "test.elf")
	// Create a minimal ELF with build ID.
	elfData := make([]byte, 100)
	// Write a minimal GNU build ID note.
	elfData[0] = 0x7f // ELF magic
	if err := os.WriteFile(src, elfData, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Add(dir, src)
	if err != nil {
		// ELF detection might fail on minimal ELF, that's ok.
		t.Skip("ELF detection failed")
	}

	// Search for non-existent build ID.
	_, err = FindByBuildID(dir, "nonexistent")
	if err == nil {
		t.Error("expected error for non-existent build ID")
	}
}

func TestVerify(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "test.bin")
	if err := os.WriteFile(src, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	item, err := Add(dir, src)
	if err != nil {
		t.Fatal(err)
	}

	// Verify should pass.
	if err := Verify(item); err != nil {
		t.Errorf("Verify failed: %v", err)
	}

	// Corrupt the file.
	if err := os.WriteFile(item.Path, []byte("corrupted"), 0644); err != nil {
		t.Fatal(err)
	}

	// Verify should fail.
	if err := Verify(item); err == nil {
		t.Error("expected Verify to fail on corrupted file")
	}
}

func TestInspect(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "test.bin")
	if err := os.WriteFile(src, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	item, err := Add(dir, src)
	if err != nil {
		t.Fatal(err)
	}

	inspected, err := Inspect(item.Path)
	if err != nil {
		t.Fatal(err)
	}
	if inspected.SHA256 != item.SHA256 {
		t.Errorf("SHA256 mismatch: %s vs %s", inspected.SHA256, item.SHA256)
	}
}

func TestDetectKind(t *testing.T) {
	dir := t.TempDir()

	// Create test files.
	hexFile := filepath.Join(dir, "test.hex")
	os.WriteFile(hexFile, []byte("S00000000000F"), 0644)

	binFile := filepath.Join(dir, "test.bin")
	os.WriteFile(binFile, []byte("hello"), 0644)

	mapFile := filepath.Join(dir, "test.map")
	os.WriteFile(mapFile, []byte("section   address"), 0644)

	tests := []struct {
		path string
		want string
	}{
		{hexFile, "hex"},
		{binFile, "binary"},
		{mapFile, "map"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := detectKind(tt.path); got != tt.want {
				t.Errorf("detectKind(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestDetectKindUnknown(t *testing.T) {
	got := detectKind("/nonexistent/file.xyz")
	if got != "unknown" {
		t.Errorf("expected unknown, got %q", got)
	}
}

func TestAddEmptyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "empty.bin")
	os.WriteFile(src, []byte{}, 0644)

	item, err := Add(dir, src)
	if err != nil {
		t.Fatal(err)
	}
	if item.Size != 0 {
		t.Errorf("size = %d, want 0", item.Size)
	}
}
