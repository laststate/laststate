# LastState Billing Service API

## Authentication

All authenticated endpoints require an API key or bearer token:

```
Authorization: Bearer <api-key>
X-API-Key: <api-key>
```

## Endpoints

### Subscriptions

#### `POST /v1/subscriptions`

Create a new subscription.

```json
{
  "organization_id": "uuid",
  "provider": "stripe|mercado_pago|crypto",
  "plan_id": "hobbyist"
}
```

Response: `201 Created`
```json
{"subscription_id": "sub_xxx"}
```

#### `GET /v1/subscriptions/{id}`

Get subscription details.

#### `DELETE /v1/subscriptions/{id}`

Cancel subscription (end of period).

#### `POST /v1/subscriptions/{id}/upgrade`

Upgrade plan with proration.

```json
{"plan_id": "team"}
```

### Invoices

#### `GET /v1/invoices`

List invoices (paginated).

Query params: `organization_id`, `page`, `per_page`

#### `POST /v1/invoices`

Create a manual invoice, optionally applying a coupon.

```json
{"organization_id": "uuid", "subscription_id": "uuid-or-provider-id", "amount_cents": 4900, "currency": "usd", "provider": "stripe", "coupon_code": "LAUNCH20"}
```

#### `GET /v1/invoices/{id}`

Get invoice details.

#### `GET /v1/invoices/{id}/pdf`

Render the invoice PDF (`application/pdf`).

#### `POST /v1/invoices/{id}/refund`

Refund via the provider and mark refunded locally.

- `amount_cents` (optional): `null`/omitted/`<=0` = full; `>0` = partial.
- **Stripe**: full + partial via `refund.New{Charge, Amount}` (remote + local
  `status=refunded`). Idempotent: already-`refunded` returns current record.
- **mercado_pago/crypto/NOWPayments**: remote refund requires manual action in
  the provider dashboard; API returns `400 refund_failed` and local status is
  unchanged (`service.go:RefundInvoice`).
- Disputes: `charge.dispute.created|updated` (Stripe) marks the invoice
  `failed`, starts dunning and emits `invoice.failed` with
  `{dispute_id, dispute_status}`; MP/crypto have no dispute webhook
  (see `WEBHOOKS.md`).

```json
{"amount_cents": 1000}
```

#### `POST /v1/invoices/{id}/apply-coupon`

Validate and redeem a coupon against an invoice.

```json
{"code": "LAUNCH20"}
```

### Payments

#### `POST /v1/payments`

One-off payment via Mercado Pago (`/v1/payments`).

- `method: "pix"` (default): `payment_method_id=pix` → QR + BR code
  (`CreatePixPayment`).
- `method: "boleto"|"bolbradesco"`: `payment_method_id=bolbradesco` →
  `{payment_id, status, barcode, bank_url, due_date}`
  (`CreateBoletoPayment`; `MP_KEY` required; org needs `email` + `tax_id`
  11-digit CPF / 14-digit CNPJ; idempotency `org:plan:YYYYMMDD`).

```json
{"organization_id": "uuid", "plan_id": "team", "method": "pix"}
{"organization_id": "uuid", "plan_id": "team", "method": "boleto"}
```

### Coupons

#### `POST /v1/coupons`

Register a coupon (admin).

#### `POST /v1/coupons/validate`

Validate without redeeming (existing).

#### `POST /v1/coupons/redeem`

Validate and record redemption, optionally linked to an invoice.

```json
{"code": "LAUNCH20", "organization_id": "uuid", "amount_cents": 4900, "currency": "usd", "invoice_id": "uuid"}
```

### Customer portal

#### `POST /v1/portal/session`

Stripe Billing Portal session for self-serve payment methods/invoices.
Stripe-only; other providers return an explicit error (manage those in
their dashboards). Full self-serve guide: [`PORTAL.md`](PORTAL.md).

```json
{"organization_id": "uuid", "return_url": "https://app.laststate.io/billing"}
```

### Hosted checkout

#### `POST /v1/billing/checkout`

