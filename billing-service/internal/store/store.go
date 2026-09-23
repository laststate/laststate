// Package store handles database operations for the billing service.
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"github.com/laststate/billing-service/internal/schema"
)

// DB is the interface for database operations.
type DB interface {
	Close() error
	Migrate() error

	InsertSubscription(ctx context.Context, sub *Subscription) error
	GetSubscriptionByProviderSubID(ctx context.Context, providerSubID string) (*Subscription, error)
	ListSubscriptionsByOrg(ctx context.Context, orgID uuid.UUID) ([]Subscription, error)
	UpdateSubscriptionPlan(ctx context.Context, subID uuid.UUID, newPlanID string, proration int64) error
	UpdateSubscriptionStatus(ctx context.Context, providerSubID, status string, periodEnd time.Time) error

	InsertInvoice(ctx context.Context, inv *Invoice) error
	GetInvoiceByID(ctx context.Context, id uuid.UUID) (*Invoice, error)
	GetInvoiceByProviderID(ctx context.Context, provider, providerInvID string) (*Invoice, error)
	UpdateInvoiceStatus(ctx context.Context, providerInvID string, status string, paidAt time.Time) error
	UpdateInvoiceStatusByID(ctx context.Context, id uuid.UUID, status string, paidAt time.Time) error
	UpdateInvoiceStatusByProviderID(ctx context.Context, provider, providerInvID, status string, paidAt time.Time) error
	ListInvoices(ctx context.Context, orgID uuid.UUID, page, perPage int) ([]Invoice, int64, error)

	InsertWebhookEvent(ctx context.Context, we *WebhookEvent) error
	GetWebhookEvent(ctx context.Context, eventID string) (*WebhookEvent, error)

	GetPlanByID(ctx context.Context, planID string) (*Plan, error)

	InsertUsageMetric(ctx context.Context, m *UsageMetric) error
	GetUsageTotal(ctx context.Context, orgID uuid.UUID, metricName string, periodStart, periodEnd time.Time) (int64, error)
	ListUsageMetrics(ctx context.Context, orgID uuid.UUID, metricName string) ([]UsageMetric, error)
	// TryClaimUsageBillingRun atomically claims one usage-billing run per
	// (org, subscription, period). First caller gets claimed=true; replays
	// for the same period get claimed=false and must reuse the stored invoice.
	TryClaimUsageBillingRun(ctx context.Context, orgID uuid.UUID, subID string, periodStart, periodEnd time.Time, invoiceID uuid.UUID) (bool, error)
	// GetUsageBillingInvoice returns the invoice id stored for a claimed run.
	GetUsageBillingInvoice(ctx context.Context, orgID uuid.UUID, subID string, periodStart, periodEnd time.Time) (uuid.UUID, error)

	InsertPaymentAttempt(ctx context.Context, attempt *PaymentAttempt) error
	ListPendingPaymentAttempts(ctx context.Context, limit int) ([]PaymentAttempt, error)
	UpdatePaymentAttemptNextRetry(ctx context.Context, attemptID uuid.UUID, nextRetry time.Time) error
	UpdatePaymentAttemptStatus(ctx context.Context, attemptID uuid.UUID, status string) error
	// AdvancePaymentAttempt moves an attempt to the next schedule step:
	// attempt_number + 1 with a new next_retry_at.
	AdvancePaymentAttempt(ctx context.Context, attemptID uuid.UUID, nextRetry time.Time) error
	ListPaymentAttemptsByOrg(ctx context.Context, orgID uuid.UUID, limit int) ([]PaymentAttempt, error)
	GetOrgByID(ctx context.Context, orgID uuid.UUID) (*Organization, error)
	ListOrganizationIDs(ctx context.Context, limit int) ([]uuid.UUID, error)
	SetOrganizationTrialEndsAt(ctx context.Context, orgID uuid.UUID, endsAt *time.Time) error
	ListExpiredTrials(ctx context.Context, limit int) ([]Organization, error)

	CreateCoupon(ctx context.Context, c *Coupon) error
	GetCouponByCode(ctx context.Context, code string) (*Coupon, error)
	CountOrgCouponRedemptions(ctx context.Context, couponID, orgID uuid.UUID) (int64, error)
	// RedeemCoupon atomically increments the coupon counter and records the
	// redemption. Returns ErrCouponExhausted when a limit blocks redemption.
	RedeemCoupon(ctx context.Context, couponID, orgID uuid.UUID, invoiceID *uuid.UUID) error
}

