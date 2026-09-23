package health

import (
	"context"
	"testing"
)

func TestCalculateScore_FullHealth(t *testing.T) {
	calc := &ScoreCalculator{
		CrashStats:     func(ctx context.Context, id string, days int) (int64, error) { return 0, nil },
		UptimeStats:    func(ctx context.Context, id string, days int) (float64, error) { return 100, nil },
		BatteryHealth:  func(ctx context.Context, id string) (float64, error) { return 100, nil },
		OTASuccessRate: func(ctx context.Context, id string) (float64, error) { return 100, nil },
		TempAvg:        func(ctx context.Context, id string) (float64, error) { return 0, nil },
		NormalTemp:     25.0,
	}

	h, err := calc.CalculateScore(context.Background(), "dev-1", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if h.Score != 100 {
		t.Errorf("expected score 100, got %f", h.Score)
	}
	if h.Status != "healthy" {
		t.Errorf("expected status healthy, got %s", h.Status)
	}
}

func TestCalculateScore_Critical(t *testing.T) {
	calc := &ScoreCalculator{
		CrashStats:     func(ctx context.Context, id string, days int) (int64, error) { return 15, nil },
		UptimeStats:    func(ctx context.Context, id string, days int) (float64, error) { return 50, nil },
		BatteryHealth:  func(ctx context.Context, id string) (float64, error) { return 30, nil },
		OTASuccessRate: func(ctx context.Context, id string) (float64, error) { return 40, nil },
		TempAvg:        func(ctx context.Context, id string) (float64, error) { return 15, nil },
		NormalTemp:     25.0,
	}

	h, err := calc.CalculateScore(context.Background(), "dev-2", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if h.Score >= 70 {
		t.Errorf("expected critical score (< 70), got %f", h.Score)
	}
	if h.Status != "critical" && h.Status != "degraded" {
		t.Errorf("expected critical or degraded status, got %s", h.Status)
	}
}

func TestCalculateScore_NilFuncs(t *testing.T) {
	// All funcs nil - should default to 100
	calc := &ScoreCalculator{
		NormalTemp: 25.0,
	}

	h, err := calc.CalculateScore(context.Background(), "dev-3", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if h.Score != 100 {
		t.Errorf("expected score 100, got %f", h.Score)
	}
}

func TestCalculateScore_CrashFactor(t *testing.T) {
	calc := &ScoreCalculator{
		CrashStats: func(ctx context.Context, id string, days int) (int64, error) {
			switch id {
			case "zero":
				return 0, nil
			case "five":
				return 5, nil
			case "ten":
				return 10, nil
			case "twenty":
				return 20, nil
			default:
				return 0, nil
			}
		},
		UptimeStats:    func(ctx context.Context, id string, days int) (float64, error) { return 100, nil },
		BatteryHealth:  func(ctx context.Context, id string) (float64, error) { return 100, nil },
		OTASuccessRate: func(ctx context.Context, id string) (float64, error) { return 100, nil },
		TempAvg:        func(ctx context.Context, id string) (float64, error) { return 0, nil },
		NormalTemp:     25.0,
	}

	tests := []struct {
		id       string
		expected float64
	}{
		{"zero", 100},
		{"five", 80},   // crash factor = 0.5, so 0.4*0.5 + 0.6 = 0.8 -> 80
		{"ten", 60},    // crash factor = 0.0, so 0.4*0.0 + 0.6 = 0.6 -> 60
		{"twenty", 60}, // crash factor clamped to 0.0
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			h, err := calc.CalculateScore(context.Background(), tt.id, 7)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if h.Score != tt.expected {
				t.Errorf("expected score %f, got %f", tt.expected, h.Score)
			}
		})
	}
}

func TestFleetHealthCalculator(t *testing.T) {
	calc := &ScoreCalculator{
		CrashStats:     func(ctx context.Context, id string, days int) (int64, error) { return 0, nil },
		UptimeStats:    func(ctx context.Context, id string, days int) (float64, error) { return 100, nil },
		BatteryHealth:  func(ctx context.Context, id string) (float64, error) { return 100, nil },
		OTASuccessRate: func(ctx context.Context, id string) (float64, error) { return 100, nil },
		TempAvg:        func(ctx context.Context, id string) (float64, error) { return 0, nil },
		NormalTemp:     25.0,
	}

	fhc := &FleetHealthCalculator{
		Calculator: calc,
		ListDevices: func(ctx context.Context) ([]string, error) {
			return []string{"dev-1", "dev-2", "dev-3"}, nil
		},
		SaveScore: func(ctx context.Context, h DeviceHealth) error { return nil },
	}

	fleet, err := fhc.CalculateFleetHealth(context.Background(), 7, 2, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fleet.AverageScore != 100 {
		t.Errorf("expected average score 100, got %f", fleet.AverageScore)
	}
	if fleet.HealthyCount != 3 {
		t.Errorf("expected 3 healthy devices, got %d", fleet.HealthyCount)
	}
	if fleet.DegradedCount != 0 {
		t.Errorf("expected 0 degraded devices, got %d", fleet.DegradedCount)
	}
	if fleet.CriticalCount != 0 {
		t.Errorf("expected 0 critical devices, got %d", fleet.CriticalCount)
	}
}
