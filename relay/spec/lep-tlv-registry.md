# LEP TLV types (v1 frozen)

Aligned with Latch encoder and protocol `registry/tlv-types.md`.

| Type | Name | Notes |
|------|------|--------|
| 0 | reserved | rejected |
| 1 | IDENTITY | nested string fields |
| 2 | RESET | boot/reset block |
| 3 | EVENT | priority, severity, hashes |
| 4 | CPU | multi-arch context |
| 5 | FAULT | fault registers |
| 6 | BREADCRUMB | repeatable (**not** RTOS tasks) |
| 7 | METRIC | repeatable (**not** RTOS current) |
| 8 | POWER | power samples |
| 9 | HEALTH | watchdog / task |
| 10 | ASSERT | assertion |
| 11 | PERIPHERAL | bus faults |
| 12 | LOG | structured log |
| 13 | MEMORY | dump regions |
| 14 | STACK | stack snapshot |
| 15 | HEAP | heap stats |
| 0x0010 | BUILD_ID_BIN | extended |
| 0x0011 | PROJECT_ID | extended |
| 0x0012 | RELEASE_ID | extended |
| 0x0013 | FIRMWARE_HASH | SHA-256 |
| 0x0020 | ATTACHMENT_META | id, size, hash |
| 0x0021 | ATTACHMENT_CHUNK | index / total / data |
| 0x0030 | PROBE_WAVEFORM | reserved |

Unknown TLVs are kept and ignored by analysis.

**Note:** Older Relay drafts that mapped 6/7 to RTOS are obsolete. RTOS task tables require new TLV IDs via protocol RFC.
