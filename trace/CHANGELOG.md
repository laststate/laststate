# Changelog

All notable changes to the LastState Trace backend.

## [Unreleased]

### Changed
- **License back to AGPL-3.0** - the Apache-2.0 switch removed the §13
  lever behind the commercial Trace offering. Restored byte-identical;
  only the holder line convention stays unified elsewhere.

### Fixed
- **SCIM test isolation** - `TestSCIMBadID` used a fixed admin email, so the
  `Race` step failed with a duplicate-key error against the same Postgres
  service used by the earlier `Test` step. It now uses a unique email per
  run like the other API tests.

## [0.9.0] - 2026-09-20

### Security
- **MFA enforced at login** - enrolled users must pass TOTP (or a single-use
  emailed backup code); the password-minted session is revoked on any MFA
  failure. Staged enroll (verify-to-confirm) + otpauth URI for QR setup.
- **Reset/verify tokens hashed** - only SHA-256 hashes touch the DB; password
  reset revokes all sessions.
- **Session lifetime aligned** - cookie MaxAge 7d matches `sessions.expires_at`
  (was 14d of dead cookie).
- **Auth rate limits** - 20 req/min per IP on login/signup/register/forgot/
  reset/verify/resend (global limiter unchanged).
- **Password policy** - 12–128 chars (bcrypt truncation guard) + common-password
  blocklist, enforced in store and endpoints.
- **SAML SSO verified** - assertion XML signatures checked with
  goxmldsig against the org IdP certificate, plus Conditions
  (audience/recipient/time window) and deflate-correct AuthnRequests.
  Uncertified path still prod-rejected; signed round-trip + tamper tests.
- **RBAC cleanup** - single rank source (`store.RoleRank`), dead ACL helpers
  removed, org creation requires admin.

### Added
- **Email verification end-to-end** - tokens issued on register, verification
  links point at `/verify-email`, new VerifyEmailPage.
- **Invite emails** - `SendInviteEmail` with `/accept-invite?token=` link;
  new AcceptInvitePage + route.
- **MFA/session/org UI** - settings gains MFA enroll/verify/disable,
  session list + revoke + revoke-others, org switcher, member
  list/invite/role/remove; login shows MFA field + OIDC error codes;
  401 bounces app views to `/login`.
- **OIDC RP logout** - `GET /api/auth/oidc/logout` revokes locally and
  continues to the provider `end_session_endpoint` when advertised;
  callback failures redirect to `/login?error=` instead of blank JSON.
- **SCIM 2.0 Users** - list (userName filter, pagination), create
  (passwordless + membership), read, update (name/role/deprovision),
  delete (membership removal); audit events; openapi paths fixed
  (`/scim/v2/Users`, was wrong `/api/scim/...`); Groups still unimplemented.
- **Auth audit completeness** - register, logout, forgot, reset, mfa_enroll,
  mfa_disable, invite_accept, switch_org, session revocations, failed logins.
- **Org switch cookie rotation** - `switch-org` rotates the session cookie
  (was JSON-token only).
- **Mailer in English** - verification/reset/MFA/invite templates translated;
  links point at SPA pages.

### Added
- **Real-time SSE stream** - `GET /api/stream` pushes overview snapshots every 3s (plus heartbeats)
  to the dashboard; the web client prefers `EventSource` and falls back to polling automatically.
  The mock server (`TRACE_MOCK=true`) serves the same stream.
- **Dedicated auth pages** - `/login`, `/register`, `/forgot-password`, `/reset-password`
  (full-page, OIDC entry point, anti-enumeration UX) replacing the inline login panel.
- **Crash-to-PR view** - `PRs` nav item now renders: integration status, create-fix-branch form,
  and PR list backed by `/api/pr/status`, `/api/pr/create`, `/api/prs`.
- **Marketing pages wired** - `/landing`, `/blog`, `/faq`, `/docs`, `/pricing` render their
  (previously never-imported) styles and components, each with a back-to-dashboard link.
- **Dedicated 404 page** - unknown routes render a branded not-found page instead of a silent
  redirect to the overview.
- **Settings overhaul** - real retention form (no more `prompt()`), token management
  (list / create / revoke with one-time secret display), notification controls preserved.

### Fixed
- **Blank client-side views** - `billing`, `lep-explorer`, `public-api` (and the new `pr`) never
  rendered because the shell gated rendering on server data; client views are now exempt.

### Changed
- **Faster first paint** - heavy/standalone views are code-split via `React.lazy`
  (initial bundle 352.53 kB → 294.18 kB, gzip 102.36 → 89.92 kB), and the artificial
  700 ms minimum-load delay was removed.
- Login/logout affordance navigates to `/login` instead of opening an inline panel.

## [0.10.0] - 2026-08-20

