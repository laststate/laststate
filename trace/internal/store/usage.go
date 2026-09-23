package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// UsageCounter is the row shape of usage_counters for one (org, period, kind).
type UsageCounter struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	PeriodStart    time.Time `json:"period_start"`
	Kind           string    `json:"kind"`
	Counter        int64     `json:"counter"`
	Bytes          int64     `json:"bytes"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// UsageKind constants. Keep these in sync with the kinds the quota middleware
// emits so dashboards and limits stay aligned.
const (
	UsageKindEvents     = "events"
	UsageKindAPICalls   = "api_calls"
	UsageKindReleases   = "releases"
	UsageKindIssues     = "issues"
	UsageKindAlertsFire = "alerts_fired"
)

// MonthStart returns the truncated month boundary (UTC) for t. Period boundaries
// are computed in UTC to avoid timezone drift in quota aggregates.
func MonthStart(t time.Time) time.Time {
	return time.Date(t.UTC().Year(), t.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
}

// IncrementCounter atomically adds delta to the (org, period, kind) counter. If
// bytes is >0 it is added separately so storage-heavy events (raw uploads) can
// be tracked independently of count. Safe under concurrent ingest.
func (s *Store) IncrementCounter(ctx context.Context, orgID uuid.UUID, kind string, delta, bytes int64) error {
	if delta <= 0 && bytes <= 0 {
		return nil
	}
	_, err := s.Pool.Exec(ctx, `
INSERT INTO usage_counters(organization_id, period_start, kind, counter, bytes, updated_at)
VALUES ($1, date_trunc('month', now()), $2, GREATEST($3, 0), GREATEST($4, 0), now())
ON CONFLICT (organization_id, period_start, kind) DO UPDATE SET
  counter = usage_counters.counter + EXCLUDED.counter,
  bytes = usage_counters.bytes + EXCLUDED.bytes,
  updated_at = now()`,
		orgID, kind, delta, bytes,
	)
	return err
}

// CurrentUsage returns the counter for the current month for an org/kind.
func (s *Store) CurrentUsage(ctx context.Context, orgID uuid.UUID, kind string) (UsageCounter, error) {
	var u UsageCounter
	u.OrganizationID = orgID
	u.Kind = kind
	u.PeriodStart = MonthStart(time.Now())
	err := s.Pool.QueryRow(ctx, `
SELECT counter, bytes, updated_at FROM usage_counters
WHERE organization_id=$1 AND period_start=$2 AND kind=$3`,
		orgID, u.PeriodStart, kind,
	).Scan(&u.Counter, &u.Bytes, &u.UpdatedAt)
	return u, err
}

// UsageSummary is what /api/me/usage returns — one entry per known kind plus
// the plan tier and remaining headroom.
type UsageSummary struct {
	OrganizationID uuid.UUID        `json:"organization_id"`
	PlanTier       string           `json:"plan_tier"`
	PeriodStart    time.Time        `json:"period_start"`
	Counters       []UsageCounter   `json:"counters"`
	Limits         map[string]int64 `json:"limits"`
	Unlimited      map[string]bool  `json:"unlimited"`
}

// UsageSummary builds a JSON summary for /api/me/usage.
//
// limits/unlimited mirror plan_limits fields mapped to the matching UsageKind so
// the UI can show progress bars without knowing the plan semantics.
func (s *Store) UsageSummary(ctx context.Context, orgID uuid.UUID) (UsageSummary, error) {
	sub, err := s.GetOrgSubscription(ctx, orgID)
	if err != nil {
		return UsageSummary{}, err
	}
	plan, err := s.GetPlan(ctx, sub.PlanTier)
	if err != nil {
		// Unknown tier (e.g. legacy row). Fall back to free.
		plan, _ = s.GetPlan(ctx, "free")
	}
	now := MonthStart(time.Now())
	rows, err := s.Pool.Query(ctx, `
SELECT kind, counter, bytes, updated_at
FROM usage_counters WHERE organization_id=$1 AND period_start=$2`,
		orgID, now,
	)
	if err != nil {
		return UsageSummary{}, err
	}
	defer rows.Close()
	var counters []UsageCounter
	for rows.Next() {
		var u UsageCounter
		u.OrganizationID = orgID
		u.PeriodStart = now
		if err := rows.Scan(&u.Kind, &u.Counter, &u.Bytes, &u.UpdatedAt); err != nil {
			return UsageSummary{}, err
		}
		counters = append(counters, u)
	}
	if err := rows.Err(); err != nil {
		return UsageSummary{}, err
	}
	limits := map[string]int64{
		UsageKindEvents:   plan.EventsPerMonth,
		UsageKindReleases: int64(plan.ReleasesMax),
		UsageKindAPICalls: 0, // not yet capped per plan
	}
	unlimited := map[string]bool{
		UsageKindEvents:   plan.EventsPerMonth == 0,
		UsageKindReleases: plan.ReleasesMax == 0,
		UsageKindAPICalls: true,
	}
	return UsageSummary{
		OrganizationID: orgID,
		PlanTier:       sub.PlanTier,
		PeriodStart:    now,
		Counters:       counters,
		Limits:         limits,
		Unlimited:      unlimited,
	}, nil
}
