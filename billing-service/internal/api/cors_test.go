package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSMiddlewareWildcardHeaders(t *testing.T) {
	handler := CORSMiddleware(DefaultCORSConfig())(noopHandler())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/plans", nil)
	req.Header.Set("Origin", "https://app.laststate.io")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}
	// The middleware echoes the matched configured origin (never "*"):
	// wildcard origins are incompatible with credentialed requests.
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.laststate.io" {
		t.Errorf("Allow-Origin = %q, want echoed origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("missing Access-Control-Allow-Methods")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("missing Access-Control-Allow-Headers")
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got == "" {
		t.Error("missing Access-Control-Max-Age")
	}
}

func TestCORSMiddlewareDisallowedOrigin(t *testing.T) {
	cfg := DefaultCORSConfig()
	cfg.AllowedOrigins = []string{"https://allowed.example"}
	handler := CORSMiddleware(cfg)(noopHandler())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/plans", nil)
	req.Header.Set("Origin", "https://evil.example")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("unexpected Allow-Origin %q for disallowed origin", got)
	}
}

func TestCORSMiddlewarePreflight(t *testing.T) {
	handler := CORSMiddleware(DefaultCORSConfig())(noopHandler())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/v1/plans", nil)
	req.Header.Set("Origin", "https://app.laststate.io")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("got status %d, want 204", rec.Code)
	}
}

func TestCORSMiddlewareCredentialsEchoesOrigin(t *testing.T) {
	cfg := DefaultCORSConfig()
	cfg.AllowedOrigins = []string{"https://app.laststate.io"}
	cfg.AllowCredentials = true
	handler := CORSMiddleware(cfg)(noopHandler())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/plans", nil)
	req.Header.Set("Origin", "https://app.laststate.io")
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.laststate.io" {
		t.Errorf("Allow-Origin = %q, want echoed origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q, want true", got)
	}
	if got := rec.Header().Get("Access-Control-Expose-Headers"); got == "" {
		t.Error("missing Access-Control-Expose-Headers")
	}
}

func TestIsOriginAllowed(t *testing.T) {
	if !isOriginAllowed("", []string{"*"}) {
		t.Error("empty origin should be allowed")
	}
	if !isOriginAllowed("https://a.example", []string{"*"}) {
		t.Error("wildcard should allow any origin")
	}
	if !isOriginAllowed("https://a.example", []string{"https://a.example"}) {
		t.Error("exact origin should be allowed")
	}
	if isOriginAllowed("https://b.example", []string{"https://a.example"}) {
		t.Error("non-matching origin should be rejected")
	}
}

func TestContains(t *testing.T) {
	if !contains([]string{"a", "b"}, "a") {
		t.Error("contains should find a")
	}
	if contains([]string{"a", "b"}, "z") {
		t.Error("contains should not find z")
	}
}
