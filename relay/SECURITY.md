# Security policy

Report suspected vulnerabilities through the repository's [private vulnerability
reporting form](https://github.com/laststate/relay/security/advisories/new).
Do not open a public issue containing device keys, captured memory, exploit
details, or production endpoint credentials.

## Security defaults

- Admin and metrics listeners bind to loopback by default.
- Remote Trace destinations must use HTTPS.
- Admin and ingest bearer tokens are independent.
- Authorization uses constant-time comparison.
- Redirects are rejected so credentials are never forwarded to a different host.
- Event and artifact objects use restrictive permissions and content hashes.
- HTTP body size, header size, timeouts, connection count, and concurrent ingest
  are bounded.
- Unknown YAML fields are rejected.
- Raw event payloads and secrets are never included in normal logs.

## Cryptographic design

Relay validates LEP envelope integrity with CRC-32/IEEE and authenticity with
HMAC-SHA-256 or XChaCha20-Poly1305 before persisting or forwarding. Keys are
derived via HKDF-SHA-256 with domain separation. Nonces are checked for
repetition. Replay windows are enforced per `key_id`. Raw event payloads are
stored on disk with restrictive permissions; content hashes enable tamper
detection on export.

The older HMAC-SHA-256 envelope format remains supported for verification only
unless `allow_legacy_hmac` is explicitly enabled by the operator.

## Threat model

Relay treats devices, imported files, remote destinations, artifacts, and
network clients as untrusted. Operators must:

- Protect the data directory with restrictive file permissions.
- Use TLS outside loopback for all remote communication.
- Rotate admin and ingest tokens independently.
- Apply privacy filters and redaction policies before forwarding memory dumps
  to third-party destinations.
- Review the compatibility matrix in [docs/compatibility-matrix.md](docs/compatibility-matrix.md)
  before enabling draft features in production.

## Provisioning requirements

- Provision a unique random 256-bit master key per device fleet. Do not derive
  it from a serial number, MAC address, password, or firmware secret.
- Register a cryptographically secure random provider before enabling envelope
  or at-rest encryption.
- Increment `key_id` on every key rotation and retain old keys server-side only
  for the required migration window.
- Use verified TLS in addition to envelope encryption. Never set peer
  verification to optional in production.

## Assurance and limitations

Relay includes race-detector tests, fuzz targets for LEP parsers, sanitizer
runs, and durability verification. These checks do not replace an independent
security review, side-channel evaluation on the target platform, secure
provisioning review, or product certification. Do not claim FIPS, Common
Criteria, or PSA certification based on this repository alone.
