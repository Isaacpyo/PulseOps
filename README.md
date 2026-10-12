# PulseOps

[![CI](https://github.com/Isaacpyo/PulseOps/actions/workflows/ci.yml/badge.svg)](https://github.com/Isaacpyo/PulseOps/actions/workflows/ci.yml)

**Live demo: https://pulseops.temitopeagbola.com** — click **Try the demo** (no sign-up). Public status page: [/status/demo](https://pulseops.temitopeagbola.com/status/demo).

PulseOps is a small, genuinely working uptime-monitoring and incident-management platform. A Go service probes your HTTP and TCP endpoints on their own intervals, feeds every result through an alert-rule engine (consecutive failures, latency thresholds), and opens, escalates and auto-resolves incidents with a full timeline that operators can acknowledge, annotate and resolve. Alerts go to Slack through a transactional outbox with retries (or to a mock provider when no webhook is configured), a public status page shows 90 days of uptime per component, and the platform instruments itself with Prometheus metrics, shipped to Grafana Cloud via remote_write in production and to a local Prometheus + Grafana in Docker.

> **First request slow?** The API runs on Render's free tier, which sleeps when idle. The UI shows a “Waking the server…” banner and retries until it is up (usually 30–60s).

![Overview](docs/screenshots/overview.png)

| Check detail | Public status page |
|---|---|
| ![Check detail](docs/screenshots/check.png) | ![Status page](docs/screenshots/status.png) |

## Features

- **HTTP and TCP checks** with per-check interval, timeout and expected status; probes run in a bounded Go worker pool.
- **Alert rules → incidents**: open after *N* consecutive failures or *N* consecutive probes slower than a latency threshold; degraded incidents **escalate** to outages; incidents **auto-resolve** after *M* healthy probes. One active incident per check, enforced by a partial unique index.
- **Incident timeline** with acknowledge, resolve (with note) and free-form notes; every notification delivery is recorded on it.
- **Slack notifications** (Block Kit) via a transactional **outbox** with exponential backoff; a **mock provider** is selected by `NOTIFIER=mock` (or automatically when no webhook is set), so everything works without keys.
- **Public status page** per workspace with 90-day uptime bars, component states driven by incidents (not single blips) and recent incident history.
- **Chaos drill**: “Simulate outage” makes a check report failures for 2 minutes so you can watch the whole pipeline (incident → Slack → timeline → auto-resolve) on the live demo.
- **Observability of the monitor itself**: `/metrics` (probe latency histograms, outcomes, scheduler lag, incident and notification counters, API RED metrics), a committed Grafana dashboard, Prometheus alert rules, and a built-in **remote_write pusher** for Grafana Cloud.
- **Multi-tenant**: every query is scoped by workspace; integration tests prove cross-tenant access returns 404.
- **Seeded demo** with real, publicly probe-able targets and 90 days of history; **one-click demo login**.
- Loading, empty and error states everywhere, responsive to 375px, keyboard-navigable charts, light and dark themes.

## Architecture

```mermaid
flowchart LR
  subgraph Browser
    UI["React console + status page<br/>(Netlify)"]
  end

  subgraph API["Go API — single binary (Render free web service)"]
    HTTP["chi HTTP API<br/>auth · validation · rate limits"]
    SCHED["Scheduler<br/>claims due checks<br/>FOR UPDATE SKIP LOCKED"]
    POOL["Probe worker pool<br/>HTTP / TCP + SSRF guard"]
    RULES["Alert engine<br/>(pure function)"]
    DISP["Notification dispatcher<br/>outbox + backoff"]
    METRICS["/metrics + remote_write pusher"]
  end

  PG[("Postgres (Neon)<br/>checks · results · daily rollups<br/>incidents · timeline · outbox")]
  TARGETS(["Monitored endpoints"])
  SLACK(["Slack webhook<br/>or mock provider"])
  GC(["Grafana Cloud<br/>(Prometheus + dashboards)"])

  UI -- "JSON + bearer token" --> HTTP
  HTTP <--> PG
  SCHED -- "lease due checks" --> PG
  SCHED --> POOL
  POOL -- "probe" --> TARGETS
  POOL --> RULES
  RULES -- "result, rollup, incident,<br/>timeline, outbox — one tx" --> PG
  DISP -- "claim pending" --> PG
  DISP --> SLACK
  METRICS -- "remote_write (snappy protobuf)" --> GC
```

**Why one binary?** Render's free tier offers one web service and no free background workers. Running the scheduler, dispatcher and API in one process means probes run whenever the service is awake. Work is claimed with `FOR UPDATE SKIP LOCKED`, so scaling to several instances stays correct.

**How a probe flows** — `scheduler.go` leases due checks (pushing `next_run_at` forward) → a worker probes the target through a dialer whose `Control` hook rejects private/loopback/link-local/metadata IPs *after* DNS resolution (DNS-rebinding safe) → `store.RecordProbe` runs one transaction: insert the result, upsert the per-day rollup used by the 90-day bars, call `alert.Evaluate`, update streak counters, and open/escalate/resolve the incident with a timeline event and an outbox row → the dispatcher delivers outbox rows to Slack or the mock provider and writes “notified” to the timeline.

```mermaid
stateDiagram-v2
  [*] --> Healthy
  Healthy --> Down: N consecutive failures
  Healthy --> Degraded: N consecutive probes > latency threshold
  Degraded --> Down: N consecutive failures (escalate)
  Degraded --> Healthy: M consecutive fast probes (auto-resolve)
  Down --> Healthy: M consecutive reachable probes (auto-resolve)
  Down --> Healthy: operator resolves (streaks reset)
  Degraded --> Healthy: operator resolves (streaks reset)
```

### Repository layout

```
apps/api            Go 1.26 API: cmd/pulseops, internal/{alert,probe,store,worker,httpapi,notify,metrics,seed,…}
apps/web            React 19 + Vite + TypeScript + Tailwind console and status page, Vitest + Playwright
deploy/prometheus   Local Prometheus scrape config and alert rules
deploy/grafana      Provisioning, committed dashboard JSON and its generator
docs/PLAN.md        Architecture plan
docker-compose.yml  Whole stack locally
render.yaml         Render blueprint for the API
netlify.toml        Netlify build for apps/web
```

## Run it locally

### Everything in Docker (one command)

```bash
cp .env.example .env        # optional — defaults work as-is
docker compose up --build
```

| Service | URL |
|---|---|
| Web console | http://localhost:3000 (click **Try the demo**) |
| API | http://localhost:8080 (`/healthz`, `/metrics`) |
| Prometheus | http://localhost:9090 (alerts at `/alerts`) |
| Grafana | http://localhost:3001 — dashboard *PulseOps — platform overview* (anonymous view; admin / admin) |

The compose API sets `ALLOW_PRIVATE_TARGETS=true` so you can monitor other containers (e.g. `http://api:8080/healthz`).

### Dev mode (hot reload)

Requires Go 1.26+, Node 22+ and a Postgres 15+.

```bash
# API
cd apps/api
export DATABASE_URL=postgres://pulseops:pulseops@localhost:5432/pulseops?sslmode=disable
go run ./cmd/pulseops

# Web (another terminal)
cd apps/web
npm install
VITE_API_URL=http://localhost:8080 npm run dev     # http://localhost:5173
```

### Tests

```bash
# Go: unit tests everywhere; Postgres integration tests run when TEST_DATABASE_URL is set
cd apps/api
TEST_DATABASE_URL=postgres://pulseops:pulseops@localhost:5432/pulseops_test?sslmode=disable go test -race ./...
golangci-lint run

# Web: lint, typecheck, unit tests
cd apps/web
npm run lint && npm run typecheck && npm test

# E2E smoke (boots the real API + a production web build; needs Postgres)
E2E_DATABASE_URL=postgres://pulseops:pulseops@localhost:5432/pulseops_e2e?sslmode=disable npm run e2e
```

What is covered: the alert engine state machine, uptime bar maths, SSRF guard and probes (against real local HTTP/TCP servers), Slack payloads and routing, remote_write encoding (round-tripped through a protobuf decoder), the full probe → incident → notification → auto-resolve pipeline and outbox retries against Postgres, HTTP API auth/validation/tenant isolation/rate limits/CORS, demo seeding, and on the web side formatters, chart maths, form validation, the cold-start retry logic and the demo login flow. The Playwright smoke test covers demo login, creating a check, acknowledge → note → resolve, and the public status page at desktop width and 375px. CI runs all of it on every push and PR.

## Deploy (free tiers)

The Netlify site already exists and is linked to this repo with the custom domain configured; `netlify.toml` builds `apps/web` on every merge to `main`.

1. **Postgres on Neon** — create a project (region *AWS Europe Central (Frankfurt)* to sit next to Render), create a database `pulseops`, and copy the **pooled** connection string (it ends with `?sslmode=require`). Migrations run automatically on API boot.
2. **API on Render** — *New → Blueprint*, pick this repo; Render reads `render.yaml` and creates `pulseops-api` (Docker, free plan, Frankfurt, health check `/healthz`). Fill in the prompted secrets: `DATABASE_URL` (Neon), and optionally `SLACK_WEBHOOK_URL` and the `REMOTE_WRITE_*` values. Note the service URL, e.g. `https://pulseops-api.onrender.com`.
3. **Web on Netlify** — set `VITE_API_URL` to the Render URL in *Site configuration → Environment variables*, then trigger a deploy. If the API is on a domain other than `*.onrender.com`, update `connect-src` in `netlify.toml`.
4. **Slack (optional)** — create an incoming webhook (Slack app → *Incoming Webhooks*), set `SLACK_WEBHOOK_URL` and `NOTIFIER=slack` on Render. Workspaces can also set their own webhook under **Settings** (the shared demo workspace is read-only).
5. **Grafana Cloud (optional)** — create a free stack; under *Connections → Prometheus → Send metrics* copy the remote write URL and instance ID, and create an access-policy token with `metrics:write`. Set `REMOTE_WRITE_URL`, `REMOTE_WRITE_USERNAME` (instance ID) and `REMOTE_WRITE_PASSWORD` (token) on Render, then import `deploy/grafana/dashboards/pulseops.json` and pick your Grafana Cloud Prometheus data source.

**Free-tier behaviour to know about:** Render free services sleep after ~15 minutes without inbound traffic, and probes only run while the instance is awake. With `DEMO_RESET_ON_BOOT=true` (set in `render.yaml`), every wake re-seeds a clean demo, so visitors never land on a demo someone else wrecked. For continuous monitoring, point any free external pinger at `/healthz` or move to a paid instance. Neon's free compute also suspends when idle; the API retries its initial connection while it resumes.

## Environment variables

All variables are documented in [`.env.example`](.env.example).

| Variable | Default | Where | Purpose |
|---|---|---|---|
| `DATABASE_URL` | — (required) | API | Postgres connection string (Neon: pooled URL, `sslmode=require`) |
| `PORT` | `8080` | API | HTTP port (Render injects it) |
| `CORS_ORIGINS` | `http://localhost:5173,http://localhost:3000` | API | Comma-separated browser origins allowed to call the API |
| `PUBLIC_APP_URL` | `http://localhost:5173` | API | Console URL used in Slack message links |
| `TRUST_PROXY_HEADERS` | `false` | API | Use `X-Forwarded-For` for client IP (rate limiting) — only behind a trusted proxy |
| `LOG_LEVEL` | `info` | API | `debug` / `info` / `warn` / `error` (JSON logs) |
| `SESSION_TTL` | `168h` | API | Session lifetime for regular accounts (demo sessions: 24h) |
| `SCHEDULER_ENABLED` | `true` | API | Run the probe scheduler in this process |
| `WORKER_CONCURRENCY` | `8` | API | Concurrent probes per instance |
| `ALLOW_PRIVATE_TARGETS` | `false` | API | Allow probing private/loopback/link-local addresses (SSRF guard off) |
| `RESULT_RETENTION_DAYS` | `7` | API | Raw result retention (daily rollups kept 100 days) |
| `MAX_CHECKS_PER_WORKSPACE` | `25` | API | Abuse guard |
| `NOTIFIER` | `mock` | API | `mock` or `slack` |
| `SLACK_WEBHOOK_URL` | — | API | Default Slack incoming webhook (secret) |
| `DEMO_ENABLED` | `true` | API | Seed the demo workspace and enable `POST /api/auth/demo` |
| `DEMO_RESET_ON_BOOT` | `false` | API | Re-seed the demo workspace on every boot |
| `METRICS_TOKEN` | — | API | Bearer token required for `GET /metrics` when set (secret) |
| `REMOTE_WRITE_URL` | — | API | Prometheus remote_write endpoint (Grafana Cloud); empty disables |
| `REMOTE_WRITE_USERNAME` / `REMOTE_WRITE_PASSWORD` | — | API | Basic-auth credentials for remote_write (secret) |
| `REMOTE_WRITE_INTERVAL` | `30s` | API | Push interval |
| `VITE_API_URL` | `http://localhost:8080` | Web (build time) | API base URL |
| `POSTGRES_PASSWORD`, `GRAFANA_ADMIN_PASSWORD` | `pulseops`, `admin` | Compose | Local containers only |

## Stack substitutions

| Specified | Used | Why |
|---|---|---|
| Hosted Prometheus + Grafana | **Grafana Cloud free tier via remote_write** (production); Prometheus + Grafana containers with provisioned dashboards (local) | Render's free tier can't run a Prometheus or Grafana Agent sidecar and nothing could scrape a sleeping instance. Instead the API pushes its own registry using the Prometheus remote_write 1.0 protocol (`internal/metrics/remotewrite.go`: hand-encoded protobuf + snappy, ~150 lines, no heavy Prometheus dependency). The same `/metrics` endpoint is scraped by Prometheus in `docker compose`, and the committed dashboard JSON works in both via a data-source variable. |
| Slack webhooks | Slack **plus a mock provider** selected by `NOTIFIER` | So the app, demo and tests work without keys. The mock records deliveries in the notification log and timeline exactly like Slack. |
| Redis / Mongo | Not used | The project's stack doesn't need them: Postgres handles the work queue (`SKIP LOCKED`) and the outbox. |

## API

All JSON. Authenticated routes take `Authorization: Bearer <token>`. Errors use `{"error": {"code", "message", "fields"}}`; validation failures are `422` with per-field messages.

| Method & path | Purpose |
|---|---|
| `POST /api/auth/register` · `/login` · `/demo` · `/logout` | Sessions (rate-limited per IP) |
| `GET /api/me` | Current user and workspace |
| `GET/POST /api/checks`, `GET/PUT/DELETE /api/checks/{id}` | Check CRUD (detail includes 24h results, 90-day bars, incidents) |
| `POST /api/checks/{id}/run` | Probe on the next scheduler tick |
| `POST/DELETE /api/checks/{id}/drill` | Start/stop a chaos drill (30s–15m) |
| `GET /api/incidents?status=active\|resolved\|all` | Incident list |
| `GET /api/incidents/{id}` | Incident + timeline |
| `POST /api/incidents/{id}/acknowledge` · `/resolve` · `/notes` | Incident actions |
| `GET/PUT /api/settings`, `POST /api/settings/test-notification`, `GET /api/notifications` | Workspace + delivery settings and log |
| `GET /api/public/status/{slug}` | Public status page data (cacheable, no targets exposed) |
| `GET /healthz`, `/readyz`, `/metrics` | Liveness, readiness (DB), Prometheus |

## Security notes

- **SSRF**: user-supplied targets are validated at the API boundary *and* every outbound probe connection is checked after DNS resolution against loopback, RFC 1918, link-local (incl. cloud metadata `169.254.169.254`), CGNAT, NAT64 and other reserved ranges. Slack webhooks must be `https://hooks.slack.com/services/…`.
- **Sessions** are random 256-bit bearer tokens; only their SHA-256 is stored, so a database leak doesn't leak live sessions. Passwords use bcrypt (cost 12) and login equalises timing for unknown emails.
- Tokens live in `localStorage` because the console (Netlify) and API (Render) are different sites, where cookies would be third-party. The trade-off is XSS exposure, mitigated by a strict CSP in `netlify.toml`, no `dangerouslySetInnerHTML`, and React's escaping.
- Strict JSON decoding (unknown fields rejected, 64 KB body cap), per-IP rate limits on auth and writes, a per-workspace check cap and a 30s minimum interval to stop the service being used as a traffic cannon.
- The demo workspace is shared: its settings are read-only and it is re-seeded on boot in production.

## Known limitations / tech debt

- Rate limits are in-memory per instance and keyed on `X-Forwarded-For` behind Render; fine for one instance, not a security boundary.
- Probes only run while the free Render instance is awake (see *Deploy*); there is no multi-region probing, so a probe failure from Frankfurt can't distinguish a regional network issue from an outage.
- One workspace per user; no invitations, roles, password reset or email verification.
- Status page components map 1:1 to checks; no component grouping, scheduled maintenance or subscriber notifications.
- Notifications are Slack-only (plus mock); no per-check routing or escalation policies.
- The API's TypeScript types in `apps/web/src/api/types.ts` are maintained by hand; an OpenAPI spec + codegen would remove that drift risk.
