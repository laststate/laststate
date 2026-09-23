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

type AlertRule struct {
	ID          uuid.UUID  `json:"id"`
	ProjectID   uuid.UUID  `json:"project_id"`
	Name        string     `json:"name"`
	Kind        string     `json:"kind"`
	Enabled     bool       `json:"enabled"`
	Channel     string     `json:"channel"`
	TargetURL   string     `json:"target_url"`
	HasSecret   bool       `json:"has_secret"`
	CooldownSec int        `json:"cooldown_sec"`
	MaxRetries  int        `json:"max_retries"`
	LastFiredAt *time.Time `json:"last_fired_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	ChannelID   *uuid.UUID `json:"channel_id,omitempty"`
	Secret      string     `json:"-"`
}

type WebhookDelivery struct {
	ID         uuid.UUID       `json:"id"`
	EventType  string          `json:"event_type"`
	TargetURL  string          `json:"target_url"`
	StatusCode int             `json:"status_code"`
	Success    bool            `json:"success"`
	CreatedAt  time.Time       `json:"created_at"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

func (s *Store) CreateAlertRule(ctx context.Context, projectID uuid.UUID, name, kind, channel, target, secret string, cooldownSec, maxRetries int) (AlertRule, error) {
	if cooldownSec <= 0 {
		cooldownSec = 300
	}
	if maxRetries <= 0 {
		maxRetries = 3
	}
	secret = sealSecret(secret)
	var r AlertRule
	err := s.Pool.QueryRow(ctx, `
INSERT INTO alert_rules(project_id,name,kind,channel,target_url,secret,enabled,cooldown_sec,max_retries)
VALUES($1,$2,$3,$4,$5,$6,true,$7,$8)
RETURNING id,project_id,name,kind,enabled,channel,target_url,created_at,secret,COALESCE(cooldown_sec,300),COALESCE(max_retries,3),channel_id`,
		projectID, name, kind, channel, target, secret, cooldownSec, maxRetries).
		Scan(&r.ID, &r.ProjectID, &r.Name, &r.Kind, &r.Enabled, &r.Channel, &r.TargetURL, &r.CreatedAt, &r.Secret, &r.CooldownSec, &r.MaxRetries, &r.ChannelID)
	r.HasSecret = r.Secret != ""
	r.Secret = openSecret(r.Secret)
	return r, err
}

func (s *Store) UpdateAlertRule(ctx context.Context, projectID, id uuid.UUID, name string, enabled bool, target, secret string, cooldownSec int) (AlertRule, error) {
	if secret != "" {
		secret = sealSecret(secret)
	}
	var r AlertRule
	err := s.Pool.QueryRow(ctx, `
UPDATE alert_rules SET
  name=CASE WHEN $3<>'' THEN $3 ELSE name END,
  enabled=$4,
  target_url=CASE WHEN $5<>'' THEN $5 ELSE target_url END,
  secret=CASE WHEN $6<>'' THEN $6 ELSE secret END,
  cooldown_sec=CASE WHEN $7>0 THEN $7 ELSE cooldown_sec END
WHERE id=$1 AND project_id=$2
RETURNING id,project_id,name,kind,enabled,channel,target_url,created_at,secret,COALESCE(cooldown_sec,300),COALESCE(max_retries,3),last_fired_at`,
		id, projectID, name, enabled, target, secret, cooldownSec).
		Scan(&r.ID, &r.ProjectID, &r.Name, &r.Kind, &r.Enabled, &r.Channel, &r.TargetURL, &r.CreatedAt, &r.Secret, &r.CooldownSec, &r.MaxRetries, &r.LastFiredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AlertRule{}, ErrNotFound
	}
	r.HasSecret = r.Secret != ""
	r.Secret = ""
	return r, err
}

func (s *Store) ListAlertRules(ctx context.Context, projectID uuid.UUID) ([]AlertRule, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT id,project_id,name,kind,enabled,channel,target_url,created_at,secret,COALESCE(cooldown_sec,300),COALESCE(max_retries,3),last_fired_at,channel_id
FROM alert_rules WHERE project_id=$1 ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertRule
	for rows.Next() {
		var r AlertRule
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Name, &r.Kind, &r.Enabled, &r.Channel, &r.TargetURL, &r.CreatedAt, &r.Secret, &r.CooldownSec, &r.MaxRetries, &r.LastFiredAt, &r.ChannelID); err != nil {
			return nil, err
		}
		r.HasSecret = r.Secret != ""
		r.Secret = ""
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) RulesForKind(ctx context.Context, projectID uuid.UUID, kind string) ([]AlertRule, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT id,project_id,name,kind,enabled,channel,target_url,created_at,secret,COALESCE(cooldown_sec,300),COALESCE(max_retries,3),last_fired_at,channel_id
FROM alert_rules WHERE project_id=$1 AND enabled AND kind=$2`, projectID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertRule
	for rows.Next() {
		var r AlertRule
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Name, &r.Kind, &r.Enabled, &r.Channel, &r.TargetURL, &r.CreatedAt, &r.Secret, &r.CooldownSec, &r.MaxRetries, &r.LastFiredAt, &r.ChannelID); err != nil {
			return nil, err
		}
		r.Secret = openSecret(r.Secret)
		// apply cooldown
		if r.LastFiredAt != nil && r.CooldownSec > 0 {
			if time.Since(*r.LastFiredAt) < time.Duration(r.CooldownSec)*time.Second {
				continue
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) MarkAlertFired(ctx context.Context, ruleID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `UPDATE alert_rules SET last_fired_at=now() WHERE id=$1`, ruleID)
	return err
}

func (s *Store) RecordWebhook(ctx context.Context, projectID uuid.UUID, ruleID *uuid.UUID, eventType, target string, status int, ok bool, body string, payload any) error {
	raw, _ := json.Marshal(payload)
	_, err := s.Pool.Exec(ctx, `
INSERT INTO webhook_deliveries(project_id,rule_id,event_type,target_url,status_code,success,response_body,payload)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, projectID, ruleID, eventType, target, status, ok, body, raw)
	return err
}

