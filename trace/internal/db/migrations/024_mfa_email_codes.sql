-- 024: one-time MFA codes delivered by email (account-recovery second factor).
-- Only the SHA-256 hash is stored; raw codes travel by email only.
CREATE TABLE IF NOT EXISTS mfa_email_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id)
);
