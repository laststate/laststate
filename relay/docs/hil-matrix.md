# Relay HIL matrix (CAN / BLE / LoRa / Probe)

Hardware-in-the-loop evidence required before the `v1.0.0` tag. Each row is
one CI job or one signed lab run; record board, firmware hash, relay commit,
and result link. A row is green only with an attached log artifact.

## CAN / CAN FD

| # | Setup | What is asserted | How |
|---|-------|------------------|-----|
| C1 | `vcan0` (Linux CI) + `CANDevice.InjectFrames` | LEP encode/decode round-trip, filter ID/mask | `go test ./internal/source/ -run TestCAN` |
| C2 | SocketCAN `can0` @500k + USB-CAN adapter | 10k frames, zero loss, FD BRS flag preserved | lab run, `RunCAN` with `AdapterPath` |
| C3 | CAN-over-TCP bridge | reconnect after bridge kill, no duplicate LEP ids | fault-injection soak (kill -9 bridge, restart) |

## BLE GATT

| # | Setup | What is asserted | How |
|---|-------|------------------|-----|
| B1 | gateway mock (JSON-over-TCP) | scan → connect → notify → LEP payload | `go test ./internal/source/ -run TestBLE` |
| B2 | ESP32 peripheral + host gateway | 1h notify stream, RSSI filter, allow-list | lab run, `BLEScanner` + `BLEDeviceSimulator` |
| B3 | gateway disconnect mid-notify | reconnect, no half-frame forwarded | fault-injection soak |

## LoRa / LoRaWAN

| # | Setup | What is asserted | How |
|---|-------|------------------|-----|
| L1 | `LoRaWANServerSimulator` | JSON + legacy binary uplink decode, EUI filter | `go test ./internal/source/ -run TestLoRaWAN` |
| L2 | ChirpStack + MQTT (TTN v3 topics) | uplink → LEP payload, `finished/confirmed` mapping | staging run against ChirpStack sandbox |
| L3 | HTTP poll fallback | poll cadence, 4MB cap, retry on 5xx | `runHTTP` soak with flaky mock server |

## Probe HIL

| # | Setup | What is asserted | How |
|---|-------|------------------|-----|
| P1 | subprocess adapter (`internal/adapter`) | probe waveform TLVs pass through unchanged | `go test ./internal/adapter/` |
| P2 | real probe on pilot gateway | capture → spool → replay → Trace ingest 200 | pilot run, `.lsbundle` export/import verified |

## Recording a run

Attach to the release PR:

```text
board: <e.g. STM32H743 + MCP2518FD / ESP32-C3 / RAK7268>
firmware_sha256: <hex>
relay_commit: <git rev-parse HEAD>
scenario: <C2|B2|L2|P2>
result: PASS|FAIL + log artifact link
```

Minimum bar for `v1.0.0`: C1+C2, B1+B2, L1+L2, P1 green; C3/B3/L3 green or
filed as tracked issues with retry/backoff evidence.
