// Package anomaly implements server-side backup anomaly detection.
// The primary anomaly detection runs on-device (Latch) via TinyML.
// This package provides a server-side fallback that can detect anomalies
// from aggregated metric data without requiring on-device inference.
//
// Detection method: simple threshold-based with configurable per-metric limits.
// When an anomaly is detected, a special breadcrumb is added and the LEP
// envelope gets an ANOMALY flag set.
//
// Configuration:
//
//	LS_ANOMALY_THRESHOLD_battery_mv=3100
//	LS_ANOMALY_THRESHOLD_temperature_c=85
//	LS_ENABLE_ANOMALY=ON (in Latch config.h)
package anomaly

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// MetricType represents a monitored metric.
type MetricType string

const (
	MetricBattery     MetricType = "battery"
	MetricTemperature MetricType = "temperature"
	MetricVoltage     MetricType = "voltage"
	MetricCurrent     MetricType = "current"
	MetricFrequency   MetricType = "frequency"
	MetricUptime      MetricType = "uptime"
)

// Threshold defines the bounds for anomaly detection on a metric.
type Threshold struct {
	Metric    MetricType `json:"metric"`
	Min       float64    `json:"min"`
	Max       float64    `json:"max"`
	Enabled   bool       `json:"enabled"`
	AlertOnce bool       `json:"alert_once"` // if true, only alert first time
}

// AnomalyEvent represents a detected anomaly.
type AnomalyEvent struct {
	ID        string     `json:"id"`
	Metric    MetricType `json:"metric"`
	Value     float64    `json:"value"`
	Threshold MinMax     `json:"threshold"`
	DeviceID  string     `json:"device_id"`
	Timestamp time.Time  `json:"timestamp"`
}

// MinMax represents the min/max bounds for a threshold.
type MinMax struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// Manager handles anomaly detection.
type Manager struct {
	thresholds map[MetricType]Threshold
	events     []AnomalyEvent
	mu         sync.RWMutex
	log        *slog.Logger
	maxEvents  int
}

// NewManager creates an anomaly detection manager.
func NewManager(log *slog.Logger) *Manager {
	defaultThresholds := map[MetricType]Threshold{
		MetricBattery:     {Metric: MetricBattery, Min: 2800, Max: 4200, Enabled: true},
		MetricTemperature: {Metric: MetricTemperature, Min: -20, Max: 85, Enabled: true},
		MetricVoltage:     {Metric: MetricVoltage, Min: 1.8, Max: 3.6, Enabled: true},
		MetricCurrent:     {Metric: MetricCurrent, Min: 0, Max: 500, Enabled: true},
		MetricFrequency:   {Metric: MetricFrequency, Min: 0, Max: 200, Enabled: true},
		MetricUptime:      {Metric: MetricUptime, Min: 0, Max: 99999, Enabled: false},
	}

	return &Manager{
		thresholds: defaultThresholds,
		events:     make([]AnomalyEvent, 0, 100),
		maxEvents:  100,
		log:        log,
	}
}

// CheckAnomaly checks if a metric value is anomalous and returns an event if so.
func (m *Manager) CheckAnomaly(ctx context.Context, deviceID string, metric MetricType, value float64) (*AnomalyEvent, error) {
	m.mu.RLock()
	threshold, ok := m.thresholds[metric]
	m.mu.RUnlock()

	if !ok || !threshold.Enabled {
		return nil, nil // not monitored
	}

	if value < threshold.Min || value > threshold.Max {
		event := AnomalyEvent{
			ID:        fmt.Sprintf("ANOM-%s-%s", deviceID, time.Now().Format("20060102150405")),
			Metric:    metric,
			Value:     value,
			Threshold: MinMax{Min: threshold.Min, Max: threshold.Max},
			DeviceID:  deviceID,
			Timestamp: time.Now().UTC(),
		}

		m.mu.Lock()
		m.events = append(m.events, event)
		if len(m.events) > m.maxEvents {
			m.events = m.events[len(m.events)-m.maxEvents:]
		}
		m.mu.Unlock()

		m.log.Warn("anomaly detected",
			"device", deviceID,
			"metric", metric,
			"value", value,
			"min", threshold.Min,
			"max", threshold.Max,
		)

		return &event, nil
	}

	return nil, nil
}

// GetEvents returns recent anomaly events.
func (m *Manager) GetEvents() []AnomalyEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]AnomalyEvent, len(m.events))
	copy(result, m.events)
	return result
}

// SetThreshold updates a metric threshold.
func (m *Manager) SetThreshold(threshold Threshold) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.thresholds[threshold.Metric] = threshold
}

// GetThresholds returns all configured thresholds.
func (m *Manager) GetThresholds() map[MetricType]Threshold {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[MetricType]Threshold)
	for k, v := range m.thresholds {
		result[k] = v
	}
	return result
}

// HandleThresholds handles GET/POST /api/anomaly/thresholds
func (m *Manager) HandleThresholds(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(m.GetThresholds())
	case http.MethodPost:
		var threshold Threshold
		if err := json.NewDecoder(r.Body).Decode(&threshold); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		m.SetThreshold(threshold)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(threshold)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleEvents handles GET /api/anomaly/events
func (m *Manager) HandleEvents(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"events": m.GetEvents(),
	})
}
