package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/temitopeagbola/pulseops/apps/api/internal/uptime"
)

// ListChecks returns all checks in a workspace with 24h aggregates, the most
// recent results for sparklines, and any active incident.
func (s *Store) ListChecks(ctx context.Context, workspaceID int64) ([]CheckSummary, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+checkColumns+`,
			agg.total, agg.up, agg.avg_latency,
			i.id, i.cause, i.status
		FROM checks c
		LEFT JOIN LATERAL (
			SELECT count(*)::int AS total,
				count(*) FILTER (WHERE r.outcome <> 'down')::int AS up,
				round(avg(r.latency_ms) FILTER (WHERE r.outcome <> 'down'))::int AS avg_latency
			FROM check_results r
			WHERE r.check_id = c.id AND r.checked_at > now() - interval '24 hours'
		) agg ON true
		LEFT JOIN incidents i ON i.check_id = c.id AND i.status <> 'resolved'
		WHERE c.workspace_id = $1
		ORDER BY lower(c.name), c.id`, workspaceID)
	if err != nil {
		return nil, err
	}
	var (
		out   []CheckSummary
		index = map[int64]int{}
	)
	for rows.Next() {
		var (
			cs                  CheckSummary
			total, up           int
			incID               *int64
			incCause, incStatus *string
		)
		targets := append(cs.scanTargets(), &total, &up, &cs.AvgLatency24h, &incID, &incCause, &incStatus)
		if err := rows.Scan(targets...); err != nil {
			rows.Close()
			return nil, err
		}
		cs.Uptime24h = uptime.Percent(up, total)
		if incID != nil {
			cs.ActiveIncident = &ActiveIncidentRef{ID: *incID, Cause: deref(incCause), Status: deref(incStatus)}
		}
		cs.Recent = []ResultPoint{}
		index[cs.ID] = len(out)
		out = append(out, cs)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	recent, err := s.pool.Query(ctx, `SELECT c.id, p.checked_at, p.outcome, p.latency_ms
		FROM checks c
		CROSS JOIN LATERAL (
			SELECT checked_at, outcome, latency_ms FROM check_results r
			WHERE r.check_id = c.id ORDER BY r.checked_at DESC LIMIT 40
		) p
		WHERE c.workspace_id = $1
		ORDER BY c.id, p.checked_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer recent.Close()
	for recent.Next() {
		var (
			id int64
			p  ResultPoint
		)
		if err := recent.Scan(&id, &p.At, &p.Outcome, &p.LatencyMs); err != nil {
			return nil, err
		}
		if i, ok := index[id]; ok {
			out[i].Recent = append(out[i].Recent, p)
		}
	}
	if out == nil {
		out = []CheckSummary{}
	}
	return out, recent.Err()
}

// CountChecks returns the number of checks in a workspace.
func (s *Store) CountChecks(ctx context.Context, workspaceID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM checks WHERE workspace_id = $1`, workspaceID).Scan(&n)
	return n, err
}

// GetCheck loads one check scoped to a workspace.
func (s *Store) GetCheck(ctx context.Context, workspaceID, id int64) (Check, error) {
	var c Check
	err := s.pool.QueryRow(ctx, `SELECT `+checkColumns+` FROM checks c WHERE c.id = $1 AND c.workspace_id = $2`,
		id, workspaceID).Scan(c.scanTargets()...)
	return c, notFound(err)
}

// CreateCheck inserts a check; it is scheduled to run immediately.
func (s *Store) CreateCheck(ctx context.Context, workspaceID int64, in CheckInput) (Check, error) {
	var c Check
	err := s.pool.QueryRow(ctx, `INSERT INTO checks AS c (workspace_id, name, kind, target, interval_seconds,
			timeout_ms, expected_status, latency_threshold_ms, failure_threshold, recovery_threshold,
			enabled, is_public)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING `+checkColumns,
		workspaceID, in.Name, in.Kind, in.Target, in.IntervalSeconds, in.TimeoutMs, in.ExpectedStatus,
		in.LatencyThresholdMs, in.FailureThreshold, in.RecoveryThreshold, in.Enabled, in.IsPublic,
	).Scan(c.scanTargets()...)
	return c, err
}

// UpdateCheck replaces a check's configuration. Changing what is probed
// resets the live state; re-enabling schedules an immediate probe.
func (s *Store) UpdateCheck(ctx context.Context, workspaceID, id int64, in CheckInput) (Check, error) {
	var c Check
	err := s.pool.QueryRow(ctx, `UPDATE checks c SET
			name = $3, kind = $4, target = $5, interval_seconds = $6, timeout_ms = $7,
			expected_status = $8, latency_threshold_ms = $9, failure_threshold = $10,
			recovery_threshold = $11, enabled = $12, is_public = $13,
			status = CASE WHEN c.kind <> $4 OR c.target <> $5 THEN 'pending' ELSE c.status END,
			consecutive_failures = CASE WHEN c.kind <> $4 OR c.target <> $5 THEN 0 ELSE c.consecutive_failures END,
			consecutive_slow = CASE WHEN c.kind <> $4 OR c.target <> $5 THEN 0 ELSE c.consecutive_slow END,
			consecutive_healthy = CASE WHEN c.kind <> $4 OR c.target <> $5 THEN 0 ELSE c.consecutive_healthy END,
			consecutive_reachable = CASE WHEN c.kind <> $4 OR c.target <> $5 THEN 0 ELSE c.consecutive_reachable END,
			next_run_at = CASE WHEN (NOT c.enabled AND $12) OR c.kind <> $4 OR c.target <> $5 THEN now() ELSE c.next_run_at END,
			updated_at = now()
		WHERE c.id = $1 AND c.workspace_id = $2
		RETURNING `+checkColumns,
		id, workspaceID, in.Name, in.Kind, in.Target, in.IntervalSeconds, in.TimeoutMs, in.ExpectedStatus,
		in.LatencyThresholdMs, in.FailureThreshold, in.RecoveryThreshold, in.Enabled, in.IsPublic,
	).Scan(c.scanTargets()...)
	return c, notFound(err)
}

