# LastState — one-command platform

Self-hosted LastState stack. No setup, no `.env` required for local dev.

```bash
git clone https://github.com/laststate/laststate.git
cd laststate
docker compose up -d
```

| Service    | URL                       | Creds        |
|------------|---------------------------|--------------|
| UI         | http://localhost:8080     | —            |
| relay adm  | http://localhost:8383     | bearer token |
| grafana    | http://localhost:3001     | admin/admin  |
| prometheus | http://localhost:9090     | —            |

Extra (dev): ingest HTTP `:8384` · TCP `:8385` · UDP `:8386` · relay metrics
`:9467` · simulator metrics `:9468` · billing `:8081` · minio `:9000/:9001` ·
postgres `localhost:5433`.

## What is running

- **trace/** — backend + UI (API/workers, grouping, symbolication). Port 8080.
- **relay/** — offline-first gateway (persist-before-ACK, spool, LSAK).
  Admin 8383, ingest 8384/8385/8386, metrics 9467.
- **billing-service/** — subscriptions/entitlements (fake provider keys local).
- **simulator/** — virtual LEP fleet so `up` shows traffic end to end.
- **prometheus / grafana** — metrics + dashboards (datasource pre-provisioned).

Sources under `trace/ relay/ billing-service/ simulator/` are vendored
snapshots (see `COMPONENTS.md`). No `--recurse-submodules`, no extra clone.

## Commands

```bash
docker compose up -d          # start everything
docker compose ps             # status
docker compose logs -f trace relay simulator
pwsh -File scripts/smoke.ps1  # or: bash scripts/smoke.sh
docker compose down           # stop (keeps data)
docker compose down -v        # stop + wipe data
```

## Production

Local defaults are **dev-only** (`admin123`, `*_fake_*`, `minio12345`).
For staging/prod:

```bash
cp .env.example .env
# fill every REPLACE_ME_*, generate with: openssl rand -hex 32
docker compose up -d
```

Inject via Vault/SSM/Doppler in real deploys. Never commit `.env`.

## Layout

```
docker-compose.yml            # the whole stack (ports 8080/8383/3001/9090)
.env.example                  # prod template (REPLACE_ME_*)
relay-config.yaml             # relay sources → trace
prometheus.yml                # trace/relay/billing/simulator targets
grafana/provisioning/…        # prometheus datasource
init-multi-db.sh              # creates trace,billing DBs
trace/ relay/ billing-service/ simulator/   # vendored component source
scripts/smoke.sh smoke.ps1    # health checks
scripts/update.sh update.ps1  # re-vendor upstream at new pins
```

## Troubleshooting

- `trace` unhealthy → `docker compose logs trace postgres createbucket`
- port clash (`8080`/`8383`/`3001`/`9090` busy) → stop the other app or
  override ports in `docker-compose.yml`.
- first boot is slow (trace web build + `npm ci`) — `docker compose ps`
  until all `healthy`/`running`.
- wipe and retry: `docker compose down -v && docker compose up -d`.

Licensing: edge (`relay`, `simulator`) Apache-2.0, `trace` AGPL-3.0 —
see `trace/LICENSE` and `COMPONENTS.md`.
