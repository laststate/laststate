# Last State Relay

Offline-first gateway between devices running **Latch** and one or more **Trace**
backends. Relay accepts LEP envelopes (serial, TCP, HTTP, MQTT, adapters, or
files), validates them, stores them on disk, ACKs the device only after the
write, optionally analyzes crashes locally, and forwards events with retries
and idempotency.

[![CI](https://github.com/laststate/relay/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/laststate/relay/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE.md)
[![Go Reference](https://img.shields.io/badge/go-reference-1.26+-007D9C.svg)](go.mod)
[![Docker](https://img.shields.io/badge/docker-ready-2496ED.svg)](Dockerfile)

## Guarantees

- **Persist before ACK** — `LSAK ACK_STORED` only after object write + SQLite commit
- **At-least-once delivery** — duplicates possible after a lost response; stable IDs make retries safe
- **Works offline** — outages do not block collection
- **Raw kept locally** — original LEP bytes stay exportable and checksummed
- **No cloud lock-in** — talks only to the Trace HTTP contract

## Features

- LEP v1/v2 validation/encode (v2 is the current wire version; v1 accepted), CRC-32/IEEE, optional HMAC + ChaCha20-Poly1305, zstd payloads
- Latch stream and COBS framing with resync
- Sources: serial, TCP/TLS, UDP, HTTP/TLS, directory, MQTT, subprocess adapters
- Delivery: mirror / priority / route / local-only, JSON or binary batch, zstd
- Privacy filters per destination (local raw untouched)
- ELF catalog, build-id match, DWARF + external symbolizers, Cortex-M and multi-arch basics
- CLI/TUI, Prometheus, Grafana dashboard template, backup/restore
- Packaging: Docker, systemd, deb/rpm templates, Windows service, launchd, Homebrew

## Build

```bash
go test ./...
go build -trimpath -ldflags "-s -w \
  -X main.cliVersion=v0.4.0 \
  -X main.gitCommit=$(git rev-parse --short HEAD) \
  -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o bin/laststate-relay ./cmd/laststate-relay
```

Needs Go 1.26.1+.

## Quick start

```bash
cp relay.yaml.example relay.yaml
laststate-relay config validate relay.yaml
laststate-relay run --config relay.yaml
```

Serial collect:

```bash
laststate-relay collect \
  --serial COM5 \
  --baud 115200 \
  --framing latch-stream \
  --ack lsak-v1 \
  --data-dir ./data
```

Offline analysis (explicit ELF or auto-match from the artifact catalog):

```bash
laststate-relay analyze crash.lep --elf build/firmware.elf
laststate-relay analyze crash.lep --data-dir ./data
```

## APIs

Admin and ingest are separate. Do not expose admin publicly without TLS and a
strong token.

**Admin**

```text
GET  /v1/status | /v1/health | /v1/ready
GET  /v1/events | /v1/events/{id}
GET  /v1/destinations
POST /v1/events/{id}/replay
POST /v1/destinations/{id}/pause|resume
POST /v1/spool/prune|reconcile
```

**Ingest**

```text
GET  /v1/ingest/capabilities
GET  /v1/relay/capabilities
POST /v1/ingest
POST /v1/events:batch
```

**Trace (as destination)**

```text
GET  /v1/relay/capabilities
POST /v1/ingest
POST /v1/events:batch
```

Batching is per destination. Relay discovers limits, sends JSON or binary batches
(optionally zstd), retries only failed items, and falls back to single-event POST
for older servers.

## CLI

```text
run · collect · info · inspect · analyze
import · export · replay · status · doctor
artifacts · destinations · bundle · spool
backup · restore · config · version
```

## Docs

| Doc | Topic |
|-----|--------|
| [docs/architecture.md](docs/architecture.md) | How the pieces fit |
| [docs/durability.md](docs/durability.md) | ACK / write path |
| [docs/production.md](docs/production.md) | Deploy checklist |
| [docs/v1-release-gate.md](docs/v1-release-gate.md) | v1.0.0 tagging gate |
| [docs/hil-matrix.md](docs/hil-matrix.md) | CAN/BLE/LoRa/Probe HIL evidence |
| [docs/compatibility-matrix.md](docs/compatibility-matrix.md) | What is ready vs draft |
| [spec/](spec/) | Wire drafts (crypto, batch, TLVs, …) |
| [ROADMAP.md](ROADMAP.md) | What is left |

## Status

Native CAN (SocketCAN/CAN FD/USB-CAN/TCP), BLE GATT, and LoRa/LoRaWAN sources
are implemented (`internal/source/can.go`, `ble.go`, `lorawan.go`) with unit
coverage (`go test ./internal/source/`). Not tagged `v1.0.0` yet — that waits
on the executable gate in [docs/v1-release-gate.md](docs/v1-release-gate.md):
protocol freeze, durability soak, Latch+Trace E2E, the
[HIL matrix](docs/hil-matrix.md), and an external security review.

## Community and security

- [Contributing guide](CONTRIBUTING.md)
- [Security policy](SECURITY.md)
- [Code of conduct](CODE_OF_CONDUCT.md)
- [Support](SUPPORT.md)

## License

Apache-2.0 — [`LICENSE.md`](LICENSE.md)
