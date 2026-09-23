package billing_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/billing"
	"github.com/laststate/trace/internal/store"
)

type fakeResolver struct {
	plans map[string]store.PlanLimit
	subs  map[uuid.UUID]store.OrgSubscription
	usage map[uuid.UUID]map[string]int64
}

func newFakeResolver() *fakeResolver {
	return &fakeResolver{
		plans: map[string]store.PlanLimit{
			billing.TierFree:       {Tier: billing.TierFree, EventsPerMonth: 10000, ProjectsMax: 1, MembersMax: 3},
			billing.TierPro:        {Tier: billing.TierPro, EventsPerMonth: 1000000, ProjectsMax: 10, MembersMax: 15},
			billing.TierEnterprise: {Tier: billing.TierEnterprise, EventsPerMonth: 0, ProjectsMax: 0, MembersMax: 0},
		},
		subs:  map[uuid.UUID]store.OrgSubscription{},
		usage: map[uuid.UUID]map[string]int64{},
	}
}

func (f *fakeResolver) GetOrgSubscription(ctx context.Context, orgID uuid.UUID) (store.OrgSubscription, error) {
	sub, ok := f.subs[orgID]
	if !ok {
		return store.OrgSubscription{}, store.ErrNotFound
	}
	return sub, nil
}

func (f *fakeResolver) GetPlan(ctx context.Context, tier string) (store.PlanLimit, error) {
	p, ok := f.plans[tier]
	if !ok {
		return store.PlanLimit{}, store.ErrUnknownPlan
	}
	return p, nil
}

func (f *fakeResolver) CurrentUsage(ctx context.Context, orgID uuid.UUID, kind string) (store.UsageCounter, error) {
	m, ok := f.usage[orgID]
	v := int64(0)
	if ok {
		v = m[kind]
	}
	return store.UsageCounter{OrganizationID: orgID, Kind: kind, Counter: v}, nil
}

func TestResolvePlan_FallsBackToFreeOnUnknown(t *testing.T) {
	r := newFakeResolver()
	orgID := uuid.New()
	r.subs[orgID] = store.OrgSubscription{OrganizationID: orgID, PlanTier: "ghost-tier", SubscriptionStatus: billing.StatusActive}
	p, err := billing.ResolvePlan(context.Background(), r, orgID)
	if err != nil {
		t.Fatal(err)
	}
	if p.PlanLimit.Tier != billing.TierFree {
		t.Fatalf("expected free fallback, got %s", p.PlanLimit.Tier)
	}
}

func TestIsWritable(t *testing.T) {
	now := time.Now()
	mk := func(status string, grace *time.Time) billing.Plan {
		return billing.Plan{
			PlanLimit: store.PlanLimit{Tier: billing.TierPro},
			Sub:       store.OrgSubscription{SubscriptionStatus: status, GracePeriodEndsAt: grace},
		}
	}
	if !mk(billing.StatusActive, nil).IsWritable(now) {
		t.Error("active should be writable")
	}
	if !mk(billing.StatusTrialing, nil).IsWritable(now) {
		t.Error("trialing should be writable")
	}
	grace := now.Add(time.Hour)
	if !mk(billing.StatusPastDue, &grace).IsWritable(now) {
		t.Error("past_due inside grace should be writable")
	}
	expired := now.Add(-time.Hour)
	if mk(billing.StatusPastDue, &expired).IsWritable(now) {
		t.Error("past_due outside grace should be read-only")
	}
	if mk(billing.StatusSuspended, nil).IsWritable(now) {
		t.Error("suspended should be read-only")
	}
	if mk(billing.StatusCanceled, nil).IsWritable(now) {
		t.Error("canceled should be read-only")
	}
}

func TestEventsAllowed(t *testing.T) {
	plan := billing.Plan{PlanLimit: store.PlanLimit{EventsPerMonth: 100}}
	if a, rem, lim := plan.EventsAllowed(0); !a || rem != 100 || lim != 100 {
		t.Errorf("0 used: %v %d %d", a, rem, lim)
	}
	if a, rem, lim := plan.EventsAllowed(99); !a || rem != 1 || lim != 100 {
		t.Errorf("99 used: %v %d %d", a, rem, lim)
	}
	if a, _, _ := plan.EventsAllowed(100); a {
		t.Error("100 used should be blocked")
	}
	if a, _, lim := plan.EventsAllowed(101); a || lim != 100 {
		t.Errorf("101 used: %v lim=%d", a, lim)
	}
	// Unlimited tier.
	unlim := billing.Plan{PlanLimit: store.PlanLimit{EventsPerMonth: 0}}
	if a, rem, lim := unlim.EventsAllowed(1_000_000); !a || rem != 0 || lim != 0 {
		t.Errorf("unlimited: %v %d %d", a, rem, lim)
	}
}

