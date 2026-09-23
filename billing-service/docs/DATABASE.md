# Database Schema & Migrations

This document describes the billing service database schema, relationships,
and migration strategy.

## Overview

The billing service uses PostgreSQL. The schema defines organizations,
subscriptions, invoices, usage tracking, and payment processing.

## ER Diagram

```
┌─────────────────┐     ┌─────────────────┐
│  organizations  │     │    plans        │
├─────────────────┤     ├─────────────────┤
│ id (PK)         │     │ id (PK)         │
│ name            │     │ name            │
│ email           │     │ price_cents     │
│ tier            │     │ currency        │
│ created_at      │     │ interval        │
└────────┬────────┘     │ description     │
         │              └─────────────────┘
         │
         ├──────────────────────────────────────┐
         │                                      │
         ▼                                      ▼
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│ subscriptions   │     │    invoices     │     │ api_tokens      │
├─────────────────┤     ├─────────────────┤     ├─────────────────┤
│ id (PK)         │     │ id (PK)         │     │ id (PK)         │
│ org_id (FK)     │────▶│ org_id (FK)     │◀────│ org_id (FK)     │
│ provider        │     │ sub_id (FK)     │     │ name            │
│ provider_sub_id │     │ provider        │     │ token_hash      │
│ plan_id (FK)    │     │ provider_inv_id │     │ scopes          │
│ status          │     │ amount_cents    │     │ expires_at      │
│ period_start    │     │ currency        │     │ created_at      │
│ period_end      │     │ status          │     └─────────────────┘
│ cancel_at_end   │     │ issued_at       │
└────────┬────────┘     │ paid_at         │
         │              │ due_at          │
         │              └─────────────────┘
         │
         ├──────────────────────┐
         │                      │
         ▼                      ▼
┌─────────────────┐     ┌─────────────────┐
│ tier_quotas     │     │  usage_metrics  │
├─────────────────┤     ├─────────────────┤
│ tier (PK)       │     │ id (PK)         │
│ max_devices     │     │ org_id (FK)     │
│ max_events/day  │     │ metric_name     │
│ retention_days  │     │ value           │
│ max_api_tokens  │     │ period_start    │
│ max_alert_rules │     │ period_end      │
│ features...     │     └─────────────────┘
└─────────────────┘

┌─────────────────┐     ┌──────────────────┐
│ payment_attempts│     │ webhook_events   │
├─────────────────┤     ├──────────────────┤
│ id (PK)         │     │ id (PK)          │
│ org_id (FK)     │     │ provider         │
│ invoice_id (FK) │     │ event_id (PK,UNQ)│
│ attempt_number  │     │ raw_payload (JSONB)│
│ status          │     │ processed_at     │
│ next_retry_at   │     └──────────────────┘
└─────────────────┘
```

## Tables

### organizations

Tenants (companies/projects) that use Trace.

| Column | Type | Notes |
|--------|------|-------|
| `id` | UUID | Primary key, auto-generated |
| `name` | TEXT | Organization name |
| `email` | TEXT | Contact email |
| `tier` | TEXT | `free`, `hobbyist`, `team`, `enterprise` |
| `stripe_customer_id` | TEXT | Stripe customer reference |
| `stripe_subscription_id` | TEXT | Stripe subscription reference |
| `mercado_pago_pref_id` | TEXT | Mercado Pago preference ID |
| `trial_ends_at` | TIMESTAMPTZ | Trial period end (nullable) |
| `created_at` | TIMESTAMPTZ | Record creation time |
| `updated_at` | TIMESTAMPTZ | Last update time |

### subscriptions

Active billing subscriptions per organization.

| Column | Type | Notes |
|--------|------|-------|
| `id` | UUID | Primary key |
| `organization_id` | UUID FK→organizations | Owner |
| `provider` | TEXT | `stripe`, `mercado_pago`, `crypto` |
| `provider_subscription_id` | TEXT | Provider-side subscription ID |
| `plan_id` | TEXT FK→plans | Selected plan |
| `status` | TEXT | `active`, `past_due`, `canceled`, `expired`, `trialing` |
| `current_period_start` | TIMESTAMPTZ | Billing period start |
| `current_period_end` | TIMESTAMPTZ | Billing period end |
| `cancel_at_period_end` | BOOLEAN | Auto-cancel flag |
| `last_proration_cents` | INTEGER | Last proration amount |

