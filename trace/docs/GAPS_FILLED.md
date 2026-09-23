# Implementation status (v0.8)

| Area | Status | Notes |
|------|--------|--------|
| Unwind / symbolication | Implemented | DWARF CFI ops, EHABI, ELF symbol fallback; not full libunwind |
| Notifications | Implemented | Retry, templates, PagerDuty, Slack/Discord; secrets encrypted when key set |
| Queues | Implemented | postgres/redis production paths; nats/cf adapters documented honestly |
| Dashboard UI | Implemented | Charts, issue detail, breadcrumb replay |
| Search / export | Implemented | FTS + event query DSL + NDJSON export |
| Scale | Documented | See SCALE.md; mid-scale Postgres design |
| Auth | Implemented | Password (12–128, blocklist, lockout), MFA enforced (TOTP + emailed backup codes), OIDC + RP logout, hashed reset tokens, session revoke-on-reset, SAML verified (goxmldsig + Conditions); SCIM Users minimal (Groups missing) |
| Client SDKs | Minimal | `sdk/js`, `sdk/python` for ingest only |
| GC | Implemented | Retention + orphan sweep |
| E2E | Partial | Package tests + Playwright UI; full stack in E2E.md |

## Out of scope for this tree

- ClickHouse / multi-region HA
- SOC 2 certification process
- Full SAML IdP matrix
- Browser session video replay (firmware uses breadcrumb replay)
