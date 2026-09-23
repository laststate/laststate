package store

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/laststate/trace/internal/auth"
)

type User struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email"`
	Name  string    `json:"name"`
}

type Session struct {
	UserID         uuid.UUID
	Email          string
	Name           string
	OrganizationID uuid.UUID
	Role           string
	SessionID      uuid.UUID
	LastUsedAt     *time.Time
}

type AuditEntry struct {
	ID        uuid.UUID       `json:"id"`
	Action    string          `json:"action"`
	Target    string          `json:"target_type"`
	TargetID  string          `json:"target_id"`
	Metadata  json.RawMessage `json:"metadata"`
	CreatedAt time.Time       `json:"created_at"`
	Actor     string          `json:"actor,omitempty"`
}

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// GeneratePassword creates a strong random password.
func GeneratePassword() string {
	var b [18]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// EnsureAdmin creates admin user if none exist (idempotent).
// If password is empty or insecure defaults, a random password is generated.
func (s *Store) EnsureAdmin(ctx context.Context, email, password, name string) (User, string, error) {
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return User{}, "", err
	}
	if n > 0 {
		return User{}, "", fmtAlready()
	}
	if password == "" || password == "admin" || password == "password" || password == "admin123" {
		password = GeneratePassword()
	}
	if err := CheckPasswordPolicy(password); err != nil {
		return User{}, "", err
	}
	org, err := s.firstOrg(ctx)
	if err != nil {
		return User{}, "", err
	}
	ph, err := HashPassword(password)
	if err != nil {
		return User{}, "", err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return User{}, "", err
	}
	defer tx.Rollback(ctx)
	var u User
	if err := tx.QueryRow(ctx, `INSERT INTO users(email,name,password_hash) VALUES($1,$2,$3) RETURNING id,email,name`,
		email, name, ph).Scan(&u.ID, &u.Email, &u.Name); err != nil {
		return User{}, "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')`, org.ID, u.ID); err != nil {
		return User{}, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, "", err
	}
	return u, password, nil
}

func fmtAlready() error { return errors.New("admin already exists") }

func (s *Store) firstOrg(ctx context.Context) (Org, error) {
	var o Org
	err := s.Pool.QueryRow(ctx, `SELECT id,name,slug FROM organizations ORDER BY created_at ASC LIMIT 1`).
		Scan(&o.ID, &o.Name, &o.Slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return Org{}, ErrNotFound
	}
	return o, err
}

// CheckPasswordPolicy enforces length bounds. The 128-byte ceiling exists
// because bcrypt silently truncates past 72 bytes — without it, "password…X"
// and "password…Y" would hash identically.
func CheckPasswordPolicy(password string) error {
	if len(password) < 12 {
		return errors.New("password must be at least 12 characters")
	}
	if len(password) > 128 {
		return errors.New("password must be at most 128 characters")
	}
	lower := strings.ToLower(password)
	for _, banned := range breachedPasswords {
		if lower == banned {
			return errors.New("password is too common; choose a less predictable one")
		}
	}
	if breachedViaHIBP(password) {
		return errors.New("password appeared in known breaches; choose another one")
	}
	return nil
}

// breachedViaHIBP checks the password against Have I Been Pwned via the
// k-anonymity range API (only the first 5 hex chars of the SHA-1 leave the
// process). Opt-in via HIBP_CHECK=true; any transport failure or timeout
// fails OPEN (local blocklist above still applies) so onboarding never
// depends on a third party.
func breachedViaHIBP(password string) bool {
	if os.Getenv("HIBP_CHECK") != "true" {
		return false
	}
	sum := sha1.Sum([]byte(password))
	hexSum := strings.ToUpper(hex.EncodeToString(sum[:]))
	prefix, suffix := hexSum[:5], hexSum[5:]
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.pwnedpasswords.com/range/"+prefix, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "laststate-trace-pwcheck")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(body), "\n") {
		if hash, _, ok := strings.Cut(strings.TrimSpace(line), ":"); ok {
			if strings.EqualFold(hash, suffix) {
				return true
			}
		}
	}
	return false
}

// breachedPasswords is a minimal blocklist of the most-abused passwords.
// Exact match only (no substring games that would annoy legitimate users).
var breachedPasswords = []string{
	"password1234", "password12345", "password123456", "qwerty123456",
	"123456789012", "123456789013", "letmein12345", "welcome12345",
	"admin1234567", "changeme1234", "changeme12345", "test12345678",
	"p@ssw0rd1234", "p@ssw0rd12345", "iloveyou1234", "dragon123456",
	"monkey123456", "football1234", "baseball1234", "superman1234",
	"trustno1binding", "correcthorsebatterystaple",
}

