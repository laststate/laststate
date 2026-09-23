package store

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/laststate/trace/internal/auth"
)

var slugRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

func NormalizeSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

func (s *Store) CreateOrganization(ctx context.Context, name, slug string, ownerUserID uuid.UUID) (Org, error) {
	slug = NormalizeSlug(slug)
	if !slugRe.MatchString(slug) {
		return Org{}, errors.New("invalid slug")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Org{}, err
	}
	defer tx.Rollback(ctx)
	var o Org
	err = tx.QueryRow(ctx, `INSERT INTO organizations(name,slug) VALUES($1,$2) RETURNING id,name,slug`, name, slug).
		Scan(&o.ID, &o.Name, &o.Slug)
	if err != nil {
		return Org{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,'owner')`, o.ID, ownerUserID); err != nil {
		return Org{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Org{}, err
	}
	return o, nil
}

func (s *Store) UpdateOrganization(ctx context.Context, orgID uuid.UUID, name string) (Org, error) {
	var o Org
	err := s.Pool.QueryRow(ctx, `UPDATE organizations SET name=$2 WHERE id=$1 RETURNING id,name,slug`, orgID, name).
		Scan(&o.ID, &o.Name, &o.Slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return Org{}, ErrNotFound
	}
	return o, err
}

func (s *Store) ListOrganizationsForUser(ctx context.Context, userID uuid.UUID) ([]map[string]any, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT o.id, o.name, o.slug, m.role FROM organizations o
JOIN memberships m ON m.organization_id=o.id
WHERE m.user_id=$1 ORDER BY o.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var name, slug, role string
		if err := rows.Scan(&id, &name, &slug, &role); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "name": name, "slug": slug, "role": role})
	}
	return out, rows.Err()
}

func (s *Store) ListMembers(ctx context.Context, orgID uuid.UUID) ([]map[string]any, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT u.id, u.email, u.name, m.role, m.created_at
FROM memberships m JOIN users u ON u.id=m.user_id
WHERE m.organization_id=$1 ORDER BY m.created_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var email, name, role string
		var ts time.Time
		if err := rows.Scan(&id, &email, &name, &role, &ts); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "email": email, "name": name, "role": role, "joined_at": ts})
	}
	return out, rows.Err()
}

func (s *Store) AddMember(ctx context.Context, orgID, userID uuid.UUID, role string) error {
	switch role {
	case "viewer", "developer", "maintainer", "admin", "owner", "billing":
	default:
		role = "developer"
	}
	_, err := s.Pool.Exec(ctx, `
INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,$3)
ON CONFLICT (organization_id, user_id) DO UPDATE SET role=EXCLUDED.role`, orgID, userID, role)
	return err
}

func (s *Store) InviteMember(ctx context.Context, orgID uuid.UUID, email, role string, by *uuid.UUID) (string, error) {
	secret, _, hash, err := auth.Mint("lst_invite")
	if err != nil {
		return "", err
	}
	_, err = s.Pool.Exec(ctx, `
INSERT INTO organization_invites(organization_id,email,role,token_hash,invited_by,expires_at)
VALUES($1,$2,$3,$4,$5,now() + interval '7 days')`, orgID, email, role, hash, by)
	return secret, err
}

func (s *Store) CreateProject(ctx context.Context, orgID uuid.UUID, name, slug, desc string) (Project, error) {
	slug = NormalizeSlug(slug)
	if !slugRe.MatchString(slug) {
		return Project{}, errors.New("invalid slug")
	}
	var p Project
	err := s.Pool.QueryRow(ctx, `
INSERT INTO projects(organization_id,name,slug,description) VALUES($1,$2,$3,$4)
RETURNING id,organization_id,name,slug,description`, orgID, name, slug, desc).
		Scan(&p.ID, &p.OrganizationID, &p.Name, &p.Slug, &p.Description)
	return p, err
}

func (s *Store) UpdateProject(ctx context.Context, orgID, projectID uuid.UUID, name, desc string) (Project, error) {
	var p Project
	err := s.Pool.QueryRow(ctx, `
UPDATE projects SET name=$3, description=$4 WHERE id=$1 AND organization_id=$2
RETURNING id,organization_id,name,slug,description`, projectID, orgID, name, desc).
		Scan(&p.ID, &p.OrganizationID, &p.Name, &p.Slug, &p.Description)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return p, err
}

func (s *Store) DeleteProject(ctx context.Context, orgID, projectID uuid.UUID) error {
	ct, err := s.Pool.Exec(ctx, `DELETE FROM projects WHERE id=$1 AND organization_id=$2`, projectID, orgID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListProjects(ctx context.Context, orgID uuid.UUID) ([]Project, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT id,organization_id,name,slug,description FROM projects WHERE organization_id=$1 ORDER BY name`, orgID)
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

