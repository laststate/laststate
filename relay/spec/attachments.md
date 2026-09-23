# Attachment chunks (draft)

Large blobs ride as one or more LEP events (or TLVs inside an event).

## 0x0020 — ATTACHMENT_META

```text
attachment_id 16 bytes
total_size u64 LE
sha256 32 bytes
chunk_count u32 LE
name_len u16 LE
name UTF-8
```

## 0x0021 — ATTACHMENT_CHUNK

```text
attachment_id 16 bytes
index u32 LE
total u32 LE
data …
```

Each envelope is still stored normally. Reassembly is for analysis/export and
never blocks ACK.
