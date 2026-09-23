-- v0.5 soundboard feature
-- A soundboard that plays audio cues on crash events. Configuration
-- (enabled flag, volume) and the event log of played sounds.

CREATE TABLE IF NOT EXISTS soundboard_config (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL UNIQUE REFERENCES projects(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT true,
    volume DOUBLE PRECISION NOT NULL DEFAULT 1.0,  -- 0.0 .. 1.0
    audio_asset_id TEXT NOT NULL DEFAULT '',        -- reference to stored audio asset
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS soundboard_config_project_idx ON soundboard_config(project_id);

COMMENT ON TABLE soundboard_config IS 'Per-project soundboard configuration (enabled, volume, asset)';

CREATE TABLE IF NOT EXISTS soundboard_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    device_id UUID REFERENCES devices(id) ON DELETE SET NULL,
    issue_id UUID REFERENCES issues(id) ON DELETE SET NULL,
    triggered_by TEXT NOT NULL DEFAULT 'crash',     -- crash|fatal|alert|manual
    asset_id TEXT NOT NULL DEFAULT '',
    volume DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    played_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS soundboard_events_project_ts_idx ON soundboard_events(project_id, played_at DESC);
CREATE INDEX IF NOT EXISTS soundboard_events_device_idx ON soundboard_events(device_id);
CREATE INDEX IF NOT EXISTS soundboard_events_triggered_by_idx ON soundboard_events(triggered_by);
CREATE INDEX IF NOT EXISTS soundboard_events_metadata_idx ON soundboard_events USING gin (metadata);

COMMENT ON TABLE soundboard_events IS 'Log of soundboard plays triggered by crash events or manually';
