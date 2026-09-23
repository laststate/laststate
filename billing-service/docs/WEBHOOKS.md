# Webhook Handling

This document describes how Billing Service processes incoming webhook events
from payment providers, including event mapping, signature verification,
idempotency, and testing.

## Overview

Billing Service receives webhook events from three payment providers and
processes them through a unified pipeline:

1. **Receive** — HTTP POST to provider-specific endpoint
2. **Verify** — Signature verification (provider-specific)
3. **Parse** — JSON parsing and validation
4. **Idempotency check** — Deduplicate by event ID
5. **Dispatch** — Route to provider-specific handler
6. **Persist** — Store for audit and replay
7. **React** — Update subscriptions, invoices, trigger provisioning

## Event Mapping

### Stripe

| Stripe Event | Internal Action |
|-------------|----------------|
| `invoice.paid` | Mark invoice as paid, send receipt + PDF, emit `invoice.paid` |
| `invoice.payment_failed` | Mark invoice failed, start dunning, email nudge, emit `invoice.failed` |
| `customer.subscription.updated` | Update subscription status/dates |
| `customer.subscription.deleted` | Cancel subscription, deprovision features |
| `customer.subscription.trial_will_end` | Send trial ending warning |
| `payment_intent.payment_failed` | Start dunning process (via linked subscription when present) |
| `checkout.session.completed` | Create local subscription record from checkout metadata |
| `charge.dispute.created` | Mark invoice `failed`, start dunning, email nudge, emit `invoice.failed` with `{dispute_id, dispute_status}` |
| `charge.dispute.updated` | Same as `created` (status refresh for ops follow-up) |

**Endpoint:** `POST /webhooks/stripe`

**Signature:** `Stripe-Signature` header, verified against `whsec_` endpoint secret.

### Mercado Pago

| Mercado Pago Event | Internal Action |
|-------------------|----------------|
| `payment.updated` | Update invoice status based on `status_code` (covers Pix + boleto) |
| `payment.updated` (status 1/200) | Mark invoice as paid, send receipt + PDF, emit `invoice.paid` |
| `payment.updated` (other) | Mark invoice as failed, start dunning, emit `invoice.failed` |
| _(no dispute event)_ | MP has no chargeback webhook mapped; handle disputes in the MP dashboard (refund path is manual — see `API.md`) |

**Endpoint:** `POST /webhooks/mercado-pago`

**Signature:** `x-signature` header, format `hmac_sha256=<hex>`, verified with webhook secret.

### Crypto Gateway (Coinbase Commerce / NOWPayments)

| Crypto Event | Internal Action |
|-------------|----------------|
| `charge:confirmed` | Mark invoice as paid via provider invoice ID, send receipt, emit `invoice.paid` |
| `charge:expired` | Mark invoice as failed, emit `invoice.failed` |
| `charge:failed` | Mark invoice as failed, emit `invoice.failed` (one-off charges: no dunning chain) |

**Endpoint:** `POST /webhooks/crypto`

**Signature:** `x-cb-signature` (Coinbase: `sha256=<hex>`) or `x-nowpayments-signature` (NOWPayments: hex HMAC-SHA256).

## Signature Verification

### Stripe

```
POST body ──▶ HMAC-SHA256(whsec_secret, timestamp + "." + body)
                    │
                    ▼
         Compare with Stripe-Signature header
```

- Header format: `t=<timestamp>,v1=<signature>`
- Timestamp must be within 5 minutes of server time.
- Uses `crypto/subtle.ConstantTimeCompare` to prevent timing attacks.

### Mercado Pago

```
POST body ──▶ HMAC-SHA256(secret, body)
                    │
                    ▼
         Compare with x-signature: hmac_sha256=<hex>
```

- Signature is hex-encoded HMAC-SHA256 of the raw request body.
- Verified with `crypto/subtle.ConstantTimeCompare`.

### Crypto Gateway

**Coinbase Commerce:**
```
POST body ──▶ HMAC-SHA256(secret, body)
                    │
                    ▼
         Compare with x-cb-signature: sha256=<hex>
```

**NOWPayments:**
```
POST body ──▶ HMAC-SHA256(secret, body)
                    │
                    ▼
         Compare with x-nowpayments-signature: <hex>
```

## Idempotency

All webhook events are deduplicated by `event_id`:

1. **In-memory cache** — fast path for events within the same process lifetime.
2. **Database** — persistent store for durability across restarts.

If an event is already processed, the handler returns `200 OK` immediately
without re-processing. This prevents duplicate charges, double provisioning,
and inconsistent state.

```go
// Flow:
if inMemoryCache.Has(eventID) { return 200 }
if db.Has(eventID) { return 200 }
// ... process ...
db.Insert(eventID)
inMemoryCache.Store(eventID)
```

## Retry and Failure Handling

### Provider-side retries

All providers retry failed webhook deliveries:
- **Stripe**: Up to 4 days, exponential backoff
- **Mercado Pago**: Up to 3 days, 3 attempts
- **Crypto**: Provider-dependent, typically 24-48 hours

### Our handling

- Failed verification returns `401 Unauthorized` — providers will retry.
- Internal errors return `500 Internal Server Error` — providers will retry.
- Successful processing returns `200 OK` — no retry.
- Duplicate events return `200 OK` — idempotent, no double processing.

### Monitoring

- Failed verifications are logged with source IP for anomaly detection.
- Unhandled event types are logged for review.
- Payment failures trigger the dunning pipeline.

## Testing

### Stripe

```bash
# Install Stripe CLI
brew install stripe/stripe-cli/stripe

# Start webhook forwarding (from billing-service root)
stripe listen --forward-to localhost:8080/webhooks/stripe

# Trigger test events
stripe trigger invoice.paid
stripe trigger customer.subscription.updated
```

### Mercado Pago

Use the [Mercado Pago Developer Dashboard](https://developers.mercadopago.com/)
to send test webhooks. Configure the webhook URL to point to your relay or
localhost tunnel.

### Crypto Gateway

Both Coinbase Commerce and NOWPayments provide webhook testing in their
developer dashboards. Use sandbox/test mode accounts.

### Local testing

No mock flag exists — providers fail closed without keys. For local webhook
exercises, point the service at `httptest`-style stubs or use each
provider's dashboard test mode (Stripe CLI `listen --forward-to`,
MP/Coinbase sandboxes):

```bash
# Send a test webhook (invalid signature → 4xx, proving verification runs)
curl -X POST http://localhost:8080/webhooks/stripe \
  -H "Content-Type: application/json" \
  -H "Stripe-Signature: t=1234567890,v1=invalid" \
  -d '{"id":"evt_test","type":"invoice.paid"}'
```

## Webhook Payload Examples

See [docs/examples/](./examples/) for sample payloads from each provider.
