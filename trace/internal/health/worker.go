package health

import (
	"context"
	"encoding/json"
	"net/http"
)

// HealthEndpoints registers HTTP handlers for the fleet health API.
type HealthEndpoints struct {
	Calculator *FleetHealthCalculator
}

// HandleFleetHealth handles GET /api/fleet/health
func (e *HealthEndpoints) HandleFleetHealth(w http.ResponseWriter, r *http.Request) {
	fleet, err := e.Calculator.CalculateFleetHealth(r.Context(), 7, 10, 10)
	if err != nil {
		http.Error(w, `{"error":{"code":"internal","message":"health calculation failed"}}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(fleet); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleDeviceHealth handles GET /api/fleet/health/devices/{id}
func (e *HealthEndpoints) HandleDeviceHealth(w http.ResponseWriter, r *http.Request, deviceID string) {
	calculator := &ScoreCalculator{
		CrashStats:     func(ctx context.Context, id string, days int) (int64, error) { return 0, nil },
		UptimeStats:    func(ctx context.Context, id string, days int) (float64, error) { return 100, nil },
		BatteryHealth:  func(ctx context.Context, id string) (float64, error) { return 100, nil },
		OTASuccessRate: func(ctx context.Context, id string) (float64, error) { return 100, nil },
		TempAvg:        func(ctx context.Context, id string) (float64, error) { return 0, nil },
		NormalTemp:     25.0,
	}

	health, err := calculator.CalculateScore(r.Context(), deviceID, 7)
	if err != nil {
		http.Error(w, `{"error":{"code":"internal"}}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}
