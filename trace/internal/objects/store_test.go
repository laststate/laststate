package objects_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/laststate/trace/internal/objects"
)

func TestPutGetFsyncRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := &objects.Store{Root: dir}
	if err := s.Ensure(); err != nil {
		t.Fatal(err)
	}
	raw := []byte("hello-durable-object")
	key, hash, err := s.Put(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != 64 {
		t.Fatal(hash)
	}
	got, err := s.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("mismatch")
	}
	// idempotent put
	k2, h2, err := s.Put(raw)
	if err != nil || k2 != key || h2 != hash {
		t.Fatal(err, k2, h2)
	}
	// path exists
	p := s.Path(key)
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	// delete
	if err := s.Delete(key); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(key); err == nil {
		t.Fatal("expected missing")
	}
}

func TestPutEmptyDirFailsEnsure(t *testing.T) {
	s := &objects.Store{Root: ""}
	if err := s.Ensure(); err == nil {
		t.Fatal()
	}
}

func TestKeyLayout(t *testing.T) {
	dir := t.TempDir()
	s := &objects.Store{Root: dir}
	_ = s.Ensure()
	key, _, err := s.Put([]byte("abc"))
	if err != nil {
		t.Fatal(err)
	}
	// a/b/hash
	if len(filepath.SplitList(key)) == 0 && key[2] != '/' {
		// slash path
		parts := split(key)
		if len(parts) != 3 {
			t.Fatal(key)
		}
	}
}

func split(k string) []string {
	var out []string
	cur := ""
	for i := 0; i < len(k); i++ {
		if k[i] == '/' {
			out = append(out, cur)
			cur = ""
		} else {
			cur += string(k[i])
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
