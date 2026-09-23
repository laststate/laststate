package billing

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
)

// Checkout plans sold through hosted checkout. poc is a one-time payment
// (Stripe payment mode); pilot/fleet/enterprise are subscriptions.
const (
	PlanPOC        = "poc"
	PlanPilot      = "pilot"
	PlanFleet      = "fleet"
	PlanEnterprise = "enterprise"
)

// priceEnvForPlan maps a checkout plan to the env var holding its Stripe
// Price ID. No keys or IDs are hardcoded: without the env var set, checkout
// fails closed naming the exact variable to configure.
func priceEnvForPlan(plan string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(plan)) {
	case PlanPOC:
		return "BILLING_PRICE_POC", true
	case PlanPilot:
		return "BILLING_PRICE_PILOT", true
	case PlanFleet:
		return "BILLING_PRICE_FLEET", true
	case PlanEnterprise:
		return "BILLING_PRICE_ENTERPRISE", true
	default:
		return "", false
	}
}

// checkoutRedirectURLs returns the success/cancel URLs for hosted checkout,
// overridable via env for staging vs production.
func checkoutRedirectURLs() (success, cancel string) {
	success = os.Getenv("BILLING_CHECKOUT_SUCCESS_URL")
	if success == "" {
		success = "https://app.laststate.io/billing/success"
	}
	cancel = os.Getenv("BILLING_CHECKOUT_CANCEL_URL")
	if cancel == "" {
		cancel = "https://app.laststate.io/billing/cancel"
	}
	return success, cancel
}

// CreateCheckout creates a hosted checkout URL for plan via provider.
// It fails closed when the provider has no key or the plan has no price
// configured, so a misconfigured deploy can never take money by accident.
func (s *Service) CreateCheckout(ctx context.Context, orgID uuid.UUID, provider, plan, currency string) (string, error) {
	if orgID == uuid.Nil {
		return "", fmt.Errorf("organization_id is required")
	}
	plan = strings.ToLower(strings.TrimSpace(plan))
	priceEnv, ok := priceEnvForPlan(plan)
	if !ok {
		return "", fmt.Errorf("unknown plan %q: want one of poc, pilot, fleet, enterprise", plan)
	}
	if provider == "" {
		provider = "stripe"
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	p, providerErr := s.providerFor(provider)
	if providerErr != nil {
		return "", providerErr
	}
	if currency == "" {
		currency = "usd"
	}
	if plan == PlanPOC {
		sp, isStripe := p.(*StripeProvider)
		if !isStripe {
			return "", fmt.Errorf("poc one-time checkout is only supported via stripe")
		}
		priceID := os.Getenv(priceEnv)
		if priceID == "" {
			return "", fmt.Errorf("plan %q has no price configured: set %s", plan, priceEnv)
		}
		successURL, cancelURL := checkoutRedirectURLs()
		return sp.CreatePaymentCheckoutSession(ctx, orgID, priceID, successURL, cancelURL)
	}
	if provider == "stripe" {
		priceID := os.Getenv(priceEnv)
		if priceID == "" {
			return "", fmt.Errorf("plan %q has no price configured: set %s", plan, priceEnv)
		}
		return p.CreateCheckoutSession(ctx, orgID, priceID, currency)
	}
	return p.CreateCheckoutSession(ctx, orgID, plan, currency)
}