// CreateUser creates a password user and membership in org.
func (s *Store) CreateUser(ctx context.Context, orgID uuid.UUID, email, password, name, role string) (User, error) {
	if role == "" {
		role = "developer"
	}
	if err := CheckPasswordPolicy(password); err != nil {
		return User{}, err
	}
	ph, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)
	var u User
	if err := tx.QueryRow(ctx, `INSERT INTO users(email,name,password_hash) VALUES($1,$2,$3) RETURNING id,email,name`,
		email, name, ph).Scan(&u.ID, &u.Email, &u.Name); err != nil {
		return User{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,$3)`, orgID, u.ID, role); err != nil {
		return User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, err
	}
	return u, nil
}

// UserIsMemberOfOrg reports whether the given user has any membership in the
// given organization. Used by tenant middleware to validate X-Org-ID header.
func (s *Store) UserIsMemberOfOrg(ctx context.Context, userID, orgID uuid.UUID) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM memberships WHERE user_id=$1 AND organization_id=$2)`,
		userID, orgID).Scan(&ok)
	if err != nil {
		return false, err
	}
	return ok, nil
}

// AcceptInvite redeems invite token and adds membership.
func (s *Store) AcceptInvite(ctx context.Context, tokenSecret string, password, name string) (User, Session, string, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT id, organization_id, email, role, token_hash, expires_at
FROM organization_invites WHERE accepted_at IS NULL AND expires_at > now()`)
	if err != nil {
		return User{}, Session{}, "", err
	}
	defer rows.Close()
	var invID, orgID uuid.UUID
	var email, role string
	var exp time.Time
	found := false
	for rows.Next() {
		var h []byte
		if err := rows.Scan(&invID, &orgID, &email, &role, &h, &exp); err != nil {
			return User{}, Session{}, "", err
		}
		if auth.Equal(h, auth.Hash(tokenSecret)) {
			found = true
			break
		}
	}
	if !found {
		return User{}, Session{}, "", ErrNotFound
	}
	if name == "" {
		name = email
	}
	u, err := s.CreateUser(ctx, orgID, email, password, name, role)
	if err != nil {
		var existing User
		var ph string
		if err2 := s.Pool.QueryRow(ctx, `SELECT id,email,name,password_hash FROM users WHERE email=$1`, email).
			Scan(&existing.ID, &existing.Email, &existing.Name, &ph); err2 != nil {
			return User{}, Session{}, "", err
		}
		u = existing
		_ = s.AddMember(ctx, orgID, u.ID, role)
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE organization_invites SET accepted_at=now() WHERE id=$1`, invID)
	sess, secret, err := s.MintSession(ctx, u, orgID, role)
	return u, sess, secret, err
}

