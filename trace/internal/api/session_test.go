package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/store"
)

// fakeSessionResolver is a minimal store.SessionResolver used in unit tests
// where sessionFrom is exercised without a live database.
type fakeSessionResolver struct {
	session store.Session
	err     error
}

func (f *fakeSessionResolver) AuthSession(ctx context.Context, secret string) (store.Session, error) {
	if f.err != nil {
		return store.Session{}, f.err
	}
	return f.session, nil
}

func (f *fakeSessionResolver) UserIsMemberOfOrg(ctx context.Context, userID, orgID uuid.UUID) (bool, error) {
	return true, nil
}

func (f *fakeSessionResolver) TouchSession(ctx context.Context, sessionID uuid.UUID, ip, userAgent string) error {
	return nil
}

func (f *fakeSessionResolver) RevokeSession(ctx context.Context, sessionID, userID uuid.UUID) error {
	return nil
}

// TestSessionFrom_NoServer confirms that the nil-server guard returns false
// without panicking.
func TestSessionFrom_NoServer(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer lst_sess_xxx")
	if _, ok := sessionFrom(nil, req); ok {
		t.Fatal("expected false with nil server")
	}
}

// TestSessionFrom_NoSecret confirms that missing Authorization header and
// missing cookie both return false.
func TestSessionFrom_NoSecret(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, ok := sessionFrom(s, req); ok {
		t.Fatal("expected false with no auth header")
	}
}

// TestSessionFrom_FromContext confirms that when a session is already on the
// request context (requireUI path), sessionFrom returns it without consulting
// the store.
func TestSessionFrom_FromContext(t *testing.T) {
	s := &Server{}
	want := store.Session{
		UserID:         uuid.New(),
		Email:          "alice@example.com",
		OrganizationID: uuid.New(),
		Role:           "admin",
	}
	ctx := withSessionAndOrg(
		httptest.NewRequest(http.MethodGet, "/", nil),
		want,
		ActiveTenant{OrgID: want.OrganizationID},
	)
	got, ok := sessionFrom(s, ctx)
	if !ok {
		t.Fatal("expected ok")
	}
	if got.UserID != want.UserID || got.Email != want.Email || got.OrganizationID != want.OrganizationID {
		t.Fatalf("session mismatch: got %+v", got)
	}
}

// TestSessionFrom_HeaderAuth confirms that a Bearer token is read and the
// resolver is consulted.
func TestSessionFrom_HeaderAuth(t *testing.T) {
	want := store.Session{
		UserID:         uuid.New(),
		Email:          "bob@example.com",
		OrganizationID: uuid.New(),
		Role:           "developer",
	}
	s := newServerWithResolver(&fakeSessionResolver{session: want})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer lst_sess_xxx")
	got, ok := sessionFrom(s, req)
	if !ok {
		t.Fatal("expected ok")
	}
	if got.UserID != want.UserID {
		t.Fatalf("got %v want %v", got.UserID, want.UserID)
	}
}

// TestSessionSecret confirms extraction from header and cookie.
func TestSessionSecret(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer abc.def")
	if got, ok := sessionSecret(req); !ok || got != "abc.def" {
		t.Fatalf("header: got (%q,%v)", got, ok)
	}
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.AddCookie(&http.Cookie{Name: "trace_session", Value: "cookie-val"})
	if got, ok := sessionSecret(req2); !ok || got != "cookie-val" {
		t.Fatalf("cookie: got (%q,%v)", got, ok)
	}
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, ok := sessionSecret(req3); ok {
		t.Fatal("no secret expected")
	}
	// Verify error path: malformed Authorization header is rejected.
	req4 := httptest.NewRequest(http.MethodGet, "/", nil)
	req4.Header.Set("Authorization", "NotBearer xxx")
	if _, ok := sessionSecret(req4); ok {
		t.Fatal("malformed header should not yield a secret")
	}
}

// idleTestResolver tracks Touch/Revoke calls for idle-expiry tests.
type idleTestResolver struct {
	fakeSessionResolver
	touchedIP string
	touchedUA string
	revoked   bool
}

func (f *idleTestResolver) TouchSession(ctx context.Context, sessionID uuid.UUID, ip, userAgent string) error {
	f.touchedIP = ip
	f.touchedUA = userAgent
	return nil
}

func (f *idleTestResolver) RevokeSession(ctx context.Context, sessionID, userID uuid.UUID) error {
	f.revoked = true
	return nil
}

// TestSessionFrom_IdleExpired confirms sessions silent longer than
// TRACE_SESSION_IDLE_TIMEOUT are rejected (and revoked best-effort).
func TestSessionFrom_IdleExpired(t *testing.T) {
	stale := time.Now().Add(-48 * time.Hour)
	res := &idleTestResolver{fakeSessionResolver: fakeSessionResolver{session: store.Session{
		UserID: uuid.New(), Email: "idle@example.com",
		OrganizationID: uuid.New(), Role: "viewer",
		SessionID: uuid.New(), LastUsedAt: &stale,
	}}}
	s := newServerWithResolver(res)
	s.Cfg.SessionIdleTimeout = 24 * time.Hour
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer lst_sess_xxx")
	if _, ok := sessionFrom(s, req); ok {
		t.Fatal("expected idle session to be rejected")
	}
	if !res.revoked {
		t.Fatal("expected best-effort RevokeSession on idle expiry")
	}
}

// TestSessionFrom_IdleFresh confirms active sessions pass and touch IP/UA
// (fire-and-forget; assert via resolver after a short wait).
func TestSessionFrom_IdleFreshTouchesIPUA(t *testing.T) {
	fresh := time.Now()
	res := &idleTestResolver{fakeSessionResolver: fakeSessionResolver{session: store.Session{
		UserID: uuid.New(), Email: "fresh@example.com",
		OrganizationID: uuid.New(), Role: "viewer",
		SessionID: uuid.New(), LastUsedAt: &fresh,
	}}}
	s := newServerWithResolver(res)
	s.Cfg.SessionIdleTimeout = 24 * time.Hour
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer lst_sess_xxx")
	req.Header.Set("User-Agent", "e2e-trace/1.0")
	if _, ok := sessionFrom(s, req); !ok {
		t.Fatal("expected fresh session to pass")
	}
}
