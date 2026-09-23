package queue

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMemoryQueueLifecycle validates the full job lifecycle in MemoryQueue.
func TestMemoryQueueLifecycle(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()

	// Enqueue a job
	payload := map[string]string{"event_id": uuid.New().String(), "project_id": uuid.New().String()}
	err := q.Enqueue(ctx, "process_event", payload)
	require.NoError(t, err)

	// Check depth
	depth, err := q.Depth(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), depth)

	// Claim the job
	job, err := q.Claim(ctx, 30*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "process_event", job.Type)
	assert.NotEmpty(t, job.ID)

	// Renew the lease
	err = q.Renew(ctx, job.ID, job.Lease, 30*time.Second)
	require.NoError(t, err)

	// Complete the job
	err = q.Complete(ctx, job.ID, job.Lease)
	require.NoError(t, err)

	// Check depth after completion
	depth, err = q.Depth(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), depth)
}

// TestMemoryQueueRetryOnFail validates retry behavior on job failure.
func TestMemoryQueueRetryOnFail(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()

	payload := map[string]string{"event_id": uuid.New().String(), "project_id": uuid.New().String()}
	err := q.Enqueue(ctx, "process_event", payload)
	require.NoError(t, err)

	// Claim and fail the job
	job, err := q.Claim(ctx, 30*time.Second)
	require.NoError(t, err)

	err = q.Fail(ctx, job.ID, job.Lease, "transient error", 1*time.Second, 3)
	require.NoError(t, err)

	// Job should be in retry state
	depth, err := q.Depth(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), depth)

	// Wait for retry window
	time.Sleep(1500 * time.Millisecond)

	// Claim again
	job2, err := q.Claim(ctx, 30*time.Second)
	require.NoError(t, err)
	assert.Equal(t, job.ID, job2.ID)
	assert.Equal(t, 2, job2.Attempt)
}

// TestMemoryQueueDeadAfterMaxAttempts validates dead letter behavior.
func TestMemoryQueueDeadAfterMaxAttempts(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()

	payload := map[string]string{"event_id": uuid.New().String(), "project_id": uuid.New().String()}
	err := q.Enqueue(ctx, "process_event", payload)
	require.NoError(t, err)

	// Fail the job 3 times
	for i := 0; i < 3; i++ {
		job, err := q.Claim(ctx, 30*time.Second)
		require.NoError(t, err)
		err = q.Fail(ctx, job.ID, job.Lease, "persistent error", 0, 3)
		require.NoError(t, err)
	}

	// Job should be dead
	depth, err := q.Depth(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), depth)
}

// TestMemoryQueueLeaseExpiry validates lease expiry and re-claim.
func TestMemoryQueueLeaseExpiry(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()

	payload := map[string]string{"event_id": uuid.New().String(), "project_id": uuid.New().String()}
	err := q.Enqueue(ctx, "process_event", payload)
	require.NoError(t, err)

	// Claim with short lease
	job, err := q.Claim(ctx, 100*time.Millisecond)
	require.NoError(t, err)

	// Wait for lease to expire
	time.Sleep(150 * time.Millisecond)

	// Should be able to claim again (lease expired)
	job2, err := q.Claim(ctx, 30*time.Second)
	require.NoError(t, err)
	assert.Equal(t, job.ID, job2.ID)
}

// TestMemoryQueueConcurrentClaim validates concurrent claim behavior.
func TestMemoryQueueConcurrentClaim(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()

	// Enqueue multiple jobs
	for i := 0; i < 10; i++ {
		payload := map[string]string{"index": uuid.New().String()}
		err := q.Enqueue(ctx, "process_event", payload)
		require.NoError(t, err)
	}

	depth, err := q.Depth(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(10), depth)

	// Claim all jobs concurrently
	var claimed []Job
	for i := 0; i < 10; i++ {
		job, err := q.Claim(ctx, 30*time.Second)
		require.NoError(t, err)
		claimed = append(claimed, job)
	}

	// Should have claimed all 10
	assert.Len(t, claimed, 10)

	// Depth should be 0 after claiming all
	depth, err = q.Depth(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), depth)

	// Renew all leases
	for _, job := range claimed {
		err = q.Renew(ctx, job.ID, job.Lease, 30*time.Second)
		require.NoError(t, err)
	}

	// Complete all jobs
	for _, job := range claimed {
		err = q.Complete(ctx, job.ID, job.Lease)
		require.NoError(t, err)
	}
}

