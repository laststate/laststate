// SPDX-License-Identifier: Apache-2.0
package observability

import (
	"context"
	"testing"
)

func TestSetupDisabled(t *testing.T) {
	cfg := Config{
		Enabled:        false,
		ServiceName:    "test",
		ServiceVersion: "0.0.0",
	}
	cleanup, err := Setup(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cleanup == nil {
		t.Fatal("expected cleanup function")
	}
	cleanup()
}

func TestSetupEnabled(t *testing.T) {
	cfg := Config{
		Enabled:        true,
		ServiceName:    "test",
		ServiceVersion: "0.0.0",
		TraceEndpoint:  "http://localhost:4318",
		MetricEndpoint: "http://localhost:4318",
	}
	cleanup, err := Setup(context.Background(), cfg)
	if err != nil {
		// This might fail if OTLP collector is not available, that's ok.
		t.Skipf("OTLP setup failed (expected in test env): %v", err)
	}
	if cleanup == nil {
		t.Fatal("expected cleanup function")
	}
	cleanup()
}

func TestStartSpan(t *testing.T) {
	ctx := context.Background()
	spanCtx, span := StartSpan(ctx, "test-span")
	if spanCtx == nil {
		t.Fatal("expected span context")
	}
	if span == nil {
		t.Fatal("expected span")
	}
	span.End()
}
