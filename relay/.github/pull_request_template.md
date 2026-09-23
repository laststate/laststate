## What changed

Describe the behavior changed and why.

Closes #

## Validation

- [ ] `gofmt -l cmd internal` produces no output
- [ ] `go vet ./...` passes
- [ ] `go test -race ./...` passes
- [ ] New or changed behavior has tests
- [ ] `go build ./cmd/laststate-relay` succeeds

## Durability

- [ ] Persist-before-ACK invariant is preserved
- [ ] Crash recovery paths are tested or explicitly noted as untested
- [ ] SQLite migration is backward compatible (if applicable)

## Security

- [ ] No raw payloads or secrets appear in logs
- [ ] Admin / ingest separation is preserved
- [ ] Bearer token comparison remains constant-time
- [ ] Redirects are still rejected

## Risk

Describe durability, compatibility, security, or operator impact. Write
`none` when not applicable.

## Runtime evidence

Name the platform, Go version, and scenario tested, or write `not hardware-tested`.
