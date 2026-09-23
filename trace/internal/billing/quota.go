package billing

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/store"
)

// CheckEvents enforces the events-per-month quota for one org. Returns
// ErrQuotaExceeded (typed, so middleware can 429) when blocked.
//
// The check is best-effort against the last-resolved snapshot of the counter:
// concurrent ingesters may briefly overshoot the limit but the monthly reset
// naturally absorbs the variance. For exact enforcement, callers should also
// wrap the counter increment in a transaction.
func CheckEvents(ctx context.Context, r Resolver, orgID uuid.UUID, now time.Time) error {
	p, err := ResolvePlan(ctx, r, orgID)
	if err != nil {
		return err
	}
	if !p.IsWritable(now) {
		return &ErrQuotaExceeded{
			Reason:   "subscription not writable: " + p.Sub.SubscriptionStatus,
			Kind:     store.UsageKindEvents,
			PlanTier: p.Sub.PlanTier,
		}
	}
	cur, err := r.CurrentUsage(ctx, orgID, store.UsageKindEvents)
	if err != nil {
		// Without a counter row we treat usage as 0 and let the limit decide.
		cur.Counter = 0
	}
	allowed, _, _ := p.EventsAllowed(cur.Counter)
	if allowed {
		return nil
	}
	hint := 0
	if p.Sub.GracePeriodEndsAt != nil && now.Before(*p.Sub.GracePeriodEndsAt) {
		hint = int(time.Until(*p.Sub.GracePeriodEndsAt).Seconds())
		if hint < 0 {
			hint = 0
		}
	}
	return &ErrQuotaExceeded{
		Reason:     "monthly event quota exhausted",
		Kind:       store.UsageKindEvents,
		PlanTier:   p.Sub.PlanTier,
		Limit:      p.EventsPerMonth,
		Current:    cur.Counter,
		RetryAfter: hint,
	}
}

// CheckProjectLimit enforces the projects_max plan limit. Skips when 0.
func CheckProjectLimit(ctx context.Context, r Resolver, orgID uuid.UUID, currentProjects int) error {
	p, err := ResolvePlan(ctx, r, orgID)
	if err != nil {
		return err
	}
	if p.ProjectsMax == 0 {
		return nil
	}
	if currentProjects >= p.ProjectsMax {
		return &ErrQuotaExceeded{
			Reason:   "project limit reached",
			Kind:     "projects",
			PlanTier: p.Sub.PlanTier,
			Limit:    int64(p.ProjectsMax),
			Current:  int64(currentProjects),
		}
	}
	return nil
}

// CheckMemberLimit enforces seats_max / members_max. Both are treated together
// because in this codebase seats ARE members-of-org.
func CheckMemberLimit(ctx context.Context, r Resolver, orgID uuid.UUID, currentMembers int) error {
	p, err := ResolvePlan(ctx, r, orgID)
	if err != nil {
		return err
	}
	if p.MembersMax == 0 && p.SeatsMax == 0 {
		return nil
	}
	cap := p.MembersMax
	if p.SeatsMax > cap {
		cap = p.SeatsMax
	}
	if currentMembers >= cap {
		return &ErrQuotaExceeded{
			Reason:   "seat limit reached",
			Kind:     "members",
			PlanTier: p.Sub.PlanTier,
			Limit:    int64(cap),
			Current:  int64(currentMembers),
		}
	}
	return nil
}
