-- The executable applies this schema idempotently at startup.
CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
-- See internal/store/store.go for the complete current schema and compatibility ALTERs.
