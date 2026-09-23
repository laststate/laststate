# LEP crypto (v1 — Latch device path)

**Status:** Frozen with protocol LEP v1. Aligned with Latch `ls_envelope_encode` / decrypt.

Tests: `internal/lep/crypto_test.go`.

## Flags

| Flag | Bit | Meaning |
|------|-----|---------|
| Authenticated | 0 | Auth trailer present |
| Encrypted | 1 | Payload is ciphertext |
| AEAD | 2 | 28-byte AEAD metadata present |
| Truncated | 3 | Optional fields omitted (Latch) |
| Compressed | 4 | Payload is zstd (gateway-only) |

Rules: Encrypted ↔ AEAD; AEAD requires Authenticated.

## AEAD (XChaCha20-Poly1305)

Metadata after 24-byte header:

| Offset | Size | Field |
|--------|------|--------|
| 0 | 24 | XChaCha20 nonce |
| 24 | 4 | key_id u32 LE |

Wire:

```
header(24) || metadata(28) || ciphertext || payload_crc(4) || tag(16)
```

- AAD = `header(24) || metadata(28)` (includes header CRC)
- payload_crc covers `metadata || ciphertext`
- Device key derivation:

```
salt = key_id||sequence||event_id  (12 bytes LE)
derived = HKDF-SHA256(salt, ikm=device_key, info="laststate/latch/envelope/v1", len=32)
```

## Auth-only (HMAC-SHA-256)

```
header(24) || payload || payload_crc(4) || mac(32)
mac = HMAC-SHA256(key, header || payload || payload_crc)
```

Key id is out of band (config). Prefer numeric decimal ids matching Latch `key_id`.

## Compression

Bit 4 `Compressed`: entire payload is zstd. Order: TLV → optional zstd → optional Seal.
Devices (Latch) do not set bit 4; bit 3 is Truncated only.

## Replay

Optional: remember `(source_id, event_id)` → payload hash; reject different payload same id.
Identical retransmit → ACK_DUPLICATE.
