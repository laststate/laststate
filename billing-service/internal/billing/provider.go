// Package billing implements the payment processing layer.
package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v76"
	portalsession "github.com/stripe/stripe-go/v76/billingportal/session"
	"github.com/stripe/stripe-go/v76/checkout/session"
	"github.com/stripe/stripe-go/v76/customer"
	"github.com/stripe/stripe-go/v76/invoice"
	"github.com/stripe/stripe-go/v76/refund"
	"github.com/stripe/stripe-go/v76/subscription"
	"github.com/stripe/stripe-go/v76/webhook"
)

// PaymentProvider is the interface for payment gateway providers.
type PaymentProvider interface {
	// CreateCheckoutSession creates a payment checkout session.
	CreateCheckoutSession(ctx context.Context, orgID uuid.UUID, planID string, currency string) (string, error)
	// CreateSubscription creates a new subscription. payerEmail is the
	// customer email; providers that need it for recurring billing
	// (Mercado Pago Preapproval) require it non-empty, others ignore it.
	CreateSubscription(ctx context.Context, orgID uuid.UUID, planID string, payerEmail string) (string, error)
	// CancelSubscription cancels a subscription.
	CancelSubscription(ctx context.Context, subID string) error
	// UpdateSubscription updates a subscription (e.g., plan change).
	UpdateSubscription(ctx context.Context, subID string, planID string) error
	// GetSubscription retrieves subscription details.
	GetSubscription(ctx context.Context, subID string) (*SubscriptionInfo, error)
	// VerifyWebhook verifies a webhook signature.
	VerifyWebhook(payload []byte, sig []byte, endpointSecret string) (map[string]interface{}, error)
}

// SubscriptionInfo contains subscription details.
type SubscriptionInfo struct {
	ID                string    `json:"id"`
	CustomerID        string    `json:"customer_id"`
	PlanID            string    `json:"plan_id"`
	Status            string    `json:"status"`
	CurrentPeriodEnd  time.Time `json:"current_period_end"`
	CancelAtPeriodEnd bool      `json:"cancel_at_period_end"`
}

// WebhookPayload is the common structure for webhook events.
type WebhookPayload struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	CreatedAt int64           `json:"created_at"`
}

// StripeProvider implements PaymentProvider for Stripe.
type StripeProvider struct {
	apiKey string
}

// NewStripeProvider creates a new Stripe provider.
func NewStripeProvider(apiKey string) *StripeProvider {
	stripe.Key = apiKey
	return &StripeProvider{apiKey: apiKey}
}

func (p *StripeProvider) CreateCheckoutSession(ctx context.Context, orgID uuid.UUID, planID string, currency string) (string, error) {
	params := &stripe.CheckoutSessionParams{
		Mode:       stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		SuccessURL: stripe.String("https://app.laststate.io/billing/success"),
		CancelURL:  stripe.String("https://app.laststate.io/billing/cancel"),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(planID),
				Quantity: stripe.Int64(1),
			},
		},
		Metadata: map[string]string{
			"organization_id": orgID.String(),
			"plan_id":         planID,
		},
	}
	s, err := session.New(params)
	if err != nil {
		return "", fmt.Errorf("failed to create checkout session: %w", err)
	}
	return s.URL, nil
}

// CreatePaymentCheckoutSession creates a one-time payment checkout session
// (as opposed to CreateCheckoutSession, which is subscription mode).
// Used for one-off sales like the paid POC: no subscription is created,
// the customer pays once and the webhook records a paid invoice.
func (p *StripeProvider) CreatePaymentCheckoutSession(ctx context.Context, orgID uuid.UUID, priceID, successURL, cancelURL string) (string, error) {
	if p.apiKey == "" {
		return "", fmt.Errorf("stripe not configured: set STRIPE_KEY")
	}
	if priceID == "" {
		return "", fmt.Errorf("stripe price id is required")
	}
	params := &stripe.CheckoutSessionParams{
		Mode:       stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL: stripe.String(successURL),
		CancelURL:  stripe.String(cancelURL),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceID),
				Quantity: stripe.Int64(1),
			},
		},
		Metadata: map[string]string{
			"organization_id": orgID.String(),
			"plan_id":         "poc",
		},
	}
	params.Context = ctx
	s, err := session.New(params)
	if err != nil {
		return "", fmt.Errorf("failed to create payment checkout session: %w", err)
	}
	return s.URL, nil
}

func (p *StripeProvider) CreateSubscription(ctx context.Context, orgID uuid.UUID, planID string, payerEmail string) (string, error) {
	custParams := &stripe.CustomerParams{
		Metadata: map[string]string{
			"organization_id": orgID.String(),
		},
	}
	if payerEmail != "" {
		custParams.Email = stripe.String(payerEmail)
	}
	c, err := customer.New(custParams)
	if err != nil {
		return "", fmt.Errorf("failed to create customer: %w", err)
	}

	subParams := &stripe.SubscriptionParams{
		Customer: stripe.String(c.ID),
		Items:    []*stripe.SubscriptionItemsParams{{Price: stripe.String(planID)}},
	}
	sub, err := subscription.New(subParams)
	if err != nil {
		return "", fmt.Errorf("failed to create subscription: %w", err)
	}
	return sub.ID, nil
}

