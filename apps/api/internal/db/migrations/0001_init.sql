-- PulseOps initial schema.

CREATE TABLE workspaces (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name              TEXT        NOT NULL,
    slug              TEXT        NOT NULL UNIQUE,
    slack_webhook_url TEXT,
    is_demo           BOOLEAN     NOT NULL DEFAULT false,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workspace_id  BIGINT      NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    email         TEXT        NOT NULL,
    name          TEXT        NOT NULL,
    password_hash TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_lower_idx ON users (lower(email));

-- Opaque bearer tokens; only the SHA-256 of the token is stored.
CREATE TABLE sessions (
    token_hash BYTEA PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

CREATE TABLE checks (
    id                   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workspace_id         BIGINT      NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name                 TEXT        NOT NULL,
    kind                 TEXT        NOT NULL CHECK (kind IN ('http', 'tcp')),
    target               TEXT        NOT NULL,
    interval_seconds     INT         NOT NULL CHECK (interval_seconds BETWEEN 15 AND 3600),
    timeout_ms           INT         NOT NULL CHECK (timeout_ms BETWEEN 100 AND 30000),
    -- NULL accepts any 2xx/3xx response.
    expected_status      INT CHECK (expected_status BETWEEN 100 AND 599),
    -- NULL disables the latency rule.
    latency_threshold_ms INT CHECK (latency_threshold_ms BETWEEN 1 AND 60000),
    failure_threshold    INT         NOT NULL DEFAULT 3 CHECK (failure_threshold BETWEEN 1 AND 10),
    recovery_threshold   INT         NOT NULL DEFAULT 2 CHECK (recovery_threshold BETWEEN 1 AND 10),
    enabled              BOOLEAN     NOT NULL DEFAULT true,
    is_public            BOOLEAN     NOT NULL DEFAULT true,

    -- Live state maintained by the probe workers.
    status               TEXT        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'up', 'degraded', 'down')),
    consecutive_failures INT         NOT NULL DEFAULT 0,
    consecutive_slow     INT         NOT NULL DEFAULT 0,
    consecutive_healthy  INT         NOT NULL DEFAULT 0,
    consecutive_reachable INT        NOT NULL DEFAULT 0,
    last_checked_at      TIMESTAMPTZ,
    last_latency_ms      INT,
    last_error           TEXT,
    next_run_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- While set in the future, probes report a simulated outage (chaos drill).
    drill_until          TIMESTAMPTZ,

    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX checks_due_idx ON checks (next_run_at) WHERE enabled;
CREATE INDEX checks_workspace_idx ON checks (workspace_id, id);

CREATE TABLE check_results (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    check_id    BIGINT      NOT NULL REFERENCES checks (id) ON DELETE CASCADE,
    checked_at  TIMESTAMPTZ NOT NULL,
    outcome     TEXT        NOT NULL CHECK (outcome IN ('up', 'degraded', 'down')),
    latency_ms  INT         NOT NULL,
    status_code INT,
    error       TEXT
);
CREATE INDEX check_results_check_time_idx ON check_results (check_id, checked_at DESC);
CREATE INDEX check_results_time_idx ON check_results (checked_at);

-- Per-check, per-UTC-day rollup backing the 90-day uptime bars.
CREATE TABLE check_daily (
    check_id       BIGINT NOT NULL REFERENCES checks (id) ON DELETE CASCADE,
    day            DATE   NOT NULL,
    total          INT    NOT NULL DEFAULT 0,
    -- Reachable probes (healthy or degraded). Uptime = up / total.
    up             INT    NOT NULL DEFAULT 0,
    degraded       INT    NOT NULL DEFAULT 0,
    latency_sum_ms BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (check_id, day)
);

CREATE TABLE incidents (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workspace_id    BIGINT      NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    check_id        BIGINT REFERENCES checks (id) ON DELETE SET NULL,
    -- Snapshot so incident history survives deleting the check.
    check_name      TEXT        NOT NULL,
    title           TEXT        NOT NULL,
    cause           TEXT        NOT NULL CHECK (cause IN ('down', 'degraded')),
    status          TEXT        NOT NULL CHECK (status IN ('open', 'acknowledged', 'resolved')),
    opened_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    acknowledged_at TIMESTAMPTZ,
    acknowledged_by BIGINT REFERENCES users (id) ON DELETE SET NULL,
    resolved_at     TIMESTAMPTZ,
    resolved_by     BIGINT REFERENCES users (id) ON DELETE SET NULL
);
-- At most one active incident per check; the alert engine relies on this.
CREATE UNIQUE INDEX incidents_one_active_per_check ON incidents (check_id) WHERE status <> 'resolved';
CREATE INDEX incidents_workspace_idx ON incidents (workspace_id, opened_at DESC);

CREATE TABLE incident_events (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    incident_id   BIGINT      NOT NULL REFERENCES incidents (id) ON DELETE CASCADE,
    at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    kind          TEXT        NOT NULL,
    message       TEXT        NOT NULL,
    actor_user_id BIGINT REFERENCES users (id) ON DELETE SET NULL
);
CREATE INDEX incident_events_incident_idx ON incident_events (incident_id, at, id);

-- Transactional outbox for alert notifications.
CREATE TABLE notifications (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workspace_id    BIGINT      NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    incident_id     BIGINT REFERENCES incidents (id) ON DELETE CASCADE,
    event           TEXT        NOT NULL,
    provider        TEXT,
    status          TEXT        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    attempts        INT         NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    payload         JSONB       NOT NULL,
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at         TIMESTAMPTZ
);
CREATE INDEX notifications_pending_idx ON notifications (next_attempt_at) WHERE status = 'pending';
CREATE INDEX notifications_workspace_idx ON notifications (workspace_id, created_at DESC);