Hosted checkout URL for plans. `poc` is a one-time $499 payment (Stripe
payment mode); `pilot`/`fleet`/`enterprise` are subscriptions. Prices come
from env (`BILLING_PRICE_*` = Stripe Price IDs); without them the endpoint
fails closed naming the missing variable. Go-live runbook:
[`CHECKOUT.md`](CHECKOUT.md).

```json
{"organization_id": "uuid", "plan": "poc", "provider": "stripe", "currency": "usd"}
{"organization_id": "uuid", "plan": "fleet", "provider": "stripe"}
```

### Usage

#### `POST /v1/usage`

Record usage metrics. Idempotent via `Idempotency-Key` header (wins) or
`idempotency_key` JSON field: replays with the same
`(organization_id, metric_name, key)` return `200 {"deduped":true}` instead
of double-recording. Empty key = legacy append-only.

```json
{
  "organization_id": "uuid",
  "metrics": {
    "events": 1500,
    "devices": 42
  },
  "period_start": "2026-09-01T00:00:00Z",
  "period_end": "2026-10-01T00:00:00Z",
  "idempotency_key": "ingest-2026-09-0001"
}
```

#### `GET /v1/usage`

Get usage metrics.

Query params: `organization_id`, `metric_name`

#### `GET /v1/usage/charges`

Base + overage charges for a plan/period (emits `usage.limit_exceeded` when over quota).

Query params: `organization_id`, `plan_id`, `metric_name`, `period_start`, `period_end` (RFC3339)

#### `POST /v1/usage/invoices`

Create the base + overage invoice for a period. Idempotent per
`(organization_id, subscription_id, period)`: replays return the first
invoice with `200 {"invoice":…, "charges":…, "deduped":true}`.

```json
{"organization_id": "uuid", "subscription_id": "uuid-or-provider-id", "plan_id": "team", "currency": "usd", "period_start": "2026-09-01T00:00:00Z", "period_end": "2026-10-01T00:00:00Z"}
```

### Dunning

#### `POST /v1/dunning/{org_id}/initiate`

Start dunning process for failed payment.

### Entitlements

#### `GET /v1/entitlements/{org_id}`

Read current entitlements for an org (proxies Trace Admin API
`GET /v1/admin/organizations/{id}/entitlements`). See
[`ENTITLEMENTS.md`](ENTITLEMENTS.md#63) for shape. `502 trace_unavailable`
when Trace is unreachable/unconfigured.

#### `POST /v1/entitlements/{org_id}/provision`

Activate features for an organization.

```json
{"tier": "team"}
```

#### `POST /v1/entitlements/{org_id}/deprovision`

Remove features when subscription ends.

### Trials

#### `POST /v1/trials/{org_id}/start`

Start a trial period.

Query params: `days` (default: 14)

#### `POST /v1/trials/{org_id}/end?tier={plan}`

End trial, converging onto an explicit tier (default `free`).

#### `POST /v1/trials/{org_id}/extend?days=N`

Re-provision the trial tier and push the persisted deadline out (default 14 days).

### Webhooks

#### `POST /webhooks/stripe`

Stripe webhook endpoint (no auth).

#### `POST /webhooks/mercado-pago`

Mercado Pago webhook endpoint (HMAC-SHA256 verified).

#### `POST /webhooks/crypto`

Crypto gateway webhook endpoint (HMAC verified).

#### `POST /webhooks/nowpayments`

NOWPayments IPN endpoint (alias of `/webhooks/crypto`; accepts
`x-nowpayments-signature`).

### Billing API v2 — outbound event webhooks

Push instead of poll — full contracts in [`API_V2.md`](API_V2.md):

- `POST /v1/webhook-endpoints` — register `{organization_id, url, secret, events[]}`
- `GET /v1/webhook-endpoints?organization_id=…` — list (secrets never echoed)
- `DELETE /v1/webhook-endpoints/{id}` — remove (204)

## Error Responses

All errors follow this format:

```json
{
  "error": {
    "code": "invalid_plan",
    "message": "Plan 'starter' does not exist"
  }
}
```

Common error codes:
- `invalid_plan` — Plan ID not found
- `subscription_exists` — Organization already has active subscription
- `trial_already_active` — Trial already started
- `payment_failed` — Payment provider error
- `insufficient_funds` — Card declined / insufficient balance