func (p *StripeProvider) CancelSubscription(ctx context.Context, subID string) error {
	_, err := subscription.Cancel(subID, &stripe.SubscriptionCancelParams{
		Prorate: stripe.Bool(true),
	})
	if err != nil {
		return fmt.Errorf("failed to cancel subscription: %w", err)
	}
	return nil
}

func (p *StripeProvider) UpdateSubscription(ctx context.Context, subID string, planID string) error {
	params := &stripe.SubscriptionItemsParams{
		ID:    stripe.String(subID),
		Price: stripe.String(planID),
	}
	_, err := subscription.Update(subID, &stripe.SubscriptionParams{
		Items: []*stripe.SubscriptionItemsParams{params},
	})
	if err != nil {
		return fmt.Errorf("failed to update subscription: %w", err)
	}
	return nil
}

func (p *StripeProvider) GetSubscription(ctx context.Context, subID string) (*SubscriptionInfo, error) {
	sub, err := subscription.Get(subID, &stripe.SubscriptionParams{
		Expand: []*string{stripe.String("data.default_payment_method")},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get subscription: %w", err)
	}
	status := string(sub.Status)
	var periodEnd time.Time
	if sub.CurrentPeriodEnd != 0 {
		periodEnd = time.Unix(sub.CurrentPeriodEnd, 0)
	}
	planID := ""
	if len(sub.Items.Data) > 0 && sub.Items.Data[0].Price != nil {
		planID = sub.Items.Data[0].Price.ID
	}
	return &SubscriptionInfo{
		ID:                sub.ID,
		CustomerID:        sub.Customer.ID,
		PlanID:            planID,
		Status:            status,
		CurrentPeriodEnd:  periodEnd,
		CancelAtPeriodEnd: sub.CancelAtPeriodEnd,
	}, nil
}

func (p *StripeProvider) VerifyWebhook(payload []byte, sig []byte, endpointSecret string) (map[string]interface{}, error) {
	event, err := webhook.ConstructEvent(payload, string(sig), endpointSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to verify webhook: %w", err)
	}
	// Return the full event object so callers can extract event ID, type, etc.
	data := map[string]interface{}{
		"id":     event.ID,
		"type":   string(event.Type),
		"object": string(event.Object),
	}
	if event.Data != nil && event.Data.Object != nil {
		if b, err := json.Marshal(event.Data.Object); err == nil {
			var objData map[string]interface{}
			if err := json.Unmarshal(b, &objData); err == nil {
				data["data"] = objData
			}
		}
	}
	return data, nil
}

// RefundInvoice refunds a Stripe invoice by retrieving its charge.
// amountCents nil (or <=0) means a full refund; otherwise a partial one.
// Only Stripe invoices carry retrievable charges; other providers return
// an explicit unsupported error instead of pretending.
func (p *StripeProvider) RefundInvoice(stripeInvoiceID string, amountCents *int64) (string, error) {
	if p.apiKey == "" {
		return "", fmt.Errorf("stripe not configured: api key is required")
	}
	inv, err := invoice.Get(stripeInvoiceID, &stripe.InvoiceParams{
		Expand: []*string{stripe.String("charge")},
	})
	if err != nil {
		return "", fmt.Errorf("failed to retrieve stripe invoice: %w", err)
	}
	if inv.Charge == nil || inv.Charge.ID == "" {
		return "", fmt.Errorf("stripe invoice %s has no charge to refund", stripeInvoiceID)
	}
	params := &stripe.RefundParams{Charge: stripe.String(inv.Charge.ID)}
	if amountCents != nil && *amountCents > 0 {
		params.Amount = stripe.Int64(*amountCents)
	}
	rf, err := refund.New(params)
	if err != nil {
		return "", fmt.Errorf("failed to create stripe refund: %w", err)
	}
	return rf.ID, nil
}

// PortalSession creates a Stripe Billing Portal session so customers
// self-serve payment methods, invoices, and cancellation.
func (p *StripeProvider) PortalSession(customerID, returnURL string) (string, error) {
	if p.apiKey == "" {
		return "", fmt.Errorf("stripe not configured: api key is required")
	}
	if customerID == "" {
		return "", fmt.Errorf("stripe customer id is required")
	}
	if returnURL == "" {
		returnURL = "https://app.laststate.io/billing"
	}
	sess, err := portalsession.New(&stripe.BillingPortalSessionParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(returnURL),
	})
	if err != nil {
		return "", fmt.Errorf("failed to create portal session: %w", err)
	}
	return sess.URL, nil
}
