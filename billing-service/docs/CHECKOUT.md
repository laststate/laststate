# Taking money — go-live runbook

How to go from "code ready" to "customer paid". No keys live in this repo;
everything below is env-driven. Until the env vars exist, checkout fails
closed with the exact variable name in the error.

## 1. Stripe setup (test mode first)

1. Create the products/prices in the [Stripe dashboard](https://dashboard.stripe.com/test/products)
   (test mode toggle ON):
   - POC — one-time $499 USD payment (plan `poc`)
   - Pilot — recurring $499/mo (plan `pilot`)
   - Fleet — recurring $1,999/mo (plan `fleet`)
   - Enterprise — recurring $7,999/mo (plan `enterprise`)
2. Copy each Price ID (`price_...`) into env:

```bash
STRIPE_KEY=sk_test_...
STRIPE_WEBHOOK_SECRET=whsec_...
BILLING_PRICE_POC=price_...
BILLING_PRICE_PILOT=price_...
BILLING_PRICE_FLEET=price_...
BILLING_PRICE_ENTERPRISE=price_...
BILLING_CHECKOUT_SUCCESS_URL=https://app.laststate.io/billing/success
BILLING_CHECKOUT_CANCEL_URL=https://app.laststate.io/billing/cancel
```

3. Register the webhook endpoint in Stripe (test mode):
   `https://<billing-host>/webhooks/stripe`, events
   `checkout.session.completed`, `invoice.paid`, `invoice.payment_failed`,
   `customer.subscription.deleted`. Put the signing secret in
   `STRIPE_WEBHOOK_SECRET`.

## 2. Test the money path end-to-end (no real charge)

```bash
# 1. create a checkout session for the POC (one-time $499)
curl -X POST $BILLING_URL/v1/billing/checkout \
  -H "Authorization: Bearer $ADMIN_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"organization_id": "<org-uuid>", "plan": "poc", "provider": "stripe"}'
# → {"checkout_url": "https://checkout.stripe.com/c/pay/..."}
# 2. pay with 4242 4242 4242 4242, any future expiry, any CVC
# 3. confirm the webhook recorded it (invoice paid, subscription anchored)
```

For local webhook delivery, use the Stripe CLI:
`stripe listen --forward-to localhost:8080/webhooks/stripe`.

## 2b. Mock mode (no Stripe account yet)

Without any key, the service fails closed — that is the test: every
missing variable surfaces by name. To also validate the Stripe request
shape (line items, mode, metadata) with zero network and zero account, run
[stripe-mock](https://github.com/stripe/stripe-mock) standalone:

```bash
docker run -d -p 12111:12111 -p 12112:12112 stripe/stripe-mock:latest
curl http://localhost:12111/v1/checkout/sessions -u sk_test_mock: \
  -d mode=payment \
  -d "line_items[0][price]=price_mock_123" \
  -d "line_items[0][quantity]=1" \
  --data-urlencode success_url=https://app.laststate.io/billing/success \
  --data-urlencode cancel_url=https://app.laststate.io/billing/cancel
# → fake session JSON; proves the exact params our provider.go sends.
```

This proves the request contract; it does **not** prove money movement.
That needs §2 with test keys, then §3 in `GOLIVE.md`.

## 3. Go live

1. Flip the dashboard out of test mode, recreate the four prices in live
   mode, replace `sk_test_`/`whsec_(test)` with live values.
2. Re-run the POC checkout above with a real card for $499.
3. Refund policy for the paid POC: no durable ACK in 1 week → full refund
   via `POST /v1/invoices/{id}/refund` (Stripe) or the dashboard.

## 4. Other providers

- **Mercado Pago / Coinbase / NOWPayments**: `POST /v1/billing/checkout`
  accepts `provider: "mp" | "crypto" | "nowpayments"` and passes `plan`
  straight to their hosted checkout (no price map needed). Each fails
  closed naming its key (`MP_KEY`, `CRYPTO_KEY`, `NOWPAYMENTS_KEY`).
- One-off Pix/boleto stays on `POST /v1/payments` (no checkout session).

## Env reference

| Var | Required for | Without it |
|-----|--------------|------------|
| `STRIPE_KEY` | any Stripe checkout | `stripe not configured: set STRIPE_KEY` |
| `BILLING_PRICE_POC` | POC $499 checkout | error names the var |
| `BILLING_PRICE_PILOT/FLEET/ENTERPRISE` | plan subscriptions | error names the var |
| `BILLING_CHECKOUT_SUCCESS_URL/CANCEL_URL` | redirect after pay | defaults to app.laststate.io |
| `STRIPE_WEBHOOK_SECRET` | webhook verification | webhooks rejected |
