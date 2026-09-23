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

// ListOpts supports pagination and filters.
type ListOpts struct {
	Limit  int
	Offset int
	Status string
	Query  string
	Sev    string
}

func (o *ListOpts) normalize() {
	if o.Limit <= 0 || o.Limit > 200 {
		o.Limit = 50
	}
	if o.Offset < 0 {
		o.Offset = 0
	}
}

func (s *Store) ListIssuesPage(ctx context.Context, projectID uuid.UUID, opt ListOpts) ([]Issue, int, error) {
	opt.normalize()
	where := `project_id=$1 AND merged_into IS NULL`
	args := []any{projectID}
	n := 2
	if opt.Status != "" {
		where += fmt.Sprintf(` AND status=$%d`, n)
		args = append(args, opt.Status)
		n++
	}
	if opt.Sev != "" {
		where += fmt.Sprintf(` AND severity=$%d`, n)
		args = append(args, opt.Sev)
		n++
	}
	if opt.Query != "" {
		where += fmt.Sprintf(` AND (title ILIKE $%d OR fingerprint ILIKE $%d)`, n, n)
		args = append(args, "%"+opt.Query+"%")
		n++
	}
	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM issues WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, opt.Limit, opt.Offset)
	rows, err := s.Pool.Query(ctx, fmt.Sprintf(`
SELECT id,project_id,fingerprint,title,status,severity,event_count,affected_devices,first_seen,last_seen,probable_cause,resolved_at,COALESCE(regression_count,0),assignee_user_id
FROM issues WHERE %s ORDER BY last_seen DESC LIMIT $%d OFFSET $%d`, where, n, n+1), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Issue
	for rows.Next() {
		var i Issue
		if err := rows.Scan(&i.ID, &i.ProjectID, &i.Fingerprint, &i.Title, &i.Status, &i.Severity, &i.EventCount, &i.AffectedDevices, &i.FirstSeen, &i.LastSeen, &i.ProbableCause, &i.ResolvedAt, &i.RegressionCount, &i.AssigneeUserID); err != nil {
			return nil, 0, err
		}
		out = append(out, i)
	}
	return out, total, rows.Err()
}

func (s *Store) ListEventsPage(ctx context.Context, projectID uuid.UUID, opt ListOpts) ([]Event, int, error) {
	opt.normalize()
	where := `project_id=$1`
	args := []any{projectID}
	n := 2
	if opt.Status != "" {
		where += fmt.Sprintf(` AND state=$%d`, n)
		args = append(args, opt.Status)
		n++
	}
	if opt.Sev != "" {
		where += fmt.Sprintf(` AND severity=$%d`, n)
		args = append(args, opt.Sev)
		n++
	}
	if opt.Query != "" {
		where += fmt.Sprintf(` AND (event_id ILIKE $%d OR fingerprint ILIKE $%d OR pipeline ILIKE $%d)`, n, n, n)
		args = append(args, "%"+opt.Query+"%")
		n++
	}
	var total int
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM events WHERE `+where, args...).Scan(&total)
	args = append(args, opt.Limit, opt.Offset)
	rows, err := s.Pool.Query(ctx, fmt.Sprintf(`
SELECT id,event_id,project_id,device_id,release_id,artifact_id,issue_id,type,severity,architecture,sequence,source_event_id,
raw_object_key,raw_hash,size_bytes,state,fingerprint,decoded,analysis,frames,received_at,processed_at,duplicate_count,COALESCE(pipeline,'issue'),COALESCE(process_version,0)
FROM events WHERE %s ORDER BY received_at DESC LIMIT $%d OFFSET $%d`, where, n, n+1), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out, err := scanEvents(rows)
	return out, total, err
}

// CountOrgDevices returns the total number of devices across all projects that
// belong to the given organization.
func (s *Store) CountOrgDevices(ctx context.Context, orgID uuid.UUID) (int64, error) {
	var n int64
	err := s.Pool.QueryRow(ctx, `
SELECT COUNT(*) FROM devices d
JOIN projects p ON p.id = d.project_id
WHERE p.organization_id = $1`, orgID).Scan(&n)
	return n, err
}

// ListDevicesPage returns a page of devices for a project.
func (s *Store) ListDevicesPage(ctx context.Context, projectID uuid.UUID, opt ListOpts) ([]Device, int, error) {
	opt.normalize()
	where := `project_id=$1`
	args := []any{projectID}
	n := 2
	if opt.Status != "" {
		where += fmt.Sprintf(` AND status=$%d`, n)
		args = append(args, opt.Status)
		n++
	}
	if opt.Query != "" {
		where += fmt.Sprintf(` AND (device_id ILIKE $%d OR product ILIKE $%d OR build_id ILIKE $%d)`, n, n, n)
		args = append(args, "%"+opt.Query+"%")
		n++
	}
	var total int
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM devices WHERE `+where, args...).Scan(&total)
	args = append(args, opt.Limit, opt.Offset)
	rows, err := s.Pool.Query(ctx, fmt.Sprintf(`
SELECT id,project_id,device_id,product,hardware_revision,firmware_version,build_id,status,first_seen,last_seen
FROM devices WHERE %s ORDER BY last_seen DESC LIMIT $%d OFFSET $%d`, where, n, n+1), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.DeviceID, &d.Product, &d.HardwareRevision, &d.FirmwareVersion, &d.BuildID, &d.Status, &d.FirstSeen, &d.LastSeen); err != nil {
			return nil, 0, err
		}
		out = append(out, d)
	}
	return out, total, rows.Err()
}

