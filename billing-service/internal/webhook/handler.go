// Package webhook handles incoming webhook events from payment providers.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/laststate/billing-service/internal/billing"
	"github.com/laststate/billing-service/internal/entitlement"
)

// Handler processes incoming webhook events.
type Handler struct {
	billing             *billing.Service
	entitlement         *entitlement.Service
	logger              *zap.Logger
	mpSecret            string // MercadoPago webhook secret
	cryptoSecret        string // Crypto gateway webhook secret
	stripeWebhookSecret string // Stripe webhook endpoint secret
}

// NewHandler creates a new webhook handler.
func NewHandler(billingSvc *billing.Service, entitlementSvc *entitlement.Service, logger *zap.Logger, mpSecret, cryptoSecret, stripeWebhookSecret string) *Handler {
	return &Handler{
		billing:             billingSvc,
		entitlement:         entitlementSvc,
		logger:              logger,
		mpSecret:            mpSecret,
		cryptoSecret:        cryptoSecret,
		stripeWebhookSecret: stripeWebhookSecret,
	}
}

// WebhookRequest represents an incoming webhook request.
type WebhookRequest struct {
	Provider string          `json:"provider"`
	EventID  string          `json:"event_id"`
	Payload  json.RawMessage `json:"payload"`
	Sig      []byte          `json:"sig,omitempty"`
}

// HandleStripeWebhook handles a Stripe webhook.
func (h *Handler) HandleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("failed to read body", zap.Error(err))
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Verify signature
	sig := r.Header.Get("Stripe-Signature")
	if sig == "" {
		h.logger.Error("missing Stripe signature")
		http.Error(w, "missing signature", http.StatusBadRequest)
		return
	}

	if h.stripeWebhookSecret == "" {
		h.logger.Error("stripe webhook secret not configured")
		http.Error(w, "stripe webhook secret not configured", http.StatusInternalServerError)
		return
	}
	payload, err := h.billing.VerifyWebhook(body, []byte(sig), h.stripeWebhookSecret)
	if err != nil {
		h.logger.Error("failed to verify webhook", zap.Error(err))
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	eventID := getString(payload, "id")
	if eventID == "" {
		h.logger.Error("missing event ID")
		http.Error(w, "missing event ID", http.StatusBadRequest)
		return
	}

	// Process with idempotency
	if err := h.billing.ProcessWebhook(r.Context(), "stripe", eventID, body); err != nil {
		h.logger.Error("failed to process webhook", zap.Error(err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ok")
}

// HandleMercadoPagoWebhook handles a Mercado Pago webhook.
func (h *Handler) HandleMercadoPagoWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("failed to read body", zap.Error(err))
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// P4: Verify signature using x-signature header
	if !h.verifyMercadoPagoSignature(r.Header.Get("x-signature"), body) {
		h.logger.Error("invalid mercado pago signature")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		h.logger.Error("failed to parse payload", zap.Error(err))
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	eventID := getString(payload, "id")
	if eventID == "" {
		h.logger.Error("missing event ID")
		http.Error(w, "missing event ID", http.StatusBadRequest)
		return
	}

	if err := h.billing.ProcessWebhook(r.Context(), "mercado_pago", eventID, body); err != nil {
		h.logger.Error("failed to process webhook", zap.Error(err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ok")
}

// verifyMercadoPagoSignature verifies the x-signature header against the body.
// MercadoPago uses HMAC-SHA256 with the webhook secret.
func (h *Handler) verifyMercadoPagoSignature(sigHeader string, body []byte) bool {
	if h.mpSecret == "" {
		h.logger.Error("mercado pago webhook secret not configured")
		return false
	}
	if sigHeader == "" {
		return false
	}

	// Parse signature: hmac_sha256=<hash>
	parts := strings.SplitN(sigHeader, "=", 2)
	if len(parts) != 2 || parts[0] != "hmac_sha256" {
		return false
	}

	sigBytes, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}

	// Compute expected HMAC
	hmac := hmac.New(sha256.New, []byte(h.mpSecret))
	hmac.Write(body)
	expectedMAC := hmac.Sum(nil)

	return subtle.ConstantTimeCompare(sigBytes, expectedMAC) == 1
}

// HandleCryptoWebhook handles a crypto gateway webhook.
func (h *Handler) HandleCryptoWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("failed to read body", zap.Error(err))
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// P4: Verify signature using x-cb-signature or x-nowpayments-signature header
	sigHeader := r.Header.Get("x-cb-signature")
	if sigHeader == "" {
		sigHeader = r.Header.Get("x-nowpayments-signature")
	}
	if !h.verifyCryptoSignature(sigHeader, body) {
		h.logger.Error("invalid crypto webhook signature")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		h.logger.Error("failed to parse payload", zap.Error(err))
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	eventID := getString(payload, "id")
	if eventID == "" {
		h.logger.Error("missing event ID")
		http.Error(w, "missing event ID", http.StatusBadRequest)
		return
	}

	if err := h.billing.ProcessWebhook(r.Context(), "crypto", eventID, body); err != nil {
		h.logger.Error("failed to process webhook", zap.Error(err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ok")
}

// verifyCryptoSignature verifies the webhook signature for crypto gateways.
// Coinbase Commerce and NOWPayments use different signature formats.
func (h *Handler) verifyCryptoSignature(sigHeader string, body []byte) bool {
	if h.cryptoSecret == "" {
		h.logger.Error("crypto webhook secret not configured")
		return false
	}
	if sigHeader == "" {
		return false
	}

	// Coinbase Commerce: x-cb-signature = sha256(hex(signature))
	// NOWPayments: x-nowpayments-signature = hex(hmac_sha256(body, secret))
	if strings.HasPrefix(sigHeader, "sha256=") {
		// Coinbase Commerce format
		sigHex := strings.TrimPrefix(sigHeader, "sha256=")
		sigBytes, err := hex.DecodeString(sigHex)
		if err != nil {
			return false
		}
		hmac := hmac.New(sha256.New, []byte(h.cryptoSecret))
		hmac.Write(body)
		expectedMAC := hmac.Sum(nil)
		return subtle.ConstantTimeCompare(sigBytes, expectedMAC) == 1
	}

	// Assume HMAC-SHA256 hex format (NOWPayments and others)
	sigBytes, err := hex.DecodeString(sigHeader)
	if err != nil {
		return false
	}
	hmac := hmac.New(sha256.New, []byte(h.cryptoSecret))
	hmac.Write(body)
	expectedMAC := hmac.Sum(nil)
	return subtle.ConstantTimeCompare(sigBytes, expectedMAC) == 1
}

// Helper to get string from map
func getString(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// Helper to get UUID from map
func getUUID(m map[string]interface{}, key string) (uuid.UUID, error) {
	s := getString(m, key)
	if s == "" {
		return uuid.Nil, fmt.Errorf("missing key: %s", key)
	}
	return uuid.Parse(s)
}
