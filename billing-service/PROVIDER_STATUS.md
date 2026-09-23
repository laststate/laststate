# Billing Providers — Status 2026-08-26 (audit correction)

**Correction to prior audit:** `internal/billing/providers_other.go` is **NOT stubs**. Both providers make real HTTP calls.

## Providers

| Provider | File | Implementation | Test coverage |
|---|---|---|---|
| **Stripe** | `internal/billing/service.go` + `providers` | `stripe-go/v76`, checkout + subscription + webhook (HMAC SHA256); refunds full+partial (`refund.New`); disputes `charge.dispute.created|updated` → failed+dunning+`invoice.failed{dispute_id}` | `service_test.go` 25KB, `handlers_test.go` 23KB |
| **Mercado Pago** | `providers_other.go:22` `MercadoPagoProvider` | Preferences API (`/checkout/preferences`), Preapproval (`/preapproval/:id`), Pix (`/v1/payments` `payment_method_id=pix` → BR Code + QR base64, `providers_nowpayments.go`), **Boleto (`/v1/payments` `payment_method_id=bolbradesco` → barcode + `external_resource_url`, CPF/CNPJ via org `tax_id`, `CreateBoletoPayment`)**, HMAC `x-signature: hmac_sha256=` (`handler.go:142`); refunds/disputes **manual** (dashboard) | `handler_test.go` MP HMAC vectors + `providers_nowpayments_test.go` Pix success/validation |
| **Crypto (Coinbase Commerce)** | `providers_other.go:366` `CryptoProvider` | Charges API (`/charges`, `hosted_url`), `X-CC-Api-Key` + `X-CC-Version: 2018-03-22`, timeline `COMPLETED/RESOLVED/CANCELED` | `handler_test.go` `verifyCryptoSignature` |
| **Crypto (NOWPayments)** | `providers_nowpayments.go` `NowPaymentsProvider` | Invoice API (`/v1/invoice` → `invoice_url`), payment status (`/v1/payment/:id` → `finished/confirmed/...`), IPN `x-nowpayments-signature` HMAC-SHA256, wired via `Service.SetNowPayments` + `NOWPAYMENTS_KEY` (aliases: `nowpayments`, `now_payments`, `np`) | `providers_nowpayments_test.go` checkout/status/webhook/aliases |

## What the audit got wrong

* Previously flagged as "fail explicitly / boundaries with mock" — inaccurate. `CreateCheckoutSession` returns `init_point`/`hosted_url` from live API when `apiKey != ""`, else returns `not configured` error (correct for dev without keys).
* `Cancel/Update/GetSubscription` correctly fall back to **test/dev mode** only when `apiKey == ""` (allows `go test` without network). With keys, they hit real APIs (Preapproval / Charges).

## Remaining risk (real)

* **Bus factor 1** — `billing-service` has only 7 commits (`git log --oneline`). Code is solid but single owner (Matheus). Mitigate via `CODEOWNERS` + this doc.
* **Keys in compose** — `docker-compose.yml` now uses `${STRIPE_KEY:-sk_test_...}` etc. via root `.env.example`. Production MUST inject real keys via Vault/SSM; `sk_test_fake` is dev-only and never committed to prod.
* **Dunning is real but unproven at scale** — `dunning.go` implements Day 0/1/3/7/14 retries + email/SMS/webhook dispatchers, configurable via `BILLING_DUNNING_*`. Needs load test with Stripe test clock.

## Verification

```bash
go test ./... -count=1 -run TestMercadoPago   # MP HMAC + preference creation (mock baseURL)
go test ./... -count=1 -run TestCrypto       # Coinbase Commerce charge flow
go vet ./... && golangci-lint run
```
