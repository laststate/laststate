# Release channels

| Channel | Cadence | Expectation |
|---------|---------|-------------|
| `nightly` | daily from `main` | can break; CI only |
| `beta` | freeze candidates | config stable within the minor |
| `stable` | tagged `vX.Y.Z` | no breaking config within a major |

Ship SHA-256 checksums with every artifact. Code signing and macOS notarization
are still on the ops backlog — until then, pin known digests yourself.
