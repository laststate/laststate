# Binary batch (draft)

Content-Type: `application/vnd.laststate.batch.v1`

## Layout

```text
magic "LSBT" (4)
version u8 = 1
flags u8          # bit0 = body after header is zstd
reserved u16 = 0
count u32 LE
for each event:
  id_len u16 LE
  id UTF-8
  payload_len u32 LE
  payload (raw LEP)
```

When bit0 is set, everything after the 12-byte header is one zstd frame that
decompresses to the `count` + events region.

JSON batch remains the default. Peers advertise `binary_batch: true` and
`compression: ["identity","zstd"]`.
