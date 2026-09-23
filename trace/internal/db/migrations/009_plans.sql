-- v0.9: Plans, entitlements, per-org subscription state
-- Forward-only idempotent; safe to re-run.

ALTER TABLE organizations ADD COLUMN IF NOT EXISTS plan_tier TEXT NOT NULL DEFAULT 'free';
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS subscription_status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS grace_period_ends_at TIMESTAMPTZ;
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS trial_ends_at TIMESTAMPTZ;
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS billing_customer_ref TEXT;
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS billing_subscription_ref TEXT;

CREATE TABLE IF NOT EXISTS plan_limits (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tier TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    -- Ordered limits: events_per_month, projects_max, retention_days, seats_max,
    -- api_keys_max, releases_max, members_max, custom_unlimited (JSONB of flags).
    events_per_month BIGINT NOT NULL DEFAULT 0,
    projects_max INT NOT NULL DEFAULT 0,
    retention_days INT NOT NULL DEFAULT 30,
    seats_max INT NOT NULL DEFAULT 0,
    api_keys_max INT NOT NULL DEFAULT 0,
    releases_max INT NOT NULL DEFAULT 0,
    members_max INT NOT NULL DEFAULT 0,
    features JSONB NOT NULL DEFAULT '{}'::jsonb,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tier)
);

-- Seed default plans. 0 means "unlimited" for numeric limits, but we keep
-- explicit business rules per tier to be safe.
INSERT INTO plan_limits (tier, name, description, events_per_month, projects_max, retention_days, seats_max, api_keys_max, releases_max, members_max, features)
VALUES
    ('free', 'Free', 'Open-source tier — self-hosted friendly defaults', 10000, 1, 30, 3, 2, 5, 3,
     '{"ip_allowlist": false, "sso": false, "audit_export": false, "priority_support": false}'::jsonb),
    ('pro', 'Pro', 'For growing teams', 1000000, 10, 90, 15, 20, 200, 15,
     '{"ip_allowlist": false, "sso": false, "audit_export": true, "priority_support": true}'::jsonb),
    ('enterprise', 'Enterprise', 'Custom limits, SSO, dedicated support', 0, 0, 365, 0, 0, 0, 0,
     '{"ip_allowlist": true, "sso": true, "audit_export": true, "priority_support": true, "custom_retention": true, "unlimited": true}'::jsonb)
ON CONFLICT (tier) DO NOTHING;

-- Audit log of every billing-driven entitlement change. Idempotent by
-- (org_id, billing_event_id) so retries from the billing service are safe.
CREATE TABLE IF NOT EXISTS entitlement_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    -- External identifier from billing service (Stripe evt_..., MP id, etc.)
    billing_event_id TEXT NOT NULL,
    -- Snapshot of applied state for forensics.
    plan_tier TEXT NOT NULL,
    subscription_status TEXT NOT NULL,
    grace_period_ends_at TIMESTAMPTZ,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Free-form payload from the Admin API request (raw JSON for audit).
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    actor TEXT NOT NULL DEFAULT 'billing-service',
    UNIQUE (organization_id, billing_event_id)
);
CREATE INDEX IF NOT EXISTS entitlement_events_org_idx ON entitlement_events(organization_id, applied_at DESC);
