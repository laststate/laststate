package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/laststate/trace/internal/auth"
)

// NATSQueue is an in-process JetStream-like buffer that can later wrap nats.go.
// When TRACE_QUEUE=nats without a real client, NewFromEnv can still use this
// for multi-worker in one process (better than silent postgres fallback).
type NATSQueue struct {
	mu    sync.Mutex
	ready []*memJob
	byID  map[uuid.UUID]*memJob
}

func NewNATSLocal() *NATSQueue {
	return &NATSQueue{byID: map[uuid.UUID]*memJob{}}
}

func (q *NATSQueue) Enqueue(ctx context.Context, typ string, payload any) error {
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
	j := &memJob{
		Job:       Job{ID: id, Type: typ, Payload: raw},
		status:    "ready",
		available: time.Now(),
		max:       20,
	}
	q.byID[id] = j
	q.ready = append(q.ready, j)
	return nil
}

func (q *NATSQueue) Depth(ctx context.Context) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return int64(len(q.ready)), nil
}

func (q *NATSQueue) Claim(ctx context.Context, leaseFor time.Duration) (Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := time.Now()
	// Reclaim expired leases back into ready
	for _, j := range q.byID {
		if j.status == "leased" && j.until.Before(now) {
			j.status = "retry"
			j.available = now
			j.Lease = ""
			q.ready = append(q.ready, j)
		}
	}
	for i, j := range q.ready {
		if j.status != "ready" && j.status != "retry" {
			continue
		}
		if j.available.After(now) {
			continue
		}
		j.status = "leased"
		j.Attempt++
		j.Lease = auth.RandomHex(8)
		j.until = now.Add(leaseFor)
		q.ready = append(q.ready[:i], q.ready[i+1:]...)
		return j.Job, nil
	}
	return Job{}, pgx.ErrNoRows
}

func (q *NATSQueue) Renew(ctx context.Context, id uuid.UUID, lease string, leaseFor time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.byID[id]
	if !ok || j.Lease != lease {
		return errLease
	}
	j.until = time.Now().Add(leaseFor)
	return nil
}

func (q *NATSQueue) Complete(ctx context.Context, id uuid.UUID, lease string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.byID[id]
	if !ok || j.Lease != lease {
		return errLease
	}
	j.status = "done"
	j.Lease = ""
	return nil
}

func (q *NATSQueue) Fail(ctx context.Context, id uuid.UUID, lease, msg string, retryIn time.Duration, maxAttempts int) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.byID[id]
	if !ok || j.Lease != lease {
		return errLease
	}
	j.err = msg
	j.Lease = ""
	if j.Attempt >= maxAttempts {
		j.status = "dead"
		return nil
	}
	j.status = "retry"
	j.available = time.Now().Add(retryIn)
	q.ready = append(q.ready, j)
	return nil
}

// CFQueue is a Cloudflare Queues-shaped local pull consumer.
type CFQueue struct {
	inner *MemoryQueue
}

func NewCFLocal() *CFQueue {
	return &CFQueue{inner: NewMemory()}
}

func (q *CFQueue) Enqueue(ctx context.Context, typ string, payload any) error {
	return q.inner.Enqueue(ctx, typ, payload)
}
func (q *CFQueue) Claim(ctx context.Context, leaseFor time.Duration) (Job, error) {
	return q.inner.Claim(ctx, leaseFor)
}
func (q *CFQueue) Renew(ctx context.Context, id uuid.UUID, lease string, leaseFor time.Duration) error {
	return q.inner.Renew(ctx, id, lease, leaseFor)
}
func (q *CFQueue) Complete(ctx context.Context, id uuid.UUID, lease string) error {
	return q.inner.Complete(ctx, id, lease)
}
func (q *CFQueue) Fail(ctx context.Context, id uuid.UUID, lease, msg string, retryIn time.Duration, maxAttempts int) error {
	return q.inner.Fail(ctx, id, lease, msg, retryIn, maxAttempts)
}
func (q *CFQueue) Depth(ctx context.Context) (int64, error) {
	return q.inner.Depth(ctx)
}

func (q *CFQueue) String() string { return fmt.Sprintf("cf-local") }

// NATSJetStreamLocal simulates JetStream stream + durable consumer semantics
// (subjects, sequence numbers, ack, redelivery) without the nats.go dependency.
type NATSJetStreamLocal struct {
	*NATSQueue
	seq uint64
}

func NewNATSJetStreamLocal() *NATSJetStreamLocal {
	return &NATSJetStreamLocal{NATSQueue: NewNATSLocal()}
}

func (q *NATSJetStreamLocal) Enqueue(ctx context.Context, typ string, payload any) error {
	// JetStream-style: wrap with subject + seq in payload envelope when map
	if m, ok := payload.(map[string]any); ok {
		q.mu.Lock()
		q.seq++
		seq := q.seq
		q.mu.Unlock()
		cp := map[string]any{}
		for k, v := range m {
			cp[k] = v
		}
		cp["_js"] = map[string]any{"stream": "TRACE", "subject": "trace.jobs." + typ, "seq": seq}
		return q.NATSQueue.Enqueue(ctx, typ, cp)
	}
	return q.NATSQueue.Enqueue(ctx, typ, payload)
}
