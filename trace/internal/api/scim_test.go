package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSCIMRequiresSession(t *testing.T) {
	s, _, _ := testAPI(t)
	h := s.Handler()
	for _, tc := range []struct{ method, path string }{
		{"GET", "/scim/v2/Users"},
		{"POST", "/scim/v2/Users"},
		{"GET", "/scim/v2/Users/" + uuid.NewString()},
		{"DELETE", "/scim/v2/Users/" + uuid.NewString()},
		{"GET", "/scim/v2/Groups"},
		{"POST", "/scim/v2/Groups"},
		{"GET", "/scim/v2/Groups/" + uuid.NewString()},
		{"DELETE", "/scim/v2/Groups/" + uuid.NewString()},
	} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, nil)
		h.ServeHTTP(rr, req)
		if rr.Code != 401 && rr.Code != 403 {
			t.Fatalf("%s %s = %d, want 401/403 without session", tc.method, tc.path, rr.Code)
		}
	}
}

func TestSCIMBadID(t *testing.T) {
	s, st, _ := testAPI(t)
	h := s.Handler()
	// Unique email per run: CI runs `go test ./...` then `go test -race`
	// against the same postgres service, so a fixed email collides on the
	// second run with `duplicate key ... users_email_key`.
	secret := testUISession(t, st, "scim-"+time.Now().Format("150405.000000")+"@t.local", "admin")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/scim/v2/Users/not-a-uuid", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	h.ServeHTTP(rr, req)
	if rr.Code != 400 {
		t.Fatalf("bad id = %d, want 400", rr.Code)
	}
}
