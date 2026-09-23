-- v0.7: sampling, audit chain, notify delivery log, SCIM tokens, RBAC scopes

ALTER TABLE project_settings
  ADD COLUMN IF NOT EXISTS sample_rate DOUBLE PRECISION NOT NULL DEFAULT 1.0,
  ADD COLUMN IF NOT EXISTS data_region TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS notify_template TEXT NOT NULL DEFAULT '';

-- Append-only audit with hash chain (forensic-ish)
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS prev_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS entry_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS immutable BOOLEAN NOT NULL DEFAULT true;

CREATE TABLE IF NOT EXISTS notify_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    channel_kind TEXT NOT NULL DEFAULT '',
    target TEXT NOT NULL DEFAULT '',
    success BOOLEAN NOT NULL DEFAULT false,
    status_code INT NOT NULL DEFAULT 0,
    attempts INT NOT NULL DEFAULT 1,
    error TEXT NOT NULL DEFAULT '',
    body_preview TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS notify_deliveries_project_idx ON notify_deliveries(project_id, created_at DESC);

-- SCIM service tokens (enterprise directory sync stub)
CREATE TABLE IF NOT EXISTS scim_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    label TEXT NOT NULL DEFAULT 'scim',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ
);

-- Granular permission grants beyond role enum
CREATE TABLE IF NOT EXISTS role_scopes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    scope TEXT NOT NULL, -- e.g. issues:write, alerts:admin, audit:read
    UNIQUE (org_id, role, scope)
);
