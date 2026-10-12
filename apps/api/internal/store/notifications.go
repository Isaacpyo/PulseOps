package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/isaacpyo/pulseops/apps/api/internal/notify"
)

// PendingNotification is a claimed outbox row ready for delivery.
type PendingNotification struct {
	ID               int64
	WorkspaceID      int64
	IncidentID       *int64
	Event            string
	Attempts         int
	Message          notify.Message
	WorkspaceWebhook string
}

// leaseDuration is how long a claimed notification is hidden from other
// dispatchers; if a process dies mid-send the row is retried afterwards.
const leaseDuration = 60 * time.Second

// ClaimNotifications leases up to limit due outbox rows.
func (s *Store) ClaimNotifications(ctx context.Context, limit int) ([]PendingNotification, error) {
	rows, err := s.pool.Query(ctx, `UPDATE notifications n
		SET attempts = n.attempts + 1, next_attempt_at = now() + make_interval(secs => $2)
		FROM workspaces w
		WHERE w.id = n.workspace_id AND n.id IN (
			SELECT id FROM notifications
			WHERE status = 'pending' AND next_attempt_at <= now()
			ORDER BY id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING n.id, n.workspace_id, n.incident_id, n.event, n.attempts, n.payload, COALESCE(w.slack_webhook_url, '')`,
		limit, leaseDuration.Seconds())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (PendingNotification, error) {
		var (
			p   PendingNotification
			raw []byte
		)
		if err := row.Scan(&p.ID, &p.WorkspaceID, &p.IncidentID, &p.Event, &p.Attempts, &raw, &p.WorkspaceWebhook); err != nil {
			return p, err
		}
		return p, json.Unmarshal(raw, &p.Message)
	})
}

// MarkNotificationSent records a successful delivery and adds a timeline
// entry to the related incident.
func (s *Store) MarkNotificationSent(ctx context.Context, n PendingNotification, provider, timelineMsg string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE notifications SET status = 'sent', provider = $2, sent_at = now(), last_error = NULL
			WHERE id = $1`, n.ID, provider); err != nil {
			return err
		}
		if n.IncidentID != nil {
			return addEvent(ctx, tx, *n.IncidentID, "notified", timelineMsg, nil)
		}
		return nil
	})
}

// MarkNotificationFailed records a failed attempt. When retryAt is nil the
// notification is marked permanently failed and the incident timeline notes it.
func (s *Store) MarkNotificationFailed(ctx context.Context, n PendingNotification, provider, errMsg string, retryAt *time.Time) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if retryAt != nil {
			_, err := tx.Exec(ctx, `UPDATE notifications SET provider = $2, last_error = $3, next_attempt_at = $4
				WHERE id = $1`, n.ID, provider, errMsg, *retryAt)
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE notifications SET status = 'failed', provider = $2, last_error = $3
			WHERE id = $1`, n.ID, provider, errMsg); err != nil {
			return err
		}
		if n.IncidentID != nil {
			return addEvent(ctx, tx, *n.IncidentID, "notify_failed",
				"Notification \""+n.Event+"\" could not be delivered via "+provider+": "+errMsg, nil)
		}
		return nil
	})
}

// EnqueueNotification adds an ad-hoc notification (e.g. a settings test).
func (s *Store) EnqueueNotification(ctx context.Context, workspaceID int64, msg notify.Message) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return s.enqueue(ctx, tx, workspaceID, nil, msg)
	})
}

// ListNotifications returns the workspace's notification log, newest first.
func (s *Store) ListNotifications(ctx context.Context, workspaceID int64, limit int) ([]Notification, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, incident_id, event, provider, status, attempts, payload,
			last_error, created_at, sent_at
		FROM notifications WHERE workspace_id = $1
		ORDER BY created_at DESC, id DESC LIMIT $2`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Notification, error) {
		var n Notification
		return n, row.Scan(&n.ID, &n.IncidentID, &n.Event, &n.Provider, &n.Status, &n.Attempts, &n.Payload,
			&n.LastError, &n.CreatedAt, &n.SentAt)
	})
	if out == nil {
		out = []Notification{}
	}
	return out, err
}