// ListTokens returns project tokens without secrets.
func (s *Store) ListTokens(ctx context.Context, projectID uuid.UUID) ([]Token, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT id, project_id, name, prefix, scopes FROM tokens
WHERE project_id=$1 AND revoked_at IS NULL ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.Name, &t.Prefix, &t.Scopes); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) RevokeToken(ctx context.Context, projectID, tokenID uuid.UUID) error {
	ct, err := s.Pool.Exec(ctx, `UPDATE tokens SET revoked_at=now() WHERE id=$1 AND project_id=$2 AND revoked_at IS NULL`, tokenID, projectID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Login(ctx context.Context, email, password string) (Session, string, error) {
	var u User
	var hash string
	err := s.Pool.QueryRow(ctx, `SELECT id,email,name,password_hash FROM users WHERE email=$1`, email).
		Scan(&u.ID, &u.Email, &u.Name, &hash)
	if errors.Is(err, pgx.ErrNoRows) || !CheckPassword(hash, password) {
		return Session{}, "", ErrNotFound
	}
	if err != nil {
		return Session{}, "", err
	}
	var orgID uuid.UUID
	var role string
	err = s.Pool.QueryRow(ctx, `SELECT organization_id, role FROM memberships WHERE user_id=$1 ORDER BY created_at ASC LIMIT 1`, u.ID).
		Scan(&orgID, &role)
	if err != nil {
		return Session{}, "", err
	}
	return s.MintSession(ctx, u, orgID, role)
}

// MintSession creates a bearer session token for a user.
func (s *Store) MintSession(ctx context.Context, u User, orgID uuid.UUID, role string) (Session, string, error) {
	secret, prefix, th, err := auth.Mint("lst_sess")
	if err != nil {
		return Session{}, "", err
	}
	var sid uuid.UUID
	err = s.Pool.QueryRow(ctx, `
INSERT INTO sessions(user_id,token_hash,prefix,expires_at,last_used_at) VALUES($1,$2,$3,now() + interval '7 days',now())
RETURNING id`, u.ID, th, prefix).Scan(&sid)
	if err != nil {
		return Session{}, "", err
	}
	return Session{UserID: u.ID, Email: u.Email, Name: u.Name, OrganizationID: orgID, Role: role, SessionID: sid}, secret, nil
}

func (s *Store) AuthSession(ctx context.Context, secret string) (Session, error) {
	prefix := auth.PrefixOf(secret)
	var sid, uid, orgID uuid.UUID
	var hash []byte
	var email, name, role string
	var lastUsed *time.Time
	err := s.Pool.QueryRow(ctx, `
SELECT s.id, s.user_id, s.token_hash, u.email, u.name, m.organization_id, m.role, s.last_used_at
FROM sessions s
JOIN users u ON u.id=s.user_id
JOIN memberships m ON m.user_id=u.id
WHERE s.prefix=$1 AND s.revoked_at IS NULL AND s.expires_at > now()
ORDER BY m.created_at ASC LIMIT 1`, prefix).
		Scan(&sid, &uid, &hash, &email, &name, &orgID, &role, &lastUsed)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	if !auth.Equal(hash, auth.Hash(secret)) {
		return Session{}, ErrNotFound
	}
	return Session{UserID: uid, Email: email, Name: name, OrganizationID: orgID, Role: role, SessionID: sid, LastUsedAt: lastUsed}, nil
}

// roleRanks is the canonical role hierarchy. The api layer resolves ranks
// through RoleRank so both layers share one source of truth.
var roleRanks = map[string]int{
	"viewer": 1, "developer": 2, "maintainer": 3, "admin": 4, "owner": 5,
	"billing": 1,
}

// RoleRank returns the rank for a role (ok=false for unknown roles).
func RoleRank(role string) (int, bool) {
	rank, ok := roleRanks[role]
	return rank, ok
}

func roleAtLeast(role, need string) bool {
	rank, _ := RoleRank(role)
	needRank, _ := RoleRank(need)
	return rank >= needRank
}

func (s Session) Can(need string) bool { return roleAtLeast(s.Role, need) }

func (s *Store) Audit(ctx context.Context, actorUser, actorToken *uuid.UUID, orgID, projectID *uuid.UUID, action, targetType, targetID, ip, ua string, meta any) {
	var raw json.RawMessage = json.RawMessage(`{}`)
	if meta != nil {
		if b, err := json.Marshal(meta); err == nil {
			raw = b
		}
	}
	// Hash chain: prev_hash from last row, entry_hash = sha256(prev|action|target|meta|ts)
	prev := ""
	_ = s.Pool.QueryRow(ctx, `SELECT COALESCE(entry_hash,'') FROM audit_logs ORDER BY created_at DESC LIMIT 1`).Scan(&prev)
	sum := sha256Hex(prev + "|" + action + "|" + targetType + "|" + targetID + "|" + string(raw))
	_, err := s.Pool.Exec(ctx, `
INSERT INTO audit_logs(actor_user_id,actor_token_id,organization_id,project_id,action,target_type,target_id,ip,user_agent,metadata,prev_hash,entry_hash,immutable)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,true)`, actorUser, actorToken, orgID, projectID, action, targetType, targetID, ip, ua, raw, prev, sum)
	if err != nil {
		// Fallback without hash columns (pre-migration)
		_, _ = s.Pool.Exec(ctx, `
INSERT INTO audit_logs(actor_user_id,actor_token_id,organization_id,project_id,action,target_type,target_id,ip,user_agent,metadata)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, actorUser, actorToken, orgID, projectID, action, targetType, targetID, ip, ua, raw)
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// RecordNotifyDelivery logs fan-out attempts (best-effort; table may be missing pre-migration).
func (s *Store) RecordNotifyDelivery(ctx context.Context, projectID uuid.UUID, kind, target string, success bool, status, attempts int, errBody string) error {
	if attempts <= 0 {
		attempts = 1
	}
	if len(errBody) > 2000 {
		errBody = errBody[:2000]
	}
	_, err := s.Pool.Exec(ctx, `
INSERT INTO notify_deliveries(project_id,channel_kind,target,success,status_code,attempts,error,body_preview)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, projectID, kind, target, success, status, attempts, errBody, errBody)
	return err
}

// HasScope checks org-level granular scope grant (role_scopes). Falls back to true if table empty/missing.
func (s *Store) HasScope(ctx context.Context, orgID uuid.UUID, role, scope string) bool {
	var n int
	err := s.Pool.QueryRow(ctx, `
SELECT COUNT(*) FROM role_scopes WHERE org_id=$1 AND role=$2 AND scope=$3`, orgID, role, scope).Scan(&n)
	if err != nil {
		return true // pre-migration / no table → don't lock out
	}
	if n > 0 {
		return true
	}
	// if org has any scopes configured, deny missing; if zero scopes, allow (default open)
	var total int
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM role_scopes WHERE org_id=$1`, orgID).Scan(&total)
	return total == 0
}

func (s *Store) ListAudit(ctx context.Context, orgID uuid.UUID, limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows pgx.Rows
	var err error
	if orgID == uuid.Nil {
		rows, err = s.Pool.Query(ctx, `
SELECT a.id, a.action, a.target_type, a.target_id, a.metadata, a.created_at, COALESCE(u.email,'')
FROM audit_logs a LEFT JOIN users u ON u.id=a.actor_user_id
ORDER BY a.created_at DESC LIMIT $1`, limit)
	} else {
		rows, err = s.Pool.Query(ctx, `
SELECT a.id, a.action, a.target_type, a.target_id, a.metadata, a.created_at, COALESCE(u.email,'')
FROM audit_logs a LEFT JOIN users u ON u.id=a.actor_user_id
WHERE a.organization_id=$1
ORDER BY a.created_at DESC LIMIT $2`, orgID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.Action, &e.Target, &e.TargetID, &e.Metadata, &e.CreatedAt, &e.Actor); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) CreateArtifact(ctx context.Context, projectID uuid.UUID, typ, buildID, arch, sha, key string, size int64, status string) (Artifact, error) {
	if status == "" {
		status = "quarantine"
	}
	var a Artifact
	err := s.Pool.QueryRow(ctx, `
INSERT INTO artifacts(project_id,type,build_id,architecture,size,sha256,object_key,status)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT (project_id, sha256) DO UPDATE SET
  build_id=EXCLUDED.build_id, architecture=EXCLUDED.architecture, status=EXCLUDED.status
RETURNING id,project_id,build_id,sha256,object_key,architecture,status`,
		projectID, typ, buildID, arch, size, sha, key, status).
		Scan(&a.ID, &a.ProjectID, &a.BuildID, &a.SHA256, &a.ObjectKey, &a.Architecture, &a.Status)
	return a, err
}

func (s *Store) PromoteArtifact(ctx context.Context, projectID, id uuid.UUID) (Artifact, error) {
	var a Artifact
	err := s.Pool.QueryRow(ctx, `
UPDATE artifacts SET status='ready', quarantine_reason='' WHERE id=$1 AND project_id=$2
RETURNING id,project_id,build_id,sha256,object_key,architecture,status`, id, projectID).
		Scan(&a.ID, &a.ProjectID, &a.BuildID, &a.SHA256, &a.ObjectKey, &a.Architecture, &a.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Artifact{}, ErrNotFound
	}
	return a, err
}

func (s *Store) QuarantineArtifact(ctx context.Context, projectID, id uuid.UUID, reason string) error {
	ct, err := s.Pool.Exec(ctx, `
UPDATE artifacts SET status='quarantine', quarantine_reason=$3 WHERE id=$1 AND project_id=$2`, id, projectID, reason)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListArtifacts(ctx context.Context, projectID uuid.UUID, limit int) ([]Artifact, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,project_id,build_id,sha256,object_key,architecture,status FROM artifacts
WHERE project_id=$1 ORDER BY created_at DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		var a Artifact
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.BuildID, &a.SHA256, &a.ObjectKey, &a.Architecture, &a.Status); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetArtifact(ctx context.Context, id uuid.UUID) (Artifact, error) {
	return s.GetArtifactInProject(ctx, uuid.Nil, id)
}

func (s *Store) GetArtifactInProject(ctx context.Context, projectID, id uuid.UUID) (Artifact, error) {
	var a Artifact
	q := `SELECT id,project_id,build_id,sha256,object_key,architecture,status FROM artifacts WHERE id=$1`
	args := []any{id}
	if projectID != uuid.Nil {
		q += ` AND project_id=$2`
		args = append(args, projectID)
	}
	err := s.Pool.QueryRow(ctx, q, args...).Scan(&a.ID, &a.ProjectID, &a.BuildID, &a.SHA256, &a.ObjectKey, &a.Architecture, &a.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Artifact{}, ErrNotFound
	}
	return a, err
}

func (s *Store) ListReleases(ctx context.Context, projectID uuid.UUID, limit int) ([]Release, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,project_id,version,build_id,status,COALESCE(git_commit,''),COALESCE(toolchain,'') FROM releases WHERE project_id=$1 ORDER BY introduced_at DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Release
	for rows.Next() {
		var r Release
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Version, &r.BuildID, &r.Status, &r.GitCommit, &r.Toolchain); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetRelease(ctx context.Context, id uuid.UUID) (Release, error) {
	return s.GetReleaseInProject(ctx, uuid.Nil, id)
}

func (s *Store) GetReleaseInProject(ctx context.Context, projectID, id uuid.UUID) (Release, error) {
	var r Release
	q := `SELECT id,project_id,version,build_id,status,COALESCE(git_commit,''),COALESCE(toolchain,'') FROM releases WHERE id=$1`
	args := []any{id}
	if projectID != uuid.Nil {
		q += ` AND project_id=$2`
		args = append(args, projectID)
	}
	err := s.Pool.QueryRow(ctx, q, args...).Scan(&r.ID, &r.ProjectID, &r.Version, &r.BuildID, &r.Status, &r.GitCommit, &r.Toolchain)
	if errors.Is(err, pgx.ErrNoRows) {
		return Release{}, ErrNotFound
	}
	return r, err
}

func (s *Store) GetDevice(ctx context.Context, id uuid.UUID) (Device, error) {
	return s.GetDeviceInProject(ctx, uuid.Nil, id)
}

func (s *Store) GetDeviceInProject(ctx context.Context, projectID, id uuid.UUID) (Device, error) {
	var d Device
	q := `
SELECT id,project_id,device_id,product,hardware_revision,firmware_version,build_id,status,first_seen,last_seen
FROM devices WHERE id=$1`
	args := []any{id}
	if projectID != uuid.Nil {
		q += ` AND project_id=$2`
		args = append(args, projectID)
	}
	err := s.Pool.QueryRow(ctx, q, args...).Scan(&d.ID, &d.ProjectID, &d.DeviceID, &d.Product, &d.HardwareRevision, &d.FirmwareVersion, &d.BuildID, &d.Status, &d.FirstSeen, &d.LastSeen)
	if errors.Is(err, pgx.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	return d, err
}

func (s *Store) ListEventsByIssue(ctx context.Context, issueID uuid.UUID, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,event_id,project_id,device_id,release_id,artifact_id,issue_id,type,severity,architecture,sequence,source_event_id,
raw_object_key,raw_hash,size_bytes,state,fingerprint,decoded,analysis,frames,received_at,processed_at,duplicate_count,COALESCE(pipeline,'issue'),COALESCE(process_version,0)
FROM events WHERE issue_id=$1 ORDER BY received_at DESC LIMIT $2`, issueID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *Store) ListEventsByDevice(ctx context.Context, deviceID uuid.UUID, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,event_id,project_id,device_id,release_id,artifact_id,issue_id,type,severity,architecture,sequence,source_event_id,
raw_object_key,raw_hash,size_bytes,state,fingerprint,decoded,analysis,frames,received_at,processed_at,duplicate_count,COALESCE(pipeline,'issue'),COALESCE(process_version,0)
FROM events WHERE device_id=$1 ORDER BY received_at DESC LIMIT $2`, deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *Store) UpdateIssueStatus(ctx context.Context, projectID, id uuid.UUID, status string) (Issue, error) {
	var i Issue
	resolved := status == "resolved"
	err := s.Pool.QueryRow(ctx, `
UPDATE issues SET status=$3,
  resolved_at=CASE WHEN $4 THEN now() ELSE NULL END
WHERE id=$1 AND project_id=$2
RETURNING id,project_id,fingerprint,title,status,severity,event_count,affected_devices,first_seen,last_seen,probable_cause,resolved_at,COALESCE(regression_count,0)`,
		id, projectID, status, resolved).Scan(&i.ID, &i.ProjectID, &i.Fingerprint, &i.Title, &i.Status, &i.Severity, &i.EventCount, &i.AffectedDevices, &i.FirstSeen, &i.LastSeen, &i.ProbableCause, &i.ResolvedAt, &i.RegressionCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return Issue{}, ErrNotFound
	}
	if err == nil {
		_, _ = s.Pool.Exec(ctx, `INSERT INTO issue_activity(issue_id,project_id,action,body) VALUES($1,$2,'status',$3)`, id, projectID, status)
	}
	return i, err
}

func (s *Store) AddIssueComment(ctx context.Context, projectID, issueID uuid.UUID, author *uuid.UUID, body string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.Pool.QueryRow(ctx, `
INSERT INTO issue_comments(issue_id,project_id,author_user_id,body) VALUES($1,$2,$3,$4) RETURNING id`,
		issueID, projectID, author, body).Scan(&id)
	if err == nil {
		_, _ = s.Pool.Exec(ctx, `INSERT INTO issue_activity(issue_id,project_id,actor_user_id,action,body) VALUES($1,$2,$3,'comment',$4)`,
			issueID, projectID, author, body)
	}
	return id, err
}

func (s *Store) ListIssueComments(ctx context.Context, issueID uuid.UUID) ([]map[string]any, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT c.id, c.body, c.created_at, COALESCE(u.email,'')
FROM issue_comments c LEFT JOIN users u ON u.id=c.author_user_id
WHERE c.issue_id=$1 ORDER BY c.created_at ASC`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var body, email string
		var ts time.Time
		if err := rows.Scan(&id, &body, &ts, &email); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "body": body, "created_at": ts, "author": email})
	}
	return out, rows.Err()
}

