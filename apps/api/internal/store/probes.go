package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/isaacpyo/pulseops/apps/api/internal/alert"
	"github.com/isaacpyo/pulseops/apps/api/internal/notify"
)

// ClaimedCheck is a check leased for probing plus the time it was due.
type ClaimedCheck struct {
	Check
	DueAt time.Time
}

// ClaimDueChecks atomically leases up to limit due checks by pushing their
// next_run_at forward one interval. SKIP LOCKED makes this safe across
// multiple API instances.
func (s *Store) ClaimDueChecks(ctx context.Context, limit int) ([]ClaimedCheck, error) {
	rows, err := s.pool.Query(ctx, `UPDATE checks c
		-- During a chaos drill probe every 10s so the demo pipeline reacts quickly.
		SET next_run_at = now() + make_interval(secs => CASE
			WHEN c.drill_until > now() THEN LEAST(c.interval_seconds, 10)
			ELSE c.interval_seconds END)
		FROM (
			SELECT id, next_run_at AS due_at FROM checks
			WHERE enabled AND next_run_at <= now()
			ORDER BY next_run_at
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		) due
		WHERE c.id = due.id
		RETURNING `+checkColumns+`, due.due_at`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ClaimedCheck, error) {
		var c ClaimedCheck
		return c, row.Scan(append(c.scanTargets(), &c.DueAt)...)
	})
}

// ProbeRecord is a completed probe to persist.
type ProbeRecord struct {
	CheckID    int64
	CheckedAt  time.Time
	OK         bool
	LatencyMs  int
	StatusCode int
	Error      string
}

// ProbeOutcome reports what RecordProbe did, for metrics and logging.
type ProbeOutcome struct {
	Check      Check
	Outcome    alert.Outcome
	Decision   alert.Decision
	IncidentID int64
}

// RecordProbe persists a probe result and applies the alert engine's
// decision in a single transaction: result row, daily rollup, check state,
// incident lifecycle, timeline event and outbox notification either all
// commit or none do.
func (s *Store) RecordProbe(ctx context.Context, rec ProbeRecord) (ProbeOutcome, error) {
	var out ProbeOutcome
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		c := &out.Check
		if err := tx.QueryRow(ctx, `SELECT `+checkColumns+` FROM checks c WHERE c.id = $1 FOR UPDATE`, rec.CheckID).
			Scan(c.scanTargets()...); err != nil {
			return notFound(err)
		}
		if !c.Enabled {
			return ErrConflict
		}

		var (
			activeID    int64
			activeCause string
		)
		err := tx.QueryRow(ctx, `SELECT id, cause FROM incidents WHERE check_id = $1 AND status <> 'resolved'`, c.ID).
			Scan(&activeID, &activeCause)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		rule := alert.Rule{
			FailureThreshold:  c.FailureThreshold,
			RecoveryThreshold: c.RecoveryThreshold,
		}
		if c.LatencyThresholdMs != nil {
			rule.LatencyThresholdMs = *c.LatencyThresholdMs
		}
		state := alert.State{
			ConsecutiveFailures:  c.ConsecutiveFailures,
			ConsecutiveSlow:      c.ConsecutiveSlow,
			ConsecutiveHealthy:   c.ConsecutiveHealthy,
			ConsecutiveReachable: c.ConsecutiveReachable,
			Active:               alert.Cause(activeCause),
		}
		next, outcome, decision := alert.Evaluate(rule, state, alert.Probe{OK: rec.OK, LatencyMs: rec.LatencyMs})
		out.Outcome, out.Decision = outcome, decision

		var statusCode *int
		if rec.StatusCode != 0 {
			statusCode = &rec.StatusCode
		}
		var errText *string
		if rec.Error != "" {
			errText = &rec.Error
		}
		if _, err := tx.Exec(ctx, `INSERT INTO check_results (check_id, checked_at, outcome, latency_ms, status_code, error)
			VALUES ($1, $2, $3, $4, $5, $6)`, c.ID, rec.CheckedAt, outcome, rec.LatencyMs, statusCode, errText); err != nil {
			return err
		}

		up, degraded, latency := 0, 0, 0
		if outcome != alert.OutcomeDown {
			up, latency = 1, rec.LatencyMs
		}
		if outcome == alert.OutcomeDegraded {
			degraded = 1
		}
		if _, err := tx.Exec(ctx, `INSERT INTO check_daily (check_id, day, total, up, degraded, latency_sum_ms)
			VALUES ($1, $2::date, 1, $3, $4, $5)
			ON CONFLICT (check_id, day) DO UPDATE SET
				total = check_daily.total + 1,
				up = check_daily.up + EXCLUDED.up,
				degraded = check_daily.degraded + EXCLUDED.degraded,
				latency_sum_ms = check_daily.latency_sum_ms + EXCLUDED.latency_sum_ms`,
			c.ID, rec.CheckedAt.UTC().Format("2006-01-02"), up, degraded, latency); err != nil {
			return err
		}

		if err := tx.QueryRow(ctx, `UPDATE checks c SET
				status = $2, consecutive_failures = $3, consecutive_slow = $4,
				consecutive_healthy = $5, consecutive_reachable = $6,
				last_checked_at = $7, last_latency_ms = $8, last_error = $9
			WHERE c.id = $1
			RETURNING `+checkColumns,
			c.ID, outcome, next.ConsecutiveFailures, next.ConsecutiveSlow, next.ConsecutiveHealthy,
			next.ConsecutiveReachable, rec.CheckedAt, rec.LatencyMs, errText,
		).Scan(c.scanTargets()...); err != nil {
			return err
		}

		incID, err := s.applyDecision(ctx, tx, *c, rec, decision, activeID)
		out.IncidentID = incID
		return err
	})
	return out, err
}

