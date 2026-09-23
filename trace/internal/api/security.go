package api

import (
	"net/http"
	"strings"
	"time"
)

// timeNow is a variable that can be overridden in tests.
var timeNow = time.Now

// SecurityHeadersConfig holds security header configuration.
type SecurityHeadersConfig struct {
	// ContentSecurityPolicy is the Content-Security-Policy header value.
	ContentSecurityPolicy string
	// XFrameOptions is the X-Frame-Options header value.
	XFrameOptions string
	// XContentTypeOptions is the X-Content-Type-Options header value.
	XContentTypeOptions string
	// StrictTransportSecurity is the Strict-Transport-Security header value.
	StrictTransportSecurity string
	// ReferrerPolicy is the Referrer-Policy header value.
	ReferrerPolicy string
	// PermissionsPolicy is the Permissions-Policy header value.
	PermissionsPolicy string
}

// DefaultSecurityHeadersConfig returns default security header configuration.
func DefaultSecurityHeadersConfig() SecurityHeadersConfig {
	return SecurityHeadersConfig{
		ContentSecurityPolicy:   "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self' wss: ws:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
		XFrameOptions:           "DENY",
		XContentTypeOptions:     "nosniff",
		StrictTransportSecurity: "max-age=31536000; includeSubDomains; preload",
		ReferrerPolicy:          "strict-origin-when-cross-origin",
		PermissionsPolicy:       "camera=(), microphone=(), geolocation=(), payment=()",
	}
}

// SecurityHeadersMiddleware returns the security headers middleware handler.
func SecurityHeadersMiddleware(config SecurityHeadersConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Security-Policy", config.ContentSecurityPolicy)
			w.Header().Set("X-Frame-Options", config.XFrameOptions)
			w.Header().Set("X-Content-Type-Options", config.XContentTypeOptions)
			w.Header().Set("Strict-Transport-Security", config.StrictTransportSecurity)
			w.Header().Set("Referrer-Policy", config.ReferrerPolicy)
			w.Header().Set("Permissions-Policy", config.PermissionsPolicy)

			// Add X-XSS-Protection for older browsers.
			w.Header().Set("X-XSS-Protection", "1; mode=block")

			// Add X-DNS-Prefetch-Control.
			w.Header().Set("X-DNS-Prefetch-Control", "off")

			// Add X-Download-Options.
			w.Header().Set("X-Download-Options", "noopen")

			// Add X-Permitted-Cross-Domain-Policies.
			w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")

			next.ServeHTTP(w, r)
		})
	}
}

// RequestLoggingMiddleware logs incoming requests for audit trail.
type RequestLoggingMiddleware struct {
	logger interface {
		Info(msg string, keysAndValues ...any)
		Error(msg string, keysAndValues ...any)
	}
}

// NewRequestLoggingMiddleware creates a new RequestLoggingMiddleware.
func NewRequestLoggingMiddleware(logger interface {
	Info(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}) *RequestLoggingMiddleware {
	return &RequestLoggingMiddleware{logger: logger}
}

// Middleware returns the request logging middleware handler.
func (m *RequestLoggingMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := timeNow()

		// Wrap response writer to capture status code.
		sw := &statusWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(sw, r)

		duration := timeNow().Sub(start)

		m.logger.Info("request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.statusCode,
			"duration", duration,
			"client_ip", getClientIP(r),
			"user_agent", r.UserAgent(),
		)
	})
}

// statusWriter wraps http.ResponseWriter to capture status code.
type statusWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// getClientIP extracts the client IP from the request.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	// Check X-Real-IP header.
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr.
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		return ip[:idx]
	}
	return ip
}
