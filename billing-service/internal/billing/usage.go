// Package billing — usage-based (metered) charges.
//
// Plans include a monthly allowance of billable units (events by default).
// When an organization exceeds the allowance, the excess is billed in fixed
// unit blocks ("overage"). Included allowances and overage unit pricing are
// derived from the tier quotas (events/day x 30) with sane defaults and can
// be overridden per plan via configuration:
//
//	BILLING_OVERAGE_JSON={"plans":{"team":{"events":{"included":20000000,"unit_size":1000,"price_per_unit_cents":25}}}}
//
// Enterprise is unmetered (-1 quota) and never generates overage charges.
package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/laststate/billing-service/internal/store"
)

// OverageRule describes how excess usage of one metric is billed for a plan.
type OverageRule struct {
	// Included is the number of units included per billing period.
	Included int64 `json:"included"`
	// UnitSize is the number of units in one billable block (e.g. 1000 events).
	UnitSize int64 `json:"unit_size"`
	// PricePerUnitCents is the price of one block, in cents.
	PricePerUnitCents int64 `json:"price_per_unit_cents"`
}

// OverageConfig maps plan -> metric -> rule.
type OverageConfig map[string]map[string]OverageRule

// MetricEvents is the default metered metric.
const MetricEvents = "events"

// eventsPerMonth converts a daily quota into a monthly allowance.
// -1 (and 0) mean "no metering": nothing is ever billed.
func eventsPerMonth(perDay int64) int64 {
	if perDay <= 0 {
		return 0
	}
	return perDay * 30
}

// DefaultOverageConfig mirrors the seeded tier quotas (max_events_per_day)
// and prices overage blocks consistently across paid tiers. Free is metered
// but has no card on file, so overage there is informational only.
func DefaultOverageConfig() OverageConfig {
	return OverageConfig{
		"free": {MetricEvents: {
			Included: eventsPerMonth(1000), UnitSize: 1000, PricePerUnitCents: 50,
		}},
		"hobbyist": {MetricEvents: {
			Included: eventsPerMonth(50000), UnitSize: 1000, PricePerUnitCents: 40,
		}},
		"team": {MetricEvents: {
			Included: eventsPerMonth(500000), UnitSize: 1000, PricePerUnitCents: 25,
		}},
		// enterprise: unmetered — absent from the map.
	}
}

var (
	overageMu     sync.RWMutex
	overageConfig = DefaultOverageConfig()
)

// SetOverageConfig replaces the overage configuration (used by tests and by
// LoadOverageConfigFromEnv).
func SetOverageConfig(cfg OverageConfig) {
	overageMu.Lock()
	defer overageMu.Unlock()
	overageConfig = cfg
}

// ParseOverageConfig parses an OverageConfig JSON document, validating that
// every rule has a positive unit size, a non-negative price, and a
// non-negative included allowance.
func ParseOverageConfig(data []byte) (OverageConfig, error) {
	var cfg OverageConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("overage config: %w", err)
	}
	for plan, metrics := range cfg {
		for metric, rule := range metrics {
			if rule.UnitSize <= 0 {
				return nil, fmt.Errorf("overage config: %s/%s: unit_size must be > 0", plan, metric)
			}
			if rule.PricePerUnitCents < 0 || rule.Included < 0 {
				return nil, fmt.Errorf("overage config: %s/%s: negative price or allowance", plan, metric)
			}
		}
	}
	return cfg, nil
}

// LoadOverageConfigFromEnv loads BILLING_OVERAGE_JSON (inline document).
func LoadOverageConfigFromEnv() error {
	raw := os.Getenv("BILLING_OVERAGE_JSON")
	if raw == "" {
		return nil
	}
	cfg, err := ParseOverageConfig([]byte(raw))
	if err != nil {
		return err
	}
	SetOverageConfig(cfg)
	return nil
}

func overageRule(planID, metric string) (OverageRule, bool) {
	overageMu.RLock()
	defer overageMu.RUnlock()
	metrics, ok := overageConfig[planID]
	if !ok {
		return OverageRule{}, false
	}
	rule, ok := metrics[metric]
	return rule, ok
}

