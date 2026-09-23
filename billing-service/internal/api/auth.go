// Package api provides the HTTP API for the billing service.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/laststate/billing-service/internal/store"
)

// AuthConfig holds authentication configuration.
type AuthConfig struct {
	// APIKeyStore is the source of truth for valid API keys.
	APIKeyStore store.APIKeyStore
	// JWTSecret is the secret for JWT validation (if using JWT auth).
	JWTSecret string
	// EnableJWTAuth enables JWT token authentication.
	EnableJWTAuth bool
	// EnableAPIKeyAuth enables API key authentication.
	EnableAPIKeyAuth bool
}

// AuthMiddleware handles authentication and authorization.
type AuthMiddleware struct {
	config AuthConfig
	logger *zap.Logger
	// apiKeys caches validated API keys to avoid repeated DB lookups.
	// Entries expire after apiKeyTTL so revocations take effect promptly.
	apiKeys   map[string]cachedAPIKey
	apiKeysMu sync.RWMutex
	apiKeyTTL time.Duration
}

// cachedAPIKey pairs a validated key with the time it was cached.
type cachedAPIKey struct {
	key      store.APIKey
	cachedAt time.Time
}

// NewAuthMiddleware creates a new AuthMiddleware.
func NewAuthMiddleware(config AuthConfig, logger *zap.Logger) *AuthMiddleware {
	return &AuthMiddleware{
		config:    config,
		logger:    logger,
		apiKeys:   make(map[string]cachedAPIKey),
		apiKeyTTL: 5 * time.Minute,
	}
}

// Middleware returns the authentication middleware handler.
func (m *AuthMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for webhook endpoints.
		if strings.HasPrefix(r.URL.Path, "/webhooks/") {
			next.ServeHTTP(w, r)
			return
		}

		apiKey := extractAPIKey(r)
		if apiKey == "" {
			writeJSONError(w, http.StatusUnauthorized, "missing_api_key", "API key is required")
			return
		}

		// Serve from cache while fresh, then revalidate against the store.
		if key, ok := m.cachedKey(apiKey); ok {
			// Add API key to context.
			ctx := context.WithValue(r.Context(), apiKeyContextKey, key)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Validate against store (with in-process caching to reduce DB load).
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		key, err := m.config.APIKeyStore.GetByKey(ctx, apiKey)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeJSONError(w, http.StatusUnauthorized, "invalid_api_key", "API key not found")
				return
			}
			m.logger.Error("failed to validate API key", zap.Error(err))
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authentication service unavailable")
			return
		}
		if !key.Active {
			writeJSONError(w, http.StatusForbidden, "api_key_inactive", "API key is inactive")
			return
		}

		// Cache the key and update last-used atomically under one lock.
		m.apiKeysMu.Lock()
		key.LastUsed = time.Now()
		m.apiKeys[apiKey] = cachedAPIKey{key: key, cachedAt: time.Now()}
		m.apiKeysMu.Unlock()

		// Add API key to context.
		ctx = context.WithValue(r.Context(), apiKeyContextKey, key)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// cachedKey returns the cached key for apiKey when it is present and fresh.
func (m *AuthMiddleware) cachedKey(apiKey string) (store.APIKey, bool) {
	m.apiKeysMu.RLock()
	entry, ok := m.apiKeys[apiKey]
	if !ok || time.Since(entry.cachedAt) > m.apiKeyTTL {
		m.apiKeysMu.RUnlock()
		if ok {
			// Drop the stale entry so the next request revalidates the store.
			m.apiKeysMu.Lock()
			delete(m.apiKeys, apiKey)
			m.apiKeysMu.Unlock()
		}
		return store.APIKey{}, false
	}
	m.apiKeysMu.RUnlock()
	return entry.key, true
}

// extractAPIKey extracts the API key from request headers.
func extractAPIKey(r *http.Request) string {
	// Check X-API-Key header first.
	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		return apiKey
	}

	// Check Authorization header.
	if auth := r.Header.Get("Authorization"); auth != "" {
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimPrefix(auth, "Bearer ")
		}
		if strings.HasPrefix(auth, "Token ") {
			return strings.TrimPrefix(auth, "Token ")
		}
	}

	return ""
}

// apiKeyContextKeyType is the type for the API key context key.
type apiKeyContextKeyType string

// apiKeyContextKey is the context key for the API key.
const apiKeyContextKey apiKeyContextKeyType = "api_key"

// APIKeyFromContext extracts the API key from the request context.
func APIKeyFromContext(ctx context.Context) (store.APIKey, bool) {
	key, ok := ctx.Value(apiKeyContextKey).(store.APIKey)
	return key, ok
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
