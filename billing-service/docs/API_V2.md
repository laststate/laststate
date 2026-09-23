# Billing API v2 — outbound event webhooks

v1 is poll-based (`GET /v1/subscriptions`, `GET /v1/invoices`). v2 pushes
signed event notifications to customer HTTPS endpoints so integrations stop
polling. Both versions run side by side; v1 is unchanged.

## Events

`subscription.created | subscription.updated | subscription.canceled |
invoice.paid | invoice.failed | usage.limit_exceeded | dunning.escalated`

Wired today: created/canceled/updated emit from `Service.CreateSubscription`,
`CancelSubscription`, `UpgradePlan`. Extend by calling
`svc.Events().Emit(ctx, orgID, billing.EventInvoicePaid, data)` wherever the
state transition commits (same pattern).

## Manage endpoints (auth required)

```bash
curl -X POST localhost:8080/v1/webhook-endpoints \
  -H 'Authorization: Bearer <key>' -H 'Content-Type: application/json' -d '{
    "organization_id": "<org-uuid>",
    "url": "https://example.com/hooks/billing",
    "secret": "<hmac-secret>",
    "events": ["invoice.paid", "subscription.canceled"]
  }'
# "events": [] or omitted = all events for the org.

curl 'localhost:8080/v1/webhook-endpoints?organization_id=<org-uuid>' \
  -H 'Authorization: Bearer <key>'

curl -X DELETE localhost:8080/v1/webhook-endpoints/<endpoint-uuid> \
  -H 'Authorization: Bearer <key>'   # → 204
```

Inbound provider webhooks (unchanged): `POST /webhooks/stripe`,
`/webhooks/mercado-pago`, `/webhooks/crypto`, `/webhooks/nowpayments`
(NOWPayments IPN also accepted on `/webhooks/crypto` via
`x-nowpayments-signature`).

## Verify deliveries

Every POST carries the raw JSON event, plus:

```text
X-LastState-Event: invoice.paid
X-LastState-Signature: sha256=<hex hmac-sha256(secret, raw-body)>
```

```python
import hmac, hashlib
digest = hmac.new(secret.encode(), raw_body, hashlib.sha256).hexdigest()
assert hmac.compare_digest("sha256=" + digest, request.headers["X-LastState-Signature"])
```

Retries: 3 attempts, backoff 1s → 4s → 16s. Non-2xx keeps the last error in
logs; billing writes are never blocked by delivery failure.

## Production note

The default dispatcher store is in-memory (fine for tests/dev and a single
replica). For multi-replica prod, persist `WebhookEndpoint` rows in Postgres
(same shape as the struct) and replay missed events from the webhook-events
table — the `EventDispatcher` seam (`SetEventDispatcher`) is designed for
that swap without touching handlers.
