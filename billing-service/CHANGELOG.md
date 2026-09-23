# Changelog

All notable changes to the LastState Billing Service.

## [Unreleased]

### Added
- **Hosted checkout** (`POST /v1/billing/checkout`): `poc` one-time $499
  (Stripe payment mode), `pilot`/`fleet`/`enterprise` subscriptions.
  Prices from `BILLING_PRICE_*` env, fail-closed without them.
  Go-live runbook: `docs/CHECKOUT.md`.
- **Canonical pricing** (`docs/PRICING.md`): Local $0 / Pilot $499 /
  Fleet $1,999 / Enterprise $7,999; POC mechanics and SLO month.

### Changed
- **License back to proprietary** - the Apache-2.0 switch broke the
  commercial model (this service is the money path). Terms unchanged
  apart from `Copyright 2026 LastState Contributors`.

### Fixed
- **CI lint green** - fixed the 12 `golangci-lint` findings failing `CI/lint`
  (`errcheck` on SMTP `Quit`, `gofmt`/`goimports` alignment, `gosec` G705
  false positive on the invoice PDF download, `govet` shadows, dead
  `provider` default in `CreateInvoice`, empty branch in the dispatcher
  test). Release checksum step now passes `actionlint` (SC2035).

## [0.2.0] - 2026-09-20

### Security
- **Removed local NF-e fabrication** - `GenerateNFEdetails` no longer mints
  Chave de Acesso/CNPJ locally; it returns nil until a certified fiscal
  provider populates `NFEDetails`. PDF still renders provider-supplied NF-e.
- **Fail-closed providers** - Mercado Pago / Coinbase / NOWPayments
  Cancel/Update/Get now error without an API key instead of faking success.
- **Fixed Mercado Pago overcharge** - checkout `unit_price` used cents
  (4900 instead of 49.00); subscriptions now use Preapproval with
  currency-unit amounts.
- **Fixed dunning DB breakage** - suspension wrote invalid `suspended`
  status; SMS went to TaxID. Now: terminal `max_retries_exceeded`,
  subscriptions flip to `past_due`, SMS needs a real phone.
- **Fixed stale CORS test** - middleware echoes the configured origin
  (never `*`); test updated.

### Added
- **SMTP email** (`BILLING_SMTP_*`): receipts with PDF attach, payment-failed
  nudges, trial ending/ended notices; log transport when unset.
- **Invoice surface**: `POST /v1/invoices` (optional coupon),
  `GET /v1/invoices/{id}/pdf`, `POST /v1/invoices/{id}/refund` (Stripe),
  `POST /v1/invoices/{id}/apply-coupon`.
- **Coupons**: `POST /v1/coupons` + `POST /v1/coupons/redeem` (validate +
  atomic redeem); invoice creation applies coupons.
- **Pix**: `POST /v1/payments` (Mercado Pago Pix QR + BR code).
- **Portal**: `POST /v1/portal/session` (Stripe Billing Portal).
- **Trials persisted**: `trial_ends_at` in DB, `POST /v1/trials/{org}/extend`,
  hourly sweeper with trial-ended email; trial end converges to an
  explicit tier (no more hardcoded `pro`).
- **Workers wired**: dunning worker + trial sweeper start in `main.go`
  (`BILLING_DUNNING_ENABLED`, `BILLING_TRIAL_SWEEP_INTERVAL`).
- **Webhook reactions**: receipts + `invoice.paid` emits, dunning +
  `invoice.failed` emits on failures, `checkout.session.completed`
  records local subscriptions, `trial_will_end` warnings,
  `usage.limit_exceeded` emits, `dunning.escalated` on suspension.
- **Usage billing route**: `POST /v1/usage/invoices` (base + overage).
- **Pricing defaults in USD** (were BRL for hobbyist/team), matching
  `docs/PRICING.md` and `ListTiers`.

## [0.6.0] - 2026-08-21

### Added
- **Usage-based billing** — metered overage pricing per metric (events by
  default). Per-plan allowances derive from tier quotas (events/day x 30);
  excess is billed in whole unit blocks. Overrides via `BILLING_OVERAGE_JSON`;
  startup fails fast on malformed pricing/overage config. New
  `GET /v1/usage/charges` endpoint and `GenerateUsageInvoice`.
- **Coupon system** — `coupons` + `coupon_redemptions` tables with percent or
  fixed-amount discounts (exactly one per coupon), global and per-organization
  redemption caps, expiry, and atomic guarded redemption. New
  `POST /v1/coupons/validate` endpoint.
- **Test suite** — full package coverage: api (auth, cors, handlers,
  ratelimit), billing (dunning, pdf, service, tax), entitlement, schema,
  store, webhook.
- **Configurable pricing** — `PricingConfig` externalized from the provider
  layer via `BILLING_PRICING_JSON` / `BILLING_PRICING_CONFIG`; now actually
  loaded at startup.