// AssignIssue sets assignee within project.
func (s *Store) AssignIssue(ctx context.Context, projectID, issueID uuid.UUID, userID *uuid.UUID) (Issue, error) {
	var i Issue
	err := s.Pool.QueryRow(ctx, `
UPDATE issues SET assignee_user_id=$3 WHERE id=$1 AND project_id=$2
RETURNING id,project_id,fingerprint,title,status,severity,event_count,affected_devices,first_seen,last_seen,probable_cause,resolved_at,COALESCE(regression_count,0),assignee_user_id`,
		issueID, projectID, userID).Scan(&i.ID, &i.ProjectID, &i.Fingerprint, &i.Title, &i.Status, &i.Severity, &i.EventCount, &i.AffectedDevices, &i.FirstSeen, &i.LastSeen, &i.ProbableCause, &i.ResolvedAt, &i.RegressionCount, &i.AssigneeUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Issue{}, ErrNotFound
	}
	if err == nil {
		_, _ = s.Pool.Exec(ctx, `INSERT INTO issue_activity(issue_id,project_id,actor_user_id,action,body) VALUES($1,$2,$3,'assign',$4)`,
			issueID, projectID, userID, fmt.Sprintf("%v", userID))
	}
	return i, err
}

// MergeIssues moves source into target within the same project.
func (s *Store) MergeIssues(ctx context.Context, projectID, sourceID, targetID uuid.UUID, actor *uuid.UUID) error {
	if sourceID == targetID {
		return errors.New("cannot merge issue into itself")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var sp, tp uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT project_id FROM issues WHERE id=$1`, sourceID).Scan(&sp); err != nil {
		return ErrNotFound
	}
	if err := tx.QueryRow(ctx, `SELECT project_id FROM issues WHERE id=$1`, targetID).Scan(&tp); err != nil {
		return ErrNotFound
	}
	if sp != projectID || tp != projectID {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `UPDATE events SET issue_id=$2 WHERE issue_id=$1`, sourceID, targetID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
UPDATE issues SET event_count=event_count+(SELECT event_count FROM issues WHERE id=$2),
  affected_devices=(SELECT COUNT(DISTINCT device_id) FROM events WHERE issue_id=$1 AND device_id IS NOT NULL),
  last_seen=GREATEST(last_seen,(SELECT last_seen FROM issues WHERE id=$2)),
  status=CASE WHEN status='resolved' AND (SELECT status FROM issues WHERE id=$2)='open' THEN 'open' ELSE status END
WHERE id=$1`, targetID, sourceID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE issues SET merged_into=$2, status='archived' WHERE id=$1`, sourceID, targetID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO issue_merges(project_id,source_issue_id,target_issue_id,actor_user_id) VALUES($1,$2,$3,$4)`,
		projectID, sourceID, targetID, actor); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO issue_activity(issue_id,project_id,actor_user_id,action,body) VALUES($1,$2,$3,'merge',$4)`,
		targetID, projectID, actor, sourceID.String()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SplitIssue creates a new issue from selected events.
func (s *Store) SplitIssue(ctx context.Context, projectID, sourceID uuid.UUID, eventIDs []uuid.UUID, title string, actor *uuid.UUID) (Issue, error) {
	if len(eventIDs) == 0 {
		return Issue{}, errors.New("no events")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Issue{}, err
	}
	defer tx.Rollback(ctx)
	var src Issue
	if err := tx.QueryRow(ctx, `
SELECT id,project_id,fingerprint,title,status,severity,event_count,affected_devices,first_seen,last_seen,probable_cause
FROM issues WHERE id=$1 AND project_id=$2`, sourceID, projectID).Scan(
		&src.ID, &src.ProjectID, &src.Fingerprint, &src.Title, &src.Status, &src.Severity, &src.EventCount, &src.AffectedDevices, &src.FirstSeen, &src.LastSeen, &src.ProbableCause); err != nil {
		return Issue{}, ErrNotFound
	}
	if title == "" {
		title = src.Title + " (split)"
	}
	fp := src.Fingerprint + ":split:" + uuid.NewString()[:8]
	var ni Issue
	if err := tx.QueryRow(ctx, `
INSERT INTO issues(project_id,fingerprint,title,status,severity,event_count,affected_devices,probable_cause,split_from)
VALUES($1,$2,$3,'open',$4,$5,0,$6,$7)
RETURNING id,project_id,fingerprint,title,status,severity,event_count,affected_devices,first_seen,last_seen,probable_cause,resolved_at,COALESCE(regression_count,0)`,
		projectID, fp, title, src.Severity, len(eventIDs), src.ProbableCause, sourceID).Scan(
		&ni.ID, &ni.ProjectID, &ni.Fingerprint, &ni.Title, &ni.Status, &ni.Severity, &ni.EventCount, &ni.AffectedDevices, &ni.FirstSeen, &ni.LastSeen, &ni.ProbableCause, &ni.ResolvedAt, &ni.RegressionCount); err != nil {
		return Issue{}, err
	}
	for _, eid := range eventIDs {
		ct, err := tx.Exec(ctx, `UPDATE events SET issue_id=$2, fingerprint=$3 WHERE id=$1 AND project_id=$4 AND issue_id=$5`,
			eid, ni.ID, fp, projectID, sourceID)
		if err != nil {
			return Issue{}, err
		}
		if ct.RowsAffected() == 0 {
			return Issue{}, fmt.Errorf("event %s not in source issue", eid)
		}
	}
	_, _ = tx.Exec(ctx, `UPDATE issues SET event_count=GREATEST(0,event_count-$2) WHERE id=$1`, sourceID, len(eventIDs))
	_, _ = tx.Exec(ctx, `INSERT INTO issue_activity(issue_id,project_id,actor_user_id,action,body) VALUES($1,$2,$3,'split',$4)`,
		sourceID, projectID, actor, ni.ID.String())
	if err := tx.Commit(ctx); err != nil {
		return Issue{}, err
	}
	return ni, nil
}

func (s *Store) SetIssueLabels(ctx context.Context, projectID, issueID uuid.UUID, labels []string) error {
	ct, err := s.Pool.Exec(ctx, `UPDATE issues SET labels=$3 WHERE id=$1 AND project_id=$2`, issueID, projectID, labels)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateDeviceMeta(ctx context.Context, projectID, deviceID uuid.UUID, aliases, tags []string, probe string) (Device, error) {
	var d Device
	err := s.Pool.QueryRow(ctx, `
UPDATE devices SET
  aliases=COALESCE($3, aliases),
  tags=COALESCE($4, tags),
  probe_id=CASE WHEN $5<>'' THEN $5 ELSE probe_id END,
  probe_serial=CASE WHEN $5<>'' THEN $5 ELSE COALESCE(probe_serial,'') END
WHERE id=$1 AND project_id=$2
RETURNING id,project_id,device_id,product,hardware_revision,firmware_version,build_id,status,first_seen,last_seen`,
		deviceID, projectID, aliases, tags, probe).Scan(
		&d.ID, &d.ProjectID, &d.DeviceID, &d.Product, &d.HardwareRevision, &d.FirmwareVersion, &d.BuildID, &d.Status, &d.FirstSeen, &d.LastSeen)
	if errors.Is(err, pgx.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	d.Aliases, d.Tags = aliases, tags
	return d, err
}

func (s *Store) RecordFirmwareHistory(ctx context.Context, projectID, deviceID uuid.UUID, fw, buildID string) error {
	if fw == "" && buildID == "" {
		return nil
	}
	_, err := s.Pool.Exec(ctx, `
INSERT INTO device_firmware_history(device_id,project_id,firmware_version,build_id)
VALUES($1,$2,$3,$4)
ON CONFLICT DO NOTHING`, deviceID, projectID, fw, buildID)
	// upsert by matching latest
	_, _ = s.Pool.Exec(ctx, `
UPDATE device_firmware_history SET last_seen=now()
WHERE device_id=$1 AND firmware_version=$2 AND build_id=$3`, deviceID, fw, buildID)
	var n int
	_ = s.Pool.QueryRow(ctx, `
SELECT COUNT(*) FROM device_firmware_history WHERE device_id=$1 AND firmware_version=$2 AND build_id=$3`, deviceID, fw, buildID).Scan(&n)
	if n == 0 {
		_, err = s.Pool.Exec(ctx, `
INSERT INTO device_firmware_history(device_id,project_id,firmware_version,build_id) VALUES($1,$2,$3,$4)`,
			deviceID, projectID, fw, buildID)
	}
	return err
}

func (s *Store) ListFirmwareHistory(ctx context.Context, projectID, deviceID uuid.UUID) ([]map[string]any, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT firmware_version, build_id, first_seen, last_seen FROM device_firmware_history
WHERE project_id=$1 AND device_id=$2 ORDER BY last_seen DESC`, projectID, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var fw, bid string
		var fs, ls time.Time
		if err := rows.Scan(&fw, &bid, &fs, &ls); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"firmware_version": fw, "build_id": bid, "first_seen": fs, "last_seen": ls})
	}
	return out, rows.Err()
}

