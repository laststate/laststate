package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/laststate/trace/internal/auth"
)

// fmt used in Overview arch names

var ErrNotFound = errors.New("not found")
var ErrDuplicate = errors.New("duplicate")
var ErrConflict = errors.New("event_id conflict: different payload")

type Store struct {
	Pool *pgxpool.Pool
}

type Org struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Slug string    `json:"slug"`
}

type Project struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	Name           string    `json:"name"`
	Slug           string    `json:"slug"`
	Description    string    `json:"description"`
}

type Token struct {
	ID        uuid.UUID `json:"id"`
	ProjectID uuid.UUID `json:"project_id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	Scopes    []string  `json:"scopes"`
}

type Device struct {
	ID               uuid.UUID `json:"id"`
	ProjectID        uuid.UUID `json:"project_id"`
	DeviceID         string    `json:"device_id"`
	Product          string    `json:"product"`
	HardwareRevision string    `json:"hardware_revision"`
	FirmwareVersion  string    `json:"firmware_version"`
	BuildID          string    `json:"build_id"`
	Status           string    `json:"status"`
	FirstSeen        time.Time `json:"first_seen"`
	LastSeen         time.Time `json:"last_seen"`
	Aliases          []string  `json:"aliases,omitempty"`
	Tags             []string  `json:"tags,omitempty"`
}

type Release struct {
	ID        uuid.UUID `json:"id"`
	ProjectID uuid.UUID `json:"project_id,omitempty"`
	Version   string    `json:"version"`
	BuildID   string    `json:"build_id"`
	Status    string    `json:"status"`
	GitCommit string    `json:"git_commit,omitempty"`
	Toolchain string    `json:"toolchain,omitempty"`
}

type Artifact struct {
	ID           uuid.UUID `json:"id"`
	ProjectID    uuid.UUID `json:"project_id,omitempty"`
	BuildID      string    `json:"build_id"`
	SHA256       string    `json:"sha256"`
	ObjectKey    string    `json:"object_key"`
	Architecture string    `json:"architecture"`
	Status       string    `json:"status"`
}

type Issue struct {
	ID              uuid.UUID  `json:"id"`
	ProjectID       uuid.UUID  `json:"project_id"`
	Fingerprint     string     `json:"fingerprint"`
	Title           string     `json:"title"`
	Status          string     `json:"status"`
	Severity        string     `json:"severity"`
	EventCount      int64      `json:"event_count"`
	AffectedDevices int64      `json:"affected_devices"`
	FirstSeen       time.Time  `json:"first_seen"`
	LastSeen        time.Time  `json:"last_seen"`
	ProbableCause   string     `json:"probable_cause"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
	RegressionCount int        `json:"regression_count"`
	AssigneeUserID  *uuid.UUID `json:"assignee_user_id,omitempty"`
	IsRegression    bool       `json:"is_regression,omitempty"`
	IsNew           bool       `json:"is_new,omitempty"`
}

type Event struct {
	ID             uuid.UUID       `json:"id"`
	EventID        string          `json:"event_id"`
	ProjectID      uuid.UUID       `json:"project_id"`
	DeviceID       *uuid.UUID      `json:"device_id,omitempty"`
	ReleaseID      *uuid.UUID      `json:"release_id,omitempty"`
	ArtifactID     *uuid.UUID      `json:"artifact_id,omitempty"`
	IssueID        *uuid.UUID      `json:"issue_id,omitempty"`
	Type           int16           `json:"type"`
	Severity       string          `json:"severity"`
	Architecture   int16           `json:"architecture"`
	Sequence       int64           `json:"sequence"`
	SourceEventID  int64           `json:"source_event_id"`
	RawObjectKey   string          `json:"raw_object_key"`
	RawHash        string          `json:"raw_hash"`
	SizeBytes      int64           `json:"size_bytes"`
	State          string          `json:"state"`
	Fingerprint    string          `json:"fingerprint"`
	Decoded        json.RawMessage `json:"decoded"`
	Analysis       json.RawMessage `json:"analysis"`
	Frames         json.RawMessage `json:"frames"`
	ReceivedAt     time.Time       `json:"received_at"`
	ProcessedAt    *time.Time      `json:"processed_at,omitempty"`
	DuplicateCount int64           `json:"duplicate_count"`
	Pipeline       string          `json:"pipeline,omitempty"`
	ProcessVersion int             `json:"process_version,omitempty"`
}

type IngestResult struct {
	Event     Event
	Duplicate bool
}

func (s *Store) Bootstrap(ctx context.Context) (org Org, project Project, tokenSecret string, err error) {
	var n int
	if err = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM organizations`).Scan(&n); err != nil {
		return
	}
	if n > 0 {
		err = fmt.Errorf("already bootstrapped")
		return
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)

	if err = tx.QueryRow(ctx, `INSERT INTO organizations(name, slug) VALUES('Default','default') RETURNING id,name,slug`).
		Scan(&org.ID, &org.Name, &org.Slug); err != nil {
		return
	}
	if err = tx.QueryRow(ctx, `INSERT INTO projects(organization_id,name,slug,description) VALUES($1,'Default','default','Bootstrapped project') RETURNING id,organization_id,name,slug,description`,
		org.ID).Scan(&project.ID, &project.OrganizationID, &project.Name, &project.Slug, &project.Description); err != nil {
		return
	}
	secret, prefix, hash, err := auth.Mint("lst_ingest")
	if custom := os.Getenv("TRACE_BOOTSTRAP_TOKEN"); custom != "" {
		secret = custom
		prefix = auth.PrefixOf(custom)
		hash = auth.Hash(custom)
		err = nil
	} else if err != nil {
		return
	}
	var tokID uuid.UUID
	if err = tx.QueryRow(ctx, `INSERT INTO tokens(project_id,name,prefix,hash,scopes) VALUES($1,'default-ingest',$2,$3,$4) RETURNING id`,
		project.ID, prefix, hash, []string{"event:write", "event:read", "artifact:write"}).Scan(&tokID); err != nil {
		return
	}
	if err = tx.Commit(ctx); err != nil {
		return
	}
	tokenSecret = secret
	return
}

func (s *Store) AuthIngestToken(ctx context.Context, secret string) (Token, error) {
	prefix := auth.PrefixOf(secret)
	var t Token
	var hash []byte
	err := s.Pool.QueryRow(ctx, `
SELECT id, project_id, name, prefix, scopes, hash
FROM tokens WHERE prefix=$1 AND revoked_at IS NULL`, prefix).
		Scan(&t.ID, &t.ProjectID, &t.Name, &t.Prefix, &t.Scopes, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Token{}, ErrNotFound
	}
	if err != nil {
		return Token{}, err
	}
	if !auth.Equal(hash, auth.Hash(secret)) {
		return Token{}, ErrNotFound
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE tokens SET last_used_at=now() WHERE id=$1`, t.ID)
	return t, nil
}

