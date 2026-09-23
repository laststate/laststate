package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type SAMLConfig struct {
	OrgID          uuid.UUID `json:"org_id"`
	Enabled        bool      `json:"enabled"`
	EntityID       string    `json:"entity_id"`
	SSOURL         string    `json:"sso_url"`
	CertificatePEM string    `json:"certificate_pem,omitempty"`
	ACSURL         string    `json:"acs_url"`
}

func (s *Store) GetSAMLConfig(ctx context.Context, orgID uuid.UUID) (SAMLConfig, error) {
	var c SAMLConfig
	err := s.Pool.QueryRow(ctx, `
SELECT org_id, enabled, entity_id, sso_url, certificate_pem, acs_url
FROM saml_configs WHERE org_id=$1`, orgID).
		Scan(&c.OrgID, &c.Enabled, &c.EntityID, &c.SSOURL, &c.CertificatePEM, &c.ACSURL)
	return c, err
}

func (s *Store) UpsertSAMLConfig(ctx context.Context, c SAMLConfig) (SAMLConfig, error) {
	err := s.Pool.QueryRow(ctx, `
INSERT INTO saml_configs(org_id,enabled,entity_id,sso_url,certificate_pem,acs_url)
VALUES($1,$2,$3,$4,$5,$6)
ON CONFLICT (org_id) DO UPDATE SET
  enabled=EXCLUDED.enabled, entity_id=EXCLUDED.entity_id, sso_url=EXCLUDED.sso_url,
  certificate_pem=EXCLUDED.certificate_pem, acs_url=EXCLUDED.acs_url
RETURNING org_id,enabled,entity_id,sso_url,certificate_pem,acs_url`,
		c.OrgID, c.Enabled, c.EntityID, c.SSOURL, c.CertificatePEM, c.ACSURL).
		Scan(&c.OrgID, &c.Enabled, &c.EntityID, &c.SSOURL, &c.CertificatePEM, &c.ACSURL)
	return c, err
}

// ExportEventsNDJSON pulls recent events as maps for warehouse / analytics sink.
func (s *Store) ExportEventsNDJSON(ctx context.Context, projectID uuid.UUID, since time.Time, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 1000
	}
	if limit > 10000 {
		limit = 10000
	}
	if since.IsZero() {
		since = time.Now().Add(-24 * time.Hour)
	}
	rows, err := s.Pool.Query(ctx, `
SELECT e.id, e.event_id, e.severity, e.state, e.pipeline, e.received_at,
  e.fingerprint, e.architecture, e.analysis, d.device_id, r.version
FROM events e
LEFT JOIN devices d ON d.id=e.device_id
LEFT JOIN releases r ON r.id=e.release_id
WHERE e.project_id=$1 AND e.received_at >= $2
ORDER BY e.received_at ASC
LIMIT $3`, projectID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id uuid.UUID
		var eventID, sev, state string
		var pipe, fp *string
		var arch *int16
		var ts time.Time
		var analysis []byte
		var deviceID, version *string
		if err := rows.Scan(&id, &eventID, &sev, &state, &pipe, &ts, &fp, &arch, &analysis, &deviceID, &version); err != nil {
			return nil, err
		}
		item := map[string]any{
			"id": id, "event_id": eventID, "severity": sev, "state": state, "received_at": ts,
		}
		if pipe != nil {
			item["pipeline"] = *pipe
		}
		if fp != nil {
			item["fingerprint"] = *fp
		}
		if arch != nil {
			item["architecture"] = *arch
		}
		if deviceID != nil {
			item["device_id"] = *deviceID
		}
		if version != nil {
			item["release"] = *version
		}
		if len(analysis) > 0 && string(analysis) != "null" {
			var a any
			if json.Unmarshal(analysis, &a) == nil {
				item["analysis"] = a
			}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) LogAnalyticsExport(ctx context.Context, projectID uuid.UUID, kind, key string, rows int) error {
	_, err := s.Pool.Exec(ctx, `
INSERT INTO analytics_exports(project_id,kind,object_key,rows) VALUES($1,$2,$3,$4)`, projectID, kind, key, rows)
	return err
}
