package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/laststate/trace/internal/api"
	"github.com/laststate/trace/internal/config"
	"github.com/laststate/trace/internal/db"
	"github.com/laststate/trace/internal/lep"
	"github.com/laststate/trace/internal/objects"
	"github.com/laststate/trace/internal/queue"
	"github.com/laststate/trace/internal/store"
)

func testAPI(t *testing.T) (*api.Server, *store.Store, string) {
	t.Helper()
	url := os.Getenv("TRACE_DATABASE_URL")
	if url == "" {
		url = "postgres://trace:trace@localhost:5432/trace?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Skip("postgres unavailable:", err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	st := &store.Store{Pool: pool}
	dir := t.TempDir()
	obj := &objects.Store{Root: dir}
	if err := obj.Ensure(); err != nil {
		t.Fatal(err)
	}
	// bootstrap if needed
	secret := ""
	if _, _, s, err := st.Bootstrap(ctx); err == nil {
		secret = s
	} else {
		// create token for default project
		p, err := st.DefaultProject(ctx)
		if err != nil {
			t.Skip(err)
		}
		_, secret, err = st.CreateProjectToken(ctx, p.ID, "test", []string{"event:write", "event:read", "artifact:write"})
		if err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Load()
	cfg.OpenUI = false
	cfg.MaxEventSize = 1 << 20
	cfg.MaxBatchEvents = 10
	cfg.RateLimitPerMin = 100000
	cfg.MaxConnsPerIP = 1000
	s := &api.Server{Cfg: cfg, Store: st, Object: obj, Queue: queue.NewMemory()}
	return s, st, secret
}

// testUISession creates a password user with org membership and returns a session secret.
func testUISession(t *testing.T, st *store.Store, email, role string) string {
	t.Helper()
	ctx := context.Background()
	p, err := st.DefaultProject(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if role == "" {
		role = "viewer"
	}
	pw := store.GeneratePassword()
	u, err := st.CreateUser(ctx, p.OrganizationID, email, pw, "Test", role)
	if err != nil {
		t.Fatal(err)
	}
	_, secret, err := st.MintSession(ctx, u, p.OrganizationID, role)
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

func TestHealthAndOpenAPI(t *testing.T) {
	s, _, _ := testAPI(t)
	h := s.Handler()
	for _, path := range []string{"/health/live", "/openapi.json", "/metrics", "/v1/relay/capabilities"} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		h.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Fatalf("%s -> %d %s", path, rr.Code, rr.Body.String())
		}
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
}

func TestIngestRequiresAuthAndScope(t *testing.T) {
	s, st, secret := testAPI(t)
	h := s.Handler()
	raw, err := lep.Encode(lep.Header{Type: lep.TypeError, Architecture: 1, Sequence: 1, EventID: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// no auth
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest", bytes.NewReader(raw))
	h.ServeHTTP(rr, req)
	if rr.Code != 401 {
		t.Fatalf("want 401 got %d", rr.Code)
	}

	// success
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/ingest", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Idempotency-Key", "api-test-"+time.Now().Format("150405.000000"))
	h.ServeHTTP(rr, req)
	if rr.Code != 202 {
		t.Fatalf("ingest %d %s", rr.Code, rr.Body.String())
	}
	body, _ := io.ReadAll(rr.Body)
	if len(body) == 0 {
		t.Fatal("empty receipt")
	}

	// duplicate
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/v1/ingest", bytes.NewReader(raw))
	req2.Header.Set("Authorization", "Bearer "+secret)
	req2.Header.Set("Idempotency-Key", req.Header.Get("Idempotency-Key"))
	h.ServeHTTP(rr2, req2)
	if rr2.Code != 202 {
		t.Fatalf("dup %d", rr2.Code)
	}

	// token without event:write
	p, _ := st.DefaultProject(context.Background())
	_, readOnly, err := st.CreateProjectToken(context.Background(), p.ID, "ro", []string{"event:read"})
	if err != nil {
		t.Fatal(err)
	}
	rr3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/v1/ingest", bytes.NewReader(raw))
	req3.Header.Set("Authorization", "Bearer "+readOnly)
	h.ServeHTTP(rr3, req3)
	if rr3.Code != 403 && rr3.Code != 401 {
		t.Fatalf("scope %d %s", rr3.Code, rr3.Body.String())
	}
}

func TestUIRequiresLoginWhenClosed(t *testing.T) {
	s, st, _ := testAPI(t)
	s.Cfg.OpenUI = false
	h := s.Handler()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/issues", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != 401 {
		t.Fatalf("want 401 got %d", rr.Code)
	}

	// session with membership
	email := "api-" + time.Now().Format("150405.000000") + "@t.local"
	secret := testUISession(t, st, email, "viewer")
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("overview %d %s", rr.Code, rr.Body.String())
	}
}

func TestLoginJSON(t *testing.T) {
	s, _, _ := testAPI(t)
	h := s.Handler()
	rr := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]string{"email": "nope@x", "password": "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	h.ServeHTTP(rr, req)
	if rr.Code != 401 {
		t.Fatal(rr.Code)
	}
}

func TestLoginMFAEnforcement(t *testing.T) {
	s, st, _ := testAPI(t)
	h := s.Handler()
	ctx := context.Background()

	email := "mfa-" + time.Now().Format("150405.000000") + "@t.local"
	password := "supersecretpassword123"
	p, err := st.DefaultProject(ctx)
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUser(ctx, p.OrganizationID, email, password, "MFA", "viewer")
	if err != nil {
		t.Fatal(err)
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "LastState", AccountName: email})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EnableMFA(ctx, u.ID, key.Secret()); err != nil {
		t.Fatal(err)
	}

	login := func(mfa string) (int, string) {
		payload := map[string]string{"email": email, "password": password}
		if mfa != "" {
			payload["mfa_code"] = mfa
		}
		body, _ := json.Marshal(payload)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
		h.ServeHTTP(rr, req)
		return rr.Code, rr.Body.String()
	}

	// 1. No code → mfa_required, no usable session.
	if code, body := login(""); code != 401 || !strings.Contains(body, "mfa_required") {
		t.Fatalf("want 401 mfa_required, got %d %s", code, body)
	}
	// 2. Wrong code → mfa_invalid.
	if code, body := login("000000"); code != 401 || !strings.Contains(body, "mfa_invalid") {
		t.Fatalf("want 401 mfa_invalid, got %d %s", code, body)
	}
	// 3. Valid TOTP → 200 with session.
	valid, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if code, body := login(valid); code != 200 || !strings.Contains(body, "token") {
		t.Fatalf("want 200 with token, got %d %s", code, body)
	}
}

func TestAlertSSRFRejected(t *testing.T) {
	s, st, _ := testAPI(t)
	// create alert requires admin; private target must be rejected as SSRF
	secret := testUISession(t, st, "alert-"+time.Now().Format("150405.000000")+"@t.local", "admin")
	h := s.Handler()
	payload, _ := json.Marshal(map[string]string{
		"kind": "new_issue", "name": "x", "channel": "webhook",
		"target_url": "http://127.0.0.1/steal",
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/alerts", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+secret)
	h.ServeHTTP(rr, req)
	if rr.Code != 400 {
		t.Fatalf("want 400 ssrf, got %d %s", rr.Code, rr.Body.String())
	}
}

func TestBatchIngestJSON(t *testing.T) {
	s, _, secret := testAPI(t)
	raw, _ := lep.Encode(lep.Header{Type: lep.TypeHealth, Architecture: 1}, nil)
	body, _ := json.Marshal(map[string]any{
		"events": []map[string]any{
			{"event_id": "batch-1-" + time.Now().Format("150405.000"), "payload": raw},
		},
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/events:batch", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/json")
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["accepted"] == nil {
		t.Fatal(resp)
	}
}

func TestIngestConflictDifferentHash(t *testing.T) {
	// same event_id + different payload → 422 conflict (not 202, not silent 409 success)
	s, _, secret := testAPI(t)
	h := s.Handler()
	// Different LEP sequences → different payload hashes for the same client event_id.
	raw1, err := lep.Encode(lep.Header{Type: lep.TypeError, Architecture: 1, Sequence: 1, EventID: 42}, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw2, err := lep.Encode(lep.Header{Type: lep.TypeError, Architecture: 1, Sequence: 2, EventID: 42}, nil)
	if err != nil {
		t.Fatal(err)
	}
	idem := "evt-conflict-" + time.Now().Format("150405.000000")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest", bytes.NewReader(raw1))
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Idempotency-Key", idem)
	h.ServeHTTP(rr, req)
	if rr.Code != 202 {
		t.Fatalf("first ingest %d %s", rr.Code, rr.Body.String())
	}

	// true duplicate (same bytes)
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/ingest", bytes.NewReader(raw1))
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Idempotency-Key", idem)
	h.ServeHTTP(rr, req)
	if rr.Code != 202 {
		t.Fatalf("duplicate %d %s", rr.Code, rr.Body.String())
	}
	var dup map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &dup)
	if dup["duplicate"] != true {
		t.Fatalf("want duplicate body got %v", dup)
	}

	// conflict: same id, different payload
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/ingest", bytes.NewReader(raw2))
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Idempotency-Key", idem)
	h.ServeHTTP(rr, req)
	if rr.Code != 422 {
		t.Fatalf("want 422 conflict got %d %s", rr.Code, rr.Body.String())
	}
	var errBody map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &errBody)
	errObj, _ := errBody["error"].(map[string]any)
	if errObj == nil || errObj["code"] != "conflict" {
		t.Fatalf("want error.code=conflict got %s", rr.Body.String())
	}
}

func TestSearchAndPagination(t *testing.T) {
	s, st, _ := testAPI(t)
	secret := testUISession(t, st, "search-"+time.Now().Format("150405.000000")+"@t.local", "viewer")
	h := s.Handler()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=test", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/issues?limit=5&offset=0", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
}
