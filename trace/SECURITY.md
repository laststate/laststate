# Security Policy

Report suspected vulnerabilities through the repository's [private vulnerability
reporting form](https://github.com/laststate/trace/security/advisories/new).
Do not open a public issue containing API keys, captured memory, exploit
details, or production endpoint credentials.

## Security defaults

| Setting | Default | Notes |
|---------|---------|-------|
| `TRACE_ALLOW_PUBLIC_REGISTER` | `false` | Viewer-only if enabled |
| `TRACE_OIDC_AUTO_JOIN` | `false` | Invite/membership required |
| `TRACE_SAML_INSECURE` | `false` | Unsigned ACS disabled |
| `TRACE_TRUSTED_PROXIES` | empty | XFF ignored unless peer matches |
| `TRACE_SECRETS_KEY` | empty | AES-GCM for channel/alert secrets when set |
| `TRACE_COOKIE_SECURE` | auto | `true` when `TRACE_PUBLIC_URL` is https |

`ValidateProduction()` rejects open UI, public register, SAML insecure, and OIDC auto-join.

## Sessions

Browser sessions use an **HttpOnly** cookie (`trace_session`, 7-day lifetime
matching `sessions.expires_at`). Bearer tokens remain supported for Relay and
scripts. Prefer cookies for the SPA (`credentials: 'same-origin'`).
Password reset revokes all sessions; MFA (TOTP or single-use emailed code)
is enforced at login when enrolled. Auth endpoints carry a strict 20 req/min
per-IP limiter on top of the global one.

## Cryptographic design

- **Secrets at rest**: AES-GCM encrypts channel and alert secrets when
  `TRACE_SECRETS_KEY` is set. Key material is derived from a 256-bit random
  value and never stored in plaintext.
- **Token comparison**: Bearer tokens use constant-time comparison to prevent
  timing attacks.
- **Webhook signatures**: Outbound webhooks validate URLs (SSRF protection)
  and use exponential backoff for retries.

## Feature maturity

| Feature | Status |
|---------|--------|
| Password login + scopes | Supported (12–128 chars, common-password blocklist + opt-in HIBP k-anonymity, 5-fail lockout) |
| MFA (TOTP + emailed backup codes) | Enforced at login when enrolled |
| OIDC (PKCE, JWKS) | Supported; membership required; RP logout + error route |
| SAML ACS | Verified (goxmldsig signature + Conditions); IdP cert required, insecure path prod-rejected (see `docs/SAML.md`) |
| SCIM | Users minimal profile (list/create/read/update/deprovision) + Groups flat (list/create/read/replace-members/delete, no nesting) |
| Sessions | IP/UA persisted + `TouchSession`; idle timeout `TRACE_SESSION_IDLE_TIMEOUT` (default 24h, lazy per-request); list/revoke via `/api/me/sessions` |
| HIBP breach check | Opt-in `HIBP_CHECK=true` (k-anonymity `api.pwnedpasswords.com/range`, 3s timeout, fail-open; local blocklist always on) |

## Production hardening

```bash
export TRACE_ENV=production
export TRACE_OPEN_UI=false
export TRACE_ALLOW_PUBLIC_REGISTER=false
export TRACE_OIDC_AUTO_JOIN=false
export TRACE_SAML_INSECURE=false
export TRACE_TRUSTED_PROXIES=10.0.0.0/8,172.16.0.0/12
export TRACE_SECRETS_KEY="$(openssl rand -base64 32)"
export TRACE_QUEUE=postgres   # or redis
```

## Incident response

### Vulnerability disclosure

1. Submit details through the [private vulnerability reporting form](https://github.com/laststate/trace/security/advisories/new).
2. Acknowledge receipt within 48 hours.
3. Work with maintainers to validate and patch.
4. Coordinate disclosure timeline with the reporter.
5. Publish security advisory after patch is available.

### Security contact

For urgent security issues, contact the maintainers directly via the GitHub
private vulnerability reporting form.

## Dependencies

- **SBOM generation**: Generated on every CI run.
- **Trivy scanning**: Filesystem scans against the source tree in CI.
- **govulncheck**: Go vulnerability scanner run as part of the security job.
- **Dependabot**: Automated dependency updates for Go modules and GitHub Actions.