// UsageChargeSummary is the outcome of metering one metric for one period.
type UsageChargeSummary struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	PlanID         string    `json:"plan_id"`
	Metric         string    `json:"metric"`
	PeriodStart    time.Time `json:"period_start"`
	PeriodEnd      time.Time `json:"period_end"`
	// TotalUnits is the usage recorded in the period.
	TotalUnits int64 `json:"total_units"`
	// IncludedUnits is the allowance for the period.
	IncludedUnits int64 `json:"included_units"`
	// OverageUnits is TotalUnits - IncludedUnits when positive.
	OverageUnits int64 `json:"overage_units"`
	// BillableBlocks is the number of whole unit blocks billed.
	BillableBlocks int64 `json:"billable_blocks"`
	// UnitSize is the size of one billable block.
	UnitSize int64 `json:"unit_size"`
	// OverageCents is the overage amount for the period, in cents.
	OverageCents int64 `json:"overage_cents"`
	// Metered is false when the plan is unmetered (e.g. enterprise).
	Metered bool `json:"metered"`
}

// CalculateUsageCharges meters one metric for one organization over a period
// and returns the overage summary. Free-of-charge results (Metered=true,
// OverageCents=0) are returned without error so callers can surface them.
func (s *Service) CalculateUsageCharges(ctx context.Context, orgID uuid.UUID, planID, metric string, periodStart, periodEnd time.Time) (*UsageChargeSummary, error) {
	summary := &UsageChargeSummary{
		OrganizationID: orgID,
		PlanID:         planID,
		Metric:         metric,
		PeriodStart:    periodStart,
		PeriodEnd:      periodEnd,
	}

	rule, metered := overageRule(planID, metric)
	summary.Metered = metered
	if !metered {
		return summary, nil
	}
	summary.IncludedUnits = rule.Included
	summary.UnitSize = rule.UnitSize

	total, err := s.GetUsage(ctx, orgID, metric, periodStart, periodEnd)
	if err != nil {
		return nil, fmt.Errorf("usage query: %w", err)
	}
	summary.TotalUnits = total

	if total > rule.Included {
		summary.OverageUnits = total - rule.Included
		// Whole blocks only; a partial block is never billed twice next
		// period because periods do not overlap.
		summary.BillableBlocks = (summary.OverageUnits + rule.UnitSize - 1) / rule.UnitSize
		summary.OverageCents = summary.BillableBlocks * rule.PricePerUnitCents
		s.events.Emit(ctx, orgID, EventUsageLimitExceeded, map[string]any{
			"plan_id": planID, "metric": metric, "total": total,
			"included": rule.Included, "overage_units": summary.OverageUnits,
		})
	}
	return summary, nil
}

// MeteredPlans lists the plans that currently have metered overage rules,
// sorted for stable output (useful for admin tooling and tests).
func MeteredPlans() []string {
	overageMu.RLock()
	defer overageMu.RUnlock()
	plans := make([]string, 0, len(overageConfig))
	for plan := range overageConfig {
		plans = append(plans, plan)
	}
	sort.Strings(plans)
	return plans
}

// GenerateUsageInvoice issues an invoice for one billing period combining
// the plan base price with any metered overage for the given metric.
//
// Idempotent per (org, subscription, period): the run is claimed in
// usage_billing_runs before invoicing; replays return the stored invoice
// instead of billing the same window twice.
func (s *Service) GenerateUsageInvoice(ctx context.Context, orgID uuid.UUID, subID, planID, currency string, periodStart, periodEnd time.Time) (*store.Invoice, *UsageChargeSummary, error) {
	summary, err := s.CalculateUsageCharges(ctx, orgID, planID, MetricEvents, periodStart, periodEnd)
	if err != nil {
		return nil, nil, err
	}

	total := int64(planPriceCents(planID)) + summary.OverageCents
	if summary.Metered && summary.OverageCents > 0 && s.logger != nil {
		s.logger.Info("usage overage billed",
			zap.String("organization_id", orgID.String()),
			zap.Int64("overage_units", summary.OverageUnits),
			zap.Int64("overage_cents", summary.OverageCents))
	}

	invoice, err := s.GenerateInvoice(ctx, orgID, subID, int(total), currency)
	if err != nil {
		return nil, summary, err
	}
	claimed, err := s.db.TryClaimUsageBillingRun(ctx, orgID, subID, periodStart, periodEnd, invoice.ID)
	if err != nil {
		return nil, summary, err
	}
	if !claimed {
		if id, gerr := s.db.GetUsageBillingInvoice(ctx, orgID, subID, periodStart, periodEnd); gerr == nil && id != uuid.Nil {
			if prev, perr := s.db.GetInvoiceByID(ctx, id); perr == nil && prev != nil {
				return prev, summary, nil
			}
		}
		return invoice, summary, store.ErrUsageBillingDuplicate
	}
	return invoice, summary, nil
}