func (s *Store) UpsertBootSession(ctx context.Context, projectID uuid.UUID, deviceID *uuid.UUID, bootID string) error {
	if bootID == "" {
		return nil
	}
	_, err := s.Pool.Exec(ctx, `
INSERT INTO boot_sessions(project_id,device_id,boot_id,event_count) VALUES($1,$2,$3,1)
ON CONFLICT (project_id, boot_id) DO UPDATE SET
  last_event_at=now(), event_count=boot_sessions.event_count+1,
  device_id=COALESCE(EXCLUDED.device_id, boot_sessions.device_id)`, projectID, deviceID, bootID)
	return err
}

func (s *Store) MarkDeviceHealth(ctx context.Context, deviceID uuid.UUID, severity string) error {
	switch severity {
	case "fatal":
		_, err := s.Pool.Exec(ctx, `UPDATE devices SET status='unhealthy', last_fatal_at=now(), last_seen=now() WHERE id=$1 AND status<>'retired'`, deviceID)
		return err
	case "error":
		_, err := s.Pool.Exec(ctx, `
UPDATE devices SET status=CASE WHEN status='unhealthy' THEN status ELSE 'degraded' END, last_seen=now()
WHERE id=$1 AND status<>'retired'`, deviceID)
		return err
	case "health", "healthy", "info":
		// only promote to healthy if not unhealthy/degraded recently with fatal
		_, err := s.Pool.Exec(ctx, `
UPDATE devices SET
  status=CASE WHEN status IN ('retired','unhealthy') THEN status ELSE 'healthy' END,
  last_healthy_at=now(), last_seen=now()
WHERE id=$1 AND (last_fatal_at IS NULL OR last_fatal_at < now() - interval '1 hour')`, deviceID)
		return err
	default:
		_, err := s.Pool.Exec(ctx, `UPDATE devices SET last_seen=now() WHERE id=$1`, deviceID)
		return err
	}
}

