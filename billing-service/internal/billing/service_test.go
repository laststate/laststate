package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/laststate/billing-service/internal/store"
)

// mockDB implements a minimal in-memory store for testing.
// It mirrors the store.DB interface without requiring a real database.
type mockDB struct {
	mu sync.RWMutex

	subscriptions   map[string]*store.Subscription
	invoices        map[uuid.UUID]*store.Invoice
	webhookEvents   map[string]*store.WebhookEvent
	plans           map[string]*store.Plan
	usageMetrics    []store.UsageMetric
	usageKeys       map[string]bool
	usageRuns       map[string]uuid.UUID
	paymentAttempts []store.PaymentAttempt
	coupons         map[string]*store.Coupon
	redemptions     map[uuid.UUID]map[uuid.UUID]int64
	trialEndsAt     map[uuid.UUID]*time.Time
}

func newMockDB() *mockDB {
	return &mockDB{
		subscriptions: make(map[string]*store.Subscription),
		invoices:      make(map[uuid.UUID]*store.Invoice),
		webhookEvents: make(map[string]*store.WebhookEvent),
		plans: map[string]*store.Plan{
			"free": {ID: "free", Name: "Free", PriceCents: 0, Currency: "usd", Interval: "month"},
			"pro":  {ID: "pro", Name: "Pro", PriceCents: 2900, Currency: "usd", Interval: "month"},
			"ent":  {ID: "enterprise", Name: "Enterprise", PriceCents: 9900, Currency: "usd", Interval: "month"},
		},
		coupons:     make(map[string]*store.Coupon),
		redemptions: make(map[uuid.UUID]map[uuid.UUID]int64),
		trialEndsAt: make(map[uuid.UUID]*time.Time),
		usageKeys:   make(map[string]bool),
		usageRuns:   make(map[string]uuid.UUID),
	}
}

func (m *mockDB) InsertSubscription(_ context.Context, sub *store.Subscription) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	subCopy := *sub
	m.subscriptions[sub.ProviderSubID] = &subCopy
	return nil
}

func (m *mockDB) GetSubscriptionByProviderSubID(_ context.Context, id string) (*store.Subscription, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sub, ok := m.subscriptions[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return sub, nil
}

func (m *mockDB) UpdateSubscriptionStatus(_ context.Context, id, status string, periodEnd time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sub := range m.subscriptions {
		if sub.ProviderSubID == id {
			sub.Status = status
			if !periodEnd.IsZero() {
				sub.CurrentPeriodEnd = periodEnd
			}
			return nil
		}
	}
	return nil
}

func (m *mockDB) UpdateSubscriptionPlan(_ context.Context, id uuid.UUID, newPlanID string, proration int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sub := range m.subscriptions {
		if sub.ID == id {
			sub.PlanID = newPlanID
			sub.LastProrationCents = proration
			return nil
		}
	}
	return nil
}

func (m *mockDB) InsertInvoice(_ context.Context, inv *store.Invoice) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	invCopy := *inv
	m.invoices[inv.ID] = &invCopy
	return nil
}

