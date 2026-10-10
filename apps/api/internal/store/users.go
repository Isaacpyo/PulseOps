package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const userColumns = `u.id, u.workspace_id, u.email, u.name, u.password_hash, u.created_at`

func (u *User) scanTargets() []any {
	return []any{&u.ID, &u.WorkspaceID, &u.Email, &u.Name, &u.PasswordHash, &u.CreatedAt}
}

const workspaceColumns = `w.id, w.name, w.slug, w.slack_webhook_url, w.is_demo, w.created_at`

func (w *Workspace) scanTargets() []any {
	return []any{&w.ID, &w.Name, &w.Slug, &w.SlackWebhookURL, &w.IsDemo, &w.CreatedAt}
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify converts a workspace name into a URL-safe slug.
func Slugify(name string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	if s == "" {
		s = "workspace"
	}
	return s
}

func randomSuffix() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Register creates a workspace and its first user in one transaction.
func (s *Store) Register(ctx context.Context, workspaceName, email, name, passwordHash string) (User, Workspace, error) {
	var (
		u User
		w Workspace
	)
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		base := Slugify(workspaceName)
		slug := base
		for attempt := 0; ; attempt++ {
			err := tx.QueryRow(ctx, `INSERT INTO workspaces (name, slug) VALUES ($1, $2)
				ON CONFLICT (slug) DO NOTHING
				RETURNING id, name, slug, slack_webhook_url, is_demo, created_at`,
				workspaceName, slug).Scan(w.scanTargets()...)
			if err == nil {
				break
			}
			if !errors.Is(err, pgx.ErrNoRows) || attempt >= 5 {
				return err
			}
			slug = base + "-" + randomSuffix()
		}
		err := tx.QueryRow(ctx, `INSERT INTO users (workspace_id, email, name, password_hash)
			VALUES ($1, $2, $3, $4)
			RETURNING id, workspace_id, email, name, password_hash, created_at`,
			w.ID, email, name, passwordHash).Scan(u.scanTargets()...)
		if isUniqueViolation(err, "users_email_lower_idx") {
			return ErrEmailTaken
		}
		return err
	})
	return u, w, err
}

// UserByEmail looks up a user case-insensitively.
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users u WHERE lower(u.email) = lower($1)`, email).
		Scan(u.scanTargets()...)
	return u, notFound(err)
}

// CreateSession stores a session token hash.
func (s *Store) CreateSession(ctx context.Context, tokenHash []byte, userID int64, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		tokenHash, userID, expiresAt)
	return err
}

// SessionUser resolves a non-expired session to its user and workspace.
func (s *Store) SessionUser(ctx context.Context, tokenHash []byte) (User, Workspace, error) {
	var (
		u User
		w Workspace
	)
	err := s.pool.QueryRow(ctx, `SELECT `+userColumns+`, `+workspaceColumns+`
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		JOIN workspaces w ON w.id = u.workspace_id
		WHERE s.token_hash = $1 AND s.expires_at > now()`, tokenHash).
		Scan(append(u.scanTargets(), w.scanTargets()...)...)
	return u, w, notFound(err)
}

// DeleteSession revokes a session.
func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

// DeleteExpiredSessions removes stale sessions.
func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= now()`)
	return tag.RowsAffected(), err
}

// WorkspaceBySlug finds a workspace by its public slug.
func (s *Store) WorkspaceBySlug(ctx context.Context, slug string) (Workspace, error) {
	var w Workspace
	err := s.pool.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspaces w WHERE w.slug = $1`, slug).
		Scan(w.scanTargets()...)
	return w, notFound(err)
}

// Workspace loads a workspace by ID.
func (s *Store) Workspace(ctx context.Context, id int64) (Workspace, error) {
	var w Workspace
	err := s.pool.QueryRow(ctx, `SELECT `+workspaceColumns+` FROM workspaces w WHERE w.id = $1`, id).
		Scan(w.scanTargets()...)
	return w, notFound(err)
}

// UpdateWorkspaceSettings updates the name and Slack webhook. A nil webhook
// leaves the stored value unchanged; an empty string clears it.
func (s *Store) UpdateWorkspaceSettings(ctx context.Context, id int64, name string, webhook *string) (Workspace, error) {
	var w Workspace
	err := s.pool.QueryRow(ctx, `UPDATE workspaces w SET
			name = $2,
			slack_webhook_url = CASE WHEN $3::boolean THEN NULLIF($4, '') ELSE w.slack_webhook_url END
		WHERE w.id = $1
		RETURNING `+workspaceColumns,
		id, name, webhook != nil, deref(webhook)).Scan(w.scanTargets()...)
	return w, notFound(err)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