### Added
- **LEP v2 support** - `internal/lep` codec now emits the current wire version (2) on `Encode` and
  accepts v1 and v2 on `Validate`/`Decode` (version-domain separated). Capabilities advertise
  `lep_versions: [1, 2]` on both `/v1/relay/capabilities` and the mock server.
- **Protocol golden vectors re-vendored** - `internal/lep/testdata/protocol-vectors` follows the v2
  manifest (`kind`: valid / invalid / crypto-aead / crypto-hmac / stream / lsak); the test loader
  parses stream frames and LSAK control messages locally.
- **Web LEP explorer codec ported to the real wire format** - `web/src/lep/codec.ts` now matches
  `internal/lep` (24-byte little-endian header, CRC-32/IEEE over header[0:20] and payload) and
  accepts v1/v2 while emitting v2.

### Added
- **Deployment modes** - `TRACE_DEPLOYMENT=local|enterprise` (default `enterprise`)
  - `local`: self-hosted, everything unlocked - auth not required, quotas off, billing/paywall hidden
  - `enterprise`: managed SaaS - auth required, quotas enforced, billing catalog + paywall active
- **Billing routes wired** - `GET /v1/billing/tiers`, `GET /v1/billing/subscription`, `DELETE /v1/billing/subscription`, `POST /v1/billing/checkout`, `GET /v1/billing/plans`, `GET /v1/usage` are now mounted (deployment-aware)
- **Usage metrics endpoint** - `GET /v1/usage` returns `{metrics:[{metric_name,value}]}` for the billing view
- **Capabilities/bootstrap deployment info** - `/v1/relay/capabilities` and `/api/bootstrap` expose `deployment` and `billing_enabled`

### Changed
- `apiBillingTiers` response now includes `items` (frontend contract) alongside `tiers`, plus `deployment`/`billing_enabled`
- Frontend hides `billing`/`pricing` navigation and renders an "everything unlocked" panel in local mode
- `TRACE_OPEN_UI` and `TRACE_ALLOW_PUBLIC_REGISTER` production checks are skipped when `TRACE_DEPLOYMENT=local` (opt-in escape hatch)

### Known limitations
- **SCIM 2.0 provisioning is a 501 stub** - enterprise SSO user/group provisioning is incomplete (returns HTTP 501).
- **billing-service integration maturity is unverified** - the Admin API entitlement sync with billing-service is wired but not yet validated end-to-end.

## [0.9.0] - 2026-08-15

### Added
- **Billing portal** - `billing` view in Trace UI with plans, usage, checkout
- **Billing API endpoints** - `/v1/billing/tiers`, `/v1/billing/subscription`, `/v1/billing/checkout`, `/v1/billing/plans`
- **Self-hosted CLI** - `cmd/laststate` for init, up, down, status, logs, config, bootstrap
- **Unified docker-compose** - `docker-compose.laststate.yml` for Trace + Relay + PostgreSQL
- **Landing page** - `/pricing` and `/docs` static pages served by Trace
- **Tier quotas** - device/event/retention limits per subscription tier
- **API tokens** - per-org scoped tokens with expiry and scopes
- **Usage metering** - `POST /v1/usage`, `GET /v1/usage` endpoints
- **Oncall schedules** - `apiOncall`, `apiOncallShift` endpoints
- **Escalation policies** - `apiEscalation` endpoint
- **Suspect commits** - `apiSuspectCommits` for issue investigation
- **Issue replay** - `apiIssueReplay` for breadcrumb replay
- **SAMLMetadata** - SAML metadata endpoint for IdP configuration
- **Analytics export** - NDJSON export for external warehouses

### Changed
- Navigation: added `billing` view to sidebar
- OpenAPI: updated to v1.0.0 with billing, orgs, and admin tags
- Default plans: free, hobbyist, team, enterprise (was free, pro, enterprise)

### Fixed
- Settings view: proper retention configuration UI
- Compliance view: security features list updated

## [0.8.0] - 2026-08-14

### Added
- Mock preview mode (`TRACE_MOCK=true`)
- Web UI with React, charts, issue detail, breadcrumb replay
- Search with FTS + event query DSL
- NDJSON export
- Backup/restore scripts
- Helm charts for production deployment

## [0.7.0] - 2026-08-10

### Added
- Issue fingerprinting and correlation
- Cortex-M, RISC-V, Xtensa, Linux signal capture analysis
- DWARF symbolication
- Notification channels (Slack, Discord, PagerDuty, email)
- Organization and project management
- Audit logging

## [0.6.0] - 2026-08-05

### Added
- Event ingestion pipeline
- Health, boot, log, metric event types
- PostgreSQL queue backend
- Redis queue backend
- Prometheus metrics

## [0.5.0] - 2026-07-29

### Added
- Initial project setup
- LEP v1 protocol implementation
- Basic ingest API
