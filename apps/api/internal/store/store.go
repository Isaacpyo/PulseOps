// Package store contains all Postgres access for PulseOps. Every query that
// touches tenant data is scoped by workspace ID.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrNotFound means the row does not exist or belongs to another workspace.
	ErrNotFound = errors.New("not found")
	// ErrConflict means the operation is invalid for the row's current state.
	ErrConflict = errors.New("conflict")
	// ErrEmailTaken is returned when registering an existing email.
	ErrEmailTaken = errors.New("email already registered")
)

// Store wraps the connection pool.
type Store struct {
	pool   *pgxpool.Pool
	appURL string
	now    func() time.Time
}

// New returns a Store. appURL is the web console base URL used for links in
// notification payloads.
func New(pool *pgxpool.Pool, appURL string) *Store {
	return &Store{pool: pool, appURL: appURL, now: time.Now}
}

// Pool exposes the underlying pool (seeding, tests).
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Ping checks database connectivity.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && (constraint == "" || pgErr.ConstraintName == constraint)
}
