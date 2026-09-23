package queue_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/laststate/trace/internal/queue"
)

func TestMemoryQueueRoundTrip(t *testing.T) {
	q := queue.NewMemory()
	ctx := context.Background()
	if err := q.Enqueue(ctx, "process_event", map[string]string{"event_id": "x"}); err != nil {
		t.Fatal(err)
	}
	job, err := q.Claim(ctx, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if job.Type != "process_event" {
		t.Fatalf("type %s", job.Type)
	}
	if err := q.Renew(ctx, job.ID, job.Lease, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := q.Complete(ctx, job.ID, job.Lease); err != nil {
		t.Fatal(err)
	}
	_, err = q.Claim(ctx, time.Second)
	if err != pgx.ErrNoRows {
		t.Fatalf("expected empty, got %v", err)
	}
}

func TestMemoryQueueFailRetry(t *testing.T) {
	q := queue.NewMemory()
	ctx := context.Background()
	_ = q.Enqueue(ctx, "t", map[string]int{"n": 1})
	job, err := q.Claim(ctx, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Fail(ctx, job.ID, job.Lease, "boom", time.Millisecond, 5); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	job2, err := q.Claim(ctx, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if job2.Attempt < 2 {
		t.Fatal(job2.Attempt)
	}
}

func TestMemoryQueueLeaseMismatch(t *testing.T) {
	q := queue.NewMemory()
	ctx := context.Background()
	_ = q.Enqueue(ctx, "t", nil)
	job, _ := q.Claim(ctx, time.Second)
	if err := q.Complete(ctx, job.ID, "wrong"); err == nil {
		t.Fatal()
	}
	if err := q.Renew(ctx, job.ID, "wrong", time.Second); err == nil {
		t.Fatal()
	}
}

func TestMemoryQueueConcurrentClaims(t *testing.T) {
	q := queue.NewMemory()
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		_ = q.Enqueue(ctx, "t", map[string]int{"i": i})
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	ids := map[string]bool{}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				job, err := q.Claim(ctx, time.Second)
				if err != nil {
					return
				}
				mu.Lock()
				if ids[job.ID.String()] {
					t.Error("duplicate claim", job.ID)
				}
				ids[job.ID.String()] = true
				mu.Unlock()
				_ = q.Complete(ctx, job.ID, job.Lease)
			}
		}()
	}
	wg.Wait()
	if len(ids) != 20 {
		t.Fatalf("got %d", len(ids))
	}
}

func TestFactory(t *testing.T) {
	q, name, err := queue.NewFromEnv(nil, "memory", "")
	if err != nil || name != "memory" || q == nil {
		t.Fatal(err, name)
	}
	_, _, err = queue.NewFromEnv(nil, "postgres", "")
	if err == nil {
		t.Fatal("postgres without pool should fail")
	}
	_, _, err = queue.NewFromEnv(nil, "nope", "")
	if err == nil {
		t.Fatal()
	}
	q2, name, err := queue.NewFromEnv(nil, "nats", "memory")
	if err != nil || q2 == nil || name != "nats-memory-sim" {
		t.Fatal(err, name)
	}
	if _, _, err := queue.NewFromEnv(nil, "nats", "nats://broker:4222"); err == nil {
		t.Fatal("expected error for real nats URL without client")
	}
	q3, name, err := queue.NewFromEnv(nil, "cf", "")
	if err != nil || q3 == nil {
		t.Fatal(err)
	}
	q4, name, err := queue.NewFromEnv(nil, "redis", "redis://localhost:6379/0")
	if err != nil || name != "redis" {
		t.Fatal(err, name)
	}
	_ = q4
}

func TestNATSLocalQueue(t *testing.T) {
	q := queue.NewNATSLocal()
	ctx := context.Background()
	_ = q.Enqueue(ctx, "t", map[string]int{"a": 1})
	job, err := q.Claim(ctx, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Complete(ctx, job.ID, job.Lease); err != nil {
		t.Fatal(err)
	}
}