func (m *mockDB) GetInvoiceByID(_ context.Context, id uuid.UUID) (*store.Invoice, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	inv, ok := m.invoices[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return inv, nil
}

func (m *mockDB) UpdateInvoiceStatus(_ context.Context, providerInvID string, status string, paidAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, inv := range m.invoices {
		if inv.ProviderInvID != nil && *inv.ProviderInvID == providerInvID {
			inv.Status = status
			inv.PaidAt = &paidAt
			return nil
		}
	}
	return nil
}

func (m *mockDB) UpdateInvoiceStatusByProviderID(_ context.Context, provider, providerInvID, status string, paidAt time.Time) error {
	return m.UpdateInvoiceStatus(context.Background(), providerInvID, status, paidAt)
}

func (m *mockDB) GetInvoiceByProviderID(_ context.Context, provider, providerInvID string) (*store.Invoice, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, inv := range m.invoices {
		if inv.Provider == provider && inv.ProviderInvID != nil && *inv.ProviderInvID == providerInvID {
			return inv, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockDB) UpdateInvoiceStatusByID(_ context.Context, id uuid.UUID, status string, paidAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if inv, ok := m.invoices[id]; ok {
		inv.Status = status
		inv.PaidAt = &paidAt
		return nil
	}
	return fmt.Errorf("not found")
}

func (m *mockDB) InsertWebhookEvent(_ context.Context, we *store.WebhookEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	weCopy := *we
	m.webhookEvents[we.EventID] = &weCopy
	return nil
}

func (m *mockDB) GetWebhookEvent(_ context.Context, eventID string) (*store.WebhookEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	we, ok := m.webhookEvents[eventID]
	if !ok {
		return nil, nil
	}
	return we, nil
}

func (m *mockDB) GetPlanByID(_ context.Context, planID string) (*store.Plan, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.plans[planID]
	if !ok {
		return nil, store.ErrNotFound
	}
	return p, nil
}

func (m *mockDB) ListInvoices(_ context.Context, orgID uuid.UUID, _page, _perPage int) ([]store.Invoice, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []store.Invoice
	for _, inv := range m.invoices {
		if inv.OrganizationID == orgID {
			out = append(out, *inv)
		}
	}
	return out, int64(len(out)), nil
}

func (m *mockDB) InsertUsageMetric(_ context.Context, u *store.UsageMetric) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u.IdempotencyKey != "" {
		k := u.OrganizationID.String() + "|" + u.MetricName + "|" + u.IdempotencyKey
		if m.usageKeys[k] {
			return store.ErrUsageDuplicate
		}
		m.usageKeys[k] = true
	}
	uCopy := *u
	m.usageMetrics = append(m.usageMetrics, uCopy)
	return nil
}

func usageRunKey(orgID uuid.UUID, subID string, start, end time.Time) string {
	return orgID.String() + "|" + subID + "|" + start.UTC().Format(time.RFC3339) + "|" + end.UTC().Format(time.RFC3339)
}

func (m *mockDB) TryClaimUsageBillingRun(_ context.Context, orgID uuid.UUID, subID string, start, end time.Time, invoiceID uuid.UUID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := usageRunKey(orgID, subID, start, end)
	if _, ok := m.usageRuns[k]; ok {
		return false, nil
	}
	m.usageRuns[k] = invoiceID
	return true, nil
}

func (m *mockDB) GetUsageBillingInvoice(_ context.Context, orgID uuid.UUID, subID string, start, end time.Time) (uuid.UUID, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.usageRuns[usageRunKey(orgID, subID, start, end)]
	if !ok {
		return uuid.Nil, store.ErrNotFound
	}
	return id, nil
}

func (m *mockDB) GetUsageTotal(_ context.Context, _ uuid.UUID, metricName string, _, _ time.Time) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total := int64(0)
	for _, u := range m.usageMetrics {
		if u.MetricName == metricName {
			total += u.Value
		}
	}
	return total, nil
}

func (m *mockDB) ListUsageMetrics(_ context.Context, _ uuid.UUID, _ string) ([]store.UsageMetric, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]store.UsageMetric, len(m.usageMetrics))
	copy(out, m.usageMetrics)
	return out, nil
}

func (m *mockDB) InsertPaymentAttempt(_ context.Context, pa *store.PaymentAttempt) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	paCopy := *pa
	m.paymentAttempts = append(m.paymentAttempts, paCopy)
	return nil
}

func (m *mockDB) ListPendingPaymentAttempts(_ context.Context, limit int) ([]store.PaymentAttempt, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]store.PaymentAttempt, 0)
	for _, pa := range m.paymentAttempts {
		if pa.Status == "pending" {
			out = append(out, pa)
		}
	}
	return out, nil
}

func (m *mockDB) UpdatePaymentAttemptNextRetry(_ context.Context, attemptID uuid.UUID, nextRetry time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, pa := range m.paymentAttempts {
		if pa.ID == attemptID {
			m.paymentAttempts[i].NextRetryAt = &nextRetry
			break
		}
	}
	return nil
}

func (m *mockDB) UpdatePaymentAttemptStatus(_ context.Context, attemptID uuid.UUID, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, pa := range m.paymentAttempts {
		if pa.ID == attemptID {
			m.paymentAttempts[i].Status = status
			break
		}
	}
	return nil
}

func (m *mockDB) AdvancePaymentAttempt(_ context.Context, attemptID uuid.UUID, nextRetry time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, pa := range m.paymentAttempts {
		if pa.ID == attemptID {
			m.paymentAttempts[i].AttemptNumber++
			m.paymentAttempts[i].NextRetryAt = &nextRetry
			break
		}
	}
	return nil
}

