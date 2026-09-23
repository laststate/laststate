package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"go.uber.org/zap"
)

func TestRateLimiterAllowsWithinLimit(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{RequestsPerMinute: 3}, zap.NewNop())
	handler := rl.Middleware(noopHandler())

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/plans", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: got status %d, want 200", i, rec.Code)
		}
		if rec.Header().Get("X-RateLimit-Limit") == "" {
			t.Fatalf("request %d: missing X-RateLimit-Limit", i)
		}
	}
}

func TestRateLimiterRejectsOverLimit(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{RequestsPerMinute: 2}, zap.NewNop())
	handler := rl.Middleware(noopHandler())

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/plans", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: got status %d, want 200", i, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/plans", nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("got status %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After = %q, want 60", got)
	}
}

func TestRateLimiterSkipsHealthAndMetrics(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{RequestsPerMinute: 1}, zap.NewNop())
	handler := rl.Middleware(noopHandler())

	for _, path := range []string{"/health", "/metrics"} {
		for i := 0; i < 3; i++ {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("%s request %d: got status %d, want 200", path, i, rec.Code)
			}
		}
	}
}

func TestRateLimiterSeparatesByIP(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{RequestsPerMinute: 1}, zap.NewNop())
	handler := rl.Middleware(noopHandler())

	reqA := httptest.NewRequest(http.MethodGet, "/v1/plans", nil)
	reqA.Header.Set("X-Forwarded-For", "1.1.1.1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, reqA)
	if rec.Code != http.StatusOK {
		t.Fatalf("first request: got %d, want 200", rec.Code)
	}

	reqA2 := httptest.NewRequest(http.MethodGet, "/v1/plans", nil)
	reqA2.Header.Set("X-Forwarded-For", "1.1.1.1")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, reqA2)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second same-IP request: got %d, want 429", rec.Code)
	}

	reqB := httptest.NewRequest(http.MethodGet, "/v1/plans", nil)
	reqB.Header.Set("X-Forwarded-For", "2.2.2.2")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, reqB)
	if rec.Code != http.StatusOK {
		t.Fatalf("different-IP request: got %d, want 200", rec.Code)
	}
}

func TestGetClientIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/v1/plans", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.1, 203.0.113.2")
	if got := getClientIP(r); got != "203.0.113.1" {
		t.Errorf("X-Forwarded-For = %q, want first hop", got)
	}

	r = httptest.NewRequest(http.MethodGet, "/v1/plans", nil)
	r.Header.Set("X-Real-IP", "198.51.100.7")
	if got := getClientIP(r); got != "198.51.100.7" {
		t.Errorf("X-Real-IP = %q, want 198.51.100.7", got)
	}

	r = httptest.NewRequest(http.MethodGet, "/v1/plans", nil)
	r.RemoteAddr = "127.0.0.1:8080"
	if got := getClientIP(r); got != "127.0.0.1" {
		t.Errorf("RemoteAddr with port = %q, want 127.0.0.1", got)
	}

	r = httptest.NewRequest(http.MethodGet, "/v1/plans", nil)
	r.RemoteAddr = "10.0.0.1"
	if got := getClientIP(r); got != "10.0.0.1" {
		t.Errorf("RemoteAddr without port = %q, want 10.0.0.1", got)
	}
}

func TestGetClientIPIPv6(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/v1/plans", nil)
	r.RemoteAddr = "[2001:db8::1]:8080"
	if got := getClientIP(r); got != "2001:db8::1" {
		t.Errorf("IPv6 RemoteAddr = %q, want 2001:db8::1", got)
	}

	r = httptest.NewRequest(http.MethodGet, "/v1/plans", nil)
	r.Header.Set("X-Forwarded-For", "2001:db8::2, 203.0.113.9")
	if got := getClientIP(r); got != "2001:db8::2" {
		t.Errorf("IPv6 X-Forwarded-For = %q, want 2001:db8::2", got)
	}
}

func TestRateLimitHeadersAreNumeric(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{RequestsPerMinute: 5, BurstSize: 5}, zap.NewNop())
	rec := httptest.NewRecorder()
	rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/plans", nil))

	if got := rec.Header().Get("X-RateLimit-Limit"); got != "5" {
		t.Errorf("X-RateLimit-Limit = %q, want \"5\"", got)
	}
	if got := rec.Header().Get("X-RateLimit-Remaining"); got != "4" {
		t.Errorf("X-RateLimit-Remaining = %q, want \"4\"", got)
	}
	if reset := rec.Header().Get("X-RateLimit-Reset"); reset == "" {
		t.Error("X-RateLimit-Reset is empty")
	} else if _, err := strconv.ParseInt(reset, 10, 64); err != nil {
		t.Errorf("X-RateLimit-Reset = %q, not a unix timestamp: %v", reset, err)
	}
}
