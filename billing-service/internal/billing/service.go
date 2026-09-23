package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/laststate/billing-service/internal/store"
)

// Service manages billing operations.
type Service struct {
	stripe      PaymentProvider
	mp          PaymentProvider
	crypto      PaymentProvider
	nowpayments PaymentProvider  // optional 4th gateway (NOWPayments); nil = disabled
	events      *EventDispatcher // optional v2 outbound webhooks; nil = disabled
	mail        *Mailer          // transactional email; log transport when SMTP unset
	db          store.DB
	logger      *zap.Logger
	idempotency sync.Map // eventID -> processed flag (in-memory fast path)
}

// NewService creates a new billing service.
func NewService(db store.DB, stripe, mp, crypto PaymentProvider) *Service {
	return &Service{
		stripe: stripe,
		mp:     mp,
		crypto: crypto,
		db:     db,
		mail:   MailerFromEnv(),
	}
}

// SetMailer overrides the transactional mailer (tests inject a sink).
func (s *Service) SetMailer(m *Mailer) {
	s.mail = m
}

// VerifyWebhook verifies a webhook signature and parses the payload.
// This is a public helper used by the webhook handler.
// It dispatches to the correct provider based on the endpoint secret prefix.
// Supports whsec_ (Stripe), and any secret for MercadoPago/Crypto providers.
func (s *Service) VerifyWebhook(payload []byte, sig []byte, endpointSecret string) (map[string]interface{}, error) {
	if endpointSecret == "" {
		return nil, fmt.Errorf("endpoint secret is required")
	}
	if len(sig) == 0 {
		return nil, fmt.Errorf("webhook signature is required")
	}

	// Dispatch to the appropriate provider based on endpoint secret prefix.
	// Stripe secrets start with "whsec_"; MercadoPago and Crypto accept any secret.
	if len(endpointSecret) > 6 && endpointSecret[:6] == "whsec_" {
		if s.stripe == nil {
			return nil, fmt.Errorf("stripe provider not configured")
		}
		return s.stripe.VerifyWebhook(payload, sig, endpointSecret)
	}

	// For MercadoPago and Crypto, accept any non-empty secret and delegate
	// to the provider's own verification logic. The handler may also have
	// already verified the signature before calling this method.
	if s.mp != nil {
		if result, err := s.mp.VerifyWebhook(payload, sig, endpointSecret); err == nil {
			return result, nil
		}
	}
	if s.crypto != nil {
		if result, err := s.crypto.VerifyWebhook(payload, sig, endpointSecret); err == nil {
			return result, nil
		}
	}
	if s.nowpayments != nil {
		if result, err := s.nowpayments.VerifyWebhook(payload, sig, endpointSecret); err == nil {
			return result, nil
		}
	}

	return nil, fmt.Errorf("no configured provider could verify the webhook")
}

// SetNowPayments attaches the optional NOWPayments gateway without breaking
// the NewService(db, stripe, mp, crypto) constructor used by existing callers.
func (s *Service) SetNowPayments(p PaymentProvider) {
	s.nowpayments = p
}

// SetEventDispatcher attaches the v2 outbound webhook dispatcher (nil disables).
func (s *Service) SetEventDispatcher(d *EventDispatcher) {
	s.events = d
}

// Events returns the attached v2 dispatcher (may be nil).
func (s *Service) Events() *EventDispatcher {
	return s.events
}

// SetLogger sets the logger.
func (s *Service) SetLogger(logger *zap.Logger) {
	s.logger = logger
}

