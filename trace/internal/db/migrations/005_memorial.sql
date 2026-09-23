-- v0.5 memorial features
-- A memorial view of devices that have gone silent (>30 days since last
-- activity) and a candles table for users to leave tributes to those
-- devices. Used by the memorial dashboard.

-- CTE-based view: lists devices that have not been seen in over 30 days.
CREATE OR REPLACE VIEW memorial_devices AS
WITH silent AS (
    SELECT
        d.id,
        d.device_id,
        d.product,
        d.hardware_revision,
        d.firmware_version,
        d.first_seen,
        d.last_seen,
        d.project_id,
        p.name AS project_name
    FROM devices d
    JOIN projects p ON p.id = d.project_id
    WHERE d.last_seen < (now() - interval '30 days')
)
SELECT * FROM silent;

COMMENT ON VIEW memorial_devices IS 'CTE view of devices silent for more than 30 days, for the memorial dashboard';

CREATE TABLE IF NOT EXISTS memorial_candles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    author_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    message TEXT NOT NULL DEFAULT '',
    is_public BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS memorial_candles_device_idx ON memorial_candles(device_id);
CREATE INDEX IF NOT EXISTS memorial_candles_project_ts_idx ON memorial_candles(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS memorial_candles_author_idx ON memorial_candles(author_user_id);

COMMENT ON TABLE memorial_candles IS 'Tributes left by users on silent (memorial) devices';
