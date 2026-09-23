package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/laststate/billing-service/internal/billing"
	"github.com/laststate/billing-service/internal/entitlement"
	"github.com/laststate/billing-service/internal/store"
)

// webhookMockDB provides just enough storage for webhook processing.
type webhookMockDB struct {
	store.DB
	mu            sync.Mutex
	webhookEvents map[string]*store.WebhookEvent
}

func newWebhookMockDB() *webhookMockDB {
	return &webhookMockDB{webhookEvents: make(map[string]*store.WebhookEvent)}
}

func (m *webhookMockDB) GetWebhookEvent(_ context.Context, eventID string) (*store.WebhookEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	we, ok := m.webhookEvents[eventID]
	if !ok {
		return nil, nil
	}
	return we, nil
}

func (m *webhookMockDB) InsertWebhookEvent(_ context.Context, we *store.WebhookEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := *we
	m.webhookEvents[we.EventID] = &c
	return nil
}

func (m *webhookMockDB) UpdateInvoiceStatus(_ context.Context, _ string, _ string, _ time.Time) error {
	return nil
}

func (m *webhookMockDB) UpdateInvoiceStatusByProviderID(_ context.Context, _ string, _ string, _ string, _ time.Time) error {
	return nil
}

func (m *webhookMockDB) UpdateSubscriptionStatus(_ context.Context, _ string, _ string, _ time.Time) error {
	return nil
}

func (m *webhookMockDB) GetInvoiceByProviderID(_ context.Context, _, _ string) (*store.Invoice, error) {
	return nil, store.ErrNotFound
}

func (m *webhookMockDB) GetOrgByID(_ context.Context, _ uuid.UUID) (*store.Organization, error) {
	return nil, store.ErrNotFound
}

func (m *webhookMockDB) GetSubscriptionByProviderSubID(_ context.Context, _ string) (*store.Subscription, error) {
	return nil, store.ErrNotFound
}

func (m *webhookMockDB) InsertSubscription(_ context.Context, _ *store.Subscription) error {
	return nil
}

func (m *webhookMockDB) InsertPaymentAttempt(_ context.Context, _ *store.PaymentAttempt) error {
	return nil
}

// webhookMockProvider verifies by returning the parsed payload as the event map.
type webhookMockProvider struct {
	billing.PaymentProvider
}

func (p *webhookMockProvider) VerifyWebhook(payload []byte, _ []byte, _ string) (map[string]interface{}, error) {
	var m map[string]interface{}
	if err := json.Unmarshal(payload, &m); err != nil {
		return nil, err
	}
	return m, nil
}

type failingProvider struct {
	billing.PaymentProvider
}

func (p *failingProvider) VerifyWebhook(_ []byte, _ []byte, _ string) (map[string]interface{}, error) {
	return nil, errors.New("bad signature")
}

func newWebhookHandler(mpSecret, cryptoSecret, stripeSecret string) (*Handler, *webhookMockDB) {
	db := newWebhookMockDB()
	svc := billing.NewService(db, &webhookMockProvider{}, &webhookMockProvider{}, &webhookMockProvider{})
	svc.SetLogger(zap.NewNop())
	ent := entitlement.NewService("http://trace")
	ent.SetLogger(zap.NewNop())
	return NewHandler(svc, ent, zap.NewNop(), mpSecret, cryptoSecret, stripeSecret), db
}

