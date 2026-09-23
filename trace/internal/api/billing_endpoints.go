package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
)

// tierItem is the JSON shape of a subscription tier as consumed by the billing
// view. Field names are camelCase to match the frontend Plan interface.
type tierItem struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	PriceCents           int      `json:"priceCents"`
	Currency             string   `json:"currency"`
	MaxDevices           int      `json:"maxDevices"`
	MaxEventsPerDay      int64    `json:"maxEventsPerDay"`
	RetentionDays        int      `json:"retentionDays"`
	MaxAPITokens         int      `json:"maxApiTokens"`
	MaxAlertRules        int      `json:"maxAlertRules"`
	Features             []string `json:"features"`
	IsUnlimitedDevices   bool     `json:"isUnlimitedDevices,omitempty"`
	IsUnlimitedEvents    bool     `json:"isUnlimitedEvents,omitempty"`
	IsUnlimitedRetention bool     `json:"isUnlimitedRetention,omitempty"`
}

// enterpriseTiers is the managed-SaaS plan catalog. In enterprise mode the
// billing service pushes real entitlements via the admin API; these tiers are
// the display catalog for the billing view.
func enterpriseTiers() []tierItem {
	return []tierItem{
		{
			ID: "free", Name: "Free", PriceCents: 0, Currency: "usd",
			MaxDevices: 5, MaxEventsPerDay: 1000, RetentionDays: 30,
			MaxAPITokens: 1, MaxAlertRules: 0,
			Features: []string{"symbolication"},
		},
		{
			ID: "hobbyist", Name: "Hobbyist", PriceCents: 900, Currency: "usd",
			MaxDevices: 100, MaxEventsPerDay: 50000, RetentionDays: 90,
			MaxAPITokens: 5, MaxAlertRules: 5,
			Features: []string{"symbolication", "analytics_export", "custom_alerts"},
		},
		{
			ID: "team", Name: "Team", PriceCents: 4900, Currency: "usd",
			MaxDevices: 1000, MaxEventsPerDay: 500000, RetentionDays: 365,
			MaxAPITokens: 20, MaxAlertRules: 50,
			Features: []string{"symbolication", "analytics_export", "custom_alerts", "custom_integrations", "oncall", "escalation", "audit_logs", "sso"},
		},
		{
			ID: "enterprise", Name: "Enterprise", PriceCents: 19900, Currency: "usd",
			MaxDevices: -1, MaxEventsPerDay: -1, RetentionDays: -1,
			MaxAPITokens: -1, MaxAlertRules: -1,
			IsUnlimitedDevices: true, IsUnlimitedEvents: true, IsUnlimitedRetention: true,
			Features: []string{"symbolication", "analytics_export", "custom_alerts", "custom_integrations", "oncall", "escalation", "audit_logs", "sso", "priority_support", "sla", "on_prem"},
		},
	}
}

// localTier is the single "everything unlocked" tier shown in local deployment
// mode: unlimited everything, free, no paywall.
func localTier() []tierItem {
	return []tierItem{
		{
			ID: "local", Name: "Everything unlocked", PriceCents: 0, Currency: "usd",
			MaxDevices: -1, MaxEventsPerDay: -1, RetentionDays: -1,
			MaxAPITokens: -1, MaxAlertRules: -1,
			IsUnlimitedDevices: true, IsUnlimitedEvents: true, IsUnlimitedRetention: true,
			Features: []string{"all_features", "unlimited_devices", "unlimited_events", "no_paywall"},
		},
	}
}

// apiBillingTiers returns the list of available subscription tiers.
// The response includes both "items" (frontend contract) and "tiers"
// (backward-compatible alias), plus deployment metadata so the UI can render
// the right state (paywall vs everything unlocked).
func (s *Server) apiBillingTiers(w http.ResponseWriter, r *http.Request) {
	var tiers []tierItem
	if s.Cfg.IsLocal() {
		tiers = localTier()
	} else {
		tiers = enterpriseTiers()
	}

	writeJSON(w, 200, map[string]any{
		"items":           tiers,
		"tiers":           tiers,
		"currency":        "usd",
		"interval":        "month",
		"deployment":      s.Cfg.Deployment,
		"billing_enabled": !s.Cfg.IsLocal(),
	})
}