// CreateEventIdempotent stores an event once. Same event_id + same hash = duplicate.
// Same event_id + different hash = conflict (not silent duplicate).
func (s *Store) CreateEventIdempotent(ctx context.Context, projectID uuid.UUID, eventID string, headerType, arch int16, sequence, sourceEventID int64, severity, rawKey, rawHash string, size int64, decoded json.RawMessage, pipeline string) (IngestResult, error) {
	if pipeline == "" {
		pipeline = "issue"
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return IngestResult{}, err
	}
	defer tx.Rollback(ctx)

	var existing Event
	err = tx.QueryRow(ctx, `
SELECT id,event_id,project_id,type,severity,architecture,sequence,source_event_id,raw_object_key,raw_hash,size_bytes,state,fingerprint,decoded,analysis,frames,received_at,duplicate_count
FROM events WHERE project_id=$1 AND event_id=$2 FOR UPDATE`, projectID, eventID).Scan(
		&existing.ID, &existing.EventID, &existing.ProjectID, &existing.Type, &existing.Severity, &existing.Architecture,
		&existing.Sequence, &existing.SourceEventID, &existing.RawObjectKey, &existing.RawHash, &existing.SizeBytes,
		&existing.State, &existing.Fingerprint, &existing.Decoded, &existing.Analysis, &existing.Frames, &existing.ReceivedAt, &existing.DuplicateCount,
	)
	if err == nil {
		if existing.RawHash != "" && existing.RawHash != rawHash {
			return IngestResult{}, ErrConflict
		}
		_, _ = tx.Exec(ctx, `UPDATE events SET duplicate_count=duplicate_count+1 WHERE id=$1`, existing.ID)
		existing.DuplicateCount++
		if err := tx.Commit(ctx); err != nil {
			return IngestResult{}, err
		}
		return IngestResult{Event: existing, Duplicate: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return IngestResult{}, err
	}

	var e Event
	if decoded == nil {
		decoded = json.RawMessage(`{}`)
	}
	err = tx.QueryRow(ctx, `
INSERT INTO events(event_id,project_id,type,severity,architecture,sequence,source_event_id,raw_object_key,raw_hash,size_bytes,state,decoded,analysis,frames,pipeline)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'persisted',$11,'{}'::jsonb,'[]'::jsonb,$12)
RETURNING id,event_id,project_id,type,severity,architecture,sequence,source_event_id,raw_object_key,raw_hash,size_bytes,state,fingerprint,decoded,analysis,frames,received_at,duplicate_count,pipeline`,
		eventID, projectID, headerType, severity, arch, sequence, sourceEventID, rawKey, rawHash, size, decoded, pipeline,
	).Scan(&e.ID, &e.EventID, &e.ProjectID, &e.Type, &e.Severity, &e.Architecture, &e.Sequence, &e.SourceEventID,
		&e.RawObjectKey, &e.RawHash, &e.SizeBytes, &e.State, &e.Fingerprint, &e.Decoded, &e.Analysis, &e.Frames, &e.ReceivedAt, &e.DuplicateCount, &e.Pipeline)
	if err != nil {
		return IngestResult{}, err
	}
	_, _ = tx.Exec(ctx, `INSERT INTO event_state_history(event_id,from_state,to_state,reason) VALUES($1,'','persisted','ingest')`, e.ID)
	if err := tx.Commit(ctx); err != nil {
		return IngestResult{}, err
	}
	// Jobs are enqueued by the API layer via queue.Jober (postgres/redis/nats/cf),
	// not inline here — keeps ingest and worker backends congruent.
	return IngestResult{Event: e, Duplicate: false}, nil
}

func (s *Store) GetEventByID(ctx context.Context, id uuid.UUID) (Event, error) {
	return s.scanEvent(ctx, `SELECT id,event_id,project_id,device_id,release_id,artifact_id,issue_id,type,severity,architecture,sequence,source_event_id,
raw_object_key,raw_hash,size_bytes,state,fingerprint,decoded,analysis,frames,received_at,processed_at,duplicate_count,COALESCE(pipeline,'issue'),COALESCE(process_version,0)
FROM events WHERE id=$1`, id)
}

func (s *Store) GetEventInProject(ctx context.Context, projectID, id uuid.UUID) (Event, error) {
	return s.scanEvent(ctx, `SELECT id,event_id,project_id,device_id,release_id,artifact_id,issue_id,type,severity,architecture,sequence,source_event_id,
raw_object_key,raw_hash,size_bytes,state,fingerprint,decoded,analysis,frames,received_at,processed_at,duplicate_count,COALESCE(pipeline,'issue'),COALESCE(process_version,0)
FROM events WHERE id=$1 AND project_id=$2`, id, projectID)
}

func (s *Store) scanEvent(ctx context.Context, q string, args ...any) (Event, error) {
	var e Event
	err := s.Pool.QueryRow(ctx, q, args...).Scan(
		&e.ID, &e.EventID, &e.ProjectID, &e.DeviceID, &e.ReleaseID, &e.ArtifactID, &e.IssueID, &e.Type, &e.Severity,
		&e.Architecture, &e.Sequence, &e.SourceEventID, &e.RawObjectKey, &e.RawHash, &e.SizeBytes, &e.State, &e.Fingerprint,
		&e.Decoded, &e.Analysis, &e.Frames, &e.ReceivedAt, &e.ProcessedAt, &e.DuplicateCount, &e.Pipeline, &e.ProcessVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	return e, err
}

func (s *Store) ListEvents(ctx context.Context, projectID uuid.UUID, limit int) ([]Event, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,event_id,project_id,device_id,release_id,artifact_id,issue_id,type,severity,architecture,sequence,source_event_id,
raw_object_key,raw_hash,size_bytes,state,fingerprint,decoded,analysis,frames,received_at,processed_at,duplicate_count,COALESCE(pipeline,'issue'),COALESCE(process_version,0)
FROM events WHERE project_id=$1 ORDER BY received_at DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
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

// UpsertDevice updates device telemetry. Empty deviceID becomes unique anon id (never "unknown").
// Fatal/error severity marks unhealthy; health events may mark healthy.
func (s *Store) UpsertDevice(ctx context.Context, projectID uuid.UUID, deviceID, product, hw, fw, buildID, severity string) (Device, error) {
	anon := false
	if deviceID == "" {
		deviceID = "anon_" + auth.RandomHex(8)
		anon = true
	}
	status := "healthy"
	switch severity {
	case "fatal", "error":
		status = "unhealthy"
	case "warning":
		status = "degraded"
	}
	var d Device
	// For anonymous inserts we always insert new row (unique device_id)
	if anon {
		err := s.Pool.QueryRow(ctx, `
INSERT INTO devices(project_id,device_id,product,hardware_revision,firmware_version,build_id,status,last_seen,last_event_at)
VALUES($1,$2,$3,$4,$5,$6,$7,now(),now())
RETURNING id,project_id,device_id,product,hardware_revision,firmware_version,build_id,status,first_seen,last_seen`,
			projectID, deviceID, product, hw, fw, buildID, status,
		).Scan(&d.ID, &d.ProjectID, &d.DeviceID, &d.Product, &d.HardwareRevision, &d.FirmwareVersion, &d.BuildID, &d.Status, &d.FirstSeen, &d.LastSeen)
		return d, err
	}
	err := s.Pool.QueryRow(ctx, `
INSERT INTO devices(project_id,device_id,product,hardware_revision,firmware_version,build_id,status,last_seen,last_event_at)
VALUES($1,$2,$3,$4,$5,$6,$7,now(),now())
ON CONFLICT (project_id, device_id) DO UPDATE SET
  product=CASE WHEN EXCLUDED.product<>'' THEN EXCLUDED.product ELSE devices.product END,
  hardware_revision=CASE WHEN EXCLUDED.hardware_revision<>'' THEN EXCLUDED.hardware_revision ELSE devices.hardware_revision END,
  firmware_version=CASE WHEN EXCLUDED.firmware_version<>'' THEN EXCLUDED.firmware_version ELSE devices.firmware_version END,
  build_id=CASE WHEN EXCLUDED.build_id<>'' THEN EXCLUDED.build_id ELSE devices.build_id END,
  last_seen=now(), last_event_at=now(),
  status=CASE
    WHEN devices.status = 'retired' THEN devices.status
    WHEN EXCLUDED.status = 'unhealthy' THEN 'unhealthy'
    WHEN EXCLUDED.status = 'degraded' AND devices.status <> 'unhealthy' THEN 'degraded'
    WHEN EXCLUDED.status = 'healthy' AND devices.status NOT IN ('unhealthy','degraded','retired') THEN 'healthy'
    WHEN EXCLUDED.status = 'healthy' AND devices.status IN ('unhealthy','degraded') THEN devices.status
    ELSE devices.status
  END
RETURNING id,project_id,device_id,product,hardware_revision,firmware_version,build_id,status,first_seen,last_seen`,
		projectID, deviceID, product, hw, fw, buildID, status,
	).Scan(&d.ID, &d.ProjectID, &d.DeviceID, &d.Product, &d.HardwareRevision, &d.FirmwareVersion, &d.BuildID, &d.Status, &d.FirstSeen, &d.LastSeen)
	return d, err
}

func (s *Store) UpsertRelease(ctx context.Context, projectID uuid.UUID, version, buildID, gitCommit, toolchain string) (Release, error) {
	if buildID == "" && version == "" {
		return Release{}, nil
	}
	if buildID == "" {
		buildID = "version:" + version
	}
	if version == "" {
		version = buildID
	}
	var r Release
	err := s.Pool.QueryRow(ctx, `
INSERT INTO releases(project_id,version,build_id,status,git_commit,toolchain) VALUES($1,$2,$3,'active',$4,$5)
ON CONFLICT (project_id, build_id) DO UPDATE SET
  version=CASE WHEN EXCLUDED.version<>'' THEN EXCLUDED.version ELSE releases.version END,
  git_commit=CASE WHEN EXCLUDED.git_commit<>'' THEN EXCLUDED.git_commit ELSE releases.git_commit END,
  toolchain=CASE WHEN EXCLUDED.toolchain<>'' THEN EXCLUDED.toolchain ELSE releases.toolchain END
RETURNING id,project_id,version,build_id,status,COALESCE(git_commit,''),COALESCE(toolchain,'')`, projectID, version, buildID, gitCommit, toolchain).
		Scan(&r.ID, &r.ProjectID, &r.Version, &r.BuildID, &r.Status, &r.GitCommit, &r.Toolchain)
	return r, err
}

func (s *Store) FindArtifactByBuildID(ctx context.Context, projectID uuid.UUID, buildID, arch string) (Artifact, error) {
	var a Artifact
	q := `
SELECT id,project_id,build_id,sha256,object_key,architecture,status FROM artifacts
WHERE project_id=$1 AND lower(build_id)=lower($2) AND status='ready'`
	args := []any{projectID, buildID}
	if arch != "" {
		q += ` AND (architecture='' OR lower(architecture)=lower($3))`
		args = append(args, arch)
	}
	q += ` ORDER BY created_at DESC LIMIT 1`
	err := s.Pool.QueryRow(ctx, q, args...).Scan(&a.ID, &a.ProjectID, &a.BuildID, &a.SHA256, &a.ObjectKey, &a.Architecture, &a.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Artifact{}, ErrNotFound
	}
	return a, err
}

// LinkEventToIssue transactionally increments issue once and links event (idempotent).
// Reopens resolved issues as regressions.
func (s *Store) LinkEventToIssue(ctx context.Context, projectID, eventID uuid.UUID, fp, title, severity, probable string, deviceID *uuid.UUID) (Issue, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Issue{}, err
	}
	defer tx.Rollback(ctx)

	// If event already linked to this fingerprint, return without double-count
	var existingIssueID *uuid.UUID
	var existingFP string
	_ = tx.QueryRow(ctx, `SELECT issue_id, fingerprint FROM events WHERE id=$1 AND project_id=$2 FOR UPDATE`, eventID, projectID).
		Scan(&existingIssueID, &existingFP)
	if existingIssueID != nil && existingFP == fp {
		var issue Issue
		err = tx.QueryRow(ctx, `
SELECT id,project_id,fingerprint,title,status,severity,event_count,affected_devices,first_seen,last_seen,probable_cause,resolved_at,COALESCE(regression_count,0)
FROM issues WHERE id=$1`, *existingIssueID).Scan(
			&issue.ID, &issue.ProjectID, &issue.Fingerprint, &issue.Title, &issue.Status, &issue.Severity,
			&issue.EventCount, &issue.AffectedDevices, &issue.FirstSeen, &issue.LastSeen, &issue.ProbableCause, &issue.ResolvedAt, &issue.RegressionCount)
		if err != nil {
			return Issue{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Issue{}, err
		}
		return issue, nil
	}

	var issue Issue
	var prevStatus string
	err = tx.QueryRow(ctx, `
INSERT INTO issues(project_id,fingerprint,title,status,severity,event_count,affected_devices,probable_cause)
VALUES($1,$2,$3,'open',$4,0,0,$5)
ON CONFLICT (project_id, fingerprint) DO UPDATE SET
  last_seen=now(),
  severity=CASE WHEN EXCLUDED.severity='fatal' THEN 'fatal' ELSE issues.severity END,
  probable_cause=CASE WHEN issues.probable_cause='' THEN EXCLUDED.probable_cause ELSE issues.probable_cause END,
  title=CASE WHEN issues.title='' THEN EXCLUDED.title ELSE issues.title END
RETURNING id,project_id,fingerprint,title,status,severity,event_count,affected_devices,first_seen,last_seen,probable_cause,resolved_at,COALESCE(regression_count,0)`,
		projectID, fp, title, severity, probable,
	).Scan(&issue.ID, &issue.ProjectID, &issue.Fingerprint, &issue.Title, &issue.Status, &issue.Severity,
		&issue.EventCount, &issue.AffectedDevices, &issue.FirstSeen, &issue.LastSeen, &issue.ProbableCause, &issue.ResolvedAt, &issue.RegressionCount)
	if err != nil {
		return Issue{}, err
	}
	prevStatus = issue.Status

	// Increment only once per event link
	if existingIssueID == nil || *existingIssueID != issue.ID {
		_ = tx.QueryRow(ctx, `
UPDATE issues SET event_count=event_count+1, last_seen=now() WHERE id=$1
RETURNING event_count`, issue.ID).Scan(&issue.EventCount)
		issue.IsNew = issue.EventCount == 1
	}

	// Regression: reopen resolved/ignored
	if prevStatus == "resolved" || prevStatus == "ignored" {
		_ = tx.QueryRow(ctx, `
UPDATE issues SET status='open', resolved_at=NULL, regression_count=COALESCE(regression_count,0)+1
WHERE id=$1 RETURNING status, regression_count`, issue.ID).Scan(&issue.Status, &issue.RegressionCount)
		issue.IsRegression = true
		_, _ = tx.Exec(ctx, `INSERT INTO issue_activity(issue_id,project_id,action,body) VALUES($1,$2,'regression','reopened by new event')`, issue.ID, projectID)
	}

	_, err = tx.Exec(ctx, `UPDATE events SET issue_id=$2, fingerprint=$3 WHERE id=$1`, eventID, issue.ID, fp)
	if err != nil {
		return Issue{}, err
	}

	if deviceID != nil {
		_, _ = tx.Exec(ctx, `
UPDATE issues SET affected_devices=(SELECT COUNT(DISTINCT device_id) FROM events WHERE issue_id=$1 AND device_id IS NOT NULL)
WHERE id=$1`, issue.ID)
		_ = tx.QueryRow(ctx, `SELECT affected_devices FROM issues WHERE id=$1`, issue.ID).Scan(&issue.AffectedDevices)
	}

	if err := tx.Commit(ctx); err != nil {
		return Issue{}, err
	}
	return issue, nil
}

func (s *Store) FinalizeEvent(ctx context.Context, eventID uuid.UUID, deviceID, releaseID, artifactID, issueID *uuid.UUID, state, fp string, decoded, analysis, frames json.RawMessage) error {
	var from string
	_ = s.Pool.QueryRow(ctx, `SELECT state FROM events WHERE id=$1`, eventID).Scan(&from)
	_, err := s.Pool.Exec(ctx, `
UPDATE events SET device_id=$2, release_id=$3, artifact_id=$4, issue_id=COALESCE($5, issue_id), state=$6, fingerprint=CASE WHEN $7<>'' THEN $7 ELSE fingerprint END,
  decoded=$8, analysis=$9, frames=$10, processed_at=now(), process_version=COALESCE(process_version,0)+1, reprocess_requested=false
WHERE id=$1`, eventID, deviceID, releaseID, artifactID, issueID, state, fp, decoded, analysis, frames)
	if err != nil {
		return err
	}
	_, _ = s.Pool.Exec(ctx, `INSERT INTO event_state_history(event_id,from_state,to_state,reason) VALUES($1,$2,$3,'worker')`, eventID, from, state)
	if issueID != nil {
		_, _ = s.Pool.Exec(ctx, `
UPDATE issues SET affected_devices=(SELECT COUNT(DISTINCT device_id) FROM events WHERE issue_id=$1 AND device_id IS NOT NULL)
WHERE id=$1`, *issueID)
	}
	return nil
}

// MarkEventProcessing sets state processing if not already ready (idempotent reprocess).
func (s *Store) MarkEventProcessing(ctx context.Context, eventID uuid.UUID) (alreadyDone bool, err error) {
	var state string
	var reprocess bool
	err = s.Pool.QueryRow(ctx, `SELECT state, COALESCE(reprocess_requested,false) FROM events WHERE id=$1 FOR UPDATE`, eventID).Scan(&state, &reprocess)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		// FOR UPDATE outside tx may fail on some drivers — fall back without lock
		err = s.Pool.QueryRow(ctx, `SELECT state, COALESCE(reprocess_requested,false) FROM events WHERE id=$1`, eventID).Scan(&state, &reprocess)
		if err != nil {
			return false, err
		}
	}
	if state == "ready" && !reprocess {
		return true, nil
	}
	from := state
	_, err = s.Pool.Exec(ctx, `UPDATE events SET state='processing' WHERE id=$1`, eventID)
	if err != nil {
		return false, err
	}
	_, _ = s.Pool.Exec(ctx, `INSERT INTO event_state_history(event_id,from_state,to_state,reason) VALUES($1,$2,'processing','worker_start')`, eventID, from)
	return false, nil
}

func (s *Store) RequestReprocess(ctx context.Context, projectID, eventID uuid.UUID) error {
	ct, err := s.Pool.Exec(ctx, `
UPDATE events SET reprocess_requested=true, state='persisted' WHERE id=$1 AND project_id=$2`, eventID, projectID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	payload, _ := json.Marshal(map[string]string{
		"event_id": eventID.String(), "project_id": projectID.String(),
	})
	_, err = s.Pool.Exec(ctx, `INSERT INTO jobs(type,payload,status,project_id) VALUES('process_event',$1,'ready',$2)`, payload, projectID)
	return err
}

func (s *Store) ListIssues(ctx context.Context, projectID uuid.UUID, limit int) ([]Issue, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,project_id,fingerprint,title,status,severity,event_count,affected_devices,first_seen,last_seen,probable_cause,resolved_at,COALESCE(regression_count,0)
FROM issues WHERE project_id=$1 ORDER BY last_seen DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Issue
	for rows.Next() {
		var i Issue
		if err := rows.Scan(&i.ID, &i.ProjectID, &i.Fingerprint, &i.Title, &i.Status, &i.Severity, &i.EventCount, &i.AffectedDevices, &i.FirstSeen, &i.LastSeen, &i.ProbableCause, &i.ResolvedAt, &i.RegressionCount); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) GetIssue(ctx context.Context, id uuid.UUID) (Issue, error) {
	return s.GetIssueInProject(ctx, uuid.Nil, id)
}

func (s *Store) GetIssueInProject(ctx context.Context, projectID, id uuid.UUID) (Issue, error) {
	var i Issue
	q := `
SELECT id,project_id,fingerprint,title,status,severity,event_count,affected_devices,first_seen,last_seen,probable_cause,resolved_at,COALESCE(regression_count,0),assignee_user_id
FROM issues WHERE id=$1`
	args := []any{id}
	if projectID != uuid.Nil {
		q += ` AND project_id=$2`
		args = append(args, projectID)
	}
	err := s.Pool.QueryRow(ctx, q, args...).Scan(&i.ID, &i.ProjectID, &i.Fingerprint, &i.Title, &i.Status, &i.Severity, &i.EventCount, &i.AffectedDevices, &i.FirstSeen, &i.LastSeen, &i.ProbableCause, &i.ResolvedAt, &i.RegressionCount, &i.AssigneeUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Issue{}, ErrNotFound
	}
	return i, err
}

func (s *Store) ListDevices(ctx context.Context, projectID uuid.UUID, limit int) ([]Device, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,project_id,device_id,product,hardware_revision,firmware_version,build_id,status,first_seen,last_seen
FROM devices WHERE project_id=$1 ORDER BY last_seen DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.DeviceID, &d.Product, &d.HardwareRevision, &d.FirmwareVersion, &d.BuildID, &d.Status, &d.FirstSeen, &d.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) DefaultProject(ctx context.Context) (Project, error) {
	var p Project
	err := s.Pool.QueryRow(ctx, `SELECT id,organization_id,name,slug,description FROM projects ORDER BY created_at ASC LIMIT 1`).
		Scan(&p.ID, &p.OrganizationID, &p.Name, &p.Slug, &p.Description)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return p, err
}

func (s *Store) ProjectForSession(ctx context.Context, orgID uuid.UUID, projectID *uuid.UUID) (Project, error) {
	if projectID != nil {
		var p Project
		err := s.Pool.QueryRow(ctx, `
SELECT id,organization_id,name,slug,description FROM projects WHERE id=$1 AND organization_id=$2`,
			*projectID, orgID).Scan(&p.ID, &p.OrganizationID, &p.Name, &p.Slug, &p.Description)
		if errors.Is(err, pgx.ErrNoRows) {
			return Project{}, ErrNotFound
		}
		return p, err
	}
	var p Project
	err := s.Pool.QueryRow(ctx, `
SELECT id,organization_id,name,slug,description FROM projects WHERE organization_id=$1 ORDER BY created_at ASC LIMIT 1`,
		orgID).Scan(&p.ID, &p.OrganizationID, &p.Name, &p.Slug, &p.Description)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return p, err
}

func (s *Store) UserCanAccessProject(ctx context.Context, userID, projectID uuid.UUID) (bool, string, error) {
	var role string
	err := s.Pool.QueryRow(ctx, `
SELECT m.role FROM memberships m
JOIN projects p ON p.organization_id=m.organization_id
WHERE m.user_id=$1 AND p.id=$2`, userID, projectID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	return true, role, nil
}

func (s *Store) Overview(ctx context.Context, projectID uuid.UUID) (map[string]any, error) {
	var devices, openIssues, eventsToday, regressions, resolved, fatalOpen, unhealthy int64
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM devices WHERE project_id=$1`, projectID).Scan(&devices)
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM issues WHERE project_id=$1 AND status='open'`, projectID).Scan(&openIssues)
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM events WHERE project_id=$1 AND received_at > now() - interval '24 hours'`, projectID).Scan(&eventsToday)
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM issues WHERE project_id=$1 AND COALESCE(regression_count,0)>0`, projectID).Scan(&regressions)
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM issues WHERE project_id=$1 AND status='resolved'`, projectID).Scan(&resolved)
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM issues WHERE project_id=$1 AND status='open' AND severity='fatal'`, projectID).Scan(&fatalOpen)
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM devices WHERE project_id=$1 AND status='unhealthy'`, projectID).Scan(&unhealthy)

	// events per day last 14 days
	rows, err := s.Pool.Query(ctx, `
SELECT date_trunc('day', received_at)::date AS d, COUNT(*)
FROM events WHERE project_id=$1 AND received_at > now() - interval '14 days'
GROUP BY 1 ORDER BY 1`, projectID)
	trend := []map[string]any{}
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var d time.Time
			var n int64
			if rows.Scan(&d, &n) == nil {
				trend = append(trend, map[string]any{"date": d.Format("2006-01-02"), "count": n})
			}
		}
	}

	// severity breakdown (7d)
	sevRows, _ := s.Pool.Query(ctx, `
SELECT severity, COUNT(*) FROM events
WHERE project_id=$1 AND received_at > now() - interval '7 days'
GROUP BY 1 ORDER BY 2 DESC`, projectID)
	bySev := []map[string]any{}
	if sevRows != nil {
		defer sevRows.Close()
		for sevRows.Next() {
			var sev string
			var n int64
			if sevRows.Scan(&sev, &n) == nil {
				bySev = append(bySev, map[string]any{"severity": sev, "count": n})
			}
		}
	}

	// architecture breakdown
	archRows, _ := s.Pool.Query(ctx, `
SELECT architecture, COUNT(*) FROM events
WHERE project_id=$1 AND received_at > now() - interval '14 days'
GROUP BY 1 ORDER BY 2 DESC`, projectID)
	byArch := []map[string]any{}
	if archRows != nil {
		defer archRows.Close()
		for archRows.Next() {
			var arch int16
			var n int64
			if archRows.Scan(&arch, &n) == nil {
				name := map[int16]string{1: "cortex-m", 2: "riscv", 3: "xtensa", 4: "linux"}[arch]
				if name == "" {
					name = fmt.Sprintf("arch-%d", arch)
				}
				byArch = append(byArch, map[string]any{"architecture": arch, "name": name, "count": n})
			}
		}
	}

	// pipeline breakdown
	pipeRows, _ := s.Pool.Query(ctx, `
SELECT COALESCE(pipeline,'issue'), COUNT(*) FROM events
WHERE project_id=$1 AND received_at > now() - interval '7 days'
GROUP BY 1 ORDER BY 2 DESC`, projectID)
	byPipe := []map[string]any{}
	if pipeRows != nil {
		defer pipeRows.Close()
		for pipeRows.Next() {
			var p string
			var n int64
			if pipeRows.Scan(&p, &n) == nil {
				byPipe = append(byPipe, map[string]any{"pipeline": p, "count": n})
			}
		}
	}

	// top issues
	topRows, _ := s.Pool.Query(ctx, `
SELECT id, title, severity, status, event_count, affected_devices, last_seen
FROM issues WHERE project_id=$1 ORDER BY last_seen DESC LIMIT 8`, projectID)
	topIssues := []map[string]any{}
	if topRows != nil {
		defer topRows.Close()
		for topRows.Next() {
			var id uuid.UUID
			var title, sev, status string
			var ec, ad int64
			var ls time.Time
			if topRows.Scan(&id, &title, &sev, &status, &ec, &ad, &ls) == nil {
				topIssues = append(topIssues, map[string]any{
					"id": id, "title": title, "severity": sev, "status": status,
					"event_count": ec, "affected_devices": ad, "last_seen": ls,
				})
			}
		}
	}

	// hourly last 24h for sparkline density
	hourRows, _ := s.Pool.Query(ctx, `
SELECT date_trunc('hour', received_at) AS h, COUNT(*)
FROM events WHERE project_id=$1 AND received_at > now() - interval '24 hours'
GROUP BY 1 ORDER BY 1`, projectID)
	hourly := []map[string]any{}
	if hourRows != nil {
		defer hourRows.Close()
		for hourRows.Next() {
			var h time.Time
			var n int64
			if hourRows.Scan(&h, &n) == nil {
				hourly = append(hourly, map[string]any{"hour": h.Format(time.RFC3339), "count": n})
			}
		}
	}

	// dual series: total events vs fatal per day (14d) for dual area chart
	fatalTrend := []map[string]any{}
	fRows, _ := s.Pool.Query(ctx, `
SELECT date_trunc('day', received_at)::date AS d, COUNT(*)
FROM events WHERE project_id=$1 AND severity='fatal' AND received_at > now() - interval '14 days'
GROUP BY 1 ORDER BY 1`, projectID)
	if fRows != nil {
		defer fRows.Close()
		for fRows.Next() {
			var d time.Time
			var n int64
			if fRows.Scan(&d, &n) == nil {
				fatalTrend = append(fatalTrend, map[string]any{"date": d.Format("2006-01-02"), "count": n})
			}
		}
	}

	// issues opened per day
	issueTrend := []map[string]any{}
	iRows, _ := s.Pool.Query(ctx, `
SELECT date_trunc('day', first_seen)::date AS d, COUNT(*)
FROM issues WHERE project_id=$1 AND first_seen > now() - interval '14 days'
GROUP BY 1 ORDER BY 1`, projectID)
	if iRows != nil {
		defer iRows.Close()
		for iRows.Next() {
			var d time.Time
			var n int64
			if iRows.Scan(&d, &n) == nil {
				issueTrend = append(issueTrend, map[string]any{"date": d.Format("2006-01-02"), "count": n})
			}
		}
	}

	// status breakdown of issues
	statusBreak := []map[string]any{}
	sRows, _ := s.Pool.Query(ctx, `
SELECT status, COUNT(*) FROM issues WHERE project_id=$1 GROUP BY 1 ORDER BY 2 DESC`, projectID)
	if sRows != nil {
		defer sRows.Close()
		for sRows.Next() {
			var st string
			var n int64
			if sRows.Scan(&st, &n) == nil {
				statusBreak = append(statusBreak, map[string]any{"status": st, "count": n})
			}
		}
	}

	// Nightwatch-style stacked severity per hour (24h): ok / warn / err
	sevHourly := []map[string]any{}
	shRows, _ := s.Pool.Query(ctx, `
SELECT date_trunc('hour', received_at) AS h,
  COUNT(*) FILTER (WHERE lower(severity) IN ('info','debug','ok','notice')) AS ok,
  COUNT(*) FILTER (WHERE lower(severity) IN ('warning','warn')) AS warn,
  COUNT(*) FILTER (WHERE lower(severity) IN ('error','fatal','critical')) AS err
FROM events WHERE project_id=$1 AND received_at > now() - interval '24 hours'
GROUP BY 1 ORDER BY 1`, projectID)
	if shRows != nil {
		defer shRows.Close()
		for shRows.Next() {
			var h time.Time
			var ok, warn, errn int64
			if shRows.Scan(&h, &ok, &warn, &errn) == nil {
				sevHourly = append(sevHourly, map[string]any{
					"hour": h.Format(time.RFC3339),
					"ok":   ok,
					"warn": warn,
					"err":  errn,
				})
			}
		}
	}

	// Exception handled/unhandled per hour (resolved-like vs open activity via events)
	// Use issue status counts as totals; hourly open-activity from new issues + events severity
	excHourly := []map[string]any{}
	ehRows, _ := s.Pool.Query(ctx, `
SELECT date_trunc('hour', e.received_at) AS h,
  COUNT(*) FILTER (WHERE i.status = 'resolved') AS handled,
  COUNT(*) FILTER (WHERE i.status IS DISTINCT FROM 'resolved') AS unhandled
FROM events e
LEFT JOIN issues i ON i.id = e.issue_id
WHERE e.project_id=$1 AND e.received_at > now() - interval '24 hours'
GROUP BY 1 ORDER BY 1`, projectID)
	if ehRows != nil {
		defer ehRows.Close()
		for ehRows.Next() {
			var h time.Time
			var handled, unhandled int64
			if ehRows.Scan(&h, &handled, &unhandled) == nil {
				excHourly = append(excHourly, map[string]any{
					"hour":      h.Format(time.RFC3339),
					"handled":   handled,
					"unhandled": unhandled,
				})
			}
		}
	}

	// severity totals 24h for legend chips
	var ok24, warn24, err24 int64
	_ = s.Pool.QueryRow(ctx, `
SELECT
  COUNT(*) FILTER (WHERE lower(severity) IN ('info','debug','ok','notice')),
  COUNT(*) FILTER (WHERE lower(severity) IN ('warning','warn')),
  COUNT(*) FILTER (WHERE lower(severity) IN ('error','fatal','critical'))
FROM events WHERE project_id=$1 AND received_at > now() - interval '24 hours'`, projectID).Scan(&ok24, &warn24, &err24)

	var eventsTotal, issuesTotal int64
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM events WHERE project_id=$1`, projectID).Scan(&eventsTotal)
	_ = s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM issues WHERE project_id=$1`, projectID).Scan(&issuesTotal)

	return map[string]any{
		"devices":           devices,
		"open_issues":       openIssues,
		"events_today":      eventsToday,
		"regressions":       regressions,
		"resolved_issues":   resolved,
		"fatal_open":        fatalOpen,
		"unhealthy_devices": unhealthy,
		"events_total":      eventsTotal,
		"issues_total":      issuesTotal,
		"events_trend":      trend,
		"fatal_trend":       fatalTrend,
		"issues_trend":      issueTrend,
		"by_severity":       bySev,
		"by_architecture":   byArch,
		"by_pipeline":       byPipe,
		"by_status":         statusBreak,
		"top_issues":        topIssues,
		"events_hourly":     hourly,
		"severity_hourly":   sevHourly,
		"exception_hourly":  excHourly,
		"severity_24h": map[string]any{
			"ok": ok24, "warn": warn24, "err": err24,
		},
	}, nil
}

// PurgeOldEvents deletes events older than retentionDays and returns object keys for GC.
func (s *Store) PurgeOldEvents(ctx context.Context, retentionDays int, limit int) ([]string, error) {
	if retentionDays <= 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.Pool.Query(ctx, `
DELETE FROM events WHERE id IN (
  SELECT id FROM events WHERE received_at < now() - ($1 || ' days')::interval ORDER BY received_at ASC LIMIT $2
) RETURNING raw_object_key`, fmt.Sprintf("%d", retentionDays), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// OrphanObjectKeys is deprecated: use ReferencedObjectKeys + objects.ListKeys / FS walk via gc.Runner.
// Kept for API stability; returns keys that exist in orphan_gc_log for diagnostics.
func (s *Store) ListObjectKeys(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.Pool.Query(ctx, `
SELECT raw_object_key FROM events
UNION
SELECT object_key FROM artifacts
LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			out = append(out, k)
		}
	}
	return out, rows.Err()
}

