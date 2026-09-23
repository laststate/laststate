-- Hardening + product completeness (v0.4)
-- event processing history, regression, orgs extras, relays, retention helpers

CREATE TABLE IF NOT EXISTS event_state_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    from_state TEXT NOT NULL DEFAULT '',
    to_state TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    actor TEXT NOT NULL DEFAULT 'system',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS event_state_history_event_idx ON event_state_history(event_id, created_at DESC);

CREATE TABLE IF NOT EXISTS issue_activity (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    actor_user_id UUID REFERENCES users(id),
    action TEXT NOT NULL,
    body TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS issue_activity_issue_idx ON issue_activity(issue_id, created_at DESC);

CREATE TABLE IF NOT EXISTS issue_comments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    author_user_id UUID REFERENCES users(id),
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS issue_labels (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    color TEXT NOT NULL DEFAULT '#666666',
    UNIQUE (project_id, name)
);

CREATE TABLE IF NOT EXISTS issue_label_links (
    issue_id UUID NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    label_id UUID NOT NULL REFERENCES issue_labels(id) ON DELETE CASCADE,
    PRIMARY KEY (issue_id, label_id)
);

ALTER TABLE issues ADD COLUMN IF NOT EXISTS resolved_at TIMESTAMPTZ;
ALTER TABLE issues ADD COLUMN IF NOT EXISTS regression_count INT NOT NULL DEFAULT 0;
ALTER TABLE issues ADD COLUMN IF NOT EXISTS labels TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[];

ALTER TABLE events ADD COLUMN IF NOT EXISTS pipeline TEXT NOT NULL DEFAULT 'issue';
ALTER TABLE events ADD COLUMN IF NOT EXISTS process_version INT NOT NULL DEFAULT 0;
ALTER TABLE events ADD COLUMN IF NOT EXISTS reprocess_requested BOOLEAN NOT NULL DEFAULT false;

-- Anonymous devices get unique synthetic IDs; never force 'unknown'
ALTER TABLE devices ADD COLUMN IF NOT EXISTS aliases TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[];
ALTER TABLE devices ADD COLUMN IF NOT EXISTS tags TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[];
ALTER TABLE devices ADD COLUMN IF NOT EXISTS probe_id TEXT NOT NULL DEFAULT '';

ALTER TABLE releases ADD COLUMN IF NOT EXISTS git_commit TEXT NOT NULL DEFAULT '';
ALTER TABLE releases ADD COLUMN IF NOT EXISTS toolchain TEXT NOT NULL DEFAULT '';
ALTER TABLE releases ADD COLUMN IF NOT EXISTS rollout_pct INT NOT NULL DEFAULT 100;

ALTER TABLE artifacts ADD COLUMN IF NOT EXISTS quarantine_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE artifacts ADD COLUMN IF NOT EXISTS project_slug TEXT NOT NULL DEFAULT '';
ALTER TABLE artifacts ADD COLUMN IF NOT EXISTS release_version TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS relays (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    relay_id TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    version TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'unknown',
    capabilities JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_heartbeat TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, relay_id)
);

CREATE TABLE IF NOT EXISTS organization_invites (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'developer',
    token_hash BYTEA NOT NULL,
    invited_by UUID REFERENCES users(id),
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS cooldown_sec INT NOT NULL DEFAULT 300;
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS max_retries INT NOT NULL DEFAULT 3;
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS last_fired_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS hardware_revisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    revision TEXT NOT NULL,
    bom TEXT NOT NULL DEFAULT '',
    lot TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, revision, lot)
);

-- advisory lock helper table not needed; use pg_advisory_lock in Migrate

CREATE INDEX IF NOT EXISTS events_raw_hash_idx ON events(project_id, raw_hash);
CREATE INDEX IF NOT EXISTS events_received_retention_idx ON events(received_at);
