# Trace Database

Database schema, migrations, and operational guide for Trace's PostgreSQL backend.

## Overview

Trace stores crash events, user accounts, billing data, alert rules, and
analytics results in PostgreSQL. The schema is managed through automated
migrations applied on startup.

## Migration strategy

Migrations are stored in `internal/db/migrations/` as numbered SQL files:

```
001_init.sql        — Core schema (projects, crashes, symbols)
002_phase2.sql      — Enhanced crash data
003_alerts.sql      — Alert rules and on-call
004_hardening.sql   — Hardened constraints
005_*.sql           — Feature-specific migrations (anomaly, chaos, DNA, etc.)
006_complete2.sql   — Additional schema additions
007_gaps.sql        — Fill gaps
008_close_opens.sql — Close open items
009_plans.sql       — Billing plans
010_usage.sql       — Usage tracking
011_users_mfa.sql   — MFA for users
012_exports.sql     — Export functionality
013_text_search.sql — Full-text search
```

**Rules:**
- Always create new migration files; never modify existing ones.
- Use `CREATE TABLE IF NOT EXISTS` and `ALTER TABLE` for safety.
- Add indexes in the same migration that introduces the table.
- Seed data uses `ON CONFLICT ... DO NOTHING` for idempotency.
- Test migrations against a fresh database before deploying.

## Running migrations

```bash
# Automatic (on startup)
go run ./cmd/trace

# Check current status
go run ./cmd/trace --migrate status

# Run pending migrations
go run ./cmd/trace --migrate up

# Rollback last migration (development only)
go run ./cmd/trace --migrate down
```

## Key tables

### Core

| Table | Purpose |
|-------|---------|
| `projects` | Crash reporting projects |
| `crashes` | Crash events with metadata |
| `symbols` | Symbol files for resolution |
| `users` | User accounts |
| `sessions` | Auth sessions (`002` + `010 ip/ua` + `026/027 last_used_at`; see `SESSIONS_ONBOARDING.md`) |
| `onboarding_steps` | Onboarding progress (`011`: `(user_id, step)` UNIQUE; see `SESSIONS_ONBOARDING.md`) |
| `api_keys` | API key management |
| `organizations` | Multi-tenant organizations |

### Analytics & Monitoring

| Table | Purpose |
|-------|---------|
| `alerts` | Alert rules and notifications |
| `oncall_schedules` | On-call rotation schedules |
| `anomaly_events` | Detected anomaly records |
| `fleet_health` | Fleet health scores |
| `device_dna` | Device fingerprint data |

### Billing

| Table | Purpose |
|-------|---------|
| `plans` | Subscription plans |
| `usage` | Usage metrics per organization |
| `subscriptions` | Active subscriptions |

### Operations

| Table | Purpose |
|-------|---------|
| `exports` | Data export history |
| `audit_log` | Admin action audit trail |
| `webhook_deliveries` | Outbound webhook tracking |
| `chaos_results` | Chaos engineering test results |
| `postmortem_records` | Postmortem documents |
| `pr_records` | PR integration records |

## Indexes

Critical indexes for query performance:

- `crashes.project_id` — Fast crash lookup by project
- `crashes.created_at` — Time-range queries
- `crashes.exception_type` — Exception filtering
- `users.email` — Auth lookup
- `api_keys.key_hash` — Token authentication
- `alerts.project_id` — Alert queries
- `usage.organization_id + period` — Usage aggregation

## Backup and restore

```bash
# Backup
./scripts/backup.sh

# Restore
./scripts/restore.sh <backup-file>
```

Backups include both database and object storage. Run daily in production.

## Maintenance

### Vacuum and analyze

PostgreSQL should run automatic VACUUM and ANALYZE. For high-write workloads:

```sql
-- Check table bloat
SELECT
    schemaname,
    tablename,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) as size,
    n_dead_tup,
    n_live_tup
FROM pg_stat_user_tables
ORDER BY n_dead_tup DESC
LIMIT 10;
```

### Connection pooling

Use PgBouncer in front of PostgreSQL for connection management:

```bash
docker run -d --name pgbouncer \
  -p 6432:6432 \
  -e PGBOUNCER_DATABASE=trace \
  -e PGBOUNCER_USERS="trace:password" \
  -e PGBOUNCER_AUTH_TYPE=scram-sha-256 \
  postgres/pgbouncer
```

### Monitoring

Monitor these PostgreSQL metrics:

- Active connections
- Query latency (p99)
- Table bloat
- Dead tuples
- Replication lag (if using replicas)
