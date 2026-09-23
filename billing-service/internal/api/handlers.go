package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"github.com/laststate/billing-service/internal/billing"
	"github.com/laststate/billing-service/internal/entitlement"
	"github.com/laststate/billing-service/internal/store"
)

// createSubscription handles POST /v1/subscriptions
func createSubscription(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OrganizationID string `json:"organization_id"`
			Provider       string `json:"provider"`
			PlanID         string `json:"plan_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
			return
		}

		orgID, err := uuid.Parse(req.OrganizationID)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_organization_id", "invalid organization_id")
			return
		}

		subID, err := svc.CreateSubscription(r.Context(), orgID, req.Provider, req.PlanID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, map[string]string{"subscription_id": subID})
	}
}

// createCheckout handles POST /v1/billing/checkout.
// Body: {organization_id, plan (poc|pilot|fleet|enterprise),
// provider (default stripe), currency?}.
// Returns {checkout_url} for the customer to complete payment.
func createCheckout(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OrganizationID string `json:"organization_id"`
			Plan           string `json:"plan"`
			Provider       string `json:"provider"`
			Currency       string `json:"currency"`
		}
		if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
			return
		}
		orgID, orgErr := uuid.Parse(req.OrganizationID)
		if orgErr != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_organization_id", "invalid organization_id")
			return
		}
		url, checkoutErr := svc.CreateCheckout(r.Context(), orgID, req.Provider, req.Plan, req.Currency)
		if checkoutErr != nil {
			writeJSONError(w, http.StatusBadGateway, "checkout_failed", checkoutErr.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"checkout_url": url})
	}
}

// createPayment handles POST /v1/payments (one-off; Pix or boleto via Mercado Pago).
func createPayment(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OrganizationID string `json:"organization_id"`
			PlanID         string `json:"plan_id"`
			Method         string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
			return
		}
		orgID, err := uuid.Parse(req.OrganizationID)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_organization_id", "invalid organization_id")
			return
		}
		switch req.Method {
		case "", "pix":
			res, err := svc.CreatePixPayment(r.Context(), orgID, req.PlanID)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "payment_failed", err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, res)
		case "boleto", "bolbradesco":
			res, err := svc.CreateBoletoPayment(r.Context(), orgID, req.PlanID)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "payment_failed", err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, res)
		default:
			writeJSONError(w, http.StatusBadRequest, "unsupported_method", "only pix and boleto are supported")
		}
	}
}

// createInvoice handles POST /v1/invoices (manual invoice, optional coupon).
func createInvoice(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OrganizationID string `json:"organization_id"`
			SubscriptionID string `json:"subscription_id"`
			AmountCents    int    `json:"amount_cents"`
			Currency       string `json:"currency"`
			Provider       string `json:"provider"`
			CouponCode     string `json:"coupon_code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
			return
		}
		orgID, err := uuid.Parse(req.OrganizationID)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_organization_id", "invalid organization_id")
			return
		}
		invoice, err := svc.CreateInvoice(r.Context(), orgID, req.SubscriptionID, req.AmountCents, req.Currency, req.Provider, req.CouponCode)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invoice_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, invoice)
	}
}

// invoicePDF handles GET /v1/invoices/{id}/pdf
func invoicePDF(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(mux.Vars(r)["id"])
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_invoice_id", "invalid invoice_id")
			return
		}
		pdf, err := svc.InvoicePDF(r.Context(), id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "invoice not found")
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "failed to render invoice pdf")
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", "attachment; filename=\"invoice-"+id.String()+".pdf\"")
		w.WriteHeader(http.StatusOK)
		//nolint:gosec // G705 false positive: pdf bytes are server-generated,
		// served as attachment with explicit Content-Type/Disposition.
		_, _ = w.Write(pdf)
	}
}

