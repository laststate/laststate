# Trace Roadmap

Vision and planned features for the Trace crash analytics platform.

## Current state

Trace is a self-hosted crash analytics backend that ingests LEP-encoded device
crashes via Relay, runs analysis pipelines, and exposes results through a web
UI and REST API. Core functionality is stable and production-ready.

**v0.8.0 milestone:** Several feature paths are DB-backed (Memorial Wall,
Postmortem, Anomaly Events, Chaos Results, Fleet Health, Public Crash API and
Device DNA). Mock preview mode and integration boundaries remain; this is not
a claim that every product surface is production-ready.

## Near-term (next 3 months)

- [ ] **Multi-region support** — Deploy Trace instances across regions with
  automatic crash routing based on device location. (control-plane model only;
  replication backend required)
- [x] **Enhanced anomaly detection** — ML-based anomaly detection for crash
  pattern recognition and predictive alerting. (DB-backed, ML pending)
- [ ] **Custom dashboards** — User-configurable dashboards with drag-and-drop
  widgets and sharing.
- [x] **Improved symbolication** — Support for more architectures (ARM64,
  RISC-V, WebAssembly) and faster symbol resolution. (DB-backed)

## Medium-term (3-6 months)

- [ ] **Real-time streaming** — WebSocket-based real-time crash feed for
  live monitoring during testing.
- [x] **Advanced on-call** — Escalation policies, shift swapping, and
  integration with more scheduling tools. (DB-backed)
- [ ] **Cost optimization** — Automatic data tiering, compression, and
  retention policy enforcement.
- [ ] **API v2** — Modernized API with GraphQL support and improved
  pagination.

## Long-term (6-12 months)

- [x] **AI-powered postmortems** — Template-based generation complete; LLM integration pending API key.
- [ ] **Fleet digital twin** — Simulate fleet behavior and predict failures
  before they occur.
- [x] **Cross-project analytics** — Correlate crash data across multiple
  projects and services. (DB-backed)
- [ ] **Compliance reporting** — Automated compliance reports for SOC 2,
  ISO 27001, and other frameworks.

## Completed features

- [x] Core crash ingestion and analysis
- [x] Symbolication (LLVM, addr2line)
- [x] Device DNA fingerprinting
- [x] Anomaly detection (DB-backed)
- [x] Fleet health scoring (DB-backed)
- [x] Web UI with React SPA
- [x] REST API with OpenAPI spec
- [x] OIDC SSO (PKCE, JWKS validation, membership-gated)
- [x] SAML SSO (goxmldsig signature verification + Conditions; IdP certificate required)
- [ ] Multi-provider billing (Stripe is implemented; Mercado Pago/Crypto require
  provider-specific production flows)
- [x] Helm charts for Kubernetes
- [x] Self-hosted deployment with Docker Compose
- [x] SDKs (JavaScript, Python)
- [x] Chaos engineering integration (DB-backed)
- [x] Soundboard for incident alerts
- [x] Memorial wall for device tributes (DB-backed)
- [x] All feature APIs now use real database queries

## Contributing

This roadmap is a living document. Suggestions and feedback are welcome via
[GitHub Issues](https://github.com/laststate/trace/issues) and
[GitHub Discussions](https://github.com/laststate/trace/discussions).
