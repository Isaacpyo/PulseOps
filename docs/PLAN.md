# PulseOps MVP — build plan

## Scope

Register HTTP/TCP checks → Go workers probe them on an interval → results feed an
alert-rule engine (consecutive failures, latency threshold) → incidents open,
escalate and auto-resolve with a timeline → Slack notifications (real webhook or
mock provider) → public status page with 90-day uptime bars. Everything is
instrumented with Prometheus metrics, scraped locally and pushed to Grafana Cloud
in production via remote_write.

## Architecture

```
apps/api   Go 1.24, chi, pgx, prometheus/client_golang — one binary:
           HTTP API + scheduler + probe worker pool + notification dispatcher
           + optional remote_write pusher
apps/web   React 19 + Vite + TypeScript + Tailwind — console + public status page
deploy/    Prometheus config, Grafana provisioning + committed dashboard JSON
```

Why a single binary: Render's free tier gives one web service that sleeps when idle;
running the scheduler in-process means probes run whenever the service is awake
without paying for a background worker. Work claiming uses
`FOR UPDATE SKIP LOCKED`, so running more than one instance stays correct.

### Data model (Postgres)

| table | purpose |
|---|---|
| `workspaces` | tenant; owns checks, incidents, Slack settings, public status slug |
| `users`, `sessions` | bcrypt passwords; opaque bearer tokens stored as SHA-256 hashes |
| `checks` | target config + rule config + live state (consecutive counters, next_run_at) |
| `check_results` | raw probe results (pruned after `RESULT_RETENTION_DAYS`) |
| `check_daily` | per-check per-UTC-day rollup → 90-day uptime bars without scanning raw rows |
| `incidents`, `incident_events` | incident lifecycle + timeline (partial unique index: one active incident per check) |
| `notifications` | outbox: rows written in the same transaction as the incident change, delivered by a dispatcher with retry/backoff |

### Core logic (pure, unit-tested)

- `alert.Evaluate(rule, state, result) → (state, decision)` — classifies a probe as
  healthy / degraded (slow) / down, tracks consecutive counters and decides
  open / escalate (degraded → down) / resolve.
- `uptime.Bars(rollups, today, 90)` — fixed 90-slot series with "no data" gaps.
- `probe` — HTTP and TCP probers behind an SSRF guard that checks the *dialled* IP
  (blocks loopback/private/link-local/metadata unless `ALLOW_PRIVATE_TARGETS=true`).
- `metrics/remotewrite` — Prometheus remote_write 1.0 encoder (hand-rolled protobuf +
  snappy) so the API can push to Grafana Cloud with no sidecar.

### API surface

Auth: `POST /api/auth/{register,login,demo,logout}`, `GET /api/me`.
Checks: CRUD at `/api/checks`, `POST /api/checks/{id}/run`, `POST /api/checks/{id}/drill`.
Incidents: `GET /api/incidents`, `GET /api/incidents/{id}`,
`POST /api/incidents/{id}/{acknowledge,resolve,notes}`.
Settings/notifications: `GET|PUT /api/settings`, `POST /api/settings/test-notification`,
`GET /api/notifications`. Public: `GET /api/public/status/{slug}`.
Ops: `/healthz`, `/readyz`, `/metrics`.

### Frontend

Routes: `/` (pitch + sign in + "Try the demo"), `/app` overview, `/app/checks/:id`,
`/app/incidents`, `/app/incidents/:id`, `/app/settings`, `/status/:slug`.
A fetch wrapper tracks slow in-flight requests and shows a "waking server…" banner
(Render cold start) and retries idempotent requests while the instance boots.

## Hosting

Netlify (existing site, base `apps/web`) → Render free web service (Docker, `render.yaml`)
→ Neon Postgres. Grafana Cloud free tier receives metrics via remote_write.
Redis/Mongo are not needed by this project.

## Delivery steps

1. API skeleton: config, migrations, store, auth.
2. Probes + alert engine + scheduler + notifier outbox.
3. Metrics + remote_write; seed + demo login.
4. Web console + status page.
5. Prometheus/Grafana provisioning, docker-compose, render.yaml, CI.
6. Tests (Go unit + Postgres integration, Vitest, Playwright smoke), README.
