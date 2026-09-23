package billing

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func resetOverageDefaults(t *testing.T) {
	t.Helper()
	SetOverageConfig(DefaultOverageConfig())
}

func TestCalculateUsageChargesWithinAllowance(t *testing.T) {
	resetOverageDefaults(t)
	svc := newTestService()
	org := uuid.New()
	ctx := context.Background()

	// Record usage below the team allowance (15M events/month).
	if err := svc.RecordUsage(ctx, org, MetricEvents, 1000, time.Now().Add(-time.Hour), time.Now()); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}

	summary, err := svc.CalculateUsageCharges(ctx, org, "team", MetricEvents, time.Now().Add(-24*time.Hour), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("CalculateUsageCharges: %v", err)
	}
	if !summary.Metered {
		t.Fatal("team should be metered")
	}
	if summary.OverageUnits != 0 || summary.OverageCents != 0 {
		t.Fatalf("usage within allowance should not produce overage, got %+v", summary)
	}
}

func TestCalculateUsageChargesOverageBilledInBlocks(t *testing.T) {
	resetOverageDefaults(t)
	svc := newTestService()
	org := uuid.New()
	ctx := context.Background()

	// Hobbyist: 1.5M included. Record 1.5M + 2500 extra -> 3 blocks of 1000.
	total := int64(1500000 + 2500)
	if err := svc.RecordUsage(ctx, org, MetricEvents, total, time.Now().Add(-time.Hour), time.Now()); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}

	summary, err := svc.CalculateUsageCharges(ctx, org, "hobbyist", MetricEvents, time.Now().Add(-24*time.Hour), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("CalculateUsageCharges: %v", err)
	}
	if summary.OverageUnits != 2500 {
		t.Fatalf("overage units = %d, want 2500", summary.OverageUnits)
	}
	if summary.BillableBlocks != 3 {
		t.Fatalf("billable blocks = %d, want 3", summary.BillableBlocks)
	}
	// hobbyist overage: $0.40 per 1000 events
	if summary.OverageCents != 120 {
		t.Fatalf("overage cents = %d, want 120", summary.OverageCents)
	}
}

func TestCalculateUsageChargesUnmeteredPlan(t *testing.T) {
	resetOverageDefaults(t)
	svc := newTestService()
	org := uuid.New()
	ctx := context.Background()

	if err := svc.RecordUsage(ctx, org, MetricEvents, 99_000_000, time.Now().Add(-time.Hour), time.Now()); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}

	summary, err := svc.CalculateUsageCharges(ctx, org, "enterprise", MetricEvents, time.Now().Add(-24*time.Hour), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("CalculateUsageCharges: %v", err)
	}
	if summary.Metered {
		t.Fatal("enterprise should not be metered")
	}
	if summary.OverageCents != 0 {
		t.Fatalf("unmetered plan produced overage: %+v", summary)
	}
}

func TestOverageConfigValidation(t *testing.T) {
	if _, err := ParseOverageConfig([]byte(`{"team":{"events":{"included":100,"unit_size":0,"price_per_unit_cents":10}}}`)); err == nil {
		t.Fatal("unit_size 0 should be rejected")
	}
	if _, err := ParseOverageConfig([]byte(`{"team":{"events":{"included":100,"unit_size":10,"price_per_unit_cents":-1}}}`)); err == nil {
		t.Fatal("negative price should be rejected")
	}
	cfg, err := ParseOverageConfig([]byte(`{"team":{"events":{"included":100,"unit_size":10,"price_per_unit_cents":5}}}`))
	if err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if cfg["team"]["events"].Included != 100 {
		t.Fatalf("parsed included = %d, want 100", cfg["team"]["events"].Included)
	}
}

func TestMeteredPlansSorted(t *testing.T) {
	resetOverageDefaults(t)
	plans := MeteredPlans()
	if len(plans) != 3 {
		t.Fatalf("metered plans = %v, want 3 entries", plans)
	}
	for i := 1; i < len(plans); i++ {
		if plans[i-1] > plans[i] {
			t.Fatalf("MeteredPlans not sorted: %v", plans)
		}
	}
}
