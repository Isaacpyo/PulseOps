# PulseOps

[![CI](https://github.com/temitopeagbola/PulseOps/actions/workflows/ci.yml/badge.svg)](https://github.com/temitopeagbola/PulseOps/actions/workflows/ci.yml)

**Uptime monitoring and incident management for teams that need to know when an HTTP or TCP endpoint fails, and to coordinate the response.**

## Overview

PulseOps answers two questions: *is my service up?* and *what are we doing about it?* A Go service probes registered HTTP and TCP targets on their own intervals and runs every result through an alert-rule engine. After N consecutive failures, or N probes slower than a latency threshold, it opens an incident. It escalates degraded incidents to outages and auto-resolves them once the target recovers. Operators acknowledge, annotate and resolve incidents from a React console. Every state change is delivered to Slack (or a mock provider when no webhook is configured) through a transactional outbox, and a public status page shows 90 days of uptime per component. The service also exports Prometheus metrics about itself, which a bundled Grafana dashboard visualises.

![Overview](docs/screenshots/overview.png)

| Check detail | Public status page |
|---|---|
| ![Check detail](docs/screenshots/check.png) | ![Status page](docs/screenshots/status.png) |

## Key features

- **HTTP and TCP checks**: per-check interval (30–3600s), timeout (500–30000 ms), optional expected HTTP status, run by a bounded Go worker pool.
- **Alert rules**:
  - an incident opens after *N* consecutive failures, or *N* consecutive probes over a latency threshold;
  - a degraded incident escalates to an outage if the target starts failing;
  - incidents auto-resolve after *M* healthy probes;
  - each check has at most one active incident, enforced by a partial unique index.
- **Incident timeline**: acknowledge, resolve (with an optional note) and free-form notes. Every notification delivery is also recorded on the timeline.
- **Slack notifications** (Block Kit), sent from an outbox table with exponential backoff (15s up to 10m, 5 attempts). A **mock provider** is used when `NOTIFIER=mock` or no webhook is configured.
- **Public status page** per workspace (`/status/:slug`):
  - 90-day uptime bars;
  - component states driven by active incidents;
  - incidents resolved in the last 14 days;
  - check targets are never exposed.
- **Chaos drill** ("Simulate outage"): makes a check report failures for 30s–15m, probing every 10s, so you can watch the full alert pipeline end to end.
- **Self-monitoring**:
  - `/metrics` exposes probe latency histograms, outcomes, scheduler lag, incident and notification counters, and API request metrics;
  - a committed Grafana dashboard and Prometheus alert rules;
  - a built-in remote_write pusher for Grafana Cloud.
- **Accounts and workspaces**: email/password registration, opaque bearer sessions, one workspace per user. Every query is scoped to the workspace.
- **Demo mode**: a seeded demo workspace with real public targets and 90 days of history, reached through a one-click "Try the demo" login.
- **Web console**:
  - check list with sparklines;
  - check detail with a 24h latency chart (keyboard-navigable), 90-day bars and a probe table;
  - incidents, settings (Slack webhook, test notification, delivery log);
  - a "waking server" banner and retries for cold starts;
  - light and dark themes, responsive down to 375px.

## Tech stack

| Area | Technology |
|---|---|
| API | Go 1.26, [chi](https://github.com/go-chi/chi), [pgx](https://github.com/jackc/pgx), `log/slog` |
| Database | PostgreSQL (17 in Docker and CI), embedded SQL migrations |
| Auth | bcrypt password hashes; random 256-bit bearer tokens stored as SHA-256 |
| Web | React 19, TypeScript (strict), Vite 7, Tailwind CSS 4, TanStack Query 5, React Router 7 |
| Notifications | Slack incoming webhooks, plus a mock provider |
| Observability | Prometheus client (`/metrics`), Prometheus remote_write → Grafana Cloud; local Prometheus 3 + Grafana 12 |
| Testing | Go `testing` (unit + Postgres integration), Vitest + Testing Library, Playwright |
| Hosting | Web on Netlify (`netlify.toml`), API on Render (`render.yaml`, Docker), Postgres on Neon |
| CI | GitHub Actions (`.github/workflows/ci.yml`) |

## Architecture

```mermaid
flowchart LR
  UI["React console + status page<br/>(apps/web, Netlify)"]

  subgraph API["Go API — single binary (apps/api, Render)"]
    HTTP["chi HTTP API"]
    SCHED["Scheduler<br/>FOR UPDATE SKIP LOCKED"]
    POOL["Probe worker pool<br/>+ SSRF guard"]
    RULES["Alert engine"]
    DISP["Notification dispatcher"]
    METRICS["/metrics + remote_write"]
  end

  PG[("PostgreSQL")]
  TARGETS(["Monitored endpoints"])
  SLACK(["Slack webhook / mock"])
  PROM(["Prometheus / Grafana Cloud"])

  UI -- "JSON + bearer token" --> HTTP
  HTTP <--> PG
  SCHED -- "lease due checks" --> PG
  SCHED --> POOL
  POOL -- "probe" --> TARGETS
  POOL --> RULES
  RULES -- "result, rollup, incident,<br/>timeline, outbox (one tx)" --> PG
  DISP -- "claim pending" --> PG
  DISP --> SLACK
  PROM -. "scrape /metrics (local)" .-> METRICS
  METRICS -- "remote_write (production)" --> PROM
```

- **One process** runs the HTTP API, the probe scheduler, the notification dispatcher, housekeeping (pruning) and the optional remote_write pusher (`apps/api/cmd/pulseops/main.go`). The scheduler claims work with `FOR UPDATE SKIP LOCKED`, so several instances can run safely.
- **A probe's path**:
  1. The scheduler leases due checks.
  2. A worker probes the target through a dialer that rejects loopback, private, link-local and other reserved IPs after DNS resolution.
  3. `store.RecordProbe` saves the result in one transaction: result row, daily rollup (which backs the 90-day bars), `alert.Evaluate`, streak counters, incident change, timeline event and outbox row.
  4. The dispatcher delivers outbox rows and logs each delivery on the incident timeline.
- **Data model** (`apps/api/internal/db/migrations/0001_init.sql`): `workspaces`, `users`, `sessions`, `checks`, `check_results`, `check_daily`, `incidents`, `incident_events`, `notifications`.

```mermaid
stateDiagram-v2
  [*] --> Healthy
  Healthy --> Down: N consecutive failures
  Healthy --> Degraded: N consecutive probes over latency threshold
  Degraded --> Down: N consecutive failures (escalate)
  Degraded --> Healthy: M consecutive fast probes (auto-resolve)
  Down --> Healthy: M consecutive reachable probes (auto-resolve)
  Down --> Healthy: operator resolves (streaks reset)
  Degraded --> Healthy: operator resolves (streaks reset)
```

## Project structure

```
apps/api/            Go API (module github.com/temitopeagbola/pulseops/apps/api)
  cmd/pulseops/        entrypoint (also `pulseops healthcheck` for the container)
  internal/alert/      alert rule engine (pure functions)
  internal/probe/      HTTP/TCP probes and SSRF guard
  internal/store/      PostgreSQL queries (tenant-scoped)
  internal/worker/     scheduler, notification dispatcher, maintenance
  internal/httpapi/    routes, middleware, validation, handlers
  internal/notify/     Slack and mock providers
  internal/metrics/    Prometheus collectors and remote_write pusher
  internal/seed/       demo workspace seeding
  internal/db/         connection pool and embedded migrations
apps/web/            React console and public status page (Vite)
  src/                 pages, components, API client, hooks
  e2e/                 Playwright smoke test
deploy/prometheus/   local Prometheus scrape config and alert rules
deploy/grafana/      Grafana provisioning, dashboard JSON and its generator
docs/                PLAN.md (architecture plan) and README screenshots
.github/workflows/   CI
docker-compose.yml   full local stack
render.yaml          Render blueprint for the API
netlify.toml         Netlify build for the web app
```

## Getting started

### Prerequisites

- **Docker** with Compose v2 (`docker compose`), for the one-command setup.
- For running without Docker:
  - **Go 1.26+** (`apps/api/go.mod`);
  - **Node.js 22** (CI and Netlify use 22; `apps/web/package.json` engines: `>=20`) with npm;
  - **PostgreSQL** (Docker and CI use 17).

### Option A: everything in Docker

```bash
cp .env.example .env    # optional; compose has defaults for every value
docker compose up --build
```

| Service | URL |
|---|---|
| Web console | http://localhost:3000 (click **Try the demo**) |
| API | http://localhost:8080 (`/healthz`, `/readyz`, `/metrics`) |
| Prometheus | http://localhost:9090 |
| Grafana | http://localhost:3001 (dashboard *PulseOps — platform overview*; anonymous view, admin login `admin` / `admin` unless `GRAFANA_ADMIN_PASSWORD` is set) |

Compose sets `ALLOW_PRIVATE_TARGETS=true` for the API so you can monitor other containers, e.g. `http://api:8080/healthz`.

### Option B: run the API and web app directly

```bash
# 1. PostgreSQL (skip if you already have one)
docker run -d --name pulseops-pg -p 5432:5432 \
  -e POSTGRES_USER=pulseops -e POSTGRES_PASSWORD=pulseops -e POSTGRES_DB=pulseops \
  postgres:17-alpine

# 2. API (migrations run and the demo workspace is seeded on startup)
cd apps/api
export DATABASE_URL='postgres://pulseops:pulseops@localhost:5432/pulseops?sslmode=disable'
go run ./cmd/pulseops            # http://localhost:8080

# 3. Web console (in another terminal)
cd apps/web
npm ci
VITE_API_URL=http://localhost:8080 npm run dev    # http://localhost:5173
```

### Environment variables

Every variable is listed with a comment in [`.env.example`](.env.example). Never commit real values.

**API** (`apps/api`, read in `internal/config/config.go`):

| Name | Purpose |
|---|---|
| `DATABASE_URL` | PostgreSQL connection string (required) |
| `PORT` | HTTP port (default `8080`; Render injects it) |
| `CORS_ORIGINS` | Comma-separated browser origins allowed to call the API |
| `PUBLIC_APP_URL` | Web console base URL, used for links in notifications |
| `TRUST_PROXY_HEADERS` | Use `X-Forwarded-For` for the client IP (only behind a trusted proxy) |
| `LOG_LEVEL` | `debug` / `info` / `warn` / `error` |
| `SESSION_TTL` | Session lifetime for regular accounts (demo sessions: 24h) |
| `SCHEDULER_ENABLED` | Run the probe scheduler in this process |
| `WORKER_CONCURRENCY` | Concurrent probes per instance |
| `ALLOW_PRIVATE_TARGETS` | Allow probing private, loopback and link-local addresses (turns the SSRF guard off) |
| `RESULT_RETENTION_DAYS` | Days of raw probe results to keep |
| `MAX_CHECKS_PER_WORKSPACE` | Maximum checks per workspace |
| `NOTIFIER` | `mock` or `slack` |
| `SLACK_WEBHOOK_URL` | Default Slack incoming webhook (secret) |
| `DEMO_ENABLED` | Seed the demo workspace and enable `POST /api/auth/demo` |
| `DEMO_RESET_ON_BOOT` | Wipe and re-seed the demo workspace on every boot |
| `METRICS_TOKEN` | Bearer token required for `GET /metrics` when set (secret) |
| `REMOTE_WRITE_URL` | Prometheus remote_write endpoint (e.g. Grafana Cloud); empty disables it |
| `REMOTE_WRITE_USERNAME` / `REMOTE_WRITE_PASSWORD` | Basic-auth credentials for remote_write (secret) |
| `REMOTE_WRITE_INTERVAL` | Push interval |

**Web** (`apps/web`, inlined at build time): `VITE_API_URL`, the API base URL.

**Docker Compose only:** `POSTGRES_PASSWORD`, `GRAFANA_ADMIN_PASSWORD`.

**Tests only:** `TEST_DATABASE_URL` (Go integration tests), `E2E_DATABASE_URL` and `E2E_API_BIN` (Playwright).

### Database, migrations and seed data

- Migrations are embedded in the binary (`apps/api/internal/db/migrations`) and applied automatically on startup. There is no separate migrate command.
- With `DEMO_ENABLED=true` (the default), startup seeds the demo workspace if it doesn't exist. Set `DEMO_RESET_ON_BOOT=true` to re-seed it on every start.

## Scripts

| Where | Command | What it does |
|---|---|---|
| `apps/web` | `npm run dev` | Vite dev server (http://localhost:5173) |
| `apps/web` | `npm run build` | Typecheck and production build to `dist/` |
| `apps/web` | `npm run preview` | Serve the production build on port 4173 |
| `apps/web` | `npm run lint` | ESLint |
| `apps/web` | `npm run typecheck` | TypeScript (`tsc -b --noEmit`) |
| `apps/web` | `npm test` | Vitest unit tests |
| `apps/web` | `npm run e2e` | Playwright smoke test (see [Testing](#testing)) |
| `apps/api` | `go run ./cmd/pulseops` | Run the API (needs `DATABASE_URL`) |
| `apps/api` | `go build ./...` / `go vet ./...` | Build / vet |
| `apps/api` | `go test ./...` | Go tests (set `TEST_DATABASE_URL` for integration tests) |
| `apps/api` | `golangci-lint run` | Lint (CI uses v2.14.0) |
| repo root | `python3 deploy/grafana/generate_dashboard.py` | Regenerate the committed Grafana dashboard JSON |

## Testing

```bash
# Go: unit tests everywhere; Postgres integration tests run when TEST_DATABASE_URL is set
docker exec pulseops-pg createdb -U pulseops pulseops_test   # or `createdb pulseops_test` on your own Postgres
cd apps/api
TEST_DATABASE_URL='postgres://pulseops:pulseops@localhost:5432/pulseops_test?sslmode=disable' go test -race ./...

# Go lint (install once)
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
golangci-lint run

# Web: lint, typecheck, unit tests
cd apps/web
npm run lint && npm run typecheck && npm test

# E2E: starts the real API (go run) and a production web build, then drives Chromium
docker exec pulseops-pg createdb -U pulseops pulseops_e2e    # or `createdb pulseops_e2e`
npx playwright install chromium                   # once
E2E_DATABASE_URL='postgres://pulseops:pulseops@localhost:5432/pulseops_e2e?sslmode=disable' npm run e2e
```

Each Go integration test runs in its own temporary schema. The Playwright test boots the API with the scheduler off and the demo reset on, so seeded state is deterministic. It covers demo login, check creation, acknowledge → note → resolve on an incident, and the public status page, at desktop width and 375px.

CI (`.github/workflows/ci.yml`) runs on every push and pull request:
- **API:** gofmt, `go vet`, golangci-lint, build, and `go test -race` against a Postgres service container.
- **Web:** ESLint, typecheck, Vitest and build.
- **E2E:** the Playwright smoke test.
- **Docker:** `docker compose build`.

## Deployment

The repo contains config for this topology:

- **Web → Netlify.** `netlify.toml` builds `apps/web` with `npm run build`, publishes `dist`, uses Node 22, rewrites all routes to `index.html` and sets security headers. Set `VITE_API_URL` in the Netlify site's environment variables. The Content-Security-Policy `connect-src` allows `https://*.onrender.com`; update it if the API lives elsewhere.
- **API → Render.** `render.yaml` is a Blueprint for `pulseops-api`:
  - Docker runtime (`apps/api/Dockerfile`), free plan, Frankfurt region;
  - deploys from `main`, health check `/healthz`;
  - non-secret env vars are preset, `METRICS_TOKEN` is generated;
  - `DATABASE_URL`, `SLACK_WEBHOOK_URL` and the `REMOTE_WRITE_*` values are prompted for (`sync: false`).
- **Database → Neon** (or any PostgreSQL). Use the pooled connection string with `sslmode=require`.
- **Metrics → Grafana Cloud** (optional). Set the `REMOTE_WRITE_*` variables, then import `deploy/grafana/dashboards/pulseops.json` and select your Prometheus data source.
- **Production URL:** `render.yaml` sets `CORS_ORIGINS` and `PUBLIC_APP_URL` to `https://pulseops.temitopeagbola.com`.

Steps:
1. Create the Neon database.
2. In Render, choose *New → Blueprint* for this repository and fill in the prompted values.
3. Set `VITE_API_URL` on Netlify to the Render service URL and redeploy.
4. Optionally set `NOTIFIER=slack` with `SLACK_WEBHOOK_URL`, and the Grafana Cloud `REMOTE_WRITE_*` values.

Render's free plan spins the service down when idle, and probes only run while it is awake. The web client shows a "Waking the server…" banner and retries during the cold start.

## API reference

All JSON. Authenticated routes take `Authorization: Bearer <token>`. Errors use `{"error": {"code", "message", "fields"}}`; validation failures return `422` with per-field messages.

| Method & path | Purpose |
|---|---|
| `POST /api/auth/register` · `/login` · `/demo` · `/logout` | Sessions (auth routes are rate-limited per IP) |
| `GET /api/me` | Current user and workspace |
| `GET/POST /api/checks`, `GET/PUT/DELETE /api/checks/{id}` | Check CRUD (detail includes 24h results, 90-day bars, incidents) |
| `POST /api/checks/{id}/run` | Probe on the next scheduler tick |
| `POST/DELETE /api/checks/{id}/drill` | Start/stop a chaos drill |
| `GET /api/incidents?status=active\|resolved\|all` | Incident list |
| `GET /api/incidents/{id}` | Incident and timeline |
| `POST /api/incidents/{id}/acknowledge` · `/resolve` · `/notes` | Incident actions |
| `GET/PUT /api/settings`, `POST /api/settings/test-notification`, `GET /api/notifications` | Workspace and notification settings, delivery log |
| `GET /api/public/status/{slug}` | Public status page data (no auth) |
| `GET /healthz`, `/readyz`, `/metrics` | Liveness, readiness (database), Prometheus metrics |

## Security notes and known limitations

- **SSRF:** probe targets are validated at the API boundary, and every outbound probe connection is checked after DNS resolution against private and reserved ranges (including the `169.254.169.254` metadata address). Slack webhook URLs must be `https://hooks.slack.com/services/…`.
- **Token storage:** the web app keeps the bearer token in `localStorage` because the console and API are on different sites. `netlify.toml` sets a strict Content-Security-Policy to limit XSS exposure.
- **Rate limits** are in-memory per instance.
- **Demo workspace:** it is shared by all visitors, so its settings are read-only.
- **Single region:** there is no multi-region probing.
- **Accounts:** one workspace per user, and no password reset or email verification.
- **Notifications** support Slack (and the mock provider) only.
- **API types:** the TypeScript types in `apps/web/src/api/types.ts` are maintained by hand alongside the Go API.

## Contributing

There is no CONTRIBUTING.md yet. The workflow is:
1. Create a branch from `main`.
2. Make small commits with tests for new code.
3. Open a pull request.
4. Wait for all four CI jobs (API, Web, E2E, Docker) to pass before merging.

Don't commit secrets; `.env` files are gitignored, and new variable names belong in `.env.example`.

## License

No license file is included. All rights reserved.
