package queue

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/laststate/trace/internal/auth"
)

// MemoryQueue is an in-process Jober for tests and single-node TRACE_QUEUE=memory.
type MemoryQueue struct {
	mu   sync.Mutex
	jobs map[uuid.UUID]*memJob
}

type memJob struct {
	Job
	status    string
	available time.Time
	until     time.Time
	max       int
	err       string
}

func NewMemory() *MemoryQueue {
	return &MemoryQueue{jobs: map[uuid.UUID]*memJob{}}
}

func (q *MemoryQueue) Enqueue(ctx context.Context, typ string, payload any) error {
	if err := GuardEnqueue(ctx, q); err != nil {
		return err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	id := uuid.New()
	q.jobs[id] = &memJob{
		Job:       Job{ID: id, Type: typ, Payload: raw, Attempt: 0},
		status:    "ready",
		available: time.Now(),
		max:       20,
	}
	return nil
}

func (q *MemoryQueue) Depth(ctx context.Context) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var n int64
	for _, j := range q.jobs {
		if j.status == "ready" || j.status == "retry" {
			n++
		}
	}
	return n, nil
}

func (q *MemoryQueue) Claim(ctx context.Context, leaseFor time.Duration) (Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := time.Now()
	// First pass: reset expired leases to retry
	for _, j := range q.jobs {
		if j.status == "leased" && j.until.Before(now) {
			j.status = "retry"
			j.available = now
		}
	}
	// Second pass: claim available jobs
	for _, j := range q.jobs {
		if (j.status == "ready" || j.status == "retry") && !j.available.After(now) {
			j.status = "leased"
			j.Attempt++
			j.Lease = auth.RandomHex(8)
			j.until = now.Add(leaseFor)
			return j.Job, nil
		}
	}
	return Job{}, pgx.ErrNoRows
}

func (q *MemoryQueue) Renew(ctx context.Context, id uuid.UUID, lease string, leaseFor time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[id]
	if !ok || j.Lease != lease || j.status != "leased" {
		return errLease
	}
	j.until = time.Now().Add(leaseFor)
	return nil
}

func (q *MemoryQueue) Complete(ctx context.Context, id uuid.UUID, lease string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[id]
	if !ok || j.Lease != lease {
		return errLease
	}
	j.status = "done"
	j.Lease = ""
	return nil
}

func (q *MemoryQueue) Fail(ctx context.Context, id uuid.UUID, lease, msg string, retryIn time.Duration, maxAttempts int) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[id]
	if !ok || j.Lease != lease {
		return errLease
	}
	j.err = msg
	j.Lease = ""
	if j.Attempt >= maxAttempts {
		j.status = "dead"
	} else {
		j.status = "retry"
		j.available = time.Now().Add(retryIn)
	}
	return nil
}

var errLease = errString("job lease mismatch")

type errString string

func (e errString) Error() string { return string(e) }
