-- 027: backfill session activity for pre-026 rows so idle timeout applies.
-- New sessions already insert last_used_at=now() (MintSession).
UPDATE sessions SET last_used_at = created_at WHERE last_used_at IS NULL;
