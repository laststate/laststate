# Claude Code context for Billing Service

Billing Service is a payment and entitlement management backend written in Go.
It handles subscriptions, invoicing, and feature provisioning across multiple
payment providers for the LastState platform.

## Key files

- `cmd/billing-service/main.go` — entry point
- `internal/api/router.go` — HTTP router setup
- `internal/api/handlers.go` — API request handlers
- `internal/api/auth.go` — authentication middleware
- `internal/billing/service.go` — core billing logic
- `internal/webhook/handler.go` — webhook processing
- `internal/schema/schema.go` — database schema (embedded SQL)
- `internal/store/store.go` — database layer
- `go.mod` — Go module dependencies
- `docs/API.md` — API reference
- `docs/ARCHITECTURE.md` — architecture overview
- `docs/WEBHOOKS.md` — webhook handling guide
- `docs/DATABASE.md` — database schema reference

## Conventions

- Structured logging with `go.uber.org/zap`
- Context-first API design
- Interfaces for testability (store, provider)
- Constant-time comparisons for security-sensitive operations
- Webhook payloads bounded and validated before processing

## Testing

```bash
go test ./...          # Run all tests
go test -race ./...    # Race detector
go test ./internal/billing  # Billing package only
go test ./internal/webhook  # Webhook package only
```

## Build

```bash
go build ./cmd/billing-service
go run ./cmd/billing-service --db sqlite --port 8080
```
