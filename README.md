# LastState

Self-hosted LastState stack. One repository, one command, no account.

```bash
git clone --recurse-submodules https://github.com/laststate/laststate.git
cd laststate
docker compose up -d
```

> [!NOTE]
> `--recurse-submodules` matters. `trace`, `relay`, `billing-service` and
> `simulator` are submodules, not copies. A plain `git clone` leaves those
> directories empty and `docker compose up` fails on the missing build
> contexts. If you already cloned without the flag, run
> `git submodule update --init --recursive` instead of cloning again.

| Service    | URL                   | Credentials  |
|------------|-----------------------|--------------|
| UI         | http://localhost:8080 | —            |
| Relay admin| http://localhost:8383 | bearer token |
| Grafana    | http://localhost:3001 | admin/admin  |
| Prometheus | http://localhost:9090 | —            |

For development only, the stack also exposes ingest (HTTP `:8384`,
TCP `:8385`, UDP `:8386`), Relay metrics (`:9467`), simulator metrics
(`:9468`), billing (`:8081`), MinIO (`:9000`/`:9001`) and Postgres
(`localhost:5433`).

## What runs here

| Directory         | Upstream                          | Role                                                        |
|-------------------|-----------------------------------|-------------------------------------------------------------|
| `trace/`          | `laststate/trace` (AGPL-3.0)      | Backend, workers and web UI on `:8080`                      |
| `relay/`          | `laststate/relay` (Apache-2.0)    | Offline-first gateway; admin `:8383`, ingest `:8384`-`:8386`|
| `billing-service/`| `laststate/billing-service`       | Subscriptions and entitlements (proprietary)                |
| `simulator/`      | `laststate/simulator` (Apache-2.0)| Virtual LEP fleet, so `up` shows traffic end to end         |

Pinned revisions are listed in [`COMPONENTS.md`](COMPONENTS.md). Component
sources stay in their own repositories; this repo only records which revision
it was tested against.

## How it fits together

Devices (or the simulator) send LEP envelopes to Relay. Relay persists each
envelope to disk before acknowledging it, then forwards batches to Trace.
Trace fingerprints crashes into issues, serves the UI on `:8080` and exposes
metrics. Prometheus scrapes every service; Grafana ships with Prometheus
pre-configured as its default datasource.

```text
simulator ──HTTP/TCP/UDP──▶ relay ──batch──▶ trace ──UI :8080
                                │                │
                             spool           postgres + minio
                                │                │
                                └──── prometheus ──── grafana :3001
```

## Commands

```bash
docker compose up -d            # start everything
docker compose ps               # status
docker compose logs -f trace relay simulator
docker compose down             # stop, keep data
docker compose down -v          # stop and delete all data
```

Health checks after boot:

```bash
bash scripts/smoke.sh           # Linux / macOS
pwsh -File scripts/smoke.ps1    # Windows
```

> [!TIP]
> The first boot takes several minutes. Trace builds its web UI (`npm ci`
> plus the Vite build) inside Docker on first run. Wait until
> `docker compose ps` shows every service `running` before opening the UI.

## Configuration

Local defaults are compiled into `docker-compose.yml`, so the stack boots
with no `.env` file. Copy the template only when you need to change something:

```bash
cp .env.example .env
```

> [!WARNING]
> The built-in defaults (`admin123`, `dev-*-token-12345`, `*_fake_*`,
> `minio12345`) are development placeholders. They must never reach staging
> or production. Generate real secrets with `openssl rand -hex 32` (or
> `openssl rand -base64 32`) and inject them through Vault, AWS SSM, Doppler
> or 1Password Connect — never commit `.env`.

Provider keys (Stripe, Mercado Pago, Coinbase Commerce, NOWPayments) come
from their dashboards. Locally the stack runs with fake keys and skips real
charges; see [`billing-service/docs/PRICING.md`](billing-service/docs/PRICING.md)
for the plan table.

## Updating components

Submodules are pinned. To move to newer upstream revisions:

```bash
bash scripts/update.sh           # Linux / macOS
pwsh -File scripts/update.ps1    # Windows
```

This pulls `main` in each submodule, records the new SHAs in
`COMPONENTS.md`, and leaves the result for you to test (`docker compose up
--build`, then `scripts/smoke.sh`) before committing. Never commit a pin you
have not booted.

## Troubleshooting

- `trace` never becomes healthy: `docker compose logs trace postgres
  createbucket`. Postgres must be healthy and the MinIO bucket created
  before Trace starts.
- A port is already in use (`8080`, `8383`, `3001`, `9090`): stop the
  conflicting process or remap the port in `docker-compose.yml`.
- Empty `trace/`, `relay/`, … directories: submodules were not initialized.
  Run `git submodule update --init --recursive`.
- Start over: `docker compose down -v && docker compose up -d`. This
  deletes Postgres, MinIO objects, the Relay spool and Grafana data.

> [!CAUTION]
> `docker compose down -v` deletes every named volume: crash history,
> symbol artifacts, billing data and dashboards. There is no undo. Dump
> Postgres (`pg_dump`) and mirror the MinIO bucket before wiping a host
> you care about.

## Layout

```text
docker-compose.yml          the whole stack
.env.example                production template (every secret a REPLACE_ME_*)
relay-config.yaml           relay sources and the trace destination
prometheus.yml              scrape targets for all services
grafana/provisioning/       prometheus datasource
init-multi-db.sh            creates the trace and billing databases
COMPONENTS.md               pinned submodule revisions
scripts/smoke.sh|ps1        post-boot health checks
scripts/update.sh|ps1       bump submodules to newer upstream mains
```

## License

Each component keeps its own license: `trace` is AGPL-3.0, `relay` and
`simulator` are Apache-2.0, `billing-service` is proprietary. See
[`trace/LICENSE`](trace/LICENSE), [`relay/LICENSE.md`](relay/LICENSE.md),
[`simulator/LICENSE`](simulator/LICENSE) and
[`billing-service/LICENSE`](billing-service/LICENSE).

> [!IMPORTANT]
> Offering Trace over a network triggers AGPL §13: every network user is
> entitled to the Corresponding Source, including your modifications. To
> operate Trace as a closed SaaS you need a commercial license. Local
> self-hosting for your own use is unaffected.
