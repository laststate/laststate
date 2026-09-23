# Pinned components

`trace`, `relay`, `billing-service` and `simulator` are git submodules
tracking `main`. The SHAs below are the revisions this distribution was last
booted and smoke-tested against.

| Component       | Upstream                                        | Revision                                 | Describe      |
|-----------------|-------------------------------------------------|------------------------------------------|---------------|
| trace           | https://github.com/laststate/trace.git          | `60fea8eae0fc0f613e1464ed619dcd3843b3dd39` | v0.9.0-3      |
| relay           | https://github.com/laststate/relay.git          | `dbce8d0f50e6c6a98825b38dc86f9e6bf28f286e` | v0.5.0-6      |
| billing-service | https://github.com/laststate/billing-service.git| `9b03866d345b54f27602fb31ef91fb08bf847014` | v0.2.0-5      |
| simulator       | https://github.com/laststate/simulator.git      | `c05d3260281d6858e064993708126aba8fad1dcd` | v0.1.0-2      |

Verify with `git submodule status`. To move to newer upstream work, run
`scripts/update.sh` (or `scripts/update.ps1`), boot the stack, run the smoke
checks, then commit the new gitlinks together with this file.

Licenses: `trace` is AGPL-3.0, `relay` and `simulator` are Apache-2.0,
`billing-service` is proprietary.
