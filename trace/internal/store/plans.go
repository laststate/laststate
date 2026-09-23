package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PlanLimit is the row shape of plan_limits. A 0 numeric limit means "unlimited"
// for that metric per the plan semantics; callers should not rely on 0-as-zero.
type PlanLimit struct {
	ID             uuid.UUID      `json:"id"`
	Tier           string         `json:"tier"`
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	EventsPerMonth int64          `json:"events_per_month"`
	ProjectsMax    int            `json:"projects_max"`
	RetentionDays  int            `json:"retention_days"`
	SeatsMax       int            `json:"seats_max"`
	APIKeysMax     int            `json:"api_keys_max"`
	ReleasesMax    int            `json:"releases_max"`
	MembersMax     int            `json:"members_max"`
	Features       map[string]any `json:"features"`
	Active         bool           `json:"active"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// OrgSubscription is the per-organization billing state.
type OrgSubscription struct {
	OrganizationID         uuid.UUID  `json:"organization_id"`
	PlanTier               string     `json:"plan_tier"`
	SubscriptionStatus     string     `json:"subscription_status"`
	GracePeriodEndsAt      *time.Time `json:"grace_period_ends_at,omitempty"`
	TrialEndsAt            *time.Time `json:"trial_ends_at,omitempty"`
	BillingCustomerRef     string     `json:"billing_customer_ref,omitempty"`
	BillingSubscriptionRef string     `json:"billing_subscription_ref,omitempty"`
}

// ErrUnknownPlan is returned when a tier does not exist in plan_limits.
var ErrUnknownPlan = errors.New("store: unknown plan tier")

// ListPlans returns every plan row ordered by tier. Useful for /api/admin/plans.
func (s *Store) ListPlans(ctx context.Context) ([]PlanLimit, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT id, tier, name, description, events_per_month, projects_max, retention_days,
       seats_max, api_keys_max, releases_max, members_max, features, active, created_at, updated_at
FROM plan_limits WHERE active=true ORDER BY tier`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlanLimit
	for rows.Next() {
		var p PlanLimit
		var feats []byte
		if err := rows.Scan(&p.ID, &p.Tier, &p.Name, &p.Description,
			&p.EventsPerMonth, &p.ProjectsMax, &p.RetentionDays,
			&p.SeatsMax, &p.APIKeysMax, &p.ReleasesMax, &p.MembersMax,
			&feats, &p.Active, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		if len(feats) > 0 {
			_ = json.Unmarshal(feats, &p.Features)
		}
		if p.Features == nil {
			p.Features = map[string]any{}
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetPlan fetches a single plan by tier.
func (s *Store) GetPlan(ctx context.Context, tier string) (PlanLimit, error) {
	var p PlanLimit
	var feats []byte
	err := s.Pool.QueryRow(ctx, `
SELECT id, tier, name, description, events_per_month, projects_max, retention_days,
       seats_max, api_keys_max, releases_max, members_max, features, active, created_at, updated_at
FROM plan_limits WHERE tier=$1`, tier).Scan(
		&p.ID, &p.Tier, &p.Name, &p.Description,
		&p.EventsPerMonth, &p.ProjectsMax, &p.RetentionDays,
		&p.SeatsMax, &p.APIKeysMax, &p.ReleasesMax, &p.MembersMax,
		&feats, &p.Active, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PlanLimit{}, ErrUnknownPlan
	}
	if err != nil {
		return PlanLimit{}, err
	}
	if len(feats) > 0 {
		_ = json.Unmarshal(feats, &p.Features)
	}
	if p.Features == nil {
		p.Features = map[string]any{}
	}
	return p, nil
}

// UpsertPlan creates or updates a plan row by tier. Returns the new row.
func (s *Store) UpsertPlan(ctx context.Context, p PlanLimit) (PlanLimit, error) {
	if p.Features == nil {
		p.Features = map[string]any{}
	}
	feats, err := json.Marshal(p.Features)
	if err != nil {
		return PlanLimit{}, err
	}
	var out PlanLimit
	var outFeats []byte
	err = s.Pool.QueryRow(ctx, `
INSERT INTO plan_limits(tier, name, description, events_per_month, projects_max, retention_days,
                       seats_max, api_keys_max, releases_max, members_max, features, active, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,now())
ON CONFLICT (tier) DO UPDATE SET
  name=EXCLUDED.name,
  description=EXCLUDED.description,
  events_per_month=EXCLUDED.events_per_month,
  projects_max=EXCLUDED.projects_max,
  retention_days=EXCLUDED.retention_days,
  seats_max=EXCLUDED.seats_max,
  api_keys_max=EXCLUDED.api_keys_max,
  releases_max=EXCLUDED.releases_max,
  members_max=EXCLUDED.members_max,
  features=EXCLUDED.features,
  active=EXCLUDED.active,
  updated_at=now()
RETURNING id, tier, name, description, events_per_month, projects_max, retention_days,
          seats_max, api_keys_max, releases_max, members_max, features, active, created_at, updated_at`,
		p.Tier, p.Name, p.Description, p.EventsPerMonth, p.ProjectsMax, p.RetentionDays,
		p.SeatsMax, p.APIKeysMax, p.ReleasesMax, p.MembersMax, feats, p.Active,
	).Scan(&out.ID, &out.Tier, &out.Name, &out.Description,
		&out.EventsPerMonth, &out.ProjectsMax, &out.RetentionDays,
		&out.SeatsMax, &out.APIKeysMax, &out.ReleasesMax, &out.MembersMax,
		&outFeats, &out.Active, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return PlanLimit{}, err
	}
	if len(outFeats) > 0 {
		_ = json.Unmarshal(outFeats, &out.Features)
	}
	return out, nil
}

// GetOrgSubscription returns the billing-relevant columns for an org.
func (s *Store) GetOrgSubscription(ctx context.Context, orgID uuid.UUID) (OrgSubscription, error) {
	var out OrgSubscription
	err := s.Pool.QueryRow(ctx, `
SELECT id, plan_tier, subscription_status, grace_period_ends_at, trial_ends_at,
       COALESCE(billing_customer_ref,''), COALESCE(billing_subscription_ref,'')
FROM organizations WHERE id=$1`, orgID).Scan(
		&out.OrganizationID, &out.PlanTier, &out.SubscriptionStatus,
		&out.GracePeriodEndsAt, &out.TrialEndsAt,
		&out.BillingCustomerRef, &out.BillingSubscriptionRef,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrgSubscription{}, ErrNotFound
	}
	return out, err
}

// SetOrgSubscription updates the org's billing state and records a plan_history
// row in the same transaction. Returns the new state.
//
// actor is a free-form identifier (e.g. "billing-service", "admin:user@host").
// reason is short context ("trial_started", "renewal_failed", etc.).
func (s *Store) SetOrgSubscription(ctx context.Context, sub OrgSubscription, actor, reason string) (OrgSubscription, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return OrgSubscription{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Read current tier for history.
	var prevTier string
	if err := tx.QueryRow(ctx, `SELECT plan_tier FROM organizations WHERE id=$1`, sub.OrganizationID).Scan(&prevTier); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OrgSubscription{}, ErrNotFound
		}
		return OrgSubscription{}, err
	}

	_, err = tx.Exec(ctx, `
UPDATE organizations SET
  plan_tier=$2,
  subscription_status=$3,
  grace_period_ends_at=$4,
  trial_ends_at=$5,
  billing_customer_ref=NULLIF($6,''),
  billing_subscription_ref=NULLIF($7,'')
WHERE id=$1`,
		sub.OrganizationID, sub.PlanTier, sub.SubscriptionStatus,
		sub.GracePeriodEndsAt, sub.TrialEndsAt,
		sub.BillingCustomerRef, sub.BillingSubscriptionRef,
	)
	if err != nil {
		return OrgSubscription{}, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO plan_history(organization_id, from_tier, to_tier, reason, actor)
VALUES ($1, NULLIF($2,''), $3, $4, $5)`,
		sub.OrganizationID, prevTier, sub.PlanTier, reason, actor); err != nil {
		return OrgSubscription{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return OrgSubscription{}, err
	}
	return sub, nil
}

// RecordEntitlementEvent persists an idempotent entitlement event from the
// billing service. Returns true if the event was newly inserted, false if a
// duplicate was silently ignored.
func (s *Store) RecordEntitlementEvent(ctx context.Context, orgID uuid.UUID, billingEventID, tier, status string, graceEndsAt *time.Time, actor string, payload json.RawMessage) (bool, error) {
	if payload == nil {
		payload = json.RawMessage("{}")
	}
	ct, err := s.Pool.Exec(ctx, `
INSERT INTO entitlement_events(organization_id, billing_event_id, plan_tier, subscription_status, grace_period_ends_at, actor, payload)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (organization_id, billing_event_id) DO NOTHING`,
		orgID, billingEventID, tier, status, graceEndsAt, actor, payload,
	)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

// PlanHistoryEntry is one row of plan_history for audit display.
type PlanHistoryEntry struct {
	ID        uuid.UUID `json:"id"`
	FromTier  string    `json:"from_tier,omitempty"`
	ToTier    string    `json:"to_tier"`
	Reason    string    `json:"reason"`
	Actor     string    `json:"actor"`
	ChangedAt time.Time `json:"changed_at"`
}

// ListPlanHistory returns the most recent plan changes for an org.
func (s *Store) ListPlanHistory(ctx context.Context, orgID uuid.UUID, limit int) ([]PlanHistoryEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id, COALESCE(from_tier,''), to_tier, reason, actor, changed_at
FROM plan_history WHERE organization_id=$1
ORDER BY changed_at DESC LIMIT $2`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlanHistoryEntry
	for rows.Next() {
		var h PlanHistoryEntry
		if err := rows.Scan(&h.ID, &h.FromTier, &h.ToTier, &h.Reason, &h.Actor, &h.ChangedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