// --- Health Scores ---

type HealthScore struct {
	ID         uuid.UUID       `json:"id"`
	ProjectID  uuid.UUID       `json:"project_id"`
	DeviceID   string          `json:"device_id"`
	Score      float64         `json:"score"`
	Status     string          `json:"status"`
	Components json.RawMessage `json:"components"`
	Updated    time.Time       `json:"updated_at"`
}

// ListHealthScores returns health scores for a project, ordered by score descending.
// Maps the device_health_scores schema (overall_score/assessment/computed_at)
// onto the API-facing HealthScore shape.
func (s *Store) ListHealthScores(ctx context.Context, projectID uuid.UUID, limit int) ([]HealthScore, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id, project_id, device_id::text, overall_score, assessment,
  jsonb_build_object('crash_factor', crash_rate, 'uptime_factor', crash_free_rate,
                     'boot_failure_factor', boot_failure_rate,
                     'error_count', error_count, 'fatal_count', fatal_count),
  computed_at
FROM device_health_scores WHERE project_id=$1 ORDER BY overall_score DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HealthScore
	for rows.Next() {
		var h HealthScore
		if err := rows.Scan(&h.ID, &h.ProjectID, &h.DeviceID, &h.Score, &h.Status, &h.Components, &h.Updated); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// UpsertHealthScore inserts or updates a device health score.
func (s *Store) UpsertHealthScore(ctx context.Context, projectID uuid.UUID, deviceID string, score float64, status string, components json.RawMessage) (HealthScore, error) {
	if components == nil {
		components = json.RawMessage(`{}`)
	}
	var h HealthScore
	err := s.Pool.QueryRow(ctx, `
INSERT INTO device_health_scores(device_id, project_id, overall_score, assessment, computed_at)
VALUES($1::uuid, $2, $3, $4, now())
ON CONFLICT (device_id) DO UPDATE SET
  overall_score=EXCLUDED.overall_score, assessment=EXCLUDED.assessment,
  computed_at=now()
RETURNING id, project_id, device_id::text, overall_score, assessment, $5::jsonb, computed_at`,
		deviceID, projectID, score, status, components).
		Scan(&h.ID, &h.ProjectID, &h.DeviceID, &h.Score, &h.Status, &h.Components, &h.Updated)
	return h, err
}

// --- Device DNA ---

type DeviceDNA struct {
	ID            uuid.UUID `json:"id"`
	ProjectID     uuid.UUID `json:"project_id"`
	DeviceID      string    `json:"device_id"`
	Fingerprint   string    `json:"fingerprint"`
	MCUUID        string    `json:"mcu_uuid"`
	BootAvg       float64   `json:"boot_avg"`
	BootStddev    float64   `json:"boot_stddev"`
	ClockFreqMHz  float64   `json:"clock_freq_mhz"`
	FlashWearPct  float64   `json:"flash_wear_pct"`
	BootloaderSig string    `json:"bootloader_sig"`
	Updated       time.Time `json:"updated_at"`
}

// ListDeviceDNAs returns device DNA records, ordered by updated_at descending.
func (s *Store) ListDeviceDNAs(ctx context.Context, projectID uuid.UUID, limit int) ([]DeviceDNA, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,project_id,device_id,fingerprint,mcu_uuid,boot_avg,boot_stddev,clock_freq_mhz,flash_wear_pct,bootloader_sig,updated_at
FROM device_dna WHERE project_id=$1 ORDER BY updated_at DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeviceDNA
	for rows.Next() {
		var d DeviceDNA
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.DeviceID, &d.Fingerprint, &d.MCUUID, &d.BootAvg, &d.BootStddev, &d.ClockFreqMHz, &d.FlashWearPct, &d.BootloaderSig, &d.Updated); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// UpsertDeviceDNA inserts or updates a device DNA record.
func (s *Store) UpsertDeviceDNA(ctx context.Context, projectID uuid.UUID, deviceID, fingerprint, mcuUUID string, bootAvg, bootStddev, clockFreqMHz, flashWearPct float64, bootloaderSig string) (DeviceDNA, error) {
	var d DeviceDNA
	err := s.Pool.QueryRow(ctx, `
INSERT INTO device_dna(project_id,device_id,fingerprint,mcu_uuid,boot_avg,boot_stddev,clock_freq_mhz,flash_wear_pct,bootloader_sig,updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,now())
ON CONFLICT (project_id, device_id) DO UPDATE SET
  fingerprint=EXCLUDED.fingerprint, mcu_uuid=EXCLUDED.mcu_uuid,
  boot_avg=EXCLUDED.boot_avg, boot_stddev=EXCLUDED.boot_stddev,
  clock_freq_mhz=EXCLUDED.clock_freq_mhz, flash_wear_pct=EXCLUDED.flash_wear_pct,
  bootloader_sig=EXCLUDED.bootloader_sig, updated_at=now()
RETURNING id,project_id,device_id,fingerprint,mcu_uuid,boot_avg,boot_stddev,clock_freq_mhz,flash_wear_pct,bootloader_sig,updated_at`,
		projectID, deviceID, fingerprint, mcuUUID, bootAvg, bootStddev, clockFreqMHz, flashWearPct, bootloaderSig).
		Scan(&d.ID, &d.ProjectID, &d.DeviceID, &d.Fingerprint, &d.MCUUID, &d.BootAvg, &d.BootStddev, &d.ClockFreqMHz, &d.FlashWearPct, &d.BootloaderSig, &d.Updated)
	return d, err
}

// --- Chaos Injections ---

type ChaosResult struct {
	ID        uuid.UUID       `json:"id"`
	DeviceID  string          `json:"device_id"`
	Type      string          `json:"type"`
	Status    string          `json:"status"`
	Captured  json.RawMessage `json:"captured"`
	Duration  time.Duration   `json:"duration"`
	Error     string          `json:"error,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// ListChaosResults returns chaos injection results, ordered by timestamp descending.
func (s *Store) ListChaosResults(ctx context.Context, limit int) ([]ChaosResult, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,device_id,type,status,captured,duration,error,timestamp
FROM chaos_injections ORDER BY timestamp DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChaosResult
	for rows.Next() {
		var c ChaosResult
		if err := rows.Scan(&c.ID, &c.DeviceID, &c.Type, &c.Status, &c.Captured, &c.Duration, &c.Error, &c.Timestamp); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreateChaosResult inserts a chaos injection result record.
func (s *Store) CreateChaosResult(ctx context.Context, deviceID, injType, status string, captured json.RawMessage, duration time.Duration, err string) (ChaosResult, error) {
	if captured == nil {
		captured = json.RawMessage(`{}`)
	}
	var c ChaosResult
	errInsert := s.Pool.QueryRow(ctx, `
INSERT INTO chaos_injections(device_id,type,status,captured,duration,error)
VALUES($1,$2,$3,$4,$5,$6)
RETURNING id,device_id,type,status,captured,duration,error,timestamp`,
		deviceID, injType, status, captured, duration, err).
		Scan(&c.ID, &c.DeviceID, &c.Type, &c.Status, &c.Captured, &c.Duration, &c.Error, &c.Timestamp)
	return c, errInsert
}

// --- Anomaly Events ---

type AnomalyEvent struct {
	ID           uuid.UUID `json:"id"`
	ProjectID    uuid.UUID `json:"project_id"`
	DeviceID     string    `json:"device_id"`
	Metric       string    `json:"metric"`
	Value        float64   `json:"value"`
	ThresholdMin float64   `json:"threshold_min"`
	ThresholdMax float64   `json:"threshold_max"`
	Timestamp    time.Time `json:"timestamp"`
}

// ListAnomalyEvents returns anomaly events for a project, ordered by timestamp descending.
func (s *Store) ListAnomalyEvents(ctx context.Context, projectID uuid.UUID, limit int) ([]AnomalyEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,project_id,device_id,metric,value,threshold_min,threshold_max,timestamp
FROM anomaly_events WHERE project_id=$1 ORDER BY timestamp DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AnomalyEvent
	for rows.Next() {
		var a AnomalyEvent
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.DeviceID, &a.Metric, &a.Value, &a.ThresholdMin, &a.ThresholdMax, &a.Timestamp); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CreateAnomalyEvent inserts an anomaly event record.
func (s *Store) CreateAnomalyEvent(ctx context.Context, projectID uuid.UUID, deviceID, metric string, value, thresholdMin, thresholdMax float64) (AnomalyEvent, error) {
	var a AnomalyEvent
	err := s.Pool.QueryRow(ctx, `
INSERT INTO anomaly_events(project_id,device_id,metric,value,threshold_min,threshold_max,timestamp)
VALUES($1,$2,$3,$4,$5,$6,now())
RETURNING id,project_id,device_id,metric,value,threshold_min,threshold_max,timestamp`,
		projectID, deviceID, metric, value, thresholdMin, thresholdMax).
		Scan(&a.ID, &a.ProjectID, &a.DeviceID, &a.Metric, &a.Value, &a.ThresholdMin, &a.ThresholdMax, &a.Timestamp)
	return a, err
}

// --- Postmortem Reports ---

type PostmortemReport struct {
	ID          uuid.UUID `json:"id"`
	ProjectID   uuid.UUID `json:"project_id"`
	IssueID     uuid.UUID `json:"issue_id"`
	Markdown    string    `json:"markdown"`
	Status      string    `json:"status"`
	GeneratedAt time.Time `json:"generated_at"`
}

// ListPostmortems returns postmortem reports for a project, ordered by generated_at descending.
func (s *Store) ListPostmortems(ctx context.Context, projectID uuid.UUID, limit int) ([]PostmortemReport, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,project_id,issue_id,markdown,status,generated_at
FROM postmortem_reports WHERE project_id=$1 ORDER BY generated_at DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PostmortemReport
	for rows.Next() {
		var p PostmortemReport
		if err := rows.Scan(&p.ID, &p.ProjectID, &p.IssueID, &p.Markdown, &p.Status, &p.GeneratedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreatePostmortem inserts a postmortem report record.
func (s *Store) CreatePostmortem(ctx context.Context, projectID, issueID uuid.UUID, markdown, status string) (PostmortemReport, error) {
	var p PostmortemReport
	err := s.Pool.QueryRow(ctx, `
INSERT INTO postmortem_reports(project_id,issue_id,markdown,status,generated_at)
VALUES($1,$2,$3,$4,now())
RETURNING id,project_id,issue_id,markdown,status,generated_at`,
		projectID, issueID, markdown, status).
		Scan(&p.ID, &p.ProjectID, &p.IssueID, &p.Markdown, &p.Status, &p.GeneratedAt)
	return p, err
}

// --- Issue Comments ---

// Note: ListIssueComments and AddIssueComment are defined in phase2.go
// Note: ListEventsByIssueInProject is defined in complete.go

// --- PR Records ---

type PRRecord struct {
	ID         uuid.UUID `json:"id"`
	ProjectID  uuid.UUID `json:"project_id"`
	IssueID    uuid.UUID `json:"issue_id"`
	PRURL      string    `json:"pr_url"`
	PRNumber   int       `json:"pr_number"`
	BranchName string    `json:"branch_name"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

// ListPRRecords returns PR records for a project, ordered by created_at descending.
func (s *Store) ListPRRecords(ctx context.Context, projectID uuid.UUID, limit int) ([]PRRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,project_id,issue_id,pr_url,pr_number,branch_name,status,created_at
FROM pr_records WHERE project_id=$1 ORDER BY created_at DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PRRecord
	for rows.Next() {
		var p PRRecord
		if err := rows.Scan(&p.ID, &p.ProjectID, &p.IssueID, &p.PRURL, &p.PRNumber, &p.BranchName, &p.Status, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreatePRRecord inserts a PR record linking a PR to an issue.
func (s *Store) CreatePRRecord(ctx context.Context, projectID, issueID uuid.UUID, prURL string, prNumber int, branchName, status string) (PRRecord, error) {
	var p PRRecord
	err := s.Pool.QueryRow(ctx, `
INSERT INTO pr_records(project_id,issue_id,pr_url,pr_number,branch_name,status,created_at)
VALUES($1,$2,$3,$4,$5,$6,now())
RETURNING id,project_id,issue_id,pr_url,pr_number,branch_name,status,created_at`,
		projectID, issueID, prURL, prNumber, branchName, status).
		Scan(&p.ID, &p.ProjectID, &p.IssueID, &p.PRURL, &p.PRNumber, &p.BranchName, &p.Status, &p.CreatedAt)
	return p, err
}

// --- Memorial Devices ---

type MemorialDevice struct {
	DeviceID        string    `json:"device_id"`
	Product         string    `json:"product"`
	FirmwareVersion string    `json:"firmware_version"`
	BuildID         string    `json:"build_id"`
	Status          string    `json:"status"`
	FirstSeen       time.Time `json:"first_seen"`
	LastSeen        time.Time `json:"last_seen"`
	EventCount      int64     `json:"event_count"`
	UniqueIssues    int64     `json:"unique_issues"`
}

// ListMemorialDevices returns anonymized device data for devices that have not been seen recently.
func (s *Store) ListMemorialDevices(ctx context.Context, days int) ([]MemorialDevice, error) {
	if days <= 0 {
		days = 30
	}
	rows, err := s.Pool.Query(ctx, `
SELECT devices.device_id, devices.product, devices.firmware_version, devices.build_id, devices.status,
  devices.first_seen, devices.last_seen,
  COUNT(DISTINCT e.id) AS event_count,
  COUNT(DISTINCT e.issue_id) AS unique_issues
FROM devices
LEFT JOIN events e ON e.device_id = devices.id
WHERE devices.last_seen < now() - ($1 || ' days')::interval
GROUP BY devices.device_id, devices.product, devices.firmware_version, devices.build_id,
  devices.status, devices.first_seen, devices.last_seen
ORDER BY devices.last_seen ASC`, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MemorialDevice
	for rows.Next() {
		var m MemorialDevice
		if err := rows.Scan(&m.DeviceID, &m.Product, &m.FirmwareVersion, &m.BuildID, &m.Status, &m.FirstSeen, &m.LastSeen, &m.EventCount, &m.UniqueIssues); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// --- Crash Stats ---

type CrashStat struct {
	Architecture  string `json:"architecture"`
	Region        string `json:"region"`
	Severity      string `json:"severity"`
	CrashCount    int64  `json:"crash_count"`
	UniqueDevices int64  `json:"unique_devices"`
}

// PublicCrashStats returns aggregated crash statistics grouped by architecture, region, severity.
func (s *Store) PublicCrashStats(ctx context.Context, days int, arch, region, severity string) ([]CrashStat, error) {
	if days <= 0 {
		days = 30
	}
	var q string
	var args []any
	argIdx := 1
	q = `
SELECT COALESCE(a.architecture,'unknown') AS architecture,
  COALESCE(i.region,'unknown') AS region,
  COALESCE(e.severity,'unknown') AS severity,
  COUNT(*) AS crash_count,
  COUNT(DISTINCT e.device_id) AS unique_devices
FROM events e
LEFT JOIN artifacts a ON a.id = e.artifact_id
LEFT JOIN issues i ON i.id = e.issue_id
WHERE e.received_at > now() - ($1 || ' days')::interval AND e.severity IN ('fatal','error')`
	args = append(args, days)
	if arch != "" {
		q += fmt.Sprintf(` AND a.architecture=$%d`, argIdx)
		args = append(args, arch)
		argIdx++
	}
	if region != "" {
		q += fmt.Sprintf(` AND i.region=$%d`, argIdx)
		args = append(args, region)
		argIdx++
	}
	if severity != "" {
		q += fmt.Sprintf(` AND e.severity=$%d`, argIdx)
		args = append(args, severity)
		argIdx++
	}
	q += ` GROUP BY a.architecture, i.region, e.severity ORDER BY crash_count DESC`

	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CrashStat
	for rows.Next() {
		var c CrashStat
		if err := rows.Scan(&c.Architecture, &c.Region, &c.Severity, &c.CrashCount, &c.UniqueDevices); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- Feature Flags ---

type FeatureFlag struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// GetFeatureFlag returns a feature flag by name.
func (s *Store) GetFeatureFlag(ctx context.Context, flagName string) (FeatureFlag, error) {
	var f FeatureFlag
	err := s.Pool.QueryRow(ctx, `SELECT name, enabled FROM feature_flags WHERE name=$1`, flagName).
		Scan(&f.Name, &f.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return FeatureFlag{}, ErrNotFound
	}
	return f, err
}

// SetFeatureFlag inserts or updates a feature flag.
func (s *Store) SetFeatureFlag(ctx context.Context, flagName string, enabled bool) (FeatureFlag, error) {
	var f FeatureFlag
	err := s.Pool.QueryRow(ctx, `
INSERT INTO feature_flags(name,enabled) VALUES($1,$2)
ON CONFLICT (name) DO UPDATE SET enabled=EXCLUDED.enabled
RETURNING name, enabled`, flagName, enabled).
		Scan(&f.Name, &f.Enabled)
	return f, err
}

// --- Postmortem Quota ---

type PostmortemQuota struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

// IncrementPostmortemQuota atomically increments the daily postmortem quota counter.
func (s *Store) IncrementPostmortemQuota(ctx context.Context, date string) (PostmortemQuota, error) {
	var q PostmortemQuota
	err := s.Pool.QueryRow(ctx, `
INSERT INTO postmortem_quota(date,count) VALUES($1,1)
ON CONFLICT (date) DO UPDATE SET count=postmortem_quota.count+1
RETURNING date, count`, date).
		Scan(&q.Date, &q.Count)
	return q, err
}

// GetPostmortemQuota returns the current quota count for a date.
func (s *Store) GetPostmortemQuota(ctx context.Context, date string) (PostmortemQuota, error) {
	var q PostmortemQuota
	err := s.Pool.QueryRow(ctx, `SELECT date, count FROM postmortem_quota WHERE date=$1`, date).
		Scan(&q.Date, &q.Count)
	if errors.Is(err, pgx.ErrNoRows) {
		return PostmortemQuota{Date: date, Count: 0}, nil
	}
	return q, err
}

// --- Global Search ---

func (s *Store) GlobalSearch(ctx context.Context, projectID uuid.UUID, q string, limit int) (map[string]any, error) {
	if limit <= 0 {
		limit = 20
	}
	like := "%" + q + "%"
	events, _ := s.Pool.Query(ctx, `
SELECT id,event_id,severity,state FROM events
WHERE project_id=$1 AND (event_id ILIKE $2 OR fingerprint ILIKE $2 OR raw_hash ILIKE $2)
ORDER BY received_at DESC LIMIT $3`, projectID, like, limit)
	var ev []map[string]any
	if events != nil {
		defer events.Close()
		for events.Next() {
			var id uuid.UUID
			var eid, sev, st string
			if events.Scan(&id, &eid, &sev, &st) == nil {
				ev = append(ev, map[string]any{"id": id, "event_id": eid, "severity": sev, "state": st})
			}
		}
	}
	issues, _ := s.Pool.Query(ctx, `
SELECT id,title,status,severity FROM issues
WHERE project_id=$1 AND (title ILIKE $2 OR fingerprint ILIKE $2)
ORDER BY last_seen DESC LIMIT $3`, projectID, like, limit)
	var is []map[string]any
	if issues != nil {
		defer issues.Close()
		for issues.Next() {
			var id uuid.UUID
			var title, status, sev string
			if issues.Scan(&id, &title, &status, &sev) == nil {
				is = append(is, map[string]any{"id": id, "title": title, "status": status, "severity": sev})
			}
		}
	}
	devices, _ := s.Pool.Query(ctx, `
SELECT id,device_id,status FROM devices
WHERE project_id=$1 AND (device_id ILIKE $2 OR product ILIKE $2 OR build_id ILIKE $2)
ORDER BY last_seen DESC LIMIT $3`, projectID, like, limit)
	var dev []map[string]any
	if devices != nil {
		defer devices.Close()
		for devices.Next() {
			var id uuid.UUID
			var did, st string
			if devices.Scan(&id, &did, &st) == nil {
				dev = append(dev, map[string]any{"id": id, "device_id": did, "status": st})
			}
		}
	}
	return map[string]any{"events": ev, "issues": is, "devices": dev, "query": q}, nil
}
