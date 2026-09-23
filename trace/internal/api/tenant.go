package api

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/store"
)

// activeTenant is the HTTP middleware that authenticates the request and
// installs the resolved ActiveTenant on the request context. It enforces the
// X-Org-ID header: if present, the caller must be a member of that org.
//
// Behaviour summary:
//   - Anonymous request → pass through unchanged. Public routes (login,
//     /api/health/*) are designed to not require a tenant.
//   - Authenticated request without X-Org-ID → tenant is the user's primary
//     org (sess.OrganizationID).
//   - Authenticated request WITH X-Org-ID where user is a member → tenant
//     is the requested org.
//   - Authenticated request WITH X-Org-ID where user is NOT a member → 403.
//   - OpenUI mode (TRACE_OPEN_UI=true) is a local-dev escape hatch; tenant
//     middleware remains active, but endpoints that handle OpenUI must opt in
//     (see requireUI) — this middleware just installs the tenant for callers
//     that DO authenticate.
func (s *Server) activeTenant(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := sessionFrom(s, r)
		if !ok {
			// Local deployment mode: install the default project's org as the
			// active tenant so org-scoped endpoints (usage, billing) work even
			// when the operator has not logged in.
			if s.Cfg.IsLocal() && s.Store != nil {
				if p, err := s.Store.DefaultProject(r.Context()); err == nil {
					next.ServeHTTP(w, withSessionAndOrg(r, store.Session{}, ActiveTenant{OrgID: p.OrganizationID}))
					return
				}
			}
			next.ServeHTTP(w, r)
			return
		}
		headerOrg := r.Header.Get("X-Org-ID")
		tenant, err := s.resolveTenant(r.Context(), sess, headerOrg)
		if err != nil {
			writeErr(w, http.StatusForbidden, "tenant_forbidden", "not a member of requested organization", false)
			return
		}
		next.ServeHTTP(w, withSessionAndOrg(r, sess, tenant))
	})
}

// checkOrgMatch is a per-handler fallback that verifies the resolved
// ActiveTenant matches the org argument the caller is acting on. Handlers
// that take an orgID from the URL or path should call this before touching
// storage.
func (s *Server) checkOrgMatch(r *http.Request, orgID uuid.UUID) error {
	if orgID == uuid.Nil {
		return store.ErrNotFound
	}
	if s.Cfg.OpenUI {
		return nil
	}
	tenant, ok := orgFrom(r)
	if !ok {
		// No tenant in ctx → anonymous path; trust the handler's own checks.
		return nil
	}
	if tenant.OrgID != orgID {
		return store.ErrNotFound
	}
	return nil
}
