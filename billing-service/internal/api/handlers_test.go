package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/laststate/billing-service/internal/billing"
	"github.com/laststate/billing-service/internal/entitlement"
	"github.com/laststate/billing-service/internal/store"
	"github.com/laststate/billing-service/internal/webhook"
)

// handlersMockDB is a minimal in-memory store for handler tests.
// It embeds store.DB so the interface is satisfied without implementing
// every method; only the ones used by billing.Service in handler paths
// are overridden.
type handlersMockDB struct {
	store.DB
	mu            sync.RWMutex
	subscriptions map[string]*store.Subscription
	invoices      map[uuid.UUID]*store.Invoice
	plans         map[string]*store.Plan
	usage         []store.UsageMetric
	usageKeys     map[string]bool
	usageRuns     map[string]uuid.UUID
	coupons       map[string]*store.Coupon
}

func newHandlersMockDB() *handlersMockDB {
	return &handlersMockDB{
		subscriptions: make(map[string]*store.Subscription),
		invoices:      make(map[uuid.UUID]*store.Invoice),
		plans: map[string]*store.Plan{
			"free": {ID: "free", Name: "Free", PriceCents: 0, Currency: "usd", Interval: "month"},
			"pro":  {ID: "pro", Name: "Pro", PriceCents: 2900, Currency: "usd", Interval: "month"},
		},
		usageKeys: make(map[string]bool),
		usageRuns: make(map[string]uuid.UUID),
	}
}

func (m *handlersMockDB) InsertSubscription(_ context.Context, sub *store.Subscription) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := *sub
	m.subscriptions[c.ProviderSubID] = &c
	return nil
}

func (m *handlersMockDB) GetSubscriptionByProviderSubID(_ context.Context, id string) (*store.Subscription, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sub, ok := m.subscriptions[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return sub, nil
}

func (m *handlersMockDB) UpdateSubscriptionStatus(_ context.Context, id, status string, periodEnd time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sub, ok := m.subscriptions[id]; ok {
		sub.Status = status
		if !periodEnd.IsZero() {
			sub.CurrentPeriodEnd = periodEnd
		}
	}
	return nil
}

func (m *handlersMockDB) UpdateSubscriptionPlan(_ context.Context, id uuid.UUID, planID string, proration int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sub := range m.subscriptions {
		if sub.ID == id {
			sub.PlanID = planID
			sub.LastProrationCents = proration
		}
	}
	return nil
}

func (m *handlersMockDB) GetPlanByID(_ context.Context, planID string) (*store.Plan, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.plans[planID]
	if !ok {
		return nil, store.ErrNotFound
	}
	return p, nil
}

func (m *handlersMockDB) InsertInvoice(_ context.Context, inv *store.Invoice) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := *inv
	m.invoices[c.ID] = &c
	return nil
}

func (m *handlersMockDB) GetInvoiceByID(_ context.Context, id uuid.UUID) (*store.Invoice, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	inv, ok := m.invoices[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return inv, nil
}

func (m *handlersMockDB) ListInvoices(_ context.Context, orgID uuid.UUID, _page, _perPage int) ([]store.Invoice, int64, error) {
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

func (m *handlersMockDB) InsertUsageMetric(_ context.Context, u *store.UsageMetric) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u.IdempotencyKey != "" {
		k := u.OrganizationID.String() + "|" + u.MetricName + "|" + u.IdempotencyKey
		if m.usageKeys[k] {
			return store.ErrUsageDuplicate
		}
		m.usageKeys[k] = true
	}
	c := *u
	m.usage = append(m.usage, c)
	return nil
}

func (m *handlersMockDB) TryClaimUsageBillingRun(_ context.Context, orgID uuid.UUID, subID string, start, end time.Time, _ uuid.UUID) (bool, error) {
	return true, nil
}

func (m *handlersMockDB) GetUsageBillingInvoice(_ context.Context, orgID uuid.UUID, subID string, start, end time.Time) (uuid.UUID, error) {
	return uuid.Nil, store.ErrNotFound
}

func (m *handlersMockDB) ListUsageMetrics(_ context.Context, _ uuid.UUID, _ string) ([]store.UsageMetric, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]store.UsageMetric, len(m.usage))
	copy(out, m.usage)
	return out, nil
}

func (m *handlersMockDB) GetUsageTotal(_ context.Context, _ uuid.UUID, _ string, _, _ time.Time) (int64, error) {
	return 0, nil
}

