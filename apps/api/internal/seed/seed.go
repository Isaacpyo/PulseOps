// Package seed creates the shared demo workspace: real, probe-able checks
// plus 90 days of synthetic history so the status page and charts have
// something to show on first visit.
package seed

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	mrand "math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/isaacpyo/pulseops/apps/api/internal/auth"
	"github.com/isaacpyo/pulseops/apps/api/internal/notify"
)

// Demo account identifiers.
const (
	DemoEmail         = "demo@pulseops.dev"
	DemoSlug          = "demo"
	DemoWorkspaceName = "Acme Cloud"
	DemoUserName      = "Demo Operator"
)

const seedLockID = 7_310_442_902

type pastIncident struct {
	daysAgo  int
	hour     int
	minutes  int // duration
	cause    string
	ackAfter int // minutes; 0 = never acknowledged
	note     string
	manual   bool // resolved by an operator rather than automatically
}

type demoCheck struct {
	name      string
	kind      string
	target    string
	interval  int
	timeout   int
	latency   int // latency threshold
	baseMs    int // typical latency
	incidents []pastIncident
}

var demoChecks = []demoCheck{
	{"Marketing website", "http", "https://example.com/", 60, 10000, 1500, 95, []pastIncident{
		{63, 14, 22, "down", 4, "Origin returned 502s after a bad deploy; rolled back.", true},
		{12, 9, 41, "degraded", 6, "CDN cache purge caused cold-cache latency.", false},
	}},
	{"GitHub status API", "http", "https://www.githubstatus.com/api/v2/status.json", 60, 10000, 1200, 170, []pastIncident{
		{41, 18, 9, "down", 0, "", false},
	}},
	{"Cloudflare edge", "http", "https://www.cloudflare.com/cdn-cgi/trace", 60, 8000, 800, 48, []pastIncident{
		{27, 3, 15, "degraded", 5, "Regional routing issue upstream; no action needed.", false},
	}},
	{"Public DNS resolver", "tcp", "1.1.1.1:53", 60, 5000, 300, 11, []pastIncident{
		{75, 22, 4, "down", 0, "", false},
	}},
	{"Google DNS", "tcp", "8.8.8.8:53", 60, 5000, 300, 14, nil},
	{"Wikipedia", "http", "https://www.wikipedia.org/", 120, 10000, 1500, 130, []pastIncident{
		{5, 11, 31, "down", 3, "TLS certificate on the origin expired; renewed and redeployed.", true},
		{2, 16, 12, "degraded", 0, "", false},
	}},
}

// activeIncidentCheck has a live, open incident when the demo is seeded so
// visitors (and the e2e test) can acknowledge and resolve it.
const activeIncidentCheck = "GitHub status API"

// EnsureDemo seeds the demo workspace if it is missing. With reset=true it
// wipes the demo workspace's checks, incidents and notifications and
// re-seeds (users and sessions are kept so logged-in visitors stay in).
func EnsureDemo(ctx context.Context, pool *pgxpool.Pool, reset bool, appURL string) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, seedLockID); err != nil {
			return err
		}
		var wsID int64
		err := tx.QueryRow(ctx, `SELECT id FROM workspaces WHERE slug = $1`, DemoSlug).Scan(&wsID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if err := tx.QueryRow(ctx, `INSERT INTO workspaces (name, slug, is_demo) VALUES ($1, $2, true) RETURNING id`,
				DemoWorkspaceName, DemoSlug).Scan(&wsID); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM checks WHERE workspace_id = $1`, wsID).Scan(&n); err != nil {
				return err
			}
			if n > 0 && !reset {
				return nil
			}
			for _, q := range []string{
				`DELETE FROM notifications WHERE workspace_id = $1`,
				`DELETE FROM incidents WHERE workspace_id = $1`,
				`DELETE FROM checks WHERE workspace_id = $1`,
				`UPDATE workspaces SET name = '` + DemoWorkspaceName + `', slack_webhook_url = NULL, is_demo = true WHERE id = $1`,
			} {
				if _, err := tx.Exec(ctx, q, wsID); err != nil {
					return err
				}
			}
		}

		userID, err := ensureDemoUser(ctx, tx, wsID)
		if err != nil {
			return err
		}
		s := &seeder{tx: tx, wsID: wsID, userID: userID, now: time.Now().UTC(), appURL: appURL}
		for i, dc := range demoChecks {
			if err := s.seedCheck(ctx, i, dc); err != nil {
				return fmt.Errorf("seed %s: %w", dc.name, err)
			}
		}
		slog.Info("demo workspace seeded", "workspace", wsID, "checks", len(demoChecks), "reset", reset)
		return nil
	})
}

func ensureDemoUser(ctx context.Context, tx pgx.Tx, wsID int64) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `SELECT id FROM users WHERE lower(email) = $1`, DemoEmail).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	// The demo account is entered via POST /api/auth/demo only; its password
	// is random and never disclosed.
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return 0, err
	}
	hash, err := auth.HashPassword(hex.EncodeToString(b))
	if err != nil {
		return 0, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO users (workspace_id, email, name, password_hash) VALUES ($1, $2, $3, $4) RETURNING id`,
		wsID, DemoEmail, DemoUserName, hash).Scan(&id)
	return id, err
}

