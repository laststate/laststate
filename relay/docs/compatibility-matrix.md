# Compatibility matrix

Supports **LEP v1 and v2** (v2 is the current wire version; v1 accepted for
backward compatibility). Device path = Latch.

> LEP v2.0.0 is frozen upstream (protocol `FREEZE.md`, decision 2026-08-26;
> freeze note `9606571`). No breaking wire changes without ADR + major bump.
> Relay vendors the golden vectors (`internal/lep/testdata/`) and the
> protocol conformance suite runs them on every CI.

## Protocol

| Component | Version | Where |
|-----------|---------|--------|
| LEP | v1 / v2 (v2 current) | `internal/lep` |
| LEP CRC | CRC-32/IEEE | poly `0x04C11DB7` |
| Flags | bit3 TRUNCATED, bit4 COMPRESSED | `lep.go` |
| LEP crypto | v1 / v2 device path | `lep-crypto.md`, XChaCha+HKDF+HMAC |
| LSAK ACK | v1 | `internal/latchstream` |
| Latch stream | v1 | [latch-integration.md](latch-integration.md) |
| COBS | — | `internal/framing` |
| Architecture codes | v1.0 Latch table | `architecture-codes.md` |
| TLV registry | v1 / v2 core 1–15 | `lep-tlv-registry.md` |
| Binary batch | v1 | `binary-batch.md` |
| Attachments | v1 | `attachments.md` |
| Bundle | v1 + optional Ed25519 | `bundle-format.md` |

## Sources

| Type | Status |
|------|--------|
| serial | ready |
| tcp / tls | ready |
| udp | ready |
| http / tls | ready |
| directory | ready |
| mqtt | ready |
| adapter (subprocess) | ready (`internal/adapter`) |
| native CAN (SocketCAN + CAN FD + USB-CAN + CAN-over-TCP) | ready (`internal/source/can.go`, `can_linux.go`, `can_test.go`) |
| native BLE GATT (gateway link) | ready (`internal/source/ble.go`, `ble_test.go`) |
| native LoRa/LoRaWAN (MQTT TTN/ChirpStack + HTTP poll) | ready (`internal/source/lorawan.go`, `lorawan_test.go`) |

## Analysis

| Feature | Status |
|---------|--------|
| Cortex-M / Latch multi-arch CPU TLV | ready |
| RISC-V / Xtensa / Linux codes | aligned v1.0 |
| DWARF symbolicate | ready |
| Full CFI / EHABI unwind | not yet |

## Product v1.0.0 still needs

- Latch + Trace end-to-end CI on real MCU (see `docs/v1-release-gate.md`)
- Hardware-in-the-loop matrix (see `docs/hil-matrix.md`)
- External security review
- Power-loss soak on production filesystems
