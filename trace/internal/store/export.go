package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// WebhookExport represents a data export.
type WebhookExport struct {
	ID             uuid.UUID      `json:"id"`
	OrganizationID uuid.UUID      `json:"organization_id"`
	ProjectID      *uuid.UUID     `json:"project_id,omitempty"`
	Kind           string         `json:"kind"`
	Filters        map[string]any `json:"filters"`
	ObjectKey      string         `json:"object_key,omitempty"`
	SizeBytes      int64          `json:"size_bytes"`
	RowsCount      int64          `json:"rows_count"`
	Status         string         `json:"status"`
	Error          string         `json:"error,omitempty"`
	CreatedBy      uuid.UUID      `json:"created_by"`
	CreatedAt      time.Time      `json:"created_at"`
	CompletedAt    *time.Time     `json:"completed_at,omitempty"`
}

// CreateExport creates a new export record.
func (s *Store) CreateExport(ctx context.Context, id, orgID uuid.UUID, projectID *uuid.UUID, kind string, filters map[string]any, createdBy uuid.UUID) (*WebhookExport, error) {
	filtersJSON, err := json.Marshal(filters)
	if err != nil {
		return nil, err
	}
	var export WebhookExport
	err = s.Pool.QueryRow(ctx, `
INSERT INTO webhook_exports(id, organization_id, project_id, kind, filters, created_by, status, created_at)
VALUES ($1, $2, $3, $4, $5, $6, 'pending', now())
RETURNING id, organization_id, project_id, kind, filters, object_key, size_bytes, rows_count, status, error, created_by, created_at, completed_at`,
		id, orgID, projectID, kind, filtersJSON, createdBy).Scan(
		&export.ID, &export.OrganizationID, &export.ProjectID, &export.Kind,
		&filtersJSON, &export.ObjectKey, &export.SizeBytes, &export.RowsCount,
		&export.Status, &export.Error, &export.CreatedBy, &export.CreatedAt, &export.CompletedAt,
	)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(filtersJSON, &export.Filters)
	return &export, nil
}

// GetExport gets an export by ID.
func (s *Store) GetExport(ctx context.Context, id uuid.UUID) (*WebhookExport, error) {
	var export WebhookExport
	var filtersJSON []byte
	err := s.Pool.QueryRow(ctx, `
SELECT id, organization_id, project_id, kind, filters, object_key, size_bytes, rows_count, status, error, created_by, created_at, completed_at
FROM webhook_exports WHERE id=$1`, id).Scan(
		&export.ID, &export.OrganizationID, &export.ProjectID, &export.Kind,
		&filtersJSON, &export.ObjectKey, &export.SizeBytes, &export.RowsCount,
		&export.Status, &export.Error, &export.CreatedBy, &export.CreatedAt, &export.CompletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal(filtersJSON, &export.Filters)
	return &export, nil
}

// ListExports lists exports for an org.
func (s *Store) ListExports(ctx context.Context, orgID uuid.UUID, limit int) ([]WebhookExport, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
SELECT id, organization_id, project_id, kind, filters, object_key, size_bytes, rows_count, status, error, created_by, created_at, completed_at
FROM webhook_exports WHERE organization_id=$1 ORDER BY created_at DESC LIMIT $2`,
		orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var exports []WebhookExport
	for rows.Next() {
		var export WebhookExport
		var filtersJSON []byte
		if err := rows.Scan(
			&export.ID, &export.OrganizationID, &export.ProjectID, &export.Kind,
			&filtersJSON, &export.ObjectKey, &export.SizeBytes, &export.RowsCount,
			&export.Status, &export.Error, &export.CreatedBy, &export.CreatedAt, &export.CompletedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(filtersJSON, &export.Filters)
		exports = append(exports, export)
	}
	return exports, rows.Err()
}
