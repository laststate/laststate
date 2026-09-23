# Contributing to Relay

Thank you for helping make device diagnostics more reliable. Contributions do
not need to be large: a new source adapter, a clearer operator doc, a test
vector, or a focused code review can be more valuable than a new feature.

## Find a useful first contribution

- Browse [`good first issue`](https://github.com/laststate/relay/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22) for work that should not require understanding the whole pipeline.
- Browse [`help wanted`](https://github.com/laststate/relay/issues?q=is%3Aissue+is%3Aopen+label%3A%22help+wanted%22) for transport adapters, packaging, and operator tooling where outside experience is especially useful.
- Use [GitHub Discussions](https://github.com/laststate/relay/discussions) for integration questions or an early design proposal.
- Open an issue directly for a small bug or documentation gap.

Comment on an issue before starting substantial work so contributors do not
duplicate effort. For delivery semantics, persistence layout, or auth changes,
start a discussion first.

## AI-assisted contributions

AI-assisted work is welcome when it is focused, reviewable, and held to the
same evidence standard as any other contribution. Before editing, read the
tool-neutral [agent guide](AGENT.md) and the scoped `AGENTS.md` instructions in
the directories you touch. The guide is available through the native entry
points for Codex, Claude, Gemini, and GitHub Copilot.

AI assistance does not substitute for contributor or maintainer judgment. Do
not claim durability, security, or production evidence that was not actually
produced. Do not expose keys, memory captures, private collector details,
endpoints, credentials, or proprietary firmware.

## Maintainer response target

We aim to acknowledge new issues and pull requests within 72 hours and provide
an initial triage within seven days. This is a maintainer target, not an SLA.
If there is no response after seven days, one friendly ping is welcome.

## Development setup

You need Go 1.26.1+. Build and validate the complete suite:

```sh
gofmt -l cmd internal          # should be empty
go vet ./...
go test -race ./...
go build ./cmd/laststate-relay
```

Use the narrowest useful loop while developing:

| Change | Fast validation |
|---|---|
| Documentation | Factual cross-checks against current docs and code. |
| LEP parser or framing | `go test ./internal/lep -run TestProtocolVectors -count=1 -v` |
| Delivery or spool | `go test -race ./internal/delivery ./internal/spool` |
| Admin or auth | `go test -race ./internal/admin` |
| Packaging or Docker | Local build and container run |
| CI or release automation | Local `actionlint` when available |

## Invariants that changes must preserve

- **Persist before ACK** — the device must never receive `ACK_STORED` before
  the raw LEP object is durably written.
- **At-least-once delivery** — stable IDs must make retries safe; duplicates
  after a lost response are acceptable, data loss is not.
- **Admin / ingest separation** — admin endpoints must not be reachable from
  ingest traffic.
- **Bounded input** — every network source must bound length, header size, and
  connection count before allocation.
- **Constant-time auth** — bearer token comparison must be constant-time;
  redirects must be rejected.
- **No secret leakage** — raw payloads and secrets must never appear in normal
  logs.
- AI-generated or human-authored changes use the same focused review, test,
  and evidence requirements.

Do not include device secrets, production keys, captured customer memory,
private endpoints, or proprietary firmware in code, fixtures, issues, or
pull requests. Report vulnerabilities privately through the process in
[SECURITY.md](SECURITY.md).

## Pull requests

1. Create a focused branch from `main`.
2. Keep the change focused. Separate refactors from behavior changes when
   practical.
3. Add tests for protocol parsers, state transitions, persistence, or failure
   behavior changed by the patch.
4. Explain durability, compatibility, and migration effects in the pull request.
5. Add an entry to [CHANGELOG.md](CHANGELOG.md) when the change affects
   operators or adopters.
6. Fill in the pull request template, including runtime evidence.
7. Resolve review threads and keep the branch current before merge.

Draft pull requests are welcome for early technical feedback. No Contributor
License Agreement is required; contributions are accepted under the
repository's Apache-2.0 license.

Repeated contributors who review changes, help with triage, or own a subsystem
can be invited into the maintainer workflow as the community grows.

By participating, you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).
