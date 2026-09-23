-- v0.6: DLQ, retention settings, FTS, alert skip log, job project scoping

ALTER TABLE jobs ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE CASCADE;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS dead_reason TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS jobs_dead_idx ON jobs(status) WHERE status = 'dead';
CREATE INDEX IF NOT EXISTS jobs_project_idx ON jobs(project_id) WHERE project_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS alert_skip_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    issue_id UUID REFERENCES issues(id) ON DELETE SET NULL,
    rule_id UUID REFERENCES alert_rules(id) ON DELETE SET NULL,
    kind TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL, -- cooldown|dedupe|disabled|ssrf|error
    detail TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS alert_skip_log_project_idx ON alert_skip_log(project_id, created_at DESC);

CREATE TABLE IF NOT EXISTS project_settings (
    project_id UUID PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    retention_events_days INT NOT NULL DEFAULT 90,
    retention_health_days INT NOT NULL DEFAULT 30,
    retention_logs_days INT NOT NULL DEFAULT 14,
    retention_metrics_days INT NOT NULL DEFAULT 30,
    analyzer_version_min INT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Full-text search helpers
ALTER TABLE issues ADD COLUMN IF NOT EXISTS search_tsv tsvector;
ALTER TABLE events ADD COLUMN IF NOT EXISTS search_tsv tsvector;

CREATE OR REPLACE FUNCTION issues_search_tsv_update() RETURNS trigger AS $$
BEGIN
  NEW.search_tsv :=
    setweight(to_tsvector('simple', coalesce(NEW.title,'')), 'A') ||
    setweight(to_tsvector('simple', coalesce(NEW.fingerprint,'')), 'B') ||
    setweight(to_tsvector('simple', coalesce(NEW.probable_cause,'')), 'C');
  RETURN NEW;
END
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS issues_search_tsv_trg ON issues;
CREATE TRIGGER issues_search_tsv_trg BEFORE INSERT OR UPDATE ON issues
FOR EACH ROW EXECUTE PROCEDURE issues_search_tsv_update();

CREATE OR REPLACE FUNCTION events_search_tsv_update() RETURNS trigger AS $$
BEGIN
  NEW.search_tsv :=
    setweight(to_tsvector('simple', coalesce(NEW.event_id,'')), 'A') ||
    setweight(to_tsvector('simple', coalesce(NEW.fingerprint,'')), 'B') ||
    setweight(to_tsvector('simple', coalesce(NEW.raw_hash,'')), 'C') ||
    setweight(to_tsvector('simple', coalesce(NEW.pipeline,'')), 'D');
  RETURN NEW;
END
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS events_search_tsv_trg ON events;
CREATE TRIGGER events_search_tsv_trg BEFORE INSERT OR UPDATE ON events
FOR EACH ROW EXECUTE PROCEDURE events_search_tsv_update();

CREATE INDEX IF NOT EXISTS issues_search_idx ON issues USING GIN (search_tsv);
CREATE INDEX IF NOT EXISTS events_search_idx ON events USING GIN (search_tsv);

-- backfill tsv
UPDATE issues SET title = title WHERE search_tsv IS NULL;
UPDATE events SET event_id = event_id WHERE search_tsv IS NULL;

CREATE TABLE IF NOT EXISTS dead_letter_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    action TEXT NOT NULL, -- requeue|discard
    actor_user_id UUID REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
