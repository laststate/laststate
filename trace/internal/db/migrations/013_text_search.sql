-- Migration 013: Add text search indexes using pg_trgm
-- Enables fast fuzzy text search on event_id, fingerprint, and raw_hash columns

-- Enable pg_trgm extension for trigram-based indexing
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Create GIN indexes with trgm operator class for text search
CREATE INDEX IF NOT EXISTS idx_events_event_id_trgm ON events USING gin (event_id gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_events_fingerprint_trgm ON events USING gin (fingerprint gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_events_raw_hash_trgm ON events USING gin (raw_hash gin_trgm_ops);

-- Also add indexes for issues table search columns
CREATE INDEX IF NOT EXISTS idx_issues_title_trgm ON issues USING gin (title gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_issues_fingerprint_trgm ON issues USING gin (fingerprint gin_trgm_ops);

-- Create a view for common search patterns
CREATE OR REPLACE VIEW search_events AS
SELECT
    e.id,
    e.event_id,
    e.project_id,
    e.severity,
    e.received_at,
    e.fingerprint,
    i.title as issue_title
FROM events e
LEFT JOIN issues i ON e.issue_id = i.id
WHERE e.project_id IS NOT NULL;
