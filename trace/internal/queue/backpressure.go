package queue

import (
	"context"
	"fmt"
	"sync/atomic"
)

// DepthQuerier is optional; backends may report ready queue depth for backpressure.
type DepthQuerier interface {
	Depth(ctx context.Context) (int64, error)
}

// MaxDepth is soft limit for Enqueue (0 = unlimited). Used by Memory/NATS/Redis wrappers.
var defaultMaxDepth int64 = 50_000

// SetDefaultMaxDepth configures process-wide soft queue limit (0 disables).
func SetDefaultMaxDepth(n int64) { atomic.StoreInt64(&defaultMaxDepth, n) }

func DefaultMaxDepth() int64 { return atomic.LoadInt64(&defaultMaxDepth) }

// ErrBackpressure is returned when Depth >= MaxDepth.
var ErrBackpressure = fmt.Errorf("queue backpressure: depth limit reached")

// GuardEnqueue checks depth before insert when q implements DepthQuerier.
// Returns context error if ctx is cancelled.
func GuardEnqueue(ctx context.Context, q Jober) error {
	// Check context cancellation first
	if err := ctx.Err(); err != nil {
		return err
	}
	max := DefaultMaxDepth()
	if max <= 0 {
		return nil
	}
	dq, ok := q.(DepthQuerier)
	if !ok {
		return nil
	}
	n, err := dq.Depth(ctx)
	if err != nil {
		return nil // don't block if depth unavailable
	}
	if n >= max {
		return ErrBackpressure
	}
	return nil
}