type seeder struct {
	tx     pgx.Tx
	wsID   int64
	userID int64
	now    time.Time
	appURL string
}

func (s *seeder) seedCheck(ctx context.Context, idx int, dc demoCheck) error {
	// Deterministic PRNG so seeded history is reproducible; not security-sensitive.
	rng := mrand.New(mrand.NewPCG(42, uint64(idx)+1)) //nolint:gosec
	active := dc.name == activeIncidentCheck

	status := "up"
	failures, slow, healthy, reachable := 0, 0, 2, 2
	if active {
		status, failures, slow, healthy, reachable = "degraded", 0, 3, 0, 3
	}
	lastLatency := dc.baseMs
	if active {
		lastLatency = dc.latency + 640
	}
	lastChecked := s.now.Add(-time.Duration(rng.IntN(dc.interval)) * time.Second)

	var checkID int64
	if err := s.tx.QueryRow(ctx, `INSERT INTO checks (workspace_id, name, kind, target, interval_seconds, timeout_ms,
			latency_threshold_ms, failure_threshold, recovery_threshold, status, consecutive_failures,
			consecutive_slow, consecutive_healthy, consecutive_reachable, last_checked_at, last_latency_ms,
			next_run_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 3, 2, $8, $9, $10, $11, $12, $13, $14, $15, $16, $16)
		RETURNING id`,
		s.wsID, dc.name, dc.kind, dc.target, dc.interval, dc.timeout, dc.latency, status,
		failures, slow, healthy, reachable, lastChecked, lastLatency,
		s.now.Add(time.Duration(rng.IntN(20))*time.Second), s.now.AddDate(0, 0, -90),
	).Scan(&checkID); err != nil {
		return err
	}

	// Index past incidents by day for the rollups.
	incByDay := map[int][]pastIncident{}
	for _, pi := range dc.incidents {
		incByDay[pi.daysAgo] = append(incByDay[pi.daysAgo], pi)
	}

	today := time.Date(s.now.Year(), s.now.Month(), s.now.Day(), 0, 0, 0, 0, time.UTC)
	perDay := 86400 / dc.interval
	for daysAgo := 89; daysAgo >= 0; daysAgo-- {
		day := today.AddDate(0, 0, -daysAgo)
		total := perDay
		if daysAgo == 0 {
			total = max(1, int(s.now.Sub(today).Seconds())/dc.interval)
		}
		down, degraded := 0, 0
		// Occasional single-probe blips that stay below the alert threshold.
		switch r := rng.Float64(); {
		case r < 0.02:
			down += 2
		case r < 0.10:
			down++
		}
		for _, pi := range incByDay[daysAgo] {
			probes := max(1, pi.minutes*60/dc.interval)
			if pi.cause == "down" {
				down += probes
			} else {
				degraded += probes
			}
		}
		if active && daysAgo == 0 {
			degraded += 25 * 60 / dc.interval
		}
		down = min(down, total)
		up := total - down
		degraded = min(degraded, up)
		latencySum := int64(up) * int64(dc.baseMs*(90+rng.IntN(25))/100)
		if _, err := s.tx.Exec(ctx, `INSERT INTO check_daily (check_id, day, total, up, degraded, latency_sum_ms)
			VALUES ($1, $2, $3, $4, $5, $6)`, checkID, day.Format("2006-01-02"), total, up, degraded, latencySum); err != nil {
			return err
		}
	}

	if err := s.seedResults(ctx, rng, checkID, dc, active); err != nil {
		return err
	}
	for _, pi := range dc.incidents {
		if err := s.seedPastIncident(ctx, checkID, dc, pi); err != nil {
			return err
		}
	}
	if active {
		return s.seedActiveIncident(ctx, checkID, dc)
	}
	return nil
}

