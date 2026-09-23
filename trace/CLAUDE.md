# Claude Code context for Trace

Trace is a self-hosted crash analytics and observability backend written in Go.
It ingests LEP-encoded device crashes via Relay, runs analysis pipelines, and
exposes results through a web UI and REST API.

## Key files

- `cmd/trace/main.go` — entry point
- `internal/api/server.go` — HTTP server setup
- `internal/api/middleware.go` — auth, rate limiting, CORS
- `internal/store/store.go` — database layer
- `internal/worker/worker.go` — event processing
- `internal/analysis/analysis.go` — crash analysis
- `go.mod` — Go module dependencies
- `.env.example` — configuration reference
- `docs/DEPLOYMENT.md` — deployment guide
- `docs/SELF_HOSTING.md` — self-hosting guide

## Conventions

- Structured logging with `go.uber.org/zap`
- Context-first API design
- Interfaces for testability (store, queue, provider)
- Migration files in `internal/db/migrations/`
- Protocol vectors in `internal/lep/testdata/`

## Testing

```bash
go test ./...          # Run all tests
go test -race ./...    # Race detector
go test ./internal/api # API package only
```

## Build

```bash
go build ./cmd/trace
TRACE_MOCK=true go run ./cmd/trace
```