func (m *mockDB) ListSubscriptionsByOrg(_ context.Context, orgID uuid.UUID) ([]store.Subscription, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]store.Subscription, 0)
	for _, sub := range m.subscriptions {
		if sub.OrganizationID == orgID {
			out = append(out, *sub)
		}
	}
	return out, nil
}

func (m *mockDB) ListPaymentAttemptsByOrg(_ context.Context, orgID uuid.UUID, limit int) ([]store.PaymentAttempt, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]store.PaymentAttempt, 0)
	for _, pa := range m.paymentAttempts {
		if pa.OrganizationID == orgID {
			out = append(out, pa)
		}
	}
	// Mirror Postgres impl: newest first.
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *mockDB) GetOrgByID(_ context.Context, orgID uuid.UUID) (*store.Organization, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, pa := range m.paymentAttempts {
		if pa.OrganizationID == orgID {
			return &store.Organization{ID: orgID, Name: "Test Org", Email: "test@test.com"}, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockDB) SetOrganizationTrialEndsAt(_ context.Context, orgID uuid.UUID, endsAt *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trialEndsAt[orgID] = endsAt
	return nil
}

func (m *mockDB) ListExpiredTrials(_ context.Context, limit int) ([]store.Organization, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now()
	out := make([]store.Organization, 0)
	for id, endsAt := range m.trialEndsAt {
		if endsAt != nil && !endsAt.After(now) {
			out = append(out, store.Organization{ID: id})
		}
	}
	return out, nil
}

func (m *mockDB) ListOrganizationIDs(_ context.Context, limit int) ([]uuid.UUID, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	seen := map[uuid.UUID]bool{}
	var out []uuid.UUID
	for _, sub := range m.subscriptions {
		if !seen[sub.OrganizationID] {
			seen[sub.OrganizationID] = true
			out = append(out, sub.OrganizationID)
		}
	}
	return out, nil
}

func (m *mockDB) Close() error   { return nil }
func (m *mockDB) Migrate() error { return nil }

func (m *mockDB) CreateCoupon(_ context.Context, c *store.Coupon) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.coupons[c.Code] = c
	return nil
}

func (m *mockDB) GetCouponByCode(_ context.Context, code string) (*store.Coupon, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.coupons[code]
	if !ok {
		return nil, store.ErrNotFound
	}
	return c, nil
}

func (m *mockDB) CountOrgCouponRedemptions(_ context.Context, couponID, orgID uuid.UUID) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if orgs, ok := m.redemptions[couponID]; ok {
		return orgs[orgID], nil
	}
	return 0, nil
}

func (m *mockDB) RedeemCoupon(_ context.Context, couponID, orgID uuid.UUID, invoiceID *uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var coupon *store.Coupon
	for _, c := range m.coupons {
		if c.ID == couponID {
			coupon = c
			break
		}
	}
	if coupon == nil || !coupon.Active || (coupon.ExpiresAt != nil && coupon.ExpiresAt.Before(time.Now())) {
		return store.ErrCouponExhausted
	}
	if coupon.MaxRedemptions != nil && coupon.RedeemedCount >= *coupon.MaxRedemptions {
		return store.ErrCouponExhausted
	}
	coupon.RedeemedCount++
	if m.redemptions[couponID] == nil {
		m.redemptions[couponID] = make(map[uuid.UUID]int64)
	}
	m.redemptions[couponID][orgID]++
	_ = invoiceID
	return nil
}

func newTestService() *Service {
	db := newMockDB()
	logger, _ := zap.NewDevelopment()
	svc := NewService(db, nil, nil, nil)
	svc.SetLogger(logger)
	return svc
}

// --- NewService ---

func TestNewService(t *testing.T) {
	db := newMockDB()
	svc := NewService(db, nil, nil, nil)
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
	if svc.db == nil {
		t.Fatal("db not set")
	}
	if svc.db == nil {
		t.Error("db is nil")
	}
}

// --- Webhook processing ---