func (m *handlersMockDB) InsertPaymentAttempt(_ context.Context, _ *store.PaymentAttempt) error {
	return nil
}

func (m *handlersMockDB) GetWebhookEvent(_ context.Context, _ string) (*store.WebhookEvent, error) {
	return nil, nil
}

func (m *handlersMockDB) InsertWebhookEvent(_ context.Context, _ *store.WebhookEvent) error {
	return nil
}

// handlersMockProvider implements billing.PaymentProvider via embedding.
type handlersMockProvider struct {
	billing.PaymentProvider
}

func (p *handlersMockProvider) CreateSubscription(_ context.Context, _ uuid.UUID, planID string, _ string) (string, error) {
	return "mock_sub_" + planID, nil
}

func (p *handlersMockProvider) CreateCheckoutSession(_ context.Context, _ uuid.UUID, planID string, _ string) (string, error) {
	return "https://checkout.mock/session/" + planID, nil
}

func (m *handlersMockDB) GetOrgByID(_ context.Context, orgID uuid.UUID) (*store.Organization, error) {
	return &store.Organization{ID: orgID, Name: "Test Org", Email: "test@test.com"}, nil
}

func (p *handlersMockProvider) CancelSubscription(_ context.Context, _ string) error {
	return nil
}

func (p *handlersMockProvider) UpdateSubscription(_ context.Context, _ string, _ string) error {
	return nil
}

func (p *handlersMockProvider) GetSubscription(_ context.Context, id string) (*billing.SubscriptionInfo, error) {
	return &billing.SubscriptionInfo{ID: id, Status: "active"}, nil
}

func (p *handlersMockProvider) VerifyWebhook(_ []byte, _ []byte, _ string) (map[string]interface{}, error) {
	return nil, nil
}

func newBillingWithDB(db store.DB) *billing.Service {
	svc := billing.NewService(db, &handlersMockProvider{}, &handlersMockProvider{}, &handlersMockProvider{})
	svc.SetLogger(zap.NewNop())
	return svc
}

func newHandlersBillingService() *billing.Service {
	return newBillingWithDB(newHandlersMockDB())
}

func newHandlersEntitlementService(t *testing.T) *entitlement.Service {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	svc := entitlement.NewService(srv.URL)
	svc.SetLogger(zap.NewNop())
	return svc
}