// refundInvoice handles POST /v1/invoices/{id}/refund (optional amount_cents for partial).
func refundInvoice(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(mux.Vars(r)["id"])
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_invoice_id", "invalid invoice_id")
			return
		}
		var req struct {
			AmountCents *int64 `json:"amount_cents"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		invoice, err := svc.RefundInvoice(r.Context(), id, req.AmountCents)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "refund_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, invoice)
	}
}

// applyCoupon handles POST /v1/invoices/{id}/apply-coupon
func applyCoupon(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(mux.Vars(r)["id"])
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_invoice_id", "invalid invoice_id")
			return
		}
		var req struct {
			Code string `json:"code"`
		}
		if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil || req.Code == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "coupon code is required")
			return
		}
		invoice, err := svc.GetInvoice(r.Context(), id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "invoice not found")
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "failed to get invoice")
			return
		}
		discount, err := svc.RedeemCouponCode(r.Context(), invoice.OrganizationID, req.Code, int64(invoice.AmountCents), invoice.Currency, &invoice.ID)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_coupon", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"discount_cents": discount, "invoice_id": invoice.ID.String()})
	}
}

// createCoupon handles POST /v1/coupons (admin: register a coupon).
func createCoupon(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var c store.Coupon
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
			return
		}
		if err := svc.CreateCoupon(r.Context(), &c); err != nil {
			writeJSONError(w, http.StatusBadRequest, "coupon_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, c)
	}
}

// redeemCoupon handles POST /v1/coupons/redeem (validate + record redemption).
func redeemCoupon(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Code           string `json:"code"`
			OrganizationID string `json:"organization_id"`
			AmountCents    int64  `json:"amount_cents"`
			Currency       string `json:"currency"`
			InvoiceID      string `json:"invoice_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
			return
		}
		orgID, err := uuid.Parse(req.OrganizationID)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_organization_id", "invalid organization_id")
			return
		}
		var invoiceID *uuid.UUID
		if req.InvoiceID != "" {
			parsed, parseErr := uuid.Parse(req.InvoiceID)
			if parseErr != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid_invoice_id", "invalid invoice_id")
				return
			}
			invoiceID = &parsed
		}
		discount, err := svc.RedeemCouponCode(r.Context(), orgID, req.Code, req.AmountCents, req.Currency, invoiceID)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_coupon", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"discount_cents": discount})
	}
}

// portalSession handles POST /v1/portal/session (Stripe Billing Portal).
func portalSession(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OrganizationID string `json:"organization_id"`
			ReturnURL      string `json:"return_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
			return
		}
		orgID, err := uuid.Parse(req.OrganizationID)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_organization_id", "invalid organization_id")
			return
		}
		url, err := svc.PortalSession(r.Context(), orgID, req.ReturnURL)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "portal_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"url": url})
	}
}

// getSubscription handles GET /v1/subscriptions/{id}
func getSubscription(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		subID := vars["id"]

		sub, err := svc.GetSubscription(r.Context(), subID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		json.NewEncoder(w).Encode(sub)
	}
}

// cancelSubscription handles DELETE /v1/subscriptions/{id}
func cancelSubscription(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		subID := vars["id"]

		if err := svc.CancelSubscription(r.Context(), subID); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// upgradePlan handles POST /v1/subscriptions/{id}/upgrade
func upgradePlan(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		subID := vars["id"]

		var req struct {
			PlanID string `json:"plan_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
			return
		}

		if err := svc.UpgradePlan(r.Context(), subID, req.PlanID); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "upgraded"})
	}
}

// listInvoices handles GET /v1/invoices
func listInvoices(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Parse pagination params
		var (
			orgID uuid.UUID
			err   error
		)
		if oid := r.URL.Query().Get("organization_id"); oid != "" {
			orgID, err = uuid.Parse(oid)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid_organization_id", "invalid organization_id")
				return
			}
		}

		page := 1
		if p := r.URL.Query().Get("page"); p != "" {
			if parsed, perr := strconv.Atoi(p); perr == nil && parsed > 0 {
				page = parsed
			}
		}
		perPage := 20
		if pp := r.URL.Query().Get("per_page"); pp != "" {
			if parsed, perr := strconv.Atoi(pp); perr == nil && parsed > 0 && parsed <= 100 {
				perPage = parsed
			}
		}

		invoices, total, err := svc.ListInvoices(r.Context(), orgID, page, perPage)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "failed to list invoices")
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"invoices": invoices,
			"total":    total,
			"page":     page,
			"per_page": perPage,
		})
	}
}

