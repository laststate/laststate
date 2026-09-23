# Sessions (IP/UA + idle) + onboarding + e2e track

## Sessions

- `sessions` schema (`002_phase2` + `010_usage` + `026_session_activity` +
  `027_session_activity_backfill`):
  `id, user_id, token_hash, prefix, expires_at (+7d), created_at, revoked_at,
  ip INET, user_agent TEXT, last_used_at TIMESTAMPTZ`.
  New sessions insert `last_used_at=now()` (`MintSession`); legacy rows
  get backfill `last_used_at=created_at` (027).
- Every authenticated request (`sessionFrom`) checks idle and touches
  fire-and-forget (2s): `TouchSession → last_used_at=now(), ip, user_agent`
  (if ip+ua are empty, only `last_used_at`). `GET /api/me` uses `sessionFrom`
  (idle enforced; previously bypassed via direct `AuthSession`).
- Idle: `TRACE_SESSION_IDLE_TIMEOUT` (default `24h`; `0` disables),
  **lazy per-request** enforcement (no sweeper): expired → revoke
  best-effort + `401`. Distinct from `TRACE_HTTP_IDLE_TIMEOUT_SEC=120s`
  (HTTP keep-alive) and cookie `MaxAge 7d` / `expires_at +7d` (absolute lifetime).
- `X-Forwarded-For`/`X-Real-IP` only counts when peer ∈ `TRACE_TRUSTED_PROXIES`
  (`middleware.go:ConfigureTrustedProxies`).
- Management: `GET /api/me/sessions`, `DELETE /api/me/sessions/{id}`,
  `POST /api/me/sessions/revoke-others` (`tenancy_endpoints.go`,
  `openapi.go`, `store/ops.go:ListSessions/Revoke*`).
- Tests: `session_test.go:TestSessionFrom_IdleExpired|IdleFreshTouchesIPUA`
  (+ base `NoServer/NoSecret/FromContext/HeaderAuth`).

## Onboarding (backend; no UI wizard)

- API (`requireAuth`): `GET /api/onboarding/status`,
  `POST /api/onboarding/step {step}`, `GET /api/onboarding/progress`
  (`onboarding_endpoints.go`, routes in `server.go`, `openapi.go tags:[auth]`).
- Valid steps: `signup, org_create, invite_members, first_event, mfa_setup`
  (5 total; `Progress 0–1`, `Percentage 0–100`).
- Idempotent store: `onboarding_steps(user_id, step UNIQUE)` +
  `MarkOnboardingStep ON CONFLICT DO UPDATE` (`store/onboarding.go`,
  migration `011_users_mfa.sql:53-61`).
- UI: `trace/web/src` has no wizard (grep `onboarding`=0) — future target;
  the backend already sustains the e2e track below.

## E2E track (onboarding + sessions)

```bash
# 1. signup → 201 + cookie/session (last_used_at=now, ip/ua on 1st touch)
# 2. GET /api/onboarding/status → progress 1/5 (signup)
# 3. POST /api/onboarding/step org_create → invite_members → first_event (ingest)
#    → mfa_setup → progress 5/5
# 4. GET /api/me/sessions → row with ip/user_agent/last_used_at
# 5. Idle: TRACE_SESSION_IDLE_TIMEOUT=1s, wait, request → 401 + revoke
# 6. revoke-others → only the current session survives
```

Existing full-stack (ingest→worker→issue→UI) in `docs/E2E.md`; Playwright
UI smoke (`web/e2e/smoke.spec.ts`, `product.spec.ts`) does not cover
sessions/onboarding (mocks) — coverage here is API/store + the track above.
Mock detail: `docs/MOCK.md` (`TRACE_MOCK=true` bypasses auth).
