package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type OncallSchedule struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Timezone string    `json:"timezone"`
	Enabled  bool      `json:"enabled"`
}

type EscalationPolicy struct {
	ID      uuid.UUID       `json:"id"`
	Name    string          `json:"name"`
	Enabled bool            `json:"enabled"`
	Levels  json.RawMessage `json:"levels"`
}

// WhoIsOnCall returns emails currently on shift (local weekday/minute, timezone ignored → UTC).
func (s *Store) WhoIsOnCall(ctx context.Context, projectID uuid.UUID, at time.Time) ([]string, error) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	wd := int(at.Weekday())
	min := at.Hour()*60 + at.Minute()
	rows, err := s.Pool.Query(ctx, `
SELECT DISTINCT sh.user_email
FROM oncall_shifts sh
JOIN oncall_schedules sc ON sc.id = sh.schedule_id
WHERE sc.project_id=$1 AND sc.enabled
  AND sh.weekday=$2 AND sh.start_minute <= $3 AND sh.end_minute > $3`, projectID, wd, min)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		if rows.Scan(&e) == nil {
			out = append(out, e)
		}
	}
	return out, rows.Err()
}

func (s *Store) ListOncallSchedules(ctx context.Context, projectID uuid.UUID) ([]OncallSchedule, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT id,name,timezone,enabled FROM oncall_schedules WHERE project_id=$1 ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OncallSchedule
	for rows.Next() {
		var o OncallSchedule
		if rows.Scan(&o.ID, &o.Name, &o.Timezone, &o.Enabled) == nil {
			out = append(out, o)
		}
	}
	return out, rows.Err()
}

func (s *Store) CreateOncallSchedule(ctx context.Context, projectID uuid.UUID, name, tz string) (OncallSchedule, error) {
	if tz == "" {
		tz = "UTC"
	}
	var o OncallSchedule
	err := s.Pool.QueryRow(ctx, `
INSERT INTO oncall_schedules(project_id,name,timezone) VALUES($1,$2,$3)
RETURNING id,name,timezone,enabled`, projectID, name, tz).Scan(&o.ID, &o.Name, &o.Timezone, &o.Enabled)
	return o, err
}

func (s *Store) AddOncallShift(ctx context.Context, scheduleID uuid.UUID, email string, weekday, startMin, endMin int) error {
	if endMin <= startMin {
		endMin = 1440
	}
	_, err := s.Pool.Exec(ctx, `
INSERT INTO oncall_shifts(schedule_id,user_email,weekday,start_minute,end_minute)
VALUES($1,$2,$3,$4,$5)
ON CONFLICT DO NOTHING`, scheduleID, email, weekday, startMin, endMin)
	return err
}

