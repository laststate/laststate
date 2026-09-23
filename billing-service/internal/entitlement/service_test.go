package entitlement

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/laststate/billing-service/internal/store"
)

func newTraceServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func newNopService(t *testing.T, traceURL string) *Service {
	t.Helper()
	svc := NewService(traceURL)
	svc.SetLogger(zap.NewNop())
	return svc
}

func TestNewService(t *testing.T) {
	svc := NewService("http://trace")
	if svc.traceURL != "http://trace" {
		t.Errorf("traceURL = %q, want http://trace", svc.traceURL)
	}
	if svc.client == nil || svc.client.Timeout != 30*time.Second {
		t.Errorf("client timeout = %v, want 30s", svc.client.Timeout)
	}
	svc.SetLogger(zap.NewNop())
	if svc.logger == nil {
		t.Error("logger not set")
	}
}

func TestProvision(t *testing.T) {
	var gotPath, gotMethod, gotAuth string
	var gotReq ProvisioningRequest
	srv := newTraceServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		w.WriteHeader(http.StatusOK)
	}))

	svc := newNopService(t, srv.URL)
	orgID := uuid.New()
	if err := svc.Provision(context.Background(), orgID, "team"); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	wantPath := "/v1/admin/organizations/" + orgID.String() + "/entitlements"
	if gotPath != wantPath {
		t.Errorf("path = %s, want %s", gotPath, wantPath)
	}
	if !strings.HasPrefix(gotAuth, "Bearer") {
		t.Errorf("Authorization = %q, want Bearer prefix", gotAuth)
	}
	if gotReq.OrganizationID != orgID.String() || gotReq.Tier != "team" {
		t.Errorf("request = %+v, want org %s tier team", gotReq, orgID)
	}
	if len(gotReq.Features) != 10 {
		t.Errorf("features = %d, want 10 for team", len(gotReq.Features))
	}
}

func TestDeprovision(t *testing.T) {
	var gotReq DeprovisioningRequest
	srv := newTraceServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		w.WriteHeader(http.StatusAccepted)
	}))

	svc := newNopService(t, srv.URL)
	if err := svc.Deprovision(context.Background(), uuid.New(), "free"); err != nil {
		t.Fatalf("Deprovision: %v", err)
	}
	if gotReq.Tier != "free" || len(gotReq.Features) != 2 {
		t.Errorf("request = %+v, want tier free with 2 features", gotReq)
	}
}

func TestProvisionTraceError(t *testing.T) {
	srv := newTraceServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	svc := newNopService(t, srv.URL)
	if err := svc.Provision(context.Background(), uuid.New(), "team"); err == nil {
		t.Fatal("expected error for non-2xx Trace response")
	}
}

func TestSendEntitlementRequestNetworkError(t *testing.T) {
	svc := newNopService(t, "http://127.0.0.1:1")
	err := svc.sendEntitlementRequest(context.Background(), "http://127.0.0.1:1", map[string]string{"x": "y"})
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestSendEntitlementRequestMarshalError(t *testing.T) {
	svc := newNopService(t, "http://unused")
	err := svc.sendEntitlementRequest(context.Background(), "http://unused", make(chan int))
	if err == nil {
		t.Fatal("expected marshal error for channel")
	}
}

func TestHandleTrialStart(t *testing.T) {
	var calls int
	srv := newTraceServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	svc := newNopService(t, srv.URL)

	if err := svc.HandleTrialStart(context.Background(), uuid.New(), 14); err != nil {
		t.Fatalf("HandleTrialStart: %v", err)
	}
	if calls != 1 {
		t.Errorf("provision calls = %d, want 1", calls)
	}
}

func TestHandleTrialStartDefaultDays(t *testing.T) {
	srv := newTraceServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	svc := newNopService(t, srv.URL)
	if err := svc.HandleTrialStart(context.Background(), uuid.New(), 0); err != nil {
		t.Fatalf("HandleTrialStart with 0 days: %v", err)
	}
}

func TestHandleTrialEndWithSubscription(t *testing.T) {
	var gotTier string
	srv := newTraceServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ProvisioningRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotTier = req.Tier
		w.WriteHeader(http.StatusOK)
	}))
	svc := newNopService(t, srv.URL)

	if err := svc.HandleTrialEnd(context.Background(), uuid.New(), "team"); err != nil {
		t.Fatalf("HandleTrialEnd with subscription: %v", err)
	}
	if gotTier != "team" {
		t.Errorf("tier = %q, want team", gotTier)
	}
}