func (s *Store) ListWebhookDeliveries(ctx context.Context, projectID uuid.UUID, limit int) ([]WebhookDelivery, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id,event_type,target_url,status_code,success,created_at,payload
FROM webhook_deliveries WHERE project_id=$1 ORDER BY created_at DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebhookDelivery
	for rows.Next() {
		var d WebhookDelivery
		if err := rows.Scan(&d.ID, &d.EventType, &d.TargetURL, &d.StatusCode, &d.Success, &d.CreatedAt, &d.Payload); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) EnqueueJobDelayed(ctx context.Context, typ string, payload any, delay time.Duration) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if delay < 0 {
		delay = 0
	}
	_, err = s.Pool.Exec(ctx, `
INSERT INTO jobs(type,payload,status,available_at) VALUES($1,$2,'ready',now() + $3::interval)`,
		typ, raw, fmt.Sprintf("%f seconds", delay.Seconds()))
	return err
}

func (s *Store) EnqueueJob(ctx context.Context, typ string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	// Best-effort extract project_id for multi-tenant job scoping
	var projectID *uuid.UUID
	if m, ok := payload.(map[string]string); ok {
		if s, ok := m["project_id"]; ok {
			if id, err := uuid.Parse(s); err == nil {
				projectID = &id
			}
		}
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO jobs(type,payload,status,project_id) VALUES($1,$2,'ready',$3)`, typ, raw, projectID)
	return err
}

// UpsertOIDCUser creates or finds user by email and mints a session for an
// existing membership only. Does NOT auto-join the first organization unless
// autoJoin is true (TRACE_OIDC_AUTO_JOIN — off by default).
func (s *Store) UpsertOIDCUser(ctx context.Context, email, name string) (User, Session, string, error) {
	return s.UpsertOIDCUserOpts(ctx, email, name, false)
}

func (s *Store) UpsertOIDCUserOpts(ctx context.Context, email, name string, autoJoin bool) (User, Session, string, error) {
	var u User
	err := s.Pool.QueryRow(ctx, `SELECT id,email,name FROM users WHERE email=$1`, email).Scan(&u.ID, &u.Email, &u.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		ph, _ := HashPassword("oidc-disabled-" + uuid.NewString())
		err = s.Pool.QueryRow(ctx, `INSERT INTO users(email,name,password_hash) VALUES($1,$2,$3) RETURNING id,email,name`,
			email, name, ph).Scan(&u.ID, &u.Email, &u.Name)
		if err != nil {
			return User{}, Session{}, "", err
		}
	} else if err != nil {
		return User{}, Session{}, "", err
	}
	var orgID uuid.UUID
	var role string
	err = s.Pool.QueryRow(ctx, `
SELECT organization_id, role FROM memberships WHERE user_id=$1 ORDER BY created_at ASC NULLS LAST LIMIT 1`, u.ID).
		Scan(&orgID, &role)
	if errors.Is(err, pgx.ErrNoRows) || orgID == uuid.Nil {
		if !autoJoin {
			return User{}, Session{}, "", fmt.Errorf("no organization membership for %s; invite required", email)
		}
		org, oerr := s.firstOrg(ctx)
		if oerr != nil {
			return User{}, Session{}, "", oerr
		}
		orgID = org.ID
		role = "viewer" // least privilege when auto-join is explicitly enabled
		_, _ = s.Pool.Exec(ctx, `INSERT INTO memberships(organization_id,user_id,role) VALUES($1,$2,$3) ON CONFLICT (organization_id, user_id) DO NOTHING`, orgID, u.ID, role)
	} else if err != nil {
		return User{}, Session{}, "", err
	}
	if name != "" && u.Name == "" {
		_, _ = s.Pool.Exec(ctx, `UPDATE users SET name=$2 WHERE id=$1`, u.ID, name)
		u.Name = name
	}
	sess, secret, err := s.MintSession(ctx, u, orgID, role)
	return u, sess, secret, err
}
