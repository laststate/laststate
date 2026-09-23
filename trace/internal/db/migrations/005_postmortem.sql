-- v0.5 postmortem reports
-- AI-generated postmortem documents linked to incidents. Stores markdown
-- body, generation status, and metadata for review/workflow tracking.

CREATE TABLE IF NOT EXISTS postmortem_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    issue_id UUID REFERENCES issues(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'pending', -- pending|generating|ready|reviewed|archived
    markdown TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    generated_at TIMESTAMPTZ,
    reviewed_at TIMESTAMPTZ,
    reviewer_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS postmortem_reports_project_idx ON postmortem_reports(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS postmortem_reports_issue_idx ON postmortem_reports(issue_id);
CREATE INDEX IF NOT EXISTS postmortem_reports_status_idx ON postmortem_reports(status);
CREATE INDEX IF NOT EXISTS postmortem_reports_generated_idx ON postmortem_reports(generated_at DESC);
CREATE INDEX IF NOT EXISTS postmortem_reports_metadata_idx ON postmortem_reports USING gin (metadata);

COMMENT ON TABLE postmortem_reports IS 'AI-generated postmortem documents linked to incidents';