// getInvoice handles GET /v1/invoices/{id}
func getInvoice(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		invoiceID := vars["id"]

		// Parse UUID
		id, err := uuid.Parse(invoiceID)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_invoice_id", "invalid invoice_id")
			return
		}

		invoice, err := svc.GetInvoice(r.Context(), id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "not_found", "invoice not found")
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "failed to get invoice")
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(invoice)
	}
}

// recordUsage handles POST /v1/usage (idempotent via Idempotency-Key).
func recordUsage(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OrganizationID string           `json:"organization_id"`
			Metrics        map[string]int64 `json:"metrics"`
			PeriodStart    *time.Time       `json:"period_start,omitempty"`
			PeriodEnd      *time.Time       `json:"period_end,omitempty"`
			IdempotencyKey string           `json:"idempotency_key,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
			return
		}

		orgID, err := uuid.Parse(req.OrganizationID)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_organization_id", "invalid organization_id")
			return
		}
		key := req.IdempotencyKey
		if h := r.Header.Get("Idempotency-Key"); h != "" {
			key = h
		}

		// Use provided period boundaries, falling back to the current month.
		now := time.Now()
		periodStart := now
		periodEnd := now.AddDate(0, 1, 0)
		if req.PeriodStart != nil {
			periodStart = *req.PeriodStart
		}
		if req.PeriodEnd != nil {
			periodEnd = *req.PeriodEnd
		}
		allDeduped := len(req.Metrics) > 0
		for name, value := range req.Metrics {
			deduped, err := svc.RecordUsageIdempotent(r.Context(), orgID, name, value, periodStart, periodEnd, key)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
				return
			}
			if !deduped {
				allDeduped = false
			}
		}

		if allDeduped && key != "" {
			writeJSON(w, http.StatusOK, map[string]any{"deduped": true})
			return
		}
		w.WriteHeader(http.StatusCreated)
	}
}

// getUsage handles GET /v1/usage
func getUsage(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgIDStr := r.URL.Query().Get("organization_id")
		if orgIDStr == "" {
			writeJSONError(w, http.StatusBadRequest, "missing_organization_id", "organization_id is required")
			return
		}
		orgID, err := uuid.Parse(orgIDStr)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_organization_id", "invalid organization_id")
			return
		}

		// Get usage for all metrics (or filter by metric_name if provided)
		metricName := r.URL.Query().Get("metric_name")
		metrics, err := svc.ListUsageMetrics(r.Context(), orgID, metricName)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "failed to get usage metrics")
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"metrics": metrics,
			"org_id":  orgID,
		})
	}
}

// initiateDunning handles POST /v1/dunning/{org_id}/initiate
func initiateDunning(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		orgID, err := uuid.Parse(vars["org_id"])
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_org_id", "invalid org_id")
			return
		}

		if err := svc.InitiateDunning(r.Context(), orgID, ""); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		w.WriteHeader(http.StatusCreated)
	}
}

// provisionEntitlement handles POST /v1/entitlements/{org_id}/provision
func provisionEntitlement(svc *entitlement.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		orgID, err := uuid.Parse(vars["org_id"])
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_org_id", "invalid org_id")
			return
		}

		var req struct {
			Tier string `json:"tier"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
			return
		}

		if err := svc.Provision(r.Context(), orgID, req.Tier); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "provisioned"})
	}
}

