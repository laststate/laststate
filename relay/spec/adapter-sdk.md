# Adapter SDK (draft)

Custom transports (CAN, BLE, LoRa, …) plug in as external processes.

## Subprocess

```text
adapter-bin --config <path>
```

**stdout** — length-prefixed frames:

```text
u32 LE length
payload bytes   # raw LEP or framed stream
```

**stderr** — logs only.

**stdin** (optional JSON lines):

```json
{"op":"shutdown"}
```

## gRPC (optional later)

```protobuf
service Adapter {
  rpc Subscribe(SubscribeRequest) returns (stream Frame);
}
message Frame {
  string source_id = 1;
  bytes payload = 2;
}
```

## Config example

```yaml
- id: can0
  type: adapter
  adapter:
    command: /usr/local/bin/ls-can-adapter
    args: ["--iface", "can0"]
  framing:
    type: raw
```
