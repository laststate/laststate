package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/laststate/trace/internal/auth"
)

// ErrEmailAlreadyVerified is returned when trying to verify an already verified email.
var ErrEmailAlreadyVerified = errors.New("store: email already verified")

// ErrInvalidResetToken is returned when the reset token is invalid or expired.
var ErrInvalidResetToken = errors.New("store: invalid or expired reset token")

// ErrMFARequired is returned when MFA is required but not provided.
var ErrMFARequired = errors.New("store: MFA required")

// hashToken hashes a one-time token for storage. Raw tokens are emailed, so
// the database must never hold a usable copy (sessions follow the same rule).
func hashToken(token string) string {
	return hex.EncodeToString(auth.Hash(token))
}

// VerifyEmail verifies a user's email using a token.
func (s *Store) VerifyEmail(ctx context.Context, token string) error {
	var userID uuid.UUID
	var expiresAt time.Time
	err := s.Pool.QueryRow(ctx, `
SELECT user_id, expires_at FROM verification_tokens WHERE token=$1`, hashToken(token)).
		Scan(&userID, &expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidResetToken
		}
		return err
	}
	if time.Now().After(expiresAt) {
		return ErrInvalidResetToken
	}
	// Mark email as verified.
	_, err = s.Pool.Exec(ctx, `
UPDATE users SET email_verified_at=now(), updated_at=now() WHERE id=$1`, userID)
	if err != nil {
		return err
	}
	// Delete the token.
	_, err = s.Pool.Exec(ctx, `DELETE FROM verification_tokens WHERE user_id=$1`, userID)
	return err
}

// IssueVerificationToken creates (or refreshes) a verification token for a
// user and returns the raw token to email. Only the hash is stored.
func (s *Store) IssueVerificationToken(ctx context.Context, userID uuid.UUID) (string, error) {
	token, err := GenerateVerifyToken()
	if err != nil {
		return "", err
	}
	_, err = s.Pool.Exec(ctx, `
INSERT INTO verification_tokens(user_id, token, expires_at)
VALUES ($1, $2, now() + interval '24 hours')
ON CONFLICT (user_id) DO UPDATE SET token=$2, expires_at=now() + interval '24 hours'`,
		userID, hashToken(token))
	if err != nil {
		return "", err
	}
	return token, nil
}

// RequestPasswordReset generates a reset token and returns it.
func (s *Store) RequestPasswordReset(ctx context.Context, email string) (string, error) {
	var userID uuid.UUID
	err := s.Pool.QueryRow(ctx, `SELECT id FROM users WHERE email=$1`, email).Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Return a dummy token to prevent email enumeration.
			return GenerateResetToken()
		}
		return "", err
	}
	token, err := GenerateResetToken()
	if err != nil {
		return "", err
	}
	_, err = s.Pool.Exec(ctx, `
INSERT INTO password_reset_tokens(user_id, token, expires_at)
VALUES ($1, $2, now() + interval '1 hour')
ON CONFLICT (user_id) DO UPDATE SET token=$2, expires_at=now() + interval '1 hour'`,
		userID, hashToken(token))
	if err != nil {
		return "", err
	}
	return token, nil
}

// ResetPassword resets a user's password using a reset token.
// All existing sessions are revoked: whoever held the old password
// (or a stolen session) loses access on reset.
func (s *Store) ResetPassword(ctx context.Context, token, newPassword string) error {
	var userID uuid.UUID
	var expiresAt time.Time
	err := s.Pool.QueryRow(ctx, `
SELECT user_id, expires_at FROM password_reset_tokens WHERE token=$1`, hashToken(token)).
		Scan(&userID, &expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidResetToken
		}
		return err
	}
	if time.Now().After(expiresAt) {
		return ErrInvalidResetToken
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `
UPDATE users SET password_hash=$1, updated_at=now() WHERE id=$2`, hash, userID)
	if err != nil {
		return err
	}
	// Delete the token.
	if _, err = s.Pool.Exec(ctx, `DELETE FROM password_reset_tokens WHERE user_id=$1`, userID); err != nil {
		return err
	}
	return s.RevokeAllSessions(ctx, userID)
}

// GenerateVerifyToken generates a random verification token.
func GenerateVerifyToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b[:]), nil
}

// GenerateResetToken generates a random reset token.
func GenerateResetToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b[:]), nil
}

// StageMFA stores a TOTP secret without enabling it. The user confirms
// enrollment by verifying a code (apiMfaVerify), which flips is_enabled.
func (s *Store) StageMFA(ctx context.Context, userID uuid.UUID, secret string) error {
	_, err := s.Pool.Exec(ctx, `
INSERT INTO mfa_secrets(user_id, secret, is_enabled, created_at)
VALUES ($1, $2, false, now())
ON CONFLICT (user_id) DO UPDATE SET secret=$2, is_enabled=false, updated_at=now()`,
		userID, secret)
	return err
}

// OrgMember is one membership row joined with its user.
type OrgMember struct {
	ID    uuid.UUID
	Email string
	Name  string
	Role  string
}

