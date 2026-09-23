// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

package admin

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// AuthPolicy describes the authentication required for a handler.
type AuthPolicy string

const (
	// AuthNone allows the request to pass without any authentication.
	AuthNone AuthPolicy = "none"
	// AuthBearer permits either the admin token or the ingest token.
	AuthBearer AuthPolicy = "bearer"
	// AuthAdmin requires the admin token specifically.
	AuthAdmin AuthPolicy = "admin"
	// AuthIngest requires the ingest token specifically.
	AuthIngest AuthPolicy = "ingest"
)

// EndpointAuth maps request paths to their required authentication policy.
type EndpointAuth map[string]AuthPolicy

// withEndpointAuth returns a middleware that enforces per-endpoint
// authentication policies. Routes not listed in policies use defaultPolicy.
func withEndpointAuth(server *Server, policies EndpointAuth, defaultPolicy AuthPolicy) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			policy, ok := policies[request.URL.Path]
			if !ok {
				policy = defaultPolicy
			}
			switch policy {
			case AuthNone:
				// No authentication required.
			case AuthBearer:
				if !verifyBearer(request, server.AdminToken, server.IngestToken) {
					writeError(writer, http.StatusUnauthorized, "unauthorized", "valid bearer token required")
					return
				}
			case AuthAdmin:
				if !verifyBearer(request, server.AdminToken, "") {
					writeError(writer, http.StatusUnauthorized, "admin_token_required", "admin token required")
					return
				}
			case AuthIngest:
				if !verifyBearer(request, "", server.IngestToken) {
					writeError(writer, http.StatusUnauthorized, "ingest_token_required", "valid ingest token required")
					return
				}
			}
			writer.Header().Set("X-Auth-Policy", string(policy))
			next.ServeHTTP(writer, request)
		})
	}
}

// verifyBearer checks the Authorization header against the expected tokens.
func verifyBearer(request *http.Request, adminToken, ingestToken string) bool {
	provided := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
	if adminToken != "" {
		return constantTimeEqual(provided, adminToken)
	}
	if ingestToken != "" {
		return constantTimeEqual(provided, ingestToken)
	}
	return true
}

// constantTimeEqual compares two strings using constant-time comparison.
func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var result byte
	for i := range a {
		result |= a[i] ^ b[i]
	}
	return result == 0
}

// RequestLog is the structured log entry emitted for every request.
type RequestLog struct {
	Method    string        `json:"method"`
	Path      string        `json:"path"`
	Status    int           `json:"status"`
	Duration  time.Duration `json:"duration_ms"`
	ClientIP  string        `json:"client_ip"`
	UserAgent string        `json:"user_agent"`
	RequestID string        `json:"request_id"`
}

// ResponseWriter captures the status code written to the response.
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *responseWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.status = status
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// RequestLoggingMiddleware wraps the next handler with structured request
// logging.
func RequestLoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			start := time.Now()
			rw := &responseWriter{
				ResponseWriter: writer,
				status:         http.StatusOK,
			}
			next.ServeHTTP(rw, request)
			duration := time.Since(start)

			switch {
			case rw.status >= 500:
				logger.Error("request completed with 5xx",
					slog.String("method", request.Method),
					slog.String("path", request.URL.Path),
					slog.Int("status", rw.status),
					slog.Duration("duration", duration),
				)
			case duration > time.Second:
				logger.Warn("slow request",
					slog.String("method", request.Method),
					slog.String("path", request.URL.Path),
					slog.Int("status", rw.status),
					slog.Duration("duration", duration),
				)
			default:
				logger.Info("request completed",
					slog.String("method", request.Method),
					slog.String("path", request.URL.Path),
					slog.Int("status", rw.status),
					slog.Duration("duration", duration),
				)
			}
		})
	}
}