### invoices

Billing records per subscription.

| Column | Type | Notes |
|--------|------|-------|
| `id` | UUID | Primary key |
| `organization_id` | UUID FK→organizations | Bill to |
| `subscription_id` | UUID FK→subscriptions | Related sub |
| `provider` | TEXT | Payment provider |
| `provider_invoice_id` | TEXT | Provider-side invoice ID |
| `amount_cents` | INTEGER | Amount in smallest currency unit |
| `currency` | TEXT | `usd`, `brl`, etc. |
| `status` | TEXT | `pending`, `paid`, `failed`, `refunded` |
| `issued_at` | TIMESTAMPTZ | Invoice creation |
| `paid_at` | TIMESTAMPTZ | Payment confirmation |
| `due_at` | TIMESTAMPTZ | Payment deadline |
| `pdf_url` | TEXT | Generated PDF URL |
| `nfe_url` | TEXT | NF-e URL (Brazil) |

### tier_quotas

Feature and usage limits per subscription tier.

| Column | Type | Default |
|--------|------|---------|
| `tier` | TEXT PK | — |
| `max_devices` | INTEGER | 0 |
| `max_events_per_day` | BIGINT | 0 |
| `retention_days` | INTEGER | 30 |
| `max_api_tokens` | INTEGER | 0 |
| `max_alert_rules` | INTEGER | 0 |
| `symbolication` | BOOLEAN | false |
| `analytics` | BOOLEAN | false |
| `custom_integrations` | BOOLEAN | false |
| `priority_support` | BOOLEAN | false |
| `sso` | BOOLEAN | false |
| `audit_logs` | BOOLEAN | false |
| `oncall` | BOOLEAN | false |

**Seed data:**

| Tier | Devices | Events/day | Retention | API Tokens | Alert Rules | Features |
|------|---------|-----------|-----------|------------|-------------|----------|
| free | 5 | 1,000 | 30d | 1 | 0 | symbolication |
| hobbyist | 100 | 50,000 | 90d | 5 | 5 | + analytics |
| team | 1,000 | 500,000 | 1y | 20 | 50 | + integrations, SSO, audit, on-call |
| enterprise | ∞ | ∞ | ∞ | ∞ | ∞ | + priority support, SLA, on-prem |

### plans

Plan definitions with pricing.

| Column | Type | Notes |
|--------|------|-------|
| `id` | TEXT PK | `free`, `hobbyist`, `team`, `enterprise` |
| `name` | TEXT | Display name |
| `price_cents` | INTEGER | Price in cents |
| `currency` | TEXT | `usd` default |
| `interval` | TEXT | `day`, `week`, `month`, `year` |
| `description` | TEXT | Plan description |

**Seed data:**

| ID | Name | Price | Interval | Description |
|----|------|-------|----------|-------------|
| free | Free | $0 | month | 5 devices, 1k events/day, 30d retention |
| hobbyist | Hobbyist | $9/mo | month | 100 devices, 50k events/day, 90d retention |
| team | Team | $49/mo | month | 1k devices, 500k events/day, 1y retention |
| enterprise | Enterprise | $199/mo | month | Unlimited, on-prem, SLA |

### api_tokens

Scoped API tokens per organization.

| Column | Type | Notes |
|--------|------|-------|
| `id` | UUID | Primary key |
| `organization_id` | UUID FK→organizations | Token owner |
| `name` | TEXT | Human-readable name |
| `token_hash` | TEXT | SHA-256 hash of the token (unique) |
| `scopes` | TEXT[] | `read`, `write`, `admin`, `billing`, `entitlements`, `analytics` |
| `expires_at` | TIMESTAMPTZ | Expiration (nullable = never) |
| `last_used_at` | TIMESTAMPTZ | Last usage timestamp |
| `created_at` | TIMESTAMPTZ | Creation time |
| `created_by` | UUID | Admin who created the token |