// Coupon is a discount code. Exactly one of PercentOff / AmountOffCents is set.
type Coupon struct {
	ID                 uuid.UUID  `json:"id"`
	Code               string     `json:"code"`
	Description        string     `json:"description,omitempty"`
	PercentOff         *int       `json:"percent_off,omitempty"`
	AmountOffCents     *int       `json:"amount_off_cents,omitempty"`
	Currency           string     `json:"currency"`
	MaxRedemptions     *int64     `json:"max_redemptions,omitempty"`
	RedeemedCount      int64      `json:"redeemed_count"`
	MaxPerOrganization int64      `json:"max_per_organization"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	Active             bool       `json:"active"`
	CreatedAt          time.Time  `json:"created_at"`
}

// ErrCouponExhausted is returned when a coupon can no longer be redeemed.
var ErrCouponExhausted = fmt.Errorf("coupon exhausted")

// APIKey represents a valid API key.
type APIKey struct {
	ID        uuid.UUID `json:"id"`
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	OrgID     uuid.UUID `json:"organization_id"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	LastUsed  time.Time `json:"last_used"`
}

// APIKeyStore defines the interface for API key validation.
type APIKeyStore interface {
	GetByKey(ctx context.Context, key string) (APIKey, error)
}

// PostgresDB wraps the database connection.
type PostgresDB struct {
	*sql.DB
}

// NewPostgres creates a new database connection.
func NewPostgres(url string) (*PostgresDB, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	return &PostgresDB{db}, nil
}

// Close closes the underlying database connection.
func (d *PostgresDB) Close() error {
	return d.DB.Close()
}

// Migrate runs schema migrations.
func (d *PostgresDB) Migrate() error {
	return migratePostgres(d.DB)
}

func migratePostgres(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	var currentVersion int
	err = db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&currentVersion)
	if err != nil {
		return fmt.Errorf("get current migration version: %w", err)
	}

	_, err = db.Exec(schema.MigrationSQL)
	if err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	_, err = db.Exec(`INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT (version) DO NOTHING`, currentVersion+1)
	if err != nil {
		return fmt.Errorf("update migration version: %w", err)
	}

	return nil
}

// Organization represents a tenant in the system.
type Organization struct {
	ID               uuid.UUID  `json:"id"`
	Name             string     `json:"name"`
	Email            string     `json:"email"`
	Tier             string     `json:"tier"`
	Address          string     `json:"address,omitempty"`
	TaxID            string     `json:"tax_id,omitempty"`
	StripeCustomerID *string    `json:"stripe_customer_id,omitempty"`
	StripeSubID      *string    `json:"stripe_subscription_id,omitempty"`
	MPPrefID         *string    `json:"mercado_pago_pref_id,omitempty"`
	Phone            string     `json:"phone,omitempty"`
	TrialEndsAt      *time.Time `json:"trial_ends_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// Subscription represents a customer subscription.
type Subscription struct {
	ID                 uuid.UUID `json:"id"`
	OrganizationID     uuid.UUID `json:"organization_id"`
	Provider           string    `json:"provider"`
	ProviderSubID      string    `json:"provider_subscription_id"`
	PlanID             string    `json:"plan_id"`
	Status             string    `json:"status"`
	CurrentPeriodStart time.Time `json:"current_period_start"`
	CurrentPeriodEnd   time.Time `json:"current_period_end"`
	CancelAtPeriodEnd  bool      `json:"cancel_at_period_end"`
	LastProrationCents int64     `json:"last_proration_cents,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// Invoice represents a billing invoice.
type Invoice struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	SubscriptionID *uuid.UUID `json:"subscription_id,omitempty"`
	Provider       string     `json:"provider"`
	ProviderInvID  *string    `json:"provider_invoice_id,omitempty"`
	AmountCents    int        `json:"amount_cents"`
	Currency       string     `json:"currency"`
	Status         string     `json:"status"`
	IssuedAt       time.Time  `json:"issued_at"`
	PaidAt         *time.Time `json:"paid_at,omitempty"`
	DueAt          *time.Time `json:"due_at,omitempty"`
	PDFUrl         *string    `json:"pdf_url,omitempty"`
	NFEUrl         *string    `json:"nfe_url,omitempty"`
}

