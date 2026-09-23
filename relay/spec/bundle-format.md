# `.lsbundle` format v1

ZIP with safe paths and a `manifest.json`:

```text
manifest.json
events/evt_<id>.lep
artifacts/      # optional firmware blobs
signatures/     # optional detached material
```

Manifest holds version, time, relay id, event paths, SHA-256, sizes, and source
ids. Import checks paths, sizes, and hashes, then runs normal durable ingest.
Matching hashes count as duplicates.

## Signatures

Digest = SHA-256 of a stable JSON projection (version, relay_id, sorted events
and artifacts — see `bundle.CanonicalManifestJSON`). Ed25519 signatures appear
in `manifest.signatures` as `{alg, key_id, signature}` (base64).
