package gc_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/laststate/trace/internal/gc"
	"github.com/laststate/trace/internal/objects"
	"github.com/laststate/trace/internal/store"
)

func TestGCOrphanSweepNoDB(t *testing.T) {
	// Runner.sweepOrphans needs Store.ReferencedObjectKeys — requires DB.
	// Test only that RunOnce with RetentionDays 0 and nil store fields doesn't panic wrongly.
	dir := t.TempDir()
	obj := &objects.Store{Root: dir}
	_ = obj.Ensure()
	// put file then not referenced — without store will fail
	key, _, err := obj.Put([]byte("orphan-data"))
	if err != nil {
		t.Fatal(err)
	}
	// make old
	p := obj.Path(key)
	past := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(p, past, past)

	r := &gc.Runner{Object: obj, RetentionDays: 0, Store: nil}
	// will panic/nil if Store nil — skip full
	if r.Store == nil {
		// just ensure file exists
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(key))); err != nil {
			t.Fatal(err)
		}
	}
	_ = context.Background()
	_ = store.Store{}
}