// apiBillingSubscription returns the current subscription for the org.
func (s *Server) apiBillingSubscription(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if s.Cfg.IsLocal() {
			// Local deployment: no subscription is sold — the org is on an
			// implicit "local" plan with everything unlocked.
			writeJSON(w, 200, map[string]any{
				"id":                 "local",
				"planId":             "local",
				"plan_id":            "local",
				"status":             "active",
				"provider":           "self_hosted",
				"currentPeriodEnd":   "",
				"current_period_end": "",
				"deployment":         "local",
				"billing_enabled":    false,
			})
			return
		}
		writeErr(w, http.StatusNotImplemented, "billing_unavailable", "subscription data is served by the billing service", false)

	case http.MethodDelete:
		if s.Cfg.IsLocal() {
			writeErr(w, 400, "billing_disabled", "subscriptions are not used in local deployment mode", false)
			return
		}
		writeErr(w, http.StatusNotImplemented, "billing_unavailable", "subscription cancellation is served by the billing service", false)

	default:
		writeErr(w, 405, "method", "GET|DELETE", false)
	}
}

// apiBillingCheckout creates a checkout session for a plan upgrade.
func (s *Server) apiBillingCheckout(w http.ResponseWriter, r *http.Request) {
	if s.Cfg.IsLocal() {
		writeErr(w, 400, "billing_disabled", "billing is not available in local deployment mode — everything is unlocked", false)
		return
	}
	var body struct {
		PlanID   string `json:"plan_id"`
		Provider string `json:"provider"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.PlanID == "" {
		writeErr(w, 400, "bad_json", "plan_id required", false)
		return
	}

	provider := body.Provider
	if provider == "" {
		provider = "stripe"
	}

	writeErr(w, http.StatusNotImplemented, "billing_unavailable", "checkout is served by the billing service", false)
}

// apiBillingPlans returns the static plan definitions for the pricing page.
func (s *Server) apiBillingPlans(w http.ResponseWriter, r *http.Request) {
	type plan struct {
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		Price     string   `json:"price"`
		Features  []string `json:"features"`
		Highlight bool     `json:"highlight"`
	}

	plans := []plan{}
	if s.Cfg.IsLocal() {
		plans = []plan{
			{
				ID: "local", Name: "Everything unlocked", Price: "Free",
				Features:  []string{"unlimited devices", "unlimited events", "unlimited retention", "no paywall"},
				Highlight: true,
			},
		}
	} else {
		plans = []plan{
			{
				ID: "free", Name: "Free", Price: "$0",
				Features: []string{"5 devices", "1k events/day", "30-day retention", "symbolication"},
			},
			{
				ID: "hobbyist", Name: "Hobbyist", Price: "$9",
				Features:  []string{"100 devices", "50k events/day", "90-day retention", "custom alerts", "analytics export", "5 API tokens"},
				Highlight: true,
			},
			{
				ID: "team", Name: "Team", Price: "$49",
				Features: []string{"1k devices", "500k events/day", "1-year retention", "SSO/SAML", "on-call", "audit logs", "20 API tokens"},
			},
			{
				ID: "enterprise", Name: "Enterprise", Price: "$199",
				Features: []string{"unlimited everything", "on-premise", "SLA", "priority support", "custom integrations"},
			},
		}
	}

	writeJSON(w, 200, map[string]any{"plans": plans, "interval": "month", "deployment": s.Cfg.Deployment, "billing_enabled": !s.Cfg.IsLocal()})
}

// apiUsageMetrics returns current usage as a flat metrics list in the shape the
// billing view expects: {metrics:[{metric_name, value}]}.
func (s *Server) apiUsageMetrics(w http.ResponseWriter, r *http.Request) {
	tenant, ok := orgFrom(r)
	if !ok || tenant.OrgID == uuid.Nil {
		writeJSON(w, 200, map[string]any{"metrics": []map[string]any{}})
		return
	}
	summary, err := s.Store.UsageSummary(r.Context(), tenant.OrgID)
	if err != nil {
		writeErr(w, 500, "internal", err.Error(), true)
		return
	}
	counters := map[string]int64{}
	for _, c := range summary.Counters {
		counters[c.Kind] = c.Counter
	}
	devices, _ := s.Store.CountOrgDevices(r.Context(), tenant.OrgID)
	metrics := []map[string]any{
		{"metric_name": "events", "value": counters["events"]},
		{"metric_name": "devices", "value": devices},
		{"metric_name": "api_calls", "value": counters["api_calls"]},
		{"metric_name": "releases", "value": counters["releases"]},
		{"metric_name": "issues", "value": counters["issues"]},
	}
	writeJSON(w, 200, map[string]any{"metrics": metrics})
}

// billingOrgQuota returns the quota info for the current org's tier.
func (s *Server) billingOrgQuota(orgID uuid.UUID) (map[string]any, error) {
	// In production, query the tier_quotas table joined with the org's tier
	return map[string]any{
		"tier":               "free",
		"max_devices":        5,
		"max_events_per_day": 1000,
		"retention_days":     30,
		"max_api_tokens":     1,
		"max_alert_rules":    0,
		"symbolication":      true,
		"analytics":          false,
	}, nil
}
