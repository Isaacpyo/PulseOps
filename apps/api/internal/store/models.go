package store

import (
	"encoding/json"
	"time"
)

// Workspace is a tenant.
type Workspace struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	Slug            string    `json:"slug"`
	SlackWebhookURL *string   `json:"-"`
	IsDemo          bool      `json:"isDemo"`
	CreatedAt       time.Time `json:"createdAt"`
}

// User is an account belonging to one workspace.
type User struct {
	ID           int64     `json:"id"`
	WorkspaceID  int64     `json:"workspaceId"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
}

// Check is a monitored target plus its rule config and live state.
type Check struct {
	ID                   int64      `json:"id"`
	WorkspaceID          int64      `json:"-"`
	Name                 string     `json:"name"`
	Kind                 string     `json:"kind"`
	Target               string     `json:"target"`
	IntervalSeconds      int        `json:"intervalSeconds"`
	TimeoutMs            int        `json:"timeoutMs"`
	ExpectedStatus       *int       `json:"expectedStatus"`
	LatencyThresholdMs   *int       `json:"latencyThresholdMs"`
	FailureThreshold     int        `json:"failureThreshold"`
	RecoveryThreshold    int        `json:"recoveryThreshold"`
	Enabled              bool       `json:"enabled"`
	IsPublic             bool       `json:"isPublic"`
	Status               string     `json:"status"`
	ConsecutiveFailures  int        `json:"consecutiveFailures"`
	ConsecutiveSlow      int        `json:"consecutiveSlow"`
	ConsecutiveHealthy   int        `json:"consecutiveHealthy"`
	ConsecutiveReachable int        `json:"consecutiveReachable"`
	LastCheckedAt        *time.Time `json:"lastCheckedAt"`
	LastLatencyMs        *int       `json:"lastLatencyMs"`
	LastError            *string    `json:"lastError"`
	NextRunAt            time.Time  `json:"nextRunAt"`
	DrillUntil           *time.Time `json:"drillUntil"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}

const checkColumns = `c.id, c.workspace_id, c.name, c.kind, c.target, c.interval_seconds, c.timeout_ms,
	c.expected_status, c.latency_threshold_ms, c.failure_threshold, c.recovery_threshold,
	c.enabled, c.is_public, c.status, c.consecutive_failures, c.consecutive_slow,
	c.consecutive_healthy, c.consecutive_reachable, c.last_checked_at, c.last_latency_ms,
	c.last_error, c.next_run_at, c.drill_until, c.created_at, c.updated_at`

func (c *Check) scanTargets() []any {
	return []any{
		&c.ID, &c.WorkspaceID, &c.Name, &c.Kind, &c.Target, &c.IntervalSeconds, &c.TimeoutMs,
		&c.ExpectedStatus, &c.LatencyThresholdMs, &c.FailureThreshold, &c.RecoveryThreshold,
		&c.Enabled, &c.IsPublic, &c.Status, &c.ConsecutiveFailures, &c.ConsecutiveSlow,
		&c.ConsecutiveHealthy, &c.ConsecutiveReachable, &c.LastCheckedAt, &c.LastLatencyMs,
		&c.LastError, &c.NextRunAt, &c.DrillUntil, &c.CreatedAt, &c.UpdatedAt,
	}
}

// CheckInput is the user-editable part of a check (already validated).
type CheckInput struct {
	Name               string
	Kind               string
	Target             string
	IntervalSeconds    int
	TimeoutMs          int
	ExpectedStatus     *int
	LatencyThresholdMs *int
	FailureThreshold   int
	RecoveryThreshold  int
	Enabled            bool
	IsPublic           bool
}

// ResultPoint is a compact probe result for sparklines.
type ResultPoint struct {
	At        time.Time `json:"at"`
	Outcome   string    `json:"outcome"`
	LatencyMs int       `json:"latencyMs"`
}

// ActiveIncidentRef summarises a check's active incident.
type ActiveIncidentRef struct {
	ID     int64  `json:"id"`
	Cause  string `json:"cause"`
	Status string `json:"status"`
}

// CheckSummary is a check plus 24h aggregates for list views.
type CheckSummary struct {
	Check
	Uptime24h      *float64           `json:"uptime24h"`
	AvgLatency24h  *int               `json:"avgLatency24h"`
	Recent         []ResultPoint      `json:"recent"`
	ActiveIncident *ActiveIncidentRef `json:"activeIncident"`
}

// CheckResult is a stored probe result.
type CheckResult struct {
	ID         int64     `json:"id"`
	CheckedAt  time.Time `json:"checkedAt"`
	Outcome    string    `json:"outcome"`
	LatencyMs  int       `json:"latencyMs"`
	StatusCode *int      `json:"statusCode"`
	Error      *string   `json:"error"`
}

// Incident is an alert raised against a check.
type Incident struct {
	ID             int64      `json:"id"`
	CheckID        *int64     `json:"checkId"`
	CheckName      string     `json:"checkName"`
	Title          string     `json:"title"`
	Cause          string     `json:"cause"`
	Status         string     `json:"status"`
	OpenedAt       time.Time  `json:"openedAt"`
	AcknowledgedAt *time.Time `json:"acknowledgedAt"`
	AcknowledgedBy *string    `json:"acknowledgedBy"`
	ResolvedAt     *time.Time `json:"resolvedAt"`
	ResolvedBy     *string    `json:"resolvedBy"`
}

const incidentColumns = `i.id, i.check_id, i.check_name, i.title, i.cause, i.status, i.opened_at,
	i.acknowledged_at, ack.name, i.resolved_at, res.name`

const incidentJoins = `LEFT JOIN users ack ON ack.id = i.acknowledged_by
	LEFT JOIN users res ON res.id = i.resolved_by`

func (i *Incident) scanTargets() []any {
	return []any{&i.ID, &i.CheckID, &i.CheckName, &i.Title, &i.Cause, &i.Status, &i.OpenedAt,
		&i.AcknowledgedAt, &i.AcknowledgedBy, &i.ResolvedAt, &i.ResolvedBy}
}

// IncidentEvent is one entry on an incident timeline.
type IncidentEvent struct {
	ID      int64     `json:"id"`
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"`
	Message string    `json:"message"`
	Actor   *string   `json:"actor"`
}

// Notification is an outbox row as shown in the notification log.
type Notification struct {
	ID         int64           `json:"id"`
	IncidentID *int64          `json:"incidentId"`
	Event      string          `json:"event"`
	Provider   *string         `json:"provider"`
	Status     string          `json:"status"`
	Attempts   int             `json:"attempts"`
	Payload    json.RawMessage `json:"payload"`
	LastError  *string         `json:"lastError"`
	CreatedAt  time.Time       `json:"createdAt"`
	SentAt     *time.Time      `json:"sentAt"`
}
