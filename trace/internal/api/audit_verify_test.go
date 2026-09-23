package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/store"
)

// auditVerifyTestStore stubs the auth path so the handler can require auth and
// optionally pass an admin role check without a live DB. VerifyAuditChain
// itself still needs a real Store, so this test only covers the auth/RBAC
// surface; integration tests live in internal/store.
type auditVerifyTestStore struct {
	session store.Session
}

func (a *auditVerifyTestStore) AuthSession(ctx context.Context, secret string) (store.Session, error) {
	if a.session.UserID == uuid.Nil {
		return store.Session{}, store.ErrNotFound
	}
	return a.session, nil
}

func (a *auditVerifyTestStore) UserIsMemberOfOrg(ctx context.Context, userID, orgID uuid.UUID) (bool, error) {
	return true, nil
}

func (a *auditVerifyTestStore) TouchSession(ctx context.Context, sessionID uuid.UUID, ip, userAgent string) error {
	return nil
}

func (a *auditVerifyTestStore) RevokeSession(ctx context.Context, sessionID, userID uuid.UUID) error {
	return nil
}

func TestAuditVerify_RequiresAuth(t *testing.T) {
	s := &Server{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit/verify", nil)
	s.apiAuditVerify(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d want 401 body=%s", rr.Code, rr.Body.String())
	}
}

func TestAuditVerify_RequiresAdmin(t *testing.T) {
	s := newServerWithResolver(&auditVerifyTestStore{
		session: store.Session{UserID: uuid.New(), Role: "viewer"},
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit/verify", nil)
	req.Header.Set("Authorization", "Bearer lst_sess_xxx")
	s.apiAuditVerify(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("got %d want 403 body=%s", rr.Code, rr.Body.String())
	}
}

func TestAuditVerify_BadFromRejected(t *testing.T) {
	s := newServerWithResolver(&auditVerifyTestStore{
		session: store.Session{UserID: uuid.New(), Role: "admin"},
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit/verify", nil)
	req.URL.RawQuery = "from=not-a-time"
	req.Header.Set("Authorization", "Bearer lst_sess_xxx")
	s.apiAuditVerify(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d want 400 body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	errObj, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %T", body["error"])
	}
	if errObj["code"] != "bad_request" {
		t.Fatalf("code=%v", errObj["code"])
	}
}

func TestAuditVerify_BadLimitRejected(t *testing.T) {
	s := newServerWithResolver(&auditVerifyTestStore{
		session: store.Session{UserID: uuid.New(), Role: "admin"},
	})
	for _, v := range []string{"0", "-1", "999999999", "abc"} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/admin/audit/verify", nil)
		req.URL.RawQuery = "limit=" + v
		req.Header.Set("Authorization", "Bearer lst_sess_xxx")
		s.apiAuditVerify(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("limit=%q got %d want 400", v, rr.Code)
		}
	}
}

func TestAuditVerify_BadOrgRejected(t *testing.T) {
	s := newServerWithResolver(&auditVerifyTestStore{
		session: store.Session{UserID: uuid.New(), Role: "admin"},
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit/verify", nil)
	req.URL.RawQuery = "org=not-a-uuid"
	req.Header.Set("Authorization", "Bearer lst_sess_xxx")
	s.apiAuditVerify(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d want 400", rr.Code)
	}
}