func (s *Store) InsertHealthSample(ctx context.Context, projectID uuid.UUID, deviceID, eventID *uuid.UUID, payload any) error {
	raw, _ := json.Marshal(payload)
	_, err := s.Pool.Exec(ctx, `INSERT INTO health_samples(project_id,device_id,event_id,payload) VALUES($1,$2,$3,$4)`,
		projectID, deviceID, eventID, raw)
	return err
}

func (s *Store) InsertLogEntry(ctx context.Context, projectID uuid.UUID, deviceID, eventID *uuid.UUID, level, msg string) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO log_entries(project_id,device_id,event_id,level,message) VALUES($1,$2,$3,$4,$5)`,
		projectID, deviceID, eventID, level, msg)
	return err
}

func (s *Store) InsertMetric(ctx context.Context, projectID uuid.UUID, deviceID, eventID *uuid.UUID, name string, value float64) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO metric_samples(project_id,device_id,event_id,name,value) VALUES($1,$2,$3,$4,$5)`,
		projectID, deviceID, eventID, name, value)
	return err
}

// TryDedupeJob returns true if this is the first time seeing key (should run).
func (s *Store) TryDedupeJob(ctx context.Context, key, jobType string) (bool, error) {
	ct, err := s.Pool.Exec(ctx, `
INSERT INTO job_dedupe(dedupe_key,job_type) VALUES($1,$2) ON CONFLICT DO NOTHING`, key, jobType)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

func (s *Store) FindArtifactMatch(ctx context.Context, projectID uuid.UUID, buildID, arch, releaseVersion string) (Artifact, error) {
	var a Artifact
	err := s.Pool.QueryRow(ctx, `
SELECT id,project_id,build_id,sha256,object_key,architecture,status FROM artifacts
WHERE project_id=$1 AND lower(build_id)=lower($2) AND status='ready'
  AND ($3='' OR architecture='' OR lower(architecture)=lower($3))
  AND ($4='' OR release_version='' OR release_version=$4)
ORDER BY
  CASE WHEN $3<>'' AND lower(architecture)=lower($3) THEN 0 ELSE 1 END,
  CASE WHEN $4<>'' AND release_version=$4 THEN 0 ELSE 1 END,
  created_at DESC
LIMIT 1`, projectID, buildID, arch, releaseVersion).Scan(
		&a.ID, &a.ProjectID, &a.BuildID, &a.SHA256, &a.ObjectKey, &a.Architecture, &a.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Artifact{}, ErrNotFound
	}
	return a, err
}

func (s *Store) UpdateReleaseRollout(ctx context.Context, projectID, releaseID uuid.UUID, pct int, commit, toolchain string) (Release, error) {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	var r Release
	err := s.Pool.QueryRow(ctx, `
UPDATE releases SET
  rollout_pct=$3,
  git_commit=CASE WHEN $4<>'' THEN $4 ELSE git_commit END,
  toolchain=CASE WHEN $5<>'' THEN $5 ELSE toolchain END
WHERE id=$1 AND project_id=$2
RETURNING id,project_id,version,build_id,status,COALESCE(git_commit,''),COALESCE(toolchain,'')`,
		releaseID, projectID, pct, commit, toolchain).Scan(&r.ID, &r.ProjectID, &r.Version, &r.BuildID, &r.Status, &r.GitCommit, &r.Toolchain)
	if errors.Is(err, pgx.ErrNoRows) {
		return Release{}, ErrNotFound
	}
	return r, err
}

func (s *Store) CrashFreeRate(ctx context.Context, projectID, releaseID uuid.UUID) (map[string]any, error) {
	var sessions, crashes int64
	_ = s.Pool.QueryRow(ctx, `
SELECT COUNT(DISTINCT e.device_id) FROM events e
WHERE e.project_id=$1 AND e.release_id=$2`, projectID, releaseID).Scan(&sessions)
	_ = s.Pool.QueryRow(ctx, `
SELECT COUNT(DISTINCT e.device_id) FROM events e
WHERE e.project_id=$1 AND e.release_id=$2 AND e.severity='fatal'`, projectID, releaseID).Scan(&crashes)
	rate := 100.0
	if sessions > 0 {
		rate = 100.0 * float64(sessions-crashes) / float64(sessions)
	}
	_, _ = s.Pool.Exec(ctx, `
INSERT INTO release_stats(release_id,sessions,crash_sessions,updated_at) VALUES($1,$2,$3,now())
ON CONFLICT (release_id) DO UPDATE SET sessions=$2, crash_sessions=$3, updated_at=now()`, releaseID, sessions, crashes)
	return map[string]any{
		"release_id": releaseID, "sessions": sessions, "crash_sessions": crashes,
		"crash_free_rate": rate,
	}, nil
}

func (s *Store) CompareHardwareFailures(ctx context.Context, projectID uuid.UUID) ([]map[string]any, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT COALESCE(d.hardware_revision,'(none)'), COUNT(*) FILTER (WHERE e.severity='fatal'), COUNT(*)
FROM devices d
LEFT JOIN events e ON e.device_id=d.id
WHERE d.project_id=$1
GROUP BY 1 ORDER BY 2 DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var rev string
		var fatal, total int64
		if err := rows.Scan(&rev, &fatal, &total); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"revision": rev, "fatal_events": fatal, "events": total})
	}
	return out, rows.Err()
}

