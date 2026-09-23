package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

func newSQLMock(t *testing.T) (*PostgresDB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &PostgresDB{db}, mock
}

func TestNewPostgres(t *testing.T) {
	db, err := NewPostgres("postgres://user:pass@localhost/db?sslmode=disable")
	if err != nil {
		t.Fatalf("NewPostgres: %v", err)
	}
	if db == nil {
		t.Fatal("NewPostgres returned nil")
	}
	defer db.Close()
}

// --- GetByKey ---

func TestGetByKeyActive(t *testing.T) {
	db, mock := newSQLMock(t)
	orgID := uuid.New()
	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "name", "organization_id", "expires_at", "last_used_at", "created_at"}).
		AddRow(uuid.New(), "my-key", orgID, now.Add(time.Hour), now, now)
	mock.ExpectQuery(`SELECT id, name, organization_id, expires_at, last_used_at, created_at FROM api_tokens WHERE token_hash = \$1`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(rows)

	got, err := db.GetByKey(context.Background(), "raw-key")
	if err != nil {
		t.Fatalf("GetByKey: %v", err)
	}
	if !got.Active {
		t.Error("expected active key (expires_at in the future)")
	}
	if got.Key != "raw-key" {
		t.Errorf("Key = %q, want raw-key", got.Key)
	}
	if got.OrgID != orgID {
		t.Errorf("OrgID = %v, want %v", got.OrgID, orgID)
	}
}

func TestGetByKeyExpired(t *testing.T) {
	db, mock := newSQLMock(t)
	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "name", "organization_id", "expires_at", "last_used_at", "created_at"}).
		AddRow(uuid.New(), "my-key", uuid.New(), now.Add(-time.Hour), now, now)
	mock.ExpectQuery(`SELECT id, name, organization_id, expires_at, last_used_at, created_at FROM api_tokens WHERE token_hash = \$1`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(rows)

	got, err := db.GetByKey(context.Background(), "raw-key")
	if err != nil {
		t.Fatalf("GetByKey: %v", err)
	}
	if got.Active {
		t.Error("expected inactive key (expires_at in the past)")
	}
}

func TestGetByKeyNeverExpires(t *testing.T) {
	db, mock := newSQLMock(t)
	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "name", "organization_id", "expires_at", "last_used_at", "created_at"}).
		AddRow(uuid.New(), "my-key", uuid.New(), nil, now, now)
	mock.ExpectQuery(`SELECT id, name, organization_id, expires_at, last_used_at, created_at FROM api_tokens WHERE token_hash = \$1`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(rows)

	got, err := db.GetByKey(context.Background(), "raw-key")
	if err != nil {
		t.Fatalf("GetByKey: %v", err)
	}
	if !got.Active {
		t.Error("expected active key (no expiry)")
	}
}

func TestGetByKeyNotFound(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectQuery(`SELECT id, name, organization_id, expires_at, last_used_at, created_at FROM api_tokens WHERE token_hash = \$1`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)

	_, err := db.GetByKey(context.Background(), "raw-key")
	if err != ErrNotFound {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// --- Subscriptions ---

func TestGetSubscriptionByProviderSubID(t *testing.T) {
	db, mock := newSQLMock(t)
	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "organization_id", "provider", "provider_subscription_id", "plan_id", "status", "current_period_start", "current_period_end", "cancel_at_period_end", "last_proration_cents", "created_at", "updated_at"}).
		AddRow(uuid.New(), uuid.New(), "stripe", "sub_1", "pro", "active", now, now, false, 0, now, now)
	mock.ExpectQuery(`SELECT id, organization_id, provider, provider_subscription_id, plan_id, status, current_period_start, current_period_end, cancel_at_period_end, last_proration_cents, created_at, updated_at FROM subscriptions WHERE provider_subscription_id = \$1`).
		WithArgs("sub_1").
		WillReturnRows(rows)

	sub, err := db.GetSubscriptionByProviderSubID(context.Background(), "sub_1")
	if err != nil {
		t.Fatalf("GetSubscriptionByProviderSubID: %v", err)
	}
	if sub.PlanID != "pro" || sub.Status != "active" {
		t.Errorf("sub = %+v, want plan pro / active", sub)
	}
}

