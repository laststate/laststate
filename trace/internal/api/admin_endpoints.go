package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/billing"
	"github.com/laststate/trace/internal/store"
)

// adminBillingSecret returns the configured HMAC secret(s) for the admin API.
//
// Configuration is via env TRACE_BILLING_HMAC_SECRET, with multiple secrets
// separated by comma for rotation. When unset, the endpoint is disabled and
// returns 503 so a misconfigured deployment does not silently accept requests.
func (s *Server) adminBillingSecrets() [][]byte {
	raw := os.Getenv("TRACE_BILLING_HMAC_SECRET")
	if raw == "" {
		return nil
	}
	var out [][]byte
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, []byte(p))
	}
	return out
}

// adminApplyEntitlements is the HMAC-protected endpoint the proprietary
// billing service calls to push subscription changes into Trace.
//
// URL: POST /v1/admin/organizations/{id}/entitlements
// Headers:
//
//	X-Billing-Timestamp: unix epoch seconds
//	X-Billing-Signature: hex(hmac_sha256(secret, ts + "\n" + body))
//
// The signing secret is configured via TRACE_BILLING_HMAC_SECRET.
func (s *Server) adminApplyEntitlements(w http.ResponseWriter, r *http.Request) {
	secrets := s.adminBillingSecrets()
	if len(secrets) == 0 {
		writeErr(w, 503, "admin_disabled", "TRACE_BILLING_HMAC_SECRET not configured", false)
		return
	}
	orgID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_request", "invalid organization id", false)
		return
	}
	tsHeader := r.Header.Get(billing.HeaderTimestamp)
	sigHeader := r.Header.Get(billing.HeaderSignature)
	ts, err := strconv.ParseInt(tsHeader, 10, 64)
	if err != nil {
		writeErr(w, 401, "unauthorized", "invalid timestamp", false)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		writeErr(w, 400, "bad_request", err.Error(), false)
		return
	}
	if err := billing.VerifySignature(secrets, ts, body, sigHeader, nowFunc(), billing.DefaultClockSkew); err != nil {
		writeErr(w, 401, "unauthorized", err.Error(), false)
		return
	}
	var req billing.ApplyEntitlementsRequest
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&req); err != nil {
		writeErr(w, 400, "bad_request", err.Error(), false)
		return
	}
	resp, err := billing.ApplyEntitlements(r.Context(), s.Store, orgID, req)
	if err != nil {
		if err.Error() == "billing: unknown plan tier" {
			writeErr(w, 400, "unknown_plan", err.Error(), false)
			return
		}
		// store.ErrNotFound surfaces from GetOrgSubscription when the org does
		// not exist; map to 404 to make it obvious in the billing service.
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "ErrNotFound") {
			writeErr(w, 404, "not_found", "organization not found", false)
			return
		}
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	writeJSON(w, 200, resp)
}

// adminListEntitlements is a GET convenience for the billing service to read
// the current state. Same auth model.
func (s *Server) adminListEntitlements(w http.ResponseWriter, r *http.Request) {
	secrets := s.adminBillingSecrets()
	if len(secrets) == 0 {
		writeErr(w, 503, "admin_disabled", "TRACE_BILLING_HMAC_SECRET not configured", false)
		return
	}
	ts, _ := strconv.ParseInt(r.Header.Get(billing.HeaderTimestamp), 10, 64)
	body := []byte{}
	if err := billing.VerifySignature(secrets, ts, body, r.Header.Get(billing.HeaderSignature), nowFunc(), billing.DefaultClockSkew); err != nil {
		writeErr(w, 401, "unauthorized", err.Error(), false)
		return
	}
	orgID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "bad_request", "invalid organization id", false)
		return
	}
	sub, err := s.Store.GetOrgSubscription(r.Context(), orgID)
	if err != nil {
		writeErr(w, 404, "not_found", "organization not found", false)
		return
	}
	history, _ := s.Store.ListPlanHistory(r.Context(), orgID, 20)
	if history == nil {
		history = []store.PlanHistoryEntry{}
	}
	writeJSON(w, 200, map[string]any{
		"organization_id":          sub.OrganizationID,
		"plan_tier":                sub.PlanTier,
		"subscription_status":      sub.SubscriptionStatus,
		"grace_period_ends_at":     sub.GracePeriodEndsAt,
		"billing_customer_ref":     sub.BillingCustomerRef,
		"billing_subscription_ref": sub.BillingSubscriptionRef,
		"plan_history":             history,
	})
}