func TestCheckEvents_BlocksSuspended(t *testing.T) {
	r := newFakeResolver()
	orgID := uuid.New()
	r.subs[orgID] = store.OrgSubscription{OrganizationID: orgID, PlanTier: billing.TierPro, SubscriptionStatus: billing.StatusSuspended}
	err := billing.CheckEvents(context.Background(), r, orgID, time.Now())
	if err == nil || !billing.IsQuotaExceeded(err) {
		t.Fatalf("expected quota error, got %v", err)
	}
	var qe *billing.ErrQuotaExceeded
	if !asQuota(err, &qe) || qe.PlanTier != billing.TierPro {
		t.Fatalf("wrong error shape: %+v", err)
	}
}

func TestCheckEvents_BlocksWhenExceeded(t *testing.T) {
	r := newFakeResolver()
	orgID := uuid.New()
	r.subs[orgID] = store.OrgSubscription{OrganizationID: orgID, PlanTier: billing.TierFree, SubscriptionStatus: billing.StatusActive}
	r.usage[orgID] = map[string]int64{store.UsageKindEvents: 10000}
	err := billing.CheckEvents(context.Background(), r, orgID, time.Now())
	if err == nil || !billing.IsQuotaExceeded(err) {
		t.Fatalf("expected quota error, got %v", err)
	}
	var qe *billing.ErrQuotaExceeded
	if !asQuota(err, &qe) || qe.Limit != 10000 || qe.Current != 10000 {
		t.Fatalf("wrong limit/current: %+v", err)
	}
}

func TestCheckEvents_AllowsUnlimited(t *testing.T) {
	r := newFakeResolver()
	orgID := uuid.New()
	r.subs[orgID] = store.OrgSubscription{OrganizationID: orgID, PlanTier: billing.TierEnterprise, SubscriptionStatus: billing.StatusActive}
	r.usage[orgID] = map[string]int64{store.UsageKindEvents: 10_000_000}
	if err := billing.CheckEvents(context.Background(), r, orgID, time.Now()); err != nil {
		t.Fatalf("unlimited tier should pass: %v", err)
	}
}

func TestCheckEvents_GraceHint(t *testing.T) {
	r := newFakeResolver()
	orgID := uuid.New()
	now := time.Now()
	grace := now.Add(2 * time.Minute)
	r.subs[orgID] = store.OrgSubscription{
		OrganizationID: orgID, PlanTier: billing.TierFree, SubscriptionStatus: billing.StatusPastDue,
		GracePeriodEndsAt: &grace,
	}
	r.usage[orgID] = map[string]int64{store.UsageKindEvents: 10000}
	err := billing.CheckEvents(context.Background(), r, orgID, now)
	if err == nil {
		t.Fatal("expected error")
	}
	var qe *billing.ErrQuotaExceeded
	if !asQuota(err, &qe) || qe.RetryAfter == 0 {
		t.Fatalf("expected retry hint in grace, got %+v", err)
	}
}

func TestCheckProjectLimit(t *testing.T) {
	r := newFakeResolver()
	orgID := uuid.New()
	r.subs[orgID] = store.OrgSubscription{OrganizationID: orgID, PlanTier: billing.TierFree, SubscriptionStatus: billing.StatusActive}
	if err := billing.CheckProjectLimit(context.Background(), r, orgID, 0); err != nil {
		t.Fatal(err)
	}
	if err := billing.CheckProjectLimit(context.Background(), r, orgID, 1); err == nil {
		t.Fatal("free tier at 1 should block")
	}
	// Enterprise (ProjectsMax=0) unlimited.
	r.subs[orgID] = store.OrgSubscription{OrganizationID: orgID, PlanTier: billing.TierEnterprise, SubscriptionStatus: billing.StatusActive}
	if err := billing.CheckProjectLimit(context.Background(), r, orgID, 999); err != nil {
		t.Fatalf("enterprise should be unlimited: %v", err)
	}
}

func asQuota(err error, dst **billing.ErrQuotaExceeded) bool {
	var qe *billing.ErrQuotaExceeded
	if !billing.IsQuotaExceeded(err) {
		return false
	}
	//nolint:errorlint // we know it's an *ErrQuotaExceeded here
	qe = err.(*billing.ErrQuotaExceeded)
	*dst = qe
	return true
}