// WebhookEvent represents a processed webhook event (for idempotency).
type WebhookEvent struct {
	ID          uuid.UUID `json:"id"`
	Provider    string    `json:"provider"`
	EventID     string    `json:"event_id"`
	RawPayload  []byte    `json:"raw_payload"`
	ProcessedAt time.Time `json:"processed_at"`
}

// PaymentAttempt represents a dunning attempt.
type PaymentAttempt struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	InvoiceID      *uuid.UUID `json:"invoice_id,omitempty"`
	AttemptNumber  int        `json:"attempt_number"`
	Status         string     `json:"status"`
	LastError      *string    `json:"last_error,omitempty"`
	NextRetryAt    *time.Time `json:"next_retry_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// UsageMetric represents a usage metric for metering.
type UsageMetric struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	MetricName     string    `json:"metric_name"`
	RecordedAt     time.Time `json:"recorded_at"`
	Value          int64     `json:"value"`
	PeriodStart    time.Time `json:"period_start"`
	PeriodEnd      time.Time `json:"period_end"`
	// IdempotencyKey dedupes retries: replays with the same
	// (organization_id, metric_name, idempotency_key) are dropped.
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// ErrUsageDuplicate is returned when a usage ingest with the same
// idempotency key was already recorded (safe to treat as success).
var ErrUsageDuplicate = fmt.Errorf("usage duplicate")

// ErrUsageBillingDuplicate is returned when a usage-billing run for the same
// (org, subscription, period) was already claimed.
var ErrUsageBillingDuplicate = fmt.Errorf("usage billing already claimed for period")

// Plan represents a billing plan.
type Plan struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	PriceCents  int    `json:"price_cents"`
	Currency    string `json:"currency"`
	Interval    string `json:"interval"`
	Description string `json:"description"`
}

// ErrNotFound is returned when a resource is not found.
var ErrNotFound = fmt.Errorf("not found")

func (d *PostgresDB) InsertSubscription(ctx context.Context, sub *Subscription) error {
	_, err := d.ExecContext(ctx, `INSERT INTO subscriptions (id, organization_id, provider, provider_subscription_id, plan_id, status, current_period_start, current_period_end, cancel_at_period_end, last_proration_cents, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		sub.ID, sub.OrganizationID, sub.Provider, sub.ProviderSubID, sub.PlanID, sub.Status, sub.CurrentPeriodStart, sub.CurrentPeriodEnd, sub.CancelAtPeriodEnd, sub.LastProrationCents, sub.CreatedAt, sub.UpdatedAt)
	return err
}

