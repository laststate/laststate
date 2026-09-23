package billing

import (
	"os"
	"testing"
)

type mockTaxEngine struct {
	rate float64
}

func (m *mockTaxEngine) CalculateTax(amountCents int, jurisdiction string) (int64, float64, error) {
	return int64(float64(amountCents) * m.rate), m.rate, nil
}

func TestGenerateTaxDefaultJurisdictions(t *testing.T) {
	tests := []struct {
		name         string
		amount       int
		rate         float64
		jurisdiction string
		wantRate     float64
		wantTax      int64
	}{
		{
			name:         "Texas US-TX",
			amount:       10000,
			rate:         0.0,
			jurisdiction: "US-TX",
			wantRate:     0.0825,
			wantTax:      825,
		},
		{
			name:         "California US-CA",
			amount:       10000,
			rate:         0.0,
			jurisdiction: "US-CA",
			wantRate:     0.0725,
			wantTax:      725,
		},
		{
			name:         "New York US-NY",
			amount:       10000,
			rate:         0.0,
			jurisdiction: "US-NY",
			wantRate:     0.08875,
			wantTax:      888,
		},
		{
			name:         "Sao Paulo BR-SP",
			amount:       10000,
			rate:         0.0,
			jurisdiction: "BR-SP",
			wantRate:     0.18,
			wantTax:      1800,
		},
		{
			name:         "Rio de Janeiro BR-RJ",
			amount:       10000,
			rate:         0.0,
			jurisdiction: "BR-RJ",
			wantRate:     0.17,
			wantTax:      1700,
		},
		{
			name:         "Germany EU-DE",
			amount:       10000,
			rate:         0.0,
			jurisdiction: "EU-DE",
			wantRate:     0.19,
			wantTax:      1900,
		},
		{
			name:         "Custom Rate without jurisdiction",
			amount:       10000,
			rate:         0.05,
			jurisdiction: "",
			wantRate:     0.05,
			wantTax:      500,
		},
		{
			name:         "Zero amount",
			amount:       0,
			rate:         0.0,
			jurisdiction: "US-TX",
			wantRate:     0.0825,
			wantTax:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tax, rate := GenerateTax(tt.amount, tt.rate, tt.jurisdiction)
			if rate != tt.wantRate {
				t.Errorf("rate = %f, want %f", rate, tt.wantRate)
			}
			if tax != tt.wantTax {
				t.Errorf("tax = %d, want %d", tax, tt.wantTax)
			}
		})
	}
}

func TestGenerateTaxWithCustomEngine(t *testing.T) {
	SetTaxEngine(&mockTaxEngine{rate: 0.12})
	defer SetTaxEngine(nil)

	tax, rate := GenerateTax(10000, 0.0, "CUSTOM")
	if rate != 0.12 {
		t.Errorf("expected rate 0.12, got %f", rate)
	}
	if tax != 1200 {
		t.Errorf("expected tax 1200, got %d", tax)
	}
}

func TestGenerateTaxWithEnvEngine(t *testing.T) {
	os.Setenv("STRIPE_TAX_ENABLED", "true")
	defer os.Unsetenv("STRIPE_TAX_ENABLED")

	tax, rate := GenerateTax(10000, 0.0, "UNKNOWN-REGION")
	if rate != 0.085 {
		t.Errorf("expected fallback engine rate 0.085, got %f", rate)
	}
	if tax != 850 {
		t.Errorf("expected tax 850, got %d", tax)
	}
}