func TestHandleTrialEndWithoutSubscription(t *testing.T) {
	var gotTier string
	srv := newTraceServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req DeprovisioningRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotTier = req.Tier
		w.WriteHeader(http.StatusOK)
	}))
	svc := newNopService(t, srv.URL)

	if err := svc.HandleTrialEnd(context.Background(), uuid.New(), "free"); err != nil {
		t.Fatalf("HandleTrialEnd without subscription: %v", err)
	}
	if gotTier != "free" {
		t.Errorf("tier = %q, want free", gotTier)
	}
}

func TestHandleTrialEndDeprovisionError(t *testing.T) {
	srv := newTraceServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	svc := newNopService(t, srv.URL)
	if err := svc.HandleTrialEnd(context.Background(), uuid.New(), "free"); err == nil {
		t.Fatal("expected error when deprovision fails")
	}
}

func TestUpdateUsage(t *testing.T) {
	svc := newNopService(t, "http://unused")
	if err := svc.UpdateUsage(context.Background(), uuid.New(), map[string]int64{"crashes": 3}); err != nil {
		t.Fatalf("UpdateUsage: %v", err)
	}
}

func TestListTiers(t *testing.T) {
	tiers := ListTiers()
	if len(tiers) != 4 {
		t.Fatalf("got %d tiers, want 4", len(tiers))
	}
	byName := make(map[string]TierInfo, len(tiers))
	for _, tier := range tiers {
		byName[tier.Name] = tier
	}

	free, ok := byName["Free"]
	if !ok {
		t.Fatal("missing Free tier")
	}
	if free.PriceCents != 0 || free.MaxDevices != 5 || free.MaxEventsPerDay != 1000 {
		t.Errorf("Free tier = %+v", free)
	}

	ent, ok := byName["Enterprise"]
	if !ok {
		t.Fatal("missing Enterprise tier")
	}
	if !ent.IsUnlimitedDevices || !ent.IsUnlimitedEvents || !ent.IsUnlimitedRetention {
		t.Errorf("Enterprise should be unlimited: %+v", ent)
	}
	if ent.PriceCents != 19900 {
		t.Errorf("Enterprise price = %d, want 19900", ent.PriceCents)
	}
}

func TestGetFeaturesForTier(t *testing.T) {
	cases := map[string]int{
		"free":       2,
		"hobbyist":   6,
		"team":       10,
		"enterprise": 13,
		"trial":      6,
		"garbage":    1,
	}
	for tier, want := range cases {
		if got := len(getFeaturesForTier(tier)); got != want {
			t.Errorf("tier %q: got %d features, want %d", tier, got, want)
		}
	}
	if got := getFeaturesForTier("unknown")[0]; got != "basic-crash-capture" {
		t.Errorf("default feature = %q, want basic-crash-capture", got)
	}
}

func TestGetTraceAdminToken(t *testing.T) {
	_ = getTraceAdminToken()
}

func TestNotImplementedStoreOps(t *testing.T) {
	svc := newNopService(t, "http://unused")
	if _, err := svc.GetOrganization(context.Background(), uuid.New()); err == nil {
		t.Fatal("GetOrganization should return error (not implemented)")
	}
	if err := svc.UpdateOrganizationTier(context.Background(), uuid.New(), "pro"); err == nil {
		t.Fatal("UpdateOrganizationTier should return error (not implemented)")
	}
}

