# Trace agent guide

This is the canonical guide for AI-assisted work in Trace. It applies to every
contribution, whether the assistant is Codex, Claude, Copilot, Gemini, Cursor,
or another tool. Tool-specific entry points point here so that the project has
one source of truth.

Trace is a self-hosted crash analytics and observability backend. It ingests
LEP-encoded device crashes via Relay, runs analysis pipelines (symbolication,
anomaly detection, device DNA, health scoring), and exposes the results through
a web UI and REST API.

## Architecture overview

```
┌───────────┐     ┌───────────┐     ┌───────────────────────┐
│   Latch   │────▶│   Relay   │────▶│        Trace           │
│ (device)  │     │ (gateway) │     │  ┌─────────────────┐  │
│  C11/Rust │     │   Go      │     │  │ API server      │  │
└───────────┘     └───────────┘     │  │ Worker pipeline │  │
                          ┌─────────┤  │ Analytics engine│  │
                          │         │  │ Web UI (SPA)    │  │
                          ▼         │  └─────────────────┘  │
                    PostgreSQL    │         │               │
                    (events)      │         ▼               │
                    S3/MinIO      │    ┌─────────────────┐  │
                    (artifacts)   │    │ Prometheus/Grafana│  │
                                  │    └─────────────────┘  │
                                  └─────────────────────────┘
```

Key packages:
- `internal/api/` — HTTP handlers, middleware, auth, OpenAPI
- `internal/worker/` — event processing pipeline
- `internal/analysis/` — crash analysis, symbolication, anomaly detection
- `internal/store/` — database layer
- `internal/queue/` — message queue backends (postgres, redis, nats, memory)
- `internal/billing/` — plan and quota management
- `internal/webhook/` — outbound webhook delivery

## Invariants

- **Event IDs are stable** — derived from LEP bytes, not modified.
- **At-least-once delivery** — workers must be idempotent.
- **No secret leakage** — raw payloads, tokens, and secrets never appear in logs.
- **Auth is scoped** — bearer tokens carry explicit scopes; admin tokens cannot
  perform ingest operations and vice versa.
- **TLS everywhere** — all remote communication requires TLS 1.2+.
- **Bounded input** — all network sources must bound size, headers, and
  connection count before allocation.

## Development setup

```bash
# Build
go build ./cmd/trace

# Run with mock data (no database required)
TRACE_MOCK=true go run ./cmd/trace

# Run with PostgreSQL
docker compose up -d postgres
TRACE_MOCK=false go run ./cmd/trace

# Run tests
go test ./...
go test -race ./...

# Lint
gofmt -l cmd internal
go vet ./...
```

## Testing

- Unit tests for all packages.
- Integration tests for queue backends, store operations, and API endpoints.
- Protocol vector tests for LEP encoding/decoding.
- Chaos tests for fault injection.

## Pull requests

1. Create a focused branch from `main`.
2. Add tests for behavior changes.
3. Update documentation when touching public APIs or configuration.
4. Add a CHANGELOG entry for operator-facing changes.
5. Fill in the PR template with runtime evidence.

No Contributor License Agreement is required; contributions are accepted under
the repository's license.

By participating, you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).
