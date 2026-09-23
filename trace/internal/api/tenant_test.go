package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/store"
)

// tenantTestStore is a fake store.SessionResolver that only implements
// AuthSession and UserIsMemberOfOrg — the two methods the activeTenant
// middleware touches.
type tenantTestStore struct {
	session store.Session
	member  bool
	memErr  error
}

func (t *tenantTestStore) AuthSession(ctx context.Context, secret string) (store.Session, error) {
	if t.session.UserID == uuid.Nil {
		return store.Session{}, store.ErrNotFound
	}
	return t.session, nil
}

func (t *tenantTestStore) UserIsMemberOfOrg(ctx context.Context, userID, orgID uuid.UUID) (bool, error) {
	if t.memErr != nil {
		return false, t.memErr
	}
	return t.member, nil
}

func (t *tenantTestStore) TouchSession(ctx context.Context, sessionID uuid.UUID, ip, userAgent string) error {
	return nil
}

func (t *tenantTestStore) RevokeSession(ctx context.Context, sessionID, userID uuid.UUID) error {
	return nil
}

// makeAuthRequest builds a request whose Authorization header carries a
// non-empty secret. The fake store will resolve it to the configured session.
func makeAuthRequest(method, path string, headers map[string]string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer lst_sess_xxxxxxxxxxxxxxxxxxxxxxxx")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

func TestActiveTenant_AnonymousPasses(t *testing.T) {
	s := &Server{}
	hits := 0
	h := s.activeTenant(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		// Tenant must NOT be installed for anonymous requests.
		if _, ok := orgFrom(r); ok {
			t.Error("anonymous request should not have an active tenant")
		}
		w.WriteHeader(200)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if rr.Code != 200 {
		t.Fatalf("code=%d", rr.Code)
	}
	if hits != 1 {
		t.Fatalf("handler not called: %d", hits)
	}
}

func TestActiveTenant_AuthedInstallsPrimaryOrg(t *testing.T) {
	orgID := uuid.New()
	userID := uuid.New()
	s := newServerWithResolver(&tenantTestStore{
		session: store.Session{
			UserID:         userID,
			Email:          "alice@example.com",
			OrganizationID: orgID,
			Role:           "admin",
		},
	})
	h := s.activeTenant(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant, ok := orgFrom(r)
		if !ok {
			t.Fatal("expected tenant")
		}
		if tenant.OrgID != orgID {
			t.Fatalf("tenant org: got %s want %s", tenant.OrgID, orgID)
		}
		w.WriteHeader(200)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, makeAuthRequest(http.MethodGet, "/api/me", nil))
	if rr.Code != 200 {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestActiveTenant_XOrgIDAcceptedIfMember(t *testing.T) {
	primary := uuid.New()
	other := uuid.New()
	userID := uuid.New()
	s := newServerWithResolver(&tenantTestStore{
		session: store.Session{
			UserID:         userID,
			OrganizationID: primary,
			Role:           "admin",
		},
		member: true, // user IS a member of "other"
	})
	h := s.activeTenant(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant, _ := orgFrom(r)
		if tenant.OrgID != other {
			t.Fatalf("expected override to %s, got %s", other, tenant.OrgID)
		}
		w.WriteHeader(200)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, makeAuthRequest(http.MethodGet, "/api/me", map[string]string{"X-Org-ID": other.String()}))
	if rr.Code != 200 {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestActiveTenant_XOrgIDForbiddenIfNotMember(t *testing.T) {
	primary := uuid.New()
	other := uuid.New()
	s := newServerWithResolver(&tenantTestStore{
		session: store.Session{
			UserID:         uuid.New(),
			OrganizationID: primary,
			Role:           "admin",
		},
		member: false,
	})
	h := s.activeTenant(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream should not be called when tenant is forbidden")
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, makeAuthRequest(http.MethodGet, "/api/me", map[string]string{"X-Org-ID": other.String()}))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestActiveTenant_XOrgIDBadUUID(t *testing.T) {
	orgID := uuid.New()
	s := newServerWithResolver(&tenantTestStore{
		session: store.Session{
			UserID:         uuid.New(),
			OrganizationID: orgID,
			Role:           "admin",
		},
	})
	h := s.activeTenant(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("downstream should not be called when X-Org-ID is malformed")
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, makeAuthRequest(http.MethodGet, "/api/me", map[string]string{"X-Org-ID": "not-a-uuid"}))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestCheckOrgMatch_OpenUIBypass(t *testing.T) {
	s := &Server{Cfg: configWithOpenUI(true)}
	// No tenant in context, but OpenUI is on → no error.
	if err := s.checkOrgMatch(httptest.NewRequest(http.MethodGet, "/", nil), uuid.New()); err != nil {
		t.Fatalf("openui should bypass: %v", err)
	}
}

func TestCheckOrgMatch_TenantMatches(t *testing.T) {
	orgID := uuid.New()
	s := &Server{Cfg: configWithOpenUI(false)}
	req := withSessionAndOrg(
		httptest.NewRequest(http.MethodGet, "/", nil),
		store.Session{UserID: uuid.New(), OrganizationID: orgID},
		ActiveTenant{OrgID: orgID},
	)
	if err := s.checkOrgMatch(req, orgID); err != nil {
		t.Fatalf("matching tenant: %v", err)
	}
}

func TestCheckOrgMatch_TenantMismatch(t *testing.T) {
	tenant := uuid.New()
	other := uuid.New()
	s := &Server{Cfg: configWithOpenUI(false)}
	req := withSessionAndOrg(
		httptest.NewRequest(http.MethodGet, "/", nil),
		store.Session{UserID: uuid.New(), OrganizationID: tenant},
		ActiveTenant{OrgID: tenant},
	)
	if err := s.checkOrgMatch(req, other); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCheckOrgMatch_NilOrg(t *testing.T) {
	s := &Server{Cfg: configWithOpenUI(false)}
	if err := s.checkOrgMatch(httptest.NewRequest(http.MethodGet, "/", nil), uuid.Nil); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound for nil org, got %v", err)
	}
}
