-- v0.5 fleet health scoring
-- Aggregates per-device health metrics into a single score with trending.
-- Used by the fleet dashboard and alerting to surface degrading devices.

CREATE TABLE IF NOT EXISTS device_health_scores (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL UNIQUE REFERENCES devices(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    overall_score DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    crash_rate DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    boot_failure_rate DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    crash_free_rate DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    error_count INTEGER NOT NULL DEFAULT 0,
    fatal_count INTEGER NOT NULL DEFAULT 0,
    assessment TEXT NOT NULL DEFAULT 'unknown',  -- healthy|degraded|critical|unknown
    computed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS device_health_scores_device_idx ON device_health_scores(device_id);
CREATE INDEX IF NOT EXISTS device_health_scores_project_idx ON device_health_scores(project_id, computed_at DESC);
CREATE INDEX IF NOT EXISTS device_health_scores_assessment_idx ON device_health_scores(assessment);

COMMENT ON TABLE device_health_scores IS 'Rolling per-device health score used for fleet-wide dashboards';

-- Trend history for alert thresholds and trend-line graphs
CREATE TABLE IF NOT EXISTS health_trend (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    score DOUBLE PRECISION NOT NULL,
    assessment TEXT NOT NULL DEFAULT 'unknown',
    captured_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS health_trend_device_ts_idx ON health_trend(device_id, captured_at DESC);
CREATE INDEX IF NOT EXISTS health_trend_ts_idx ON health_trend(captured_at DESC);

COMMENT ON TABLE health_trend IS 'Time-series of device health snapshots for trend detection';

-- Function that re-derives the aggregate health score from recent events.
-- Triggered by the event ingestion pipeline or called on a schedule.
CREATE OR REPLACE FUNCTION fn_recalc_device_health(p_device_id UUID)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    v_crash_rate DOUBLE PRECISION := 0.0;
    v_boot_fail DOUBLE PRECISION := 0.0;
    v_error_ct INTEGER := 0;
    v_fatal_ct INTEGER := 0;
    v_total BIGINT := 0;
    v_score DOUBLE PRECISION;
    v_assessment TEXT;
    v_project_id UUID;
BEGIN
    SELECT project_id INTO v_project_id FROM devices WHERE id = p_device_id;
    IF v_project_id IS NULL THEN
        RETURN;
    END IF;

    SELECT COALESCE(SUM(CASE WHEN type = 1 OR severity IN ('crash', 'fatal') THEN 1 ELSE 0 END)::DOUBLE PRECISION / NULLIF(COUNT(*), 0), 0)
      INTO v_crash_rate FROM events WHERE device_id = p_device_id AND received_at > now() - interval '24 hours';

    SELECT COALESCE(SUM(CASE WHEN type = 5 THEN 1 ELSE 0 END)::DOUBLE PRECISION / NULLIF(COUNT(*), 0), 0)
      INTO v_boot_fail FROM events WHERE device_id = p_device_id AND received_at > now() - interval '24 hours';

    SELECT COALESCE(COUNT(*), 0) INTO v_error_ct FROM events WHERE device_id = p_device_id AND received_at > now() - interval '24 hours' AND (type = 2 OR severity = 'error');
    SELECT COALESCE(COUNT(*), 0) INTO v_fatal_ct FROM events WHERE device_id = p_device_id AND received_at > now() - interval '24 hours' AND (type = 1 OR severity IN ('fatal', 'crash'));

    v_total := v_error_ct + v_fatal_ct;

    v_score := 1.0
        - (0.4 * v_crash_rate)
        - (0.3 * v_boot_fail)
        - LEAST(v_total * 0.001, 0.3);
    v_score := GREATEST(v_score, 0.0);

    IF v_score >= 0.9 THEN
        v_assessment := 'healthy';
    ELSIF v_score >= 0.7 THEN
        v_assessment := 'degraded';
    ELSIF v_score >= 0.4 THEN
        v_assessment := 'critical';
    ELSE
        v_assessment := 'critical';
    END IF;

    INSERT INTO device_health_scores(device_id, project_id, overall_score, crash_rate, boot_failure_rate, crash_free_rate, error_count, fatal_count, assessment, computed_at)
        VALUES (p_device_id, v_project_id, v_score, v_crash_rate, v_boot_fail, 1.0 - v_crash_rate, v_error_ct, v_fatal_ct, v_assessment, now())
        ON CONFLICT (device_id) DO UPDATE
            SET overall_score = v_score, crash_rate = v_crash_rate, boot_failure_rate = v_boot_fail,
                crash_free_rate = 1.0 - v_crash_rate, error_count = v_error_ct, fatal_count = v_fatal_ct,
                assessment = v_assessment, computed_at = now();
END;
$$;

-- Trigger: recalc health after new crash events land.
CREATE OR REPLACE FUNCTION fn_health_on_crash()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.device_id IS NOT NULL AND (NEW.type IN (1, 2) OR NEW.severity IN ('crash', 'fatal', 'error')) THEN
        PERFORM fn_recalc_device_health(NEW.device_id);
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE TRIGGER trg_health_on_crash
    AFTER INSERT ON events
    FOR EACH ROW
    EXECUTE FUNCTION fn_health_on_crash();

