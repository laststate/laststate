# Billing Service Roadmap

Vision and planned features for the LastState billing and entitlement platform.

## Current state

Billing Service handles subscriptions, invoicing, and feature provisioning
across multiple payment providers (Stripe, Mercado Pago, Crypto gateways)
for the LastState platform. Core billing logic is stable, but provider-specific
credentials and fiscal integrations are required before production use.

## Near-term (next 3 months)

- [x] **Automated dunning** — Full dunning pipeline with Day 0/1/3/7/14
  retry schedule, email notifications, and auto-suspension. Implemented in
  `internal/billing/dunning.go` with `DunningWorker`.
- [x] **Usage-based billing** — Metered overage pricing per metric
  (events by default) with block pricing, per-plan allowances derived from
  tier quotas, and `BILLING_OVERAGE_JSON` overrides. Implemented in
  `internal/billing/usage.go`; exposed via `GET /v1/usage/charges` and
  `GenerateUsageInvoice`.
- [x] **Tax calculation** — Jurisdiction-aware tax calculation with default
  rates for US, BR, EU, IN. Implemented in `internal/billing/pdf.go` as
  `GenerateTax()`.
- [x] **Invoice PDF generation** — PDF-1.4 invoices with line items and tax
  breakdown via `GeneratePDF`. NF-e remains out of scope: it requires a
  certified external fiscal provider and is never fabricated locally.

## Medium-term (3-6 months)

- [x] **Multi-currency** — Support for BRL, EUR, GBP with automatic FX
  conversion. Currency symbols and formatting in `internal/billing/pdf.go`.
- [x] **Coupon system** — Discount codes with percent or fixed-amount
  discounts, global and per-organization redemption caps, expiry, and atomic
  redemption. Implemented in `internal/billing/coupon.go` + `coupons` /
  `coupon_redemptions` tables; exposed via `POST /v1/coupons/validate`.
- [ ] **Team management** — Per-organization team members with role-based
  billing access.
- [x] **Billing API v2** — Outbound signed event webhooks
  (`subscription.created/updated/canceled`, `invoice.paid/failed`,
  `usage.limit_exceeded`, `dunning.escalated`) via
  `POST|GET /v1/webhook-endpoints` + `DELETE /v1/webhook-endpoints/{id}`;
  HMAC `X-LastState-Signature: sha256=<hex>`, 3x backoff retry.
  Implemented in `internal/billing/events.go` (`EventDispatcher`),
  emitted from create/cancel/upgrade; contracts in `docs/API_V2.md`.

## Long-term (6-12 months)

- [x] **Crypto payments** — Coinbase Commerce (`CryptoProvider`,
  `internal/billing/providers_other.go`) + NOWPayments (`NowPaymentsProvider`,
  `internal/billing/providers_nowpayments.go`, `NOWPAYMENTS_KEY`) with
  invoice/checkout, status mapping, and HMAC webhook verification.
  Crypto has no native recurring billing: each cycle mints a fresh invoice
  and renewal is driven by the billing scheduler.
- [ ] **Marketplace** — Third-party integrations and add-ons with revenue
  sharing.
- [ ] **Enterprise contracts** — Custom pricing, net-30 terms, and purchase
  order support.
- [x] **Mercado Pago Pix** — Instant Pix charges via `/v1/payments`
  (`payment_method_id=pix`) returning BR Code + QR base64
  (`CreatePixPayment`). Checkout Pro preferences + Preapproval
  subscription APIs already live in `MercadoPagoProvider`.
- [ ] **Analytics dashboard** — Revenue analytics, churn prediction, and
  customer lifetime value.

## Completed features

- [x] Subscription management (create, update, cancel, upgrade)
- [x] Multi-provider support (Stripe, Mercado Pago incl. Pix, Coinbase
  Commerce, NOWPayments — all live HTTP integrations with HMAC webhooks;
  configure via STRIPE_KEY / MP_KEY / CRYPTO_KEY / NOWPAYMENTS_KEY)
- [x] Webhook processing with idempotency
- [x] API token management with scoped access
- [x] Usage metrics tracking
- [x] Tier quotas and feature gates
- [x] Plan definitions with seed data
- [x] Proration calculations
- [x] Helm charts for Kubernetes deployment
- [x] REST API with documentation
- [x] Security policy with PCI-DSS compliance
- [x] Automated dunning pipeline (Day 0/1/3/7/14)
- [x] Jurisdiction-aware tax calculation
- [x] Multi-currency support (USD, BRL, EUR, GBP)
- [x] Usage-based billing (metered overage per metric)
- [x] Coupon system (percent/fixed discounts, redemption caps)

## Contributing

This roadmap is a living document. Suggestions and feedback are welcome via
[GitHub Issues](https://github.com/laststate/billing-service/issues) and
[GitHub Discussions](https://github.com/laststate/billing-service/discussions).
