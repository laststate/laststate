# Architecture codes (v1 frozen)

Canonical = Latch producer table + protocol registry.

| Code | Name |
|------|------|
| 0 | unknown |
| 1 | cortex-m |
| 2 | riscv |
| 3 | xtensa |
| 4 | linux |
| 5 | riscv64 |
| ≥6 | unallocated — allocate via protocol RFC |

CPU context (TLV 4) and fault regs (TLV 5) use the Latch multi-arch container for codes 1–5.
CPU64 (TLV 16) carries architecture code 5 (riscv64) with word size 8.
See protocol `architectures/` and `registry/tlv-types.md`.