func TestTraceBillingIntegrationEndToEnd(t *testing.T) {
	hmacSecret := "test-secret-key-12345"
	t.Setenv("TRACE_BILLING_HMAC_SECRET", hmacSecret)
	t.Setenv("TRACE_ADMIN_TOKEN", "admin-jwt-token")

	var receivedHeaders http.Header
	var receivedBody []byte
	var receivedTier string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		receivedBody, _ = io.ReadAll(r.Body)

		var req ProvisioningRequest
		_ = json.Unmarshal(receivedBody, &req)
		receivedTier = req.Tier

		// Verify HMAC signature manually
		tsStr := r.Header.Get("X-Billing-Timestamp")
		sigStr := r.Header.Get("X-Billing-Signature")
		if tsStr == "" || sigStr == "" {
			http.Error(w, "missing hmac headers", http.StatusUnauthorized)
			return
		}

		mac := hmac.New(sha256.New, []byte(hmacSecret))
		mac.Write([]byte(tsStr))
		mac.Write([]byte("\n"))
		mac.Write(receivedBody)
		expectedSig := hex.EncodeToString(mac.Sum(nil))

		if expectedSig != sigStr {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	defer srv.Close()

	svc := newNopService(t, srv.URL)
	orgID := uuid.New()

	// 1. Test Provisioning with HMAC
	if err := svc.Provision(context.Background(), orgID, "enterprise"); err != nil {
		t.Fatalf("Provision end-to-end failed: %v", err)
	}

	if receivedTier != "enterprise" {
		t.Errorf("expected tier enterprise, got %s", receivedTier)
	}
	if receivedHeaders.Get("Authorization") != "Bearer admin-jwt-token" {
		t.Errorf("unexpected authorization header: %s", receivedHeaders.Get("Authorization"))
	}
	if receivedHeaders.Get("X-Billing-Timestamp") == "" {
		t.Error("missing X-Billing-Timestamp header")
	}
	if receivedHeaders.Get("X-Billing-Signature") == "" {
		t.Error("missing X-Billing-Signature header")
	}

	// 2. Test Deprovisioning with HMAC
	if err := svc.Deprovision(context.Background(), orgID, "free"); err != nil {
		t.Fatalf("Deprovision end-to-end failed: %v", err)
	}
	if receivedTier != "free" {
		t.Errorf("expected tier free, got %s", receivedTier)
	}
}

type fakeOrgStore struct {
	mu        sync.Mutex
	deadlines map[uuid.UUID]*time.Time
	tiers     map[uuid.UUID]string
}

func newFakeOrgStore() *fakeOrgStore {
	return &fakeOrgStore{deadlines: map[uuid.UUID]*time.Time{}, tiers: map[uuid.UUID]string{}}
}

func (f *fakeOrgStore) GetOrgByID(_ context.Context, orgID uuid.UUID) (*store.Organization, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tier, ok := f.tiers[orgID]
	if !ok {
		return nil, store.ErrNotFound
	}
	return &store.Organization{ID: orgID, Tier: tier}, nil
}

func (f *fakeOrgStore) UpdateOrganizationTier(_ context.Context, orgID uuid.UUID, tier string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tiers[orgID] = tier
	return nil
}

func (f *fakeOrgStore) SetOrganizationTrialEndsAt(_ context.Context, orgID uuid.UUID, endsAt *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deadlines[orgID] = endsAt
	return nil
}

func (f *fakeOrgStore) ListExpiredTrials(_ context.Context, _ int) ([]store.Organization, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	var out []store.Organization
	for id, dl := range f.deadlines {
		if dl != nil && !dl.After(now) {
			out = append(out, store.Organization{ID: id, Tier: f.tiers[id]})
		}
	}
	return out, nil
}

func TestTrialDeadlinePersisted(t *testing.T) {
	srv := newTraceServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	svc := newNopService(t, srv.URL)
	st := newFakeOrgStore()
	svc.SetStore(st)
	orgID := uuid.New()

	if err := svc.HandleTrialStart(context.Background(), orgID, 14); err != nil {
		t.Fatalf("HandleTrialStart: %v", err)
	}
	st.mu.Lock()
	dl := st.deadlines[orgID]
	st.mu.Unlock()
	if dl == nil || time.Until(*dl) < 13*24*time.Hour {
		t.Fatalf("deadline not persisted ~14d out, got %v", dl)
	}

	if err := svc.ExtendTrial(context.Background(), orgID, 30); err != nil {
		t.Fatalf("ExtendTrial: %v", err)
	}
	st.mu.Lock()
	dl2 := st.deadlines[orgID]
	st.mu.Unlock()
	if dl2 == nil || time.Until(*dl2) < 29*24*time.Hour || !dl2.After(*dl) {
		t.Fatalf("extend did not push deadline out, got %v (was %v)", dl2, dl)
	}
}

func TestExpireDueTrials(t *testing.T) {
	var tiers []string
	srv := newTraceServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if tier, ok := req["tier"].(string); ok {
			tiers = append(tiers, tier)
		}
		w.WriteHeader(http.StatusOK)
	}))
	svc := newNopService(t, srv.URL)
	st := newFakeOrgStore()
	svc.SetStore(st)

	past := time.Now().Add(-time.Hour)
	freeOrg := uuid.New()
	paidOrg := uuid.New()
	st.tiers[freeOrg] = "free"
	st.tiers[paidOrg] = "team"
	st.deadlines[freeOrg] = &past
	st.deadlines[paidOrg] = &past

	n, err := svc.ExpireDueTrials(context.Background())
	if err != nil {
		t.Fatalf("ExpireDueTrials: %v", err)
	}
	if n != 2 {
		t.Fatalf("expired = %d, want 2", n)
	}
	// Free org converges to free, paid org keeps its tier.
	got := map[string]bool{}
	for _, tr := range tiers {
		got[tr] = true
	}
	if !got["free"] || !got["team"] {
		t.Fatalf("tiers converged = %v, want free+team", tiers)
	}
	// Deadlines cleared so the sweeper is idempotent.
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.deadlines[freeOrg] != nil || st.deadlines[paidOrg] != nil {
		t.Fatal("deadlines not cleared after expiry")
	}
}
