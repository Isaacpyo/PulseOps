// Package db owns the Postgres connection pool and schema migrations.
package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrationLockID is an arbitrary constant for pg_advisory_lock so that
// concurrently booting instances apply migrations one at a time.
const migrationLockID = 7_310_442_901

// Connect opens a pgx pool and verifies connectivity, retrying while the
// database wakes up (Neon free-tier computes suspend when idle).
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	if cfg.MaxConns < 10 {
		cfg.MaxConns = 10
	}
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	backoff := 500 * time.Millisecond
	for attempt := 1; ; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = pool.Ping(pingCtx)
		cancel()
		if err == nil {
			return pool, nil
		}
		if attempt >= 8 || ctx.Err() != nil {
			pool.Close()
			return nil, fmt.Errorf("connect to postgres: %w", err)
		}
		slog.Warn("postgres not ready, retrying", "attempt", attempt, "err", err)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
		}
		backoff = min(backoff*2, 5*time.Second)
	}
}

// Migrate applies any embedded migrations that have not yet been recorded.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrationLockID) //nolint:errcheck

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	names, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)

	for _, name := range names {
		version := strings.TrimSuffix(strings.TrimPrefix(name, "migrations/"), ".sql")
		var exists bool
		if err := conn.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", version,
		).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		sql, err := migrationFS.ReadFile(name)
		if err != nil {
			return err
		}
		if err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version)
			return err
		}); err != nil {
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
		slog.Info("applied migration", "version", version)
	}
	return nil
}