// providerFor returns the PaymentProvider for a provider name.
// Supported: stripe, mercado_pago (aliases: mercadopago, mp),
// crypto (aliases: coinbase, coinbase_commerce), nowpayments (aliases:
// now_payments, np). Unknown names return an error.
func (s *Service) providerFor(name string) (PaymentProvider, error) {
	switch name {
	case "stripe":
		if s.stripe == nil {
			return nil, fmt.Errorf("stripe provider not configured")
		}
		return s.stripe, nil
	case "mercado_pago", "mercadopago", "mp":
		if s.mp == nil {
			return nil, fmt.Errorf("mercado pago provider not configured")
		}
		return s.mp, nil
	case "crypto", "coinbase", "coinbase_commerce":
		if s.crypto == nil {
			return nil, fmt.Errorf("crypto provider not configured")
		}
		return s.crypto, nil
	case "nowpayments", "now_payments", "np":
		if s.nowpayments == nil {
			return nil, fmt.Errorf("nowpayments provider not configured (set NOWPAYMENTS_KEY)")
		}
		return s.nowpayments, nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
}

// ProcessWebhook processes an incoming webhook event with idempotency.
// Uses in-memory cache for fast path and DB for durability across restarts.
func (s *Service) ProcessWebhook(ctx context.Context, provider string, eventID string, payload []byte) error {
	// Check in-memory idempotency cache first (fast path)
	if _, exists := s.idempotency.Load(eventID); exists {
		return nil // Already processed
	}

	// Check DB for previously processed events (durability across restarts)
	existing, err := s.db.GetWebhookEvent(ctx, eventID)
	if err == nil && existing != nil {
		// Already processed and persisted — return nil (idempotent)
		s.logger.Info("duplicate webhook event (persisted)", zap.String("event_id", eventID))
		return nil
	}

	// Mark as processing in memory
	s.idempotency.Store(eventID, true)
	defer s.idempotency.Delete(eventID)

	// Dispatch to provider-specific handler (aliases normalized).
	switch provider {
	case "stripe":
		return s.processStripeWebhook(ctx, eventID, payload)
	case "mercado_pago", "mercadopago", "mp":
		return s.processMercadoPagoWebhook(ctx, eventID, payload)
	case "crypto", "coinbase", "coinbase_commerce":
		return s.processCryptoWebhook(ctx, eventID, payload)
	case "nowpayments", "now_payments", "np":
		return s.processNowPaymentsWebhook(ctx, eventID, payload)
	default:
		return fmt.Errorf("unknown provider: %s", provider)
	}
}

func (s *Service) processStripeWebhook(ctx context.Context, eventID string, payload []byte) error {
	var evt struct {
		Type   string          `json:"type"`
		Data   json.RawMessage `json:"data"`
		Object json.RawMessage `json:"object"`
	}
	if err := json.Unmarshal(payload, &evt); err != nil {
		return fmt.Errorf("parse stripe event: %w", err)
	}

	switch evt.Type {
	case "invoice.paid":
		var inv struct {
			ID           string `json:"id"`
			Subscription string `json:"subscription"`
		}
		if err := json.Unmarshal(evt.Data, &inv); err == nil && inv.ID != "" {
			if err := s.db.UpdateInvoiceStatus(ctx, inv.ID, "paid", time.Now()); err != nil {
				s.logger.Warn("failed to mark invoice paid", zap.String("invoice", inv.ID), zap.Error(err))
			} else if rec, rerr := s.db.GetInvoiceByProviderID(ctx, "stripe", inv.ID); rerr == nil && rec != nil {
				s.sendReceipt(ctx, rec)
				s.events.Emit(ctx, rec.OrganizationID, EventInvoicePaid, map[string]any{
					"invoice_id": rec.ID.String(), "provider": "stripe",
				})
			}
		}
	case "invoice.payment_failed":
		var inv struct {
			ID           string `json:"id"`
			Subscription string `json:"subscription"`
		}
		if err := json.Unmarshal(evt.Data, &inv); err == nil && inv.ID != "" {
			if err := s.db.UpdateInvoiceStatus(ctx, inv.ID, "failed", time.Now()); err != nil {
				s.logger.Warn("failed to mark invoice failed", zap.String("invoice", inv.ID), zap.Error(err))
			}
			s.failSubscriptionInvoice(ctx, "stripe", inv.Subscription, inv.ID)
		}
	case "payment_intent.payment_failed":
		var pi struct {
			ID      string `json:"id"`
			Invoice string `json:"invoice"`
		}
		if err := json.Unmarshal(evt.Data, &pi); err == nil && pi.Invoice != "" {
			if err := s.db.UpdateInvoiceStatus(ctx, pi.Invoice, "failed", time.Now()); err != nil {
				s.logger.Warn("failed to mark invoice failed", zap.String("invoice", pi.Invoice), zap.Error(err))
			}
			if rec, rerr := s.findInvoice(ctx, "stripe", pi.Invoice); rerr == nil && rec != nil {
				if err := s.InitiateDunning(ctx, rec.OrganizationID, rec.ID.String()); err != nil && s.logger != nil {
					s.logger.Warn("failed to initiate dunning", zap.Error(err))
				}
				s.sendPaymentFailed(ctx, rec.OrganizationID, 1)
				s.events.Emit(ctx, rec.OrganizationID, EventInvoiceFailed, map[string]any{
					"invoice_id": rec.ID.String(), "provider": "stripe", "payment_intent": pi.ID,
				})
			}
		}
	case "checkout.session.completed":
		s.recordCheckoutSubscription(ctx, evt.Data)
	case "charge.dispute.created", "charge.dispute.updated":
		// A dispute freezes the funds: treat like a failure for dunning,
		// and surface the dispute id for ops follow-up.
		var d struct {
			ID      string `json:"id"`
			Charge  string `json:"charge"`
			Status  string `json:"status"`
			Invoice string `json:"invoice"`
		}
		if err := json.Unmarshal(evt.Data, &d); err == nil {
			ref := d.Invoice
			if ref == "" {
				ref = d.Charge
			}
			if ref != "" {
				if rec, rerr := s.findInvoice(ctx, "stripe", ref); rerr == nil && rec != nil {
					_ = s.db.UpdateInvoiceStatus(ctx, ref, "failed", time.Now())
					if err := s.InitiateDunning(ctx, rec.OrganizationID, rec.ID.String()); err != nil && s.logger != nil {
						s.logger.Warn("failed to initiate dunning", zap.Error(err))
					}
					s.sendPaymentFailed(ctx, rec.OrganizationID, 1)
					s.events.Emit(ctx, rec.OrganizationID, EventInvoiceFailed, map[string]any{
						"invoice_id": rec.ID.String(), "provider": "stripe",
						"dispute_id": d.ID, "dispute_status": d.Status,
					})
				} else {
					s.events.Emit(ctx, uuid.Nil, EventInvoiceFailed, map[string]any{
						"provider": "stripe", "dispute_id": d.ID, "dispute_status": d.Status,
					})
				}
			}
		}
	case "customer.subscription.trial_will_end":
		var sub struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(evt.Data, &sub); err == nil && sub.ID != "" {
			if rec, rerr := s.db.GetSubscriptionByProviderSubID(ctx, sub.ID); rerr == nil && rec != nil {
				if org, oerr := s.db.GetOrgByID(ctx, rec.OrganizationID); oerr == nil && org != nil && org.Email != "" {
					if err := s.mail.Send(ctx, TrialEndingEmail(org.Email, org.Name)); err != nil && s.logger != nil {
						s.logger.Info("trial-ending email not sent via smtp", zap.Error(err))
					}
				}
			}
		}
	case "customer.subscription.updated":
		var sub struct {
			ID               string `json:"id"`
			Status           string `json:"status"`
			CurrentPeriodEnd int64  `json:"current_period_end"`
		}
		if err := json.Unmarshal(evt.Data, &sub); err == nil && sub.ID != "" {
			periodEnd := time.Unix(sub.CurrentPeriodEnd, 0)
			if err := s.db.UpdateSubscriptionStatus(ctx, sub.ID, sub.Status, periodEnd); err != nil {
				s.logger.Warn("failed to update subscription status", zap.String("sub", sub.ID), zap.Error(err))
			}
		}
	case "customer.subscription.deleted":
		var sub struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(evt.Data, &sub); err == nil && sub.ID != "" {
			if err := s.db.UpdateSubscriptionStatus(ctx, sub.ID, "canceled", time.Time{}); err != nil {
				s.logger.Warn("failed to cancel subscription", zap.String("sub", sub.ID), zap.Error(err))
			}
		}
	default:
		s.logger.Info("unhandled stripe event type", zap.String("type", evt.Type))
	}

	// Persist event for idempotency
	if err := s.persistWebhookEvent(ctx, "stripe", eventID, payload); err != nil {
		s.logger.Warn("failed to persist webhook event", zap.String("event_id", eventID), zap.Error(err))
	}

	return nil
}

// findInvoice resolves an invoice by provider id, falling back to a local
// UUID reference. Provider webhooks are inconsistent about which they send.
func (s *Service) findInvoice(ctx context.Context, provider, ref string) (*store.Invoice, error) {
	if inv, err := s.db.GetInvoiceByProviderID(ctx, provider, ref); err == nil && inv != nil {
		return inv, nil
	}
	if id, err := uuid.Parse(ref); err == nil {
		return s.db.GetInvoiceByID(ctx, id)
	}
	return nil, fmt.Errorf("invoice %s not found", ref)
}

// failSubscriptionInvoice marks a provider invoice failed, starts dunning
// for its org, and emits invoice.failed. Best-effort throughout: webhook
// processing must not fail because a side effect did.
func (s *Service) failSubscriptionInvoice(ctx context.Context, provider, providerSubID, providerInvID string) {
	if providerSubID == "" {
		return
	}
	sub, err := s.db.GetSubscriptionByProviderSubID(ctx, providerSubID)
	if err != nil || sub == nil {
		s.logger.Warn("failed invoice for unknown subscription", zap.String("sub", providerSubID))
		return
	}
	if err := s.InitiateDunning(ctx, sub.OrganizationID, providerInvID); err != nil && s.logger != nil {
		s.logger.Warn("failed to initiate dunning", zap.Error(err))
	}
	s.sendPaymentFailed(ctx, sub.OrganizationID, 1)
	s.events.Emit(ctx, sub.OrganizationID, EventInvoiceFailed, map[string]any{
		"provider": provider, "subscription_id": providerSubID, "invoice_id": providerInvID,
	})
}

// recordCheckoutSubscription records the local subscription (and first
// invoice anchor) when a Stripe Checkout Session completes. Metadata
// organization_id/plan_id is attached at session creation.
func (s *Service) recordCheckoutSubscription(ctx context.Context, data json.RawMessage) {
	var session struct {
		ID           string            `json:"id"`
		Customer     string            `json:"customer"`
		Subscription string            `json:"subscription"`
		Metadata     map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal(data, &session); err != nil {
		return
	}
	orgIDStr, planID := session.Metadata["organization_id"], session.Metadata["plan_id"]
	orgID, err := uuid.Parse(orgIDStr)
	if err != nil || planID == "" || session.Subscription == "" {
		s.logger.Warn("checkout.session.completed missing metadata")
		return
	}
	if existing, err := s.db.GetSubscriptionByProviderSubID(ctx, session.Subscription); err == nil && existing != nil {
		return // already recorded (webhook redelivery)
	}
	now := time.Now()
	sub := &store.Subscription{
		ID:                 uuid.New(),
		OrganizationID:     orgID,
		Provider:           "stripe",
		ProviderSubID:      session.Subscription,
		PlanID:             planID,
		Status:             "active",
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.Add(30 * 24 * time.Hour),
	}
	if err := s.db.InsertSubscription(ctx, sub); err != nil {
		s.logger.Warn("failed to record checkout subscription", zap.Error(err))
		return
	}
	s.events.Emit(ctx, orgID, EventSubscriptionCreated, map[string]any{
		"provider": "stripe", "plan_id": planID, "subscription_id": session.Subscription,
	})
}

func (s *Service) processMercadoPagoWebhook(ctx context.Context, eventID string, payload []byte) error {
	var evt struct {
		Type     string          `json:"type"`
		Data     json.RawMessage `json:"data"`
		Resource string          `json:"resource"`
	}
	if err := json.Unmarshal(payload, &evt); err != nil {
		return fmt.Errorf("parse mp event: %w", err)
	}

	switch evt.Type {
	case "payment.updated":
		var p struct {
			ID                string  `json:"id"`
			StatusCode        int     `json:"status_code"`
			TransactionAmount float64 `json:"transaction_amount"`
			InvoiceID         string  `json:"invoice_id"`
		}
		if err := json.Unmarshal(evt.Data, &p); err == nil {
			if p.StatusCode == 200 || p.StatusCode == 1 { // approved
				if p.InvoiceID != "" {
					if err := s.db.UpdateInvoiceStatus(ctx, p.InvoiceID, "paid", time.Now()); err != nil {
						s.logger.Warn("failed to mark MP invoice paid", zap.String("invoice", p.InvoiceID), zap.Error(err))
					} else if rec, rerr := s.findInvoice(ctx, "mercado_pago", p.InvoiceID); rerr == nil && rec != nil {
						s.sendReceipt(ctx, rec)
						s.events.Emit(ctx, rec.OrganizationID, EventInvoicePaid, map[string]any{
							"invoice_id": rec.ID.String(), "provider": "mercado_pago",
						})
					}
				}
			} else if p.InvoiceID != "" {
				// Rejected/cancelled payment → failed + dunning.
				if err := s.db.UpdateInvoiceStatus(ctx, p.InvoiceID, "failed", time.Now()); err != nil {
					s.logger.Warn("failed to mark MP invoice failed", zap.String("invoice", p.InvoiceID), zap.Error(err))
				}
				if rec, rerr := s.findInvoice(ctx, "mercado_pago", p.InvoiceID); rerr == nil && rec != nil {
					if err := s.InitiateDunning(ctx, rec.OrganizationID, rec.ID.String()); err != nil && s.logger != nil {
						s.logger.Warn("failed to initiate dunning", zap.Error(err))
					}
					s.sendPaymentFailed(ctx, rec.OrganizationID, 1)
					s.events.Emit(ctx, rec.OrganizationID, EventInvoiceFailed, map[string]any{
						"invoice_id": rec.ID.String(), "provider": "mercado_pago",
					})
				}
			}
		}
	default:
		s.logger.Info("unhandled mp event type", zap.String("type", evt.Type))
	}

	// Persist event for idempotency
	if err := s.persistWebhookEvent(ctx, "mercado_pago", eventID, payload); err != nil {
		s.logger.Warn("failed to persist webhook event", zap.String("event_id", eventID), zap.Error(err))
	}

	return nil
}

func (s *Service) processCryptoWebhook(ctx context.Context, eventID string, payload []byte) error {
	var evt struct {
		Type     string          `json:"type"`
		Data     json.RawMessage `json:"data"`
		ChargeID string          `json:"charge_id"`
	}
	if err := json.Unmarshal(payload, &evt); err != nil {
		return fmt.Errorf("parse crypto event: %w", err)
	}

	switch evt.Type {
	case "charge:confirmed":
		// Charge is confirmed — mark corresponding invoice as paid
		if evt.ChargeID != "" {
			if err := s.db.UpdateInvoiceStatusByProviderID(ctx, "crypto", evt.ChargeID, "paid", time.Now()); err != nil {
				s.logger.Warn("failed to mark crypto invoice paid", zap.String("charge", evt.ChargeID), zap.Error(err))
			} else if rec, rerr := s.findInvoice(ctx, "crypto", evt.ChargeID); rerr == nil && rec != nil {
				s.sendReceipt(ctx, rec)
				s.events.Emit(ctx, rec.OrganizationID, EventInvoicePaid, map[string]any{
					"invoice_id": rec.ID.String(), "provider": "crypto",
				})
			}
		}
	case "charge:failed", "charge:expired":
		if evt.ChargeID != "" {
			if err := s.db.UpdateInvoiceStatusByProviderID(ctx, "crypto", evt.ChargeID, "failed", time.Now()); err != nil {
				s.logger.Warn("failed to mark crypto invoice failed", zap.String("charge", evt.ChargeID), zap.Error(err))
			} else if rec, rerr := s.findInvoice(ctx, "crypto", evt.ChargeID); rerr == nil && rec != nil {
				s.events.Emit(ctx, rec.OrganizationID, EventInvoiceFailed, map[string]any{
					"invoice_id": rec.ID.String(), "provider": "crypto",
				})
			}
		}
	default:
		s.logger.Info("unhandled crypto event type", zap.String("type", evt.Type))
	}

	// Persist event for idempotency
	if err := s.persistWebhookEvent(ctx, "crypto", eventID, payload); err != nil {
		s.logger.Warn("failed to persist webhook event", zap.String("event_id", eventID), zap.Error(err))
	}

	return nil
}

func (s *Service) processNowPaymentsWebhook(ctx context.Context, eventID string, payload []byte) error {
	// NOWPayments IPN payload: {"payment_id":..,"payment_status":"finished",
	// "order_id":"<org>:<plan>", ...}. Accept both flat and nested shapes.
	var evt struct {
		PaymentID     string `json:"payment_id"`
		PaymentStatus string `json:"payment_status"`
		OrderID       string `json:"order_id"`
		Type          string `json:"type"`
	}
	if err := json.Unmarshal(payload, &evt); err != nil {
		return fmt.Errorf("parse nowpayments event: %w", err)
	}
	status := strings.ToLower(evt.PaymentStatus)
	if status == "" {
		status = strings.ToLower(evt.Type)
	}
	switch status {
	case "finished", "confirmed":
		anchor := evt.PaymentID
		if anchor == "" {
			anchor = eventID
		}
		if err := s.db.UpdateInvoiceStatusByProviderID(ctx, "nowpayments", anchor, "paid", time.Now()); err != nil {
			s.logger.Warn("failed to mark nowpayments invoice paid", zap.String("payment", anchor), zap.Error(err))
		} else if rec, rerr := s.findInvoice(ctx, "nowpayments", anchor); rerr == nil && rec != nil {
			s.sendReceipt(ctx, rec)
			s.events.Emit(ctx, rec.OrganizationID, EventInvoicePaid, map[string]any{
				"invoice_id": rec.ID.String(), "provider": "nowpayments",
			})
		}
		// Also try the crypto table for backwards-compat anchors.
		_ = s.db.UpdateInvoiceStatusByProviderID(ctx, "crypto", anchor, "paid", time.Now())
	case "failed", "expired", "refunded":
		anchor := evt.PaymentID
		if anchor == "" {
			anchor = eventID
		}
		_ = s.db.UpdateInvoiceStatusByProviderID(ctx, "nowpayments", anchor, "failed", time.Now())
		if rec, rerr := s.findInvoice(ctx, "nowpayments", anchor); rerr == nil && rec != nil {
			s.events.Emit(ctx, rec.OrganizationID, EventInvoiceFailed, map[string]any{
				"invoice_id": rec.ID.String(), "provider": "nowpayments",
			})
		}
	default:
		s.logger.Info("unhandled nowpayments event status", zap.String("status", evt.PaymentStatus))
	}
	if err := s.persistWebhookEvent(ctx, "nowpayments", eventID, payload); err != nil {
		s.logger.Warn("failed to persist webhook event", zap.String("event_id", eventID), zap.Error(err))
	}
	return nil
}

// persistWebhookEvent stores the event in DB for idempotency across restarts.
func (s *Service) persistWebhookEvent(ctx context.Context, provider, eventID string, payload []byte) error {
	we := &store.WebhookEvent{
		Provider:    provider,
		EventID:     eventID,
		RawPayload:  payload,
		ProcessedAt: time.Now(),
	}
	return s.db.InsertWebhookEvent(ctx, we)
}

// CreateSubscription creates a new subscription for an organization.
func (s *Service) CreateSubscription(ctx context.Context, orgID uuid.UUID, provider string, planID string) (string, error) {
	p, err := s.providerFor(provider)
	if err != nil {
		return "", err
	}
	// Recurring providers (Mercado Pago Preapproval) bill the payer email.
	payerEmail := ""
	if org, orgErr := s.db.GetOrgByID(ctx, orgID); orgErr == nil && org != nil {
		payerEmail = org.Email
	} else if s.logger != nil {
		s.logger.Warn("creating subscription without org email", zap.String("org_id", orgID.String()))
	}
	subID, err := p.CreateSubscription(ctx, orgID, planID, payerEmail)
	if err != nil {
		return "", err
	}

	// Store subscription in DB
	now := time.Now()
	sub := &store.Subscription{
		ID:                 uuid.New(),
		OrganizationID:     orgID,
		Provider:           provider,
		ProviderSubID:      subID,
		PlanID:             planID,
		Status:             "active",
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.Add(30 * 24 * time.Hour), // 30 days
	}
	if err := s.db.InsertSubscription(ctx, sub); err != nil {
		return "", fmt.Errorf("failed to store subscription: %w", err)
	}

	// v2: push, don't poll — best-effort, never blocks the write path.
	s.events.Emit(ctx, orgID, EventSubscriptionCreated, map[string]any{
		"provider": provider, "plan_id": planID, "subscription_id": subID,
	})

	return subID, nil
}

// CreatePixPayment creates an instant Pix payment via Mercado Pago.
// The payer email resolves from the organization record.
func (s *Service) CreatePixPayment(ctx context.Context, orgID uuid.UUID, planID string) (*PixPaymentResult, error) {
	mp, ok := s.mp.(*MercadoPagoProvider)
	if !ok || s.mp == nil {
		return nil, fmt.Errorf("mercado pago provider not configured")
	}
	payerEmail := ""
	if org, err := s.db.GetOrgByID(ctx, orgID); err == nil && org != nil {
		payerEmail = org.Email
	}
	return mp.CreatePixPayment(ctx, orgID, planID, payerEmail)
}

// CreateBoletoPayment creates a boleto bancário via Mercado Pago.
// Payer email and fiscal document resolve from the organization record.
func (s *Service) CreateBoletoPayment(ctx context.Context, orgID uuid.UUID, planID string) (*BoletoPaymentResult, error) {
	mp, ok := s.mp.(*MercadoPagoProvider)
	if !ok || s.mp == nil {
		return nil, fmt.Errorf("mercado pago provider not configured")
	}
	payerEmail, taxID := "", ""
	if org, err := s.db.GetOrgByID(ctx, orgID); err == nil && org != nil {
		payerEmail, taxID = org.Email, org.TaxID
	}
	return mp.CreateBoletoPayment(ctx, orgID, planID, payerEmail, taxID)
}

// GetSubscription returns a subscription by its provider subscription ID.
// It queries the local DB first; if not found, it falls back to the provider.
func (s *Service) GetSubscription(ctx context.Context, subID string) (*SubscriptionInfo, error) {
	// Try local DB first
	sub, err := s.db.GetSubscriptionByProviderSubID(ctx, subID)
	if err == nil && sub != nil {
		// Convert local record to SubscriptionInfo
		return &SubscriptionInfo{
			ID:                sub.ProviderSubID,
			CustomerID:        "", // would need to be stored in DB
			PlanID:            sub.PlanID,
			Status:            sub.Status,
			CurrentPeriodEnd:  sub.CurrentPeriodEnd,
			CancelAtPeriodEnd: sub.CancelAtPeriodEnd,
		}, nil
	}

	// If the subscription was not found in the local DB, we need to know
	// which provider it belongs to in order to fall back. If sub is nil
	// (not found or error), we cannot determine the provider, so return an
	// error rather than risking a nil pointer dereference.
	if sub == nil {
		return nil, fmt.Errorf("subscription %s not found locally", subID)
	}

	// Fall back to the provider using the known subscription record.
	return s.getProviderSubscription(ctx, sub, subID)
}

// getProviderSubscription dispatches GetSubscription to the correct provider.
func (s *Service) getProviderSubscription(ctx context.Context, sub *store.Subscription, subID string) (*SubscriptionInfo, error) {
	provider, err := s.providerFor(sub.Provider)
	if err != nil {
		return nil, err
	}
	return provider.GetSubscription(ctx, subID)
}

// CancelSubscription cancels a subscription.
func (s *Service) CancelSubscription(ctx context.Context, subID string) error {
	sub, err := s.db.GetSubscriptionByProviderSubID(ctx, subID)
	if err != nil {
		return err
	}

	provider, err := s.providerFor(sub.Provider)
	if err != nil {
		return err
	}

	if err := provider.CancelSubscription(ctx, sub.ProviderSubID); err != nil {
		return err
	}

	// Pass the actual period end so current_period_end is updated on cancel.
	if err := s.db.UpdateSubscriptionStatus(ctx, sub.ProviderSubID, "canceled", sub.CurrentPeriodEnd); err != nil {
		return err
	}
	s.events.Emit(ctx, sub.OrganizationID, EventSubscriptionCanceled, map[string]any{
		"provider": sub.Provider, "subscription_id": sub.ProviderSubID,
	})
	return nil
}

// UpgradePlan upgrades a subscription to a new plan with proration.
func (s *Service) UpgradePlan(ctx context.Context, subID string, newPlanID string) error {
	sub, err := s.db.GetSubscriptionByProviderSubID(ctx, subID)
	if err != nil {
		return err
	}

	provider, err := s.providerFor(sub.Provider)
	if err != nil {
		return err
	}

	// Calculate proration
	proration, err := s.CalculateProration(ctx, sub, newPlanID)
	if err != nil {
		return err
	}

	// Update subscription with provider
	if err := provider.UpdateSubscription(ctx, sub.ProviderSubID, newPlanID); err != nil {
		return err
	}

	// Update local subscription
	if err := s.db.UpdateSubscriptionPlan(ctx, sub.ID, newPlanID, proration); err != nil {
		return err
	}

	s.events.Emit(ctx, sub.OrganizationID, EventSubscriptionUpdated, map[string]any{
		"provider": sub.Provider, "subscription_id": sub.ProviderSubID,
		"new_plan_id": newPlanID, "proration_cents": proration,
	})

	return nil
}

// CalculateProration calculates the net proration amount (in cents) for a
// mid-cycle plan change.
//
// Business rule (consolidated):
//
//	unused_ratio  = days_remaining / total_days   (fraction of the current
//	                                                billing period not yet used)
//	credit_old    = round(old_price * unused_ratio)   // credit for the unused
//	                                                   // portion of the old plan
//	charge_new    = round(new_price * unused_ratio)   // prorated charge for the
//	                                                   // new plan over the same
//	                                                   // remaining window
//	proration     = charge_new - credit_old
//
// A positive result is an immediate additional charge (typical upgrade); a
// negative result is a credit issued to the customer (typical downgrade). Taxes
// are intentionally excluded here — proration operates on net plan prices and
// tax is applied downstream when the resulting invoice is generated.
func (s *Service) CalculateProration(ctx context.Context, sub *store.Subscription, newPlanID string) (int64, error) {
	// Fetch old plan price from DB or cache
	oldPlan, err := s.db.GetPlanByID(ctx, sub.PlanID)
	if err != nil {
		return 0, fmt.Errorf("fetch old plan: %w", err)
	}
	newPlan, err := s.db.GetPlanByID(ctx, newPlanID)
	if err != nil {
		return 0, fmt.Errorf("fetch new plan: %w", err)
	}

	// Calculate days in current period
	totalDays := sub.CurrentPeriodEnd.Sub(sub.CurrentPeriodStart).Hours() / 24
	if totalDays <= 0 {
		return 0, fmt.Errorf("invalid period duration")
	}

	now := time.Now()
	if now.After(sub.CurrentPeriodEnd) {
		now = sub.CurrentPeriodEnd
	}
	if now.Before(sub.CurrentPeriodStart) {
		now = sub.CurrentPeriodStart
	}
	daysRemaining := sub.CurrentPeriodEnd.Sub(now).Hours() / 24

	// Fraction of the billing period that remains unused.
	unusedRatio := daysRemaining / totalDays

	// Credit for the unused portion of the old plan plus the prorated charge for
	// the new plan over the same remaining window. Round each leg independently
	// to avoid truncation loss from the float64→int64 conversion.
	creditOld := int64(math.Round(float64(oldPlan.PriceCents) * unusedRatio))
	chargeNew := int64(math.Round(float64(newPlan.PriceCents) * unusedRatio))
	proration := chargeNew - creditOld

	s.logger.Info("calculated proration",
		zap.Int64("old_price", int64(oldPlan.PriceCents)),
		zap.Int64("new_price", int64(newPlan.PriceCents)),
		zap.Int64("days_remaining", int64(daysRemaining)),
		zap.Int64("credit_old_cents", creditOld),
		zap.Int64("charge_new_cents", chargeNew),
		zap.Int64("proration_cents", proration),
		zap.Bool("is_credit", proration < 0),
	)

	return proration, nil
}

// GenerateInvoice generates an invoice for a subscription.
// The provider is determined by looking up the subscription. Falls back to
// "stripe" if the subscription cannot be resolved (e.g., external subscription
// not yet synced to the local DB).
func (s *Service) GenerateInvoice(ctx context.Context, orgID uuid.UUID, subID string, amountCents int, currency string) (*store.Invoice, error) {
	// Try to determine the provider from the subscription record.
	provider := "stripe" // Default fallback
	sub, err := s.db.GetSubscriptionByProviderSubID(ctx, subID)
	if err == nil && sub != nil {
		provider = sub.Provider
	}

	invoice := &store.Invoice{
		ID:             uuid.New(),
		OrganizationID: orgID,
		SubscriptionID: strToUUID(subID),
		Provider:       provider,
		AmountCents:    amountCents,
		Currency:       currency,
		Status:         "pending",
		IssuedAt:       time.Now(),
		DueAt:          timePtr(time.Now().Add(30 * 24 * time.Hour)),
	}

	if err := s.db.InsertInvoice(ctx, invoice); err != nil {
		return nil, err
	}

	return invoice, nil
}

// GenerateInvoicePDF generates a professional PDF invoice with branding,
// tax breakdown, and optional NF-e details for Brazilian customers.
func (s *Service) GenerateInvoicePDF(invoice *store.Invoice) ([]byte, error) {
	// Build the PDFInvoice structure
	orgInfo := OrgInfo{
		Name:  "LastState Inc.",
		Email: "billing@laststate.dev",
	}

	var orgRecord *store.Organization
	if s.db != nil && invoice.OrganizationID != uuid.Nil {
		if org, err := s.db.GetOrgByID(context.Background(), invoice.OrganizationID); err == nil && org != nil {
			orgRecord = org
			orgInfo = OrgInfo{
				Name:    org.Name,
				Email:   org.Email,
				Address: org.Address,
				TaxID:   org.TaxID,
			}
		}
	}

	plan := &PlanInfo{
		Name:        "Subscription",
		Description: fmt.Sprintf("Subscription renewal (%s)", invoice.Provider),
		PriceCents:  invoice.AmountCents,
		Currency:    invoice.Currency,
		Interval:    "monthly",
	}

	// Calculate tax using jurisdiction-aware rates
	taxCents, _ := GenerateTax(invoice.AmountCents, 0.0, "")

	items := []InvoiceLine{
		{
			Description: plan.Name + " - " + plan.Interval,
			Quantity:    1,
			UnitPrice:   plan.PriceCents,
			Amount:      plan.PriceCents,
		},
	}

	taxRate := 0.0
	if invoice.AmountCents != 0 {
		taxRate = float64(taxCents) / float64(invoice.AmountCents)
	}

	nfe := GenerateNFEdetails(orgRecord, invoice)

	// Locale follows the invoice currency; tax stays jurisdiction-explicit
	// (GenerateTax with "" jurisdiction = 0) rather than guessed from text.
	locale := "en-US"
	if strings.EqualFold(invoice.Currency, "BRL") {
		locale = "pt-BR"
	}

	pdfInv := &PDFInvoice{
		Invoice:   invoice,
		Org:       orgInfo,
		Plan:      *plan,
		Items:     items,
		TaxRate:   taxRate,
		TaxAmount: int64(taxCents),
		Currency:  invoice.Currency,
		Locale:    locale,
		NFEnfe:    nfe,
	}

	return GeneratePDF(pdfInv)
}

// InvoicePDF renders the PDF bytes for a stored invoice.
func (s *Service) InvoicePDF(ctx context.Context, id uuid.UUID) ([]byte, error) {
	invoice, err := s.db.GetInvoiceByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.GenerateInvoicePDF(invoice)
}

// RecordUsage records a usage metric for an organization.
func (s *Service) RecordUsage(ctx context.Context, orgID uuid.UUID, metricName string, value int64, periodStart, periodEnd time.Time) error {
	_, err := s.RecordUsageIdempotent(ctx, orgID, metricName, value, periodStart, periodEnd, "")
	return err
}

// RecordUsageIdempotent records usage with a caller-supplied idempotency key.
// Replays with the same (org, metric, key) are dropped by the store
// (ErrUsageDuplicate → deduped=true, nil error) so retries never double-bill.
// Empty key preserves the legacy append-only behavior.
func (s *Service) RecordUsageIdempotent(ctx context.Context, orgID uuid.UUID, metricName string, value int64, periodStart, periodEnd time.Time, idemKey string) (bool, error) {
	metric := &store.UsageMetric{
		OrganizationID: orgID,
		MetricName:     metricName,
		Value:          value,
		PeriodStart:    periodStart,
		PeriodEnd:      periodEnd,
		IdempotencyKey: idemKey,
	}
	if err := s.db.InsertUsageMetric(ctx, metric); err != nil {
		if errors.Is(err, store.ErrUsageDuplicate) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

// GetUsage returns usage metrics for an organization in a period.
func (s *Service) GetUsage(ctx context.Context, orgID uuid.UUID, metricName string, periodStart, periodEnd time.Time) (int64, error) {
	return s.db.GetUsageTotal(ctx, orgID, metricName, periodStart, periodEnd)
}

// InitiateDunning starts the dunning process for a failed payment.
// The chain starts at schedule index 0 (Day 0, immediate retry); the worker
// advances it via AdvancePaymentAttempt.
func (s *Service) InitiateDunning(ctx context.Context, orgID uuid.UUID, invoiceID string) error {
	attempt := &store.PaymentAttempt{
		OrganizationID: orgID,
		InvoiceID:      strToUUID(invoiceID),
		AttemptNumber:  0,
		Status:         "pending",
		NextRetryAt:    timePtr(time.Now()),
	}
	return s.db.InsertPaymentAttempt(ctx, attempt)
}

// ListInvoices returns paginated invoices for an organization.
func (s *Service) ListInvoices(ctx context.Context, orgID uuid.UUID, page, perPage int) ([]store.Invoice, int64, error) {
	return s.db.ListInvoices(ctx, orgID, page, perPage)
}

// sendReceipt emails a payment receipt with the invoice PDF attached.
// Transport failures are logged, never fatal: the charge already happened.
func (s *Service) sendReceipt(ctx context.Context, invoice *store.Invoice) {
	org, err := s.db.GetOrgByID(ctx, invoice.OrganizationID)
	if err != nil || org == nil || org.Email == "" {
		return
	}
	pdf, err := s.GenerateInvoicePDF(invoice)
	if err != nil && s.logger != nil {
		s.logger.Warn("failed to render receipt pdf", zap.Error(err))
	}
	msg := ReceiptEmail(org.Email, org.Name, invoice.AmountCents, invoice.Currency, invoice.ID.String(), pdf)
	if err := s.mail.Send(ctx, msg); err != nil && s.logger != nil {
		s.logger.Info("receipt email not sent via smtp", zap.Error(err), zap.String("to", org.Email))
	}
}

// sendPaymentFailed emails a dunning nudge for a failed charge.
func (s *Service) sendPaymentFailed(ctx context.Context, orgID uuid.UUID, attempt int) {
	org, err := s.db.GetOrgByID(ctx, orgID)
	if err != nil || org == nil || org.Email == "" {
		return
	}
	msg := PaymentFailedEmail(org.Email, org.Name, attempt)
	if err := s.mail.Send(ctx, msg); err != nil && s.logger != nil {
		s.logger.Info("payment-failed email not sent via smtp", zap.Error(err), zap.String("to", org.Email))
	}
}

// GetInvoice returns a single invoice by ID.
func (s *Service) GetInvoice(ctx context.Context, id uuid.UUID) (*store.Invoice, error) {
	return s.db.GetInvoiceByID(ctx, id)
}

// BillUsageCycle generates base + overage invoices for every org holding an
// active subscription, for the given period. Intended for a scheduled job
// (see BILLING_USAGE_BILLING_ENABLED in main); the on-demand route
// POST /v1/usage/invoices covers manual runs. Idempotent per
// (org, subscription, period) via usage_billing_runs: reruns and concurrent
// schedulers converge on the first invoice instead of double-billing.
func (s *Service) BillUsageCycle(ctx context.Context, periodStart, periodEnd time.Time) (int, error) {
	orgIDs, err := s.db.ListOrganizationIDs(ctx, 1000)
	if err != nil {
		return 0, err
	}
	billed := 0
	for _, orgID := range orgIDs {
		subs, err := s.db.ListSubscriptionsByOrg(ctx, orgID)
		if err != nil {
			continue
		}
		for _, sub := range subs {
			if sub.Status != "active" {
				continue
			}
			_, _, err := s.GenerateUsageInvoice(ctx, orgID, sub.ProviderSubID, sub.PlanID, "usd", periodStart, periodEnd)
			if err != nil {
				if errors.Is(err, store.ErrUsageBillingDuplicate) {
					continue
				}
				if s.logger != nil {
					s.logger.Warn("usage billing failed", zap.String("org_id", orgID.String()), zap.Error(err))
				}
				continue
			}
			billed++
		}
	}
	return billed, nil
}

// GetInvoice returns a single invoice by ID.

// SweepTrials emails trial-ended notices, then converges expired trials.
// Kept here (not in main) so the mailer and the entitlement service meet
// in one tested place.
func (s *Service) SweepTrials(ctx context.Context, ent TrialConverger) (int, error) {
	if ent == nil {
		return 0, fmt.Errorf("entitlement service not configured")
	}
	orgs, err := s.db.ListExpiredTrials(ctx, 100)
	if err != nil {
		return 0, err
	}
	for _, org := range orgs {
		tier := org.Tier
		if tier == "" || tier == "trial" {
			tier = "free"
		}
		if org.Email != "" {
			if err := s.mail.Send(ctx, TrialEndedEmail(org.Email, org.Name, tier)); err != nil && s.logger != nil {
				s.logger.Info("trial-ended email not sent via smtp", zap.Error(err), zap.String("to", org.Email))
			}
		}
	}
	return ent.ExpireDueTrials(ctx)
}

// TrialConverger converges expired trials. Satisfied by
// *entitlement.Service; declared as an interface to avoid an import cycle
// in tests and keep billing decoupled.
type TrialConverger interface {
	ExpireDueTrials(ctx context.Context) (int, error)
}

// CreateInvoice records a manual invoice, optionally applying a coupon code.
// The coupon is validated and redeemed atomically against the new invoice.
// NOTE: provider is resolved from the subscription record inside
// GenerateInvoice (default stripe); the provider argument is kept for API
// compatibility and currently ignored.
func (s *Service) CreateInvoice(ctx context.Context, orgID uuid.UUID, subID string, amountCents int, currency, provider, couponCode string) (*store.Invoice, error) {
	if amountCents < 0 {
		return nil, fmt.Errorf("amount_cents must be non-negative")
	}
	if currency == "" {
		currency = "usd"
	}
	_ = provider
	discount := int64(0)
	var couponID *uuid.UUID
	if couponCode != "" {
		c, d, err := s.ValidateCoupon(ctx, couponCode, orgID, int64(amountCents), currency)
		if err != nil {
			return nil, err
		}
		discount = d.DiscountCents
		couponID = &c.ID
	}
	invoice, err := s.GenerateInvoice(ctx, orgID, subID, int(int64(amountCents)-discount), currency)
	if err != nil {
		return nil, err
	}
	if couponID != nil {
		if err := s.db.RedeemCoupon(ctx, *couponID, orgID, &invoice.ID); err != nil {
			return nil, fmt.Errorf("failed to redeem coupon: %w", err)
		}
	}
	return invoice, nil
}

// RefundInvoice refunds a paid invoice through its provider and marks it
// refunded locally. amountCents nil (or <=0) means full. Only Stripe
// invoices are refundable remotely today; other providers record the local
// status and report manual action needed.
func (s *Service) RefundInvoice(ctx context.Context, id uuid.UUID, amountCents *int64) (*store.Invoice, error) {
	invoice, err := s.db.GetInvoiceByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if invoice.Status == "refunded" {
		return invoice, nil
	}
	if invoice.Provider != "stripe" {
		return nil, fmt.Errorf("refunds for provider %q require manual action; local status unchanged", invoice.Provider)
	}
	providerInvID := ""
	if invoice.ProviderInvID != nil {
		providerInvID = *invoice.ProviderInvID
	}
	if providerInvID == "" {
		return nil, fmt.Errorf("invoice has no provider invoice id to refund")
	}
	sp, ok := s.stripe.(*StripeProvider)
	if !ok || s.stripe == nil {
		return nil, fmt.Errorf("stripe provider not configured")
	}
	if _, err := sp.RefundInvoice(providerInvID, amountCents); err != nil {
		return nil, err
	}
	if err := s.db.UpdateInvoiceStatusByID(ctx, id, "refunded", time.Now()); err != nil {
		return nil, err
	}
	invoice.Status = "refunded"
	s.events.Emit(ctx, invoice.OrganizationID, EventInvoiceFailed, map[string]any{
		"invoice_id": invoice.ID.String(), "status": "refunded",
	})
	return invoice, nil
}

// PortalSession creates a self-serve billing portal session for an org.
// Requires a Stripe customer id on the organization record.
func (s *Service) PortalSession(ctx context.Context, orgID uuid.UUID, returnURL string) (string, error) {
	org, err := s.db.GetOrgByID(ctx, orgID)
	if err != nil {
		return "", err
	}
	if org.StripeCustomerID == nil || *org.StripeCustomerID == "" {
		return "", fmt.Errorf("organization has no stripe customer")
	}
	sp, ok := s.stripe.(*StripeProvider)
	if !ok || s.stripe == nil {
		return "", fmt.Errorf("stripe provider not configured")
	}
	return sp.PortalSession(*org.StripeCustomerID, returnURL)
}

// CreateCoupon registers a new discount coupon (admin).
func (s *Service) CreateCoupon(ctx context.Context, c *store.Coupon) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	c.Code = strings.ToUpper(strings.TrimSpace(c.Code))
	if c.Code == "" {
		return fmt.Errorf("coupon code is required")
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now()
	}
	c.Active = true
	return s.db.CreateCoupon(ctx, c)
}

// RedeemCouponCode validates a coupon and records its redemption against an
// invoice, returning the discount in cents.
func (s *Service) RedeemCouponCode(ctx context.Context, orgID uuid.UUID, code string, amountCents int64, currency string, invoiceID *uuid.UUID) (int64, error) {
	c, discount, err := s.ValidateCoupon(ctx, code, orgID, amountCents, currency)
	if err != nil {
		return 0, err
	}
	if err := s.db.RedeemCoupon(ctx, c.ID, orgID, invoiceID); err != nil {
		return 0, err
	}
	return discount.DiscountCents, nil
}

// ListUsageMetrics returns usage metrics for an organization.
func (s *Service) ListUsageMetrics(ctx context.Context, orgID uuid.UUID, metricName string) ([]store.UsageMetric, error) {
	return s.db.ListUsageMetrics(ctx, orgID, metricName)
}

// Helper to convert string to UUID
func strToUUID(s string) *uuid.UUID {
	if s == "" {
		return nil
	}
	u, err := uuid.Parse(s)
	if err != nil {
		return nil
	}
	return &u
}

// timePtr returns a pointer to a time.Time value.
func timePtr(t time.Time) *time.Time {
	return &t
}