func TestProcessWebhookStripeInvoicePaid(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	// Insert a subscription and invoice first so the webhook handler has something to update.
	sub := &store.Subscription{
		ID:               uuid.New(),
		OrganizationID:   uuid.New(),
		Provider:         "stripe",
		ProviderSubID:    "sub_123",
		PlanID:           "pro",
		Status:           "active",
		CurrentPeriodEnd: time.Now().Add(30 * 24 * time.Hour),
	}
	if err := svc.db.InsertSubscription(ctx, sub); err != nil {
		t.Fatal(err)
	}

	invID := "invo_test123"
	inv := &store.Invoice{
		ID:             uuid.New(),
		OrganizationID: sub.OrganizationID,
		Provider:       "stripe",
		ProviderInvID:  &invID,
		AmountCents:    2900,
		Currency:       "usd",
		Status:         "pending",
		IssuedAt:       time.Now(),
	}
	if err := svc.db.InsertInvoice(ctx, inv); err != nil {
		t.Fatal(err)
	}

	payload := json.RawMessage(fmt.Sprintf(`{"id":"evt_1","type":"invoice.paid","data":{"id":"%s"}}`, invID))
	if err := svc.ProcessWebhook(ctx, "stripe", "evt_1", payload); err != nil {
		t.Fatalf("ProcessWebhook failed: %v", err)
	}

	// Invoice should now be marked as paid
	got, err := svc.db.GetInvoiceByID(ctx, inv.ID)
	if err != nil {
		t.Fatalf("GetInvoiceByID: %v", err)
	}
	if got.Status != "paid" {
		t.Errorf("expected paid, got %s", got.Status)
	}
}

func TestProcessWebhookStripeSubscriptionDeleted(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	sub := &store.Subscription{
		ID:               uuid.New(),
		OrganizationID:   uuid.New(),
		Provider:         "stripe",
		ProviderSubID:    "sub_del1",
		PlanID:           "pro",
		Status:           "active",
		CurrentPeriodEnd: time.Now().Add(30 * 24 * time.Hour),
	}
	_ = svc.db.InsertSubscription(ctx, sub)

	payload := json.RawMessage(`{"id":"evt_del","type":"customer.subscription.deleted","data":{"id":"sub_del1"}}`)
	if err := svc.ProcessWebhook(ctx, "stripe", "evt_del", payload); err != nil {
		t.Fatalf("ProcessWebhook: %v", err)
	}

	got, err := svc.db.GetSubscriptionByProviderSubID(ctx, "sub_del1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "canceled" {
		t.Errorf("expected canceled, got %s", got.Status)
	}
}

func TestProcessWebhookMercadoPagoApproved(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	invID := "mp_inv_1"
	inv := &store.Invoice{
		ID:             uuid.New(),
		OrganizationID: uuid.New(),
		Provider:       "mercado_pago",
		ProviderInvID:  &invID,
		AmountCents:    1000,
		Currency:       "brl",
		Status:         "pending",
		IssuedAt:       time.Now(),
	}
	_ = svc.db.InsertInvoice(ctx, inv)

	payload := json.RawMessage(fmt.Sprintf(`{"id":"mp_evt_1","type":"payment.updated","data":{"id":"p_123","status_code":200,"invoice_id":"%s"}}`, invID))
	if err := svc.ProcessWebhook(ctx, "mercado_pago", "mp_evt_1", payload); err != nil {
		t.Fatalf("ProcessWebhook MP: %v", err)
	}

	got, _ := svc.db.GetInvoiceByID(ctx, inv.ID)
	if got.Status != "paid" {
		t.Errorf("expected paid, got %s", got.Status)
	}
}

func TestProcessWebhookCryptoConfirmed(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	provInvID := "cb_charge_1"
	inv := &store.Invoice{
		ID:             uuid.New(),
		OrganizationID: uuid.New(),
		Provider:       "crypto",
		ProviderInvID:  &provInvID,
		AmountCents:    5000,
		Currency:       "btc",
		Status:         "pending",
		IssuedAt:       time.Now(),
	}
	_ = svc.db.InsertInvoice(ctx, inv)

	payload := json.RawMessage(`{"id":"cb_evt_1","type":"charge:confirmed","charge_id":"cb_charge_1"}`)
	if err := svc.ProcessWebhook(ctx, "crypto", "cb_evt_1", payload); err != nil {
		t.Fatalf("ProcessWebhook crypto: %v", err)
	}

	got, _ := svc.db.GetInvoiceByID(ctx, inv.ID)
	if got.Status != "paid" {
		t.Errorf("expected paid, got %s", got.Status)
	}
}

func TestProcessWebhookUnknownProvider(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	err := svc.ProcessWebhook(ctx, "unknown_provider", "evt_x", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}
}

