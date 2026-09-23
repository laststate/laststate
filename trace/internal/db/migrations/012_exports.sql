-- v0.12: Webhook exports (S3-compatible storage)

-- Webhook exports store metadata about data exports to S3.
CREATE TABLE IF NOT EXISTS webhook_exports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
    project_id UUID REFERENCES projects(id) ON DELETE SET NULL,
    kind TEXT NOT NULL DEFAULT 'events_ndjson',
    -- Filter criteria (JSONB for flexibility)
    filters JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- S3 object key where data is stored
    object_key TEXT NOT NULL DEFAULT '',
    -- Size in bytes
    size_bytes BIGINT NOT NULL DEFAULT 0,
    -- Rows exported
    rows_count BIGINT NOT NULL DEFAULT 0,
    -- Status: 'pending', 'processing', 'completed', 'failed'
    status TEXT NOT NULL DEFAULT 'pending',
    -- Error message if failed
    error TEXT NOT NULL DEFAULT '',
    -- Created by user
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS webhook_exports_org_idx ON webhook_exports(organization_id, created_at DESC);
CREATE INDEX IF NOT EXISTS webhook_exports_project_idx ON webhook_exports(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS webhook_exports_status_idx ON webhook_exports(status);

-- S3 configuration stored per organization (can be overridden per export).
CREATE TABLE IF NOT EXISTS s3_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    endpoint TEXT NOT NULL DEFAULT '',
    bucket TEXT NOT NULL DEFAULT '',
    access_key TEXT NOT NULL DEFAULT '',
    secret_key TEXT NOT NULL DEFAULT '',
    region TEXT NOT NULL DEFAULT 'us-east-1',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (organization_id)
);
