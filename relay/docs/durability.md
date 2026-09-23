# Durability

An event is acknowledged only after the full write path below finishes. If the
process dies mid-way, the device may retransmit; content-addressed IDs make that
safe.

```mermaid
sequenceDiagram
  participant D as Device
  participant R as Relay
  participant FS as Filesystem
  participant DB as SQLite WAL

  D->>R: LEP frame
  R->>FS: write temp object
  R->>FS: fsync (per spool.fsync)
  R->>FS: atomic rename to sha256 path
  R->>FS: dir sync (full mode)
  R->>DB: commit event + delivery rows
  R->>D: LSAK ACK_STORED
```

## Steps

1. Write a temporary file in the target directory.
2. Sync the file according to `spool.fsync` (`full`, `balanced`, or `none`).
3. Rename it to the SHA-256 object path.
4. In `full` mode, sync the parent directory (best-effort on Windows).
5. Commit metadata to SQLite WAL.
6. Create destination delivery records.

## Delivery

Delivery is at-least-once. Trace may accept an event before the response is lost;
Relay retries with the same event ID and `Idempotency-Key`.