func TestProcessWebhookInvalidJSON(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	err := svc.ProcessWebhook(ctx, "stripe", "evt_bad", []byte(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestProcessWebhookIdempotency(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	// First call processes
	err := svc.ProcessWebhook(ctx, "stripe", "evt_dup", []byte(`{"type":"test"}`))
	if err != nil {
		t.Fatalf("first process: %v", err)
	}

	// Second call must be idempotent — no error
	err = svc.ProcessWebhook(ctx, "stripe", "evt_dup", []byte(`{"type":"test"}`))
	if err != nil {
		t.Fatalf("second process should be idempotent: %v", err)
	}

	// Verify only one event stored
	we, err := svc.db.GetWebhookEvent(ctx, "evt_dup")
	if err != nil {
		t.Fatal(err)
	}
	if we == nil {
		t.Fatal("expected webhook event to be persisted")
	}
}

// --- Proration ---

func TestCalculateProrationUpgrade(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	// Use a period in the future so daysRemaining > 0
	start := time.Now().Add(10 * 24 * time.Hour)
	end := start.Add(30 * 24 * time.Hour)

	sub := &store.Subscription{
		ID:                 uuid.New(),
		OrganizationID:     uuid.New(),
		PlanID:             "free",
		CurrentPeriodStart: start,
		CurrentPeriodEnd:   end,
	}

	proration, err := svc.CalculateProration(ctx, sub, "pro")
	if err != nil {
		t.Fatalf("CalculateProration upgrade: %v", err)
	}
	if proration <= 0 {
		t.Errorf("expected positive proration for upgrade, got %d", proration)
	}
}

func TestCalculateProrationDowngrade(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	start := time.Now().Add(10 * 24 * time.Hour)
	end := start.Add(30 * 24 * time.Hour)

	sub := &store.Subscription{
		ID:                 uuid.New(),
		OrganizationID:     uuid.New(),
		PlanID:             "pro",
		CurrentPeriodStart: start,
		CurrentPeriodEnd:   end,
	}

	proration, err := svc.CalculateProration(ctx, sub, "free")
	if err != nil {
		t.Fatalf("CalculateProration downgrade: %v", err)
	}
	if proration >= 0 {
		t.Errorf("expected negative credit amount for downgrade, got %d", proration)
	}
}

func TestCalculateProrationSamePlan(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	sub := &store.Subscription{
		ID:                 uuid.New(),
		OrganizationID:     uuid.New(),
		PlanID:             "pro",
		CurrentPeriodStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		CurrentPeriodEnd:   time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}

	proration, err := svc.CalculateProration(ctx, sub, "pro")
	if err != nil {
		t.Fatalf("CalculateProration same plan: %v", err)
	}
	if proration != 0 {
		t.Errorf("expected 0 proration for same plan, got %d", proration)
	}
}

func TestCalculateProrationUnknownPlan(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	sub := &store.Subscription{
		ID:                 uuid.New(),
		OrganizationID:     uuid.New(),
		PlanID:             "free",
		CurrentPeriodStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		CurrentPeriodEnd:   time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
	}

	_, err := svc.CalculateProration(ctx, sub, "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown plan")
	}
}

func TestCalculateProrationInvalidPeriod(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	sub := &store.Subscription{
		ID:                 uuid.New(),
		OrganizationID:     uuid.New(),
		PlanID:             "free",
		CurrentPeriodStart: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), // end before start
		CurrentPeriodEnd:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	_, err := svc.CalculateProration(ctx, sub, "pro")
	if err == nil {
		t.Fatal("expected error for invalid period (end before start)")
	}
}

// --- CreateSubscription ---

// mockProvider is a no-op PaymentProvider for testing.
type mockProvider struct{}

func (m *mockProvider) CreateCheckoutSession(_ context.Context, _ uuid.UUID, _ string, _ string) (string, error) {
	return "mock_session", nil
}
func (m *mockProvider) CreateSubscription(_ context.Context, _ uuid.UUID, planID string, _ string) (string, error) {
	return "mock_sub_" + planID, nil
}
func (m *mockProvider) CancelSubscription(_ context.Context, _ string) error           { return nil }
func (m *mockProvider) UpdateSubscription(_ context.Context, _ string, _ string) error { return nil }
func (m *mockProvider) GetSubscription(_ context.Context, id string) (*SubscriptionInfo, error) {
	return &SubscriptionInfo{ID: id, Status: "active"}, nil
}
func (m *mockProvider) VerifyWebhook(_ []byte, _ []byte, _ string) (map[string]interface{}, error) {
	return nil, nil
}

func TestCreateSubscriptionStripe(t *testing.T) {
	db := newMockDB()
	svc := NewService(db, &mockProvider{}, &mockProvider{}, &mockProvider{})
	svc.SetLogger(zap.NewNop())
	ctx := context.Background()

	orgID := uuid.New()
	subID, err := svc.CreateSubscription(ctx, orgID, "stripe", "pro")
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if subID == "" {
		t.Error("expected non-empty subscription ID")
	}

	got, err := db.GetSubscriptionByProviderSubID(ctx, subID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PlanID != "pro" {
		t.Errorf("expected plan pro, got %s", got.PlanID)
	}
	if got.Status != "active" {
		t.Errorf("expected active, got %s", got.Status)
	}
}

func TestCreateSubscriptionUnknownProvider(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, err := svc.CreateSubscription(ctx, uuid.New(), "unknown", "pro")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

// --- GetSubscription ---

func TestGetSubscriptionLocalDB(t *testing.T) {
	db := newMockDB()
	svc := NewService(db, &mockProvider{}, &mockProvider{}, &mockProvider{})
	svc.SetLogger(zap.NewNop())
	ctx := context.Background()

	orgID := uuid.New()
	subID, _ := svc.CreateSubscription(ctx, orgID, "stripe", "pro")

	sub, err := svc.GetSubscription(ctx, subID)
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if sub.PlanID != "pro" {
		t.Errorf("expected plan pro, got %s", sub.PlanID)
	}
}

// --- Invoice ---

func TestGenerateInvoice(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	orgID := uuid.New()
	inv, err := svc.GenerateInvoice(ctx, orgID, "sub_1", 2900, "usd")
	if err != nil {
		t.Fatalf("GenerateInvoice: %v", err)
	}
	if inv == nil {
		t.Fatal("expected non-nil invoice")
	}
	if inv.Status != "pending" {
		t.Errorf("expected pending, got %s", inv.Status)
	}
	if inv.AmountCents != 2900 {
		t.Errorf("expected 2900, got %d", inv.AmountCents)
	}
}

// --- Dunning ---

func TestInitiateDunning(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	orgID := uuid.New()
	err := svc.InitiateDunning(ctx, orgID, "")
	if err != nil {
		t.Fatalf("InitiateDunning: %v", err)
	}
}

// --- Usage ---

func TestRecordUsageIdempotentDedupe(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	orgID := uuid.New()
	start := time.Now()
	end := start.Add(30 * 24 * time.Hour)

	deduped, err := svc.RecordUsageIdempotent(ctx, orgID, "crashes", 5, start, end, "key-1")
	if err != nil || deduped {
		t.Fatalf("first ingest: deduped=%v err=%v, want false/nil", deduped, err)
	}
	deduped, err = svc.RecordUsageIdempotent(ctx, orgID, "crashes", 5, start, end, "key-1")
	if err != nil || !deduped {
		t.Fatalf("replay: deduped=%v err=%v, want true/nil", deduped, err)
	}
	// Same key, different metric: independent (no false dedupe).
	if deduped, err := svc.RecordUsageIdempotent(ctx, orgID, "devices", 5, start, end, "key-1"); err != nil || deduped {
		t.Fatalf("other metric: deduped=%v err=%v, want false/nil", deduped, err)
	}
	// Empty key: legacy append-only, never deduped.
	if deduped, err := svc.RecordUsageIdempotent(ctx, orgID, "crashes", 5, start, end, ""); err != nil || deduped {
		t.Fatalf("empty key: deduped=%v err=%v, want false/nil", deduped, err)
	}
}

func TestGenerateUsageInvoiceIdempotentPerPeriod(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	orgID := uuid.New()
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)

	first, _, err := svc.GenerateUsageInvoice(ctx, orgID, "sub_1", "team", "usd", start, end)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, _, err := svc.GenerateUsageInvoice(ctx, orgID, "sub_1", "team", "usd", start, end)
	if err != nil && err != store.ErrUsageBillingDuplicate {
		t.Fatalf("replay err = %v", err)
	}
	if second == nil || second.ID != first.ID {
		t.Fatalf("replay must return first invoice %v, got %v", first.ID, second)
	}
}

func TestRecordUsage(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	orgID := uuid.New()
	start := time.Now()
	end := start.Add(30 * 24 * time.Hour)

	err := svc.RecordUsage(ctx, orgID, "crashes", 5, start, end)
	if err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}

	total, err := svc.GetUsage(ctx, orgID, "crashes", start, end)
	if err != nil {
		t.Fatalf("GetUsage: %v", err)
	}
	if total != 5 {
		t.Errorf("expected 5, got %d", total)
	}
}

// --- ListInvoices ---

func TestListInvoices(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	orgID := uuid.New()
	for i := 0; i < 3; i++ {
		_, err := svc.GenerateInvoice(ctx, orgID, "sub_1", 2900, "usd")
		if err != nil {
			t.Fatal(err)
		}
	}

	invoices, total, err := svc.ListInvoices(ctx, orgID, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Errorf("expected 3 total, got %d", total)
	}
	if len(invoices) != 3 {
		t.Errorf("expected 3 invoices, got %d", len(invoices))
	}
}

// --- VerifyWebhook ---

func TestVerifyWebhookEmptySecret(t *testing.T) {
	svc := newTestService()
	_, err := svc.VerifyWebhook([]byte("payload"), []byte("sig"), "")
	if err == nil {
		t.Fatal("expected error for empty secret")
	}
}

// --- strToUUID helper ---

func TestStrToUUID(t *testing.T) {
	// Empty string → nil
	if got := strToUUID(""); got != nil {
		t.Errorf("expected nil for empty string, got %v", got)
	}

	// Valid UUID
	uuidStr := "550e8400-e29b-41d4-a716-446655440000"
	got := strToUUID(uuidStr)
	if got == nil {
		t.Fatal("expected non-nil for valid UUID")
	}

	// Invalid UUID → nil
	if got := strToUUID("not-a-uuid"); got != nil {
		t.Errorf("expected nil for invalid UUID, got %v", got)
	}
}

// --- Ensure test helpers work ---

func TestNewTestServiceNotNil(t *testing.T) {
	svc := newTestService()
	if svc == nil {
		t.Fatal("newTestService returned nil")
	}
	if svc.db == nil {
		t.Fatal("db not initialized")
	}
	if svc.logger == nil {
		t.Fatal("logger not initialized")
	}
}

// --- MercadoPago & Crypto Provider Tests ---

func TestMercadoPagoProviderFlows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"mp_sub_123","status":"authorized","reason":"LastState team","next_payment_date":"2026-10-01T00:00:00Z","auto_recurring":{"currency_id":"USD"}}`))
	}))
	defer srv.Close()

	mp := NewMercadoPagoProvider("k")
	mp.baseURL = srv.URL
	mp.client = srv.Client()
	ctx := context.Background()

	// GetSubscription
	subInfo, err := mp.GetSubscription(ctx, "mp_sub_123")
	if err != nil {
		t.Fatalf("MP GetSubscription: %v", err)
	}
	if subInfo.ID != "mp_sub_123" || subInfo.Status != "active" {
		t.Errorf("MP unexpected subscription info: %+v", subInfo)
	}

	// UpdateSubscription
	if err := mp.UpdateSubscription(ctx, "mp_sub_123", "team"); err != nil {
		t.Fatalf("MP UpdateSubscription: %v", err)
	}

	// CancelSubscription
	if err := mp.CancelSubscription(ctx, "mp_sub_123"); err != nil {
		t.Fatalf("MP CancelSubscription: %v", err)
	}

	// Empty ID checks
	if _, err := mp.GetSubscription(ctx, ""); err == nil {
		t.Fatal("expected error on empty sub ID")
	}
	if err := mp.CancelSubscription(ctx, ""); err == nil {
		t.Fatal("expected error on empty sub ID")
	}
	if err := mp.UpdateSubscription(ctx, "", "team"); err == nil {
		t.Fatal("expected error on empty sub ID")
	}

	// Not-configured provider fails closed
	bare := NewMercadoPagoProvider("")
	if _, err := bare.GetSubscription(ctx, "mp_sub_123"); err == nil {
		t.Fatal("expected error without api key")
	}
	if err := bare.UpdateSubscription(ctx, "mp_sub_123", "team"); err == nil {
		t.Fatal("expected error without api key")
	}
	if err := bare.CancelSubscription(ctx, "mp_sub_123"); err == nil {
		t.Fatal("expected error without api key")
	}
}

func TestCryptoProviderFlows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"crypto_sub_123","timeline":[{"status":"COMPLETED","time":"2026-09-01T00:00:00Z"}],"expires_at":"2026-10-01T00:00:00Z"}}`))
	}))
	defer srv.Close()

	cp := NewCryptoProvider("k")
	cp.baseURL = srv.URL
	cp.client = srv.Client()
	ctx := context.Background()

	// GetSubscription
	subInfo, err := cp.GetSubscription(ctx, "crypto_sub_123")
	if err != nil {
		t.Fatalf("Crypto GetSubscription: %v", err)
	}
	if subInfo.ID != "crypto_sub_123" || subInfo.Status != "active" {
		t.Errorf("Crypto unexpected subscription info: %+v", subInfo)
	}

	// UpdateSubscription
	if err := cp.UpdateSubscription(ctx, "crypto_sub_123", "enterprise"); err != nil {
		t.Fatalf("Crypto UpdateSubscription: %v", err)
	}

	// CancelSubscription
	if err := cp.CancelSubscription(ctx, "crypto_sub_123"); err != nil {
		t.Fatalf("Crypto CancelSubscription: %v", err)
	}

	// Empty ID checks
	if _, err := cp.GetSubscription(ctx, ""); err == nil {
		t.Fatal("expected error on empty sub ID")
	}
	if err := cp.CancelSubscription(ctx, ""); err == nil {
		t.Fatal("expected error on empty sub ID")
	}
	if err := cp.UpdateSubscription(ctx, "", "enterprise"); err == nil {
		t.Fatal("expected error on empty sub ID")
	}

	// Not-configured provider fails closed
	bare := NewCryptoProvider("")
	if _, err := bare.GetSubscription(ctx, "crypto_sub_123"); err == nil {
		t.Fatal("expected error without api key")
	}
	if err := bare.UpdateSubscription(ctx, "crypto_sub_123", "enterprise"); err == nil {
		t.Fatal("expected error without api key")
	}
	if err := bare.CancelSubscription(ctx, "crypto_sub_123"); err == nil {
		t.Fatal("expected error without api key")
	}
}

