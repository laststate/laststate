package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/auth"
	"github.com/laststate/trace/internal/store"
)

const orgKey ctxKey = 2

// sessionSecret extracts the bearer/cookie session secret from an HTTP request.
// Returns the secret string and a bool indicating whether one was found.
func sessionSecret(r *http.Request) (string, bool) {
	if secret, err := auth.Bearer(r.Header.Get("Authorization")); err == nil && secret != "" {
		return secret, true
	}
	if c, err := r.Cookie("trace_session"); err == nil && c.Value != "" {
		return c.Value, true
	}
	return "", false
}

// sessionFrom returns the authenticated session for an HTTP request.
//
// Resolution order:
//  1. If requireUI already populated the request context (sessKey), use that.
//  2. Otherwise, try to authenticate via Authorization header or trace_session
//     cookie by calling s.Store.AuthSession. The resolved session is NOT
//     written back to the context; callers that need cross-middleware access
//     should still go through requireUI.
func sessionFrom(s *Server, r *http.Request) (store.Session, bool) {
	if s == nil {
		return store.Session{}, false
	}
	if v, ok := r.Context().Value(sessKey).(store.Session); ok && v.UserID != uuid.Nil {
		return v, true
	}
	secret, ok := sessionSecret(r)
	if !ok {
		return store.Session{}, false
	}
	sess, err := s.resolver().AuthSession(r.Context(), secret)
	if err != nil || sess.UserID == uuid.Nil {
		return store.Session{}, false
	}
	// Idle expiry: sessions silent longer than SessionIdleTimeout are dead.
	// Revoke best-effort so the row can't be revived by clock skew games.
	if s.Cfg.SessionIdleTimeout > 0 && sess.LastUsedAt != nil {
		if time.Since(*sess.LastUsedAt) > s.Cfg.SessionIdleTimeout {
			_ = s.resolver().RevokeSession(r.Context(), sess.SessionID, sess.UserID)
			return store.Session{}, false
		}
	}
	// Best-effort activity touch (populates the /api/me/sessions UI).
	// Fire-and-forget: auth must never wait on a metadata write.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.resolver().TouchSession(ctx, sess.SessionID, clientIP(r), r.UserAgent())
	}()
	return sess, true
}

// requireAuth is a middleware that rejects requests without a valid session.
// Used on routes that need auth but not a UI role check.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Local deployment mode: the operator running the server is trusted,
		// so authentication is not required on any route.
		if s.Cfg.IsLocal() {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := sessionFrom(s, r); !ok {
			writeErr(w, 401, "unauthorized", "authentication required", false)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ActiveTenant describes the resolved organization context for a request.
type ActiveTenant struct {
	OrgID uuid.UUID
}

// orgFrom returns the ActiveTenant that the activeTenant middleware installed
// on the request context. Returns false when the request is not authenticated.
func orgFrom(r *http.Request) (ActiveTenant, bool) {
	v, ok := r.Context().Value(orgKey).(ActiveTenant)
	return v, ok
}

// withSessionAndOrg stores the resolved session and ActiveTenant in the request
// context so downstream handlers can read them without re-authenticating.
func withSessionAndOrg(r *http.Request, sess store.Session, tenant ActiveTenant) *http.Request {
	ctx := r.Context()
	if sess.UserID != uuid.Nil {
		ctx = context.WithValue(ctx, sessKey, sess)
	}
	if tenant.OrgID != uuid.Nil {
		ctx = context.WithValue(ctx, orgKey, tenant)
	}
	return r.WithContext(ctx)
}

// errTenantForbidden is returned when an X-Org-ID header targets an org the
// caller is not a member of.
var errTenantForbidden = errors.New("tenant: forbidden")

// resolveTenant determines the ActiveTenant for an authenticated request.
// If X-Org-ID is present and the user has membership in that org, the tenant
// is overridden. If the header is present but the user is not a member, an
// error is returned. If no header is set, the session's primary org is used.
func (s *Server) resolveTenant(ctx context.Context, sess store.Session, headerOrg string) (ActiveTenant, error) {
	if headerOrg == "" {
		return ActiveTenant{OrgID: sess.OrganizationID}, nil
	}
	oid, err := uuid.Parse(headerOrg)
	if err != nil {
		return ActiveTenant{}, err
	}
	if oid == sess.OrganizationID {
		return ActiveTenant{OrgID: oid}, nil
	}
	member, err := s.resolver().UserIsMemberOfOrg(ctx, sess.UserID, oid)
	if err != nil {
		return ActiveTenant{}, err
	}
	if !member {
		return ActiveTenant{}, errTenantForbidden
	}
	return ActiveTenant{OrgID: oid}, nil
}
