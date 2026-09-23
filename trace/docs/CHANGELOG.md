# Changelog

## 0.8.0

### Security

- Disable public registration by default
- OIDC/SAML require organization membership
- HttpOnly session cookies; trusted-proxy XFF
- Optional AES-GCM encryption for notification secrets
- Readiness checks for Postgres, object store, queue, and migrations

### Reliability

- Ingest enqueues through the configured `queue.Jober` (not only Postgres jobs)
- Redis lease reclaim; Cloudflare HTTP adapter fails closed on network errors
- Object store unique temp files and content re-verification
- Idempotent ingest returns HTTP 202 with `status=duplicate`

### Product

- Multi-arch analysis improvements (DWARF CFI / EHABI / ELF symbols)
- Dashboard charts, brand assets, boot splash, live poll
- On-call schedules, escalation policies (delayed levels queued)
- Query DSL and analytics NDJSON export
- Minimal JS/Python ingest clients under `sdk/`

### Ops

- Helm chart version 0.8.0: Secret, backup PVC, security env
- Migrations `004`–`008`