func (d *PostgresDB) GetSubscriptionByProviderSubID(ctx context.Context, providerSubID string) (*Subscription, error) {
	var sub Subscription
	err := d.DB.QueryRowContext(ctx, `SELECT id, organization_id, provider, provider_subscription_id, plan_id, status, current_period_start, current_period_end, cancel_at_period_end, last_proration_cents, created_at, updated_at FROM subscriptions WHERE provider_subscription_id = $1`, providerSubID).
		Scan(&sub.ID, &sub.OrganizationID, &sub.Provider, &sub.ProviderSubID, &sub.PlanID, &sub.Status, &sub.CurrentPeriodStart, &sub.CurrentPeriodEnd, &sub.CancelAtPeriodEnd, &sub.LastProrationCents, &sub.CreatedAt, &sub.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// ListSubscriptionsByOrg returns all subscriptions for an organization,
// newest first.
func (d *PostgresDB) ListSubscriptionsByOrg(ctx context.Context, orgID uuid.UUID) ([]Subscription, error) {
	rows, err := d.QueryContext(ctx, `SELECT id, organization_id, provider, provider_subscription_id, plan_id, status, current_period_start, current_period_end, cancel_at_period_end, last_proration_cents, created_at, updated_at FROM subscriptions WHERE organization_id = $1 ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subscription
	for rows.Next() {
		var sub Subscription
		if err := rows.Scan(&sub.ID, &sub.OrganizationID, &sub.Provider, &sub.ProviderSubID, &sub.PlanID, &sub.Status, &sub.CurrentPeriodStart, &sub.CurrentPeriodEnd, &sub.CancelAtPeriodEnd, &sub.LastProrationCents, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

func (d *PostgresDB) UpdateSubscriptionPlan(ctx context.Context, subID uuid.UUID, newPlanID string, proration int64) error {
	_, err := d.ExecContext(ctx, `UPDATE subscriptions SET plan_id = $1, last_proration_cents = $2, updated_at = now() WHERE id = $3`, newPlanID, proration, subID)
	return err
}

func (d *PostgresDB) UpdateSubscriptionStatus(ctx context.Context, providerSubID, status string, periodEnd time.Time) error {
	if periodEnd.IsZero() {
		_, err := d.ExecContext(ctx, `UPDATE subscriptions SET status = $1 WHERE provider_subscription_id = $2`, status, providerSubID)
		return err
	}
	_, err := d.ExecContext(ctx, `UPDATE subscriptions SET status = $1, current_period_end = $2 WHERE provider_subscription_id = $3`, status, periodEnd, providerSubID)
	return err
}

func (d *PostgresDB) InsertInvoice(ctx context.Context, inv *Invoice) error {
	_, err := d.ExecContext(ctx, `INSERT INTO invoices (id, organization_id, subscription_id, provider, provider_invoice_id, amount_cents, currency, status, issued_at, paid_at, due_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		inv.ID, inv.OrganizationID, inv.SubscriptionID, inv.Provider, inv.ProviderInvID, inv.AmountCents, inv.Currency, inv.Status, inv.IssuedAt, inv.PaidAt, inv.DueAt)
	return err
}

func (d *PostgresDB) GetInvoiceByID(ctx context.Context, id uuid.UUID) (*Invoice, error) {
	var inv Invoice
	err := d.DB.QueryRowContext(ctx, `SELECT id, organization_id, subscription_id, provider, provider_invoice_id, amount_cents, currency, status, issued_at, paid_at, due_at, pdf_url, nfe_url FROM invoices WHERE id = $1`, id).
		Scan(&inv.ID, &inv.OrganizationID, &inv.SubscriptionID, &inv.Provider, &inv.ProviderInvID, &inv.AmountCents, &inv.Currency, &inv.Status, &inv.IssuedAt, &inv.PaidAt, &inv.DueAt, &inv.PDFUrl, &inv.NFEUrl)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

// GetInvoiceByProviderID fetches an invoice by provider + provider id.
func (d *PostgresDB) GetInvoiceByProviderID(ctx context.Context, provider, providerInvID string) (*Invoice, error) {
	var inv Invoice
	err := d.DB.QueryRowContext(ctx, `SELECT id, organization_id, subscription_id, provider, provider_invoice_id, amount_cents, currency, status, issued_at, paid_at, due_at, pdf_url, nfe_url FROM invoices WHERE provider = $1 AND provider_invoice_id = $2`, provider, providerInvID).
		Scan(&inv.ID, &inv.OrganizationID, &inv.SubscriptionID, &inv.Provider, &inv.ProviderInvID, &inv.AmountCents, &inv.Currency, &inv.Status, &inv.IssuedAt, &inv.PaidAt, &inv.DueAt, &inv.PDFUrl, &inv.NFEUrl)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

func (d *PostgresDB) UpdateInvoiceStatus(ctx context.Context, providerInvID string, status string, paidAt time.Time) error {
	// Match by provider_invoice_id or by local id (if providerInvID is a
	// UUID). The caller may pass either the provider's external invoice ID
	// or the local UUID string, so we compare against both columns. The
	// id::text comparison is guarded by a UUID-shape check to avoid
	// needless casts on every external id.
	if _, err := uuid.Parse(providerInvID); err == nil {
		_, err := d.ExecContext(ctx, `UPDATE invoices SET status = $1, paid_at = $2 WHERE provider_invoice_id = $3 OR id::text = $3`, status, paidAt, providerInvID)
		return err
	}
	_, err := d.ExecContext(ctx, `UPDATE invoices SET status = $1, paid_at = $2 WHERE provider_invoice_id = $3`, status, paidAt, providerInvID)
	return err
}

func (d *PostgresDB) UpdateInvoiceStatusByProviderID(ctx context.Context, provider, providerInvID, status string, paidAt time.Time) error {
	_, err := d.ExecContext(ctx, `UPDATE invoices SET status = $1, paid_at = $2 WHERE provider = $3 AND provider_invoice_id = $4`, status, paidAt, provider, providerInvID)
	return err
}

// UpdateInvoiceStatusByID updates an invoice by its local id (refunds,
// manual adjustments). Provider webhooks match by provider id instead.
func (d *PostgresDB) UpdateInvoiceStatusByID(ctx context.Context, id uuid.UUID, status string, paidAt time.Time) error {
	_, err := d.ExecContext(ctx, `UPDATE invoices SET status = $1, paid_at = $2 WHERE id = $3`, status, paidAt, id)
	return err
}

func (d *PostgresDB) ListInvoices(ctx context.Context, orgID uuid.UUID, page, perPage int) ([]Invoice, int64, error) {
	var total int64
	if err := d.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM invoices WHERE organization_id = $1`, orgID).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := d.QueryContext(ctx, `SELECT id, organization_id, subscription_id, provider, provider_invoice_id, amount_cents, currency, status, issued_at, paid_at, due_at, pdf_url, nfe_url FROM invoices WHERE organization_id = $1 ORDER BY issued_at DESC LIMIT $2 OFFSET $3`,
		orgID, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var invoices []Invoice
	for rows.Next() {
		var inv Invoice
		if err := rows.Scan(&inv.ID, &inv.OrganizationID, &inv.SubscriptionID, &inv.Provider, &inv.ProviderInvID, &inv.AmountCents, &inv.Currency, &inv.Status, &inv.IssuedAt, &inv.PaidAt, &inv.DueAt, &inv.PDFUrl, &inv.NFEUrl); err != nil {
			return nil, 0, err
		}
		invoices = append(invoices, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return invoices, total, nil
}

func (d *PostgresDB) InsertWebhookEvent(ctx context.Context, we *WebhookEvent) error {
	_, err := d.ExecContext(ctx, `INSERT INTO webhook_events (provider, event_id, raw_payload, processed_at) VALUES ($1, $2, $3, $4) ON CONFLICT (event_id) DO NOTHING`,
		we.Provider, we.EventID, we.RawPayload, we.ProcessedAt)
	return err
}

func (d *PostgresDB) GetWebhookEvent(ctx context.Context, eventID string) (*WebhookEvent, error) {
	var we WebhookEvent
	err := d.DB.QueryRowContext(ctx, `SELECT id, provider, event_id, raw_payload, processed_at FROM webhook_events WHERE event_id = $1`, eventID).
		Scan(&we.ID, &we.Provider, &we.EventID, &we.RawPayload, &we.ProcessedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &we, nil
}

func (d *PostgresDB) GetPlanByID(ctx context.Context, planID string) (*Plan, error) {
	var p Plan
	err := d.DB.QueryRowContext(ctx, `SELECT id, name, price_cents, currency, interval, description FROM plans WHERE id = $1`, planID).
		Scan(&p.ID, &p.Name, &p.PriceCents, &p.Currency, &p.Interval, &p.Description)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (d *PostgresDB) InsertUsageMetric(ctx context.Context, m *UsageMetric) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	key := sql.NullString{}
	if m.IdempotencyKey != "" {
		key = sql.NullString{String: m.IdempotencyKey, Valid: true}
	}
	res, err := d.ExecContext(ctx, `INSERT INTO usage_metrics (id, organization_id, metric_name, recorded_at, value, period_start, period_end, idempotency_key) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT ON CONSTRAINT ux_usage_metrics_idem DO NOTHING`,
		m.ID, m.OrganizationID, m.MetricName, m.RecordedAt, m.Value, m.PeriodStart, m.PeriodEnd, key)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 && m.IdempotencyKey != "" {
		return ErrUsageDuplicate
	}
	return nil
}

func (d *PostgresDB) GetUsageTotal(ctx context.Context, orgID uuid.UUID, metricName string, periodStart, periodEnd time.Time) (int64, error) {
	var total int64
	err := d.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(value), 0) FROM usage_metrics WHERE organization_id = $1 AND metric_name = $2 AND recorded_at BETWEEN $3 AND $4`,
		orgID, metricName, periodStart, periodEnd).Scan(&total)
	return total, err
}

func (d *PostgresDB) ListUsageMetrics(ctx context.Context, orgID uuid.UUID, metricName string) ([]UsageMetric, error) {
	var rows *sql.Rows
	var err error
	if metricName != "" {
		rows, err = d.QueryContext(ctx, `SELECT id, organization_id, metric_name, recorded_at, value, period_start, period_end, COALESCE(idempotency_key,'') FROM usage_metrics WHERE organization_id = $1 AND metric_name = $2 ORDER BY recorded_at DESC`, orgID, metricName)
	} else {
		rows, err = d.QueryContext(ctx, `SELECT id, organization_id, metric_name, recorded_at, value, period_start, period_end, COALESCE(idempotency_key,'') FROM usage_metrics WHERE organization_id = $1 ORDER BY recorded_at DESC`, orgID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metrics []UsageMetric
	for rows.Next() {
		var m UsageMetric
		if err := rows.Scan(&m.ID, &m.OrganizationID, &m.MetricName, &m.RecordedAt, &m.Value, &m.PeriodStart, &m.PeriodEnd, &m.IdempotencyKey); err != nil {
			return nil, err
		}
		metrics = append(metrics, m)
	}
	return metrics, rows.Err()
}

// TryClaimUsageBillingRun claims one billing run per (org, subscription,
// period). Uses INSERT ... ON CONFLICT DO NOTHING so concurrent schedulers
// and retries converge on a single invoice.
func (d *PostgresDB) TryClaimUsageBillingRun(ctx context.Context, orgID uuid.UUID, subID string, periodStart, periodEnd time.Time, invoiceID uuid.UUID) (bool, error) {
	res, err := d.ExecContext(ctx, `INSERT INTO usage_billing_runs (organization_id, subscription_id, period_start, period_end, invoice_id) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (organization_id, subscription_id, period_start, period_end) DO NOTHING`,
		orgID, subID, periodStart, periodEnd, invoiceID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// GetUsageBillingInvoice returns the invoice id stored for a claimed run.
func (d *PostgresDB) GetUsageBillingInvoice(ctx context.Context, orgID uuid.UUID, subID string, periodStart, periodEnd time.Time) (uuid.UUID, error) {
	var id uuid.UUID
	err := d.DB.QueryRowContext(ctx, `SELECT invoice_id FROM usage_billing_runs WHERE organization_id = $1 AND subscription_id = $2 AND period_start = $3 AND period_end = $4`,
		orgID, subID, periodStart, periodEnd).Scan(&id)
	if err == sql.ErrNoRows {
		return uuid.Nil, ErrNotFound
	}
	return id, err
}

func (d *PostgresDB) InsertPaymentAttempt(ctx context.Context, attempt *PaymentAttempt) error {
	_, err := d.ExecContext(ctx, `INSERT INTO payment_attempts (id, organization_id, invoice_id, attempt_number, status, last_error, next_retry_at, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		attempt.ID, attempt.OrganizationID, attempt.InvoiceID, attempt.AttemptNumber, attempt.Status, attempt.LastError, attempt.NextRetryAt, attempt.CreatedAt)
	return err
}

// sha256Hash returns the hex-encoded SHA-256 hash of the input.
func sha256Hash(input string) string {
	sum := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", sum)
}

// GetByKey looks up an API key by its raw value (hashed for storage safety).
// Implements the APIKeyStore interface required by the auth middleware.
// A token is considered active if expires_at is NULL or in the future.
func (d *PostgresDB) GetByKey(ctx context.Context, key string) (APIKey, error) {
	hash := sha256Hash(key)
	var a APIKey
	var lastUsedAt sql.NullTime
	var expiresAt sql.NullTime
	var createdAt time.Time
	err := d.DB.QueryRowContext(ctx,
		`SELECT id, name, organization_id, expires_at, last_used_at, created_at
		 FROM api_tokens WHERE token_hash = $1`, hash).
		Scan(&a.ID, &a.Name, &a.OrgID, &expiresAt, &lastUsedAt, &createdAt)
	if err == sql.ErrNoRows {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	a.Key = key
	a.Active = !expiresAt.Valid || expiresAt.Time.After(time.Now())
	if lastUsedAt.Valid {
		a.LastUsed = lastUsedAt.Time
	}
	return a, nil
}

// ListPendingPaymentAttempts returns all payment attempts that are pending retry.
func (d *PostgresDB) ListPendingPaymentAttempts(ctx context.Context, limit int) ([]PaymentAttempt, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := d.QueryContext(ctx, `
		SELECT id, organization_id, invoice_id, attempt_number, status, last_error, next_retry_at, created_at
		FROM payment_attempts
		WHERE status = 'pending' AND (next_retry_at IS NULL OR next_retry_at <= now())
		ORDER BY created_at ASC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PaymentAttempt
	for rows.Next() {
		var p PaymentAttempt
		if err := rows.Scan(&p.ID, &p.OrganizationID, &p.InvoiceID, &p.AttemptNumber, &p.Status, &p.LastError, &p.NextRetryAt, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdatePaymentAttemptNextRetry updates the next retry time for a payment attempt.
func (d *PostgresDB) UpdatePaymentAttemptNextRetry(ctx context.Context, attemptID uuid.UUID, nextRetry time.Time) error {
	_, err := d.ExecContext(ctx, `UPDATE payment_attempts SET next_retry_at = $1 WHERE id = $2`, nextRetry, attemptID)
	return err
}

// UpdatePaymentAttemptStatus updates the status of a payment attempt.
func (d *PostgresDB) UpdatePaymentAttemptStatus(ctx context.Context, attemptID uuid.UUID, status string) error {
	_, err := d.ExecContext(ctx, `UPDATE payment_attempts SET status = $1 WHERE id = $2`, status, attemptID)
	return err
}

// AdvancePaymentAttempt moves an attempt to the next schedule step.
func (d *PostgresDB) AdvancePaymentAttempt(ctx context.Context, attemptID uuid.UUID, nextRetry time.Time) error {
	_, err := d.ExecContext(ctx, `UPDATE payment_attempts SET attempt_number = attempt_number + 1, next_retry_at = $1 WHERE id = $2`, nextRetry, attemptID)
	return err
}

// ListPaymentAttemptsByOrg returns payment attempts for an organization, ordered by created_at DESC.
func (d *PostgresDB) ListPaymentAttemptsByOrg(ctx context.Context, orgID uuid.UUID, limit int) ([]PaymentAttempt, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := d.QueryContext(ctx, `
		SELECT id, organization_id, invoice_id, attempt_number, status, last_error, next_retry_at, created_at
		FROM payment_attempts
		WHERE organization_id = $1
		ORDER BY created_at DESC
		LIMIT $2`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PaymentAttempt
	for rows.Next() {
		var p PaymentAttempt
		if err := rows.Scan(&p.ID, &p.OrganizationID, &p.InvoiceID, &p.AttemptNumber, &p.Status, &p.LastError, &p.NextRetryAt, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateOrganizationTier updates an organization's subscription tier.
func (d *PostgresDB) UpdateOrganizationTier(ctx context.Context, orgID uuid.UUID, tier string) error {
	res, err := d.ExecContext(ctx,
		`UPDATE organizations SET tier = $1, updated_at = now() WHERE id = $2`, tier, orgID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetOrgByID returns an organization by its ID.
func (d *PostgresDB) GetOrgByID(ctx context.Context, orgID uuid.UUID) (*Organization, error) {
	var org Organization
	err := d.DB.QueryRowContext(ctx,
		`SELECT id, name, email, tier, address, tax_id, stripe_customer_id, stripe_subscription_id, mercado_pago_pref_id, phone, trial_ends_at, created_at, updated_at
		 FROM organizations WHERE id = $1`, orgID).
		Scan(&org.ID, &org.Name, &org.Email, &org.Tier, &org.Address, &org.TaxID, &org.StripeCustomerID, &org.StripeSubID, &org.MPPrefID, &org.Phone, &org.TrialEndsAt, &org.CreatedAt, &org.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &org, nil
}

// SetOrganizationTrialEndsAt sets (or clears, when nil) the trial deadline.
func (d *PostgresDB) SetOrganizationTrialEndsAt(ctx context.Context, orgID uuid.UUID, endsAt *time.Time) error {
	_, err := d.ExecContext(ctx, `UPDATE organizations SET trial_ends_at = $1, updated_at = now() WHERE id = $2`, endsAt, orgID)
	return err
}

// ListExpiredTrials returns organizations whose trial deadline passed.
func (d *PostgresDB) ListExpiredTrials(ctx context.Context, limit int) ([]Organization, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := d.QueryContext(ctx, `SELECT id, name, email, tier, address, tax_id, stripe_customer_id, stripe_subscription_id, mercado_pago_pref_id, phone, trial_ends_at, created_at, updated_at FROM organizations WHERE trial_ends_at IS NOT NULL AND trial_ends_at <= now() ORDER BY trial_ends_at ASC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Organization
	for rows.Next() {
		var org Organization
		if err := rows.Scan(&org.ID, &org.Name, &org.Email, &org.Tier, &org.Address, &org.TaxID, &org.StripeCustomerID, &org.StripeSubID, &org.MPPrefID, &org.Phone, &org.TrialEndsAt, &org.CreatedAt, &org.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, org)
	}
	return out, rows.Err()
}

// ListOrganizationIDs enumerates org ids for batch jobs (usage billing).
func (d *PostgresDB) ListOrganizationIDs(ctx context.Context, limit int) ([]uuid.UUID, error) {
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := d.QueryContext(ctx, `SELECT id FROM organizations ORDER BY created_at ASC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// CreateCoupon inserts a new discount coupon.
func (d *PostgresDB) CreateCoupon(ctx context.Context, c *Coupon) error {
	_, err := d.ExecContext(ctx, `INSERT INTO coupons (id, code, description, percent_off, amount_off_cents, currency, max_redemptions, max_per_organization, expires_at, active, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		c.ID, c.Code, c.Description, c.PercentOff, c.AmountOffCents, c.Currency, c.MaxRedemptions, c.MaxPerOrganization, c.ExpiresAt, c.Active, c.CreatedAt)
	return err
}

// GetCouponByCode looks up a coupon by its human-readable code.
func (d *PostgresDB) GetCouponByCode(ctx context.Context, code string) (*Coupon, error) {
	var c Coupon
	err := d.QueryRowContext(ctx, `SELECT id, code, description, percent_off, amount_off_cents, currency, max_redemptions, redeemed_count, max_per_organization, expires_at, active, created_at FROM coupons WHERE code = $1`, code).
		Scan(&c.ID, &c.Code, &c.Description, &c.PercentOff, &c.AmountOffCents, &c.Currency, &c.MaxRedemptions, &c.RedeemedCount, &c.MaxPerOrganization, &c.ExpiresAt, &c.Active, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// CountOrgCouponRedemptions counts how many times an organization redeemed
// a coupon (used to enforce max_per_organization).
func (d *PostgresDB) CountOrgCouponRedemptions(ctx context.Context, couponID, orgID uuid.UUID) (int64, error) {
	var count int64
	err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM coupon_redemptions WHERE coupon_id = $1 AND organization_id = $2`, couponID, orgID).Scan(&count)
	return count, err
}

// RedeemCoupon atomically claims one redemption: the UPDATE succeeds only
// while the coupon is active, unexpired, and below its global cap, then the
// redemption row is recorded in the same transaction.
func (d *PostgresDB) RedeemCoupon(ctx context.Context, couponID, orgID uuid.UUID, invoiceID *uuid.UUID) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `UPDATE coupons SET redeemed_count = redeemed_count + 1 WHERE id = $1 AND active = true AND (expires_at IS NULL OR expires_at > now()) AND (max_redemptions IS NULL OR redeemed_count < max_redemptions)`, couponID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrCouponExhausted
	}

	var invoiceAny any
	if invoiceID != nil {
		invoiceAny = *invoiceID
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO coupon_redemptions (coupon_id, organization_id, invoice_id) VALUES ($1, $2, $3)`, couponID, orgID, invoiceAny); err != nil {
		return err
	}
	return tx.Commit()
}