func (s *Store) applyDecision(ctx context.Context, tx pgx.Tx, c Check, rec ProbeRecord, d alert.Decision, activeID int64) (int64, error) {
	lastErr := rec.Error
	if lastErr == "" {
		lastErr = "unknown error"
	}
	threshold := 0
	if c.LatencyThresholdMs != nil {
		threshold = *c.LatencyThresholdMs
	}

	switch d.Action {
	case alert.ActionOpen:
		var title, detail string
		if d.Cause == alert.CauseDown {
			title = c.Name + " is down"
			detail = fmt.Sprintf("Opened after %d consecutive failed probes. Last error: %s.", c.ConsecutiveFailures, lastErr)
		} else {
			title = c.Name + " is responding slowly"
			detail = fmt.Sprintf("Opened after %d consecutive probes slower than %d ms (last: %d ms).", c.ConsecutiveSlow, threshold, rec.LatencyMs)
		}
		var (
			id       int64
			openedAt time.Time
		)
		if err := tx.QueryRow(ctx, `INSERT INTO incidents (workspace_id, check_id, check_name, title, cause, status, opened_at)
			VALUES ($1, $2, $3, $4, $5, 'open', $6) RETURNING id, opened_at`,
			c.WorkspaceID, c.ID, c.Name, title, d.Cause, rec.CheckedAt).Scan(&id, &openedAt); err != nil {
			return 0, err
		}
		if err := addEvent(ctx, tx, id, "opened", detail, nil); err != nil {
			return id, err
		}
		info := notify.IncidentInfo{ID: id, Title: title, CheckName: c.Name, Target: c.Target, Cause: string(d.Cause), OpenedAt: openedAt}
		return id, s.enqueue(ctx, tx, c.WorkspaceID, &id, notify.BuildIncidentMessage("opened", info, detail, "", s.appURL))

	case alert.ActionEscalate:
		title := c.Name + " is down"
		detail := fmt.Sprintf("Escalated from degraded performance to an outage after %d consecutive failed probes. Last error: %s.", c.ConsecutiveFailures, lastErr)
		var openedAt time.Time
		if err := tx.QueryRow(ctx, `UPDATE incidents SET cause = 'down', title = $2, status = 'open'
			WHERE id = $1 RETURNING opened_at`, activeID, title).Scan(&openedAt); err != nil {
			return activeID, err
		}
		if err := addEvent(ctx, tx, activeID, "escalated", detail, nil); err != nil {
			return activeID, err
		}
		info := notify.IncidentInfo{ID: activeID, Title: title, CheckName: c.Name, Target: c.Target, Cause: "down", OpenedAt: openedAt}
		return activeID, s.enqueue(ctx, tx, c.WorkspaceID, &activeID, notify.BuildIncidentMessage("escalated", info, detail, "", s.appURL))

	case alert.ActionResolve:
		var detail string
		if d.Cause == alert.CauseDown {
			detail = fmt.Sprintf("Recovered automatically after %d consecutive successful probes (last: %d ms).", c.ConsecutiveReachable, rec.LatencyMs)
		} else {
			detail = fmt.Sprintf("Latency back under %d ms for %d consecutive probes (last: %d ms). Resolved automatically.", threshold, c.ConsecutiveHealthy, rec.LatencyMs)
		}
		var (
			title    string
			openedAt time.Time
		)
		if err := tx.QueryRow(ctx, `UPDATE incidents SET status = 'resolved', resolved_at = $2
			WHERE id = $1 RETURNING title, opened_at`, activeID, rec.CheckedAt).Scan(&title, &openedAt); err != nil {
			return activeID, err
		}
		if err := addEvent(ctx, tx, activeID, "auto_resolved", detail, nil); err != nil {
			return activeID, err
		}
		info := notify.IncidentInfo{ID: activeID, Title: title, CheckName: c.Name, Target: c.Target, Cause: string(d.Cause), OpenedAt: openedAt}
		return activeID, s.enqueue(ctx, tx, c.WorkspaceID, &activeID, notify.BuildIncidentMessage("resolved", info, detail, "", s.appURL))
	}
	return activeID, nil
}

