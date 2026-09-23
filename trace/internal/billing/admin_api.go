package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/laststate/trace/internal/store"
)

// HMAC scheme for the Admin API used by the proprietary billing service.
//
// Headers required on every request:
//
//	X-Billing-Timestamp:  unix epoch seconds, must be within ±5 min of server time
//	X-Billing-Signature:  hex(hmac_sha256(secret, timestamp + "\n" + body))
//
// Body is the raw JSON of the request (use io.ReadAll in the handler before
// signing — the signature is computed over the EXACT bytes received).
//
// The secret is a per-deployment value configured via env (TRACE_BILLING_HMAC_SECRET)
// and shared out-of-band with the billing service. Rotating the secret is a
// separate operational concern; this code supports any number of valid secrets
// passed as a slice.

// HeaderTimestamp is the wire-format timestamp header.
const HeaderTimestamp = "X-Billing-Timestamp"

// HeaderSignature is the wire-format signature header.
const HeaderSignature = "X-Billing-Signature"

// DefaultClockSkew is the maximum allowed drift between client and server time.
const DefaultClockSkew = 5 * time.Minute

// ApplyEntitlementsRequest is the JSON body POSTed to /v1/admin/organizations/{id}/entitlements.
//
// billing_event_id MUST be unique per logical event from the billing service
// (Stripe event id, MP notification id, etc.) so retries are idempotent.
type ApplyEntitlementsRequest struct {
	BillingEventID         string     `json:"billing_event_id"`
	PlanTier               string     `json:"plan_tier"`
	SubscriptionStatus     string     `json:"subscription_status"`
	GracePeriodEndsAt      *time.Time `json:"grace_period_ends_at,omitempty"`
	BillingCustomerRef     string     `json:"billing_customer_ref,omitempty"`
	BillingSubscriptionRef string     `json:"billing_subscription_ref,omitempty"`
	Reason                 string     `json:"reason,omitempty"`
}

// ApplyEntitlementsResponse is what we return to the billing service.
type ApplyEntitlementsResponse struct {
	OrganizationID      uuid.UUID  `json:"organization_id"`
	PlanTier            string     `json:"plan_tier"`
	SubscriptionStatus  string     `json:"subscription_status"`
	GracePeriodEndsAt   *time.Time `json:"grace_period_ends_at,omitempty"`
	AppliedAt           time.Time  `json:"applied_at"`
	IdempotentDuplicate bool       `json:"idempotent_duplicate"`
}

// ApplyEntitlementsStore is the slice of *store.Store used by the handler. We
// keep it minimal so unit tests can stub it.
type ApplyEntitlementsStore interface {
	GetOrgSubscription(ctx context.Context, orgID uuid.UUID) (store.OrgSubscription, error)
	SetOrgSubscription(ctx context.Context, sub store.OrgSubscription, actor, reason string) (store.OrgSubscription, error)
	RecordEntitlementEvent(ctx context.Context, orgID uuid.UUID, billingEventID, tier, status string, graceEndsAt *time.Time, actor string, payload json.RawMessage) (bool, error)
}

