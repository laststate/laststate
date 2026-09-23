# End-to-end testing

## Package tests

```bash
go test ./internal/... -count=1
cd web && npm ci && npm test && npm run build
```

## UI smoke (Vite + mocks)

```bash
cd web && npm run test:e2e:install && npm run test:e2e
```

These tests cover routing, branding, and dashboard layout. They do not replace a live Trace stack.

## Full stack (release gate)

1. Start Postgres and Trace (`docker compose up --build`).
2. Export credentials:

```bash
export TRACE_E2E_URL=http://127.0.0.1:8080
export TRACE_E2E_TOKEN="$(tr -d '\n' < data/bootstrap-token.txt)"
```

3. Live Relay↔Trace contract (CI runs this after starting Trace):

```bash
export TRACE_E2E_URL=http://127.0.0.1:8080
export TRACE_E2E_TOKEN="$(tr -d '\n' < data/bootstrap-token.txt)"

# Trace-side: capabilities, duplicate 202, conflict 422
go test -tags=e2e ./scripts -count=1 -v

# Relay-side (from a relay checkout; not run in Trace CI — private cross-repo):
cd ../relay
go test -tags=e2e ./internal/delivery -run TestLiveTrace -count=1 -v
```

CI starts Trace and runs `go test -tags=e2e ./scripts` only. Protocol goldens are
vendored under `internal/lep/testdata/protocol-vectors/` (private org repos cannot
be checked out with the default Actions token).


4. Optional smoke binary:

```bash
TRACE_URL="$TRACE_E2E_URL" TRACE_TOKEN="$TRACE_E2E_TOKEN" go run ./scripts/smoke_ingest.go
```

5. Confirm worker processing and UI: open `/overview` and `/issues`.

### Expected path

```
Latch LEP → Relay → POST /v1/ingest
  → object store + events row
  → queue.Jober (postgres/redis)
  → worker process_event
  → issue (+ notify)
  → UI APIs
```

### Sessions + onboarding trace (see `SESSIONS_ONBOARDING.md`)

```
signup → session (last_used_at=now)
  → GET /api/onboarding/status (1/5)
  → org_create → invite_members → first_event (ingest above) → mfa_setup (5/5)
  → GET /api/me/sessions (ip/user_agent/last_used_at)
  → idle expiry → 401 → revoke-others
```
