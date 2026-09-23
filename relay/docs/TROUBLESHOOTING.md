# Troubleshooting Relay

Common issues, diagnostic commands, and resolution steps.

## Quick diagnostics

```bash
# Validate configuration (always start here)
laststate-relay config validate relay.yaml

# Run health checks and port probes
laststate-relay doctor --config relay.yaml

# Check spool state and disk usage
laststate-relay status --config relay.yaml

# Inspect spool usage and delivery counters
laststate-relay status --config relay.yaml
```

## Config validation errors

**Symptom:** Relay refuses to start with a validation error.

| Error | Cause | Fix |
|-------|-------|-----|
| `admin token required on non-loopback` | Admin listener bound to `0.0.0.0` without a token | Add `admin.token: env:LASTSTATE_ADMIN_TOKEN` or bind to `127.0.0.1` |
| `remote destination URL must be HTTPS` | A trace destination uses `http://` | Change to `https://` or move the destination to loopback |
| `unknown field in destination.auth` | Typo in auth config | Check against [spec/trace-api.md](../spec/trace-api.md) |
| `spool.min_free_bytes exceeds max_bytes` | Misconfigured spool limits | Set `min_free_bytes < max_bytes` |

**Always re-run** `laststate-relay config validate` after editing.

## Spool and disk pressure

**Symptom:** Events stop being ingested, admin API returns `507 Insufficient Storage`.

1. Check free space: `laststate-relay status`
2. If `pressure` is `reject-new`, events are being rejected at the source.
3. Options:
   - **Short term**: run `laststate-relay spool reconcile --config relay.yaml` to mark already-delivered events as purged.
   - **Medium term**: reduce `spool.max_bytes` or increase `spool.delivered_retention`.
   - **Long term**: ensure at least one destination is healthy and delivery is flowing.

**Never** set `spool.fsync: none` in production — you will lose data on power loss.

## Delivery failures

### Circuit breaker open

**Symptom:** Logs show `circuit breaker open` and delivery worker pauses.

1. Identify the failing destination:
   ```bash
   laststate-relay destinations list --config relay.yaml
   ```
2. Check the destination URL, auth, and TLS configuration.
3. Common causes:
   - Remote Trace URL changed or unreachable
   - TLS certificate expired or self-signed (set `insecure_skip_verify: false` in production)
   - Bearer token rotated without updating relay config
4. After fixing the root cause, the circuit breaker resets automatically after
   `delivery.circuit_breaker.open_for`. To force a reset:
   ```bash
   laststate-relay destinations resume <id> --config relay.yaml
   ```

### Retry exhaustion

**Symptom:** Events remain in `pending` state after many retries.

Relay retries with exponential backoff up to `delivery.retry.max_attempts` (default 50). If an event exhausts retries, it remains pending in the spool and is reported by `status`.

- **Check spool state:** `laststate-relay status --config relay.yaml`
- **Replay pending events:** `laststate-relay replay --config relay.yaml` (after fixing the destination)

## Crypto and authentication errors

**Symptom:** Ingest rejected with `invalid signature` or `unauthorized`.

### Envelope verification failed

- Verify the device is using the same `key_id` configured in `crypto.keys`.
- Check that `crypto.active_key_id` matches the device's current key.
- Ensure `crypto.replay_window` is not smaller than the device's event rate.

### Bearer token rejected

- Admin and ingest tokens are **independent**. Use the ingest token on source endpoints and the admin token on admin endpoints.
- Tokens are compared with constant-time comparison — a timing leak is not possible, but a mistyped or expired token will be rejected.
- Regenerate tokens by rotating the secret referenced in `admin.token` / `source.http.token`, then restart the relay.

### Nonce collision detected

Relay rejects events with repeated nonces to prevent related-key attacks. If you see `nonce collision` errors:

1. Check the device's random number generator — it must use a CSPRNG, not a linear congruential generator.
2. If the device fleet is small and events are high-volume, consider increasing `crypto.replay_window` or reducing the event rate.

## Source issues

### Serial source: no data received

1. Verify the serial device exists and permissions allow access (Linux: `dialout` group).
2. Check baud rate, device path, and that no other process has the port open.

### MQTT source: connection dropped

1. Check the broker URL, TLS certificate validity, and that the broker is reachable.
2. Ensure the MQTT client ID is unique — duplicate IDs cause silent disconnections.

### HTTP source: 413 Payload Too Large

- Increase `http.max_body_bytes` in the source config.
- Default is 4 MiB — LEP envelopes can exceed this with large memory dumps.

### TCP source: connection refused

- Check `tcp.listen` address and port.
- Ensure no firewall is blocking the port.
- Verify `tcp.max_clients` is not exhausted.

## Symbolication failures

**Symptom:** Stacks show `??` or unresolved addresses.

1. Verify `analysis.llvm_symbolizer` path points to a valid `llvm-symbolizer` binary.
2. For ARM: ensure `analysis.arm_addr2line` is set and points to the correct toolchain.
3. For RISC-V: ensure `analysis.riscv_addr2line` is set.
5. Check `analysis.source_path_maps` if source paths differ between build and analysis environments.

## Admin API issues

**Symptom:** Admin endpoints return `401 Unauthorized` or `403 Forbidden`.

- Admin API binds to loopback by default. To access from a remote host, set `admin.listen` to a specific interface and provide a token.
- Token scope is fixed: admin tokens cannot be used for ingest and vice versa.

## Verbose output

The relay runs with a live TUI dashboard and writes structured request logs to
stderr. For daemon-style diagnostics, inspect `laststate-relay status` and the
admin API (`GET /v1/health`) while the relay is running.

```bash
laststate-relay run --config relay.yaml
```

## Recovery procedures

### After crash

```bash
# 1. Validate config
laststate-relay config validate relay.yaml

# 2. Reconcile spool state
laststate-relay spool reconcile --config relay.yaml

# 3. Start relay
laststate-relay run --config relay.yaml
```

### After disk failure

```bash
# 1. Restore from backup
laststate-relay restore --input backup.zip --data-dir DIR

# 2. Reconcile
laststate-relay spool reconcile --config relay.yaml

# 3. Verify delivery health
laststate-relay status --config relay.yaml
```

## Getting help

When reporting issues, include:

- Relay version or commit (`laststate-relay version`)
- Configuration (redact secrets): `laststate-relay config show --config relay.yaml`
- Spool status: `laststate-relay status --config relay.yaml`
- Relevant dashboard events
- Platform and architecture

For security-sensitive issues, follow the process in [SECURITY.md](../SECURITY.md).