func (s *Store) CreateChannel(ctx context.Context, projectID uuid.UUID, kind, name string, config map[string]any, secret string) (uuid.UUID, error) {
	secret = sealSecret(secret)
	raw, _ := json.Marshal(config)
	var id uuid.UUID
	err := s.Pool.QueryRow(ctx, `
INSERT INTO notification_channels(project_id,kind,name,config,secret) VALUES($1,$2,$3,$4,$5) RETURNING id`,
		projectID, kind, name, raw, secret).Scan(&id)
	return id, err
}

func (s *Store) ListChannels(ctx context.Context, projectID uuid.UUID) ([]map[string]any, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT id, kind, name, config, enabled, created_at, (secret<>'') FROM notification_channels
WHERE project_id=$1 ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var kind, name string
		var cfg json.RawMessage
		var en, hasSecret bool
		var ts time.Time
		if err := rows.Scan(&id, &kind, &name, &cfg, &en, &ts, &hasSecret); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "kind": kind, "name": name, "config": cfg, "enabled": en, "created_at": ts, "has_secret": hasSecret})
	}
	return out, rows.Err()
}

func (s *Store) GetChannel(ctx context.Context, projectID, id uuid.UUID) (kind, name, secret string, config json.RawMessage, err error) {
	err = s.Pool.QueryRow(ctx, `
SELECT kind,name,secret,config FROM notification_channels WHERE id=$1 AND project_id=$2 AND enabled`,
		id, projectID).Scan(&kind, &name, &secret, &config)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	secret = openSecret(secret)
	return
}

