# Roadmap

## Done in this repo

- Durable spool (content-addressed objects + SQLite WAL)
- Startup reconcile and stale delivery-lease recovery
- Sources: serial, TCP/TLS, HTTP/TLS, UDP, directory, MQTT, subprocess adapter
- Latch stream, COBS, LSAK ACK/NACK
- Delivery: mirror / priority / route / local-only, batch (JSON + binary), zstd,
  retries, Retry-After, circuit breaker
- Admin vs ingest API split, bearer auth, health, readiness, metrics
- ELF catalog, build-id match, DWARF symbolication, multi-arch basics, RTOS TLVs
- LEP encode + HMAC / AEAD crypto draft, key IDs, replay window
- Signed `.lsbundle` export/import
- CLI/TUI, Docker, systemd, Windows service scripts, launchd, Homebrew, deb/rpm
  templates, udev, Grafana dashboard, backup/restore

## Protocol drafts (need upstream freeze)

Specs under `spec/` are drafts Relay uses today:

- Binary batch + zstd negotiation
- LEP crypto wire layout
- Attachment chunk TLVs
- Build / project / release identity TLVs
- Probe waveform TLVs (not implemented yet)

## Still open — transports

- [x] SocketCAN, CAN FD, USB-CAN (`internal/source/can*.go` + HIL rows C1–C3
  in `docs/hil-matrix.md`)
- [x] BLE GATT via JSON-over-TCP gateway (`internal/source/ble*.go` + rows B1–B3)
- [x] LoRa / LoRaWAN via MQTT (TTN v3 / ChirpStack) + HTTP poll
  (`internal/source/lorawan*.go` + rows L1–L3)
- Addressed RS-485 (wrap via adapter SDK for now)
- USB HID
- Full gRPC adapter service (subprocess path exists)

## Still open — analysis

- Full DWARF CFI / ARM EHABI unwinding
- Richer ThreadX / NuttX / CMSIS-RTOS views
- Heap / deep memory-map classification
- Artifact provenance signatures

## Still open — ops

- MSI, signed update channels, macOS notarization
- Native OS keyrings (`keyring:` is rejected until then)
- OpenTelemetry (Prometheus is there)
- Fault-injection soak + HIL matrix
- External security review

## v1.0.0

See [docs/compatibility-matrix.md](docs/compatibility-matrix.md). Do not tag
`v1.0.0` until protocol freeze, durability soak, Latch+Trace E2E, and review.