func (s *Store) ListIssueActivity(ctx context.Context, issueID uuid.UUID, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id, action, body, created_at FROM issue_activity WHERE issue_id=$1 ORDER BY created_at DESC LIMIT $2`, issueID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var action, body string
		var ts time.Time
		if err := rows.Scan(&id, &action, &body, &ts); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "action": action, "body": body, "created_at": ts})
	}
	return out, rows.Err()
}

func (s *Store) EventHistory(ctx context.Context, eventID uuid.UUID) ([]map[string]any, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT from_state, to_state, reason, actor, created_at FROM event_state_history
WHERE event_id=$1 ORDER BY created_at ASC`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var from, to, reason, actor string
		var ts time.Time
		if err := rows.Scan(&from, &to, &reason, &actor, &ts); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"from": from, "to": to, "reason": reason, "actor": actor, "created_at": ts})
	}
	return out, rows.Err()
}

func (s *Store) CreateProjectToken(ctx context.Context, projectID uuid.UUID, name string, scopes []string) (Token, string, error) {
	secret, prefix, hash, err := auth.Mint("lst_ingest")
	if err != nil {
		return Token{}, "", err
	}
	if len(scopes) == 0 {
		scopes = []string{"event:write", "event:read", "artifact:write"}
	}
	var t Token
	err = s.Pool.QueryRow(ctx, `
INSERT INTO tokens(project_id,name,prefix,hash,scopes) VALUES($1,$2,$3,$4,$5)
RETURNING id,project_id,name,prefix,scopes`, projectID, name, prefix, hash, scopes).
		Scan(&t.ID, &t.ProjectID, &t.Name, &t.Prefix, &t.Scopes)
	return t, secret, err
}

func scanEvents(rows pgx.Rows) ([]Event, error) {
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.EventID, &e.ProjectID, &e.DeviceID, &e.ReleaseID, &e.ArtifactID, &e.IssueID, &e.Type, &e.Severity,
			&e.Architecture, &e.Sequence, &e.SourceEventID, &e.RawObjectKey, &e.RawHash, &e.SizeBytes, &e.State, &e.Fingerprint,
			&e.Decoded, &e.Analysis, &e.Frames, &e.ReceivedAt, &e.ProcessedAt, &e.DuplicateCount, &e.Pipeline, &e.ProcessVersion); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func TokenHasScope(t Token, scope string) bool {
	for _, s := range t.Scopes {
		if s == scope || s == "project:admin" || s == "*" {
			return true
		}
	}
	return false
}