type Relay struct {
	ID            uuid.UUID       `json:"id"`
	ProjectID     uuid.UUID       `json:"project_id"`
	RelayID       string          `json:"relay_id"`
	Name          string          `json:"name"`
	Version       string          `json:"version"`
	Status        string          `json:"status"`
	Capabilities  json.RawMessage `json:"capabilities"`
	LastHeartbeat *time.Time      `json:"last_heartbeat,omitempty"`
}

func (s *Store) UpsertRelay(ctx context.Context, projectID uuid.UUID, relayID, name, version string, caps json.RawMessage) (Relay, error) {
	if caps == nil {
		caps = json.RawMessage(`{}`)
	}
	var r Relay
	err := s.Pool.QueryRow(ctx, `
INSERT INTO relays(project_id,relay_id,name,version,status,capabilities,last_heartbeat)
VALUES($1,$2,$3,$4,'online',$5,now())
ON CONFLICT (project_id, relay_id) DO UPDATE SET
  name=CASE WHEN EXCLUDED.name<>'' THEN EXCLUDED.name ELSE relays.name END,
  version=CASE WHEN EXCLUDED.version<>'' THEN EXCLUDED.version ELSE relays.version END,
  status='online', capabilities=EXCLUDED.capabilities, last_heartbeat=now()
RETURNING id,project_id,relay_id,name,version,status,capabilities,last_heartbeat`,
		projectID, relayID, name, version, caps).
		Scan(&r.ID, &r.ProjectID, &r.RelayID, &r.Name, &r.Version, &r.Status, &r.Capabilities, &r.LastHeartbeat)
	return r, err
}

func (s *Store) ListRelays(ctx context.Context, projectID uuid.UUID) ([]Relay, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT id,project_id,relay_id,name,version,status,capabilities,last_heartbeat
FROM relays WHERE project_id=$1 ORDER BY last_heartbeat DESC NULLS LAST`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Relay
	for rows.Next() {
		var r Relay
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.RelayID, &r.Name, &r.Version, &r.Status, &r.Capabilities, &r.LastHeartbeat); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) CreateHardwareRevision(ctx context.Context, projectID uuid.UUID, revision, bom, lot, notes string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.Pool.QueryRow(ctx, `
INSERT INTO hardware_revisions(project_id,revision,bom,lot,notes) VALUES($1,$2,$3,$4,$5)
ON CONFLICT (project_id, revision, lot) DO UPDATE SET bom=EXCLUDED.bom, notes=EXCLUDED.notes
RETURNING id`, projectID, revision, bom, lot, notes).Scan(&id)
	return id, err
}

func (s *Store) ListHardwareRevisions(ctx context.Context, projectID uuid.UUID) ([]map[string]any, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT id, revision, bom, lot, notes, created_at FROM hardware_revisions WHERE project_id=$1 ORDER BY revision`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var rev, bom, lot, notes string
		var ts time.Time
		if err := rows.Scan(&id, &rev, &bom, &lot, &notes, &ts); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "revision": rev, "bom": bom, "lot": lot, "notes": notes, "created_at": ts})
	}
	return out, rows.Err()
}