// seedResults writes 24h of raw results at 5-minute spacing for charts.
func (s *seeder) seedResults(ctx context.Context, rng *mrand.Rand, checkID int64, dc demoCheck, active bool) error {
	rows := make([][]any, 0, 288)
	for i := 288; i >= 1; i-- {
		at := s.now.Add(-time.Duration(i) * 5 * time.Minute)
		latency := dc.baseMs * (80 + rng.IntN(50)) / 100
		if rng.Float64() < 0.03 {
			latency *= 2 + rng.IntN(3)
		}
		outcome := "up"
		var errText *string
		var code *int
		if dc.kind == "http" {
			c := 200
			code = &c
		}
		if active && at.After(s.now.Add(-25*time.Minute)) {
			latency = dc.latency + 300 + rng.IntN(900)
		}
		if latency > dc.latency {
			outcome = "degraded"
		}
		if rng.Float64() < 0.004 {
			outcome, latency, code = "down", dc.timeout, nil
			e := "timed out after " + (time.Duration(dc.timeout) * time.Millisecond).String()
			errText = &e
		}
		rows = append(rows, []any{checkID, at, outcome, latency, code, errText})
	}
	_, err := s.tx.CopyFrom(ctx, pgx.Identifier{"check_results"},
		[]string{"check_id", "checked_at", "outcome", "latency_ms", "status_code", "error"}, pgx.CopyFromRows(rows))
	return err
}

func (s *seeder) event(ctx context.Context, incID int64, at time.Time, kind, msg string, actor *int64) error {
	_, err := s.tx.Exec(ctx, `INSERT INTO incident_events (incident_id, at, kind, message, actor_user_id) VALUES ($1, $2, $3, $4, $5)`,
		incID, at, kind, msg, actor)
	return err
}

func (s *seeder) notification(ctx context.Context, incID int64, at time.Time, event string, info notify.IncidentInfo, detail, actor string) error {
	payload, err := json.Marshal(notify.BuildIncidentMessage(event, info, detail, actor, s.appURL))
	if err != nil {
		return err
	}
	_, err = s.tx.Exec(ctx, `INSERT INTO notifications (workspace_id, incident_id, event, provider, status, attempts, payload, created_at, sent_at, next_attempt_at)
		VALUES ($1, $2, $3, 'mock', 'sent', 1, $4, $5, $5, $5)`, s.wsID, incID, event, payload, at)
	if err != nil {
		return err
	}
	return s.event(ctx, incID, at.Add(2*time.Second), "notified",
		fmt.Sprintf("Sent %q alert via the mock notifier (no Slack webhook configured).", event), nil)
}

func titleFor(name, cause string) string {
	if cause == "down" {
		return name + " is down"
	}
	return name + " is responding slowly"
}

func openDetail(dc demoCheck, cause string) string {
	if cause == "down" {
		return "Opened after 3 consecutive failed probes. Last error: timed out after " +
			(time.Duration(dc.timeout) * time.Millisecond).String() + "."
	}
	return fmt.Sprintf("Opened after 3 consecutive probes slower than %d ms (last: %d ms).", dc.latency, dc.latency+412)
}

