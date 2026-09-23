package anomaly

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

var nopLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestCheckAnomaly_Normal(t *testing.T) {
	mgr := NewManager(nopLogger)

	// Battery at 3300mV - within normal range (2800-4200)
	event, err := mgr.CheckAnomaly(context.Background(), "dev-1", MetricBattery, 3300)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Error("expected no anomaly event for normal battery value")
	}
}

func TestCheckAnomaly_LowBattery(t *testing.T) {
	mgr := NewManager(nopLogger)

	// Battery at 2500mV - below threshold (2800)
	event, err := mgr.CheckAnomaly(context.Background(), "dev-1", MetricBattery, 2500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil {
		t.Fatal("expected anomaly event for low battery")
	}
	if event.Value != 2500 {
		t.Errorf("expected value 2500, got %f", event.Value)
	}
	if event.Threshold.Min != 2800 {
		t.Errorf("expected min threshold 2800, got %f", event.Threshold.Min)
	}
}

func TestCheckAnomaly_HighTemp(t *testing.T) {
	mgr := NewManager(nopLogger)

	// Temperature at 90C - above threshold (85)
	event, err := mgr.CheckAnomaly(context.Background(), "dev-1", MetricTemperature, 90)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil {
		t.Fatal("expected anomaly event for high temperature")
	}
}

func TestCheckAnomaly_DisabledMetric(t *testing.T) {
	mgr := NewManager(nopLogger)

	// Uptime metric is disabled by default
	event, err := mgr.CheckAnomaly(context.Background(), "dev-1", MetricUptime, 999999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Error("expected no anomaly for disabled metric")
	}
}

func TestCheckAnomaly_UnmonitoredMetric(t *testing.T) {
	mgr := NewManager(nopLogger)

	// Unknown metric type
	event, err := mgr.CheckAnomaly(context.Background(), "dev-1", "unknown", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Error("expected no anomaly for unmonitored metric")
	}
}

func TestGetEvents_Empty(t *testing.T) {
	mgr := NewManager(nopLogger)
	events := mgr.GetEvents()
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestGetEvents_WithEvents(t *testing.T) {
	mgr := NewManager(nopLogger)

	// Trigger some anomalies
	mgr.CheckAnomaly(context.Background(), "dev-1", MetricBattery, 2500)
	mgr.CheckAnomaly(context.Background(), "dev-1", MetricBattery, 2600)
	mgr.CheckAnomaly(context.Background(), "dev-2", MetricTemperature, 90)

	events := mgr.GetEvents()
	if len(events) != 3 {
		t.Errorf("expected 3 events, got %d", len(events))
	}
}

func TestSetThreshold(t *testing.T) {
	mgr := NewManager(nopLogger)

	mgr.SetThreshold(Threshold{
		Metric:  MetricBattery,
		Min:     3000,
		Max:     4200,
		Enabled: true,
	})

	thresholds := mgr.GetThresholds()
	tb, ok := thresholds[MetricBattery]
	if !ok {
		t.Fatal("expected battery threshold to exist")
	}
	if tb.Min != 3000 {
		t.Errorf("expected min 3000, got %f", tb.Min)
	}
}

func TestDefaultThresholds(t *testing.T) {
	mgr := NewManager(nopLogger)
	thresholds := mgr.GetThresholds()

	// Verify all default thresholds exist
	expected := []MetricType{
		MetricBattery, MetricTemperature, MetricVoltage, MetricCurrent, MetricFrequency, MetricUptime,
	}

	for _, m := range expected {
		if _, ok := thresholds[m]; !ok {
			t.Errorf("expected default threshold for %s", m)
		}
	}
}
