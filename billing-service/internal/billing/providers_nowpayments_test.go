package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestNowPaymentsCheckoutSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/invoice" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") == "" {
			t.Error("missing x-api-key header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"inv_123","invoice_url":"https://nowpayments.io/payment/inv_123"}`))
	}))
	defer srv.Close()

	p := NewNowPaymentsProvider("test-key")
	p.baseURL = srv.URL
	p.client = srv.Client()

	url, err := p.CreateCheckoutSession(context.Background(), uuid.New(), "pro", "usd")
	if err != nil {
		t.Fatalf("CreateCheckoutSession: %v", err)
	}
	if url == "" {
		t.Fatal("expected checkout url")
	}
}

func TestNowPaymentsNotConfigured(t *testing.T) {
	p := NewNowPaymentsProvider("")
	if _, err := p.CreateCheckoutSession(context.Background(), uuid.New(), "pro", "usd"); err == nil {
		t.Fatal("expected error when api key is empty")
	}
}

func TestNowPaymentsGetSubscription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"payment_id":"pay_1","payment_status":"finished","order_id":"org:pro"}`))
	}))
	defer srv.Close()

	p := NewNowPaymentsProvider("test-key")
	p.baseURL = srv.URL
	p.client = srv.Client()

	info, err := p.GetSubscription(context.Background(), "pay_1")
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if info.Status != "active" {
		t.Fatalf("expected active, got %s", info.Status)
	}
}

func TestNowPaymentsWebhookVerify(t *testing.T) {
	p := NewNowPaymentsProvider("k")
	payload := []byte(`{"payment_id":"pay_1","payment_status":"finished"}`)
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write(payload)
	sig := []byte(hex.EncodeToString(mac.Sum(nil)))

	data, err := p.VerifyWebhook(payload, sig, "secret")
	if err != nil {
		t.Fatalf("VerifyWebhook: %v", err)
	}
	if data["payment_id"] != "pay_1" {
		t.Fatalf("unexpected payload: %v", data)
	}
	if _, err := p.VerifyWebhook(payload, []byte("deadbeef"), "secret"); err == nil {
		t.Fatal("expected invalid signature error")
	}
}

func TestMercadoPagoPixNotConfigured(t *testing.T) {
	p := NewMercadoPagoProvider("")
	if _, err := p.CreatePixPayment(context.Background(), uuid.New(), "pro", "a@b.co"); err == nil {
		t.Fatal("expected error when api key is empty")
	}
}

func TestMercadoPagoPreapprovalRecurring(t *testing.T) {
	var got struct {
		PayerEmail string `json:"payer_email"`
		AutoRec    struct {
			Amount   float64 `json:"transaction_amount"`
			Currency string  `json:"currency_id"`
		} `json:"auto_recurring"`
	}
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"id":"pre_123","status":"pending"}`))
	}))
	defer srv.Close()

	p := NewMercadoPagoProvider("k")
	p.baseURL = srv.URL
	p.client = srv.Client()

	id, err := p.CreateSubscription(context.Background(), uuid.New(), "team", "buyer@acme.dev")
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if id != "pre_123" {
		t.Fatalf("id = %s, want pre_123", id)
	}
	if gotPath != "/preapproval" {
		t.Errorf("path = %s, want /preapproval (recurring, not one-off preferences)", gotPath)
	}
	if got.PayerEmail != "buyer@acme.dev" {
		t.Errorf("payer_email = %q, want buyer@acme.dev", got.PayerEmail)
	}
	// Amounts are currency units, not cents (49.00, never 4900).
	if got.AutoRec.Amount != 49 || got.AutoRec.Currency != "USD" {
		t.Errorf("auto_recurring = %+v, want {49 USD}", got.AutoRec)
	}

	if _, err := p.CreateSubscription(context.Background(), uuid.New(), "team", ""); err == nil {
		t.Fatal("expected error without payer email")
	}
}