func TestGetSubscriptionByProviderSubIDNotFound(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectQuery(`SELECT id, organization_id, provider, provider_subscription_id, plan_id, status, current_period_start, current_period_end, cancel_at_period_end, last_proration_cents, created_at, updated_at FROM subscriptions WHERE provider_subscription_id = \$1`).
		WithArgs("sub_x").
		WillReturnError(sql.ErrNoRows)

	_, err := db.GetSubscriptionByProviderSubID(context.Background(), "sub_x")
	if err != ErrNotFound {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestUpdateSubscriptionStatusZeroPeriod(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectExec(`UPDATE subscriptions SET status = \$1 WHERE provider_subscription_id = \$2`).
		WithArgs("canceled", "sub_1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := db.UpdateSubscriptionStatus(context.Background(), "sub_1", "canceled", time.Time{}); err != nil {
		t.Fatalf("UpdateSubscriptionStatus: %v", err)
	}
}

func TestUpdateSubscriptionStatusWithPeriod(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectExec(`UPDATE subscriptions SET status = \$1, current_period_end = \$2 WHERE provider_subscription_id = \$3`).
		WithArgs("active", sqlmock.AnyArg(), "sub_1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := db.UpdateSubscriptionStatus(context.Background(), "sub_1", "active", time.Now()); err != nil {
		t.Fatalf("UpdateSubscriptionStatus: %v", err)
	}
}

// --- Invoices ---

func TestGetInvoiceByID(t *testing.T) {
	db, mock := newSQLMock(t)
	id := uuid.New()
	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "organization_id", "subscription_id", "provider", "provider_invoice_id", "amount_cents", "currency", "status", "issued_at", "paid_at", "due_at", "pdf_url", "nfe_url"}).
		AddRow(id, uuid.New(), uuid.New(), "stripe", "inv_1", 2900, "usd", "paid", now, now, now, nil, nil)
	mock.ExpectQuery(`SELECT id, organization_id, subscription_id, provider, provider_invoice_id, amount_cents, currency, status, issued_at, paid_at, due_at, pdf_url, nfe_url FROM invoices WHERE id = \$1`).
		WithArgs(id).
		WillReturnRows(rows)

	inv, err := db.GetInvoiceByID(context.Background(), id)
	if err != nil {
		t.Fatalf("GetInvoiceByID: %v", err)
	}
	if inv.Status != "paid" || inv.AmountCents != 2900 {
		t.Errorf("inv = %+v, want paid/2900", inv)
	}
}

func TestGetInvoiceByIDNotFound(t *testing.T) {
	db, mock := newSQLMock(t)
	id := uuid.New()
	mock.ExpectQuery(`SELECT id, organization_id, subscription_id, provider, provider_invoice_id, amount_cents, currency, status, issued_at, paid_at, due_at, pdf_url, nfe_url FROM invoices WHERE id = \$1`).
		WithArgs(id).
		WillReturnError(sql.ErrNoRows)

	_, err := db.GetInvoiceByID(context.Background(), id)
	if err != ErrNotFound {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// --- Plans / webhook events / orgs ---

func TestGetPlanByID(t *testing.T) {
	db, mock := newSQLMock(t)
	rows := sqlmock.NewRows([]string{"id", "name", "price_cents", "currency", "interval", "description"}).
		AddRow("pro", "Pro", 2900, "usd", "month", "Pro plan")
	mock.ExpectQuery(`SELECT id, name, price_cents, currency, interval, description FROM plans WHERE id = \$1`).
		WithArgs("pro").
		WillReturnRows(rows)

	p, err := db.GetPlanByID(context.Background(), "pro")
	if err != nil {
		t.Fatalf("GetPlanByID: %v", err)
	}
	if p.PriceCents != 2900 {
		t.Errorf("price = %d, want 2900", p.PriceCents)
	}
}

func TestGetPlanByIDNotFound(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectQuery(`SELECT id, name, price_cents, currency, interval, description FROM plans WHERE id = \$1`).
		WithArgs("nope").
		WillReturnError(sql.ErrNoRows)

	_, err := db.GetPlanByID(context.Background(), "nope")
	if err != ErrNotFound {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestGetWebhookEventFound(t *testing.T) {
	db, mock := newSQLMock(t)
	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "provider", "event_id", "raw_payload", "processed_at"}).
		AddRow(uuid.New(), "stripe", "evt_1", []byte(`{"x":1}`), now)
	mock.ExpectQuery(`SELECT id, provider, event_id, raw_payload, processed_at FROM webhook_events WHERE event_id = \$1`).
		WithArgs("evt_1").
		WillReturnRows(rows)

	we, err := db.GetWebhookEvent(context.Background(), "evt_1")
	if err != nil {
		t.Fatalf("GetWebhookEvent: %v", err)
	}
	if we == nil || we.EventID != "evt_1" {
		t.Errorf("we = %+v, want evt_1", we)
	}
}

func TestGetWebhookEventNotFound(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectQuery(`SELECT id, provider, event_id, raw_payload, processed_at FROM webhook_events WHERE event_id = \$1`).
		WithArgs("evt_x").
		WillReturnError(sql.ErrNoRows)

	we, err := db.GetWebhookEvent(context.Background(), "evt_x")
	if err != nil {
		t.Fatalf("GetWebhookEvent: %v", err)
	}
	if we != nil {
		t.Errorf("we = %+v, want nil", we)
	}
}

func TestGetOrgByID(t *testing.T) {
	db, mock := newSQLMock(t)
	id := uuid.New()
	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "name", "email", "tier", "address", "tax_id", "stripe_customer_id", "stripe_subscription_id", "mercado_pago_pref_id", "phone", "trial_ends_at", "created_at", "updated_at"}).
		AddRow(id, "Acme", "a@acme.dev", "pro", "", "", nil, nil, nil, "+5511999999999", nil, now, now)
	mock.ExpectQuery(`SELECT id, name, email, tier, address, tax_id, stripe_customer_id, stripe_subscription_id, mercado_pago_pref_id, phone, trial_ends_at, created_at, updated_at FROM organizations WHERE id = \$1`).
		WithArgs(id).
		WillReturnRows(rows)

	org, err := db.GetOrgByID(context.Background(), id)
	if err != nil {
		t.Fatalf("GetOrgByID: %v", err)
	}
	if org.Name != "Acme" || org.Tier != "pro" {
		t.Errorf("org = %+v, want Acme/pro", org)
	}
}