// deprovisionEntitlement handles POST /v1/entitlements/{org_id}/deprovision
func deprovisionEntitlement(svc *entitlement.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		orgID, err := uuid.Parse(vars["org_id"])
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_org_id", "invalid org_id")
			return
		}

		var req struct {
			Tier string `json:"tier"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
			return
		}

		if err := svc.Deprovision(r.Context(), orgID, req.Tier); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "deprovisioned"})
	}
}

// getEntitlement handles GET /v1/entitlements/{org_id} (documented in
// docs/ENTITLEMENTS.md). Proxies the Trace Admin API read.
func getEntitlement(svc *entitlement.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		orgID, err := uuid.Parse(vars["org_id"])
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_org_id", "invalid org_id")
			return
		}
		res, err := svc.Get(r.Context(), orgID)
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, "trace_unavailable", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}

// startTrial handles POST /v1/trials/{org_id}/start
func startTrial(svc *entitlement.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		orgID, err := uuid.Parse(vars["org_id"])
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_org_id", "invalid org_id")
			return
		}

		days := 14
		if d := r.URL.Query().Get("days"); d != "" {
			if parsed, err := strconv.Atoi(d); err == nil {
				days = parsed
			}
		}

		if err := svc.HandleTrialStart(r.Context(), orgID, days); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		w.WriteHeader(http.StatusCreated)
	}
}

// endTrial handles POST /v1/trials/{org_id}/end?tier={plan}
// Converges the org onto an explicit tier (default "free").
func endTrial(svc *entitlement.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		orgID, err := uuid.Parse(vars["org_id"])
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_org_id", "invalid org_id")
			return
		}

		tier := r.URL.Query().Get("tier")
		if tier == "" {
			tier = "free"
		}

		if err := svc.HandleTrialEnd(r.Context(), orgID, tier); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

// extendTrial handles POST /v1/trials/{org_id}/extend?days=N
func extendTrial(svc *entitlement.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		orgID, err := uuid.Parse(vars["org_id"])
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_org_id", "invalid org_id")
			return
		}

		days := 14
		if d := r.URL.Query().Get("days"); d != "" {
			if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 {
				days = parsed
			}
		}

		if err := svc.ExtendTrial(r.Context(), orgID, days); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"status": "extended", "days": days})
	}
}

// validateCoupon handles POST /v1/coupons/validate
func validateCoupon(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Code           string `json:"code"`
			OrganizationID string `json:"organization_id"`
			AmountCents    int64  `json:"amount_cents"`
			Currency       string `json:"currency"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
			return
		}
		if req.OrganizationID == "" {
			writeJSONError(w, http.StatusBadRequest, "missing_organization_id", "organization_id is required")
			return
		}
		orgID, err := uuid.Parse(req.OrganizationID)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_organization_id", "invalid organization_id")
			return
		}
		if req.Currency == "" {
			req.Currency = "usd"
		}

		coupon, discount, err := svc.ValidateCoupon(r.Context(), req.Code, orgID, req.AmountCents, req.Currency)
		if err != nil {
			// Unknown or exhausted coupons are client errors, not failures.
			writeJSONError(w, http.StatusBadRequest, "invalid_coupon", err.Error())
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"valid":    true,
			"coupon":   coupon,
			"discount": discount,
		})
	}
}

// usageCharges handles GET /v1/usage/charges
func usageCharges(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgIDStr := r.URL.Query().Get("organization_id")
		if orgIDStr == "" {
			writeJSONError(w, http.StatusBadRequest, "missing_organization_id", "organization_id is required")
			return
		}
		orgID, err := uuid.Parse(orgIDStr)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_organization_id", "invalid organization_id")
			return
		}
		planID := r.URL.Query().Get("plan_id")
		if planID == "" {
			writeJSONError(w, http.StatusBadRequest, "missing_plan_id", "plan_id is required")
			return
		}
		metric := r.URL.Query().Get("metric_name")
		if metric == "" {
			metric = billing.MetricEvents
		}

		now := time.Now()
		periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		periodEnd := periodStart.AddDate(0, 1, 0)
		if v := r.URL.Query().Get("period_start"); v != "" {
			t, parseErr := time.Parse(time.RFC3339, v)
			if parseErr != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid_period_start", "period_start must be RFC3339")
				return
			}
			periodStart = t
		}
		if v := r.URL.Query().Get("period_end"); v != "" {
			t, parseErr := time.Parse(time.RFC3339, v)
			if parseErr != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid_period_end", "period_end must be RFC3339")
				return
			}
			periodEnd = t
		}

		summary, err := svc.CalculateUsageCharges(r.Context(), orgID, planID, metric, periodStart, periodEnd)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "failed to calculate usage charges")
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(summary)
	}
}

