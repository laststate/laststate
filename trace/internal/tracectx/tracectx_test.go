package tracectx_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/laststate/trace/internal/tracectx"
)

func TestTraceContext(t *testing.T) {
	tid, sid := tracectx.New()
	if len(tid) < 16 || len(sid) < 8 {
		t.Fatal(tid, sid)
	}
	ctx := tracectx.With(context.Background(), tid, sid)
	if tracectx.TraceID(ctx) != tid || tracectx.SpanID(ctx) != sid {
		t.Fatal()
	}
	if tracectx.Start(ctx).IsZero() {
		t.Fatal("start")
	}
	child := tracectx.ChildSpan(ctx)
	if tracectx.TraceID(child) != tid {
		t.Fatal("trace preserved")
	}
	if tracectx.SpanID(child) == sid {
		t.Fatal("new span")
	}
	log := tracectx.Logger(ctx, slog.Default())
	if log == nil {
		t.Fatal()
	}
}
