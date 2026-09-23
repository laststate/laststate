# Gemini context for Billing Service

Billing Service is a payment and entitlement management backend written in Go.
It handles subscriptions, invoicing, and feature provisioning across multiple
payment providers (Stripe, Mercado Pago, Crypto gateways) for the LastState
platform.

## Architecture

- **Providers**: Stripe, Mercado Pago, Coinbase Commerce / NOWPayments
- **Storage**: PostgreSQL for subscriptions, invoices, usage, tokens
- **API**: REST with scoped bearer tokens and rate limiting
- **Webhooks**: Provider-specific signature verification (HMAC-SHA256)
- **Entitlements**: Integrates with Trace Admin API for feature provisioning

## Quick start

```bash
# Build
go build ./cmd/billing-service

# Run with SQLite (development)
go run ./cmd/billing-service --db sqlite --port 8080

# Run with PostgreSQL (staging)
go run ./cmd/billing-service --db postgres --db-url "postgres://..." --port 8080
```

## Key packages

- `internal/api/` — HTTP handlers and middleware
- `internal/billing/` — subscription, invoice, proration logic
- `internal/webhook/` — provider webhook processing
- `internal/entitlement/` — Trace API integration
- `internal/store/` — database operations
- `internal/schema/` — database schema

## Conventions

- Go 1.25+
- Structured logging with zap
- Interface-based design for testability
- Constant-time crypto comparisons
- All external APIs over TLS
