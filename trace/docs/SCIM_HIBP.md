# Minimal SCIM Groups + HIBP breach check

## SCIM v2 (minimal)

- Base: `internal/api/scim.go`, routes in `internal/api/server.go:118-129`,
  store in `internal/store/auth.go:198-352`, migration
  `internal/db/migrations/025_scim_groups.sql`.
- Users: `GET/POST /scim/v2/Users`, `GET/PUT/PATCH/DELETE /scim/v2/Users/{id}`
  (`filter=userName eq`, `startIndex/count`, `active=false` = deprovision).
- Groups (flat, no nesting — RFC 7644 §4.2 MAY):
  `GET/POST /scim/v2/Groups`, `GET/PUT/PATCH/DELETE /scim/v2/Groups/{id}`
  (`filter=displayName eq`, pagination; `PUT/PATCH` only swaps `members`;
  `displayName` immutable — rename via delete+create; `DELETE` cascades).
- Auth: `requireUI(..., "admin")` + `scimScopeOrg(?organization_id)` (org member).
- OpenAPI: `/scim/v2/Users`, `/scim/v2/Users/{id}`, `/scim/v2/Groups`,
  `/scim/v2/Groups/{id}` in `internal/api/openapi.go`.
- Audit: `auth.scim_group_create` with IP/UA.

## HIBP breach check

- `internal/store/phase2.go:CheckPasswordPolicy` (12–128 chars + local
  `breachedPasswords` blocklist + `breachedViaHIBP()`).
- `breachedViaHIBP`: k-anonymity — only the SHA-1 prefix (5 hex) leaves the process;
  `GET https://api.pwnedpasswords.com/range/{prefix5}`, 3s timeout,
  `User-Agent: laststate-trace-pwcheck`, 1MB cap, **fail-open** (network failure
  never blocks onboarding; the local blocklist still applies).
- Opt-in: `HIBP_CHECK=true` (default `false`; see `.env.example`).
- Coverage: `signup` (`api/auth_endpoints.go:apiSignup`), `register`
  (`api/product.go:apiRegister` via `CreateUser`), `invite-accept`
  (via `AcceptInvite→CreateUser`), `reset-password` (`apiResetPassword`);
  `Login` does not reject (correct); `EnsureAdmin` now uses `CheckPasswordPolicy`
  (previously only `len>=12`); SCIM creates `passwordless` (no policy — correct).
- Operations: enable with `HIBP_CHECK=true`; in environments without egress, keep
  `false` (the local blocklist covers the minimum).