func TestMercadoPagoPixRequiresEmail(t *testing.T) {
	p := NewMercadoPagoProvider("k")
	if _, err := p.CreatePixPayment(context.Background(), uuid.New(), "pro", ""); err == nil {
		t.Fatal("expected error for empty payer email")
	}
}

func TestMercadoPagoPixSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/payments" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		if req["payment_method_id"] != "pix" {
			t.Errorf("expected pix, got %v", req["payment_method_id"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":12345,"status":"pending","point_of_interaction":{"transaction_data":{"qr_code":"000201BR","qr_code_base64":"aGVsbG8="}}}`))
	}))
	defer srv.Close()

	p := NewMercadoPagoProvider("test-key")
	p.baseURL = srv.URL
	p.client = srv.Client()

	res, err := p.CreatePixPayment(context.Background(), uuid.New(), "pro", "buyer@example.com")
	if err != nil {
		t.Fatalf("CreatePixPayment: %v", err)
	}
	if res.QRCode != "000201BR" {
		t.Fatalf("unexpected qr: %s", res.QRCode)
	}
	if res.PaymentID != "12345" {
		t.Fatalf("unexpected payment id: %s", res.PaymentID)
	}
}

func TestMercadoPagoBoletoValidation(t *testing.T) {
	p := NewMercadoPagoProvider("")
	if _, err := p.CreateBoletoPayment(context.Background(), uuid.New(), "team", "a@b.co", "12345678901"); err == nil {
		t.Fatal("expected error when api key is empty")
	}
	pk := NewMercadoPagoProvider("k")
	if _, err := pk.CreateBoletoPayment(context.Background(), uuid.New(), "team", "", "12345678901"); err == nil {
		t.Fatal("expected error for empty payer email")
	}
	if _, err := pk.CreateBoletoPayment(context.Background(), uuid.New(), "team", "a@b.co", "123"); err == nil {
		t.Fatal("expected error for invalid fiscal document")
	}
}

func TestMercadoPagoBoletoSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/payments" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		if req["payment_method_id"] != "bolbradesco" {
			t.Errorf("expected bolbradesco, got %v", req["payment_method_id"])
		}
		if r.Header.Get("X-Idempotency-Key") == "" {
			t.Error("missing X-Idempotency-Key header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":98765,"status":"pending","date_of_expiration":"2026-09-21T00:00:00Z","transaction_details":{"external_resource_url":"https://mp/boleto/98765","barcode":{"content":"00190.00009 01234.567890 12345.678901 1 9999000004900"}}}`))
	}))
	defer srv.Close()

	p := NewMercadoPagoProvider("test-key")
	p.baseURL = srv.URL
	p.client = srv.Client()

	res, err := p.CreateBoletoPayment(context.Background(), uuid.New(), "team", "buyer@example.com", "12345678901")
	if err != nil {
		t.Fatalf("CreateBoletoPayment: %v", err)
	}
	if res.PaymentID != "98765" {
		t.Fatalf("unexpected payment id: %s", res.PaymentID)
	}
	if res.Barcode == "" || res.BankURL == "" {
		t.Fatalf("expected barcode+bank_url, got %+v", res)
	}
}

func TestProviderForAliases(t *testing.T) {
	db := newMockDB()
	svc := NewService(db, &mockProvider{}, &mockProvider{}, &mockProvider{})
	svc.SetNowPayments(NewNowPaymentsProvider("k"))

	for _, name := range []string{"stripe", "mercado_pago", "mp", "mercadopago", "crypto", "coinbase", "nowpayments", "np", "now_payments"} {
		if _, err := svc.providerFor(name); err != nil {
			t.Errorf("providerFor(%s): %v", name, err)
		}
	}
	if _, err := svc.providerFor("nope"); err == nil {
		t.Error("expected error for unknown provider")
	}
	svc2 := NewService(db, &mockProvider{}, &mockProvider{}, &mockProvider{})
	if _, err := svc2.providerFor("nowpayments"); err == nil {
		t.Error("expected error when nowpayments not attached")
	}
}
