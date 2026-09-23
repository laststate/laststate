# Customer Portal — self-serve billing (Stripe-only)

`POST /v1/portal/session` → Stripe Billing Portal (`provider.go:PortalSession`
via `billingportal/session`). Other providers: explicit error
(`organization has no stripe customer` / `stripe provider not configured`);
manage in the provider dashboard.

## Request

```json
{"organization_id": "uuid", "return_url": "https://app.laststate.io/billing"}
```

Response `200`: `{"url": "https://billing.stripe.com/session/..."}`.
Errors `400 portal_failed` (no Stripe customer or missing provider);
`400 invalid_organization_id` / `invalid_request` for invalid payloads.

## What the customer does in the portal

- Swap card / payment method.
- View/download invoices (PDF also via `GET /v1/invoices/{id}/pdf`).
- Cancel/change subscription (reflected via webhook
  `customer.subscription.updated|deleted`).
- View disputes: they show up as failures + dunning; contest in the Stripe dashboard
  (webhook `charge.dispute.created|updated` only mirrors local state).

## Limits

- Only Stripe has a portal. MP (Pix/boleto), Coinbase, NOWPayments: no portal —
  Pix/boleto are one-off (`POST /v1/payments`); partial refunds/disputes outside
  Stripe are manual (see `API.md#refund`, `WEBHOOKS.md`).
- Prerequisite: `organizations.stripe_customer_id` filled (created at
  Stripe checkout/subscription).

## Local test

```bash
curl -X POST localhost:8080/v1/portal/session \
  -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"organization_id":"<uuid>","return_url":"https://app.laststate.io/billing"}'
# no customer → 400 portal_failed "organization has no stripe customer"
```
