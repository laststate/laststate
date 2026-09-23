# Relay instructions for GitHub Copilot

The complete, tool-neutral guide is [AGENT.md](../AGENT.md). Apply it together
with the nearest scoped `AGENTS.md` file before proposing or editing code.

- Relay is an offline-first LEP collector and delivery gateway. Persist before
  ACK: never send `ACK_STORED` before the raw object is durably written.
- Treat all network input as hostile: devices, MQTT brokers, HTTP clients,
  subprocess adapters, and directory sources are untrusted. Bound all lengths
  and reject malformed LEP before allocation.
- Admin and ingest APIs are separate. Admin binds to loopback by default. Use
  constant-time token comparison. Reject redirects.
- Raw event payloads and secrets are never included in normal logs.
- Add focused tests for failure paths, crash recovery, auth boundaries, and
  durability guarantees. Use `go vet`, `go test -race`, and report what was
  actually run.
- Preserve unrelated work, avoid generated build output, use the configured
  human Git identity, and never use an `agent/` branch prefix.
- Read [SECURITY.md](../SECURITY.md), [CONTRIBUTING.md](../CONTRIBUTING.md),
  and the relevant files under `internal/` and `docs/` for security- or
  durability-sensitive work.
