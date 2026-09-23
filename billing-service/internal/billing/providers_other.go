// Package billing implements Mercado Pago and Crypto payment providers.
package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// mercadoPagoAPIBase is the default Mercado Pago API host.
const mercadoPagoAPIBase = "https://api.mercadopago.com"

// MercadoPagoProvider implements PaymentProvider for Mercado Pago (Brazil).
type MercadoPagoProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewMercadoPagoProvider creates a new Mercado Pago provider.
func NewMercadoPagoProvider(apiKey string) *MercadoPagoProvider {
	return &MercadoPagoProvider{
		apiKey:  apiKey,
		baseURL: mercadoPagoAPIBase,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// CreateCheckoutSession creates a Mercado Pago Checkout Pro preference and
// returns the hosted checkout URL (init_point) the customer is redirected to.
// It performs a real call to the Preferences API; the base URL is overridable
// for testing via the unexported baseURL field.
func (p *MercadoPagoProvider) CreateCheckoutSession(ctx context.Context, orgID uuid.UUID, planID string, currency string) (string, error) {
	if p.apiKey == "" {
		return "", fmt.Errorf("mercado pago checkout is not configured")
	}
	if currency == "" {
		currency = planCurrency(planID)
	}

	preference := map[string]any{
		"items": []map[string]any{
			{
				"title":       "LastState " + planID,
				"quantity":    1,
				"unit_price":  float64(planPriceCents(planID)) / 100.0,
				"currency_id": currency,
			},
		},
		"back_urls": map[string]string{
			"success": "https://app.laststate.io/billing/success",
			"failure": "https://app.laststate.io/billing/failure",
			"pending": "https://app.laststate.io/billing/pending",
		},
		"metadata": map[string]string{
			"organization_id": orgID.String(),
			"plan_id":         planID,
		},
	}

	body, err := json.Marshal(preference)
	if err != nil {
		return "", fmt.Errorf("marshal mp preference: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/checkout/preferences", strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("create mp preference request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("mp preference request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read mp preference response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("mercado pago returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var result struct {
		ID          string `json:"id"`
		InitPoint   string `json:"init_point"`
		SandboxInit string `json:"sandbox_init_point"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse mp preference response: %w", err)
	}
	if result.InitPoint != "" {
		return result.InitPoint, nil
	}
	if result.SandboxInit != "" {
		return result.SandboxInit, nil
	}
	if result.ID != "" {
		return result.ID, nil
	}
	return "", fmt.Errorf("mercado pago preference response did not contain a checkout URL or id")
}

// CreateSubscription creates a recurring Mercado Pago Preapproval.
// Unlike a one-off checkout preference, a preapproval bills automatically
// every month. Amounts are in currency units (NOT cents): MP rejects
// cent-denominated unit prices by overcharging 100x.
func (p *MercadoPagoProvider) CreateSubscription(ctx context.Context, orgID uuid.UUID, planID string, payerEmail string) (string, error) {
	if p.apiKey == "" {
		return "", fmt.Errorf("mercado pago not configured: api key is required")
	}
	if payerEmail == "" {
		return "", fmt.Errorf("mercado pago preapproval requires the payer email")
	}
	preapproval := map[string]any{
		"payer_email": payerEmail,
		"reason":      "LastState " + planID,
		"auto_recurring": map[string]any{
			"frequency":          1,
			"frequency_type":     "months",
			"transaction_amount": float64(planPriceCents(planID)) / 100.0,
			"currency_id":        planCurrency(planID),
		},
		"back_url": "https://app.laststate.io/billing/success",
		"metadata": map[string]string{
			"organization_id": orgID.String(),
		},
	}

	body, err := json.Marshal(preapproval)
	if err != nil {
		return "", fmt.Errorf("marshal mp preapproval: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/preapproval", strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("create mp preapproval request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("mp preapproval request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read mercado pago response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("mercado pago preapproval returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse mp preapproval response: %w", err)
	}
	if result.ID == "" {
		return "", fmt.Errorf("mercado pago response did not contain a preapproval id")
	}

	return result.ID, nil
}

// CancelSubscription cancels a Mercado Pago subscription.
// Requires an API key: without one the call fails closed instead of
// pretending the remote subscription was cancelled.
func (p *MercadoPagoProvider) CancelSubscription(ctx context.Context, subID string) error {
	if subID == "" {
		return fmt.Errorf("subscription id is required")
	}
	if p.apiKey == "" {
		return fmt.Errorf("mercado pago not configured: api key is required")
	}

	payload := map[string]string{
		"status": "cancelled",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal mp cancel payload: %w", err)
	}

	url := fmt.Sprintf("%s/preapproval/%s", p.baseURL, subID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("create mp cancel request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("mp cancel request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mercado pago cancel returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

// UpdateSubscription updates a Mercado Pago subscription.
// Requires an API key: without one the call fails closed.
func (p *MercadoPagoProvider) UpdateSubscription(ctx context.Context, subID string, planID string) error {
	if subID == "" {
		return fmt.Errorf("subscription id is required")
	}
	if planID == "" {
		return fmt.Errorf("plan id is required")
	}
	if p.apiKey == "" {
		return fmt.Errorf("mercado pago not configured: api key is required")
	}

	payload := map[string]any{
		"reason": "LastState " + planID,
		"auto_recurring": map[string]any{
			"transaction_amount": float64(planPriceCents(planID)) / 100.0,
			"currency_id":        planCurrency(planID),
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal mp update payload: %w", err)
	}

	url := fmt.Sprintf("%s/preapproval/%s", p.baseURL, subID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("create mp update request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("mp update request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mercado pago update returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

// GetSubscription retrieves a Mercado Pago subscription.
// Requires an API key: without one the call fails closed instead of
// returning a fabricated active subscription.
func (p *MercadoPagoProvider) GetSubscription(ctx context.Context, subID string) (*SubscriptionInfo, error) {
	if subID == "" {
		return nil, fmt.Errorf("subscription id is required")
	}
	if p.apiKey == "" {
		return nil, fmt.Errorf("mercado pago not configured: api key is required")
	}

	url := fmt.Sprintf("%s/preapproval/%s", p.baseURL, subID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create mp get request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mp get request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read mp response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("mercado pago get returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var mpSub struct {
		ID        string `json:"id"`
		Status    string `json:"status"` // "authorized", "paused", "cancelled"
		Reason    string `json:"reason"`
		NextDate  string `json:"next_payment_date"`
		AutoRecur struct {
			CurrencyID string `json:"currency_id"`
		} `json:"auto_recurring"`
	}
	if err := json.Unmarshal(respBody, &mpSub); err != nil {
		return nil, fmt.Errorf("parse mp subscription: %w", err)
	}

	status := "active"
	switch mpSub.Status {
	case "authorized":
		status = "active"
	case "cancelled", "cancelled_by_admin":
		status = "canceled"
	case "paused", "pending":
		status = "past_due"
	}

	periodEnd := time.Now().Add(30 * 24 * time.Hour)
	if mpSub.NextDate != "" {
		if t, err := time.Parse(time.RFC3339, mpSub.NextDate); err == nil {
			periodEnd = t
		}
	}

	return &SubscriptionInfo{
		ID:               mpSub.ID,
		Status:           status,
		PlanID:           "team",
		CurrentPeriodEnd: periodEnd,
	}, nil
}

// VerifyWebhook verifies a Mercado Pago webhook signature.
// MercadoPago uses HMAC SHA256 with the x-signature header.
func (p *MercadoPagoProvider) VerifyWebhook(payload []byte, sig []byte, endpointSecret string) (map[string]interface{}, error) {
	// Verify HMAC signature for Mercado Pago webhooks.
	// MercadoPago uses HMAC SHA256 with the x-signature header.
	if len(sig) == 0 || endpointSecret == "" {
		return nil, fmt.Errorf("mercado pago webhook signature and secret are required")
	}
	if len(sig) > 0 && endpointSecret != "" {
		mac := hmac.New(sha256.New, []byte(endpointSecret))
		mac.Write(payload)
		expectedMAC := mac.Sum(nil)
		if !hmac.Equal(expectedMAC, sig) {
			return nil, fmt.Errorf("invalid HMAC signature")
		}
	}

	var data map[string]interface{}
	if err := json.Unmarshal(payload, &data); err != nil {
		return nil, fmt.Errorf("parse mp payload: %w", err)
	}
	return data, nil
}

// coinbaseCommerceAPIBase is the default Coinbase Commerce API host.
const coinbaseCommerceAPIBase = "https://api.commerce.coinbase.com"

// CryptoProvider implements PaymentProvider for crypto gateways.
type CryptoProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewCryptoProvider creates a new crypto gateway provider.
func NewCryptoProvider(apiKey string) *CryptoProvider {
	return &CryptoProvider{
		apiKey:  apiKey,
		baseURL: coinbaseCommerceAPIBase,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// CreateCheckoutSession creates a Coinbase Commerce charge and returns the
// hosted checkout URL (hosted_url) the customer is redirected to. It performs a
// real call to the Charges API; the base URL is overridable for testing via the
// unexported baseURL field.
func (p *CryptoProvider) CreateCheckoutSession(ctx context.Context, orgID uuid.UUID, planID string, currency string) (string, error) {
	if p.apiKey == "" {
		return "", fmt.Errorf("crypto checkout is not configured")
	}
	if currency == "" {
		currency = planCurrency(planID)
	}

	charge := map[string]any{
		"name":         "LastState " + planID,
		"description":  "LastState subscription - " + planID,
		"pricing_type": "fixed_price",
		"local_price": map[string]any{
			"amount":   fmt.Sprintf("%.2f", float64(planPriceCents(planID))/100.0),
			"currency": currency,
		},
		"metadata": map[string]string{
			"organization_id": orgID.String(),
			"plan_id":         planID,
		},
		"redirect_url": "https://app.laststate.io/billing/success",
		"cancel_url":   "https://app.laststate.io/billing/cancel",
	}

	body, err := json.Marshal(charge)
	if err != nil {
		return "", fmt.Errorf("marshal crypto charge: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/charges", strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("create crypto charge request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CC-Api-Key", p.apiKey)
	req.Header.Set("X-CC-Version", "2018-03-22")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("crypto charge request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read crypto charge response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("crypto gateway returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var result struct {
		Data struct {
			ID        string `json:"id"`
			HostedURL string `json:"hosted_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse crypto charge response: %w", err)
	}
	if result.Data.HostedURL != "" {
		return result.Data.HostedURL, nil
	}
	if result.Data.ID != "" {
		return result.Data.ID, nil
	}
	return "", fmt.Errorf("crypto gateway response did not contain a checkout URL or id")
}

// CreateSubscription creates a crypto subscription.
// This makes a real API call to Coinbase Commerce when apiKey is configured.
func (p *CryptoProvider) CreateSubscription(ctx context.Context, orgID uuid.UUID, planID string, _ string) (string, error) {
	// Create crypto charge
	charge := map[string]any{
		"name":        "LastState " + planID,
		"description": "LastState subscription - " + planID,
		"price": map[string]any{
			"amount":   fmt.Sprintf("%.2f", float64(planPriceCents(planID))/100.0),
			"currency": planCurrency(planID),
		},
		"metadata": map[string]string{
			"organization_id": orgID.String(),
		},
	}

	body, err := json.Marshal(charge)
	if err != nil {
		return "", fmt.Errorf("marshal crypto request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/charges", strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("create crypto request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("crypto request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read crypto gateway response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("crypto gateway returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var result struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse crypto response: %w", err)
	}
	if result.Data.ID == "" {
		return "", fmt.Errorf("crypto gateway response did not contain a charge id")
	}

	return result.Data.ID, nil
}

// CancelSubscription cancels a crypto subscription or pending charge.
// Requires an API key: without one the call fails closed.
func (p *CryptoProvider) CancelSubscription(ctx context.Context, subID string) error {
	if subID == "" {
		return fmt.Errorf("subscription id is required")
	}
	if p.apiKey == "" {
		return fmt.Errorf("crypto gateway not configured: api key is required")
	}

	url := fmt.Sprintf("%s/charges/%s/cancel", p.baseURL, subID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("create crypto cancel request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("X-CC-Api-Key", p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("crypto cancel request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("crypto cancel returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

// UpdateSubscription updates a crypto subscription.
// Requires an API key: without one the call fails closed.
func (p *CryptoProvider) UpdateSubscription(ctx context.Context, subID string, planID string) error {
	if subID == "" {
		return fmt.Errorf("subscription id is required")
	}
	if planID == "" {
		return fmt.Errorf("plan id is required")
	}
	if p.apiKey == "" {
		return fmt.Errorf("crypto gateway not configured: api key is required")
	}

	// For Coinbase Commerce / crypto charges, an update updates charge metadata or creates a new billing intent
	url := fmt.Sprintf("%s/charges/%s", p.baseURL, subID)
	payload := map[string]any{
		"name": "LastState " + planID,
		"price": map[string]any{
			"amount":   fmt.Sprintf("%.2f", float64(planPriceCents(planID))/100.0),
			"currency": planCurrency(planID),
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal crypto update payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("create crypto update request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("X-CC-Api-Key", p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("crypto update request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("crypto update returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

// GetSubscription retrieves a crypto subscription.
// Requires an API key: without one the call fails closed instead of
// returning a fabricated active subscription.
func (p *CryptoProvider) GetSubscription(ctx context.Context, subID string) (*SubscriptionInfo, error) {
	if subID == "" {
		return nil, fmt.Errorf("subscription id is required")
	}
	if p.apiKey == "" {
		return nil, fmt.Errorf("crypto gateway not configured: api key is required")
	}

	url := fmt.Sprintf("%s/charges/%s", p.baseURL, subID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create crypto get request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("X-CC-Api-Key", p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("crypto get request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read crypto response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("crypto get returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var chargeResp struct {
		Data struct {
			ID       string `json:"id"`
			Timeline []struct {
				Status string `json:"status"` // "NEW", "PENDING", "COMPLETED", "EXPIRED", "UNRESOLVED", "RESOLVED", "CANCELED"
				Time   string `json:"time"`
			} `json:"timeline"`
			ExpiresAt string `json:"expires_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &chargeResp); err != nil {
		return nil, fmt.Errorf("parse crypto charge response: %w", err)
	}

	status := "active"
	if len(chargeResp.Data.Timeline) > 0 {
		latestStatus := chargeResp.Data.Timeline[len(chargeResp.Data.Timeline)-1].Status
		switch latestStatus {
		case "COMPLETED", "RESOLVED":
			status = "active"
		case "CANCELED":
			status = "canceled"
		case "EXPIRED":
			status = "expired"
		case "PENDING", "NEW", "UNRESOLVED":
			status = "past_due"
		}
	}

	periodEnd := time.Now().Add(30 * 24 * time.Hour)
	if chargeResp.Data.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, chargeResp.Data.ExpiresAt); err == nil {
			periodEnd = t
		}
	}

	return &SubscriptionInfo{
		ID:               chargeResp.Data.ID,
		Status:           status,
		PlanID:           "enterprise",
		CurrentPeriodEnd: periodEnd,
	}, nil
}

// VerifyWebhook verifies a crypto gateway webhook signature.
// Coinbase Commerce uses HMAC SHA256; NOWPayments uses a different scheme.
func (p *CryptoProvider) VerifyWebhook(payload []byte, sig []byte, endpointSecret string) (map[string]interface{}, error) {
	// Verify signature for crypto webhooks.
	// Coinbase Commerce uses HMAC SHA256; NOWPayments uses a different scheme.
	if len(sig) == 0 || endpointSecret == "" {
		return nil, fmt.Errorf("crypto webhook signature and secret are required")
	}
	if len(sig) > 0 && endpointSecret != "" {
		mac := hmac.New(sha256.New, []byte(endpointSecret))
		mac.Write(payload)
		expectedMAC := mac.Sum(nil)
		if !hmac.Equal(expectedMAC, sig) {
			return nil, fmt.Errorf("invalid signature")
		}
	}

	var data map[string]interface{}
	if err := json.Unmarshal(payload, &data); err != nil {
		return nil, fmt.Errorf("parse crypto payload: %w", err)
	}
	return data, nil
}
