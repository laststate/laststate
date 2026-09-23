// SPDX-License-Identifier: Apache-2.0
package symbolicate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDemangle(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"main", "main"},
		{"_Z4mainE", "main"},
		{"_Z3foov", "foo"},
		{"_ZN3foo3barE", ""},                  // invalid length
		{"_R11hello_world", "hello_world..."}, // truncated rust
		{"_R11hello", "hello"},                // short rust
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := Demangle(tt.input); got != tt.want {
				t.Errorf("Demangle(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDemangleItanium(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{"_Z4mainE", "main", true},
		{"_Z3foov", "foo", true},
		{"_Z3foovv", "", false}, // invalid
		{"_Z", "", false},
		{"main", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := demangleItanium(tt.input)
			if ok != tt.ok {
				t.Errorf("demangleItanium(%q) ok = %v, want %v", tt.input, ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("demangleItanium(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPathMapRewrite(t *testing.T) {
	pm := PathMap{
		Prefix: "/build/",
		Maps: []PathRewrite{
			{From: "/src/", To: "src/"},
		},
	}

	tests := []struct {
		path string
		want string
	}{
		{"", ""},
		{"main.c", "main.c"},
		{"/build/src/main.c", "src/main.c"},
		{"/build/lib/foo.c", "lib/foo.c"},
		{"/other/main.c", "/other/main.c"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := pm.Rewrite(tt.path); got != tt.want {
				t.Errorf("PathMap.Rewrite(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestPathMapRewriteNoPrefix(t *testing.T) {
	pm := PathMap{
		Maps: []PathRewrite{
			{From: "/build/", To: ""},
		},
	}
	got := pm.Rewrite("/build/src/main.c")
	if got != "src/main.c" {
		t.Errorf("got %q, want src/main.c", got)
	}
}

func TestCacheGetPut(t *testing.T) {
	dir := t.TempDir()
	cache := &Cache{Dir: dir}

	// Put a frame.
	frame := Frame{Address: 0x1234, Function: "main", File: "main.c", Line: 42}
	err := cache.Put("abc123", frame)
	if err != nil {
		t.Fatal(err)
	}

	// Get it back.
	got, ok := cache.Get("abc123", 0x1234)
	if !ok {
		t.Fatal("expected frame to be found")
	}
	if got.Address != 0x1234 || got.Function != "main" {
		t.Errorf("got %+v, want %+v", got, frame)
	}

	// Get non-existent.
	_, ok = cache.Get("abc123", 0x5678)
	if ok {
		t.Error("expected frame to not be found")
	}

	// Nil cache.
	var nilCache *Cache
	_, ok = nilCache.Get("abc123", 0x1234)
	if ok {
		t.Error("nil cache should return false")
	}

	// Empty dir cache.
	emptyDir := &Cache{Dir: ""}
	_, ok = emptyDir.Get("abc123", 0x1234)
	if ok {
		t.Error("empty dir cache should return false")
	}
}

func TestCachePutEmptyArtifactSHA(t *testing.T) {
	dir := t.TempDir()
	cache := &Cache{Dir: dir}
	// Should not error, just silently skip.
	err := cache.Put("", Frame{Address: 0x1234})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCachePath(t *testing.T) {
	cache := &Cache{Dir: "/data"}
	got := cache.path("abc123", 0x1234)
	expected := filepath.Join("/data", "abc123", "0000000000001234.json")
	if got != expected {
		t.Errorf("path = %q, want %q", got, expected)
	}
}

func TestSplitFileLineCol(t *testing.T) {
	tests := []struct {
		input string
		file  string
		line  int
		col   int
	}{
		{"main.c:42", "main.c", 42, 0},
		{"main.c:42:10", "main.c", 42, 10},
		{"src/main.c:100:5:3", "src/main.c:100", 5, 3},
		{"no-colon", "no-colon", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			file, line, col := splitFileLineCol(tt.input)
			if file != tt.file || line != tt.line || col != tt.col {
				t.Errorf("splitFileLineCol(%q) = (%q, %d, %d), want (%q, %d, %d)",
					tt.input, file, line, col, tt.file, tt.line, tt.col)
			}
		})
	}
}

func TestResolveWithOptions(t *testing.T) {
	// Create a minimal ELF file for testing.
	dir := t.TempDir()
	elfPath := filepath.Join(dir, "test.elf")
	if err := os.WriteFile(elfPath, []byte("not a real elf"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a cache.
	cache := &Cache{Dir: t.TempDir()}

	// Resolve with a non-ELF file should error.
	_, err := ResolveWithOptions(elfPath, []uint64{0x1234}, Options{Cache: cache, ArtifactSHA: "test"})
	if err == nil {
		t.Error("expected error for non-ELF file")
	}

	// Resolve with cache hit.
	frame := Frame{Address: 0x1234, Function: "main", File: "main.c", Line: 42}
	if err := cache.Put("test", frame); err != nil {
		t.Fatal(err)
	}
	frames, err := ResolveWithOptions(elfPath, []uint64{0x1234}, Options{Cache: cache, ArtifactSHA: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || frames[0].Function != "main" {
		t.Errorf("unexpected frames: %+v", frames)
	}
}

func TestResolveDebugFile(t *testing.T) {
	dir := t.TempDir()
	elfPath := filepath.Join(dir, "test.elf")
	if err := os.WriteFile(elfPath, []byte("not a real elf"), 0644); err != nil {
		t.Fatal(err)
	}

	// Should return warnings about primary DWARF failure.
	_, warnings, err := ResolveDebugFile(elfPath, []uint64{0x1234}, Options{})
	if err == nil {
		t.Error("expected error")
	}
	if len(warnings) == 0 {
		t.Error("expected warnings")
	}
}