// DeleteCheck removes a check. Its active incident (if any) is resolved
// first so it does not linger as an orphaned open incident.
func (s *Store) DeleteCheck(ctx context.Context, workspaceID, id int64, actor User) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var name string
		if err := tx.QueryRow(ctx, `SELECT name FROM checks WHERE id = $1 AND workspace_id = $2 FOR UPDATE`,
			id, workspaceID).Scan(&name); err != nil {
			return notFound(err)
		}
		var incID int64
		err := tx.QueryRow(ctx, `UPDATE incidents SET status = 'resolved', resolved_at = now(), resolved_by = $2
			WHERE check_id = $1 AND status <> 'resolved' RETURNING id`, id, actor.ID).Scan(&incID)
		if err == nil {
			if err := addEvent(ctx, tx, incID, "resolved", "Resolved because check \""+name+"\" was deleted by "+actor.Name+".", &actor.ID); err != nil {
				return err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM checks WHERE id = $1`, id)
		return err
	})
}

// TriggerCheck schedules a check to be probed on the next scheduler tick.
func (s *Store) TriggerCheck(ctx context.Context, workspaceID, id int64) (Check, error) {
	var c Check
	err := s.pool.QueryRow(ctx, `UPDATE checks c SET next_run_at = now()
		WHERE c.id = $1 AND c.workspace_id = $2 AND c.enabled
		RETURNING `+checkColumns, id, workspaceID).Scan(c.scanTargets()...)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := s.GetCheck(ctx, workspaceID, id); getErr == nil {
			return c, ErrConflict // exists but paused
		}
	}
	return c, notFound(err)
}

// SetDrill starts (until in the future) or stops (nil) a chaos drill: while
// active, the worker records simulated failures for the check so the full
// alert → incident → notification pipeline can be exercised safely.
func (s *Store) SetDrill(ctx context.Context, workspaceID, id int64, until *time.Time) (Check, error) {
	var c Check
	err := s.pool.QueryRow(ctx, `UPDATE checks c SET drill_until = $3, next_run_at = now()
		WHERE c.id = $1 AND c.workspace_id = $2 AND c.enabled
		RETURNING `+checkColumns, id, workspaceID, until).Scan(c.scanTargets()...)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := s.GetCheck(ctx, workspaceID, id); getErr == nil {
			return c, ErrConflict
		}
	}
	return c, notFound(err)
}

// ListResults returns the most recent results for a check, newest first.
func (s *Store) ListResults(ctx context.Context, checkID int64, since time.Time, limit int) ([]CheckResult, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, checked_at, outcome, latency_ms, status_code, error
		FROM check_results WHERE check_id = $1 AND checked_at >= $2
		ORDER BY checked_at DESC LIMIT $3`, checkID, since, limit)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (CheckResult, error) {
		var r CheckResult
		err := row.Scan(&r.ID, &r.CheckedAt, &r.Outcome, &r.LatencyMs, &r.StatusCode, &r.Error)
		return r, err
	})
	if out == nil {
		out = []CheckResult{}
	}
	return out, err
}

// DailyRollups returns per-day aggregates (with incident counts) for the
// given checks from the given UTC date onwards.
func (s *Store) DailyRollups(ctx context.Context, checkIDs []int64, from time.Time) (map[int64][]uptime.DayRollup, error) {
	out := make(map[int64][]uptime.DayRollup, len(checkIDs))
	if len(checkIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT d.check_id, d.day, d.total, d.up, d.degraded,
			COALESCE(inc.n, 0)::int
		FROM check_daily d
		LEFT JOIN (
			SELECT check_id, (opened_at AT TIME ZONE 'UTC')::date AS day, count(*) AS n
			FROM incidents WHERE check_id = ANY($1) AND opened_at >= $2
			GROUP BY 1, 2
		) inc ON inc.check_id = d.check_id AND inc.day = d.day
		WHERE d.check_id = ANY($1) AND d.day >= $3::date
		ORDER BY d.check_id, d.day`, checkIDs, from.UTC(), from.UTC().Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id int64
			r  uptime.DayRollup
		)
		if err := rows.Scan(&id, &r.Day, &r.Total, &r.Up, &r.Degraded, &r.Incidents); err != nil {
			return nil, err
		}
		out[id] = append(out[id], r)
	}
	return out, rows.Err()
}
