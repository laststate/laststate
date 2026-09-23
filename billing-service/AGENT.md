# Billing Service agent guide

This is the canonical guide for AI-assisted work in Billing Service. It applies
to every contribution, whether the assistant is Codex, Claude, Copilot, Gemini,
Cursor, or another tool. Tool-specific entry points point here so that the
project has one source of truth.

Billing Service is a payment and entitlement management backend. It handles
subscriptions, invoicing, and feature provisioning across multiple payment
providers (Stripe, Mercado Pago, Crypto gateways) for the LastState platform.

## Architecture overview

```
┌─────────────┐     ┌──────────────────────┐     ┌─────────────┐
│  Clients    │────▶│   Billing Service    │────▶│  Trace API  │
│ (Web/App)   │     │  ┌────────────────┐  │     │ (entitle)   │
│             │     │  │ API handlers   │  │     └─────────────┘
│             │     │  │ Billing engine │  │
│             │     │  │ Webhook handler│  │     ┌─────────────┐
│             │     │  │ Store (PG)     │  │     │ Stripe      │
│             │     │  └────────────────┘  │     │ MercadoPago │
│             │     └──────────────────────┘     │ Crypto      │
└─────────────┘                                 └─────────────┘
```

Key packages:
- `internal/api/` — HTTP handlers, router, auth middleware, rate limiting
- `internal/billing/` — subscription, invoice, proration, dunning logic
- `internal/entitlement/` — feature provisioning via Trace Admin API
- `internal/schema/` — database schema (embedded SQL)
- `internal/store/` — PostgreSQL data access layer
- `internal/webhook/` — incoming webhook processing

## Invariants

- **Idempotent webhooks** — events are deduplicated by event ID (in-memory + DB).
- **Provider neutrality** — billing core is provider-agnostic; providers are swappable.
- **Token scoping** — API tokens have explicit scopes (`read`, `write`, `admin`,
  `billing`, `entitlements`, `analytics`).
- **No secret leakage** — API keys, webhook secrets, and payment data never in logs.
- **Constant-time comparisons** — all signature and token comparisons use
  `crypto/subtle.ConstantTimeCompare`.
- **Bounded input** — webhook payloads are bounded in size.
- **TLS everywhere** — all external API calls use TLS.

## Development setup

```bash
# Build
go build ./cmd/billing-service

# Run with SQLite (development)
go run ./cmd/billing-service --db sqlite --port 8080

# Run with PostgreSQL (staging)
go run ./cmd/billing-service --db postgres --db-url "postgres://..." --port 8080

# Run tests
go test ./...
go test -race ./...

# Lint
gofmt -l cmd internal
go vet ./...
```

## Testing

- Unit tests for billing calculations, webhook parsing, and store operations.
- Integration tests for webhook flows with mocked providers.
- Protocol vector tests for LEP encoding/decoding.

## Pull requests

1. Create a focused branch from `main`.
2. Add tests for billing calculations, webhook parsing, or state transitions.
3. Update documentation when touching public APIs or pricing.
4. Add a CHANGELOG entry for operator-facing changes.
5. Fill in the PR template with runtime evidence.

No Contributor License Agreement is required; contributions are accepted under
the repository's license.

By participating, you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).
