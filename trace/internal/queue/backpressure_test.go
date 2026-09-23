package queue

import (
	"context"
	"testing"
)

func TestMemoryBackpressure(t *testing.T) {
	SetDefaultMaxDepth(3)
	defer SetDefaultMaxDepth(50_000)
	q := NewMemory()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := q.Enqueue(ctx, "t", map[string]int{"i": i}); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.Enqueue(ctx, "t", map[string]int{"i": 99}); err != ErrBackpressure {
		t.Fatalf("want backpressure, got %v", err)
	}
	n, err := q.Depth(ctx)
	if err != nil || n != 3 {
		t.Fatal(n, err)
	}
}