// applyAdminTest verifies HMAC sign+verify roundtrip.
func TestHMACSignVerify(t *testing.T) {
	secret := []byte("super-secret")
	now := time.Now()
	ts := now.Unix()
	body := []byte(`{"hello":"world"}`)
	sig := billing.Sign([][]byte{secret}, ts, body)
	if sig == "" {
		t.Fatal("sign returned empty")
	}
	if err := billing.VerifySignature([][]byte{secret}, ts, body, sig, now, billing.DefaultClockSkew); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestHMACVerify_RejectsBadSig(t *testing.T) {
	now := time.Now()
	err := billing.VerifySignature([][]byte{[]byte("k")}, now.Unix(), []byte("body"), "deadbeef", now, billing.DefaultClockSkew)
	if err == nil {
		t.Fatal("expected mismatch")
	}
}

func TestHMACVerify_RejectsStaleTimestamp(t *testing.T) {
	secret := []byte("k")
	old := time.Now().Add(-2 * billing.DefaultClockSkew)
	ts := old.Unix()
	body := []byte("body")
	sig := billing.Sign([][]byte{secret}, ts, body)
	err := billing.VerifySignature([][]byte{secret}, ts, body, sig, time.Now(), billing.DefaultClockSkew)
	if err == nil {
		t.Fatal("expected stale timestamp error")
	}
}

func TestHMACVerify_SupportsRotation(t *testing.T) {
	now := time.Now()
	ts := now.Unix()
	body := []byte("body")
	sig := billing.Sign([][]byte{[]byte("old")}, ts, body)
	// Server knows both old and new secrets.
	if err := billing.VerifySignature([][]byte{[]byte("new"), []byte("old")}, ts, body, sig, now, billing.DefaultClockSkew); err != nil {
		t.Fatalf("rotation should validate against old: %v", err)
	}
}

func TestApplyEntitlements_AppliesNewState(t *testing.T) {
	st := &fakeAdminStore{
		subs: map[uuid.UUID]store.OrgSubscription{
			uuid.MustParse("00000000-0000-0000-0000-000000000001"): {
				OrganizationID: uuid.MustParse("00000000-0000-0000-0000-000000000001"),
				PlanTier:       billing.TierFree, SubscriptionStatus: billing.StatusActive,
			},
		},
		events: map[string]bool{},
	}
	orgID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	resp, err := billing.ApplyEntitlements(context.Background(), st, orgID, billing.ApplyEntitlementsRequest{
		BillingEventID: "evt_1", PlanTier: billing.TierPro, SubscriptionStatus: billing.StatusActive, Reason: "renewal",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.PlanTier != billing.TierPro || resp.SubscriptionStatus != billing.StatusActive {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.IdempotentDuplicate {
		t.Fatal("first apply should not be flagged duplicate")
	}
}

func TestApplyEntitlements_IdempotentDuplicate(t *testing.T) {
	orgID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	st := &fakeAdminStore{
		subs: map[uuid.UUID]store.OrgSubscription{orgID: {
			OrganizationID: orgID, PlanTier: billing.TierPro, SubscriptionStatus: billing.StatusActive,
		}},
		events: map[string]bool{orgID.String() + ":evt_dup": true}, // pretend already inserted
	}
	resp, err := billing.ApplyEntitlements(context.Background(), st, orgID, billing.ApplyEntitlementsRequest{
		BillingEventID: "evt_dup", PlanTier: billing.TierFree, SubscriptionStatus: billing.StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.IdempotentDuplicate {
		t.Fatal("second apply should be flagged duplicate")
	}
	// And the org's tier must NOT have been downgraded.
	if st.lastSet.PlanTier == billing.TierFree {
		t.Fatalf("duplicate should not have mutated state, last set = %+v", st.lastSet)
	}
}

func TestApplyEntitlements_RejectsUnknownStatus(t *testing.T) {
	orgID := uuid.New()
	st := &fakeAdminStore{subs: map[uuid.UUID]store.OrgSubscription{orgID: {
		OrganizationID: orgID, PlanTier: billing.TierFree, SubscriptionStatus: billing.StatusActive,
	}}}
	_, err := billing.ApplyEntitlements(context.Background(), st, orgID, billing.ApplyEntitlementsRequest{
		BillingEventID: "evt_x", PlanTier: billing.TierPro, SubscriptionStatus: "weird",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestApplyEntitlements_RequiresBillingEventID(t *testing.T) {
	orgID := uuid.New()
	st := &fakeAdminStore{subs: map[uuid.UUID]store.OrgSubscription{orgID: {
		OrganizationID: orgID, PlanTier: billing.TierFree, SubscriptionStatus: billing.StatusActive,
	}}}
	_, err := billing.ApplyEntitlements(context.Background(), st, orgID, billing.ApplyEntitlementsRequest{
		PlanTier: billing.TierPro, SubscriptionStatus: billing.StatusActive,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// fakeAdminStore implements billing.ApplyEntitlementsStore for unit tests.
type fakeAdminStore struct {
	subs    map[uuid.UUID]store.OrgSubscription
	events  map[string]bool
	lastSet store.OrgSubscription
}

func (f *fakeAdminStore) GetOrgSubscription(ctx context.Context, orgID uuid.UUID) (store.OrgSubscription, error) {
	s, ok := f.subs[orgID]
	if !ok {
		return store.OrgSubscription{}, store.ErrNotFound
	}
	return s, nil
}

func (f *fakeAdminStore) SetOrgSubscription(ctx context.Context, sub store.OrgSubscription, actor, reason string) (store.OrgSubscription, error) {
	f.lastSet = sub
	f.subs[sub.OrganizationID] = sub
	return sub, nil
}

func (f *fakeAdminStore) RecordEntitlementEvent(ctx context.Context, orgID uuid.UUID, billingEventID, tier, status string, graceEndsAt *time.Time, actor string, payload json.RawMessage) (bool, error) {
	key := orgID.String() + ":" + billingEventID
	if f.events[key] {
		return false, nil
	}
	f.events[key] = true
	return true, nil
}