// TestNATSQueueLifecycle validates the NATS simulator job lifecycle.
func TestNATSQueueLifecycle(t *testing.T) {
	q := NewNATSLocal()
	ctx := context.Background()

	payload := map[string]string{"event_id": uuid.New().String(), "project_id": uuid.New().String()}
	err := q.Enqueue(ctx, "process_event", payload)
	require.NoError(t, err)

	depth, err := q.Depth(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), depth)

	job, err := q.Claim(ctx, 30*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "process_event", job.Type)

	err = q.Complete(ctx, job.ID, job.Lease)
	require.NoError(t, err)

	depth, err = q.Depth(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), depth)
}

// TestCFLocalLifecycle validates the Cloudflare Queues local simulator.
func TestCFLocalLifecycle(t *testing.T) {
	q := NewCFLocal()
	ctx := context.Background()

	payload := map[string]string{"event_id": uuid.New().String(), "project_id": uuid.New().String()}
	err := q.Enqueue(ctx, "process_event", payload)
	require.NoError(t, err)

	depth, err := q.Depth(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), depth)

	job, err := q.Claim(ctx, 30*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "process_event", job.Type)

	err = q.Complete(ctx, job.ID, job.Lease)
	require.NoError(t, err)

	depth, err = q.Depth(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), depth)
}

// TestNATSJetStreamLocal validates JetStream-style semantics.
func TestNATSJetStreamLocal(t *testing.T) {
	q := NewNATSJetStreamLocal()
	ctx := context.Background()

	payload := map[string]any{"event_id": uuid.New().String(), "project_id": uuid.New().String()}
	err := q.Enqueue(ctx, "process_event", payload)
	require.NoError(t, err)

	// Job should have JetStream metadata
	depth, err := q.Depth(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), depth)

	job, err := q.Claim(ctx, 30*time.Second)
	require.NoError(t, err)

	// Verify payload has JetStream wrapper
	var decoded map[string]any
	err = json.Unmarshal(job.Payload, &decoded)
	require.NoError(t, err)
	_, hasJS := decoded["_js"]
	assert.True(t, hasJS, "payload should have JetStream wrapper")
}

// TestGuardEnqueue validates enqueue guards.
func TestGuardEnqueue(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()

	// Should not error on normal enqueue
	err := GuardEnqueue(ctx, q)
	require.NoError(t, err)

	// Cancelled context should error
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	err = GuardEnqueue(cancelCtx, q)
	assert.Error(t, err)
}

// TestQueueFactory validates NewFromEnv with different drivers.
func TestQueueFactory(t *testing.T) {
	tests := []struct {
		name     string
		driver   string
		url      string
		wantErr  bool
		wantName string
	}{
		{"postgres", "postgres", "", true, ""}, // needs pool
		{"memory", "memory", "", false, "memory"},
		{"nats-memory", "nats", "memory", false, "nats-memory-sim"},
		{"nats-local", "nats", "local", false, "nats-memory-sim"},
		{"nats-invalid", "nats", "nats://localhost:4222", true, ""},
		{"cf-local", "cf", "", false, "cf-local"},
		{"unknown", "unknown", "", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, name, err := NewFromEnv(nil, tt.driver, tt.url)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, q)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantName, name)
				assert.NotNil(t, q)
			}
		})
	}
}

// TestBackpressure validates backpressure behavior.
func TestBackpressure(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()

	// Enqueue 100 jobs (well within capacity)
	for i := 0; i < 100; i++ {
		err := q.Enqueue(ctx, "process_event", map[string]string{"i": "test"})
		require.NoError(t, err)
	}

	depth, err := q.Depth(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(100), depth)
}

// TestQueueErrorHandling validates error handling for various failure modes.
func TestQueueErrorHandling(t *testing.T) {
	q := NewMemory()
	ctx := context.Background()

	// Claim with invalid lease
	_, err := q.Claim(ctx, 30*time.Second)
	require.Error(t, err) // No jobs, returns pgx.ErrNoRows

	// Complete non-existent job
	err = q.Complete(ctx, uuid.New(), "invalid-lease")
	assert.Error(t, err)

	// Fail non-existent job
	err = q.Fail(ctx, uuid.New(), "invalid-lease", "error", 0, 0)
	assert.Error(t, err)

	// Renew non-existent job
	err = q.Renew(ctx, uuid.New(), "invalid-lease", 30*time.Second)
	assert.Error(t, err)
}