func doRequest(t *testing.T, h http.HandlerFunc, method, path string, body []byte, vars ...map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if len(vars) > 0 && len(vars[0]) > 0 {
		req = mux.SetURLVars(req, vars[0])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// --- Subscriptions ---

func TestCreateSubscriptionHandler(t *testing.T) {
	h := createSubscription(newHandlersBillingService())
	body, _ := json.Marshal(map[string]string{
		"organization_id": uuid.New().String(),
		"provider":        "stripe",
		"plan_id":         "pro",
	})
	rec := doRequest(t, h, http.MethodPost, "/v1/subscriptions", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got status %d, want 201: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateSubscriptionHandlerBadBody(t *testing.T) {
	h := createSubscription(newHandlersBillingService())
	rec := doRequest(t, h, http.MethodPost, "/v1/subscriptions", []byte(`{invalid`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
}

func TestCreateSubscriptionHandlerBadOrgID(t *testing.T) {
	h := createSubscription(newHandlersBillingService())
	body, _ := json.Marshal(map[string]string{
		"organization_id": "not-a-uuid",
		"provider":        "stripe",
		"plan_id":         "pro",
	})
	rec := doRequest(t, h, http.MethodPost, "/v1/subscriptions", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
}

// --- Hosted checkout ---

func TestCreateCheckoutHandlerPassthrough(t *testing.T) {
	h := createCheckout(newHandlersBillingService())
	body, _ := json.Marshal(map[string]string{
		"organization_id": uuid.New().String(),
		"provider":        "mp",
		"plan":            "pilot",
		"currency":        "usd",
	})
	rec := doRequest(t, h, http.MethodPost, "/v1/billing/checkout", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got status %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["checkout_url"] == "" {
		t.Fatalf("expected checkout_url, got %v", resp)
	}
}

func TestCreateCheckoutHandlerBadBody(t *testing.T) {
	h := createCheckout(newHandlersBillingService())
	rec := doRequest(t, h, http.MethodPost, "/v1/billing/checkout", []byte(`{invalid`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
}

func TestCreateCheckoutHandlerBadOrgID(t *testing.T) {
	h := createCheckout(newHandlersBillingService())
	body, _ := json.Marshal(map[string]string{
		"organization_id": "not-a-uuid",
		"plan":            "poc",
	})
	rec := doRequest(t, h, http.MethodPost, "/v1/billing/checkout", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
}

func TestCreateCheckoutHandlerUnknownPlan(t *testing.T) {
	h := createCheckout(newHandlersBillingService())
	body, _ := json.Marshal(map[string]string{
		"organization_id": uuid.New().String(),
		"plan":            "hobby",
	})
	rec := doRequest(t, h, http.MethodPost, "/v1/billing/checkout", body)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("got status %d, want 502", rec.Code)
	}
}

func TestCreateCheckoutHandlerMissingPrice(t *testing.T) {
	t.Setenv("BILLING_PRICE_PILOT", "")
	h := createCheckout(newHandlersBillingService())
	body, _ := json.Marshal(map[string]string{
		"organization_id": uuid.New().String(),
		"provider":        "stripe",
		"plan":            "pilot",
	})
	rec := doRequest(t, h, http.MethodPost, "/v1/billing/checkout", body)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("got status %d, want 502", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "BILLING_PRICE_PILOT") {
		t.Fatalf("expected error to name BILLING_PRICE_PILOT: %s", rec.Body.String())
	}
}

func TestGetSubscriptionHandler(t *testing.T) {
	db := newHandlersMockDB()
	svc := newBillingWithDB(db)
	ctx := context.Background()
	subID, err := svc.CreateSubscription(ctx, uuid.New(), "stripe", "pro")
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}

	rec := doRequest(t, getSubscription(svc), http.MethodGet, "/v1/subscriptions/"+subID, nil, map[string]string{"id": subID})
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got billing.SubscriptionInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("bad response body: %v", err)
	}
	if got.PlanID != "pro" {
		t.Errorf("plan_id = %q, want pro", got.PlanID)
	}
}

func TestGetSubscriptionHandlerError(t *testing.T) {
	svc := newHandlersBillingService()
	rec := doRequest(t, getSubscription(svc), http.MethodGet, "/v1/subscriptions/missing", nil, map[string]string{"id": "missing"})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d, want 500", rec.Code)
	}
}

func TestCancelSubscriptionHandler(t *testing.T) {
	db := newHandlersMockDB()
	svc := newBillingWithDB(db)
	ctx := context.Background()
	subID, err := svc.CreateSubscription(ctx, uuid.New(), "stripe", "pro")
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}

	rec := doRequest(t, cancelSubscription(svc), http.MethodDelete, "/v1/subscriptions/"+subID, nil, map[string]string{"id": subID})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("got status %d, want 204", rec.Code)
	}
}

func TestCancelSubscriptionHandlerNotFound(t *testing.T) {
	svc := newHandlersBillingService()
	rec := doRequest(t, cancelSubscription(svc), http.MethodDelete, "/v1/subscriptions/missing", nil, map[string]string{"id": "missing"})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d, want 500", rec.Code)
	}
}

func TestUpgradePlanHandler(t *testing.T) {
	db := newHandlersMockDB()
	svc := newBillingWithDB(db)
	ctx := context.Background()
	subID, err := svc.CreateSubscription(ctx, uuid.New(), "stripe", "free")
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"plan_id": "pro"})
	rec := doRequest(t, upgradePlan(svc), http.MethodPost, "/v1/subscriptions/"+subID+"/upgrade", body, map[string]string{"id": subID})
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestUpgradePlanHandlerBadBody(t *testing.T) {
	svc := newHandlersBillingService()
	rec := doRequest(t, upgradePlan(svc), http.MethodPost, "/v1/subscriptions/x/upgrade", []byte(`{invalid`), map[string]string{"id": "x"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
}

// --- Invoices ---

func TestListInvoicesHandler(t *testing.T) {
	db := newHandlersMockDB()
	svc := newBillingWithDB(db)
	ctx := context.Background()
	orgID := uuid.New()
	for i := 0; i < 2; i++ {
		if _, err := svc.GenerateInvoice(ctx, orgID, "sub_1", 2900, "usd"); err != nil {
			t.Fatalf("GenerateInvoice: %v", err)
		}
	}

	rec := doRequest(t, listInvoices(svc), http.MethodGet, "/v1/invoices?organization_id="+orgID.String(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}
	var resp struct {
		Invoices []store.Invoice `json:"invoices"`
		Total    int64           `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad response: %v", err)
	}
	if resp.Total != 2 || len(resp.Invoices) != 2 {
		t.Errorf("total = %d, len = %d; want 2/2", resp.Total, len(resp.Invoices))
	}
}

func TestListInvoicesHandlerBadOrgID(t *testing.T) {
	svc := newHandlersBillingService()
	rec := doRequest(t, listInvoices(svc), http.MethodGet, "/v1/invoices?organization_id=zzz", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
}

func TestGetInvoiceHandler(t *testing.T) {
	db := newHandlersMockDB()
	svc := newBillingWithDB(db)
	ctx := context.Background()
	inv, err := svc.GenerateInvoice(ctx, uuid.New(), "sub_1", 2900, "usd")
	if err != nil {
		t.Fatalf("GenerateInvoice: %v", err)
	}

	rec := doRequest(t, getInvoice(svc), http.MethodGet, "/v1/invoices/"+inv.ID.String(), nil, map[string]string{"id": inv.ID.String()})
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}
}

func TestGetInvoiceHandlerNotFound(t *testing.T) {
	svc := newHandlersBillingService()
	rec := doRequest(t, getInvoice(svc), http.MethodGet, "/v1/invoices/"+uuid.New().String(), nil, map[string]string{"id": uuid.New().String()})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got status %d, want 404", rec.Code)
	}
}

func TestGetInvoiceHandlerBadUUID(t *testing.T) {
	svc := newHandlersBillingService()
	rec := doRequest(t, getInvoice(svc), http.MethodGet, "/v1/invoices/not-a-uuid", nil, map[string]string{"id": "not-a-uuid"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
}

// --- Usage ---

func TestRecordUsageHandler(t *testing.T) {
	svc := newHandlersBillingService()
	body, _ := json.Marshal(map[string]interface{}{
		"organization_id": uuid.New().String(),
		"metrics":         map[string]int64{"crashes": 5},
	})
	rec := doRequest(t, recordUsage(svc), http.MethodPost, "/v1/usage", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got status %d, want 201", rec.Code)
	}
}

func TestRecordUsageHandlerIdempotent(t *testing.T) {
	db := newHandlersMockDB()
	svc := newBillingWithDB(db)
	orgID := uuid.New().String()
	body, _ := json.Marshal(map[string]any{
		"organization_id": orgID, "metrics": map[string]int64{"crashes": 5},
		"idempotency_key": "key-1",
	})
	first := doRequest(t, recordUsage(svc), http.MethodPost, "/v1/usage", body)
	if first.Code != http.StatusCreated {
		t.Fatalf("first = %d, want 201: %s", first.Code, first.Body.String())
	}
	second := doRequest(t, recordUsage(svc), http.MethodPost, "/v1/usage", body)
	if second.Code != http.StatusOK {
		t.Fatalf("replay = %d, want 200: %s", second.Code, second.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(second.Body.Bytes(), &payload); err != nil || payload["deduped"] != true {
		t.Fatalf("replay body = %s, want {\"deduped\":true}", second.Body.String())
	}
	if len(db.usage) != 1 {
		t.Fatalf("usage rows = %d, want 1 (no double-bill)", len(db.usage))
	}
}

func TestRecordUsageHandlerBadBody(t *testing.T) {
	svc := newHandlersBillingService()
	rec := doRequest(t, recordUsage(svc), http.MethodPost, "/v1/usage", []byte(`{invalid`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
}

func TestRecordUsageHandlerBadOrgID(t *testing.T) {
	svc := newHandlersBillingService()
	body, _ := json.Marshal(map[string]interface{}{
		"organization_id": "bad",
		"metrics":         map[string]int64{"crashes": 1},
	})
	rec := doRequest(t, recordUsage(svc), http.MethodPost, "/v1/usage", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
}

func TestGetUsageHandler(t *testing.T) {
	svc := newHandlersBillingService()
	rec := doRequest(t, getUsage(svc), http.MethodGet, "/v1/usage?organization_id="+uuid.New().String(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}
}

func TestGetUsageHandlerMissingOrgID(t *testing.T) {
	svc := newHandlersBillingService()
	rec := doRequest(t, getUsage(svc), http.MethodGet, "/v1/usage", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
}

func TestGetUsageHandlerBadOrgID(t *testing.T) {
	svc := newHandlersBillingService()
	rec := doRequest(t, getUsage(svc), http.MethodGet, "/v1/usage?organization_id=bad", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
}

// --- Dunning ---

func TestInitiateDunningHandler(t *testing.T) {
	svc := newHandlersBillingService()
	rec := doRequest(t, initiateDunning(svc), http.MethodPost, "/v1/dunning/"+uuid.New().String()+"/initiate", nil, map[string]string{"org_id": uuid.New().String()})
	if rec.Code != http.StatusCreated {
		t.Fatalf("got status %d, want 201", rec.Code)
	}
}

func TestInitiateDunningHandlerBadOrgID(t *testing.T) {
	svc := newHandlersBillingService()
	rec := doRequest(t, initiateDunning(svc), http.MethodPost, "/v1/dunning/bad/initiate", nil, map[string]string{"org_id": "bad"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
}

// --- Entitlements ---

func TestProvisionEntitlementHandler(t *testing.T) {
	svc := newHandlersEntitlementService(t)
	orgID := uuid.New().String()
	body, _ := json.Marshal(map[string]string{"tier": "pro"})
	rec := doRequest(t, provisionEntitlement(svc), http.MethodPost, "/v1/entitlements/"+orgID+"/provision", body, map[string]string{"org_id": orgID})
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}
}

func TestProvisionEntitlementHandlerBadOrgID(t *testing.T) {
	svc := newHandlersEntitlementService(t)
	body, _ := json.Marshal(map[string]string{"tier": "pro"})
	rec := doRequest(t, provisionEntitlement(svc), http.MethodPost, "/v1/entitlements/bad/provision", body, map[string]string{"org_id": "bad"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
}

func TestProvisionEntitlementHandlerBadBody(t *testing.T) {
	svc := newHandlersEntitlementService(t)
	orgID := uuid.New().String()
	rec := doRequest(t, provisionEntitlement(svc), http.MethodPost, "/v1/entitlements/"+orgID+"/provision", []byte(`{invalid`), map[string]string{"org_id": orgID})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
}

func TestProvisionEntitlementHandlerTraceError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	svc := entitlement.NewService(srv.URL)
	svc.SetLogger(zap.NewNop())

	body, _ := json.Marshal(map[string]string{"tier": "pro"})
	orgID := uuid.New().String()
	rec := doRequest(t, provisionEntitlement(svc), http.MethodPost, "/v1/entitlements/"+orgID+"/provision", body, map[string]string{"org_id": orgID})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d, want 500", rec.Code)
	}
}

func TestDeprovisionEntitlementHandler(t *testing.T) {
	svc := newHandlersEntitlementService(t)
	orgID := uuid.New().String()
	body, _ := json.Marshal(map[string]string{"tier": "free"})
	rec := doRequest(t, deprovisionEntitlement(svc), http.MethodPost, "/v1/entitlements/"+orgID+"/deprovision", body, map[string]string{"org_id": orgID})
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}
}

func TestGetEntitlementHandler(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tier":"team","features":{"sso":true}}`))
	}))
	t.Cleanup(srv.Close)
	svc := entitlement.NewService(srv.URL)
	svc.SetLogger(zap.NewNop())

	orgID := uuid.New().String()
	rec := doRequest(t, getEntitlement(svc), http.MethodGet, "/v1/entitlements/"+orgID, nil, map[string]string{"org_id": orgID})
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil || payload["tier"] != "team" {
		t.Fatalf("body = %s, want tier=team", rec.Body.String())
	}
}

func TestGetEntitlementHandlerBadOrgID(t *testing.T) {
	svc := newHandlersEntitlementService(t)
	rec := doRequest(t, getEntitlement(svc), http.MethodGet, "/v1/entitlements/bad", nil, map[string]string{"org_id": "bad"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
}

// --- Trials ---

func TestStartTrialHandler(t *testing.T) {
	svc := newHandlersEntitlementService(t)
	orgID := uuid.New().String()
	rec := doRequest(t, startTrial(svc), http.MethodPost, "/v1/trials/"+orgID+"/start", nil, map[string]string{"org_id": orgID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("got status %d, want 201", rec.Code)
	}
}

func TestStartTrialHandlerBadOrgID(t *testing.T) {
	svc := newHandlersEntitlementService(t)
	rec := doRequest(t, startTrial(svc), http.MethodPost, "/v1/trials/bad/start", nil, map[string]string{"org_id": "bad"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", rec.Code)
	}
}

func TestEndTrialHandlerWithSubscription(t *testing.T) {
	svc := newHandlersEntitlementService(t)
	orgID := uuid.New().String()
	rec := doRequest(t, endTrial(svc), http.MethodPost, "/v1/trials/"+orgID+"/end?has_subscription=true", nil, map[string]string{"org_id": orgID})
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}
}

func TestEndTrialHandlerWithoutSubscription(t *testing.T) {
	svc := newHandlersEntitlementService(t)
	orgID := uuid.New().String()
	rec := doRequest(t, endTrial(svc), http.MethodPost, "/v1/trials/"+orgID+"/end", nil, map[string]string{"org_id": orgID})
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}
}

// --- Router ---

func TestNewRouterHealth(t *testing.T) {
	ent := newHandlersEntitlementService(t)
	billingSvc := newHandlersBillingService()
	webhookHandler := webhook.NewHandler(billingSvc, ent, zap.NewNop(), "", "", "")
	auth := NewAuthMiddleware(AuthConfig{APIKeyStore: newFakeKeyStore(), EnableAPIKeyAuth: true}, zap.NewNop())
	rl := NewRateLimiter(RateLimiterConfig{RequestsPerMinute: 100}, zap.NewNop())
	r := NewRouter(billingSvc, ent, webhookHandler, auth, rl, zap.NewNop(), DefaultRouterConfig())

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health got status %d, want 200", rec.Code)
	}
}

func TestNewRouterRequiresAuthOnV1(t *testing.T) {
	ent := newHandlersEntitlementService(t)
	billingSvc := newHandlersBillingService()
	webhookHandler := webhook.NewHandler(billingSvc, ent, zap.NewNop(), "", "", "")
	auth := NewAuthMiddleware(AuthConfig{APIKeyStore: newFakeKeyStore(), EnableAPIKeyAuth: true}, zap.NewNop())
	rl := NewRateLimiter(RateLimiterConfig{RequestsPerMinute: 100}, zap.NewNop())
	r := NewRouter(billingSvc, ent, webhookHandler, auth, rl, zap.NewNop(), DefaultRouterConfig())

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/subscriptions", bytes.NewReader([]byte(`{}`))))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("v1 without key got status %d, want 401", rec.Code)
	}
}

func TestNewRouterWebhookBypassesAuth(t *testing.T) {
	ent := newHandlersEntitlementService(t)
	billingSvc := newHandlersBillingService()
	webhookHandler := webhook.NewHandler(billingSvc, ent, zap.NewNop(), "", "", "")
	auth := NewAuthMiddleware(AuthConfig{APIKeyStore: newFakeKeyStore(), EnableAPIKeyAuth: true}, zap.NewNop())
	rl := NewRateLimiter(RateLimiterConfig{RequestsPerMinute: 100}, zap.NewNop())
	r := NewRouter(billingSvc, ent, webhookHandler, auth, rl, zap.NewNop(), DefaultRouterConfig())

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader([]byte(`{}`))))
	if rec.Code == http.StatusUnauthorized {
		t.Fatal("webhook endpoint should not require auth")
	}
}

func TestDefaultRouterConfig(t *testing.T) {
	cfg := DefaultRouterConfig()
	if !cfg.CORSEnabled {
		t.Error("CORSEnabled should default to true")
	}
	if cfg.RateLimitRequestsPerMinute != 1000 {
		t.Errorf("RateLimitRequestsPerMinute = %d, want 1000", cfg.RateLimitRequestsPerMinute)
	}
}

// --- Coupons ---

func (m *handlersMockDB) GetCouponByCode(_ context.Context, code string) (*store.Coupon, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.coupons == nil {
		return nil, store.ErrNotFound
	}
	c, ok := m.coupons[code]
	if !ok {
		return nil, store.ErrNotFound
	}
	return c, nil
}

func (m *handlersMockDB) CountOrgCouponRedemptions(_ context.Context, _, _ uuid.UUID) (int64, error) {
	return 0, nil
}

func (m *handlersMockDB) CreateCoupon(_ context.Context, c *store.Coupon) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.coupons == nil {
		m.coupons = map[string]*store.Coupon{}
	}
	cp := *c
	m.coupons[c.Code] = &cp
	return nil
}

func (m *handlersMockDB) RedeemCoupon(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ *uuid.UUID) error {
	return nil
}

func (m *handlersMockDB) GetInvoiceByProviderID(_ context.Context, _, _ string) (*store.Invoice, error) {
	return nil, store.ErrNotFound
}

func (m *handlersMockDB) UpdateInvoiceStatusByID(_ context.Context, id uuid.UUID, status string, paidAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if inv, ok := m.invoices[id]; ok {
		inv.Status = status
		inv.PaidAt = &paidAt
		return nil
	}
	return store.ErrNotFound
}

func TestValidateCouponHandler(t *testing.T) {
	db := newHandlersMockDB()
	percent := 20
	db.coupons = map[string]*store.Coupon{
		"LAUNCH20": {ID: uuid.New(), Code: "LAUNCH20", PercentOff: &percent, Currency: "usd", MaxPerOrganization: 1, Active: true, CreatedAt: time.Now()},
	}
	h := validateCoupon(newBillingWithDB(db))
	body, _ := json.Marshal(map[string]interface{}{
		"code":            "launch20",
		"organization_id": uuid.New().String(),
		"amount_cents":    4900,
		"currency":        "usd",
	})

	rec := doRequest(t, h, http.MethodPost, "/v1/coupons/validate", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Valid    bool                   `json:"valid"`
		Discount billing.CouponDiscount `json:"discount"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Valid || resp.Discount.DiscountCents != 980 {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestValidateCouponHandlerUnknown(t *testing.T) {
	h := validateCoupon(newHandlersBillingService())
	body, _ := json.Marshal(map[string]interface{}{
		"code":            "NOPE",
		"organization_id": uuid.New().String(),
		"amount_cents":    1000,
	})
	rec := doRequest(t, h, http.MethodPost, "/v1/coupons/validate", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestValidateCouponHandlerMissingOrg(t *testing.T) {
	h := validateCoupon(newHandlersBillingService())
	body, _ := json.Marshal(map[string]interface{}{"code": "X"})
	rec := doRequest(t, h, http.MethodPost, "/v1/coupons/validate", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func seedCouponDB() *handlersMockDB {
	db := newHandlersMockDB()
	percent := 20
	db.coupons = map[string]*store.Coupon{
		"LAUNCH20": {ID: uuid.New(), Code: "LAUNCH20", PercentOff: &percent, Currency: "usd", MaxPerOrganization: 1, Active: true, CreatedAt: time.Now()},
	}
	return db
}

func TestCreateCouponHandler(t *testing.T) {
	db := newHandlersMockDB()
	h := createCoupon(newBillingWithDB(db))
	body, _ := json.Marshal(map[string]any{"code": "new10", "percent_off": 10, "currency": "usd", "max_per_organization": 1})
	rec := doRequest(t, h, http.MethodPost, "/v1/coupons", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if _, ok := db.coupons["NEW10"]; !ok {
		t.Fatal("coupon not stored uppercased")
	}
}

func TestRedeemCouponHandler(t *testing.T) {
	db := seedCouponDB()
	h := redeemCoupon(newBillingWithDB(db))
	body, _ := json.Marshal(map[string]any{
		"code": "launch20", "organization_id": uuid.New().String(),
		"amount_cents": 4900, "currency": "usd",
	})
	rec := doRequest(t, h, http.MethodPost, "/v1/coupons/redeem", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		DiscountCents int64 `json:"discount_cents"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.DiscountCents != 980 {
		t.Fatalf("discount = %d, want 980 (20%% of 4900)", out.DiscountCents)
	}
}

func TestCreateInvoiceWithCouponHandler(t *testing.T) {
	db := seedCouponDB()
	h := createInvoice(newBillingWithDB(db))
	orgID := uuid.New()
	body, _ := json.Marshal(map[string]any{
		"organization_id": orgID.String(), "subscription_id": uuid.New().String(),
		"amount_cents": 4900, "currency": "usd", "provider": "stripe", "coupon_code": "LAUNCH20",
	})
	rec := doRequest(t, h, http.MethodPost, "/v1/invoices", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var inv store.Invoice
	if err := json.Unmarshal(rec.Body.Bytes(), &inv); err != nil {
		t.Fatal(err)
	}
	if inv.AmountCents != 3920 {
		t.Fatalf("amount = %d, want 3920 (4900-980)", inv.AmountCents)
	}
}

func TestInvoicePDFHandler(t *testing.T) {
	db := newHandlersMockDB()
	id := uuid.New()
	db.invoices[id] = &store.Invoice{
		ID: id, OrganizationID: uuid.New(), Provider: "stripe",
		AmountCents: 4900, Currency: "usd", Status: "paid", IssuedAt: time.Now(),
	}
	h := invoicePDF(newBillingWithDB(db))
	rec := doRequest(t, h, http.MethodGet, "/v1/invoices/"+id.String()+"/pdf", nil, map[string]string{"id": id.String()})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("content-type = %s, want application/pdf", ct)
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF")) {
		t.Fatal("body is not a PDF")
	}
}

func TestRefundInvoiceHandlerPaths(t *testing.T) {
	db := newHandlersMockDB()
	stripeID := uuid.New()
	db.invoices[stripeID] = &store.Invoice{
		ID: stripeID, OrganizationID: uuid.New(), Provider: "stripe",
		AmountCents: 4900, Currency: "usd", Status: "paid", IssuedAt: time.Now(),
	}
	mpID := uuid.New()
	db.invoices[mpID] = &store.Invoice{
		ID: mpID, OrganizationID: uuid.New(), Provider: "mercado_pago",
		AmountCents: 4900, Currency: "usd", Status: "paid", IssuedAt: time.Now(),
	}
	h := refundInvoice(newBillingWithDB(db))

	// Non-stripe → explicit manual-action error, local status untouched.
	rec := doRequest(t, h, http.MethodPost, "/v1/invoices/"+mpID.String()+"/refund", nil, map[string]string{"id": mpID.String()})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mp refund status = %d, want 400", rec.Code)
	}
	// Stripe without key → configured error (no silent success).
	rec = doRequest(t, h, http.MethodPost, "/v1/invoices/"+stripeID.String()+"/refund", nil, map[string]string{"id": stripeID.String()})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("stripe refund status = %d, want 400 without key", rec.Code)
	}
}