func hmacHex(secret string, body []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// --- Signature verification ---

func TestVerifyMercadoPagoSignature(t *testing.T) {
	handler, _ := newWebhookHandler("mp-secret", "", "")
	body := []byte(`{"id":"evt_1"}`)

	valid := "hmac_sha256=" + hmacHex("mp-secret", body)
	if !handler.verifyMercadoPagoSignature(valid, body) {
		t.Fatal("rejected valid signature")
	}
	if handler.verifyMercadoPagoSignature("hmac_sha256="+hmacHex("wrong", body), body) {
		t.Fatal("accepted signature with wrong secret")
	}
	if handler.verifyMercadoPagoSignature("", body) {
		t.Fatal("accepted empty header")
	}
	if handler.verifyMercadoPagoSignature("nohash", body) {
		t.Fatal("accepted malformed header without =")
	}
	if handler.verifyMercadoPagoSignature("hmac_sha256=zzzz", body) {
		t.Fatal("accepted invalid hex")
	}
}

func TestVerifyMercadoPagoSignatureMissingSecret(t *testing.T) {
	handler, _ := newWebhookHandler("", "", "")
	if handler.verifyMercadoPagoSignature("hmac_sha256=abcd", []byte(`{}`)) {
		t.Fatal("accepted signature without configured secret")
	}
}

func TestVerifyCryptoSignature(t *testing.T) {
	handler, _ := newWebhookHandler("", "cb-secret", "")
	body := []byte(`{"id":"evt_1"}`)

	if !handler.verifyCryptoSignature("sha256="+hmacHex("cb-secret", body), body) {
		t.Fatal("rejected valid Coinbase format signature")
	}
	if handler.verifyCryptoSignature("sha256="+hmacHex("wrong", body), body) {
		t.Fatal("accepted Coinbase signature with wrong secret")
	}
	if handler.verifyCryptoSignature("sha256=zz", body) {
		t.Fatal("accepted Coinbase signature with bad hex")
	}

	if !handler.verifyCryptoSignature(hmacHex("cb-secret", body), body) {
		t.Fatal("rejected valid bare-hex signature")
	}
	if handler.verifyCryptoSignature("zz", body) {
		t.Fatal("accepted bare signature with bad hex")
	}
}

func TestVerifyCryptoSignatureMissingSecretAndHeader(t *testing.T) {
	handler, _ := newWebhookHandler("", "", "")
	if handler.verifyCryptoSignature("sha256=abcd", []byte(`{}`)) {
		t.Fatal("accepted signature without configured secret")
	}

	handler, _ = newWebhookHandler("", "cb-secret", "")
	if handler.verifyCryptoSignature("", []byte(`{}`)) {
		t.Fatal("accepted empty header")
	}
}

// --- Stripe ---

func TestHandleStripeWebhookMissingSignature(t *testing.T) {
	handler, _ := newWebhookHandler("", "", "whsec_abc")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader([]byte(`{"id":"evt_1"}`)))
	handler.HandleStripeWebhook(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
}

func TestHandleStripeWebhookSecretNotConfigured(t *testing.T) {
	handler, _ := newWebhookHandler("", "", "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader([]byte(`{"id":"evt_1"}`)))
	req.Header.Set("Stripe-Signature", "sig")
	handler.HandleStripeWebhook(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d, want 500", rec.Code)
	}
}

func TestHandleStripeWebhookInvalidSignature(t *testing.T) {
	db := newWebhookMockDB()
	svc := billing.NewService(db, &failingProvider{}, &failingProvider{}, &failingProvider{})
	svc.SetLogger(zap.NewNop())
	ent := entitlement.NewService("http://trace")
	ent.SetLogger(zap.NewNop())
	handler := NewHandler(svc, ent, zap.NewNop(), "", "", "whsec_abc")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader([]byte(`{"id":"evt_1"}`)))
	req.Header.Set("Stripe-Signature", "bad")
	handler.HandleStripeWebhook(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401", rec.Code)
	}
}

func TestHandleStripeWebhookMissingEventID(t *testing.T) {
	handler, _ := newWebhookHandler("", "", "whsec_abc")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader([]byte(`{"type":"test"}`)))
	req.Header.Set("Stripe-Signature", "sig")
	handler.HandleStripeWebhook(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
}

func TestHandleStripeWebhookSuccess(t *testing.T) {
	handler, db := newWebhookHandler("", "", "whsec_abc")
	payload := []byte(`{"id":"evt_stripe_ok","type":"invoice.paid","data":{"id":"inv_1"}}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader(payload))
	req.Header.Set("Stripe-Signature", "whatever")
	handler.HandleStripeWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	if _, ok := db.webhookEvents["evt_stripe_ok"]; !ok {
		t.Error("stripe event not persisted for idempotency")
	}
}

// --- Mercado Pago ---

func TestHandleMercadoPagoWebhookSuccess(t *testing.T) {
	handler, db := newWebhookHandler("mp-secret", "", "")
	body := []byte(`{"id":"mp_evt_ok","type":"payment.updated","data":{"id":"p1","status_code":200,"invoice_id":"inv_1"}}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/mercado-pago", bytes.NewReader(body))
	req.Header.Set("x-signature", "hmac_sha256="+hmacHex("mp-secret", body))
	handler.HandleMercadoPagoWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	if _, ok := db.webhookEvents["mp_evt_ok"]; !ok {
		t.Error("mercado pago event not persisted")
	}
}

func TestHandleMercadoPagoWebhookInvalidSignature(t *testing.T) {
	handler, _ := newWebhookHandler("mp-secret", "", "")
	body := []byte(`{"id":"mp_evt_1"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/mercado-pago", bytes.NewReader(body))
	req.Header.Set("x-signature", "hmac_sha256="+hmacHex("wrong", body))
	handler.HandleMercadoPagoWebhook(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401", rec.Code)
	}
}

func TestHandleMercadoPagoWebhookBadPayload(t *testing.T) {
	handler, _ := newWebhookHandler("mp-secret", "", "")
	body := []byte(`{invalid`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/mercado-pago", bytes.NewReader(body))
	req.Header.Set("x-signature", "hmac_sha256="+hmacHex("mp-secret", body))
	handler.HandleMercadoPagoWebhook(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
}

// --- Crypto ---

func TestHandleCryptoWebhookSuccess(t *testing.T) {
	handler, db := newWebhookHandler("", "cb-secret", "")
	body := []byte(`{"id":"cb_evt_ok","type":"charge:confirmed","charge_id":"cb_charge_1"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/crypto", bytes.NewReader(body))
	req.Header.Set("x-cb-signature", "sha256="+hmacHex("cb-secret", body))
	handler.HandleCryptoWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	if _, ok := db.webhookEvents["cb_evt_ok"]; !ok {
		t.Error("crypto event not persisted")
	}
}

func TestHandleCryptoWebhookNowPaymentsHeader(t *testing.T) {
	handler, _ := newWebhookHandler("", "np-secret", "")
	body := []byte(`{"id":"np_evt_1","type":"charge:confirmed"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/crypto", bytes.NewReader(body))
	req.Header.Set("x-nowpayments-signature", hmacHex("np-secret", body))
	handler.HandleCryptoWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleCryptoWebhookInvalidSignature(t *testing.T) {
	handler, _ := newWebhookHandler("", "cb-secret", "")
	body := []byte(`{"id":"cb_evt_1"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/crypto", bytes.NewReader(body))
	req.Header.Set("x-cb-signature", "sha256="+hmacHex("wrong", body))
	handler.HandleCryptoWebhook(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401", rec.Code)
	}
}

// --- Helpers ---

func TestGetString(t *testing.T) {
	m := map[string]interface{}{"a": "x", "b": 5}
	if got := getString(m, "a"); got != "x" {
		t.Errorf("getString(a) = %q, want x", got)
	}
	if got := getString(m, "b"); got != "" {
		t.Errorf("getString(b) = %q, want empty (non-string)", got)
	}
	if got := getString(m, "missing"); got != "" {
		t.Errorf("getString(missing) = %q, want empty", got)
	}
}

func TestGetUUID(t *testing.T) {
	id := uuid.New()
	m := map[string]interface{}{"id": id.String()}
	got, err := getUUID(m, "id")
	if err != nil || got != id {
		t.Errorf("getUUID = %v, %v; want %v", got, err, id)
	}
	if _, err := getUUID(map[string]interface{}{}, "missing"); err == nil {
		t.Error("expected error for missing key")
	}
	if _, err := getUUID(map[string]interface{}{"id": "not-a-uuid"}, "id"); err == nil {
		t.Error("expected error for invalid UUID")
	}
}
