-- 026: session activity tracking for /api/me/sessions and idle timeout.
-- ListSessions already selects last_used_at (previously missing -> 500s).
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS sessions_user_last_used_idx ON sessions(user_id, last_used_at DESC NULLS LAST);
