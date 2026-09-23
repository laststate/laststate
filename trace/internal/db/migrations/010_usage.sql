-- v0.10: Usage counters + extended audit chain columns
-- Forward-only idempotent.

-- Per-org usage rollups. The (organization_id, period_start) is the primary key
-- so the ingest hot path can do a single UPSERT per write.
CREATE TABLE IF NOT EXISTS usage_counters (
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    period_start TIMESTAMPTZ NOT NULL, -- truncate to month boundary on insert
    kind TEXT NOT NULL,                -- 'events', 'api_calls', 'releases', etc.
    counter BIGINT NOT NULL DEFAULT 0,
    bytes BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, period_start, kind)
);
CREATE INDEX IF NOT EXISTS usage_counters_org_kind_idx ON usage_counters(organization_id, kind, period_start DESC);

-- Daily counter view support (lightweight, no view needed for now).

-- Tokens gain org_id so org-scoped API keys can be issued without picking a
-- project up-front. project_id becomes optional.
ALTER TABLE tokens ADD COLUMN IF NOT EXISTS organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE;
ALTER TABLE tokens ALTER COLUMN project_id DROP NOT NULL;

-- Membership gains audit-friendly timestamps if missing.
ALTER TABLE memberships ADD COLUMN IF NOT EXISTS invited_by UUID REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE memberships ADD COLUMN IF NOT EXISTS invited_at TIMESTAMPTZ;
ALTER TABLE memberships ADD COLUMN IF NOT EXISTS accepted_at TIMESTAMPTZ;

-- Sessions gain device metadata for the /api/me/sessions UI.
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS user_agent TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS ip INET;

-- Plan downgrade audit: every plan change is recorded in plan_history so we can
-- reconstruct a billing timeline without scanning audit_logs.
CREATE TABLE IF NOT EXISTS plan_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    from_tier TEXT,
    to_tier TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    actor TEXT NOT NULL DEFAULT 'system',
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS plan_history_org_idx ON plan_history(organization_id, changed_at DESC);
