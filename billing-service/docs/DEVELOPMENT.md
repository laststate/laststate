# Development Setup

Guide for setting up a local development environment for Billing Service.

## Prerequisites

- Go 1.25+
- PostgreSQL 15+ (for full feature testing)
- Stripe CLI (for webhook testing)
- Docker (for containerized development)

## Quick start

```bash
# Clone and build
git clone https://github.com/laststate/billing-service.git
cd billing-service
go build ./cmd/billing-service

# Run with SQLite (no external dependencies)
go run ./cmd/billing-service --db sqlite --port 8080

# Run with PostgreSQL
docker run -d --name billing-postgres \
  -e POSTGRES_DB=billing \
  -e POSTGRES_USER=billing \
  -e POSTGRES_PASSWORD=billing \
  -p 5432:5432 postgres:16-alpine

go run ./cmd/billing-service --db postgres \
  --db-url "postgres://billing:billing@localhost:5432/billing?sslmode=disable" \
  --port 8080
```

## Running tests

```bash
# All tests
go test ./...

# With race detector
go test -race ./...

# Specific packages
go test ./internal/billing -v
go test ./internal/webhook -v
go test ./internal/api -v

# With coverage
go test -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## Webhook testing

### Stripe

```bash
# Install Stripe CLI
brew install stripe/stripe-cli/stripe

# Forward webhooks to local server
stripe listen --forward-to localhost:8080/webhooks/stripe

# Trigger test events
stripe trigger invoice.paid
stripe trigger customer.subscription.updated
stripe trigger customer.subscription.deleted
```

### Mercado Pago

Use the [Mercado Pago Developer Dashboard](https://developers.mercadopago.com/)
to send test webhooks. Configure the webhook URL to your relay or localhost
tunnel.

### Crypto Gateway

Both Coinbase Commerce and NOWPayments provide webhook testing in their
developer dashboards. Use sandbox/test mode accounts.

## Configuration

All configuration is via command-line flags or environment variables:

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `--db` | `BILLING_DB` | `sqlite` | Database backend |
| `--db-url` | `BILLING_DB_URL` | — | Database connection string |
| `--port` | `BILLING_PORT` | `8080` | HTTP server port |
| `--stripe-key` | `STRIPE_KEY` | — | Stripe API key |
| `--stripe-whsec` | `STRIPE_WHSEC` | — | Stripe webhook secret |
| `--mp-secret` | `MP_SECRET` | — | Mercado Pago webhook secret |
| `--crypto-secret` | `CRYPTO_SECRET` | — | Crypto gateway secret |

## Project structure

```
billing-service/
├── cmd/billing-service/main.go    # Entry point
├── internal/
│   ├── api/                       # HTTP handlers, router, auth
│   │   ├── auth.go                # Authentication middleware
│   │   ├── cors.go                # CORS configuration
│   │   ├── handlers.go            # API request handlers
│   │   ├── ratelimit.go           # Rate limiting
│   │   └── router.go              # Route registration
│   ├── billing/                   # Core billing logic
│   │   ├── provider.go            # Payment provider interface
│   │   ├── providers_other.go     # MP and Crypto providers
│   │   └── service.go             # Subscription, invoice, proration
│   ├── entitlement/               # Feature provisioning
│   ├── schema/                    # Database schema
│   ├── store/                     # Database access layer
│   └── webhook/                   # Webhook processing
├── deploy/helm/billing-service/   # Helm charts
├── docs/                          # Documentation
│   ├── API.md                     # API reference
│   ├── ARCHITECTURE.md            # Architecture overview
│   ├── DATABASE.md                # Database schema
│   ├── DEVELOPMENT.md             # This file
│   ├── ENTITLEMENTS.md            # Plan features
│   ├── PRICING.md                 # Pricing tiers
│   └── WEBHOOKS.md                # Webhook handling
└── go.mod                         # Go module
```

## Coding conventions

- Structured logging with `go.uber.org/zap`
- Context-first API design
- Interfaces for testability (store, provider)
- Constant-time comparisons for security-sensitive operations
- All external API calls use TLS
- Webhook payloads bounded and validated

## Common issues

### SQLite not found

```bash
# Install SQLite development headers
# Ubuntu/Debian:
sudo apt-get install libsqlite3-dev

# macOS:
brew install sqlite
```

### PostgreSQL connection refused

```bash
# Check if PostgreSQL is running
docker ps | grep postgres

# Test connection
psql -h localhost -U billing -d billing -c "SELECT 1"
```

### Webhook signature verification failed

```bash
# Check that the webhook secret matches
# Stripe: must start with "whsec_"
# Mercado Pago: must match the dashboard secret
# Crypto: must match the gateway secret
```
