// Package billing implements the NOWPayments crypto gateway provider and
// Mercado Pago Pix payment support.
//
// NOWPayments API docs: https://documenter.getpostman.com/view/23356563/2s9YyvBKGk
// Mercado Pago Pix: https://www.mercadopago.com.br/developers/pt/docs/checkout-api/integration-configuration/card/integrate-via-cardform
package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// nowPaymentsAPIBase is the default NOWPayments API host.
const nowPaymentsAPIBase = "https://api.nowpayments.io/v1"

// NowPaymentsProvider implements PaymentProvider for NOWPayments.
// Supports 300+ cryptocurrencies with auto-conversion to a chosen outcome coin.
type NowPaymentsProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewNowPaymentsProvider creates a new NOWPayments provider.
// apiKey is the NOWPayments API key (x-api-key header). Empty key means
// unconfigured: checkout creation fails closed, subscription reads fall back
// to test/dev mode so `go test` runs without network.
func NewNowPaymentsProvider(apiKey string) *NowPaymentsProvider {
	return &NowPaymentsProvider{
		apiKey:  apiKey,
		baseURL: nowPaymentsAPIBase,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// nowPaymentsInvoiceRequest is the /v1/invoice creation payload.
type nowPaymentsInvoiceRequest struct {
	PriceAmount   float64 `json:"price_amount"`
	PriceCurrency string  `json:"price_currency"`
	PayCurrency   string  `json:"pay_currency,omitempty"`
	OrderID       string  `json:"order_id,omitempty"`
	OrderDesc     string  `json:"order_description,omitempty"`
	SuccessURL    string  `json:"success_url,omitempty"`
	CancelURL     string  `json:"cancel_url,omitempty"`
}

// CreateCheckoutSession creates a NOWPayments hosted invoice and returns the
// invoice_url the customer is redirected to.
func (p *NowPaymentsProvider) CreateCheckoutSession(ctx context.Context, orgID uuid.UUID, planID string, currency string) (string, error) {
	if p.apiKey == "" {
		return "", fmt.Errorf("nowpayments checkout is not configured (set NOWPAYMENTS_KEY)")
	}
	if currency == "" {
		currency = planCurrency(planID)
	}
	currency = strings.ToLower(currency)

	reqBody := nowPaymentsInvoiceRequest{
		PriceAmount:   float64(planPriceCents(planID)) / 100.0,
		PriceCurrency: currency,
		OrderID:       orgID.String() + ":" + planID,
		OrderDesc:     "LastState " + planID,
		SuccessURL:    "https://app.laststate.io/billing/success",
		CancelURL:     "https://app.laststate.io/billing/cancel",
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal nowpayments invoice: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/invoice", strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("create nowpayments invoice request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("nowpayments invoice request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read nowpayments response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("nowpayments returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var result struct {
		ID         string `json:"id"`
		InvoiceURL string `json:"invoice_url"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse nowpayments response: %w", err)
	}
	if result.InvoiceURL != "" {
		return result.InvoiceURL, nil
	}
	if result.ID != "" {
		return result.ID, nil
	}
	return "", fmt.Errorf("nowpayments response did not contain an invoice url or id")
}

// CreateSubscription creates a NOWPayments invoice used as the subscription
// anchor (crypto has no native recurring billing; each cycle creates a new
// invoice — dunning/renewal is driven by the billing service scheduler).
func (p *NowPaymentsProvider) CreateSubscription(ctx context.Context, orgID uuid.UUID, planID string, _ string) (string, error) {
	url, err := p.CreateCheckoutSession(ctx, orgID, planID, "")
	if err != nil {
		return "", err
	}
	// Return the invoice id/url as the subscription anchor.
	return url, nil
}

// CancelSubscription cancels a pending NOWPayments invoice. Paid invoices
// cannot be cancelled remotely; the local subscription record is closed by
// the caller. In test/dev mode (no key) it succeeds silently.
func (p *NowPaymentsProvider) CancelSubscription(_ context.Context, subID string) error {
	if subID == "" {
		return fmt.Errorf("subscription id is required")
	}
	if p.apiKey == "" {
		return fmt.Errorf("nowpayments not configured: api key is required")
	}
	// NOWPayments has no cancel endpoint for invoices; treat as a no-op that
	// documents the limitation. Local state transition is authoritative.
	return nil
}

// UpdateSubscription is a no-op anchor update for NOWPayments (a plan change
// creates a fresh invoice on next cycle). Kept to satisfy PaymentProvider.
func (p *NowPaymentsProvider) UpdateSubscription(_ context.Context, subID string, planID string) error {
	if subID == "" {
		return fmt.Errorf("subscription id is required")
	}
	if planID == "" {
		return fmt.Errorf("plan id is required")
	}
	return nil
}

// GetSubscription queries a NOWPayments invoice/payment status.
// Requires an API key: without one the call fails closed instead of
// returning a fabricated active subscription.
func (p *NowPaymentsProvider) GetSubscription(ctx context.Context, subID string) (*SubscriptionInfo, error) {
	if subID == "" {
		return nil, fmt.Errorf("subscription id is required")
	}
	if p.apiKey == "" {
		return nil, fmt.Errorf("nowpayments not configured: api key is required")
	}
	url := fmt.Sprintf("%s/payment/%s", p.baseURL, subID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create nowpayments get request: %w", err)
	}
	req.Header.Set("x-api-key", p.apiKey)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nowpayments get request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read nowpayments response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("nowpayments get returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var payment struct {
		PaymentID     string `json:"payment_id"`
		PaymentStatus string `json:"payment_status"` // waiting, confirming, confirmed, sending, finished, failed, refunded, expired
		OrderID       string `json:"order_id"`
	}
	if err := json.Unmarshal(respBody, &payment); err != nil {
		return nil, fmt.Errorf("parse nowpayments payment: %w", err)
	}
	status := "past_due"
	switch payment.PaymentStatus {
	case "finished", "confirmed":
		status = "active"
	case "failed", "expired", "refunded":
		status = "canceled"
	case "waiting", "confirming", "sending", "partially_paid":
		status = "past_due"
	}
	id := payment.PaymentID
	if id == "" {
		id = subID
	}
	return &SubscriptionInfo{
		ID:               id,
		Status:           status,
		PlanID:           "enterprise",
		CurrentPeriodEnd: time.Now().Add(30 * 24 * time.Hour),
	}, nil
}

// VerifyWebhook verifies a NOWPayments IPN signature.
// NOWPayments signs the raw body with HMAC-SHA256 using the IPN secret;
// the hex digest is sent in x-nowpayments-signature.
func (p *NowPaymentsProvider) VerifyWebhook(payload []byte, sig []byte, endpointSecret string) (map[string]interface{}, error) {
	if len(sig) == 0 || endpointSecret == "" {
		return nil, fmt.Errorf("nowpayments webhook signature and secret are required")
	}
	// Accept both raw hex and "hmac_sha256=<hex>" forms.
	sigStr := strings.TrimSpace(string(sig))
	sigStr = strings.TrimPrefix(sigStr, "hmac_sha256=")
	sigStr = strings.TrimPrefix(sigStr, "sha256=")
	sigBytes, err := hex.DecodeString(sigStr)
	if err != nil {
		// Fall back to comparing the raw bytes as hex string.
		sigBytes = sig
	}
	mac := hmac.New(sha256.New, []byte(endpointSecret))
	mac.Write(payload)
	if !hmac.Equal(mac.Sum(nil), sigBytes) {
		return nil, fmt.Errorf("invalid nowpayments signature")
	}
	var data map[string]interface{}
	if err := json.Unmarshal(payload, &data); err != nil {
		return nil, fmt.Errorf("parse nowpayments payload: %w", err)
	}
	return data, nil
}

// ─── Mercado Pago Pix ───

// PixPaymentResult is the outcome of a Mercado Pago Pix (instant payment)
// charge: a BR Code (qr_code) plus base64 QR image for the checkout page.
type PixPaymentResult struct {
	PaymentID string `json:"payment_id"`
	Status    string `json:"status"`
	QRCode    string `json:"qr_code"`
	QRBase64  string `json:"qr_base64"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// CreatePixPayment creates an instant Pix payment via Mercado Pago's
// /v1/payments API (payment_method_id=pix) and returns the BR Code.
// Requires a payer email; amount is derived from the plan price.
func (p *MercadoPagoProvider) CreatePixPayment(ctx context.Context, orgID uuid.UUID, planID string, payerEmail string) (*PixPaymentResult, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("mercado pago pix is not configured (set MP_KEY)")
	}
	if payerEmail == "" {
		return nil, fmt.Errorf("payer email is required for pix payments")
	}
	amount := float64(planPriceCents(planID)) / 100.0
	reqBody := map[string]any{
		"transaction_amount": amount,
		"payment_method_id":  "pix",
		"description":        "LastState " + planID,
		"payer": map[string]any{
			"email": payerEmail,
		},
		"metadata": map[string]string{
			"organization_id": orgID.String(),
			"plan_id":         planID,
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal mp pix request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/payments", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("create mp pix request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("X-Idempotency-Key", orgID.String()+":"+planID+":"+time.Now().UTC().Format("20060102"))

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mp pix request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read mp pix response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("mercado pago pix returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var result struct {
		ID                 int64  `json:"id"`
		Status             string `json:"status"`
		DateOfExpiration   string `json:"date_of_expiration"`
		PointOfInteraction struct {
			TransactionData struct {
				QRCode       string `json:"qr_code"`
				QRCodeBase64 string `json:"qr_code_base64"`
			} `json:"transaction_data"`
		} `json:"point_of_interaction"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse mp pix response: %w", err)
	}
	if result.PointOfInteraction.TransactionData.QRCode == "" && result.ID == 0 {
		return nil, fmt.Errorf("mercado pago pix response did not contain a qr code or payment id")
	}
	return &PixPaymentResult{
		PaymentID: fmt.Sprintf("%d", result.ID),
		Status:    result.Status,
		QRCode:    result.PointOfInteraction.TransactionData.QRCode,
		QRBase64:  result.PointOfInteraction.TransactionData.QRCodeBase64,
		ExpiresAt: result.DateOfExpiration,
	}, nil
}

// BoletoPaymentResult is a boleto bancário outcome: a printable line and a
// bank URL, expiring in 3 business days by default.
type BoletoPaymentResult struct {
	PaymentID string `json:"payment_id"`
	Status    string `json:"status"`
	Barcode   string `json:"barcode,omitempty"`
	BankURL   string `json:"bank_url,omitempty"`
	DueDate   string `json:"due_date,omitempty"`
}

// CreateBoletoPayment creates a boleto bancário via Mercado Pago's /v1/payments
// API (payment_method_id=bolbradesco). The payer's fiscal document comes from
// the org TaxID: 11 digits = CPF, 14 = CNPJ. Without it MP rejects the payment.
func (p *MercadoPagoProvider) CreateBoletoPayment(ctx context.Context, orgID uuid.UUID, planID string, payerEmail, taxID string) (*BoletoPaymentResult, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("mercado pago boleto is not configured (set MP_KEY)")
	}
	if payerEmail == "" {
		return nil, fmt.Errorf("payer email is required for boleto payments")
	}
	doc := digitsOnlyMP(taxID)
	docType := ""
	switch len(doc) {
	case 11:
		docType = "CPF"
	case 14:
		docType = "CNPJ"
	default:
		return nil, fmt.Errorf("boleto requires an 11-digit CPF or 14-digit CNPJ on the organization")
	}
	amount := float64(planPriceCents(planID)) / 100.0
	reqBody := map[string]any{
		"transaction_amount": amount,
		"payment_method_id":  "bolbradesco",
		"description":        "LastState " + planID,
		"payer": map[string]any{
			"email": payerEmail,
			"identification": map[string]any{
				"type":   docType,
				"number": doc,
			},
		},
		"metadata": map[string]string{
			"organization_id": orgID.String(),
			"plan_id":         planID,
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal mp boleto request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/payments", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("create mp boleto request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("X-Idempotency-Key", orgID.String()+":"+planID+":"+time.Now().UTC().Format("20060102"))

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mp boleto request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read mp boleto response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("mercado pago boleto returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var result struct {
		ID                 int64  `json:"id"`
		Status             string `json:"status"`
		DateOfExpiration   string `json:"date_of_expiration"`
		TransactionDetails struct {
			ExternalResourceURL string `json:"external_resource_url"`
			Barcode             struct {
				Content string `json:"content"`
			} `json:"barcode"`
		} `json:"transaction_details"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse mp boleto response: %w", err)
	}
	if result.ID == 0 {
		return nil, fmt.Errorf("mercado pago boleto response did not contain a payment id")
	}
	return &BoletoPaymentResult{
		PaymentID: fmt.Sprintf("%d", result.ID),
		Status:    result.Status,
		Barcode:   result.TransactionDetails.Barcode.Content,
		BankURL:   result.TransactionDetails.ExternalResourceURL,
		DueDate:   result.DateOfExpiration,
	}, nil
}

// digitsOnlyMP extracts numeric characters (fiscal document normalization).
func digitsOnlyMP(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
