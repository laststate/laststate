# Protocol golden vectors (vendored)

Copied from [laststate/protocol](https://github.com/laststate/protocol) `test-vectors/`.

Repos are private, so CI cannot check out the protocol repo with the default
`GITHUB_TOKEN`. Keep this tree in sync when wire format goldens change.

Vectors follow the v2 manifest: `kind` = valid / invalid / crypto-aead /
crypto-hmac / stream / lsak. `tests/protocol_vectors_test.go` validates the
structural kinds; the reference Go codec in the protocol repo verifies the
AEAD/HMAC tags against the documented test keys in `manifest.json`.