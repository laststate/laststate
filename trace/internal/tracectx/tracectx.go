// Package tracectx provides lightweight request tracing without external OTel deps.
package tracectx

import (
	"context"
	"log/slog"
	"time"

	"github.com/laststate/trace/internal/auth"
)

type ctxKey int

const (
	keyTraceID ctxKey = 1
	keySpanID  ctxKey = 2
	keyStart   ctxKey = 3
)

// New creates a trace + span id pair.
func New() (traceID, spanID string) {
	return auth.RandomHex(16), auth.RandomHex(8)
}

func With(ctx context.Context, traceID, spanID string) context.Context {
	ctx = context.WithValue(ctx, keyTraceID, traceID)
	ctx = context.WithValue(ctx, keySpanID, spanID)
	ctx = context.WithValue(ctx, keyStart, time.Now())
	return ctx
}

func TraceID(ctx context.Context) string {
	if v, ok := ctx.Value(keyTraceID).(string); ok {
		return v
	}
	return ""
}

func SpanID(ctx context.Context) string {
	if v, ok := ctx.Value(keySpanID).(string); ok {
		return v
	}
	return ""
}

func Start(ctx context.Context) time.Time {
	if v, ok := ctx.Value(keyStart).(time.Time); ok {
		return v
	}
	return time.Time{}
}

// Logger returns a slog.Logger with trace fields.
func Logger(ctx context.Context, base *slog.Logger) *slog.Logger {
	if base == nil {
		base = slog.Default()
	}
	tid, sid := TraceID(ctx), SpanID(ctx)
	if tid == "" {
		return base
	}
	return base.With("trace_id", tid, "span_id", sid)
}

// ChildSpan returns ctx with a new span id under the same trace.
func ChildSpan(ctx context.Context) context.Context {
	tid := TraceID(ctx)
	if tid == "" {
		tid, _ = New()
	}
	return With(ctx, tid, auth.RandomHex(8))
}
