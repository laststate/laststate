package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ProjectSettings struct {
	ProjectID            uuid.UUID `json:"project_id"`
	RetentionEventsDays  int       `json:"retention_events_days"`
	RetentionHealthDays  int       `json:"retention_health_days"`
	RetentionLogsDays    int       `json:"retention_logs_days"`
	RetentionMetricsDays int       `json:"retention_metrics_days"`
	AnalyzerVersionMin   int       `json:"analyzer_version_min"`
}

type DeadJob struct {
	ID        uuid.UUID       `json:"id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Attempts  int             `json:"attempts"`
	Error     string          `json:"last_error"`
	ProjectID *uuid.UUID      `json:"project_id,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

func (s *Store) GetOrCreateSettings(ctx context.Context, projectID uuid.UUID) (ProjectSettings, error) {
	var ps ProjectSettings
	err := s.Pool.QueryRow(ctx, `
INSERT INTO project_settings(project_id) VALUES($1)
ON CONFLICT (project_id) DO UPDATE SET project_id=EXCLUDED.project_id
RETURNING project_id, retention_events_days, retention_health_days, retention_logs_days, retention_metrics_days, analyzer_version_min`,
		projectID).Scan(&ps.ProjectID, &ps.RetentionEventsDays, &ps.RetentionHealthDays, &ps.RetentionLogsDays, &ps.RetentionMetricsDays, &ps.AnalyzerVersionMin)
	return ps, err
}

func (s *Store) UpdateSettings(ctx context.Context, projectID uuid.UUID, events, health, logs, metrics, analyzerMin int) (ProjectSettings, error) {
	var ps ProjectSettings
	err := s.Pool.QueryRow(ctx, `
INSERT INTO project_settings(project_id, retention_events_days, retention_health_days, retention_logs_days, retention_metrics_days, analyzer_version_min, updated_at)
VALUES($1,$2,$3,$4,$5,$6,now())
ON CONFLICT (project_id) DO UPDATE SET
  retention_events_days=EXCLUDED.retention_events_days,
  retention_health_days=EXCLUDED.retention_health_days,
  retention_logs_days=EXCLUDED.retention_logs_days,
  retention_metrics_days=EXCLUDED.retention_metrics_days,
  analyzer_version_min=EXCLUDED.analyzer_version_min,
  updated_at=now()
RETURNING project_id, retention_events_days, retention_health_days, retention_logs_days, retention_metrics_days, analyzer_version_min`,
		projectID, events, health, logs, metrics, analyzerMin).
		Scan(&ps.ProjectID, &ps.RetentionEventsDays, &ps.RetentionHealthDays, &ps.RetentionLogsDays, &ps.RetentionMetricsDays, &ps.AnalyzerVersionMin)
	return ps, err
}

func (s *Store) LogAlertSkip(ctx context.Context, projectID uuid.UUID, issueID, ruleID *uuid.UUID, kind, reason, detail string) {
	_, _ = s.Pool.Exec(ctx, `
INSERT INTO alert_skip_log(project_id,issue_id,rule_id,kind,reason,detail) VALUES($1,$2,$3,$4,$5,$6)`,
		projectID, issueID, ruleID, kind, reason, detail)
}

func (s *Store) ListAlertSkips(ctx context.Context, projectID uuid.UUID, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id, kind, reason, detail, created_at, issue_id, rule_id
FROM alert_skip_log WHERE project_id=$1 ORDER BY created_at DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var kind, reason, detail string
		var ts time.Time
		var issueID, ruleID *uuid.UUID
		if err := rows.Scan(&id, &kind, &reason, &detail, &ts, &issueID, &ruleID); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "kind": kind, "reason": reason, "detail": detail, "created_at": ts,
			"issue_id": issueID, "rule_id": ruleID,
		})
	}
	return out, rows.Err()
}