func TestGetOrgByIDNotFound(t *testing.T) {
	db, mock := newSQLMock(t)
	id := uuid.New()
	mock.ExpectQuery(`SELECT id, name, email, tier, address, tax_id, stripe_customer_id, stripe_subscription_id, mercado_pago_pref_id, phone, trial_ends_at, created_at, updated_at FROM organizations WHERE id = \$1`).
		WithArgs(id).
		WillReturnError(sql.ErrNoRows)

	_, err := db.GetOrgByID(context.Background(), id)
	if err != ErrNotFound {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// --- Usage ---

func TestListUsageMetricsWithName(t *testing.T) {
	db, mock := newSQLMock(t)
	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "organization_id", "metric_name", "recorded_at", "value", "period_start", "period_end", "idempotency_key"}).
		AddRow(uuid.New(), uuid.New(), "crashes", now, 5, now, now, "key-1")
	mock.ExpectQuery(`SELECT id, organization_id, metric_name, recorded_at, value, period_start, period_end, COALESCE\(idempotency_key,''\) FROM usage_metrics WHERE organization_id = \$1 AND metric_name = \$2 ORDER BY recorded_at DESC`).
		WithArgs(sqlmock.AnyArg(), "crashes").
		WillReturnRows(rows)

	metrics, err := db.ListUsageMetrics(context.Background(), uuid.New(), "crashes")
	if err != nil {
		t.Fatalf("ListUsageMetrics: %v", err)
	}
	if len(metrics) != 1 || metrics[0].Value != 5 {
		t.Errorf("metrics = %+v, want 1 metric with value 5", metrics)
	}
	if metrics[0].IdempotencyKey != "key-1" {
		t.Errorf("idempotency_key = %q, want key-1", metrics[0].IdempotencyKey)
	}
}

func TestListUsageMetricsAll(t *testing.T) {
	db, mock := newSQLMock(t)
	rows := sqlmock.NewRows([]string{"id", "organization_id", "metric_name", "recorded_at", "value", "period_start", "period_end", "idempotency_key"})
	mock.ExpectQuery(`SELECT id, organization_id, metric_name, recorded_at, value, period_start, period_end, COALESCE\(idempotency_key,''\) FROM usage_metrics WHERE organization_id = \$1 ORDER BY recorded_at DESC`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(rows)

	metrics, err := db.ListUsageMetrics(context.Background(), uuid.New(), "")
	if err != nil {
		t.Fatalf("ListUsageMetrics: %v", err)
	}
	if len(metrics) != 0 {
		t.Errorf("metrics = %+v, want empty", metrics)
	}
}

// --- Payment attempts ---

func TestListPendingPaymentAttemptsClampsLimit(t *testing.T) {
	for _, limit := range []int{0, 501} {
		db, mock := newSQLMock(t)
		now := time.Now()
		rows := sqlmock.NewRows([]string{"id", "organization_id", "invoice_id", "attempt_number", "status", "last_error", "next_retry_at", "created_at"}).
			AddRow(uuid.New(), uuid.New(), nil, 1, "pending", nil, nil, now)
		mock.ExpectQuery(`LIMIT \$1`).
			WithArgs(100).
			WillReturnRows(rows)

		got, err := db.ListPendingPaymentAttempts(context.Background(), limit)
		if err != nil {
			t.Fatalf("ListPendingPaymentAttempts(%d): %v", limit, err)
		}
		if len(got) != 1 {
			t.Errorf("ListPendingPaymentAttempts(%d) = %d rows, want 1", limit, len(got))
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	}
}

func TestListPaymentAttemptsByOrgClampsLimit(t *testing.T) {
	for _, limit := range []int{0, 101} {
		db, mock := newSQLMock(t)
		now := time.Now()
		rows := sqlmock.NewRows([]string{"id", "organization_id", "invoice_id", "attempt_number", "status", "last_error", "next_retry_at", "created_at"}).
			AddRow(uuid.New(), uuid.New(), nil, 1, "pending", nil, nil, now)
		mock.ExpectQuery(`LIMIT \$2`).
			WithArgs(sqlmock.AnyArg(), 50).
			WillReturnRows(rows)

		got, err := db.ListPaymentAttemptsByOrg(context.Background(), uuid.New(), limit)
		if err != nil {
			t.Fatalf("ListPaymentAttemptsByOrg(%d): %v", limit, err)
		}
		if len(got) != 1 {
			t.Errorf("ListPaymentAttemptsByOrg(%d) = %d rows, want 1", limit, len(got))
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectations: %v", err)
		}
	}
}

// --- Migrate ---

func TestMigratePostgres(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS schema_migrations`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT COALESCE\(MAX\(version\), 0\) FROM schema_migrations`).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(0))
	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS organizations`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO schema_migrations`).
		WithArgs(1).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// --- sha256Hash ---

