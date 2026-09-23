package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/store"
)

// apiMeOrgs lists every org the current user has a membership in, plus the
// currently active tenant for the request. Used by the org-switcher UI.
func (s *Server) apiMeOrgs(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeJSON(w, 200, map[string]any{"items": []any{}, "active_org_id": nil})
		return
	}
	items, err := s.Store.ListOrganizationsForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	active := sess.OrganizationID
	if t, ok := orgFrom(r); ok && t.OrgID != uuid.Nil {
		active = t.OrgID
	}
	writeJSON(w, 200, map[string]any{"items": items, "active_org_id": active})
}

// apiSwitchOrg issues a new session token bound to a different org. The
// previous session stays valid; clients are expected to drop the old cookie.
//
// Body: {"organization_id": "<uuid>"}
// Returns: {"session_token": "lst_sess_xxx", "session_id": "...", "organization_id": "..."}
func (s *Server) apiSwitchOrg(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	var body struct {
		OrganizationID uuid.UUID `json:"organization_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.OrganizationID == uuid.Nil {
		writeErr(w, 400, "bad_request", "organization_id required", false)
		return
	}
	isMember, err := s.Store.UserIsMemberOfOrg(r.Context(), sess.UserID, body.OrganizationID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	if !isMember {
		writeErr(w, 403, "forbidden", "not a member of target organization", false)
		return
	}
	// Look up the user's role in the target org so the new session has the
	// correct permissions without forcing a re-login.
	role, err := s.Store.ProjectRole(r.Context(), sess.UserID, body.OrganizationID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	_, raw, err := s.Store.MintSession(r.Context(),
		store.User{ID: sess.UserID, Email: sess.Email, Name: sess.Name},
		body.OrganizationID, role)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	// Rotate the cookie too: JSON-only tokens leave cookie clients on the
	// old org (the original bug).
	s.setSessionCookie(w, raw)
	s.Store.Audit(r.Context(), &sess.UserID, nil, &body.OrganizationID, nil, "auth.switch_org", "organization", body.OrganizationID.String(), clientIP(r), r.UserAgent(), nil)
	writeJSON(w, 200, map[string]any{
		"session_token":   raw,
		"organization_id": body.OrganizationID,
	})
}

// apiMeSessions lists the current user's sessions.
func (s *Server) apiMeSessions(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	items, err := s.Store.ListSessions(r.Context(), sess.UserID, 50)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	if items == nil {
		items = []store.SessionInfo{}
	}
	writeJSON(w, 200, map[string]any{"items": items, "current_session_id": sess.SessionID})
}

// apiRevokeMeSession revokes a single session by id. Users can revoke their
// own sessions; the current session is preserved (use /api/auth/logout for that).
func (s *Server) apiRevokeMeSession(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_request", "invalid id", false)
		return
	}
	if id == sess.SessionID {
		writeErr(w, 400, "bad_request", "use POST /api/auth/logout to revoke current session", false)
		return
	}
	if err := s.Store.RevokeSession(r.Context(), id, sess.UserID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 404, "not_found", "session not found", false)
			return
		}
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	s.Store.Audit(r.Context(), &sess.UserID, nil, &sess.OrganizationID, nil, "auth.session_revoke", "session", id.String(), clientIP(r), r.UserAgent(), nil)
	writeJSON(w, 200, map[string]any{"revoked": true, "id": id})
}

// apiRevokeOtherSessions revokes every session except the current one.
func (s *Server) apiRevokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFrom(s, r)
	if !ok {
		writeErr(w, 401, "unauthorized", "login required", false)
		return
	}
	if err := s.Store.RevokeAllOtherSessions(r.Context(), sess.UserID, sess.SessionID); err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	s.Store.Audit(r.Context(), &sess.UserID, nil, &sess.OrganizationID, nil, "auth.session_revoke_others", "user", sess.UserID.String(), clientIP(r), r.UserAgent(), nil)
	writeJSON(w, 200, map[string]any{"revoked_others": true})
}

// apiCreateOrgToken creates an organization-scoped API token. The token value
// is shown ONCE in the response — only the hash is persisted.
//
// Mounted at /api/org-tokens to avoid name collision with the project-scoped
// /api/tokens (legacy) endpoint.
func (s *Server) apiCreateOrgToken(w http.ResponseWriter, r *http.Request) {
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
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeErr(w, 400, "bad_request", "name required", false)
		return
	}
	if len(body.Scopes) == 0 {
		body.Scopes = []string{"event:write"}
	}
	if !s.userHasRole(sess, "admin") {
		writeErr(w, 403, "access_denied", "admin role required to mint org tokens", false)
		return
	}
	raw, prefix, hash, err := mintOrgToken()
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	tokID, err := s.Store.CreateOrgToken(r.Context(), tenant.OrgID, body.Name, prefix, hash, body.Scopes)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, map[string]any{
		"id":     tokID,
		"token":  raw,
		"prefix": prefix,
		"name":   body.Name,
		"scopes": body.Scopes,
	})
}

// apiMeUsage returns the current usage summary for the active tenant.
func (s *Server) apiMeUsage(w http.ResponseWriter, r *http.Request) {
	tenant, ok := orgFrom(r)
	if !ok || tenant.OrgID == uuid.Nil {
		writeErr(w, 400, "bad_request", "no active tenant", false)
		return
	}
	summary, err := s.Store.UsageSummary(r.Context(), tenant.OrgID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, summary)
}

// mintOrgToken returns (raw, prefix, hash). The raw value is shown to the
// caller once; only the SHA-256 hash is persisted.
func mintOrgToken() (raw, prefix string, hash []byte, err error) {
	var b [32]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", "", nil, err
	}
	raw = "lst_tok_" + base64.RawURLEncoding.EncodeToString(b[:])
	prefix = "lst_tok_" + base64.RawURLEncoding.EncodeToString(b[:6])
	sum := sha256.Sum256([]byte(raw))
	return raw, prefix, sum[:], nil
}
