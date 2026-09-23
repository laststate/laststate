# Changelog

All notable changes to the LastState Relay gateway.

## [Unreleased]

### Security
- **Bumped `google.golang.org/grpc` to v1.83.2** - fixes GO-2026-6443
  (server panic via missing authority/Host headers) and GO-2026-6348
  (heap exhaustion via HTTP/2 DATA fragmentation) that were failing the
  `security` CI job.

## [0.5.0] - 2026-08-20

### Added
- **LEP v2 codec support** — decoders now accept both LEP v1 and v2 envelopes; the
  relay emits LEP v2 on the wire (v2 is the current wire version). LEP v1 remains
  accepted for backward compatibility.
- **Re-vendored protocol golden vectors** — `internal/lep/testdata/protocol-vectors`
  refreshed from the protocol repo. The manifest (`manifest.json`) now classifies
  each vector by `kind`: `valid`, `invalid`, `crypto-aead`, `crypto-hmac`,
  `stream`, and `lsak`.

### Changed
- Lint and dead-code cleanup across the codebase (golangci-lint and manual review).

## [0.4.0] — 2026-08-15

### Added
- **Binary batch delivery** — zstd-compressed JSON/binary batches to Trace
- **LEP crypto wire layout** — draft spec for envelope encryption
- **Attachment chunk TLVs** — large memory dump support
- **Build/project/release identity TLVs** — enhanced device identity
- **Signed `.lsbundle`** — export/import with signature verification
- **Windows service** — native Windows service support
- **Homebrew formula** — `brew install laststate/relay/laststate-relay`
- **Grafana dashboard** — relay metrics dashboard template
- **Relay capabilities API** — `GET /v1/relay/capabilities`
- **Batch ingest API** — `POST /v1/events:batch`

### Changed
- CLI: added `bundle`, `doctor`, `config` subcommands
- Delivery: priority routing with per-destination batching

## [0.3.0] — 2026-08-14

### Added
- Serial, TCP/TLS, UDP, HTTP/TLS, directory, MQTT sources
- Latch stream and COBS framing with resync
- Mirror / priority / route / local-only delivery modes
- Admin vs ingest API split with bearer auth
- ELF catalog with build-id match and DWARF symbolication
- Multi-architecture basics (Cortex-M, RISC-V)
- Backup/restore
- Docker, systemd, deb/rpm templates

## [0.2.0] — 2026-08-10

### Added
- Durable spool (content-addressed objects + SQLite WAL)
- Startup reconcile and stale delivery-lease recovery
- LEP v1 validation and encoding
- Basic HTTP and serial sources
- Admin API (status, events, destinations)

## [0.1.0] — 2026-07-29

### Added
- Initial project setup
- LEP v1 codec implementation
- Basic spool storage
