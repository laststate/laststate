package api

import (
	"net/http"
	"os"
	"strconv"
	"strings"
)

// CORSConfig holds CORS configuration.
type CORSConfig struct {
	// AllowedOrigins is the list of allowed origins. Use ["*"] for all.
	AllowedOrigins []string
	// AllowedMethods is the list of allowed HTTP methods.
	AllowedMethods []string
	// AllowedHeaders is the list of allowed headers.
	AllowedHeaders []string
	// AllowCredentials enables credentials support.
	AllowCredentials bool
	// MaxAge is the maximum age of the CORS preflight cache in seconds.
	MaxAge int
}

// DefaultCORSConfig returns default CORS configuration.
// Default is restrictive — production must set BILLING_CORS_ORIGINS env.
// Wildcard "*" is rejected unless explicitly set via CORSConfigFromEnv.
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowedOrigins: []string{
			"https://app.laststate.io",
			"https://billing.laststate.io",
			"http://localhost:3000",
			"http://localhost:5173",
		},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowedHeaders:   []string{"Content-Type", "Authorization", "X-API-Key", "X-Requested-With"},
		AllowCredentials: false,
		MaxAge:           86400, // 24 hours
	}
}

// CORSConfigFromEnv builds CORS config from BILLING_CORS_ORIGINS env var.
// Comma-separated list; "*" allowed only when explicitly set and AllowCredentials is false.
func CORSConfigFromEnv() CORSConfig {
	cfg := DefaultCORSConfig()
	if v := strings.TrimSpace(envOrEmpty("BILLING_CORS_ORIGINS")); v != "" {
		parts := strings.Split(v, ",")
		origins := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				origins = append(origins, p)
			}
		}
		if len(origins) > 0 {
			cfg.AllowedOrigins = origins
		}
	}
	return cfg
}

func envOrEmpty(k string) string {
	return strings.TrimSpace(os.Getenv(k))
}

// CORSMiddleware returns the CORS middleware handler.
func CORSMiddleware(config CORSConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Check if origin is allowed.
			if !isOriginAllowed(origin, config.AllowedOrigins) {
				next.ServeHTTP(w, r)
				return
			}

			// Set CORS headers.
			if config.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			} else if contains(config.AllowedOrigins, "*") {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}

			w.Header().Set("Access-Control-Allow-Methods", strings.Join(config.AllowedMethods, ", "))
			w.Header().Set("Access-Control-Allow-Headers", strings.Join(config.AllowedHeaders, ", "))
			w.Header().Set("Access-Control-Max-Age", strconv.Itoa(config.MaxAge))

			if config.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Expose-Headers", "X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset")
			}

			// Handle preflight requests.
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// isOriginAllowed checks if the origin is in the allowed list.
func isOriginAllowed(origin string, allowed []string) bool {
	if origin == "" {
		return true // No origin header = same-origin or non-browser.
	}
	for _, a := range allowed {
		if a == "*" || a == origin {
			return true
		}
	}
	return false
}

// contains checks if a string slice contains a string.
func contains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
