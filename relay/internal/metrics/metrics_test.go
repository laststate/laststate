// SPDX-License-Identifier: Apache-2.0
package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/laststate/relay/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.OpenWithOptions(dir, store.Options{
		MaxSpoolBytes:      1 << 20,
		MinFreeBytes:       1 << 16,
		DeliveredRetention: 0,
		PressurePolicy:     "reject-new",
		FsyncMode:          "none",
		DeliveryMode:       "mirror",
	})
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestServeHTTP(t *testing.T) {
	s := openTestStore(t)
	handler := Handler{Store: s}

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, "laststate_relay_scrape_timestamp_seconds") {
		t.Error("expected timestamp metric")
	}
}

func TestServeHTTPError(t *testing.T) {
	// Use a closed store to provoke an error.
	dir := t.TempDir()
	s, err := store.OpenWithOptions(dir, store.Options{
		MaxSpoolBytes:      1 << 20,
		MinFreeBytes:       1 << 16,
		DeliveredRetention: 0,
		PressurePolicy:     "reject-new",
		FsyncMode:          "none",
		DeliveryMode:       "mirror",
	})
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	handler := Handler{Store: s}

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestContentTye(t *testing.T) {
	s := openTestStore(t)
	handler := Handler{Store: s}

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
}
