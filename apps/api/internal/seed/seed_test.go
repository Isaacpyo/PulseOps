package seed

import (
	"context"
	"testing"

	"github.com/isaacpyo/pulseops/apps/api/internal/testdb"
)

func TestEnsureDemoIsIdempotentAndResettable(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	if err := EnsureDemo(ctx, pool, false, "https://app.test"); err != nil {
		t.Fatal(err)
	}
	var checks, incidents, active, daily int
	q := func() {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM checks),
			(SELECT count(*) FROM incidents),
			(SELECT count(*) FROM incidents WHERE status <> 'resolved'),
			(SELECT count(*) FROM check_daily)`).Scan(&checks, &incidents, &active, &daily); err != nil {
			t.Fatal(err)
		}
	}
	q()
	if checks != len(demoChecks) || active != 1 || daily != len(demoChecks)*90 {
		t.Fatalf("seed counts: checks=%d active=%d daily=%d", checks, active, daily)
	}
	firstIncidents := incidents

	// Second boot without reset leaves user changes alone.
	if _, err := pool.Exec(ctx, `UPDATE incidents SET status = 'resolved', resolved_at = now() WHERE status <> 'resolved'`); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDemo(ctx, pool, false, ""); err != nil {
		t.Fatal(err)
	}
	q()
	if active != 0 || incidents != firstIncidents {
		t.Fatalf("non-reset boot modified data: active=%d incidents=%d", active, incidents)
	}

	// Sessions survive a reset so signed-in visitors are not logged out.
	if _, err := pool.Exec(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at)
		SELECT '\x01'::bytea, id, now() + interval '1 hour' FROM users WHERE email = $1`, DemoEmail); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDemo(ctx, pool, true, ""); err != nil {
		t.Fatal(err)
	}
	q()
	if checks != len(demoChecks) || active != 1 || incidents != firstIncidents {
		t.Fatalf("reset did not restore seed: checks=%d active=%d incidents=%d", checks, active, incidents)
	}
	var sessions, users int
	_ = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM sessions), (SELECT count(*) FROM users)`).Scan(&sessions, &users)
	if sessions != 1 || users != 1 {
		t.Fatalf("sessions=%d users=%d after reset", sessions, users)
	}
}