// ApplyEntitlements validates the request against the store and applies it.
// Returns the resulting state and a flag indicating whether the event was an
// idempotent retry (true) or a new apply (false).
func ApplyEntitlements(ctx context.Context, st ApplyEntitlementsStore, orgID uuid.UUID, req ApplyEntitlementsRequest) (ApplyEntitlementsResponse, error) {
	if req.BillingEventID == "" {
		return ApplyEntitlementsResponse{}, errors.New("billing: billing_event_id required")
	}
	if req.PlanTier == "" {
		return ApplyEntitlementsResponse{}, errors.New("billing: plan_tier required")
	}
	if req.SubscriptionStatus == "" {
		req.SubscriptionStatus = StatusActive
	}
	// Defense in depth — enforce known statuses so a typo from the billing
	// service cannot silently disable the org.
	switch req.SubscriptionStatus {
	case StatusActive, StatusTrialing, StatusPastDue, StatusSuspended, StatusCanceled:
	default:
		return ApplyEntitlementsResponse{}, fmt.Errorf("billing: unknown subscription_status %q", req.SubscriptionStatus)
	}

	prev, err := st.GetOrgSubscription(ctx, orgID)
	if err != nil {
		return ApplyEntitlementsResponse{}, err
	}
	payload, _ := json.Marshal(req)
	inserted, err := st.RecordEntitlementEvent(ctx, orgID, req.BillingEventID, req.PlanTier, req.SubscriptionStatus, req.GracePeriodEndsAt, "billing-service", payload)
	if err != nil {
		return ApplyEntitlementsResponse{}, err
	}
	if !inserted {
		// Duplicate billing_event_id for this org — return the current state so
		// the billing service can reconcile without re-triggering side effects.
		return ApplyEntitlementsResponse{
			OrganizationID:      orgID,
			PlanTier:            prev.PlanTier,
			SubscriptionStatus:  prev.SubscriptionStatus,
			GracePeriodEndsAt:   prev.GracePeriodEndsAt,
			AppliedAt:           time.Now().UTC(),
			IdempotentDuplicate: true,
		}, nil
	}

	sub := store.OrgSubscription{
		OrganizationID:         orgID,
		PlanTier:               req.PlanTier,
		SubscriptionStatus:     req.SubscriptionStatus,
		GracePeriodEndsAt:      req.GracePeriodEndsAt,
		BillingCustomerRef:     req.BillingCustomerRef,
		BillingSubscriptionRef: req.BillingSubscriptionRef,
	}
	out, err := st.SetOrgSubscription(ctx, sub, "billing-service", req.Reason)
	if err != nil {
		return ApplyEntitlementsResponse{}, err
	}
	return ApplyEntitlementsResponse{
		OrganizationID:     out.OrganizationID,
		PlanTier:           out.PlanTier,
		SubscriptionStatus: out.SubscriptionStatus,
		GracePeriodEndsAt:  out.GracePeriodEndsAt,
		AppliedAt:          time.Now().UTC(),
	}, nil
}

// Sign computes the hex-encoded HMAC-SHA256 signature of (timestamp + "\n" + body)
// using one of the provided secrets. Verification in production uses
// VerifySignature so the constant-time compare lives in one place.
func Sign(secrets [][]byte, timestamp int64, body []byte) string {
	if len(secrets) == 0 {
		return ""
	}
	mac := hmac.New(sha256.New, secrets[0])
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte("\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature validates an incoming signature against a set of known
// secrets. Any single matching secret validates — callers can rotate by adding
// the new secret without invalidating the old one. The comparison is
// constant-time per secret.
func VerifySignature(secrets [][]byte, timestamp int64, body []byte, gotSignature string, now time.Time, maxSkew time.Duration) error {
	if gotSignature == "" {
		return errors.New("billing: missing signature")
	}
	if len(secrets) == 0 {
		return errors.New("billing: no secrets configured")
	}
	ts := time.Unix(timestamp, 0)
	if drift := now.Sub(ts); drift > maxSkew || -drift > maxSkew {
		return fmt.Errorf("billing: timestamp out of window (drift %s)", drift)
	}
	mac := hmac.New(sha256.New, secrets[0])
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte("\n"))
	mac.Write(body)
	wantHex := hex.EncodeToString(mac.Sum(nil))
	if hmac.Equal([]byte(wantHex), []byte(gotSignature)) {
		return nil
	}
	// Try additional secrets (rotation).
	for i := 1; i < len(secrets); i++ {
		mac := hmac.New(sha256.New, secrets[i])
		mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
		mac.Write([]byte("\n"))
		mac.Write(body)
		wantHex = hex.EncodeToString(mac.Sum(nil))
		if hmac.Equal([]byte(wantHex), []byte(gotSignature)) {
			return nil
		}
	}
	return errors.New("billing: signature mismatch")
}
