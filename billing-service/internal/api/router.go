// Package api provides the HTTP API for the billing service.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/laststate/billing-service/internal/billing"
	"github.com/laststate/billing-service/internal/entitlement"
	"github.com/laststate/billing-service/internal/webhook"
)

// RouterConfig holds router configuration.
type RouterConfig struct {
	// RateLimitRequestsPerMinute is the maximum requests per minute per IP.
	RateLimitRequestsPerMinute int
	// CORSEnabled enables CORS middleware.
	CORSEnabled bool
	// CORSConfig holds CORS configuration (used when CORSEnabled is true).
	CORSConfig CORSConfig
}

// DefaultRouterConfig returns default router configuration.
func DefaultRouterConfig() RouterConfig {
	return RouterConfig{
		RateLimitRequestsPerMinute: 1000,
		CORSEnabled:                true,
		CORSConfig:                 CORSConfigFromEnv(),
	}
}

// Router sets up the HTTP routes with middleware.
func NewRouter(
	billingSvc *billing.Service,
	entitlementSvc *entitlement.Service,
	webhookHandler *webhook.Handler,
	authMiddleware *AuthMiddleware,
	rateLimiter *RateLimiter,
	logger *zap.Logger,
	config RouterConfig,
) *mux.Router {
	r := mux.NewRouter()

	// Apply CORS middleware if enabled.
	if config.CORSEnabled {
		r.Use(CORSMiddleware(config.CORSConfig))
	}

	// Apply rate limiting middleware.
	if config.RateLimitRequestsPerMinute > 0 {
		rateConfig := RateLimiterConfig{
			RequestsPerMinute: config.RateLimitRequestsPerMinute,
		}
		rl := NewRateLimiter(rateConfig, logger)
		r.Use(rl.Middleware)
	}

	// Health check (no auth, no rate limit)
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}).Methods("GET")

	// Webhook endpoints (no auth required)
	r.HandleFunc("/webhooks/stripe", webhookHandler.HandleStripeWebhook).Methods("POST")
	r.HandleFunc("/webhooks/mercado-pago", webhookHandler.HandleMercadoPagoWebhook).Methods("POST")
	r.HandleFunc("/webhooks/crypto", webhookHandler.HandleCryptoWebhook).Methods("POST")
	r.HandleFunc("/webhooks/nowpayments", webhookHandler.HandleCryptoWebhook).Methods("POST")

	// Public API (auth required via API key)
	api := r.PathPrefix("/v1").Subrouter()
	api.Use(authMiddleware.Middleware)

	// Subscriptions
	api.HandleFunc("/subscriptions", createSubscription(billingSvc)).Methods("POST")
	api.HandleFunc("/subscriptions/{id}", getSubscription(billingSvc)).Methods("GET")
	api.HandleFunc("/subscriptions/{id}", cancelSubscription(billingSvc)).Methods("DELETE")
	api.HandleFunc("/subscriptions/{id}/upgrade", upgradePlan(billingSvc)).Methods("POST")

	// One-off payments (Pix via Mercado Pago)
	api.HandleFunc("/payments", createPayment(billingSvc)).Methods("POST")

	// Invoices
	api.HandleFunc("/invoices", listInvoices(billingSvc)).Methods("GET")
	api.HandleFunc("/invoices", createInvoice(billingSvc)).Methods("POST")
	api.HandleFunc("/invoices/{id}", getInvoice(billingSvc)).Methods("GET")
	api.HandleFunc("/invoices/{id}/pdf", invoicePDF(billingSvc)).Methods("GET")
	api.HandleFunc("/invoices/{id}/refund", refundInvoice(billingSvc)).Methods("POST")
	api.HandleFunc("/invoices/{id}/apply-coupon", applyCoupon(billingSvc)).Methods("POST")

	// Usage
	api.HandleFunc("/usage", recordUsage(billingSvc)).Methods("POST")
	api.HandleFunc("/usage", getUsage(billingSvc)).Methods("GET")
	api.HandleFunc("/usage/charges", usageCharges(billingSvc)).Methods("GET")
	api.HandleFunc("/usage/invoices", createUsageInvoice(billingSvc)).Methods("POST")

	// Coupons
	api.HandleFunc("/coupons", createCoupon(billingSvc)).Methods("POST")
	api.HandleFunc("/coupons/validate", validateCoupon(billingSvc)).Methods("POST")
	api.HandleFunc("/coupons/redeem", redeemCoupon(billingSvc)).Methods("POST")

	// Customer portal (Stripe Billing Portal)
	api.HandleFunc("/portal/session", portalSession(billingSvc)).Methods("POST")

	// Hosted checkout (POC one-time + plan subscriptions)
	api.HandleFunc("/billing/checkout", createCheckout(billingSvc)).Methods("POST")

	// Dunning
	api.HandleFunc("/dunning/{org_id}/initiate", initiateDunning(billingSvc)).Methods("POST")

	// Billing API v2 — outbound event webhooks (push, don't poll)
	api.HandleFunc("/webhook-endpoints", createWebhookEndpoint(billingSvc)).Methods("POST")
	api.HandleFunc("/webhook-endpoints", listWebhookEndpoints(billingSvc)).Methods("GET")
	api.HandleFunc("/webhook-endpoints/{id}", deleteWebhookEndpoint(billingSvc)).Methods("DELETE")

	// Entitlements
	api.HandleFunc("/entitlements/{org_id}", getEntitlement(entitlementSvc)).Methods("GET")
	api.HandleFunc("/entitlements/{org_id}/provision", provisionEntitlement(entitlementSvc)).Methods("POST")
	api.HandleFunc("/entitlements/{org_id}/deprovision", deprovisionEntitlement(entitlementSvc)).Methods("POST")

	// Trials
	api.HandleFunc("/trials/{org_id}/start", startTrial(entitlementSvc)).Methods("POST")
	api.HandleFunc("/trials/{org_id}/end", endTrial(entitlementSvc)).Methods("POST")
	api.HandleFunc("/trials/{org_id}/extend", extendTrial(entitlementSvc)).Methods("POST")

	return r
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