// ReferencedObjectKeys returns every object key still pointed at by the DB
// (events, artifacts, and optional release attachments).
func (s *Store) ReferencedObjectKeys(ctx context.Context) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	// events + artifacts are the primary refs
	rows, err := s.Pool.Query(ctx, `
SELECT raw_object_key FROM events WHERE raw_object_key IS NOT NULL AND raw_object_key <> ''
UNION
SELECT object_key FROM artifacts WHERE object_key IS NOT NULL AND object_key <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil && k != "" {
			out[k] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	// Best-effort: debug_files / symbol maps if table exists
	if r2, err := s.Pool.Query(ctx, `
SELECT object_key FROM debug_files WHERE object_key IS NOT NULL AND object_key <> ''`); err == nil {
		defer r2.Close()
		for r2.Next() {
			var k string
			if r2.Scan(&k) == nil && k != "" {
				out[k] = struct{}{}
			}
		}
	}
	return out, nil
}

// OrphanCandidates returns object keys from orphan_gc_log for ops UI (already deleted or skipped).
func (s *Store) OrphanGCLog(ctx context.Context, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT object_key, reason, created_at FROM orphan_gc_log ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var k, reason string
		var ts time.Time
		if rows.Scan(&k, &reason, &ts) == nil {
			out = append(out, map[string]any{"object_key": k, "reason": reason, "created_at": ts})
		}
	}
	return out, rows.Err()
}

func (s *Store) LogOrphanGC(ctx context.Context, key, reason string) {
	_, _ = s.Pool.Exec(ctx, `INSERT INTO orphan_gc_log(object_key,reason) VALUES($1,$2)`, key, reason)
}

func (s *Store) EventsNeedingReprocess(ctx context.Context, projectID uuid.UUID, analyzerVersion int, limit int) ([]uuid.UUID, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id FROM events WHERE project_id=$1 AND pipeline IN ('issue','crash')
  AND (COALESCE(analyzer_version,0) < $2 OR reprocess_requested)
ORDER BY received_at DESC LIMIT $3`, projectID, analyzerVersion, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

func (s *Store) SetEventAnalyzerVersion(ctx context.Context, eventID uuid.UUID, av, sv int) error {
	_, err := s.Pool.Exec(ctx, `UPDATE events SET analyzer_version=$2, symbolizer_version=$3 WHERE id=$1`, eventID, av, sv)
	return err
}

func (s *Store) RemoveMember(ctx context.Context, orgID, userID uuid.UUID) error {
	ct, err := s.Pool.Exec(ctx, `DELETE FROM memberships WHERE organization_id=$1 AND user_id=$2`, orgID, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateMemberRole(ctx context.Context, orgID, userID uuid.UUID, role string) error {
	ct, err := s.Pool.Exec(ctx, `UPDATE memberships SET role=$3 WHERE organization_id=$1 AND user_id=$2`, orgID, userID, role)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetIssue must require project when multi-tenant
func (s *Store) GetIssueForWorker(ctx context.Context, projectID, issueID uuid.UUID) (Issue, error) {
	return s.GetIssueInProject(ctx, projectID, issueID)
}

func (s *Store) ListEventsByIssueInProject(ctx context.Context, projectID, issueID uuid.UUID, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,event_id,project_id,device_id,release_id,artifact_id,issue_id,type,severity,architecture,sequence,source_event_id,
raw_object_key,raw_hash,size_bytes,state,fingerprint,decoded,analysis,frames,received_at,processed_at,duplicate_count,COALESCE(pipeline,'issue'),COALESCE(process_version,0)
FROM events WHERE issue_id=$1 AND project_id=$2 ORDER BY received_at DESC LIMIT $3`, issueID, projectID, limit)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

func (s *Store) GlobalSearchExtended(ctx context.Context, projectID uuid.UUID, q string, limit int) (map[string]any, error) {
	base, err := s.GlobalSearch(ctx, projectID, q, limit)
	if err != nil {
		return nil, err
	}
	like := "%" + q + "%"
	// function/file in frames jsonb
	rows, _ := s.Pool.Query(ctx, `
SELECT id, event_id FROM events
WHERE project_id=$1 AND (frames::text ILIKE $2 OR analysis::text ILIKE $2)
ORDER BY received_at DESC LIMIT $3`, projectID, like, limit)
	var frames []map[string]any
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			var eid string
			if rows.Scan(&id, &eid) == nil {
				frames = append(frames, map[string]any{"id": id, "event_id": eid})
			}
		}
	}
	arts, _ := s.Pool.Query(ctx, `
SELECT id, build_id FROM artifacts WHERE project_id=$1 AND (build_id ILIKE $2 OR sha256 ILIKE $2) LIMIT $3`, projectID, like, limit)
	var artifacts []map[string]any
	if arts != nil {
		defer arts.Close()
		for arts.Next() {
			var id uuid.UUID
			var bid string
			if arts.Scan(&id, &bid) == nil {
				artifacts = append(artifacts, map[string]any{"id": id, "build_id": bid})
			}
		}
	}
	base["frames"] = frames
	base["artifacts"] = artifacts
	return base, nil
}