func (s *seeder) seedPastIncident(ctx context.Context, checkID int64, dc demoCheck, pi pastIncident) error {
	day := time.Date(s.now.Year(), s.now.Month(), s.now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -pi.daysAgo)
	opened := day.Add(time.Duration(pi.hour)*time.Hour + 17*time.Minute)
	resolved := opened.Add(time.Duration(pi.minutes) * time.Minute)
	title := titleFor(dc.name, pi.cause)

	var (
		ackAt, resolvedBy *int64
		ackTime           *time.Time
	)
	if pi.ackAfter > 0 {
		t := opened.Add(time.Duration(pi.ackAfter) * time.Minute)
		ackTime, ackAt = &t, &s.userID
	}
	if pi.manual {
		resolvedBy = &s.userID
	}
	var incID int64
	if err := s.tx.QueryRow(ctx, `INSERT INTO incidents (workspace_id, check_id, check_name, title, cause, status,
			opened_at, acknowledged_at, acknowledged_by, resolved_at, resolved_by)
		VALUES ($1, $2, $3, $4, $5, 'resolved', $6, $7, $8, $9, $10) RETURNING id`,
		s.wsID, checkID, dc.name, title, pi.cause, opened, ackTime, ackAt, resolved, resolvedBy).Scan(&incID); err != nil {
		return err
	}
	info := notify.IncidentInfo{ID: incID, Title: title, CheckName: dc.name, Target: dc.target, Cause: pi.cause, OpenedAt: opened}
	detail := openDetail(dc, pi.cause)
	if err := s.event(ctx, incID, opened, "opened", detail, nil); err != nil {
		return err
	}
	if err := s.notification(ctx, incID, opened.Add(3*time.Second), "opened", info, detail, ""); err != nil {
		return err
	}
	if ackTime != nil {
		if err := s.event(ctx, incID, *ackTime, "acknowledged", "Acknowledged by "+DemoUserName+".", &s.userID); err != nil {
			return err
		}
		if err := s.notification(ctx, incID, ackTime.Add(3*time.Second), "acknowledged", info, DemoUserName+" is investigating.", DemoUserName); err != nil {
			return err
		}
	}
	if pi.note != "" {
		if err := s.event(ctx, incID, resolved.Add(-2*time.Minute), "note", pi.note, &s.userID); err != nil {
			return err
		}
	}
	var resolveMsg string
	kind := "auto_resolved"
	actor := ""
	if pi.manual {
		kind, actor = "resolved", DemoUserName
		resolveMsg = "Resolved by " + DemoUserName + "."
		if err := s.event(ctx, incID, resolved, kind, resolveMsg, &s.userID); err != nil {
			return err
		}
	} else {
		resolveMsg = "Recovered automatically after 2 consecutive successful probes."
		if pi.cause == "degraded" {
			resolveMsg = fmt.Sprintf("Latency back under %d ms for 2 consecutive probes. Resolved automatically.", dc.latency)
		}
		if err := s.event(ctx, incID, resolved, kind, resolveMsg, nil); err != nil {
			return err
		}
	}
	return s.notification(ctx, incID, resolved.Add(3*time.Second), "resolved", info, resolveMsg, actor)
}

func (s *seeder) seedActiveIncident(ctx context.Context, checkID int64, dc demoCheck) error {
	opened := s.now.Add(-22 * time.Minute)
	title := titleFor(dc.name, "degraded")
	var incID int64
	if err := s.tx.QueryRow(ctx, `INSERT INTO incidents (workspace_id, check_id, check_name, title, cause, status, opened_at)
		VALUES ($1, $2, $3, $4, 'degraded', 'open', $5) RETURNING id`,
		s.wsID, checkID, dc.name, title, opened).Scan(&incID); err != nil {
		return err
	}
	detail := openDetail(dc, "degraded")
	if err := s.event(ctx, incID, opened, "opened", detail, nil); err != nil {
		return err
	}
	info := notify.IncidentInfo{ID: incID, Title: title, CheckName: dc.name, Target: dc.target, Cause: "degraded", OpenedAt: opened}
	return s.notification(ctx, incID, opened.Add(3*time.Second), "opened", info, detail, "")
}
