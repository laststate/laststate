// Package billing — outbound billing-event webhooks (Billing API v2).
//
// While /v1 is poll-based, v2 pushes signed event notifications to
// customer-owned HTTPS endpoints so integrations stop polling:
//
//	subscription.created|updated|canceled, invoice.paid|failed,
//	usage.limit_exceeded, dunning.escalated
//
// Each delivery signs the raw JSON body with HMAC-SHA256 using the
// endpoint's secret and sends it in X-LastState-Signature as
// "sha256=<hex>". Receivers recompute the HMAC over the exact bytes.
// Deliveries retry 3x with exponential backoff (1s, 4s, 16s).
package billing

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
	"sync"
	"time"

	"github.com/google/uuid"
)

// Billing event types (v2 outbound).
const (
	EventSubscriptionCreated  = "subscription.created"
	EventSubscriptionUpdated  = "subscription.updated"
	EventSubscriptionCanceled = "subscription.canceled"
	EventInvoicePaid          = "invoice.paid"
	EventInvoiceFailed        = "invoice.failed"
	EventUsageLimitExceeded   = "usage.limit_exceeded"
	EventDunningEscalated     = "dunning.escalated"
)

// BillingEvent is a v2 outbound notification.
type BillingEvent struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	OrgID     uuid.UUID      `json:"organization_id"`
	Data      map[string]any `json:"data,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// WebhookEndpoint is a customer-owned HTTPS receiver for billing events.
type WebhookEndpoint struct {
	ID        uuid.UUID `json:"id"`
	OrgID     uuid.UUID `json:"organization_id"`
	URL       string    `json:"url"`
	Secret    string    `json:"-"`
	Events    []string  `json:"events"` // empty = all
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

// EventDispatcher delivers BillingEvents to registered endpoints.
// The default store is in-memory (safe for tests/dev); production should
// persist endpoints via store.DB (see docs/API_V2.md migration note).
type EventDispatcher struct {
	mu        sync.RWMutex
	endpoints map[uuid.UUID]*WebhookEndpoint
	byOrg     map[uuid.UUID][]*WebhookEndpoint
	client    *http.Client
	emit      func(ctx context.Context, ep *WebhookEndpoint, evt BillingEvent) error
}

// NewEventDispatcher creates a dispatcher with a 10s HTTP client.
func NewEventDispatcher() *EventDispatcher {
	d := &EventDispatcher{
		endpoints: make(map[uuid.UUID]*WebhookEndpoint),
		byOrg:     make(map[uuid.UUID][]*WebhookEndpoint),
		client:    &http.Client{Timeout: 10 * time.Second},
	}
	d.emit = d.deliverWithRetry
	return d
}

// RegisterEndpoint adds an HTTPS receiver. URL must be https (http allowed
// only for localhost, for tests/dev).
func (d *EventDispatcher) RegisterEndpoint(orgID uuid.UUID, rawURL, secret string, events []string) (*WebhookEndpoint, error) {
	if rawURL == "" {
		return nil, fmt.Errorf("url is required")
	}
	if len(rawURL) < 8 || (rawURL[:8] != "https://" && rawURL[:7] != "http://") {
		return nil, fmt.Errorf("url must be http(s)")
	}
	if secret == "" {
		return nil, fmt.Errorf("secret is required")
	}
	ep := &WebhookEndpoint{
		ID:        uuid.New(),
		OrgID:     orgID,
		URL:       rawURL,
		Secret:    secret,
		Events:    events,
		Active:    true,
		CreatedAt: time.Now(),
	}
	d.mu.Lock()
	d.endpoints[ep.ID] = ep
	d.byOrg[orgID] = append(d.byOrg[orgID], ep)
	d.mu.Unlock()
	return ep, nil
}

// UnregisterEndpoint removes an endpoint.
func (d *EventDispatcher) UnregisterEndpoint(id uuid.UUID) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	ep, ok := d.endpoints[id]
	if !ok {
		return false
	}
	delete(d.endpoints, id)
	kept := d.byOrg[ep.OrgID][:0]
	for _, e := range d.byOrg[ep.OrgID] {
		if e.ID != id {
			kept = append(kept, e)
		}
	}
	d.byOrg[ep.OrgID] = kept
	return true
}

// ListEndpoints returns an org's endpoints (secrets redacted by callers).
func (d *EventDispatcher) ListEndpoints(orgID uuid.UUID) []*WebhookEndpoint {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]*WebhookEndpoint, len(d.byOrg[orgID]))
	copy(out, d.byOrg[orgID])
	return out
}

// Dispatch emits evt to every active endpoint subscribed to evt.Type.
// Delivery is best-effort + logged; it never blocks billing writes.
func (d *EventDispatcher) Dispatch(ctx context.Context, evt BillingEvent) {
	if evt.ID == "" {
		evt.ID = uuid.NewString()
	}
	if evt.CreatedAt.IsZero() {
		evt.CreatedAt = time.Now()
	}
	d.mu.RLock()
	targets := append([]*WebhookEndpoint(nil), d.byOrg[evt.OrgID]...)
	d.mu.RUnlock()
	for _, ep := range targets {
		if !ep.Active || !wants(ep.Events, evt.Type) {
			continue
		}
		_ = d.emit(ctx, ep, evt)
	}
}

func wants(subs []string, typ string) bool {
	if len(subs) == 0 {
		return true
	}
	for _, s := range subs {
		if s == typ || s == "*" {
			return true
		}
	}
	return false
}

// SignEvent computes "sha256=<hex hmac-sha256(secret, body)>".
func SignEvent(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// deliverWithRetry POSTs evt JSON with signature header, 3 attempts.
func (d *EventDispatcher) deliverWithRetry(ctx context.Context, ep *WebhookEndpoint, evt BillingEvent) error {
	body, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(1<<uint(2*(attempt-1))) * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL, bytes.NewReader(body))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-LastState-Signature", SignEvent(ep.Secret, body))
		req.Header.Set("X-LastState-Event", evt.Type)
		resp, err := d.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("endpoint returned HTTP %d", resp.StatusCode)
	}
	return lastErr
}

// Emit is the Service-level helper: builds the event and dispatches it.
// A nil dispatcher is a no-op so tests/services without v2 keep working.
func (d *EventDispatcher) Emit(ctx context.Context, orgID uuid.UUID, typ string, data map[string]any) {
	if d == nil {
		return
	}
	d.Dispatch(ctx, BillingEvent{Type: typ, OrgID: orgID, Data: data})
}