### Fixed
- **Rate limiter headers were garbage** — `string(rune(n))` emitted Unicode
  characters instead of numbers for `X-RateLimit-*`; replaced with
  `strconv`. IPv6 `RemoteAddr` now parsed via `net.SplitHostPort`, and expired
  client entries are swept so the map cannot grow unboundedly.
- **CORS `Access-Control-Max-Age`** had the same `string(rune())` bug.
- **CI unblocked** — actionlint workflow installed from the retired
  `yudai/actionlint` path (now `rhysd/actionlint`); golangci-lint migrated to
  the v2 config schema on the pinned v8 action (the v6 pin pointed at a
  nonexistent commit); workflows pin action SHAs and read the Go version from
  `go.mod`.
- Removed stray root `NOTES.txt` and misplaced chart-level `NOTES.txt`
  (the rendered copy in `templates/` is canonical).
- ROADMAP de-duplicated and reconciled with the actual implementation state.

## [0.5.0] - 2026-08-20

### Fixed
- **Lint hygiene** - `.golangci.yml` no longer both enables and disables `errcheck`;
  `errcheck` is enabled (with `check-blank: false`) and configured to ignore the
  benign `(*encoding/json.Encoder).Encode` return in HTTP responses. `golangci-lint run`
  is now clean.
- **Dead code removed** - unused `isBrazilianOrg` / `generateChaveAcesso` (pdf.go),
  unused `usageTotals` mock field, and unused `verifiedAt` webhook handler field.
- **Admin token fix** - `getTraceAdminToken` now reads the `TRACE_ADMIN_TOKEN`
  environment variable instead of returning an empty string, so entitlement
  provisioning/deprovisioning can authenticate against Trace.
- **Bugs fixed**:
  - `GenerateTax` returned the input `taxRate` parameter instead of the
    jurisdiction-overridden `rateUsed` (wrong effective rate on return).
  - `StripeProvider.VerifyWebhook` ignored the `json.Unmarshal` error for the
    event object.
  - Mercado Pago / crypto gateway `io.ReadAll` errors on the response body were
    swallowed.
  - Entitlement provisioning swallowed the Trace response-body read error and
    capitalized the error string (ST1005); now lower-cased and checked.
  - Dunning `sha256Hash` determinism test was a tautology (`h(x) != h(x)`); fixed
    to compare two evaluations.
  - Removed empty `NotifySMS` / `NotifyWebhook` no-op branches in the dunning worker.
  - Replaced deprecated `strings.Title` with a small `titleCase` helper.

## Known limitations (NOT yet production-stable)

This release is functional for Stripe-based flows and local operation, but the
following areas are explicitly **not** production-ready:

- **NF-e (Nota Fiscal Eletrônica, Brazilian e-invoice) generation returns nil** — Brazilian fiscal
  compliance is not implemented. `GenerateNFEdetails` is a placeholder; invoices
  carry no valid fiscal document.
- **Dunning notifications** — only the email channel is wired. SMS and webhook
  notification branches log "not sent" and are not connected to any provider.
- **MercadoPago and Crypto providers** — `GetSubscription` / `CancelSubscription` /
  `UpdateSubscription` / `UpgradePlan` return "not implemented".

## [0.4.0] — 2026-08-15

### Added
- **Hobbyist plan** — $9/mo, 100 devices, 50k events/day, 90-day retention
- **Team plan** — $49/mo, 1k devices, 500k events/day, SSO, audit logs, on-call
- **Enterprise plan** — $199/mo, unlimited everything, on-prem, SLA
- **Tier quotas table** — device/event/retention limits per tier
- **API tokens table** — per-org scoped tokens with expiry and scopes
- **Usage metering API** — `POST /v1/usage`, `GET /v1/usage`
- **Dunning management** — automatic retry, dunning emails, grace period
- **Proration logic** — mid-cycle plan changes with proportional billing
- **Invoice generation** — PDF + NF-e for Pro/Enterprise plans
- **Crypto gateway** — Coinbase Commerce / NOWPayments support
- **Mercado Pago integration** — Pix + Brazilian card checkout
- **Webhook idempotency** — deduplication by event_id across all providers

### Changed
- Plans table now seeds 4 tiers (free, hobbyist, team, enterprise)
- Organization tier column expanded to include 'hobbyist'

### Fixed
- Webhook signature verification for Mercado Pago (HMAC-SHA256)
- Webhook signature verification for Crypto gateways (Coinbase Commerce + NOWPayments)

## [0.3.0] — 2026-08-12

### Added
- Stripe subscription lifecycle (create, cancel, upgrade, proration)
- Basic webhook handling for Stripe events
- Entitlement provisioning/deprovisioning hooks
- Trial management (14-day auto-trial on signup)

## [0.2.0] — 2026-08-10

### Added
- Database schema migrations (PostgreSQL)
- Basic subscription CRUD API
- Health check endpoint

## [0.1.0] — 2026-08-08

### Added
- Initial project setup
- Go module, Dockerfile, docker-compose.yml
