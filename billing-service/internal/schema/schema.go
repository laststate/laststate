// Package schema defines the database schema for the billing service.
package schema

const MigrationSQL = `
-- Organizations (tenants)
CREATE TABLE IF NOT EXISTS organizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    email TEXT NOT NULL,
    tier TEXT NOT NULL DEFAULT 'free' CHECK (tier IN ('free', 'hobbyist', 'team', 'enterprise')),
    address TEXT NOT NULL DEFAULT '',
    tax_id TEXT NOT NULL DEFAULT '',
    stripe_customer_id TEXT,
    stripe_subscription_id TEXT,
    mercado_pago_pref_id TEXT,
    phone TEXT NOT NULL DEFAULT '',
    trial_ends_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Phone column for pre-migration databases (CREATE above covers fresh installs).
ALTER TABLE IF EXISTS organizations ADD COLUMN IF NOT EXISTS phone TEXT NOT NULL DEFAULT '';

-- Subscriptions
CREATE TABLE IF NOT EXISTS subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id),
    provider TEXT NOT NULL CHECK (provider IN ('stripe', 'mercado_pago', 'crypto')),
    provider_subscription_id TEXT NOT NULL,
    plan_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'past_due', 'canceled', 'expired', 'trialing')),
    current_period_start TIMESTAMPTZ NOT NULL,
    current_period_end TIMESTAMPTZ NOT NULL,
    cancel_at_period_end BOOLEAN NOT NULL DEFAULT false,
    last_proration_cents INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Invoices
CREATE TABLE IF NOT EXISTS invoices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id),
    subscription_id UUID REFERENCES subscriptions(id),
    provider TEXT NOT NULL,
    provider_invoice_id TEXT,
    amount_cents INTEGER NOT NULL,
    currency TEXT NOT NULL DEFAULT 'usd',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'paid', 'failed', 'refunded')),
    issued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    paid_at TIMESTAMPTZ,
    due_at TIMESTAMPTZ,
    pdf_url TEXT,
    nfe_url TEXT
);

-- Webhook events (idempotency store)
CREATE TABLE IF NOT EXISTS webhook_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider TEXT NOT NULL,
    event_id TEXT NOT NULL UNIQUE,
    raw_payload JSONB NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Payment attempts (dunning tracking)
CREATE TABLE IF NOT EXISTS payment_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id),
    invoice_id UUID REFERENCES invoices(id),
    attempt_number INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL CHECK (status IN ('pending', 'succeeded', 'failed', 'max_retries_exceeded')),
    last_error TEXT,
    next_retry_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Usage metrics
CREATE TABLE IF NOT EXISTS usage_metrics (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id),
    metric_name TEXT NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    value BIGINT NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    idempotency_key TEXT
);

-- Idempotency for usage ingest (pre-migration databases).
ALTER TABLE IF EXISTS usage_metrics ADD COLUMN IF NOT EXISTS idempotency_key TEXT;
-- One ingest per (org, metric, key): retries with the same key are dropped.
-- Named constraint so INSERT ... ON CONFLICT ON CONSTRAINT works.
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'ux_usage_metrics_idem') THEN
        ALTER TABLE usage_metrics ADD CONSTRAINT ux_usage_metrics_idem UNIQUE (organization_id, metric_name, idempotency_key);
    END IF;
END $$;

-- Usage billing runs: one invoice per (org, subscription, period).
CREATE TABLE IF NOT EXISTS usage_billing_runs (
    organization_id UUID NOT NULL REFERENCES organizations(id),
    subscription_id TEXT NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    invoice_id UUID NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, subscription_id, period_start, period_end)
);

-- Tier quotas — device/event/retention limits per subscription tier
CREATE TABLE IF NOT EXISTS tier_quotas (
    tier TEXT PRIMARY KEY,
    max_devices INTEGER NOT NULL DEFAULT 0,
    max_events_per_day BIGINT NOT NULL DEFAULT 0,
    retention_days INTEGER NOT NULL DEFAULT 30,
    max_api_tokens INTEGER NOT NULL DEFAULT 0,
    max_alert_rules INTEGER NOT NULL DEFAULT 0,
    symbolication BOOLEAN NOT NULL DEFAULT false,
    analytics BOOLEAN NOT NULL DEFAULT false,
    custom_integrations BOOLEAN NOT NULL DEFAULT false,
    priority_support BOOLEAN NOT NULL DEFAULT false,
    sso BOOLEAN NOT NULL DEFAULT false,
    audit_logs BOOLEAN NOT NULL DEFAULT false,
    oncall BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed tier quotas
INSERT INTO tier_quotas (tier, max_devices, max_events_per_day, retention_days, max_api_tokens, max_alert_rules, symbolication, analytics, custom_integrations, priority_support, sso, audit_logs, oncall) VALUES
('free',          5,   1000,          30,  1,  0, true,   false, false, false, false, false, false),
('hobbyist',      100, 50000,         90,  5,  5,  true,  true,   false, false, false, false, false),
('team',          1000, 500000,       365, 20, 50,  true,  true,   true,  true,  true,  true,  true),
('enterprise',    -1,  -1,            -1, -1, -1,  true,  true,   true,  true,  true,  true,  true)
ON CONFLICT (tier) DO NOTHING;

-- Plans (billing plans)
CREATE TABLE IF NOT EXISTS plans (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    price_cents INTEGER NOT NULL,
    currency TEXT NOT NULL DEFAULT 'usd',
    interval TEXT NOT NULL DEFAULT 'month' CHECK (interval IN ('day', 'week', 'month', 'year')),
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed default plans (USD cents per month)
INSERT INTO plans (id, name, price_cents, currency, interval, description) VALUES
('free',        'Free',        0,     'usd', 'month', 'Free — 5 devices, 1k events/day, 30-day retention'),
('hobbyist',    'Hobbyist',    900,   'usd', 'month', 'Hobbyist — 100 devices, 50k events/day, 90-day retention, custom alerts'),
('team',        'Team',        4900,  'usd', 'month', 'Team — 1k devices, 500k events/day, 1-year retention, SSO, audit, on-call'),
('enterprise',  'Enterprise',  19900, 'usd', 'month', 'Enterprise — unlimited, on-prem, SLA, priority support')
ON CONFLICT (id) DO NOTHING;

-- API tokens (per-organization)
-- Valid scopes: 'read', 'write', 'admin', 'billing', 'entitlements', 'analytics'
CREATE TABLE IF NOT EXISTS api_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id),
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    scopes TEXT[] NOT NULL DEFAULT '{}' CHECK (array_length(scopes, 1) IS NULL OR array_length(scopes, 1) > 0),
    expires_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by UUID
);

-- Discount coupons. Exactly one of percent_off / amount_off_cents is set.
-- max_redemptions NULL = unlimited; max_per_organization limits per-org reuse.
CREATE TABLE IF NOT EXISTS coupons (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code TEXT NOT NULL UNIQUE,
    description TEXT,
    percent_off INTEGER CHECK (percent_off IS NULL OR (percent_off > 0 AND percent_off <= 100)),
    amount_off_cents INTEGER CHECK (amount_off_cents IS NULL OR amount_off_cents >= 0),
    currency TEXT NOT NULL DEFAULT 'usd',
    max_redemptions INTEGER,
    redeemed_count INTEGER NOT NULL DEFAULT 0,
    max_per_organization INTEGER NOT NULL DEFAULT 1,
    expires_at TIMESTAMPTZ,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((percent_off IS NULL) <> (amount_off_cents IS NULL))
);

CREATE TABLE IF NOT EXISTS coupon_redemptions (
    coupon_id UUID NOT NULL REFERENCES coupons(id),
    organization_id UUID NOT NULL REFERENCES organizations(id),
    invoice_id UUID,
    redeemed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (coupon_id, organization_id, redeemed_at)
);

CREATE INDEX IF NOT EXISTS idx_subscriptions_org ON subscriptions(organization_id);
CREATE INDEX IF NOT EXISTS idx_invoices_org ON invoices(organization_id);
CREATE INDEX IF NOT EXISTS idx_webhook_events_event_id ON webhook_events(event_id);
CREATE INDEX IF NOT EXISTS idx_usage_metrics_org_period ON usage_metrics(organization_id, period_start, period_end);
CREATE INDEX IF NOT EXISTS idx_payment_attempts_org ON payment_attempts(organization_id);
CREATE INDEX IF NOT EXISTS idx_api_tokens_org ON api_tokens(organization_id);
CREATE INDEX IF NOT EXISTS idx_api_tokens_hash ON api_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_coupons_code ON coupons(code);
CREATE INDEX IF NOT EXISTS idx_coupon_redemptions_org ON coupon_redemptions(organization_id);
`
