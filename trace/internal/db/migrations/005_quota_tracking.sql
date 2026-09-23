-- v0.5 quota tracking and feature flags
-- Tracks per-organization/postmortem usage quotas (daily generation limits)
-- and project-level feature flags for gradual rollout.

CREATE TABLE IF NOT EXISTS postmortem_quota (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    date DATE NOT NULL,
    count INTEGER NOT NULL DEFAULT 0,
    max_allowed INTEGER NOT NULL DEFAULT 0,
    UNIQUE (project_id, date)
);
CREATE INDEX IF NOT EXISTS postmortem_quota_project_date_idx ON postmortem_quota(project_id, date DESC);
CREATE INDEX IF NOT EXISTS postmortem_quota_date_idx ON postmortem_quota(date);

COMMENT ON TABLE postmortem_quota IS 'Daily postmortem generation quota per project';

CREATE TABLE IF NOT EXISTS feature_flags (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,  -- NULL = global flag
    flag_key TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT false,
    rollout_percentage DOUBLE PRECISION NOT NULL DEFAULT 0.0,   -- 0.0 .. 1.0
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, flag_key)
);
CREATE INDEX IF NOT EXISTS feature_flags_project_idx ON feature_flags(project_id);
CREATE INDEX IF NOT EXISTS feature_flags_key_idx ON feature_flags(flag_key) WHERE project_id IS NULL;
CREATE INDEX IF NOT EXISTS feature_flags_enabled_idx ON feature_flags(enabled);

COMMENT ON TABLE feature_flags IS 'Per-project or global feature flags for gradual rollout and kill switches';
