# SAML SSO — implementation, lib and fallback

Status: **experimental, supported in Trace; out of scope in billing-service**
(auth is centralized in Trace; billing-service consumes entitlements via Admin API).

## Library decision (audited or documented fallback)

- Used: `github.com/russellhaering/goxmldsig v1.6.1` + `github.com/beevik/etree v1.7.0`
  (direct in `go.mod`; enveloped-signature verification + exclusive
  canonicalization + digest + RSA/ECDSA in `internal/saml/saml.go:verifySignature`).
- Evaluated and **not adopted**: `crewjam/saml` (full SP+IdP, market standard).
  Reason: larger surface (bindings, metadata, sessions, IdP) than the current
  minimum (SP metadata + ACS + Conditions). Re-evaluate when a broad IdP
  matrix is needed (see "Limitations").
- Audit: `goxmldsig` is a small, single-maintainer lib with **no formal
  external audit**. Adopted mitigations:
  - `Strict=true` in the XML parser + rejection of unsigned assertions by default.
  - `Conditions` enforcement (audience, recipient, `NotBefore`/`NotOnOrAfter`,
    `SubjectConfirmation` expiry, 5min skew) in `ParseResponse`.
  - Tamper tests: `internal/saml/saml_test.go:TestSignedRoundTrip`
    (swapping `signed@example.com`→`mallory@example.com` must fail) + rejection of
    expired/garbage/certless audience.
  - `govulncheck` + Trivy + SBOM in CI (see `SECURITY.md`).
- Documented fallback (insecure path for local testing only):
  - `VerifyOpts.AllowNoCert` ↔ `TRACE_SAML_INSECURE` (default `false`).
  - Without a certificate: ACS answers `501 saml_not_ready`, except for explicit local testing.
  - `ValidateProduction()` rejects `TRACE_SAML_INSECURE=true` in production.
  - `SECURITY.md`, `README.md` and `.env.example:168` mark it as experimental.

## Wiring

- `internal/api/enterprise.go:apiSAMLMetadata|apiSAMLLogin|apiSAMLACS|apiSAMLConfig`
- `internal/store/saml_analytics.go:GetSAMLConfig|UpsertSAMLConfig` (`saml_configs`)
- `internal/config/config.go:SAMLInsecure`, `TRACE_SAML_INSECURE`
- Routes: `GET /saml/metadata`, `GET /saml/login`, `POST /saml/acs`,
  `GET|PUT /api/saml/config` (admin).

## Limitations (out of scope)

- Full IdP matrix (`Full SAML IdP matrix` in `docs/GAPS_FILLED.md`).
- SLO/logout, signed AuthnRequest (`AuthnRequestsSigned=false` in metadata).
- `billing-service` has no SAML (only plan mentions in `docs/PRICING.md`,
  `docs/ENTITLEMENTS.md` — auth via Trace).

## Operations

```bash
# IdP cert mandatory; insecure local-only
export TRACE_SAML_INSECURE=false
```

Test: `go test ./internal/saml -run TestSignedRoundTrip -v`
