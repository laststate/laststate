-- v0.5 public API aggregations
-- Materialized view and regular view exposed to the public status page.
-- Crash statistics are aggregated and anonymized (no device IDs, no
-- project IDs) so the status page can be served without auth.

CREATE MATERIALIZED VIEW IF NOT EXISTS public_crash_stats AS
SELECT
    d.project_id,
    d.product AS product_family,
    COUNT(*) FILTER (WHERE c.severity = 'crash') AS crash_count,
    COUNT(*) FILTER (WHERE c.severity = 'fatal') AS fatal_count,
    COUNT(*) FILTER (WHERE c.severity = 'error') AS error_count,
    COUNT(DISTINCT c.device_id) AS affected_devices,
    MIN(c.received_at) AS first_event_at,
    MAX(c.received_at) AS last_event_at,
    COUNT(*) AS total_events
FROM events c
JOIN devices d ON d.id = c.device_id
GROUP BY d.project_id, d.product;

-- Unique index is required for REFRESH MATERIALIZED VIEW CONCURRENTLY
CREATE UNIQUE INDEX IF NOT EXISTS public_crash_stats_product_idx ON public_crash_stats(product_family);

COMMENT ON MATERIALIZED VIEW public_crash_stats IS 'Aggregated, anonymized crash statistics for the public status page';

CREATE OR REPLACE VIEW public_crash_trend AS
SELECT
    DATE_TRUNC('day', c.received_at) AS day,
    COUNT(*) FILTER (WHERE c.severity = 'crash') AS crash_count,
    COUNT(*) FILTER (WHERE c.severity = 'fatal') AS fatal_count,
    COUNT(DISTINCT c.device_id) AS affected_devices,
    COUNT(*) AS total_events
FROM events c
WHERE c.received_at >= now() - interval '90 days'
GROUP BY DATE_TRUNC('day', c.received_at)
ORDER BY day DESC;

COMMENT ON VIEW public_crash_trend IS 'Daily aggregated crash trend for the public status page (last 90 days)';
