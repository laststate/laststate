-- v0.5 chaos testing results
-- Records outcomes of chaos-engine injections (network partition, fault
-- injection, resource exhaustion). Used to correlate chaos events with
-- crash spikes and validate resilience hypotheses.

CREATE TABLE IF NOT EXISTS chaos_injections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    device_id UUID REFERENCES devices(id) ON DELETE SET NULL,
    injection_type TEXT NOT NULL,          -- network_partition|memory_pressure|cpu_storm|disk_io|random_fault|latency
    target TEXT NOT NULL DEFAULT '',       -- human-readable target (e.g., "api-server-3")
    status TEXT NOT NULL DEFAULT 'running',-- running|completed|failed|aborted
    captured JSONB NOT NULL DEFAULT '{}'::jsonb,  -- raw captured telemetry snapshot
    duration_seconds INTEGER NOT NULL DEFAULT 0,
    error TEXT,                            -- descriptive error if status=failed
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS chaos_injections_project_ts_idx ON chaos_injections(project_id, started_at DESC);
CREATE INDEX IF NOT EXISTS chaos_injections_device_idx ON chaos_injections(device_id);
CREATE INDEX IF NOT EXISTS chaos_injections_type_idx ON chaos_injections(injection_type);
CREATE INDEX IF NOT EXISTS chaos_injections_status_idx ON chaos_injections(status);
CREATE INDEX IF NOT EXISTS chaos_injections_metadata_idx ON chaos_injections USING gin (metadata);
CREATE INDEX IF NOT EXISTS chaos_injections_captured_idx ON chaos_injections USING gin (captured);

COMMENT ON TABLE chaos_injections IS 'Chaos-engine injection records and their outcomes';
