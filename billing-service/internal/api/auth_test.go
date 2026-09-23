package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/laststate/billing-service/internal/store"
)

// fakeKeyStore implements store.APIKeyStore with a mutable key map and call count.
type fakeKeyStore struct {
	mu    sync.Mutex
	keys  map[string]store.APIKey
	calls int
}

func newFakeKeyStore(keys ...store.APIKey) *fakeKeyStore {
	f := &fakeKeyStore{keys: make(map[string]store.APIKey)}
	for _, k := range keys {
		f.keys[k.Key] = k
	}
	return f
}

func (f *fakeKeyStore) GetByKey(_ context.Context, key string) (store.APIKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	k, ok := f.keys[key]
	if !ok {
		return store.APIKey{}, store.ErrNotFound
	}
	return k, nil
}

func (f *fakeKeyStore) revoke(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if k, ok := f.keys[key]; ok {
		k.Active = false
		f.keys[key] = k
	}
}

func (f *fakeKeyStore) lookupCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func validKey() store.APIKey {
	return store.APIKey{
		ID:     uuid.New(),
		Key:    "test-key-abc",
		Name:   "test",
		OrgID:  uuid.New(),
		Active: true,
	}
}

func noopHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func requestWithKey(key string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/v1/plans", nil)
	req.Header.Set("X-API-Key", key)
	return req
}

func TestAuthMiddlewareServesFromCache(t *testing.T) {
	key := validKey()
	storeMock := newFakeKeyStore(key)
	auth := NewAuthMiddleware(AuthConfig{APIKeyStore: storeMock, EnableAPIKeyAuth: true}, zap.NewNop())
	handler := auth.Middleware(noopHandler())

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, requestWithKey(key.Key))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: got status %d, want 200", i, rec.Code)
		}
	}
	// Only the first request should hit the store; the rest are cached.
	if calls := storeMock.lookupCalls(); calls != 1 {
		t.Fatalf("store lookups = %d, want 1 (cache hit)", calls)
	}
}

func TestAuthMiddlewareRevalidatesAfterTTL(t *testing.T) {
	key := validKey()
	storeMock := newFakeKeyStore(key)
	auth := NewAuthMiddleware(AuthConfig{APIKeyStore: storeMock, EnableAPIKeyAuth: true}, zap.NewNop())
	auth.apiKeyTTL = 50 * time.Millisecond
	handler := auth.Middleware(noopHandler())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithKey(key.Key))
	if rec.Code != http.StatusOK {
		t.Fatalf("first request: got status %d, want 200", rec.Code)
	}

	storeMock.revoke(key.Key)
	time.Sleep(60 * time.Millisecond)

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithKey(key.Key))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("post-expiry request: got status %d, want 403 (revoked key)", rec.Code)
	}
	if calls := storeMock.lookupCalls(); calls != 2 {
		t.Fatalf("store lookups = %d, want 2 (revalidated after TTL)", calls)
	}
}

func TestAuthMiddlewareMissingKey(t *testing.T) {
	auth := NewAuthMiddleware(AuthConfig{APIKeyStore: newFakeKeyStore(), EnableAPIKeyAuth: true}, zap.NewNop())
	handler := auth.Middleware(noopHandler())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/plans", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401", rec.Code)
	}
}

func TestAuthMiddlewareUnknownKey(t *testing.T) {
	auth := NewAuthMiddleware(AuthConfig{APIKeyStore: newFakeKeyStore(), EnableAPIKeyAuth: true}, zap.NewNop())
	handler := auth.Middleware(noopHandler())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithKey("missing-key"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401", rec.Code)
	}
}

func TestAuthMiddlewareInactiveKey(t *testing.T) {
	key := validKey()
	key.Active = false
	auth := NewAuthMiddleware(AuthConfig{APIKeyStore: newFakeKeyStore(key), EnableAPIKeyAuth: true}, zap.NewNop())
	handler := auth.Middleware(noopHandler())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithKey(key.Key))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("got status %d, want 403", rec.Code)
	}
}

func TestAuthMiddlewareSkipsWebhooks(t *testing.T) {
	auth := NewAuthMiddleware(AuthConfig{APIKeyStore: newFakeKeyStore(), EnableAPIKeyAuth: true}, zap.NewNop())
	handler := auth.Middleware(noopHandler())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhooks/stripe", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200 (webhook bypass)", rec.Code)
	}
}

func TestAuthMiddlewareStoreErrorIs500(t *testing.T) {
	failing := &failingKeyStore{}
	auth := NewAuthMiddleware(AuthConfig{APIKeyStore: failing, EnableAPIKeyAuth: true}, zap.NewNop())
	handler := auth.Middleware(noopHandler())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithKey("any-key"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d, want 500", rec.Code)
	}
}

func TestAPIKeyFromContext(t *testing.T) {
	key := validKey()
	ctx := context.WithValue(context.Background(), apiKeyContextKey, key)
	got, ok := APIKeyFromContext(ctx)
	if !ok || got.Key != key.Key {
		t.Fatalf("APIKeyFromContext returned %+v (ok=%v), want key %q", got, ok, key.Key)
	}
}

func TestExtractAPIKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/plans", nil)
	req.Header.Set("Authorization", "Bearer bearer-token")
	if got := extractAPIKey(req); got != "bearer-token" {
		t.Fatalf("Bearer extraction = %q, want %q", got, "bearer-token")
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/plans", nil)
	req.Header.Set("Authorization", "Token token-value")
	if got := extractAPIKey(req); got != "token-value" {
		t.Fatalf("Token extraction = %q, want %q", got, "token-value")
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/plans", nil)
	req.Header.Set("X-API-Key", "header-key")
	if got := extractAPIKey(req); got != "header-key" {
		t.Fatalf("X-API-Key extraction = %q, want %q", got, "header-key")
	}
}

// failingKeyStore returns a transient error for every lookup.
type failingKeyStore struct{}

func (f *failingKeyStore) GetByKey(_ context.Context, _ string) (store.APIKey, error) {
	return store.APIKey{}, errors.New("store unavailable")
}
