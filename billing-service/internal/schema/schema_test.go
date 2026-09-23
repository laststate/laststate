package schema

import (
	"strings"
	"testing"
)

func TestMigrationSQLTables(t *testing.T) {
	tables := []string{
		"organizations",
		"subscriptions",
		"invoices",
		"webhook_events",
		"payment_attempts",
		"usage_metrics",
		"tier_quotas",
		"plans",
		"api_tokens",
		"coupons",
		"coupon_redemptions",
	}
	for _, tbl := range tables {
		if !strings.Contains(MigrationSQL, "CREATE TABLE IF NOT EXISTS "+tbl) {
			t.Errorf("MigrationSQL missing CREATE TABLE for %q", tbl)
		}
	}
}

func TestMigrationSQLIndexes(t *testing.T) {
	indexes := []string{
		"idx_subscriptions_org",
		"idx_invoices_org",
		"idx_webhook_events_event_id",
		"idx_usage_metrics_org_period",
		"idx_payment_attempts_org",
		"idx_api_tokens_org",
		"idx_api_tokens_hash",
		"idx_coupons_code",
		"idx_coupon_redemptions_org",
	}
	for _, idx := range indexes {
		if !strings.Contains(MigrationSQL, idx) {
			t.Errorf("MigrationSQL missing index %q", idx)
		}
	}
}

func TestMigrationSQLSeeds(t *testing.T) {
	for _, tier := range []string{"free", "hobbyist", "team", "enterprise"} {
		if !strings.Contains(MigrationSQL, "'"+tier+"'") {
			t.Errorf("MigrationSQL missing seed reference for tier %q", tier)
		}
	}
	for _, plan := range []string{"'free'", "'hobbyist'", "'team'", "'enterprise'"} {
		if !strings.Contains(MigrationSQL, "("+plan+",") {
			t.Errorf("MigrationSQL missing seed plan %q", plan)
		}
	}
}
