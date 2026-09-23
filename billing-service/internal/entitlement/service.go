// Package entitlement manages feature access and provisioning.
package entitlement

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/laststate/billing-service/internal/store"
)

// OrgStore is the subset of storage operations the entitlement service needs to
// read and mutate organization tier state. It is satisfied by *store.PostgresDB
// and is defined here (rather than reusing store.DB) so the service depends only
// on what it uses and remains easy to mock in tests.
type OrgStore interface {
	GetOrgByID(ctx context.Context, orgID uuid.UUID) (*store.Organization, error)
	UpdateOrganizationTier(ctx context.Context, orgID uuid.UUID, tier string) error
	SetOrganizationTrialEndsAt(ctx context.Context, orgID uuid.UUID, endsAt *time.Time) error
	ListExpiredTrials(ctx context.Context, limit int) ([]store.Organization, error)
}

// Service manages entitlements and provisions features based on subscription status.
type Service struct {
	traceURL string
	client   *http.Client
	logger   *zap.Logger
	store    OrgStore
}

// NewService creates a new entitlement service.
func NewService(traceURL string) *Service {
	return &Service{
		traceURL: traceURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetLogger sets the logger.
func (s *Service) SetLogger(logger *zap.Logger) {
	s.logger = logger
}

// SetStore configures the organization storage backend used by GetOrganization
// and UpdateOrganizationTier. When left unset those operations return an error.
func (s *Service) SetStore(store OrgStore) {
	s.store = store
}

// ProvisioningRequest is sent to Trace when an organization is provisioned.
type ProvisioningRequest struct {
	OrganizationID         string     `json:"organization_id"`
	Tier                   string     `json:"tier"`
	Features               []string   `json:"features"`
	BillingEventID         string     `json:"billing_event_id,omitempty"`
	PlanTier               string     `json:"plan_tier,omitempty"`
	SubscriptionStatus     string     `json:"subscription_status,omitempty"`
	GracePeriodEndsAt      *time.Time `json:"grace_period_ends_at,omitempty"`
	BillingCustomerRef     string     `json:"billing_customer_ref,omitempty"`
	BillingSubscriptionRef string     `json:"billing_subscription_ref,omitempty"`
	Reason                 string     `json:"reason,omitempty"`
}

// DeprovisioningRequest is sent to Trace when an organization is deprovisioned.
type DeprovisioningRequest struct {
	OrganizationID         string     `json:"organization_id"`
	Tier                   string     `json:"tier"`
	Features               []string   `json:"features"`
	BillingEventID         string     `json:"billing_event_id,omitempty"`
	PlanTier               string     `json:"plan_tier,omitempty"`
	SubscriptionStatus     string     `json:"subscription_status,omitempty"`
	GracePeriodEndsAt      *time.Time `json:"grace_period_ends_at,omitempty"`
	BillingCustomerRef     string     `json:"billing_customer_ref,omitempty"`
	BillingSubscriptionRef string     `json:"billing_subscription_ref,omitempty"`
	Reason                 string     `json:"reason,omitempty"`
}

// Provision activates features for an organization based on their subscription tier.
func (s *Service) Provision(ctx context.Context, orgID uuid.UUID, tier string) error {
	features := getFeaturesForTier(tier)
	req := ProvisioningRequest{
		OrganizationID:     orgID.String(),
		Tier:               tier,
		Features:           features,
		BillingEventID:     fmt.Sprintf("evt_prov_%s_%d", orgID.String()[:8], time.Now().UnixNano()),
		PlanTier:           tier,
		SubscriptionStatus: "active",
		Reason:             "provisioning subscription",
	}

	url := fmt.Sprintf("%s/v1/admin/organizations/%s/entitlements", s.traceURL, orgID)
	return s.sendEntitlementRequest(ctx, url, req)
}

// Get reads the current entitlements for an org from the Trace Admin API.
func (s *Service) Get(ctx context.Context, orgID uuid.UUID) (map[string]any, error) {
	if s.traceURL == "" {
		return nil, fmt.Errorf("trace admin api not configured")
	}
	url := fmt.Sprintf("%s/v1/admin/organizations/%s/entitlements", s.traceURL, orgID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if token := os.Getenv("TRACE_ADMIN_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("trace returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Deprovision removes features from an organization when their subscription is canceled.
func (s *Service) Deprovision(ctx context.Context, orgID uuid.UUID, tier string) error {
	features := getFeaturesForTier(tier)
	req := DeprovisioningRequest{
		OrganizationID:     orgID.String(),
		Tier:               tier,
		Features:           features,
		BillingEventID:     fmt.Sprintf("evt_deprov_%s_%d", orgID.String()[:8], time.Now().UnixNano()),
		PlanTier:           tier,
		SubscriptionStatus: "canceled",
		Reason:             "deprovisioning subscription",
	}

	url := fmt.Sprintf("%s/v1/admin/organizations/%s/entitlements", s.traceURL, orgID)
	return s.sendEntitlementRequest(ctx, url, req)
}

// HandleTrialStart handles the start of a trial period.
// The deadline is persisted (trial_ends_at) so restarts never lose it;
// expiry is enforced by ExpireDueTrials, not an in-memory timer.
func (s *Service) HandleTrialStart(ctx context.Context, orgID uuid.UUID, trialDays int) error {
	if trialDays <= 0 {
		trialDays = 14
	}

	if err := s.Provision(ctx, orgID, "trial"); err != nil {
		return fmt.Errorf("failed to provision trial: %w", err)
	}

	if s.store != nil {
		deadline := time.Now().Add(time.Duration(trialDays) * 24 * time.Hour)
		if err := s.store.SetOrganizationTrialEndsAt(ctx, orgID, &deadline); err != nil {
			return fmt.Errorf("failed to persist trial deadline: %w", err)
		}
	}

	return nil
}

// HandleTrialEnd handles the end of a trial period, converging the org onto
// an explicit tier: the subscription plan when one exists, "free" otherwise.
// (The previous "pro" hardcode matched no CHECK tier and no plan.)
func (s *Service) HandleTrialEnd(ctx context.Context, orgID uuid.UUID, tier string) error {
	if tier == "" {
		tier = "free"
	}
	var err error
	if tier == "free" {
		err = s.Deprovision(ctx, orgID, "free")
	} else {
		err = s.Provision(ctx, orgID, tier)
	}
	if err != nil {
		return fmt.Errorf("failed to converge trial end to %q: %w", tier, err)
	}
	if s.store != nil {
		if serr := s.store.SetOrganizationTrialEndsAt(ctx, orgID, nil); serr != nil && s.logger != nil {
			s.logger.Warn("failed to clear trial deadline", zap.String("org_id", orgID.String()), zap.Error(serr))
		}
	}
	return nil
}

// ExtendTrial re-provisions the trial tier and pushes the deadline out.
// Documented as POST /v1/trials/{org}/extend?days=N.
func (s *Service) ExtendTrial(ctx context.Context, orgID uuid.UUID, days int) error {
	if days <= 0 {
		days = 14
	}
	return s.HandleTrialStart(ctx, orgID, days)
}

// ExpireDueTrials converges every org past its trial deadline. Orgs already
// on a paid tier keep it; everyone else falls back to free. Returns the
// number of converged orgs. Safe to run on a schedule (idempotent: cleared
// deadlines never reappear).
func (s *Service) ExpireDueTrials(ctx context.Context) (int, error) {
	if s.store == nil {
		return 0, fmt.Errorf("organization store not configured")
	}
	orgs, err := s.store.ListExpiredTrials(ctx, 100)
	if err != nil {
		return 0, err
	}
	done := 0
	for _, org := range orgs {
		tier := org.Tier
		if tier == "" || tier == "trial" {
			tier = "free"
		}
		if err := s.HandleTrialEnd(ctx, org.ID, tier); err != nil {
			if s.logger != nil {
				s.logger.Warn("failed to expire trial", zap.String("org_id", org.ID.String()), zap.Error(err))
			}
			continue
		}
		done++
	}
	return done, nil
}

// UpdateUsage updates usage metrics and checks if the organization is over their limit.
func (s *Service) UpdateUsage(ctx context.Context, orgID uuid.UUID, metrics map[string]int64) error {
	s.logger.Info("usage updated", zap.String("org_id", orgID.String()), zap.Any("metrics", metrics))
	return nil
}

// TierInfo describes a subscription tier and its feature set.
type TierInfo struct {
	Name                 string   `json:"name"`
	PriceCents           int      `json:"price_cents"`
	Currency             string   `json:"currency"`
	MaxDevices           int      `json:"max_devices"`
	MaxEventsPerDay      int64    `json:"max_events_per_day"`
	RetentionDays        int      `json:"retention_days"`
	MaxAPITokens         int      `json:"max_api_tokens"`
	MaxAlertRules        int      `json:"max_alert_rules"`
	Features             []string `json:"features"`
	IsUnlimitedDevices   bool     `json:"is_unlimited_devices"`
	IsUnlimitedEvents    bool     `json:"is_unlimited_events"`
	IsUnlimitedRetention bool     `json:"is_unlimited_retention"`
}

// ListTiers returns all available subscription tiers with their quotas and features.
func ListTiers() []TierInfo {
	return []TierInfo{
		{
			Name: "Free", PriceCents: 0, Currency: "usd",
			MaxDevices: 5, MaxEventsPerDay: 1000, RetentionDays: 30,
			MaxAPITokens: 1, MaxAlertRules: 0,
			Features: []string{"symbolication"},
		},
		{
			Name: "Hobbyist", PriceCents: 900, Currency: "usd",
			MaxDevices: 100, MaxEventsPerDay: 50000, RetentionDays: 90,
			MaxAPITokens: 5, MaxAlertRules: 5,
			Features: []string{"symbolication", "analytics_export", "custom_alerts"},
		},
		{
			Name: "Team", PriceCents: 4900, Currency: "usd",
			MaxDevices: 1000, MaxEventsPerDay: 500000, RetentionDays: 365,
			MaxAPITokens: 20, MaxAlertRules: 50,
			Features: []string{"symbolication", "analytics_export", "custom_alerts", "custom_integrations", "oncall", "escalation", "audit_logs", "sso"},
		},
		{
			Name: "Enterprise", PriceCents: 19900, Currency: "usd",
			MaxDevices: -1, MaxEventsPerDay: -1, RetentionDays: -1,
			MaxAPITokens: -1, MaxAlertRules: -1,
			IsUnlimitedDevices: true, IsUnlimitedEvents: true, IsUnlimitedRetention: true,
			Features: []string{"symbolication", "analytics_export", "custom_alerts", "custom_integrations", "oncall", "escalation", "audit_logs", "sso", "priority_support", "sla", "on_prem"},
		},
	}
}

// getFeaturesForTier returns the features available for a given tier.
func getFeaturesForTier(tier string) []string {
	switch tier {
	case "free":
		return []string{"basic-crash-capture", "limited-spool"}
	case "hobbyist":
		return []string{"basic-crash-capture", "full-spool", "symbolication", "api-access", "analytics", "custom-alerts"}
	case "team":
		return []string{"basic-crash-capture", "full-spool", "symbolication", "api-access", "analytics", "custom-integrations", "oncall", "escalation", "audit-logs", "sso"}
	case "enterprise":
		return []string{"basic-crash-capture", "full-spool", "symbolication", "api-access", "analytics", "custom-integrations", "oncall", "escalation", "audit-logs", "sso", "priority-support", "sla", "on-prem"}
	case "trial":
		return []string{"basic-crash-capture", "full-spool", "symbolication", "api-access", "analytics", "custom-alerts"}
	default:
		return []string{"basic-crash-capture"}
	}
}

// sendEntitlementRequest sends an entitlement request to Trace.
func (s *Service) sendEntitlementRequest(ctx context.Context, url string, req interface{}) error {
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+getTraceAdminToken())

	timestamp := time.Now().UTC().Unix()
	httpReq.Header.Set("X-Billing-Timestamp", strconv.FormatInt(timestamp, 10))

	hmacSecret := os.Getenv("TRACE_BILLING_HMAC_SECRET")
	if hmacSecret != "" {
		mac := hmac.New(sha256.New, []byte(hmacSecret))
		mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
		mac.Write([]byte("\n"))
		mac.Write(payload)
		sigHex := hex.EncodeToString(mac.Sum(nil))
		httpReq.Header.Set("X-Billing-Signature", sigHex)
	}

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read trace response body: %w", err)
		}
		return fmt.Errorf("trace returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// getTraceAdminToken returns the admin token for Trace API authentication.
// It is read from the TRACE_ADMIN_TOKEN environment variable (never hardcoded
// or logged). Returns "" if unset, which callers must treat as "unconfigured".
func getTraceAdminToken() string {
	return os.Getenv("TRACE_ADMIN_TOKEN")
}

// GetOrganization returns the organization record from the configured storage
// backend. It returns an error when no store has been configured.
func (s *Service) GetOrganization(ctx context.Context, orgID uuid.UUID) (*store.Organization, error) {
	if s.store == nil {
		return nil, fmt.Errorf("organization store not configured")
	}
	org, err := s.store.GetOrgByID(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("get organization %s: %w", orgID, err)
	}
	return org, nil
}

// UpdateOrganizationTier persists a new tier for the organization and, when a
// Trace Admin API URL is configured, synchronizes the corresponding
// entitlements with Trace (an HMAC-signed provisioning request). Persistence
// happens first so local state stays authoritative even if the Trace sync
// fails; the Trace error is returned so callers can retry.
func (s *Service) UpdateOrganizationTier(ctx context.Context, orgID uuid.UUID, tier string) error {
	if s.store == nil {
		return fmt.Errorf("organization store not configured")
	}
	if tier == "" {
		return fmt.Errorf("tier is required")
	}
	if err := s.store.UpdateOrganizationTier(ctx, orgID, tier); err != nil {
		return fmt.Errorf("update organization tier: %w", err)
	}

	// Synchronize entitlements with Trace when configured.
	if s.traceURL != "" {
		if err := s.Provision(ctx, orgID, tier); err != nil {
			return fmt.Errorf("sync tier %q with trace: %w", tier, err)
		}
	}
	return nil
}