func (s *Store) ListDeadJobs(ctx context.Context, projectID *uuid.UUID, limit int) ([]DeadJob, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `
SELECT id, type, payload, attempts, last_error, project_id, created_at
FROM jobs WHERE status='dead'`
	args := []any{}
	if projectID != nil {
		q += ` AND (project_id=$1 OR project_id IS NULL)`
		args = append(args, *projectID)
	}
	q += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit)
	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeadJob
	for rows.Next() {
		var j DeadJob
		if err := rows.Scan(&j.ID, &j.Type, &j.Payload, &j.Attempts, &j.Error, &j.ProjectID, &j.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) RequeueDeadJob(ctx context.Context, jobID uuid.UUID, actor *uuid.UUID) error {
	ct, err := s.Pool.Exec(ctx, `
UPDATE jobs SET status='ready', available_at=now(), last_error='', lease_token=NULL, leased_until=NULL
WHERE id=$1 AND status='dead'`, jobID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	_, _ = s.Pool.Exec(ctx, `INSERT INTO dead_letter_actions(job_id,action,actor_user_id) VALUES($1,'requeue',$2)`, jobID, actor)
	return nil
}

func (s *Store) DiscardDeadJob(ctx context.Context, jobID uuid.UUID, actor *uuid.UUID) error {
	_, _ = s.Pool.Exec(ctx, `INSERT INTO dead_letter_actions(job_id,action,actor_user_id) VALUES($1,'discard',$2)`, jobID, actor)
	ct, err := s.Pool.Exec(ctx, `DELETE FROM jobs WHERE id=$1 AND status='dead'`, jobID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RevokeSession(ctx context.Context, sessionID, userID uuid.UUID) error {
	ct, err := s.Pool.Exec(ctx, `
UPDATE sessions SET revoked_at=now() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, sessionID, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RevokeAllSessions(ctx context.Context, userID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID)
	return err
}

// RevokeAllOtherSessions revokes every active session for a user EXCEPT the
// supplied currentSessionID. Used by /api/me/sessions/revoke-others.
func (s *Store) RevokeAllOtherSessions(ctx context.Context, userID, currentSessionID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND id<>$2 AND revoked_at IS NULL`, userID, currentSessionID)
	return err
}

// SessionInfo is one row of the /api/me/sessions listing.
type SessionInfo struct {
	ID         uuid.UUID  `json:"id"`
	Prefix     string     `json:"prefix"`
	IP         string     `json:"ip,omitempty"`
	UserAgent  string     `json:"user_agent,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

// ListSessions returns active and recent sessions for a user. Active first.
func (s *Store) ListSessions(ctx context.Context, userID uuid.UUID, limit int) ([]SessionInfo, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id, prefix, COALESCE(host(ip),''), COALESCE(user_agent,''), created_at, last_used_at, expires_at, revoked_at
FROM sessions WHERE user_id=$1
ORDER BY revoked_at NULLS FIRST, last_used_at DESC NULLS LAST
LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionInfo
	for rows.Next() {
		var si SessionInfo
		var ip, ua string
		var lastUsed, revoked *time.Time
		if err := rows.Scan(&si.ID, &si.Prefix, &ip, &ua, &si.CreatedAt, &lastUsed, &si.ExpiresAt, &revoked); err != nil {
			return nil, err
		}
		si.IP = ip
		si.UserAgent = ua
		si.LastUsedAt = lastUsed
		si.RevokedAt = revoked
		out = append(out, si)
	}
	return out, rows.Err()
}

// CreateOrgToken persists a new API token scoped to an organization (no project
// required). Returns the public prefix and id used for revocation.
func (s *Store) CreateOrgToken(ctx context.Context, orgID uuid.UUID, name, prefix string, hash []byte, scopes []string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.Pool.QueryRow(ctx, `
INSERT INTO tokens(organization_id, name, prefix, hash, scopes)
VALUES ($1,$2,$3,$4,$5) RETURNING id`, orgID, name, prefix, hash, scopes).Scan(&id)
	return id, err
}

// FTSSearch uses postgres full-text search when available.
func (s *Store) FTSSearch(ctx context.Context, projectID uuid.UUID, q string, limit int) (map[string]any, error) {
	if limit <= 0 {
		limit = 20
	}
	if q == "" {
		return map[string]any{"issues": []any{}, "events": []any{}}, nil
	}
	// Prefer FTS; fallback to ILIKE via GlobalSearchExtended
	issues := []map[string]any{}
	rows, err := s.Pool.Query(ctx, `
SELECT id, title, status, severity, ts_rank(search_tsv, plainto_tsquery('simple', $2)) AS rank
FROM issues WHERE project_id=$1 AND search_tsv @@ plainto_tsquery('simple', $2)
ORDER BY rank DESC LIMIT $3`, projectID, q, limit)
	if err != nil {
		return s.GlobalSearchExtended(ctx, projectID, q, limit)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var title, status, sev string
		var rank float64
		if rows.Scan(&id, &title, &status, &sev, &rank) == nil {
			issues = append(issues, map[string]any{"id": id, "title": title, "status": status, "severity": sev, "rank": rank})
		}
	}
	events := []map[string]any{}
	erows, err := s.Pool.Query(ctx, `
SELECT id, event_id, severity, state, ts_rank(search_tsv, plainto_tsquery('simple', $2)) AS rank
FROM events WHERE project_id=$1 AND search_tsv @@ plainto_tsquery('simple', $2)
ORDER BY rank DESC LIMIT $3`, projectID, q, limit)
	if err == nil {
		defer erows.Close()
		for erows.Next() {
			var id uuid.UUID
			var eid, sev, st string
			var rank float64
			if erows.Scan(&id, &eid, &sev, &st, &rank) == nil {
				events = append(events, map[string]any{"id": id, "event_id": eid, "severity": sev, "state": st, "rank": rank})
			}
		}
	}
	base, _ := s.GlobalSearchExtended(ctx, projectID, q, limit)
	base["issues_fts"] = issues
	base["events_fts"] = events
	if len(issues) > 0 {
		base["issues"] = issues
	}
	if len(events) > 0 {
		base["events"] = events
	}
	return base, nil
}

func (s *Store) CompareReleases(ctx context.Context, projectID, a, b uuid.UUID) (map[string]any, error) {
	sa, err := s.CrashFreeRate(ctx, projectID, a)
	if err != nil {
		return nil, err
	}
	sb, err := s.CrashFreeRate(ctx, projectID, b)
	if err != nil {
		return nil, err
	}
	var issuesA, issuesB int64
	_ = s.Pool.QueryRow(ctx, `
SELECT COUNT(DISTINCT issue_id) FROM events WHERE project_id=$1 AND release_id=$2 AND issue_id IS NOT NULL`, projectID, a).Scan(&issuesA)
	_ = s.Pool.QueryRow(ctx, `
SELECT COUNT(DISTINCT issue_id) FROM events WHERE project_id=$1 AND release_id=$2 AND issue_id IS NOT NULL`, projectID, b).Scan(&issuesB)
	return map[string]any{
		"release_a": sa, "release_b": sb,
		"distinct_issues_a": issuesA, "distinct_issues_b": issuesB,
	}, nil
}

func (s *Store) EnsureLabel(ctx context.Context, projectID uuid.UUID, name, color string) (uuid.UUID, error) {
	if color == "" {
		color = "#666666"
	}
	var id uuid.UUID
	err := s.Pool.QueryRow(ctx, `
INSERT INTO issue_labels(project_id,name,color) VALUES($1,$2,$3)
ON CONFLICT (project_id, name) DO UPDATE SET color=EXCLUDED.color
RETURNING id`, projectID, name, color).Scan(&id)
	return id, err
}

func (s *Store) ListLabels(ctx context.Context, projectID uuid.UUID) ([]map[string]any, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, name, color FROM issue_labels WHERE project_id=$1 ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var name, color string
		if err := rows.Scan(&id, &name, &color); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "name": name, "color": color})
	}
	return out, rows.Err()
}

func (s *Store) LinkLabel(ctx context.Context, issueID, labelID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `
INSERT INTO issue_label_links(issue_id,label_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, issueID, labelID)
	return err
}

func (s *Store) PurgeByPipeline(ctx context.Context, projectID uuid.UUID, pipeline string, days, limit int) ([]string, error) {
	if days <= 0 || limit <= 0 {
		return nil, nil
	}
	rows, err := s.Pool.Query(ctx, `
DELETE FROM events WHERE id IN (
  SELECT id FROM events WHERE project_id=$1 AND COALESCE(pipeline,'issue')=$2
    AND received_at < now() - ($3 || ' days')::interval
  ORDER BY received_at ASC LIMIT $4
) RETURNING raw_object_key`, projectID, pipeline, fmt.Sprintf("%d", days), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			keys = append(keys, k)
		}
	}
	return keys, rows.Err()
}

func (s *Store) ListProjectsAll(ctx context.Context) ([]Project, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id,organization_id,name,slug,description FROM projects ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.OrganizationID, &p.Name, &p.Slug, &p.Description); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) SetProjectMembership(ctx context.Context, projectID, userID uuid.UUID, role string) error {
	_, err := s.Pool.Exec(ctx, `
INSERT INTO project_memberships(project_id,user_id,role) VALUES($1,$2,$3)
ON CONFLICT (project_id, user_id) DO UPDATE SET role=EXCLUDED.role`, projectID, userID, role)
	return err
}

func (s *Store) ProjectRole(ctx context.Context, userID, projectID uuid.UUID) (string, error) {
	var role string
	err := s.Pool.QueryRow(ctx, `
SELECT role FROM project_memberships WHERE user_id=$1 AND project_id=$2`, userID, projectID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		// fall back to org membership
		ok, orgRole, err2 := s.UserCanAccessProject(ctx, userID, projectID)
		if err2 != nil {
			return "", err2
		}
		if !ok {
			return "", ErrNotFound
		}
		return orgRole, nil
	}
	return role, err
}

func (s *Store) ListBootSessions(ctx context.Context, projectID uuid.UUID, deviceID *uuid.UUID, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id, boot_id, device_id, started_at, last_event_at, event_count FROM boot_sessions WHERE project_id=$1`
	args := []any{projectID}
	if deviceID != nil {
		q += ` AND device_id=$2`
		args = append(args, *deviceID)
		q += fmt.Sprintf(` ORDER BY last_event_at DESC LIMIT $%d`, 3)
		args = append(args, limit)
	} else {
		q += ` ORDER BY last_event_at DESC LIMIT $2`
		args = append(args, limit)
	}
	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var boot string
		var dev *uuid.UUID
		var start, last time.Time
		var n int64
		if err := rows.Scan(&id, &boot, &dev, &start, &last, &n); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "boot_id": boot, "device_id": dev, "started_at": start, "last_event_at": last, "event_count": n})
	}
	return out, rows.Err()
}
