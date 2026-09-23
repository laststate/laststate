// Package billing centralizes plan pricing and currency configuration.
//
// Prices and currencies were previously hardcoded in the provider layer.
// They are now externalized into a PricingConfig that can be loaded from the
// environment (a JSON file path or an inline JSON document) without breaking
// the built-in defaults. This lets operators adjust plan pricing per market
// without recompiling the service.
//
// Configuration:
//
//	BILLING_PRICING_CONFIG=/etc/billing/pricing.json   # path to a JSON file
//	BILLING_PRICING_JSON={"plans":{"team":{"price_cents":4900,"currency":"USD"}}}
//
// The JSON document has the shape:
//
//	{
//	  "plans": {
//	    "hobbyist":   {"price_cents": 900,   "currency": "USD"},
//	    "team":       {"price_cents": 4900,  "currency": "USD"},
//	    "enterprise": {"price_cents": 19900, "currency": "USD"}
//	  }
//	}
package billing

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

// PlanPricing describes the price (in cents) and currency for a single plan.
type PlanPricing struct {
	PriceCents int    `json:"price_cents"`
	Currency   string `json:"currency"`
}

// PricingConfig maps a plan ID to its pricing.
type PricingConfig struct {
	Plans map[string]PlanPricing `json:"plans"`
}

// DefaultPricingConfig returns the built-in defaults. Prices are in USD and
// match docs/PRICING.md and ListTiers (single source of truth for plan
// amounts). Per-market overrides go through BILLING_PRICING_JSON/CONFIG
// (e.g. BRL via Mercado Pago) without recompiling.
func DefaultPricingConfig() PricingConfig {
	return PricingConfig{
		Plans: map[string]PlanPricing{
			"free":       {PriceCents: 0, Currency: "USD"},
			"hobbyist":   {PriceCents: 900, Currency: "USD"},
			"team":       {PriceCents: 4900, Currency: "USD"},
			"enterprise": {PriceCents: 19900, Currency: "USD"},
		},
	}
}

var (
	pricingMu     sync.RWMutex
	pricingConfig = DefaultPricingConfig()
)

// SetPricingConfig overrides the active pricing configuration. Plans absent from
// the supplied config fall back to the built-in defaults, so partial overrides
// never break existing plans.
func SetPricingConfig(cfg PricingConfig) {
	merged := DefaultPricingConfig()
	for id, p := range cfg.Plans {
		merged.Plans[id] = p
	}
	pricingMu.Lock()
	pricingConfig = merged
	pricingMu.Unlock()
}

// ParsePricingConfig parses a JSON pricing document.
func ParsePricingConfig(data []byte) (PricingConfig, error) {
	var cfg PricingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return PricingConfig{}, fmt.Errorf("parse pricing config: %w", err)
	}
	if cfg.Plans == nil {
		return PricingConfig{}, fmt.Errorf("pricing config has no plans")
	}
	return cfg, nil
}

// LoadPricingConfigFromEnv loads pricing from BILLING_PRICING_JSON (inline JSON)
// or BILLING_PRICING_CONFIG (path to a JSON file) and applies it on top of the
// defaults. It is a no-op (returning nil) when neither variable is set, leaving
// the built-in defaults in place.
func LoadPricingConfigFromEnv() error {
	if inline := strings.TrimSpace(os.Getenv("BILLING_PRICING_JSON")); inline != "" {
		cfg, err := ParsePricingConfig([]byte(inline))
		if err != nil {
			return err
		}
		SetPricingConfig(cfg)
		return nil
	}
	if path := strings.TrimSpace(os.Getenv("BILLING_PRICING_CONFIG")); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read pricing config %q: %w", path, err)
		}
		cfg, err := ParsePricingConfig(data)
		if err != nil {
			return err
		}
		SetPricingConfig(cfg)
	}
	return nil
}

// planPricing returns the configured pricing for a plan, falling back to the
// default (free/USD) for unknown plans.
func planPricing(planID string) PlanPricing {
	pricingMu.RLock()
	defer pricingMu.RUnlock()
	if p, ok := pricingConfig.Plans[planID]; ok {
		return p
	}
	return PlanPricing{PriceCents: 0, Currency: "USD"}
}

// planPriceCents returns the price in cents for a plan.
func planPriceCents(planID string) int {
	return planPricing(planID).PriceCents
}

// planCurrency returns the default currency for a plan.
func planCurrency(planID string) string {
	cur := planPricing(planID).Currency
	if cur == "" {
		return "USD"
	}
	return cur
}
