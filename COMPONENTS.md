# Pinned components

`trace`, `relay`, `billing-service` and `simulator` are git submodules
tracking `main`. The SHAs below are the revisions this distribution was last
booted and smoke-tested against.

| Component       | Upstream                                        | Revision                                 | Describe      |
|-----------------|-------------------------------------------------|------------------------------------------|---------------|
| trace           | https://github.com/laststate/trace.git          | `953e76d379412236e2c8e773118b533fc857af5e` | v0.9.0-5      |
| relay           | https://github.com/laststate/relay.git          | `2bc57504bd78c268f25ad8a3f216e9cf93225889` | v0.5.0-8      |
| billing-service | https://github.com/laststate/billing-service.git| `00280f8f5f38ecc4f754609f06ebe0e3d8035a70` | v0.2.0-7      |
| simulator       | https://github.com/laststate/simulator.git      | `7041a205156418b8f16ce534bf8c08b2de8f026c` | v0.1.0-4      |

Verify with `git submodule status`. To move to newer upstream work, run
`scripts/update.sh` (or `scripts/update.ps1`), boot the stack, run the smoke
checks, then commit the new gitlinks together with this file.

Licenses: `trace` is AGPL-3.0, `relay` and `simulator` are Apache-2.0,
`billing-service` is proprietary.
