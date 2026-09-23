-- v0.5 pull-request records
-- Tracks PRs created to address issues (bug fixes, feature work). Enables
-- cycle-time reporting and closing-rate analytics for the incident pipeline.

CREATE TABLE IF NOT EXISTS pr_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    issue_id UUID REFERENCES issues(id) ON DELETE SET NULL,
    pr_url TEXT NOT NULL DEFAULT '',
    pr_number INTEGER NOT NULL DEFAULT 0,
    branch_name TEXT NOT NULL DEFAULT '',
    github_repo TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open',   -- open|closed|merged|draft|archived
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS pr_records_project_idx ON pr_records(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS pr_records_issue_idx ON pr_records(issue_id);
CREATE INDEX IF NOT EXISTS pr_records_status_idx ON pr_records(status);
CREATE INDEX IF NOT EXISTS pr_records_repo_number_idx ON pr_records(github_repo, pr_number);
CREATE INDEX IF NOT EXISTS pr_records_branch_idx ON pr_records(branch_name);

COMMENT ON TABLE pr_records IS 'Pull-requests linked to issues for cycle-time and closure tracking';
