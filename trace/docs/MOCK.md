# TRACE_MOCK — preview mode (`TRACE_MOCK=true`)

No DB/S3/queue/workers. Everything in memory and deterministic (charts anchored at
`2026-08-15T10:30:00Z` in `internal/mockdata/mockdata.go:now()`); auth bypass —
every request is admin (`mockdata.Me/Login`).

## Enable

```bash
TRACE_MOCK=true go run ./cmd/trace
set TRACE_MOCK=true && go run .\cmd\trace   # Windows
./scripts/mock-preview.sh / scripts\mock-preview.bat
```

Wiring: `internal/config/config.go:MockMode`, `cmd/trace/main.go`,
`internal/api/mock_handler.go:MockServer/Handler`.

## Scope (what is mocked)

- Implemented in `mock_handler.go:Handler`: `GET /health/live|/ready`,
  `/api/overview|/stream(SSE)|/issues|/issues/{id}|/events|/devices|/releases|/artifacts|/alerts|/channels|/relays|/projects|/hardware|/boots|/jobs/dead|/audit|/search|/me`,
  `POST /api/auth/login|/logout`, `POST /api/auth/register|/signup → 201`,
  OIDC login/callback → redirect `/`, `GET /v1/relay/capabilities`,
  `GET /openapi.json` (minimal), local billing
  (`GET /v1/billing/tiers|/subscription|/plans`, `POST /v1/billing/checkout → 400 billing_disabled`,
  fixed `GET /v1/usage`), accept-only ingest
  (`POST /v1/ingest|/events|/events:batch|/artifacts|/relay/heartbeat`),
  `GET /metrics` (`trace_mock_total 0`), SPA fallback with cache
  (`/` no-cache, `/assets/` immutable).
- **Not mocked / differs from prod**: no persistence (ingest `202 accepted`
  is discarded), no real SCIM/SAML/OIDC (`capabilities: saml=false scim=false`),
  no real billing (`billing_enabled=false`, checkout 400), no queue/worker/
  dunning, minimal `openapi.json` (overview/issues only).

## Limits

- Never use in production: no auth, no persistence, no real audit.
- Frontend-only: good for UI/demos/frontend tests and prototyping.
- Real E2E requires DB + `TRACE_MOCK=false` (see `docs/E2E.md`).