### usage_metrics

Metered usage tracking per organization.

| Column | Type | Notes |
|--------|------|-------|
| `id` | UUID | Primary key |
| `organization_id` | UUID FK→organizations | Org |
| `metric_name` | TEXT | `events`, `devices`, `storage`, etc. |
| `value` | BIGINT | Metric value |
| `recorded_at` | TIMESTAMPTZ | When recorded |
| `period_start` | TIMESTAMPTZ | Billing period start |
| `period_end` | TIMESTAMPTZ | Billing period end |
| `idempotency_key` | TEXT NULL | Ingest dedupe key; unique per `(organization_id, metric_name, idempotency_key)` (`ux_usage_metrics_idem`; NULLs never conflict) |

### usage_billing_runs

One invoice per usage-billing period (idempotency for `BillUsageCycle` and
`POST /v1/usage/invoices`).

| Column | Type | Notes |
|--------|------|-------|
| `organization_id` | UUID FK→organizations | Org (PK) |
| `subscription_id` | TEXT | Provider/Local sub id (PK) |
| `period_start` | TIMESTAMPTZ | Period start (PK) |
| `period_end` | TIMESTAMPTZ | Period end (PK) |
| `invoice_id` | UUID | First invoice for the run |
| `claimed_at` | TIMESTAMPTZ | Claim time |

### webhook_events

Idempotency store for processed webhook events.

| Column | Type | Notes |
|--------|------|-------|
| `id` | UUID | Primary key |
| `provider` | TEXT | `stripe`, `mercado_pago`, `crypto` |
| `event_id` | TEXT PK, UNIQUE | Provider event ID |
| `raw_payload` | JSONB | Full webhook payload |
| `processed_at` | TIMESTAMPTZ | Processing timestamp |

### payment_attempts

Dunning tracking for failed payments.

| Column | Type | Notes |
|--------|------|-------|
| `id` | UUID | Primary key |
| `organization_id` | UUID FK→organizations | Org |
| `invoice_id` | UUID FK→invoices | Related invoice |
| `attempt_number` | INTEGER | Retry number |
| `status` | TEXT | `pending`, `succeeded`, `failed`, `max_retries_exceeded` |
| `last_error` | TEXT | Error message |
| `next_retry_at` | TIMESTAMPTZ | Next retry time |
| `created_at` | TIMESTAMPTZ | Creation time |

## Indexes

| Index | Table | Columns | Purpose |
|-------|-------|---------|---------|
| `idx_subscriptions_org` | subscriptions | `organization_id` | Fast org lookup |
| `idx_invoices_org` | invoices | `organization_id` | Fast org lookup |
| `idx_webhook_events_event_id` | webhook_events | `event_id` | Idempotency lookup |
| `idx_usage_metrics_org_period` | usage_metrics | `organization_id, period_start, period_end` | Usage queries |
| `ux_usage_metrics_idem` | usage_metrics | `organization_id, metric_name, idempotency_key` UNIQUE | Ingest idempotency |
| `idx_payment_attempts_org` | payment_attempts | `organization_id` | Dunning queries |
| `idx_api_tokens_org` | api_tokens | `organization_id` | Token lookup |
| `idx_api_tokens_hash` | api_tokens | `token_hash` | Auth lookup |

## Migration Strategy

Migrations are applied automatically on startup. The migration files are in
`internal/schema/schema.go` (embedded SQL).

**Rules:**
- Migrations are additive — never modify existing migrations.
- Use `CREATE TABLE IF NOT EXISTS` and `ALTER TABLE` for safety.
- Seed data uses `ON CONFLICT ... DO NOTHING` for idempotency.
- Always test migrations against a fresh database before deploying.

**Running migrations:**
```bash
# Automatic (on startup)
go run ./cmd/billing-service

# Manual
go run ./cmd/billing-service --migrate
```

## Backup and Restore

```bash
# Backup
pg_dump -Fc trace_billing > backup_$(date +%Y%m%d).dump

# Restore
pg_restore -d trace_billing backup_$(date +%Y%m%d).dump
```
