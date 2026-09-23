package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/laststate/billing-service/internal/billing"
	"github.com/laststate/billing-service/internal/webhook"
)

func newV2Router(t *testing.T, svc *billing.Service) http.Handler {
	t.Helper()
	ent := newHandlersEntitlementService(t)
	webhookHandler := webhook.NewHandler(svc, ent, zap.NewNop(), "", "", "")
	k := validKey()
	auth := NewAuthMiddleware(AuthConfig{APIKeyStore: newFakeKeyStore(k), EnableAPIKeyAuth: true}, zap.NewNop())
	rl := NewRateLimiter(RateLimiterConfig{RequestsPerMinute: 100}, zap.NewNop())
	return NewRouter(svc, ent, webhookHandler, auth, rl, zap.NewNop(), DefaultRouterConfig())
}

func v2KeyHeader(r *http.Request) *http.Request {
	r.Header.Set("X-API-Key", "test-key-abc")
	return r
}

func TestWebhookEndpointsCRUD(t *testing.T) {
	billingSvc := newHandlersBillingService()
	billingSvc.SetEventDispatcher(billing.NewEventDispatcher())
	r := newV2Router(t, billingSvc)

	org := uuid.New().String()
	createBody, _ := json.Marshal(map[string]any{
		"organization_id": org,
		"url":             "https://example.com/hooks/billing",
		"secret":          "s3cret",
		"events":          []string{"invoice.paid"},
	})
	req := v2KeyHeader(httptest.NewRequest(http.MethodPost, "/v1/webhook-endpoints", bytes.NewReader(createBody)))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create got %d (%s), want 201", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created["url"] != "https://example.com/hooks/billing" {
		t.Fatalf("unexpected create response: %v", created)
	}
	if _, ok := created["secret"]; ok {
		t.Fatal("secret must not be echoed in responses")
	}

	req2 := v2KeyHeader(httptest.NewRequest(http.MethodGet, "/v1/webhook-endpoints?organization_id="+org, nil))
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("list got %d, want 200", rec2.Code)
	}

	id, _ := created["id"].(string)
	req3 := v2KeyHeader(httptest.NewRequest(http.MethodDelete, "/v1/webhook-endpoints/"+id, nil))
	rec3 := httptest.NewRecorder()
	r.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusNoContent {
		t.Fatalf("delete got %d, want 204", rec3.Code)
	}

	req4 := v2KeyHeader(httptest.NewRequest(http.MethodDelete, "/v1/webhook-endpoints/"+id, nil))
	rec4 := httptest.NewRecorder()
	r.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusNotFound {
		t.Fatalf("second delete got %d, want 404", rec4.Code)
	}
}

func TestWebhookEndpointsDisabled(t *testing.T) {
	billingSvc := newHandlersBillingService() // no dispatcher
	h := createWebhookEndpoint(billingSvc)
	rec := doRequest(t, h, http.MethodPost, "/v1/webhook-endpoints",
		[]byte(`{"organization_id":"`+uuid.New().String()+`","url":"https://x.example/h","secret":"s"}`))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", rec.Code)
	}
}

func TestNowpaymentsWebhookRouteExists(t *testing.T) {
	billingSvc := newHandlersBillingService()
	r := newV2Router(t, billingSvc)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhooks/nowpayments", bytes.NewReader([]byte(`{}`))))
	if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
		t.Fatalf("nowpayments route missing (got %d)", rec.Code)
	}
}
