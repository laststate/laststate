# Security Policy

Report suspected vulnerabilities through the repository's [private vulnerability
reporting form](https://github.com/laststate/billing-service/security/advisories/new).
Do not open a public issue containing payment secrets, customer PII, API keys,
or production endpoint credentials.

## Security defaults

- All API endpoints require a scoped bearer token or API key.
- Webhook endpoints use provider-specific signature verification (HMAC-SHA256).
- Admin and billing tokens are independent; scopes are enforced per-endpoint.
- All remote communication (Stripe, Mercado Pago, Crypto gateway) requires TLS.
- Payment secrets and API keys are never logged or exposed in error messages.
- Database queries use parameterized statements to prevent SQL injection.
- Unknown YAML/JSON fields in webhook payloads are rejected.

## Cryptographic design

- **Webhook signatures**: HMAC-SHA256 with provider-specific secret keys.
  - Stripe: `Stripe-Signature` header verified against `whsec_` endpoint secret.
  - Mercado Pago: `x-signature` header verified against webhook secret.
  - Crypto: `x-cb-signature` or `x-nowpayments-signature` header verified.
- **API tokens**: Stored as SHA-256 hashes; never stored in plaintext.
- **JWT secrets**: Used for session token signing; rotated per environment.

## PCI-DSS compliance

This service handles payment data and must comply with relevant PCI-DSS
requirements. The following controls are implemented:

### Scope reduction

- **No raw PAN storage**: Card numbers are never stored. Stripe handles
  card data via hosted checkout or Elements.
- **No CVV storage**: CVV codes are never received or stored.
- **Webhook-only payment data**: Only event metadata (event type, amount,
  status) is stored — never full card details.

### Encryption

- **In transit**: All APIs use TLS 1.2+. Internal communication uses mTLS.
- **At rest**: Database backups are encrypted. Sensitive columns use
  application-level encryption where required.

### Access control

- **Token scoping**: API tokens have explicit scopes (`read`, `write`,
  `admin`, `billing`, `entitlements`, `analytics`).
- **RBAC**: Organization-level access control for billing data.
- **Audit logging**: All administrative actions are logged with timestamps,
  source IP, and actor identity.

### Key management

- **API key rotation**: Supported via admin API. Old tokens remain valid until
  explicitly revoked.
- **Webhook secret rotation**: Supported per provider. Old secrets remain valid
  during migration window.
- **JWT secret rotation**: Requires service restart; plan during maintenance.

### Network security

- **Rate limiting**: Per-IP and per-token rate limits prevent abuse.
- **CORS**: Configurable allowed origins; defaults to development origins.
- **TLS termination**: Must be handled by a reverse proxy in production.
- **Firewall**: Only necessary ports exposed; admin API on loopback only.

### Data handling

- **Invoice data**: Stored with organization context; accessible only to
  authorized org members.
- **Payment attempts**: Retained for dunning tracking; purged after resolution.
- **Webhook payloads**: Stored for idempotency; redacted on export.

### Monitoring and detection

- **Failed webhook verification**: Logged with source IP for anomaly detection.
- **Rate limit violations**: Logged and trigger alerting.
- **Unauthorized access attempts**: Logged with full request context.
- **Payment failures**: Tracked via dunning pipeline with escalation.

## Compliance beyond PCI-DSS (SOC 2 / ISO 27001 / HIPAA)

PCI-DSS controls above are the baseline. The mappings below are
**self-assessed**; retain audit logs, webhook idempotency records, and key
rotation history as evidence for customer questionnaires and formal audits.

### SOC 2 (Trust Services Criteria)

| Criterion | Implementation | Evidence |
|-----------|----------------|----------|
| CC6.1 logical access | Scoped bearer/API keys, org-level RBAC, admin on loopback | `internal/api/auth.go`, access logs |
| CC6.6 encryption | TLS 1.2+ transit, encrypted backups, app-level column encryption where required | TLS config, backup policy |
| CC7.2 monitoring | Webhook 401s, rate-limit violations, unauthorized attempts logged with IP/context | log samples, `operations/README.md` alerts |
| CC7.3 incident response | Disclosure process below, 48h ack, coordinated patch | advisory history |
| A1.2 availability | Dunning Day 0/1/3/7/14 + auto-suspension, idempotent webhooks | `internal/billing/dunning.go` |

### ISO/IEC 27001:2022 (Annex A)

| Control | Implementation | Evidence |
|---------|----------------|----------|
| A.5.15–A.5.18 access control | Token scoping, RBAC, auth on every endpoint | `SECURITY.md` defaults |
| A.8.24 cryptography | HMAC-SHA256 webhooks per provider, SHA-256 token hashes, JWT rotation | `internal/webhook/handler.go` |
| A.8.13 backups / A.8.14 redundancy | Encrypted DB backups, webhook persistence for replay | `docs/DATABASE.md` |
| A.5.24 incident response | Private reporting → validate → patch → advisory | this file |

### HIPAA (when handling PHI-adjacent metadata)

| Safeguard §164.312 | Implementation | Evidence |
|--------------------|----------------|----------|
| (a)(1) access control | RBAC + scoped tokens, MFA-ready IdP upstream | access reviews |
| (a)(2)(iv) encryption | TLS transit, encrypted backups | config + backup logs |
| (b) audit controls | Admin actions logged with timestamp/IP/actor | audit log export |
| (d) integrity | Webhook HMAC verification, parameterized queries | handler code |

### Audit guide

1. Export: scoped access list, rotation history, webhook 401/500 counts,
   dunning outcomes for the period.
2. Map each finding to the tables above; file gaps as issues with owners.
3. Re-run `go test ./...`, `go vet ./...`, Trivy + GoSec from CI; attach SBOM.

## Incident response

### Vulnerability disclosure

1. Submit details through the [private vulnerability reporting form](https://github.com/laststate/billing-service/security/advisories/new).
2. Acknowledge receipt within 48 hours.
3. Work with maintainers to validate and patch.
4. Coordinate disclosure timeline with the reporter.
5. Publish security advisory after patch is available.

### Security contact

For urgent security issues, contact the maintainers directly via the GitHub
private vulnerability reporting form.

### Response process

1. Submit vulnerability details through the private reporting form.
2. Acknowledge receipt within 48 hours.
3. Work with maintainers to validate and patch.
4. Coordinate disclosure timeline with the reporter.
5. Publish security advisory after patch is available.

## Dependencies

- **SBOM generation**: Generated on every CI run.
- **Dependabot**: Automated dependency updates for Go modules.
- **GoSec**: Static analysis for Go security issues in CI.
- **Trivy**: Container image scanning in CI.
