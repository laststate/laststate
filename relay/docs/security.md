# Security Documentation

## Security Architecture

Relay validates LEP envelope integrity with CRC-32/IEEE and authenticity with
HMAC-SHA-256 or XChaCha20-Poly1305 before persisting or forwarding. Keys are
derived via HKDF-SHA-256 with domain separation. Nonces are checked for
repetition. Replay windows are enforced per `key_id`.

### LEP Envelope Encryption

- **AEAD modes**: XChaCha20-Poly1305 (preferred) and AES-256-GCM are supported
  for authenticated encryption of envelope payloads.
- **Key derivation**: HKDF-SHA-256 with domain separation ensures each key is
  only ever used for its intended purpose (envelope encryption, HMAC, signing).
- **Nonces**: Each envelope carries a 96-bit random nonce. Relay detects and
  rejects nonce collisions to prevent related-key attacks.
- **Replay protection**: Envelopes older than the configured replay window are
  discarded. Replay windows are enforced per `key_id`.

### HMAC Integrity

- Raw event payloads are HMAC'd with SHA-256 before storage.
- The older HMAC-SHA-256 envelope format remains supported for verification
  only unless `allow_legacy_hmac` is explicitly enabled by the operator.

### Key Management

- Provision a unique random 256-bit master key per device fleet.
- Do not derive keys from serial numbers, MAC addresses, passwords, or firmware
  secrets.
- Increment `key_id` on every key rotation. Retain old keys server-side only
  for the required migration window.
- Use a cryptographically secure random provider before enabling envelope or
  at-rest encryption.

## Transport Security

- **TLS 1.2+**: All remote communication (remote Trace destinations, HTTP
  sources, admin API on non-loopback interfaces) requires TLS 1.2 or higher.
- **Certificate pinning**: Relay supports certificate pinning for remote
  destinations to mitigate CA compromise risks.
- **mTLS**: Mutual TLS is supported for non-loopback TCP and HTTP sources.
  `insecure_skip_verify` must remain off in production environments.
- **Redirect protection**: HTTP redirects are rejected so credentials are
  never forwarded to a different host.

## Authentication

- **Bearer tokens**: Admin and ingest APIs use independent bearer tokens.
  Authorization is performed with constant-time comparison to prevent timing
  attacks.
- **Admin tokens**: The admin API is bound to loopback by default. Admin and
  metrics listeners use separate credentials.
- **Per-endpoint policies**: Each HTTP source and destination can have its own
  authentication policy. Use `env:` or `file:` references for production tokens
  rather than literal values in configuration.
- **Token scope**: Admin tokens cannot be used for ingest and vice versa.

## Rate Limiting

- **Token bucket**: HTTP endpoints use a token bucket algorithm to prevent
  abuse and ensure fair resource allocation.
- **Per-IP tracking**: Rate limits are tracked per source IP address to prevent
  a single client from starving others.
- **Bounded resources**: HTTP body size, header size, timeouts, connection
  count, and concurrent ingest are all bounded to prevent resource exhaustion.

## Data Privacy

- **Destination-level TLV filtering**: Privacy filters can be applied per
  destination to redact sensitive fields before forwarding data to third-party
  destinations.
- **Encrypted envelope support**: Raw event payloads can be encrypted at rest
  using the LEP envelope encryption scheme before storage on disk.
- **Content hashes**: Stored events include content hashes (CRC-32) to enable
  tamper detection on export.
- **Restrictive permissions**: Raw event payloads and secrets are stored on
  disk with restrictive file permissions and are never included in normal logs.

## Audit Trail

- **Request logging**: All admin API requests are logged with timestamps,
  source IP, and action taken.
- **Structured request entries**: Request entries use a structured format for
  easier parsing and analysis by log aggregation tools.
- **Event persistence**: Ingested events are persisted with full metadata
  including source identification and timestamps.
- **No payload logging**: Raw event payloads and secrets are never included in
  normal logs.

## Incident Response

### Vulnerability Disclosure

Report suspected vulnerabilities through the repository's [private
vulnerability reporting form](https://github.com/laststate/relay/security/advisories/new).
Do not open a public issue containing device keys, captured memory, exploit
details, or production endpoint credentials.

### Security Contact

For urgent security issues, contact the maintainers directly via the GitHub
private vulnerability reporting form.

### Response Process

1. Submit vulnerability details through the private reporting form.
2. Acknowledge receipt within 48 hours.
3. Work with maintainers to validate and patch.
4. Coordinate disclosure timeline with the reporter.
5. Publish security advisory after patch is available.

## Dependencies

- **SBOM generation**: Software Bill of Materials is generated in both SPDX
  and CycloneDX formats on every CI run via the SBOM workflow.
- **Trivy scanning**: The security job in CI runs Trivy filesystem scans
  against the source tree for known vulnerabilities.
- **govulncheck**: Go vulnerability scanner is run as part of the security job
  to identify issues in the project's direct dependencies.
- **Dependabot**: Automated dependency updates are provided for both Go modules
  and GitHub Actions, scheduled weekly.
