# LastState Billing Service

**Proprietary license.** This service is part of the LastState platform and communicates with Trace via the Admin API.

## Overview

The Billing Service manages payment processing, subscription lifecycle, and feature entitlements for the LastState platform. It integrates with multiple payment providers and synchronizes feature access with the Trace service.

## Architecture

```mermaid
flowchart LR
    Clients["Clients<br/>(Web/App/CLI)"] --> Billing["Billing Service"]
    Billing --> Trace["Trace<br/>(Admin API)"]
    Billing --> Stripe["Stripe"]
    Billing --> MP["Mercado Pago"]
    Billing --> Crypto["Crypto Gateway"]
```

## Plans

| Plan | Price | Devices | Events/day | Retention | Key Features |
|------|-------|---------|-----------|-----------|-------------|
| **Free** | $0/mo | 5 | 1,000 | 30 days | Symbolication |
| **Hobbyist** | $9/mo | 100 | 50,000 | 90 days | Custom alerts, analytics export |
| **Team** | $49/mo | 1,000 | 500,000 | 1 year | SSO, on-call, audit, integrations |
| **Enterprise** | $199/mo | Unlimited | Unlimited | Unlimited | On-prem, SLA, priority support |

## Components

### Payment Infrastructure
- **Stripe Integration**: Checkout Sessions, Subscriptions, Webhooks (`invoice.paid`, `customer.subscription.updated/deleted`)
- **Mercado Pago Integration boundary**: webhook verification and provider API
  wiring; unavailable lifecycle operations fail explicitly.
- **Crypto Gateway boundary**: Coinbase Commerce / NOWPayments webhook
  verification and API wiring; unavailable lifecycle operations fail explicitly.
- **Webhook Idempotency**: Deduplication by `event_id` / `payment_id`
- **Invoice Generation**: PDF invoices; NF-e requires a certified external
  fiscal provider and is never fabricated locally.
- **Dunning Management**: Automatic retry for failed cards, dunning emails, grace period
- **Proration Logic**: Mid-cycle upgrade/downgrade with proportional calculation
- **Tax Calculation**: IVA/GST/Sales Tax

### Entitlement Sync Service
- **Provisioning Hook**: After confirmed payment → `POST /v1/admin/organizations/{id}/entitlements`
- **Deprovisioning Hook**: After cancellation/expiration → downgrade tier
- **Usage Sync**: Polling or webhook from Trace → update metering
- **Plan Change API**: `POST /v1/subscription/upgrade` → proration + immediate sync
- **Trial Management**: 14-day automatic trial on signup

## API Endpoints

### Public (Webhooks)
- `POST /webhooks/stripe`
- `POST /webhooks/mercado-pago`
- `POST /webhooks/crypto`

### Authenticated
- `GET /health` — Health check
- `POST /v1/subscriptions` — Create subscription
- `GET /v1/subscriptions/{id}` — Get subscription
- `DELETE /v1/subscriptions/{id}` — Cancel subscription
- `POST /v1/subscriptions/{id}/upgrade` — Upgrade plan
- `GET /v1/invoices` — List invoices
- `GET /v1/invoices/{id}` — Get invoice
- `POST /v1/usage` — Record usage
- `GET /v1/usage` — Get usage
- `GET /v1/usage/charges` — Metered overage summary (plan allowance, blocks, amount)
- `POST /v1/coupons/validate` — Validate a coupon and preview its discount
- `POST /v1/dunning/{org_id}/initiate` — Start dunning
- `POST /v1/entitlements/{org_id}/provision` — Provision features
- `POST /v1/entitlements/{org_id}/deprovision` — Deprovision features
- `POST /v1/trials/{org_id}/start` — Start trial
- `POST /v1/trials/{org_id}/end` — End trial

## Development

### Requirements
- Go 1.25+
- PostgreSQL 16+
- Stripe CLI (for local webhook testing)

### Local Setup

```bash
# Start dependencies
docker-compose up -d

# Run migrations
go run cmd/billing-service/main.go --migrate

# Start server
go run cmd/billing-service/main.go \
  --db "postgres://billing:billing@localhost:5432/billing?sslmode=disable" \
  --trace-url "http://localhost:8080" \
  --stripe-key "sk_test_..." \
  --mp-key "APP_USR-..." \
  --crypto-key "..."
```

### Environment Variables
- `DB_URL` — PostgreSQL connection URL
- `TRACE_URL` — Trace Admin API URL
- `STRIPE_KEY` — Stripe API key
- `MP_KEY` — Mercado Pago API key
- `CRYPTO_KEY` — Crypto gateway API key
- `STRIPE_WEBHOOK_SECRET` — Stripe webhook endpoint secret
- `MP_WEBHOOK_SECRET` — Mercado Pago webhook secret
- `CRYPTO_WEBHOOK_SECRET` — Crypto webhook secret

## Deployment

Tagged releases publish versioned images to `ghcr.io/laststate/billing-service`
(`vMAJOR.MINOR.PATCH`, `vMAJOR.MINOR`, commit SHA) plus the Helm chart to
`oci://ghcr.io/laststate/charts`, alongside binaries in the GitHub Release.

### Docker
```bash
docker run -p 8080:8080 \
  -e DB_URL=... -e STRIPE_KEY=... \
  ghcr.io/laststate/billing-service:vX.Y.Z
```

### Kubernetes
```bash
helm install billing-service oci://ghcr.io/laststate/charts/billing-service --version X.Y.Z \
  --set env.STRIPE_KEY=$(echo -n "sk_..." | base64) \
  --set env.MP_KEY=$(echo -n "APP_USR-..." | base64)
```

## Documentation

- [Architecture](docs/ARCHITECTURE.md)
- [API Reference](docs/API.md)
- [Pricing](docs/PRICING.md)

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for all changes.

## License

**Proprietary.** All rights reserved. This software is part of the LastState platform and may not be copied, modified, or distributed without express written permission from LastState.
