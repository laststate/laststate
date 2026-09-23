package api

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRateLimiter(t *testing.T) {
	rl := newRateLimiter(5, 1000)
	ip := "1.2.3.4"
	for i := 0; i < 5; i++ {
		if !rl.allow(ip) {
			t.Fatalf("should allow %d", i)
		}
	}
	if rl.allow(ip) {
		t.Fatal("should rate limit")
	}
	// different IP ok
	if !rl.allow("9.9.9.9") {
		t.Fatal()
	}
}

func TestConnLimiter(t *testing.T) {
	cl := newConnLimiter(2)
	if !cl.acquire("a") || !cl.acquire("a") {
		t.Fatal()
	}
	if cl.acquire("a") {
		t.Fatal("max")
	}
	cl.release("a")
	if !cl.acquire("a") {
		t.Fatal()
	}
}

func TestWithSecurityRateLimit(t *testing.T) {
	var hits atomic.Int64
	h := withSecurity(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(200)
	}), 3, 10)
	// fix remote addr
	for i := 0; i < 3; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		h.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Fatal(rr.Code)
		}
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("code=%d", rr.Code)
	}
}

func TestWithSecurityConnLimit(t *testing.T) {
	block := make(chan struct{})
	h := withSecurity(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
		w.WriteHeader(200)
	}), 1000, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.1.1.1:1"
		h.ServeHTTP(rr, req)
	}()
	time.Sleep(20 * time.Millisecond)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.1.1.1:2"
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d", rr.Code)
	}
	close(block)
	wg.Wait()
}

func TestClientIPFromXFF(t *testing.T) {
	// Untrusted peer: XFF must be ignored
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	req.Header.Set("X-Forwarded-For", "8.8.8.8, 1.1.1.1")
	if clientIPFrom(req, "leftmost") != "192.0.2.1" {
		t.Fatal(clientIPFrom(req, "leftmost"))
	}
	// Trusted proxy: honor XFF
	ConfigureTrustedProxies("10.0.0.0/8")
	defer ConfigureTrustedProxies("")
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "10.1.2.3:9999"
	req2.Header.Set("X-Forwarded-For", "8.8.8.8, 1.1.1.1")
	if clientIPFrom(req2, "leftmost") != "8.8.8.8" {
		t.Fatal(clientIPFrom(req2, "leftmost"))
	}
}
