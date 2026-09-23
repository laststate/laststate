-- v0.5 anomaly detection events
-- Time-series anomalies flagged by the anomaly-detection pipeline
-- (Z-score, IQR, or ML-based). Used for proactive alerting and root-cause
-- correlation with chaos injections.

CREATE TABLE IF NOT EXISTS anomaly_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    device_id UUID REFERENCES devices(id) ON DELETE SET NULL,
    metric TEXT NOT NULL,                  -- cpu_usage|memory_usage|network_latency|crash_rate|boot_time|disk_io
    metric_group TEXT NOT NULL DEFAULT '', -- optional grouping for dashboard facets
    value DOUBLE PRECISION NOT NULL,
    min_expected DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    max_expected DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    deviation_score DOUBLE PRECISION NOT NULL DEFAULT 0.0,  -- how many sigmas away
    severity TEXT NOT NULL DEFAULT 'warning', -- info|warning|critical
    description TEXT NOT NULL DEFAULT '',
    captured_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS anomaly_events_project_ts_idx ON anomaly_events(project_id, captured_at DESC);
CREATE INDEX IF NOT EXISTS anomaly_events_device_ts_idx ON anomaly_events(device_id, captured_at DESC);
CREATE INDEX IF NOT EXISTS anomaly_events_metric_idx ON anomaly_events(metric);
CREATE INDEX IF NOT EXISTS anomaly_events_severity_idx ON anomaly_events(severity);
CREATE INDEX IF NOT EXISTS anomaly_events_ts_idx ON anomaly_events(captured_at DESC);
CREATE INDEX IF NOT EXISTS anomaly_events_metric_group_idx ON anomaly_events(metric_group);

COMMENT ON TABLE anomaly_events IS 'Flagged metric anomalies used for proactive alerting and correlation';