func TestServiceUpgradePlanMercadoPagoAndCrypto(t *testing.T) {
	db := newMockDB()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	mpProvider := NewMercadoPagoProvider("k")
	mpProvider.baseURL = srv.URL
	mpProvider.client = srv.Client()
	cryptoProvider := NewCryptoProvider("k")
	cryptoProvider.baseURL = srv.URL
	cryptoProvider.client = srv.Client()
	svc := NewService(db, &mockProvider{}, mpProvider, cryptoProvider)
	svc.SetLogger(zap.NewNop())
	ctx := context.Background()

	orgID := uuid.New()
	mpSub := &store.Subscription{
		ID:                 uuid.New(),
		OrganizationID:     orgID,
		Provider:           "mercado_pago",
		ProviderSubID:      "mp_sub_456",
		PlanID:             "free",
		Status:             "active",
		CurrentPeriodStart: time.Now(),
		CurrentPeriodEnd:   time.Now().Add(30 * 24 * time.Hour),
	}
	_ = db.InsertSubscription(ctx, mpSub)

	cryptoSub := &store.Subscription{
		ID:                 uuid.New(),
		OrganizationID:     orgID,
		Provider:           "crypto",
		ProviderSubID:      "crypto_sub_789",
		PlanID:             "free",
		Status:             "active",
		CurrentPeriodStart: time.Now(),
		CurrentPeriodEnd:   time.Now().Add(30 * 24 * time.Hour),
	}
	_ = db.InsertSubscription(ctx, cryptoSub)

	// Upgrade MP
	if err := svc.UpgradePlan(ctx, "mp_sub_456", "pro"); err != nil {
		t.Fatalf("UpgradePlan MP: %v", err)
	}

	// Upgrade Crypto
	if err := svc.UpgradePlan(ctx, "crypto_sub_789", "pro"); err != nil {
		t.Fatalf("UpgradePlan Crypto: %v", err)
	}

	// Cancel MP
	if err := svc.CancelSubscription(ctx, "mp_sub_456"); err != nil {
		t.Fatalf("CancelSubscription MP: %v", err)
	}

	// Cancel Crypto
	if err := svc.CancelSubscription(ctx, "crypto_sub_789"); err != nil {
		t.Fatalf("CancelSubscription Crypto: %v", err)
	}
}