// createUsageInvoice handles POST /v1/usage/invoices (base + overage invoice,
// idempotent per period: replays return the first invoice with 200).
func createUsageInvoice(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OrganizationID string `json:"organization_id"`
			SubscriptionID string `json:"subscription_id"`
			PlanID         string `json:"plan_id"`
			Currency       string `json:"currency"`
			PeriodStart    string `json:"period_start"`
			PeriodEnd      string `json:"period_end"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
			return
		}
		orgID, err := uuid.Parse(req.OrganizationID)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_organization_id", "invalid organization_id")
			return
		}
		periodStart, err := time.Parse(time.RFC3339, req.PeriodStart)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_period_start", "period_start must be RFC3339")
			return
		}
		periodEnd, err := time.Parse(time.RFC3339, req.PeriodEnd)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_period_end", "period_end must be RFC3339")
			return
		}
		invoice, summary, err := svc.GenerateUsageInvoice(r.Context(), orgID, req.SubscriptionID, req.PlanID, req.Currency, periodStart, periodEnd)
		if err != nil {
			if errors.Is(err, store.ErrUsageBillingDuplicate) {
				writeJSON(w, http.StatusOK, map[string]any{"invoice": invoice, "charges": summary, "deduped": true})
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"invoice": invoice, "charges": summary})
	}
}

// createWebhookEndpoint handles POST /v1/webhook-endpoints (Billing API v2).
// Registers a customer HTTPS receiver for signed billing-event pushes.
func createWebhookEndpoint(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			OrganizationID string   `json:"organization_id"`
			URL            string   `json:"url"`
			Secret         string   `json:"secret"`
			Events         []string `json:"events"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
			return
		}
		orgID, err := uuid.Parse(req.OrganizationID)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_organization_id", "invalid organization_id")
			return
		}
		d := svc.Events()
		if d == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "v2_disabled", "outbound webhooks are not enabled")
			return
		}
		ep, err := d.RegisterEndpoint(orgID, req.URL, req.Secret, req.Events)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_endpoint", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"id": ep.ID, "organization_id": ep.OrgID, "url": ep.URL,
			"events": ep.Events, "active": ep.Active,
		})
	}
}

// listWebhookEndpoints handles GET /v1/webhook-endpoints?organization_id=...
func listWebhookEndpoints(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID, err := uuid.Parse(r.URL.Query().Get("organization_id"))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_organization_id", "organization_id is required")
			return
		}
		d := svc.Events()
		if d == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "v2_disabled", "outbound webhooks are not enabled")
			return
		}
		eps := d.ListEndpoints(orgID)
		type public struct {
			ID        string    `json:"id"`
			URL       string    `json:"url"`
			Events    []string  `json:"events"`
			Active    bool      `json:"active"`
			CreatedAt time.Time `json:"created_at"`
		}
		out := make([]public, 0, len(eps))
		for _, e := range eps {
			out = append(out, public{ID: e.ID.String(), URL: e.URL, Events: e.Events, Active: e.Active, CreatedAt: e.CreatedAt})
		}
		writeJSON(w, http.StatusOK, map[string]any{"endpoints": out})
	}
}

// deleteWebhookEndpoint handles DELETE /v1/webhook-endpoints/{id}.
func deleteWebhookEndpoint(svc *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(mux.Vars(r)["id"])
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_id", "invalid endpoint id")
			return
		}
		d := svc.Events()
		if d == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "v2_disabled", "outbound webhooks are not enabled")
			return
		}
		if !d.UnregisterEndpoint(id) {
			writeJSONError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
