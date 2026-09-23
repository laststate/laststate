# Trace DB Migrations

Migrator: `internal/db/db.go:35` — `embed.FS` + `sort.Strings(names)` + `schema_migrations(version TEXT PRIMARY KEY)` with advisory lock `0x5472414345`.

## Legacy numbering (frozen)

`001_init.sql` → `004_hardening.sql` are sequential. `005_*` (11 files) share the same prefix due to a parallel feature-branch merge in v0.5:

- `005_anomaly_events.sql`
- `005_chaos_results.sql`
- `005_complete.sql`
- `005_device_dna.sql`
- `005_fleet_health.sql`
- `005_memorial.sql`
- `005_postmortem.sql`
- `005_pr_records.sql`
- `005_public_api.sql`
- `005_quota_tracking.sql`
- `005_soundboard.sql`

Lexical order (`sort.Strings`) already yields the intended apply order, and every DDL is `IF NOT EXISTS` / `ADD COLUMN IF NOT EXISTS`, so fresh and existing DBs converge. **Do not rename these 11 files** — they are already recorded in production `schema_migrations` under the old names; renaming would cause re-execution (harmless but noisy).

## Rule for new migrations

- Start at `024_*.sql` (next after `023_text_search.sql` after the 2026-08-26 audit). Use `NNN_snake_case.sql` with zero-padded `NNN`.
- Never reuse a prefix. CI lints for duplicate prefixes (`migrations_lint_test.go`).
- Keep DDL idempotent (`IF NOT EXISTS`) so `Migrate` is safe to rerun.

## Adding a migration

1. `cp 023_text_search.sql 024_your_feature.sql`
2. `go test ./internal/db -run TestMigrationsLint -count=1`
3. `go test ./... -run TestMigrations -count=1` with a test DB.

## Why not `golang-migrate`?

The current migrator is intentionally minimal (no down migrations, no external tool). Down is handled by restoring a backup (`scripts/backup.sh` / `restore.sh`).
