# Running Relay in production

Use this as a short checklist before putting a relay on a factory network or
lab gateway. Tagging `v1.0.0` still needs E2E hardware CI and a security review
— see [compatibility-matrix.md](compatibility-matrix.md).

## Setup

1. **Config**
   - `laststate-relay config validate relay.yaml`
   - Put admin and ingest tokens in `env:` or `file:` refs, not literals
   - Admin without a token is only allowed on loopback
   - Remote Trace URLs must be HTTPS
2. **Spool**
   - Prefer `spool.fsync: full`
   - Set `max_bytes` and `min_free_bytes` so the disk cannot fill silently
3. **Crypto** (optional)
   - Enable `crypto` with 32-byte keys
   - `decrypt_on_ingest` verifies device envelopes; stored bytes stay as received
   - Leave `allow_opaque_forward` on if you only want to forward ciphertext
4. **Delivery**
   - Turn on batching (and zstd if Trace supports it)
   - Tune retry + circuit breaker for your link
5. **Ops**
   - Use the unit files under `packaging/` (systemd, Windows service, Docker, …)
   - Scrape Prometheus; optional dashboard JSON is in `packaging/grafana/`
   - Stop the service before `backup`; run `spool reconcile` after `restore`

## Rules of the road

- ACK only after durable write + SQLite commit
- Event IDs are hashes of the on-wire LEP bytes
- Delivery is at-least-once with stable idempotency keys
- Privacy filters build a *copy* for a destination; local raw is untouched

## Useful commands

```bash
go test ./...
go build -o bin/laststate-relay ./cmd/laststate-relay
laststate-relay config validate relay.yaml
laststate-relay run --config relay.yaml
laststate-relay doctor --config relay.yaml
```
