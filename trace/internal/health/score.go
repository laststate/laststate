// Package health computes per-device health scores (0-100) and fleet-wide aggregates.
//
// Scoring formula:
//
//	score = (crash_weight * crash_factor) + (uptime_weight * uptime_factor) +
//	        (battery_weight * battery_factor) + (ota_weight * ota_factor) +
//	        (temp_weight * temp_factor)
//
// Weights sum to 100:
//
//	crash: 40%, uptime: 25%, battery: 15%, OTA: 10%, temp: 10%
//
// Configuration:
//
//	FLEET_HEALTH_RECALC_INTERVAL=15m  — worker recalculation interval
package health

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"
)

// Score weights (must sum to 1.0)
const (
	WeightCrash   = 0.40
	WeightUptime  = 0.25
	WeightBattery = 0.15
	WeightOTA     = 0.10
	WeightTemp    = 0.10
)

// DeviceHealth holds the computed health score and its components.
type DeviceHealth struct {
	DeviceID      string    `json:"device_id"`
	Score         float64   `json:"score"` // 0-100
	CrashFactor   float64   `json:"crash_factor"`
	UptimeFactor  float64   `json:"uptime_factor"`
	BatteryFactor float64   `json:"battery_factor,omitempty"`
	OTAFactor     float64   `json:"ota_factor,omitempty"`
	TempFactor    float64   `json:"temp_factor,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
	Status        string    `json:"status"` // "healthy", "degraded", "critical"
}

// FleetHealth holds fleet-wide health statistics.
type FleetHealth struct {
	AverageScore  float64        `json:"average_score"`
	MedianScore   float64        `json:"median_score"`
	HealthyCount  int            `json:"healthy_count"`
	DegradedCount int            `json:"degraded_count"`
	CriticalCount int            `json:"critical_count"`
	TopHealthy    []DeviceHealth `json:"top_healthy"`
	BottomDead    []DeviceHealth `json:"bottom_dead"`
	Trend         []TrendPoint   `json:"trend"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

// TrendPoint is a historical health score data point.
type TrendPoint struct {
	Date  string  `json:"date"`
	Score float64 `json:"score"`
}

// ScoreCalculator computes health scores for a device.
type ScoreCalculator struct {
	// CrashStats: recent crash count for the device
	CrashStats func(ctx context.Context, deviceID string, days int) (int64, error)
	// UptimeStats: uptime percentage (0-100)
	UptimeStats func(ctx context.Context, deviceID string, days int) (float64, error)
	// BatteryHealth: battery health percentage (0-100)
	BatteryHealth func(ctx context.Context, deviceID string) (float64, error)
	// OTASuccessRate: OTA update success rate (0-100)
	OTASuccessRate func(ctx context.Context, deviceID string) (float64, error)
	// TempAvg: average temperature deviation from normal (°C)
	TempAvg func(ctx context.Context, deviceID string) (float64, error)
	// NormalTemp: expected normal operating temperature
	NormalTemp float64
}

// CalculateScore computes a device's health score.
func (c *ScoreCalculator) CalculateScore(ctx context.Context, deviceID string, days int) (*DeviceHealth, error) {
	// Gather metrics
	var crashCount int64
	if c.CrashStats != nil {
		var err error
		crashCount, err = c.CrashStats(ctx, deviceID, days)
		if err != nil {
			return nil, fmt.Errorf("crash stats: %w", err)
		}
	}

	var uptime float64 = 100.0
	if c.UptimeStats != nil {
		var err error
		uptime, err = c.UptimeStats(ctx, deviceID, days)
		if err != nil {
			return nil, fmt.Errorf("uptime stats: %w", err)
		}
	}

	battery := 100.0
	if c.BatteryHealth != nil {
		var err error
		battery, err = c.BatteryHealth(ctx, deviceID)
		if err != nil {
			battery = 100.0 // default to full health if unavailable
		}
	}

	ota := 100.0
	if c.OTASuccessRate != nil {
		var err error
		ota, err = c.OTASuccessRate(ctx, deviceID)
		if err != nil {
			ota = 100.0
		}
	}

	tempDeviation := 0.0
	if c.TempAvg != nil {
		var err error
		tempDeviation, err = c.TempAvg(ctx, deviceID)
		if err != nil {
			tempDeviation = 0.0
		}
	}

	// Normalize crash factor: 0 crashes = 1.0 (full health), 10+ crashes = 0.0
	crashFactor := 1.0 - math.Min(float64(crashCount)/10.0, 1.0)

	// Uptime factor: direct from percentage
	uptimeFactor := math.Min(uptime/100.0, 1.0)

	// Battery factor: direct from health percentage
	batteryFactor := math.Min(battery/100.0, 1.0)

	// OTA factor: direct from success rate
	otaFactor := math.Min(ota/100.0, 1.0)

	// Temp factor: penalty for deviation. Normal = 1.0, ±5°C = 0.5, ±10°C = 0.0
	tempDeviation = math.Abs(tempDeviation)
	tempFactor := 1.0 - math.Min(tempDeviation/10.0, 1.0)

	// Weighted score
	score := (WeightCrash*crashFactor +
		WeightUptime*uptimeFactor +
		WeightBattery*batteryFactor +
		WeightOTA*otaFactor +
		WeightTemp*tempFactor) * 100.0

	// Clamp to 0-100
	score = math.Max(0, math.Min(100, score))

	// Determine status
	status := "healthy"
	if score < 30 {
		status = "critical"
	} else if score < 70 {
		status = "degraded"
	}

	return &DeviceHealth{
		DeviceID:      deviceID,
		Score:         math.Round(score*10) / 10,
		CrashFactor:   math.Round(crashFactor*100) / 100,
		UptimeFactor:  math.Round(uptimeFactor*100) / 100,
		BatteryFactor: math.Round(batteryFactor*100) / 100,
		OTAFactor:     math.Round(otaFactor*100) / 100,
		TempFactor:    math.Round(tempFactor*100) / 100,
		UpdatedAt:     time.Now().UTC(),
		Status:        status,
	}, nil
}

// FleetHealthCalculator computes fleet-wide health statistics.
type FleetHealthCalculator struct {
	Calculator  *ScoreCalculator
	ListDevices func(ctx context.Context) ([]string, error)
	SaveScore   func(ctx context.Context, health DeviceHealth) error
	Log         *slog.Logger
}

// CalculateFleetHealth computes health for all devices and returns fleet stats.
func (c *FleetHealthCalculator) CalculateFleetHealth(ctx context.Context, days int, topN, bottomN int) (*FleetHealth, error) {
	devices, err := c.ListDevices(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing devices: %w", err)
	}

	healthMap := make(map[string]*DeviceHealth)
	var scores []float64

	for _, devID := range devices {
		h, err := c.Calculator.CalculateScore(ctx, devID, days)
		if err != nil {
			c.Log.Warn("failed to calculate health", "device", devID, "error", err)
			continue
		}
		if err := c.SaveScore(ctx, *h); err != nil {
			c.Log.Warn("failed to save score", "device", devID, "error", err)
		}
		healthMap[devID] = h
		scores = append(scores, h.Score)
	}

	// Compute fleet aggregates
	fleet := &FleetHealth{
		UpdatedAt: time.Now().UTC(),
	}

	if len(scores) > 0 {
		sum := 0.0
		for _, s := range scores {
			sum += s
		}
		fleet.AverageScore = math.Round(sum/float64(len(scores))*10) / 10

		// Sort for median
		sorted := make([]float64, len(scores))
		copy(sorted, scores)
		sortFloats(sorted)
		if len(sorted)%2 == 0 {
			fleet.MedianScore = (sorted[len(sorted)/2-1] + sorted[len(sorted)/2]) / 2
		} else {
			fleet.MedianScore = sorted[len(sorted)/2]
		}

		// Count by status
		for _, h := range healthMap {
			switch h.Status {
			case "healthy":
				fleet.HealthyCount++
			case "degraded":
				fleet.DegradedCount++
			case "critical":
				fleet.CriticalCount++
			}
		}
	}

	// Top N healthy
	healthy := make([]DeviceHealth, 0)
	for _, h := range healthMap {
		if h.Status == "healthy" {
			healthy = append(healthy, *h)
		}
	}
	sortByScoreDescSlice(healthy)
	if len(healthy) > topN {
		healthy = healthy[:topN]
	}
	fleet.TopHealthy = healthy

	// Bottom N dead
	bottom := make([]DeviceHealth, 0)
	for _, h := range healthMap {
		if h.Status == "critical" || h.Status == "degraded" {
			bottom = append(bottom, *h)
		}
	}
	sortByScoreAscSlice(bottom)
	if len(bottom) > bottomN {
		bottom = bottom[:bottomN]
	}
	fleet.BottomDead = bottom

	return fleet, nil
}

// sortFloats sorts a float64 slice in place.
func sortFloats(s []float64) {
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j] > key {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
}

// sortByScoreDesc sorts DeviceHealth slice by Score descending.
func sortByScoreDesc(s []*DeviceHealth) {
	sort.Slice(s, func(i, j int) bool {
		return s[i].Score > s[j].Score
	})
}

// sortByScoreAsc sorts DeviceHealth slice by Score ascending.
func sortByScoreAsc(s []*DeviceHealth) {
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j].Score > key.Score {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
}

// sortByScoreAscSlice sorts []DeviceHealth slice by Score ascending.
func sortByScoreAscSlice(s []DeviceHealth) {
	sort.Slice(s, func(i, j int) bool {
		return s[i].Score < s[j].Score
	})
}

// sortByScoreDescSlice sorts []DeviceHealth slice by Score descending.
func sortByScoreDescSlice(s []DeviceHealth) {
	sort.Slice(s, func(i, j int) bool {
		return s[i].Score > s[j].Score
	})
}

// Worker runs periodic health score recalculation.
type Worker struct {
	Calculator *FleetHealthCalculator
	Interval   time.Duration
	Cancel     context.CancelFunc
	Log        *slog.Logger
	mu         sync.Mutex
	running    bool
}

// NewWorker creates a health score worker.
func NewWorker(calc *FleetHealthCalculator, interval time.Duration, log *slog.Logger) *Worker {
	return &Worker{
		Calculator: calc,
		Interval:   interval,
		Log:        log,
	}
}

// Start begins periodic recalculation.
func (w *Worker) Start(ctx context.Context) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()

	go func() {
		defer func() {
			w.mu.Lock()
			w.running = false
			w.mu.Unlock()
		}()

		ticker := time.NewTicker(w.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.runRecalc(ctx)
			}
		}
	}()
}

func (w *Worker) runRecalc(ctx context.Context) {
	fleet, err := w.Calculator.CalculateFleetHealth(ctx, 7, 10, 10)
	if err != nil {
		w.Log.Error("fleet health recalc failed", "error", err)
		return
	}
	w.Log.Info("fleet health recalculated",
		"average_score", fleet.AverageScore,
		"healthy", fleet.HealthyCount,
		"degraded", fleet.DegradedCount,
		"critical", fleet.CriticalCount,
	)
}

// Stop halts the worker.
func (w *Worker) Stop() {
	if w.Cancel != nil {
		w.Cancel()
	}
}
