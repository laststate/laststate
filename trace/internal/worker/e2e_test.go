package worker_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/db"
	"github.com/laststate/trace/internal/lep"
	"github.com/laststate/trace/internal/objects"
	"github.com/laststate/trace/internal/queue"
	"github.com/laststate/trace/internal/store"
	"github.com/laststate/trace/internal/worker"
)

func TestProcessCrashCreatesIssue(t *testing.T) {
	url := os.Getenv("TRACE_DATABASE_URL")
	if url == "" {
		url = "postgres://trace:trace@localhost:5432/trace?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Skip(err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	st := &store.Store{Pool: pool}
	p, err := st.DefaultProject(ctx)
	if err != nil {
		if _, p2, _, err2 := st.Bootstrap(ctx); err2 == nil {
			p = p2
		} else {
			t.Skip(err)
		}
	}
	dir := t.TempDir()
	obj := &objects.Store{Root: dir}
	if err := obj.Ensure(); err != nil {
		t.Fatal(err)
	}
	// build LEP crash
	idVal := []byte{2, 4, 'd', 'e', 'v', '1', 8, 6, 'b', 'u', 'i', 'l', 'd', '1'}
	payload := lep.EncodeTLVs([]lep.TLV{{Type: 1, Value: idVal}})
	raw, err := lep.Encode(lep.Header{Type: lep.TypeCrash, Architecture: 1, Sequence: 9, EventID: 99}, payload)
	if err != nil {
		t.Fatal(err)
	}
	key, hash, err := obj.Put(raw)
	if err != nil {
		t.Fatal(err)
	}
	res, err := st.CreateEventIdempotent(ctx, p.ID, "e2e-"+uuid.NewString(), 1, 1, 9, 99, "fatal", key, hash, int64(len(raw)), json.RawMessage(`{}`), "issue")
	if err != nil {
		t.Fatal(err)
	}
	w := &worker.Worker{Store: st, Object: obj, Queue: queue.NewMemory(), Lease: time.Second}
	if err := w.ProcessEventOnce(ctx, res.Event.ID); err != nil {
		t.Fatal(err)
	}
	ev, err := st.GetEventByID(ctx, res.Event.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ev.State != "ready" {
		t.Fatalf("state=%s", ev.State)
	}
	if ev.IssueID == nil {
		t.Fatal("expected issue")
	}
	iss, err := st.GetIssueInProject(ctx, p.ID, *ev.IssueID)
	if err != nil {
		t.Fatal(err)
	}
	if iss.EventCount < 1 {
		t.Fatal(iss)
	}
}

func TestProcessHealthNoIssue(t *testing.T) {
	url := os.Getenv("TRACE_DATABASE_URL")
	if url == "" {
		url = "postgres://trace:trace@localhost:5432/trace?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Skip(err)
	}
	defer pool.Close()
	_ = db.Migrate(ctx, pool)
	st := &store.Store{Pool: pool}
	p, err := st.DefaultProject(ctx)
	if err != nil {
		t.Skip(err)
	}
	obj := &objects.Store{Root: t.TempDir()}
	_ = obj.Ensure()
	raw, _ := lep.Encode(lep.Header{Type: lep.TypeHealth, Architecture: 1}, nil)
	key, hash, _ := obj.Put(raw)
	res, err := st.CreateEventIdempotent(ctx, p.ID, "health-"+uuid.NewString(), 4, 1, 1, 1, "info", key, hash, int64(len(raw)), json.RawMessage(`{}`), "health")
	if err != nil {
		t.Fatal(err)
	}
	w := &worker.Worker{Store: st, Object: obj, Queue: queue.NewMemory()}
	if err := w.ProcessEventOnce(ctx, res.Event.ID); err != nil {
		t.Fatal(err)
	}
	ev, _ := st.GetEventByID(ctx, res.Event.ID)
	if ev.State != "ready" {
		t.Fatal(ev.State)
	}
	if ev.IssueID != nil {
		t.Fatal("health must not create issue")
	}
}

func TestIdempotentReprocess(t *testing.T) {
	url := os.Getenv("TRACE_DATABASE_URL")
	if url == "" {
		url = "postgres://trace:trace@localhost:5432/trace?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Skip(err)
	}
	defer pool.Close()
	_ = db.Migrate(ctx, pool)
	st := &store.Store{Pool: pool}
	p, err := st.DefaultProject(ctx)
	if err != nil {
		t.Skip(err)
	}
	obj := &objects.Store{Root: t.TempDir()}
	_ = obj.Ensure()
	raw, _ := lep.Encode(lep.Header{Type: lep.TypeError, Architecture: 1}, nil)
	key, hash, _ := obj.Put(raw)
	res, err := st.CreateEventIdempotent(ctx, p.ID, "idem-"+uuid.NewString(), 2, 1, 1, 1, "error", key, hash, int64(len(raw)), json.RawMessage(`{}`), "issue")
	if err != nil {
		t.Fatal(err)
	}
	w := &worker.Worker{Store: st, Object: obj, Queue: queue.NewMemory()}
	if err := w.ProcessEventOnce(ctx, res.Event.ID); err != nil {
		t.Fatal(err)
	}
	// second process should no-op (already ready)
	if err := w.ProcessEventOnce(ctx, res.Event.ID); err != nil {
		t.Fatal(err)
	}
}