func (s *Store) ListEscalationPolicies(ctx context.Context, projectID uuid.UUID) ([]EscalationPolicy, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT id,name,enabled,levels FROM escalation_policies WHERE project_id=$1 ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EscalationPolicy
	for rows.Next() {
		var p EscalationPolicy
		if rows.Scan(&p.ID, &p.Name, &p.Enabled, &p.Levels) == nil {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

func (s *Store) CreateEscalationPolicy(ctx context.Context, projectID uuid.UUID, name string, levels any) (EscalationPolicy, error) {
	raw, _ := json.Marshal(levels)
	if len(raw) == 0 {
		raw = []byte("[]")
	}
	var p EscalationPolicy
	err := s.Pool.QueryRow(ctx, `
INSERT INTO escalation_policies(project_id,name,levels) VALUES($1,$2,$3)
RETURNING id,name,enabled,levels`, projectID, name, raw).Scan(&p.ID, &p.Name, &p.Enabled, &p.Levels)
	return p, err
}

func (s *Store) GetEscalationPolicy(ctx context.Context, projectID, id uuid.UUID) (EscalationPolicy, error) {
	var p EscalationPolicy
	err := s.Pool.QueryRow(ctx, `
SELECT id,name,enabled,levels FROM escalation_policies WHERE project_id=$1 AND id=$2`, projectID, id).
		Scan(&p.ID, &p.Name, &p.Enabled, &p.Levels)
	return p, err
}

func (s *Store) LogEscalationFire(ctx context.Context, projectID uuid.UUID, issueID, policyID *uuid.UUID, level int, target string, success bool) {
	_, _ = s.Pool.Exec(ctx, `
INSERT INTO escalation_fires(project_id,issue_id,policy_id,level,target,success)
VALUES($1,$2,$3,$4,$5,$6)`, projectID, issueID, policyID, level, target, success)
}

// UpsertReleaseCommit stores a commit linked to a release (for suspect commits).
func (s *Store) UpsertReleaseCommit(ctx context.Context, projectID, releaseID uuid.UUID, sha, author, message string, at *time.Time) error {
	_, err := s.Pool.Exec(ctx, `
INSERT INTO release_commits(project_id,release_id,sha,author,message,committed_at)
VALUES($1,$2,$3,$4,$5,$6)
ON CONFLICT (release_id, sha) DO UPDATE SET
  author=EXCLUDED.author, message=EXCLUDED.message, committed_at=COALESCE(EXCLUDED.committed_at, release_commits.committed_at)`,
		projectID, releaseID, sha, author, message, at)
	return err
}

// SuspectCommits returns commits on the event's release (and optional previous) that may relate to the crash.
func (s *Store) SuspectCommits(ctx context.Context, projectID, issueID uuid.UUID, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 20
	}
	// commits from releases that have events for this issue
	rows, err := s.Pool.Query(ctx, `
SELECT DISTINCT c.sha, c.author, c.message, c.committed_at, r.version, r.build_id, r.git_commit
FROM release_commits c
JOIN releases r ON r.id = c.release_id
JOIN events e ON e.release_id = r.id AND e.issue_id = $2
WHERE c.project_id=$1
ORDER BY c.committed_at DESC NULLS LAST
LIMIT $3`, projectID, issueID, limit)
	if err != nil {
		// table may not exist pre-migration — fallback to release.git_commit only
		return s.suspectFromReleaseGit(ctx, projectID, issueID, limit)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var sha, author, msg, ver, build, head string
		var ts *time.Time
		if rows.Scan(&sha, &author, &msg, &ts, &ver, &build, &head) == nil {
			item := map[string]any{
				"sha": sha, "author": author, "message": msg,
				"release": ver, "build_id": build, "release_head": head,
			}
			if ts != nil {
				item["committed_at"] = *ts
			}
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return s.suspectFromReleaseGit(ctx, projectID, issueID, limit)
	}
	return out, rows.Err()
}

func (s *Store) suspectFromReleaseGit(ctx context.Context, projectID, issueID uuid.UUID, limit int) ([]map[string]any, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT DISTINCT r.git_commit, r.version, r.build_id
FROM releases r
JOIN events e ON e.release_id = r.id AND e.issue_id=$2
WHERE r.project_id=$1 AND COALESCE(r.git_commit,'') <> ''
LIMIT $3`, projectID, issueID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var sha, ver, build string
		if rows.Scan(&sha, &ver, &build) == nil {
			out = append(out, map[string]any{
				"sha": sha, "author": "", "message": "release head (no commit list uploaded)",
				"release": ver, "build_id": build, "release_head": sha, "suspect_score": 0.5,
			})
		}
	}
	return out, rows.Err()
}

// EventBreadcrumbs extracts breadcrumbs from latest event analysis/decoded for an issue.
func (s *Store) EventBreadcrumbs(ctx context.Context, projectID, issueID uuid.UUID) ([]map[string]any, error) {
	var analysis, decoded []byte
	err := s.Pool.QueryRow(ctx, `
SELECT COALESCE(analysis,'null'), COALESCE(decoded,'null')
FROM events WHERE project_id=$1 AND issue_id=$2
ORDER BY received_at DESC LIMIT 1`, projectID, issueID).Scan(&analysis, &decoded)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for _, raw := range [][]byte{analysis, decoded} {
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		if bc, ok := m["breadcrumbs"].([]any); ok {
			for _, b := range bc {
				if mm, ok := b.(map[string]any); ok {
					out = append(out, mm)
				}
			}
		}
		if tl, ok := m["timeline"].([]any); ok {
			for _, b := range tl {
				if mm, ok := b.(map[string]any); ok {
					out = append(out, mm)
				}
			}
		}
	}
	return out, nil
}
