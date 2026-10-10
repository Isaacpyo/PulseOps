package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// PublicCheck is a check as exposed on a public status page. Targets are
// deliberately omitted.
type PublicCheck struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	Kind          string     `json:"kind"`
	Status        string     `json:"status"`
	Enabled       bool       `json:"enabled"`
	LastCheckedAt *time.Time `json:"lastCheckedAt"`
}

// PublicChecks returns the workspace's public checks.
func (s *Store) PublicChecks(ctx context.Context, workspaceID int64) ([]PublicCheck, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, kind, status, enabled, last_checked_at
		FROM checks WHERE workspace_id = $1 AND is_public
		ORDER BY lower(name), id`, workspaceID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PublicCheck, error) {
		var c PublicCheck
		return c, row.Scan(&c.ID, &c.Name, &c.Kind, &c.Status, &c.Enabled, &c.LastCheckedAt)
	})
	if out == nil {
		out = []PublicCheck{}
	}
	return out, err
}
