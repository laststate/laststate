// Package observability provides OpenTelemetry instrumentation for Relay.
package observability

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// Config holds OpenTelemetry configuration.
type Config struct {
	Enabled        bool
	TraceEndpoint  string // e.g., "https://otel-collector:4318"
	MetricEndpoint string // e.g., "https://otel-collector:4318"
	ServiceName    string
	ServiceVersion string
}

// Setup configures OpenTelemetry tracing and metrics.
// Returns cleanup function to call on shutdown.
func Setup(ctx context.Context, cfg Config) (func(), error) {
	if !cfg.Enabled {
		return func() {}, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String(cfg.ServiceVersion),
			attribute.String("relay.instance", os.Getenv("RELAY_INSTANCE_ID")),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	// Trace exporter
	var traceOpts []otlptracehttp.Option
	if cfg.TraceEndpoint != "" {
		traceOpts = append(traceOpts, otlptracehttp.WithEndpoint(cfg.TraceEndpoint))
		if os.Getenv("OTEL_INSECURE") == "true" {
			traceOpts = append(traceOpts, otlptracehttp.WithInsecure())
		}
	}

	traceExp, err := otlptracehttp.New(ctx, traceOpts...)
	if err != nil {
		return nil, fmt.Errorf("create trace exporter: %w", err)
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tracerProvider)

	// Metric exporter
	var metricOpts []otlpmetrichttp.Option
	if cfg.MetricEndpoint != "" {
		metricOpts = append(metricOpts, otlpmetrichttp.WithEndpoint(cfg.MetricEndpoint))
		if os.Getenv("OTEL_INSECURE") == "true" {
			metricOpts = append(metricOpts, otlpmetrichttp.WithInsecure())
		}
	}

	metricExp, err := otlpmetrichttp.New(ctx, metricOpts...)
	if err != nil {
		return nil, fmt.Errorf("create metric exporter: %w", err)
	}

	meterProvider := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(metric.NewPeriodicReader(metricExp,
			metric.WithInterval(10*time.Second))),
	)
	otel.SetMeterProvider(meterProvider)

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tracerProvider.Shutdown(ctx)
		_ = meterProvider.Shutdown(ctx)
	}

	return cleanup, nil
}

// StartSpan creates a new span with common Relay attributes.
func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, oteltrace.Span) {
	spanCtx, span := otel.Tracer("relay").Start(ctx, name)
	for _, attr := range attrs {
		span.SetAttributes(attr)
	}
	return spanCtx, span
}
