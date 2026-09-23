# Gemini context for Trace

Trace is a self-hosted crash analytics and observability backend written in Go.
It ingests LEP-encoded device crashes via Relay, runs analysis pipelines, and
exposes results through a web UI and REST API.

## Architecture

- **Ingest**: Relay ships crash data to Trace over HTTPS with bearer auth.
- **Storage**: PostgreSQL for events/metadata, S3/MinIO for artifacts.
- **Processing**: Workers analyze crashes (symbolication, anomaly detection,
  device DNA, health scoring).
- **API**: REST API with scoped bearer tokens and OIDC/SAML SSO.
- **UI**: React SPA served from the same binary.

## Quick start

```bash
# Mock mode (no database)
TRACE_MOCK=true go run ./cmd/trace

# With PostgreSQL
docker compose up -d postgres
TRACE_MOCK=false go run ./cmd/trace
```

## Key packages

- `internal/api/` — HTTP handlers and middleware
- `internal/worker/` — event processing pipeline
- `internal/analysis/` — crash analysis engine
- `internal/store/` — database operations
- `internal/queue/` — message queue abstraction

## Conventions

- Go 1.26+
- Structured logging with zap
- Interface-based design for testability
- Migration files in `internal/db/migrations/`
- All remote communication requires TLS