// MemberByID returns a single membership in an org.
func (s *Store) MemberByID(ctx context.Context, orgID, userID uuid.UUID) (*OrgMember, error) {
	var m OrgMember
	err := s.Pool.QueryRow(ctx, `
SELECT u.id, u.email, u.name, m.role FROM memberships m
JOIN users u ON u.id = m.user_id
WHERE m.organization_id=$1 AND m.user_id=$2`, orgID, userID).
		Scan(&m.ID, &m.Email, &m.Name, &m.Role)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &m, nil
}

// RenameUser updates a user's display name.
func (s *Store) RenameUser(ctx context.Context, userID uuid.UUID, name string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE users SET name=$2 WHERE id=$1`, userID, name)
	return err
}

// CreatePasswordlessUser creates (or returns) a user with an unusable random
// password hash, for SSO/SCIM provisioning. Password login is impossible
// until a real password is set via reset.
func (s *Store) CreatePasswordlessUser(ctx context.Context, email, name string) (User, error) {
	var u User
	err := s.Pool.QueryRow(ctx, `SELECT id, email, name FROM users WHERE email=$1`, email).
		Scan(&u.ID, &u.Email, &u.Name)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return User{}, err
	}
	ph, err := HashPassword("scim-disabled-" + uuid.NewString())
	if err != nil {
		return User{}, err
	}
	if name == "" {
		name = email
	}
	err = s.Pool.QueryRow(ctx, `INSERT INTO users(email, name, password_hash) VALUES($1,$2,$3) RETURNING id,email,name`,
		email, name, ph).Scan(&u.ID, &u.Email, &u.Name)
	if err != nil {
		return User{}, err
	}
	return u, nil
}

// SCIMGroup is an IdP-synced group inside one organization.
type SCIMGroup struct {
	ID         uuid.UUID
	OrgID      uuid.UUID
	DisplayName string
	ExternalID string
	Members    []uuid.UUID
}

// ListSCIMGroups returns groups in an org with member ids.
func (s *Store) ListSCIMGroups(ctx context.Context, orgID uuid.UUID) ([]SCIMGroup, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT id, display_name, external_id FROM scim_groups WHERE organization_id=$1 ORDER BY display_name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SCIMGroup
	for rows.Next() {
		var g SCIMGroup
		if err := rows.Scan(&g.ID, &g.DisplayName, &g.ExternalID); err != nil {
			return nil, err
		}
		g.OrgID = orgID
		mrows, err := s.Pool.Query(ctx, `SELECT user_id FROM scim_group_members WHERE group_id=$1`, g.ID)
		if err != nil {
			return nil, err
		}
		for mrows.Next() {
			var uid uuid.UUID
			if err := mrows.Scan(&uid); err != nil {
				mrows.Close()
				return nil, err
			}
			g.Members = append(g.Members, uid)
		}
		mrows.Close()
		out = append(out, g)
	}
	return out, rows.Err()
}

// CreateSCIMGroup creates a group; display names are unique per org.
func (s *Store) CreateSCIMGroup(ctx context.Context, orgID uuid.UUID, displayName, externalID string) (SCIMGroup, error) {
	var g SCIMGroup
	err := s.Pool.QueryRow(ctx, `
INSERT INTO scim_groups(organization_id, display_name, external_id)
VALUES ($1, $2, $3) RETURNING id, display_name, external_id`,
		orgID, displayName, externalID).Scan(&g.ID, &g.DisplayName, &g.ExternalID)
	if err != nil {
		return SCIMGroup{}, err
	}
	g.OrgID = orgID
	return g, nil
}

// GetSCIMGroup returns one group of an org.
func (s *Store) GetSCIMGroup(ctx context.Context, orgID, groupID uuid.UUID) (*SCIMGroup, error) {
	var g SCIMGroup
	err := s.Pool.QueryRow(ctx, `
SELECT id, display_name, external_id FROM scim_groups WHERE id=$1 AND organization_id=$2`,
		groupID, orgID).Scan(&g.ID, &g.DisplayName, &g.ExternalID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	g.OrgID = orgID
	mrows, err := s.Pool.Query(ctx, `SELECT user_id FROM scim_group_members WHERE group_id=$1`, g.ID)
	if err != nil {
		return nil, err
	}
	defer mrows.Close()
	for mrows.Next() {
		var uid uuid.UUID
		if err := mrows.Scan(&uid); err != nil {
			return nil, err
		}
		g.Members = append(g.Members, uid)
	}
	return &g, mrows.Err()
}

// SetSCIMGroupMembers replaces a group's membership. Every id must be a
// member of the org; unknown users abort the whole update (no partial sync).
func (s *Store) SetSCIMGroupMembers(ctx context.Context, orgID, groupID uuid.UUID, userIDs []uuid.UUID) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT true FROM scim_groups WHERE id=$1 AND organization_id=$2`, groupID, orgID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	for _, uid := range userIDs {
		var m bool
		if err := tx.QueryRow(ctx, `SELECT true FROM memberships WHERE organization_id=$1 AND user_id=$2`, orgID, uid).Scan(&m); err != nil {
			return fmt.Errorf("user %s is not a member of the org: %w", uid, ErrNotFound)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM scim_group_members WHERE group_id=$1`, groupID); err != nil {
		return err
	}
	for _, uid := range userIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO scim_group_members(group_id, user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, groupID, uid); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// DeleteSCIMGroup removes a group (memberships cascade).
func (s *Store) DeleteSCIMGroup(ctx context.Context, orgID, groupID uuid.UUID) error {
	ct, err := s.Pool.Exec(ctx, `DELETE FROM scim_groups WHERE id=$1 AND organization_id=$2`, groupID, orgID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchSession records activity metadata (best-effort; failures are ignored
// by callers so auth never depends on a metadata write).
func (s *Store) TouchSession(ctx context.Context, sessionID uuid.UUID, ip, userAgent string) error {
	if ip == "" && userAgent == "" {
		_, err := s.Pool.Exec(ctx, `UPDATE sessions SET last_used_at=now() WHERE id=$1 AND revoked_at IS NULL`, sessionID)
		return err
	}
	_, err := s.Pool.Exec(ctx, `UPDATE sessions SET last_used_at=now(), ip=$2::inet, user_agent=$3 WHERE id=$1 AND revoked_at IS NULL`, sessionID, nullIfEmpty(ip), userAgent)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// UserIDByEmail resolves a user id by email for flows that must not
// distinguish unknown addresses (the caller answers success regardless).
func (s *Store) UserIDByEmail(ctx context.Context, email string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.Pool.QueryRow(ctx, `SELECT id FROM users WHERE email=$1`, email).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, err
	}
	return id, nil
}

// IssueEmailCode creates a one-time email MFA code (10-minute lifetime)
// and returns the raw code to email. Only the hash is stored; consuming
// deletes the row so codes are single-use.
func (s *Store) IssueEmailCode(ctx context.Context, userID uuid.UUID) (string, error) {
	code, err := auth.GenerateMfaCode()
	if err != nil {
		return "", err
	}
	_, err = s.Pool.Exec(ctx, `
INSERT INTO mfa_email_codes(user_id, code_hash, expires_at)
VALUES ($1, $2, now() + interval '10 minutes')
ON CONFLICT (user_id) DO UPDATE SET code_hash=$2, expires_at=now() + interval '10 minutes'`,
		userID, hashToken(code))
	if err != nil {
		return "", err
	}
	return code, nil
}

// ConsumeEmailCode verifies a one-time email code and deletes it.
// Wrong, expired, or already-used codes all return ErrInvalidResetToken
// (indistinguishable by design).
func (s *Store) ConsumeEmailCode(ctx context.Context, userID uuid.UUID, code string) error {
	var stored string
	var expiresAt time.Time
	err := s.Pool.QueryRow(ctx, `
SELECT code_hash, expires_at FROM mfa_email_codes WHERE user_id=$1`, userID).
		Scan(&stored, &expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidResetToken
		}
		return err
	}
	raw, err := hex.DecodeString(stored)
	if err != nil {
		return ErrInvalidResetToken
	}
	if time.Now().After(expiresAt) || !auth.Equal(raw, auth.Hash(code)) {
		return ErrInvalidResetToken
	}
	_, err = s.Pool.Exec(ctx, `DELETE FROM mfa_email_codes WHERE user_id=$1`, userID)
	return err
}

// EnableMFA enables MFA for a user.
func (s *Store) EnableMFA(ctx context.Context, userID uuid.UUID, secret string) (bool, error) {
	ct, err := s.Pool.Exec(ctx, `
INSERT INTO mfa_secrets(user_id, secret, is_enabled, created_at)
VALUES ($1, $2, true, now())
ON CONFLICT (user_id) DO UPDATE SET secret=$2, is_enabled=true, updated_at=now()`,
		userID, secret)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

// DisableMFA disables MFA for a user.
func (s *Store) DisableMFA(ctx context.Context, userID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `
UPDATE mfa_secrets SET is_enabled=false, updated_at=now() WHERE user_id=$1`, userID)
	return err
}

// IsMFAEnabled checks if MFA is enabled for a user.
func (s *Store) IsMFAEnabled(ctx context.Context, userID uuid.UUID) (bool, error) {
	var enabled bool
	err := s.Pool.QueryRow(ctx, `
SELECT is_enabled FROM mfa_secrets WHERE user_id=$1`, userID).Scan(&enabled)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return enabled, nil
}

// GetMfaSecret returns the TOTP secret for a user.
func (s *Store) GetMfaSecret(ctx context.Context, userID uuid.UUID) (string, error) {
	var secret string
	err := s.Pool.QueryRow(ctx, `
SELECT secret FROM mfa_secrets WHERE user_id=$1`, userID).Scan(&secret)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errors.New("store: MFA not enabled for user")
		}
		return "", err
	}
	return secret, nil
}