func TestPortalSessionHandlerNoCustomer(t *testing.T) {
	db := newHandlersMockDB()
	h := portalSession(newBillingWithDB(db))
	orgID := uuid.New()
	body, _ := json.Marshal(map[string]any{"organization_id": orgID.String()})
	rec := doRequest(t, h, http.MethodPost, "/v1/portal/session", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (no stripe customer)", rec.Code)
	}
}

func TestCreatePaymentPixHandler(t *testing.T) {
	db := newHandlersMockDB()
	h := createPayment(newBillingWithDB(db))
	body, _ := json.Marshal(map[string]any{
		"organization_id": uuid.New().String(), "plan_id": "team", "method": "pix",
	})
	rec := doRequest(t, h, http.MethodPost, "/v1/payments", body)
	// MP key unset in tests → explicit configured error, never silent success.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 without MP key", rec.Code)
	}
}

func TestCreateUsageInvoiceHandler(t *testing.T) {
	db := newHandlersMockDB()
	h := createUsageInvoice(newBillingWithDB(db))
	body, _ := json.Marshal(map[string]any{
		"organization_id": uuid.New().String(), "subscription_id": uuid.New().String(),
		"plan_id": "team", "currency": "usd",
		"period_start": "2026-09-01T00:00:00Z", "period_end": "2026-10-01T00:00:00Z",
	})
	rec := doRequest(t, h, http.MethodPost, "/v1/usage/invoices", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
}

// --- Usage charges ---

func TestUsageChargesHandler(t *testing.T) {
	svc := newHandlersBillingService()
	h := usageCharges(svc)
	path := "/v1/usage/charges?organization_id=" + uuid.New().String() + "&plan_id=enterprise"
	rec := doRequest(t, h, http.MethodGet, path, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var summary billing.UsageChargeSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if summary.Metered {
		t.Fatal("enterprise should be unmetered")
	}
}

func TestUsageChargesHandlerMissingPlan(t *testing.T) {
	h := usageCharges(newHandlersBillingService())
	path := "/v1/usage/charges?organization_id=" + uuid.New().String()
	rec := doRequest(t, h, http.MethodGet, path, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