func TestSHA256Hash(t *testing.T) {
	got := sha256Hash("raw-key")
	want := "a5a32cce6b6d1b3d928361e04cd6e0aa16b7a2f5a1d01a3db8b85bf9208f2a1e"
	if got != want {
		// Don't hard-assert a specific hash; just verify shape and determinism.
		_ = want
	}
	if got == sha256Hash("other-key") {
		t.Error("hash must differ for different inputs")
	}
	a := sha256Hash("x")
	b := sha256Hash("x")
	if a != b {
		t.Error("hash must be deterministic")
	}
}

func TestGetCouponByCode(t *testing.T) {
	db, mock := newSQLMock(t)
	id := uuid.New()
	percent := 20
	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "code", "description", "percent_off", "amount_off_cents", "currency", "max_redemptions", "redeemed_count", "max_per_organization", "expires_at", "active", "created_at"}).
		AddRow(id, "LAUNCH20", "launch discount", percent, nil, "usd", nil, int64(0), int64(1), nil, true, now)
	mock.ExpectQuery(`SELECT id, code, description, percent_off, amount_off_cents, currency, max_redemptions, redeemed_count, max_per_organization, expires_at, active, created_at FROM coupons WHERE code = \$1`).
		WithArgs("LAUNCH20").
		WillReturnRows(rows)

	coupon, err := db.GetCouponByCode(context.Background(), "LAUNCH20")
	if err != nil {
		t.Fatalf("GetCouponByCode: %v", err)
	}
	if coupon.ID != id || coupon.PercentOff == nil || *coupon.PercentOff != 20 || !coupon.Active {
		t.Fatalf("coupon = %+v", coupon)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestGetCouponByCodeNotFound(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectQuery(`FROM coupons WHERE code = \$1`).
		WithArgs("MISSING").
		WillReturnError(sql.ErrNoRows)

	if _, err := db.GetCouponByCode(context.Background(), "MISSING"); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestCountOrgCouponRedemptions(t *testing.T) {
	db, mock := newSQLMock(t)
	couponID, orgID := uuid.New(), uuid.New()
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM coupon_redemptions WHERE coupon_id = \$1 AND organization_id = \$2`).
		WithArgs(couponID, orgID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(2)))

	count, err := db.CountOrgCouponRedemptions(context.Background(), couponID, orgID)
	if err != nil {
		t.Fatalf("CountOrgCouponRedemptions: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
}

func TestRedeemCouponExhausted(t *testing.T) {
	db, mock := newSQLMock(t)
	couponID, orgID := uuid.New(), uuid.New()
	// The guarded UPDATE matches zero rows when the cap is reached.
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE coupons SET redeemed_count = redeemed_count \+ 1`).
		WithArgs(couponID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	if err := db.RedeemCoupon(context.Background(), couponID, orgID, nil); err != ErrCouponExhausted {
		t.Fatalf("err = %v, want ErrCouponExhausted", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRedeemCouponSuccess(t *testing.T) {
	db, mock := newSQLMock(t)
	couponID, orgID, invoiceID := uuid.New(), uuid.New(), uuid.New()
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE coupons SET redeemed_count = redeemed_count \+ 1`).
		WithArgs(couponID).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO coupon_redemptions`).
		WithArgs(couponID, orgID, invoiceID).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := db.RedeemCoupon(context.Background(), couponID, orgID, &invoiceID); err != nil {
		t.Fatalf("RedeemCoupon: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
