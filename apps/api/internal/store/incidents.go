package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/isaacpyo/pulseops/apps/api/internal/notify"
)

// IncidentFilter narrows ListIncidents.
type IncidentFilter struct {
	// Status is "active" (open or acknowledged), "resolved", or "" for all.
	Status  string
	CheckID int64
	Limit   int
}

// ListIncidents returns incidents for a workspace, newest first.
func (s *Store) ListIncidents(ctx context.Context, workspaceID int64, f IncidentFilter) ([]Incident, error) {
	where := []string{"i.workspace_id = $1"}
	args := []any{workspaceID}
	switch f.Status {
	case "active":
		where = append(where, "i.status <> 'resolved'")
	case "resolved":
		where = append(where, "i.status = 'resolved'")
	}
	if f.CheckID > 0 {
		args = append(args, f.CheckID)
		where = append(where, fmt.Sprintf("i.check_id = $%d", len(args)))
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 100
	}
	args = append(args, f.Limit)
	rows, err := s.pool.Query(ctx, `SELECT `+incidentColumns+` FROM incidents i `+incidentJoins+`
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY (i.status = 'resolved'), i.opened_at DESC
		LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Incident, error) {
		var i Incident
		return i, row.Scan(i.scanTargets()...)
	})
	if out == nil {
		out = []Incident{}
	}
	return out, err
}

// GetIncident returns an incident with its timeline (oldest event first).
func (s *Store) GetIncident(ctx context.Context, workspaceID, id int64) (Incident, []IncidentEvent, error) {
	var inc Incident
	if err := s.pool.QueryRow(ctx, `SELECT `+incidentColumns+` FROM incidents i `+incidentJoins+`
		WHERE i.id = $1 AND i.workspace_id = $2`, id, workspaceID).Scan(inc.scanTargets()...); err != nil {
		return inc, nil, notFound(err)
	}
	rows, err := s.pool.Query(ctx, `SELECT e.id, e.at, e.kind, e.message, u.name
		FROM incident_events e LEFT JOIN users u ON u.id = e.actor_user_id
		WHERE e.incident_id = $1 ORDER BY e.at, e.id`, id)
	if err != nil {
		return inc, nil, err
	}
	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (IncidentEvent, error) {
		var e IncidentEvent
		return e, row.Scan(&e.ID, &e.At, &e.Kind, &e.Message, &e.Actor)
	})
	if events == nil {
		events = []IncidentEvent{}
	}
	return inc, events, err
}

// lockIncident loads an incident FOR UPDATE inside tx, scoped to a workspace,
// plus the check target for notifications.
func lockIncident(ctx context.Context, tx pgx.Tx, workspaceID, id int64) (Incident, string, error) {
	var (
		inc    Incident
		target *string
	)
	err := tx.QueryRow(ctx, `SELECT i.id, i.check_id, i.check_name, i.title, i.cause, i.status, i.opened_at, c.target
		FROM incidents i LEFT JOIN checks c ON c.id = i.check_id
		WHERE i.id = $1 AND i.workspace_id = $2
		FOR UPDATE OF i`, id, workspaceID).
		Scan(&inc.ID, &inc.CheckID, &inc.CheckName, &inc.Title, &inc.Cause, &inc.Status, &inc.OpenedAt, &target)
	return inc, deref(target), notFound(err)
}

// AcknowledgeIncident moves an open incident to acknowledged.
func (s *Store) AcknowledgeIncident(ctx context.Context, workspaceID, id int64, actor User) (Incident, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		inc, target, err := lockIncident(ctx, tx, workspaceID, id)
		if err != nil {
			return err
		}
		if inc.Status != "open" {
			return ErrConflict
		}
		if _, err := tx.Exec(ctx, `UPDATE incidents SET status = 'acknowledged', acknowledged_at = now(), acknowledged_by = $2
			WHERE id = $1`, id, actor.ID); err != nil {
			return err
		}
		if err := addEvent(ctx, tx, id, "acknowledged", "Acknowledged by "+actor.Name+".", &actor.ID); err != nil {
			return err
		}
		info := notify.IncidentInfo{ID: id, Title: inc.Title, CheckName: inc.CheckName, Target: target, Cause: inc.Cause, OpenedAt: inc.OpenedAt}
		return s.enqueue(ctx, tx, workspaceID, &id, notify.BuildIncidentMessage("acknowledged", info,
			actor.Name+" is investigating.", actor.Name, s.appURL))
	})
	if err != nil {
		return Incident{}, err
	}
	inc, _, err := s.GetIncident(ctx, workspaceID, id)
	return inc, err
}

// ResolveIncident manually resolves an active incident. The check's streak
// counters are reset so that a still-failing target must trip the full
// threshold again before a new incident opens.
func (s *Store) ResolveIncident(ctx context.Context, workspaceID, id int64, actor User, note string) (Incident, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		inc, target, err := lockIncident(ctx, tx, workspaceID, id)
		if err != nil {
			return err
		}
		if inc.Status == "resolved" {
			return ErrConflict
		}
		if _, err := tx.Exec(ctx, `UPDATE incidents SET status = 'resolved', resolved_at = now(), resolved_by = $2
			WHERE id = $1`, id, actor.ID); err != nil {
			return err
		}
		if inc.CheckID != nil {
			if _, err := tx.Exec(ctx, `UPDATE checks SET consecutive_failures = 0, consecutive_slow = 0,
				consecutive_healthy = 0, consecutive_reachable = 0 WHERE id = $1`, *inc.CheckID); err != nil {
				return err
			}
		}
		msg := "Resolved by " + actor.Name + "."
		if note != "" {
			msg += " " + note
		}
		if err := addEvent(ctx, tx, id, "resolved", msg, &actor.ID); err != nil {
			return err
		}
		info := notify.IncidentInfo{ID: id, Title: inc.Title, CheckName: inc.CheckName, Target: target, Cause: inc.Cause, OpenedAt: inc.OpenedAt}
		return s.enqueue(ctx, tx, workspaceID, &id, notify.BuildIncidentMessage("resolved", info, msg, actor.Name, s.appURL))
	})
	if err != nil {
		return Incident{}, err
	}
	inc, _, err := s.GetIncident(ctx, workspaceID, id)
	return inc, err
}

// AddIncidentNote appends an operator note to the timeline.
func (s *Store) AddIncidentNote(ctx context.Context, workspaceID, id int64, actor User, message string) (IncidentEvent, error) {
	var ev IncidentEvent
	err := s.pool.QueryRow(ctx, `INSERT INTO incident_events (incident_id, kind, message, actor_user_id)
		SELECT i.id, 'note', $3, $4 FROM incidents i WHERE i.id = $1 AND i.workspace_id = $2
		RETURNING id, at, kind, message`, id, workspaceID, message, actor.ID).
		Scan(&ev.ID, &ev.At, &ev.Kind, &ev.Message)
	if errors.Is(err, pgx.ErrNoRows) {
		return ev, ErrNotFound
	}
	ev.Actor = &actor.Name
	return ev, err
}

// PublicIncident is the reduced incident shape exposed on status pages.
type PublicIncident struct {
	ID         int64      `json:"id"`
	CheckID    int64      `json:"-"`
	CheckName  string     `json:"checkName"`
	Title      string     `json:"title"`
	Cause      string     `json:"cause"`
	Status     string     `json:"status"`
	OpenedAt   time.Time  `json:"openedAt"`
	ResolvedAt *time.Time `json:"resolvedAt"`
}

// PublicIncidents returns active incidents plus those resolved since
// `since`, limited to checks that are shown on the status page.
func (s *Store) PublicIncidents(ctx context.Context, workspaceID int64, since time.Time) ([]PublicIncident, error) {
	rows, err := s.pool.Query(ctx, `SELECT i.id, i.check_id, i.check_name, i.title, i.cause, i.status, i.opened_at, i.resolved_at
		FROM incidents i JOIN checks c ON c.id = i.check_id
		WHERE i.workspace_id = $1 AND c.is_public
			AND (i.status <> 'resolved' OR i.resolved_at >= $2)
		ORDER BY (i.status = 'resolved'), i.opened_at DESC
		LIMIT 50`, workspaceID, since)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PublicIncident, error) {
		var p PublicIncident
		return p, row.Scan(&p.ID, &p.CheckID, &p.CheckName, &p.Title, &p.Cause, &p.Status, &p.OpenedAt, &p.ResolvedAt)
	})
	if out == nil {
		out = []PublicIncident{}
	}
	return out, err
}
