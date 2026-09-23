// Package billing is the in-repo, AGPL-3.0 side of plan/quota enforcement.
//
// The Stripe + Mercado Pago integration lives in the proprietary
// `laststate/billing` service (separate repo) and pushes entitlement changes
// into Trace through the HMAC-signed Admin API declared in admin_api.go.
package billing

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/store"
)

// SubscriptionStatus values mirror the subscription_status column.
const (
	StatusActive    = "active"
	StatusTrialing  = "trialing"
	StatusPastDue   = "past_due" // in grace period, ingest still allowed
	StatusSuspended = "suspended"
	StatusCanceled  = "canceled"
)

// Default tier names — kept here so quota code does not hardcode the strings.
const (
	TierFree       = "free"
	TierPro        = "pro"
	TierEnterprise = "enterprise"
)

// ErrQuotaExceeded signals the quota middleware should return 429. The
// Reason/Limit/Kind fields feed the error body so the client can react.
type ErrQuotaExceeded struct {
	Reason   string
	Kind     string
	Limit    int64
	Current  int64
	PlanTier string
	// RetryAfter is a hint (seconds) the client should wait before retrying.
	RetryAfter int
}

func (e *ErrQuotaExceeded) Error() string { return "billing: quota exceeded for " + e.Kind }

// IsQuotaExceeded lets callers branch without type assertion.
func IsQuotaExceeded(err error) bool {
	var qe *ErrQuotaExceeded
	return errors.As(err, &qe)
}

// Plan is the resolved view of (plan_limits row + current subscription).
type Plan struct {
	store.PlanLimit
	Sub store.OrgSubscription
}

// Resolver pulls plans and subscription state from the store. Defined here as
// an interface so quota checks can be exercised in unit tests with a fake.
type Resolver interface {
	GetOrgSubscription(ctx context.Context, orgID uuid.UUID) (store.OrgSubscription, error)
	GetPlan(ctx context.Context, tier string) (store.PlanLimit, error)
	CurrentUsage(ctx context.Context, orgID uuid.UUID, kind string) (store.UsageCounter, error)
}

// ResolvePlan returns the plan + sub for an org, with a free-tier fallback so
// unknown tiers (legacy orgs) do not silently bypass enforcement.
func ResolvePlan(ctx context.Context, r Resolver, orgID uuid.UUID) (Plan, error) {
	sub, err := r.GetOrgSubscription(ctx, orgID)
	if err != nil {
		return Plan{}, err
	}
	limit, err := r.GetPlan(ctx, sub.PlanTier)
	if err != nil {
		if fb, err2 := r.GetPlan(ctx, TierFree); err2 == nil {
			limit = fb
		} else {
			return Plan{}, err
		}
	}
	return Plan{PlanLimit: limit, Sub: sub}, nil
}

// IsWritable returns whether the org is in a state that allows new ingestion.
// 'past_due' is allowed if we are still inside grace_period_ends_at; otherwise
// the org is read-only.
func (p Plan) IsWritable(now time.Time) bool {
	switch p.Sub.SubscriptionStatus {
	case StatusActive, StatusTrialing:
		return true
	case StatusPastDue:
		if p.Sub.GracePeriodEndsAt != nil && now.Before(*p.Sub.GracePeriodEndsAt) {
			return true
		}
		return false
	default:
		return false
	}
}

// EventsAllowed returns whether an event is permitted and the remaining
// headroom this month (0 if unlimited). Current is what is already recorded
// for the month; the caller bumps the counter after a successful ingest.
func (p Plan) EventsAllowed(current int64) (allowed bool, remaining int64, limit int64) {
	limit = p.EventsPerMonth
	if limit == 0 {
		return true, 0, 0
	}
	if current >= limit {
		return false, 0, limit
	}
	return true, limit - current, limit
}
