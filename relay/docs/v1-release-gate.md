# Relay v1.0.0 release gate

Do not tag `v1.0.0` until every box is checked and linked from the release PR.
This is the executable version of the "Product v1.0.0 still needs" list in
`docs/compatibility-matrix.md`.

## 1. Protocol freeze

- [ ] `spec/` drafts used by Relay are frozen (binary batch, LEP crypto wire,
      attachment TLVs, identity TLVs) — linker notes the frozen commit.
      Reference: protocol `FREEZE.md` (LEP v2.0.0, decision 2026-08-26;
      freeze note `9606571`); Relay's vendored vectors live in
      `internal/lep/testdata/` and CI runs them on every push.
- [ ] `go test ./... -count=1` green on the tag commit.
- [ ] `go vet ./... && golangci-lint run --timeout=5m` clean.

## 2. Durability soak

- [ ] `spool.fsync: full` power-loss soak on a production filesystem
      (ext4 + overlayfs/Docker): kill -9 mid-ingest ×50, `spool reconcile`
      recovers with zero ACK-without-write.
- [ ] Stale delivery-lease recovery verified (restart with in-flight batch).

## 3. Latch + Trace E2E

- [ ] Latch (real MCU build) → Relay → Trace ingest returns 200 for 1k
      envelopes, duplicates collapse by stable idempotency key.
- [ ] `.lsbundle` export/import round-trip verified (signed bundle).

## 4. Transports (HIL)

- [ ] `docs/hil-matrix.md` minimum bar green: C1+C2, B1+B2, L1+L2, P1;
      C3/B3/L3 green or tracked with evidence.

## 5. External security review

- [ ] Review report filed (scope: ingest auth, admin bearer, spool crypto,
      subprocess adapter sandboxing). Findings fixed or accepted in writing.
- [ ] `docs/security.md` updated with review reference + date.

## 6. Ops readiness

- [ ] `docs/production.md` checklist walked on a pilot gateway.
- [ ] `backup` / `restore` + `spool reconcile` rehearsed; RTO recorded.
- [ ] Prometheus + Grafana dashboard (`packaging/grafana/`) scraping;
      Datadog/New Relic bridge documented if the fleet uses them
      (see root `docs/observability-bridges.md`).

## Tagging

```bash
git -C relay status --porcelain   # must be empty
go test ./... -count=1
git tag -a v1.0.0 -m "Relay v1.0.0: protocol freeze + HIL + security review"
git push origin v1.0.0
```

Post-tag: update `docs/compatibility-matrix.md` status line and root
`docs/RELEASE_GATE.md` with the tag commit.
