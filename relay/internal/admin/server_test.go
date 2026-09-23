// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEndpointAuthRejectsBadBearer(t *testing.T) {
	server := &Server{AdminToken: "secret"}
	handler := withEndpointAuth(server, EndpointAuth{"/v1/status": AuthAdmin}, AuthAdmin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestEndpointAuthAcceptsMatchingBearer(t *testing.T) {
	server := &Server{AdminToken: "secret"}
	handler := withEndpointAuth(server, EndpointAuth{"/v1/status": AuthAdmin}, AuthAdmin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestEndpointAuthNone(t *testing.T) {
	server := &Server{AdminToken: "secret"}
	handler := withEndpointAuth(server, EndpointAuth{"/health": AuthNone}, AuthAdmin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestEndpointAuthDefaultAdmin(t *testing.T) {
	server := &Server{AdminToken: "secret"}
	handler := withEndpointAuth(server, EndpointAuth{"/v1/status": AuthNone}, AuthAdmin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/secret", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized for default admin path, got %d", rec.Code)
	}
}

func TestIngestHandlerRequiresIngestToken(t *testing.T) {
	server := &Server{AdminToken: "admin-secret", IngestToken: "ingest-secret"}
	handler := server.IngestHandler()

	req := httptest.NewRequest(http.MethodPost, "/v1/ingest", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/ingest", nil)
	req.Header.Set("Authorization", "Bearer ingest-secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("ingest token was rejected")
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/ingest", nil)
	req.Header.Set("Authorization", "Bearer admin-secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("admin token must not be accepted by the ingest API, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/ingest/capabilities", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("capabilities endpoint should be public")
	}
}
