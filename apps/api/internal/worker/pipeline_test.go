package worker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/isaacpyo/pulseops/apps/api/internal/alert"
	"github.com/isaacpyo/pulseops/apps/api/internal/metrics"
	"github.com/isaacpyo/pulseops/apps/api/internal/notify"
	"github.com/isaacpyo/pulseops/apps/api/internal/probe"
	"github.com/isaacpyo/pulseops/apps/api/internal/store"
	"github.com/isaacpyo/pulseops/apps/api/internal/testdb"
)

type fixture struct {
	t     *testing.T
	ctx   context.Context
	store *store.Store
	sched *Scheduler
	disp  *Dispatcher
	mock  *notify.MockProvider
	user  store.User
	ws    store.Workspace
	// target behaviour: 0 healthy, 1 failing (500), 2 slow
	mode atomic.Int32
	srv  *httptest.Server
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := testdb.New(t)
	f := &fixture{t: t, ctx: context.Background(), store: store.New(pool, "https://app.test"), mock: &notify.MockProvider{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch f.mode.Load() {
		case 1:
			http.Error(w, "down", http.StatusInternalServerError)
		case 2:
			time.Sleep(60 * time.Millisecond)
		}
	}))
	t.Cleanup(f.srv.Close)

	m := metrics.New()
	f.sched = &Scheduler{Store: f.store, Prober: probe.New(true), Metrics: m, Concurrency: 2}
	f.disp = &Dispatcher{Store: f.store, Router: notify.NewRouter(notify.ModeMock, "", notify.NewSlackProvider(), f.mock), Metrics: m}

	var err error
	f.user, f.ws, err = f.store.Register(f.ctx, "Ops Team", "ops@example.com", "Ada Ops", "x")
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) createCheck(latency *int) store.Check {
	f.t.Helper()
	c, err := f.store.CreateCheck(f.ctx, f.ws.ID, store.CheckInput{
		Name: "API", Kind: "http", Target: f.srv.URL, IntervalSeconds: 60, TimeoutMs: 2000,
		LatencyThresholdMs: latency, FailureThreshold: 2, RecoveryThreshold: 2, Enabled: true, IsPublic: true,
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return c
}

func (f *fixture) probe(c store.Check) store.ProbeOutcome {
	f.t.Helper()
	fresh, err := f.store.GetCheck(f.ctx, f.ws.ID, c.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	out, err := f.sched.Execute(f.ctx, fresh)
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *fixture) activeIncident(checkID int64) *store.Incident {
	f.t.Helper()
	list, err := f.store.ListIncidents(f.ctx, f.ws.ID, store.IncidentFilter{Status: "active", CheckID: checkID})
	if err != nil {
		f.t.Fatal(err)
	}
	if len(list) == 0 {
		return nil
	}
	return &list[0]
}

func eventKinds(events []store.IncidentEvent) string {
	kinds := make([]string, len(events))
	for i, e := range events {
		kinds[i] = e.Kind
	}
	return strings.Join(kinds, ",")
}

func TestOutagePipelineOpensNotifiesAndAutoResolves(t *testing.T) {
	f := newFixture(t)
	c := f.createCheck(nil)

	if out := f.probe(c); out.Outcome != alert.OutcomeUp {
		t.Fatalf("healthy probe outcome = %s", out.Outcome)
	}

	f.mode.Store(1)
	if out := f.probe(c); out.Decision.Action != alert.ActionNone {
		t.Fatalf("first failure should not alert: %+v", out.Decision)
	}
	out := f.probe(c)
	if out.Decision.Action != alert.ActionOpen || out.IncidentID == 0 {
		t.Fatalf("second failure should open an incident: %+v", out)
	}
	inc := f.activeIncident(c.ID)
	if inc == nil || inc.Cause != "down" || inc.Status != "open" || inc.Title != "API is down" {
		t.Fatalf("unexpected incident %+v", inc)
	}

	// The outbox row is delivered by the dispatcher and noted on the timeline.
	if n, err := f.disp.DispatchOnce(f.ctx); err != nil || n != 1 {
		t.Fatalf("dispatch = %d, %v", n, err)
	}
	sent := f.mock.Sent()
	if len(sent) != 1 || sent[0].Event != "opened" || !strings.Contains(sent[0].Text, "unexpected HTTP 500") {
		t.Fatalf("mock notifications = %+v", sent)
	}
	if sent[0].URL != "https://app.test/app/incidents/"+itoa(inc.ID) {
		t.Fatalf("notification link = %s", sent[0].URL)
	}

	// Recovery needs two consecutive good probes.
	f.mode.Store(0)
	if out := f.probe(c); out.Decision.Action != alert.ActionNone {
		t.Fatalf("one good probe must not resolve: %+v", out.Decision)
	}
	if out := f.probe(c); out.Decision.Action != alert.ActionResolve {
		t.Fatalf("expected auto-resolve, got %+v", out.Decision)
	}
	if f.activeIncident(c.ID) != nil {
		t.Fatal("incident still active after recovery")
	}
	if _, err := f.disp.DispatchOnce(f.ctx); err != nil {
		t.Fatal(err)
	}
	_, events, err := f.store.GetIncident(f.ctx, f.ws.ID, inc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := eventKinds(events); got != "opened,notified,auto_resolved,notified" {
		t.Fatalf("timeline = %s", got)
	}

	// Results and the daily rollup were recorded for every probe.
	results, err := f.store.ListResults(f.ctx, c.ID, time.Now().Add(-time.Hour), 100)
	if err != nil || len(results) != 5 {
		t.Fatalf("results = %d, %v", len(results), err)
	}
	rollups, err := f.store.DailyRollups(f.ctx, []int64{c.ID}, time.Now().AddDate(0, 0, -1))
	if err != nil || len(rollups[c.ID]) != 1 {
		t.Fatalf("rollups = %+v, %v", rollups, err)
	}
	if r := rollups[c.ID][0]; r.Total != 5 || r.Up != 3 || r.Incidents != 1 {
		t.Fatalf("rollup = %+v", r)
	}
}

func TestLatencyRuleOpensDegradedThenEscalates(t *testing.T) {
	f := newFixture(t)
	threshold := 30
	c := f.createCheck(&threshold)

	f.mode.Store(2)
	f.probe(c)
	out := f.probe(c)
	if out.Decision.Action != alert.ActionOpen || out.Decision.Cause != alert.CauseDegraded {
		t.Fatalf("expected degraded incident, got %+v", out.Decision)
	}
	if inc := f.activeIncident(c.ID); inc == nil || inc.Title != "API is responding slowly" {
		t.Fatalf("incident = %+v", inc)
	}

	f.mode.Store(1)
	f.probe(c)
	out = f.probe(c)
	if out.Decision.Action != alert.ActionEscalate {
		t.Fatalf("expected escalation, got %+v", out.Decision)
	}
	inc := f.activeIncident(c.ID)
	if inc == nil || inc.Cause != "down" || inc.Title != "API is down" {
		t.Fatalf("incident after escalation = %+v", inc)
	}
	_, events, _ := f.store.GetIncident(f.ctx, f.ws.ID, inc.ID)
	if got := eventKinds(events); got != "opened,escalated" {
		t.Fatalf("timeline = %s", got)
	}
}

func TestManualAcknowledgeAndResolve(t *testing.T) {
	f := newFixture(t)
	c := f.createCheck(nil)
	f.mode.Store(1)
	f.probe(c)
	f.probe(c)
	inc := f.activeIncident(c.ID)

	acked, err := f.store.AcknowledgeIncident(f.ctx, f.ws.ID, inc.ID, f.user)
	if err != nil || acked.Status != "acknowledged" || acked.AcknowledgedBy == nil || *acked.AcknowledgedBy != "Ada Ops" {
		t.Fatalf("ack = %+v, %v", acked, err)
	}
	if _, err := f.store.AcknowledgeIncident(f.ctx, f.ws.ID, inc.ID, f.user); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("double ack err = %v, want conflict", err)
	}
	if _, err := f.store.AddIncidentNote(f.ctx, f.ws.ID, inc.ID, f.user, "Looking at the load balancer"); err != nil {
		t.Fatal(err)
	}
	resolved, err := f.store.ResolveIncident(f.ctx, f.ws.ID, inc.ID, f.user, "Restarted pods.")
	if err != nil || resolved.Status != "resolved" || resolved.ResolvedBy == nil {
		t.Fatalf("resolve = %+v, %v", resolved, err)
	}
	if _, err := f.store.ResolveIncident(f.ctx, f.ws.ID, inc.ID, f.user, ""); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("double resolve err = %v", err)
	}

	// Streaks were reset: a still-failing target needs the full threshold again.
	if out := f.probe(c); out.Decision.Action != alert.ActionNone {
		t.Fatalf("re-opened too early: %+v", out.Decision)
	}
	if out := f.probe(c); out.Decision.Action != alert.ActionOpen {
		t.Fatalf("expected a new incident: %+v", out.Decision)
	}

	if _, err := f.disp.DispatchOnce(f.ctx); err != nil {
		t.Fatal(err)
	}
	events := map[string]int{}
	for _, m := range f.mock.Sent() {
		events[m.Event]++
	}
	if events["opened"] != 2 || events["acknowledged"] != 1 || events["resolved"] != 1 {
		t.Fatalf("notifications by event = %v", events)
	}
}

func TestOtherWorkspaceCannotTouchIncident(t *testing.T) {
	f := newFixture(t)
	c := f.createCheck(nil)
	f.mode.Store(1)
	f.probe(c)
	f.probe(c)
	inc := f.activeIncident(c.ID)

	intruder, ws2, err := f.store.Register(f.ctx, "Other", "other@example.com", "Mallory", "x")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AcknowledgeIncident(f.ctx, ws2.ID, inc.ID, intruder); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant ack err = %v", err)
	}
	if _, _, err := f.store.GetIncident(f.ctx, ws2.ID, inc.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant read err = %v", err)
	}
	if _, err := f.store.GetCheck(f.ctx, ws2.ID, c.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-tenant check read err = %v", err)
	}
}

func TestChaosDrillSimulatesOutageWithoutNetwork(t *testing.T) {
	f := newFixture(t)
	c := f.createCheck(nil)
	until := time.Now().Add(time.Minute)
	if _, err := f.store.SetDrill(f.ctx, f.ws.ID, c.ID, &until); err != nil {
		t.Fatal(err)
	}
	f.probe(c)
	out := f.probe(c)
	if out.Decision.Action != alert.ActionOpen {
		t.Fatalf("drill should open an incident: %+v", out.Decision)
	}
	if out.Check.LastError == nil || *out.Check.LastError != DrillError {
		t.Fatalf("last error = %v", out.Check.LastError)
	}
}

func TestClaimDueChecksLeasesEachCheckOnce(t *testing.T) {
	f := newFixture(t)
	c := f.createCheck(nil)

	first, err := f.store.ClaimDueChecks(f.ctx, 10)
	if err != nil || len(first) != 1 || first[0].ID != c.ID {
		t.Fatalf("first claim = %+v, %v", first, err)
	}
	if !first[0].NextRunAt.After(time.Now().Add(50 * time.Second)) {
		t.Fatalf("next_run_at not pushed forward: %v", first[0].NextRunAt)
	}
	second, err := f.store.ClaimDueChecks(f.ctx, 10)
	if err != nil || len(second) != 0 {
		t.Fatalf("check claimed twice: %+v", second)
	}
	if _, err := f.store.TriggerCheck(f.ctx, f.ws.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	third, _ := f.store.ClaimDueChecks(f.ctx, 10)
	if len(third) != 1 {
		t.Fatal("run-now should make the check due again")
	}
}

func TestDispatcherRetriesThenFails(t *testing.T) {
	f := newFixture(t)
	f.mock.FailWith = errors.New("slack is down")
	if err := f.store.EnqueueNotification(f.ctx, f.ws.ID, notify.TestMessage("Ops", "Ada", "https://app.test")); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	f.disp.Now = func() time.Time { return now }
	f.disp.MaxAttempts = 2

	if _, err := f.disp.DispatchOnce(f.ctx); err != nil {
		t.Fatal(err)
	}
	list, _ := f.store.ListNotifications(f.ctx, f.ws.ID, 10)
	if list[0].Status != "pending" || list[0].Attempts != 1 || list[0].LastError == nil {
		t.Fatalf("after first failure: %+v", list[0])
	}
	// Make it due again and fail the final attempt.
	if _, err := f.store.Pool().Exec(f.ctx, `UPDATE notifications SET next_attempt_at = now()`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.disp.DispatchOnce(f.ctx); err != nil {
		t.Fatal(err)
	}
	list, _ = f.store.ListNotifications(f.ctx, f.ws.ID, 10)
	if list[0].Status != "failed" || list[0].Attempts != 2 {
		t.Fatalf("after final failure: %+v", list[0])
	}
}

func TestBackoff(t *testing.T) {
	want := []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute}
	for i, w := range want {
		if got := Backoff(i + 1); got != w {
			t.Fatalf("Backoff(%d) = %v, want %v", i+1, got, w)
		}
	}
	if Backoff(50) != 10*time.Minute {
		t.Fatal("backoff must cap at 10m")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
