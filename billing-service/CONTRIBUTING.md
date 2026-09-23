# Contributing to Billing Service

Thank you for helping make cloud billing more reliable and fair. Contributions
do not need to be large: a clearer doc, a test case, a pricing example, or a
focused code review can be more valuable than a new feature.

## Find a useful first contribution

- Browse [`good first issue`](https://github.com/laststate/billing-service/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22) for work that does not require deep domain knowledge.
- Browse [`help wanted`](https://github.com/laststate/billing-service/issues?q=is%3Aissue+is%3Aopen+label%3A%22help+wanted%22) for provider integrations, docs, and tooling.
- Use [GitHub Discussions](https://github.com/laststate/billing-service/discussions) for pricing questions or an early design proposal.
- Open an issue directly for a small bug or documentation gap.

Comment on an issue before starting substantial work so contributors do not
duplicate effort. For pricing formulas, dunning logic, or multi-provider
synchronization, start a discussion first.

## AI-assisted contributions

AI-assisted work is welcome when it is focused, reviewable, and held to the
same evidence standard as any other contribution. Before editing, read the
tool-neutral [agent guide](AGENT.md) and the scoped `AGENTS.md` instructions in
the directories you touch. The guide is available through the native entry
points for Codex, Claude, Gemini, and GitHub Copilot.

AI assistance does not substitute for contributor or maintainer judgment. Do
not claim financial compliance or production evidence that was not actually
produced. Do not expose API keys, customer PII, payment secrets, or proprietary
pricing data.

## Maintainer response target

We aim to acknowledge new issues and pull requests within 72 hours and provide
an initial triage within seven days. This is a maintainer target, not an SLA.
If there is no response after seven days, one friendly ping is welcome.

## Development setup

You need Go 1.25+. Build and validate the service:

```sh
gofmt -l cmd internal          # should be empty
go vet ./...
go test -race ./...
go build ./cmd/billing-service
```

Use the narrowest useful loop while developing:

| Change | Fast validation |
|---|---|
| Documentation | Factual cross-checks against current docs and code. |
| Webhook handler | `go test ./internal/webhook -v` |
| Billing logic | `go test ./internal/billing -v -race` |
| API handlers | `go test ./internal/api -v -race` |
| Schema / migrations | Manual review against [docs/DATABASE.md](docs/DATABASE.md) |
| CI or release automation | Local `actionlint` when available |

## Invariants that changes must preserve

- **Idempotent webhooks** — events must be safely replayed; duplicates are
  acceptable, data loss or double-charging is not.
- **Provider neutrality** — the billing core must not depend on a single
  provider; all provider logic is isolated.
- **Token scoping** — admin tokens cannot perform billing operations and vice
  versa.
- **Bounded input** — every webhook payload must be bounded in size.
- **Constant-time auth** — signature comparisons must be constant-time.
- **No secret leakage** — API keys, webhook secrets, and payment data must
  never appear in logs.
- **Proration correctness** — plan changes must calculate proration consistently
  across providers.
- **Dunning consistency** — failed payments follow the documented retry schedule
  (Day 0, 1, 3, 7, 14).

Do not include production API keys, customer PII, payment secrets, or
proprietary pricing data in code, fixtures, issues, or pull requests. Report
vulnerabilities privately through the process in [SECURITY.md](SECURITY.md).

## Pull requests

1. Create a focused branch from `main`.
2. Keep the change focused. Separate refactors from behavior changes when
   practical.
3. Add tests for billing calculations, webhook parsing, or state transitions
   changed by the patch.
4. Explain financial, compatibility, and migration effects in the pull request.
5. Add an entry to [CHANGELOG.md](CHANGELOG.md) when the change affects
   operators or adopters.
6. Fill in the pull request template, including runtime evidence.
7. Resolve review threads and keep the branch current before merge.

Draft pull requests are welcome for early technical feedback. No Contributor
License Agreement is required; contributions are accepted under the
repository's license.

Repeated contributors who review changes, help with triage, or own a subsystem
can be invited into the maintainer workflow as the community grows.

By participating, you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).