func addEvent(ctx context.Context, tx pgx.Tx, incidentID int64, kind, message string, actor *int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO incident_events (incident_id, kind, message, actor_user_id) VALUES ($1, $2, $3, $4)`,
		incidentID, kind, message, actor)
	return err
}

func (s *Store) enqueue(ctx context.Context, tx pgx.Tx, workspaceID int64, incidentID *int64, msg notify.Message) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO notifications (workspace_id, incident_id, event, payload) VALUES ($1, $2, $3, $4)`,
		workspaceID, incidentID, msg.Event, payload)
	return err
}

// PruneResults deletes raw results older than before, in batches.
func (s *Store) PruneResults(ctx context.Context, before time.Time) (int64, error) {
	var total int64
	for {
		tag, err := s.pool.Exec(ctx, `DELETE FROM check_results WHERE id IN (
			SELECT id FROM check_results WHERE checked_at < $1 LIMIT 5000)`, before)
		if err != nil {
			return total, err
		}
		total += tag.RowsAffected()
		if tag.RowsAffected() < 5000 {
			return total, nil
		}
	}
}

// PruneRollups deletes daily rollups older than before (beyond the status
// page window).
func (s *Store) PruneRollups(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM check_daily WHERE day < $1::date`, before.UTC().Format("2006-01-02"))
	return tag.RowsAffected(), err
}

// ActiveIncidentCounts returns the number of active incidents by cause
// across all workspaces (for the open-incidents gauge).
func (s *Store) ActiveIncidentCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT cause, count(*)::int FROM incidents WHERE status <> 'resolved' GROUP BY cause`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{"down": 0, "degraded": 0}
	for rows.Next() {
		var (
			cause string
			n     int
		)
		if err := rows.Scan(&cause, &n); err != nil {
			return nil, err
		}
		out[cause] = n
	}
	return out, rows.Err()
}
