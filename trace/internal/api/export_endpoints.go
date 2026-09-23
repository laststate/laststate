package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/store"
)

// apiCreateExport handles creating a new webhook export.
func (s *Server) apiCreateExport(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	tenant, ok := orgFrom(r)
	if !ok || tenant.OrgID == uuid.Nil {
		writeErr(w, 400, "bad_request", "no active tenant", false)
		return
	}
	var body struct {
		Kind     string         `json:"kind"`
		Filter   map[string]any `json:"filter"`
		S3Config map[string]any `json:"s3_config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "bad_request", "invalid body", false)
		return
	}
	if body.Kind == "" {
		body.Kind = "events_ndjson"
	}
	if body.Filter == nil {
		body.Filter = map[string]any{}
	}
	// Generate export ID.
	exportID := uuid.New()
	// Create export record.
	var projectID *uuid.UUID
	_, err := s.Store.CreateExport(r.Context(), exportID, tenant.OrgID, projectID, body.Kind, body.Filter, sess.UserID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 201, map[string]any{
		"id":      exportID,
		"status":  "pending",
		"message": "Export created, processing in background",
	})
}

// apiGetExport handles getting an export by ID.
func (s *Server) apiGetExport(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	exportID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_request", "invalid id", false)
		return
	}
	export, err := s.Store.GetExport(r.Context(), exportID)
	if err != nil {
		writeErr(w, 404, "not_found", "export not found", false)
		return
	}
	// Check if user has access to this export.
	if export.OrganizationID != sess.OrganizationID {
		writeErr(w, 403, "forbidden", "access denied", false)
		return
	}
	writeJSON(w, 200, export)
}

// apiListExports handles listing exports for the current org.
func (s *Server) apiListExports(w http.ResponseWriter, r *http.Request) {
	_, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	tenant, ok := orgFrom(r)
	if !ok || tenant.OrgID == uuid.Nil {
		writeErr(w, 400, "bad_request", "no active tenant", false)
		return
	}
	exports, err := s.Store.ListExports(r.Context(), tenant.OrgID, 50)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	if exports == nil {
		exports = []store.WebhookExport{}
	}
	writeJSON(w, 200, map[string]any{"items": exports})
}

// wire export routes in server.go:
// mux.Handle("POST /api/webhooks/export", s.requireAuth(http.HandlerFunc(s.apiCreateExport)))
// mux.Handle("GET /api/webhooks/export/{id}", s.requireAuth(http.HandlerFunc(s.apiGetExport)))
// mux.Handle("GET /api/webhooks/exports", s.requireAuth(http.HandlerFunc(s.apiListExports)))
