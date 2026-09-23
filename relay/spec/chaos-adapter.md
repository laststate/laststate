# Chaos Adapter Specification

## Overview

The Chaos Adapter is a Relay component that injects controlled faults into
connected MCUs via Latch to verify crash capture correctness.

## Architecture

```
Trace UI → POST /api/chaos/inject → Trace Chaos Manager → Relay → Chaos Adapter → MCU
```

## Adapter Protocol

The adapter communicates with the Relay via length-prefixed frames on stdin/stdout.

### Frame Format

```
[4 bytes LE length][payload bytes]
```

### Request Frame (Trace → Adapter)

```json
{
  "command": "inject",
  "device_id": "DEV-001",
  "type": "hardfault",
  "params": {
    "fault_address": "0x2001FF00",
    "delay_ms": 1000
  }
}
```

### Response Frame (Adapter → Trace)

```json
{
  "status": "success",
  "device_id": "DEV-001",
  "type": "hardfault",
  "captured": true,
  "duration_ms": 1250,
  "error": null
}
```

## Injection Types

| Type | Description | Parameters |
|------|-------------|-----------|
| `hardfault` | Trigger hard fault exception | `fault_address`, `fault_value` |
| `watchdog` | Trigger watchdog timeout | `delay_ms` |
| `brownout` | Simulate voltage drop | `voltage_mv`, `duration_ms` |
| `corrupt-stack` | Corrupt stack pointer | `offset_bytes` |
| `nested-fault` | Trigger nested exception | `inner_type`, `outer_type` |
| `interrupted-flash` | Interrupt flash operation | `sector`, `interrupt_at_byte` |

## Adapter Implementations

### Serial Adapter

Communicates via UART to the MCU debug port.

**Configuration:**
```
CHAOS_ADAPTER=serial
CHAOS_SERIAL_PORT=/dev/ttyUSB0
CHAOS_SERIAL_BAUD=115200
```

**Protocol:**
1. Send hex command via serial
2. Wait for acknowledgment
3. Send injection command
4. Wait for result

### TCP Adapter

Communicates over TCP to a remote injection service.

**Configuration:**
```
CHAOS_ADAPTER=tcp
CHAOS_TCP_HOST=192.168.1.100
CHAOS_TCP_PORT=5555
```

### Adapter SDK

Uses the LastState adapter SDK for custom implementations.

**Configuration:**
```
CHAOS_ADAPTER=adapter-sdk
CHAOS_ADAPTER_PATH=./my-adapter
```

## Safety

1. **Timeout**: All injections have a configurable timeout (default 10s)
2. **Rate limiting**: Max 1 injection per 60 seconds per device
3. **Rollback**: If capture fails, device is reset to safe state
4. **Logging**: All injections are logged with full context

## Testing

Run chaos tests against a test bench:

```bash
# Inject hard fault and verify capture
curl -X POST http://localhost:8080/api/chaos/inject \
  -H "Authorization: Bearer <token>" \
  -d '{"device_id": "DEV-001", "injection": "hardfault"}'

# Check results
curl http://localhost:8080/api/chaos/results
```

## Error Handling

| Status | Description |
|--------|-------------|
| `success` | Fault injected, Latch captured it |
| `timeout` | Injection timed out, no response from device |
| `corrupt` | Fault injected but Latch failed to capture |
| `error` | Injection failed (hardware error, adapter error) |

## Security

- Only admin users can trigger injections
- All injections are logged in the audit trail
- Adapter process runs with minimal privileges
- Network adapters bind to localhost only
