package alert

import "testing"

func down() Probe       { return Probe{OK: false, LatencyMs: 0} }
func fast() Probe       { return Probe{OK: true, LatencyMs: 50} }
func slow(ms int) Probe { return Probe{OK: true, LatencyMs: ms} }
func run(r Rule, s State, ps ...Probe) (State, []Decision) {
	var ds []Decision
	for _, p := range ps {
		var d Decision
		s, _, d = Evaluate(r, s, p)
		ds = append(ds, d)
	}
	return s, ds
}

func TestClassify(t *testing.T) {
	r := Rule{FailureThreshold: 3, LatencyThresholdMs: 500, RecoveryThreshold: 2}
	cases := []struct {
		name string
		rule Rule
		p    Probe
		want Outcome
	}{
		{"failed probe is down", r, down(), OutcomeDown},
		{"fast probe is up", r, fast(), OutcomeUp},
		{"at threshold is still up", r, slow(500), OutcomeUp},
		{"over threshold is degraded", r, slow(501), OutcomeDegraded},
		{"latency rule disabled", Rule{FailureThreshold: 3}, slow(99999), OutcomeUp},
		{"negative threshold treated as disabled", Rule{LatencyThresholdMs: -1}, slow(10), OutcomeUp},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.rule, tc.p); got != tc.want {
				t.Fatalf("Classify() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestOpensDownIncidentAfterConsecutiveFailures(t *testing.T) {
	r := Rule{FailureThreshold: 3, RecoveryThreshold: 2}
	s, ds := run(r, State{}, down(), down())
	for i, d := range ds {
		if d.Action != ActionNone {
			t.Fatalf("probe %d: got %s before threshold", i, d.Action)
		}
	}
	s, _, d := Evaluate(r, s, down())
	if d.Action != ActionOpen || d.Cause != CauseDown {
		t.Fatalf("got %+v, want open/down", d)
	}
	if s.Active != CauseDown || s.ConsecutiveFailures != 3 {
		t.Fatalf("unexpected state %+v", s)
	}
	// Further failures do not re-open.
	_, _, d = Evaluate(r, s, down())
	if d.Action != ActionNone {
		t.Fatalf("got %s, want none while incident active", d.Action)
	}
}

func TestFailureStreakResetsOnSuccess(t *testing.T) {
	r := Rule{FailureThreshold: 3, RecoveryThreshold: 1}
	_, ds := run(r, State{}, down(), down(), fast(), down(), down())
	for i, d := range ds {
		if d.Action != ActionNone {
			t.Fatalf("probe %d: unexpected %s; streak should have reset", i, d.Action)
		}
	}
}

func TestResolvesDownAfterRecoveryThreshold(t *testing.T) {
	r := Rule{FailureThreshold: 1, RecoveryThreshold: 2}
	s, ds := run(r, State{}, down(), fast())
	if ds[0].Action != ActionOpen || ds[1].Action != ActionNone {
		t.Fatalf("unexpected decisions %+v", ds)
	}
	s, _, d := Evaluate(r, s, fast())
	if d.Action != ActionResolve || d.Cause != CauseDown {
		t.Fatalf("got %+v, want resolve/down", d)
	}
	if s.Active != CauseNone {
		t.Fatalf("incident should be cleared, got %q", s.Active)
	}
}

func TestDownRecoveryCountsSlowProbesAsReachable(t *testing.T) {
	r := Rule{FailureThreshold: 2, LatencyThresholdMs: 100, RecoveryThreshold: 2}
	s, ds := run(r, State{}, down(), down(), slow(300), slow(300))
	if ds[1].Action != ActionOpen || ds[1].Cause != CauseDown {
		t.Fatalf("expected open/down at probe 2, got %+v", ds[1])
	}
	if ds[3].Action != ActionResolve {
		t.Fatalf("outage should resolve once target is reachable again, got %+v", ds[3])
	}
	// The latency rule then opens a degraded incident on the next slow probe
	// because the slow streak already meets the threshold.
	_, _, d := Evaluate(r, s, slow(300))
	if d.Action != ActionOpen || d.Cause != CauseDegraded {
		t.Fatalf("got %+v, want open/degraded", d)
	}
}

func TestLatencyRuleOpensDegradedIncident(t *testing.T) {
	r := Rule{FailureThreshold: 3, LatencyThresholdMs: 200, RecoveryThreshold: 2}
	s, ds := run(r, State{}, slow(250), slow(900), fast(), slow(250), slow(250))
	for i, d := range ds {
		if d.Action != ActionNone {
			t.Fatalf("probe %d: premature %s", i, d.Action)
		}
	}
	s, _, d := Evaluate(r, s, slow(250))
	if d.Action != ActionOpen || d.Cause != CauseDegraded {
		t.Fatalf("got %+v, want open/degraded", d)
	}
	// Slow probes keep it open; fast probes resolve it.
	s, ds = run(r, s, slow(400), fast())
	if ds[0].Action != ActionNone || ds[1].Action != ActionNone {
		t.Fatalf("unexpected decisions %+v", ds)
	}
	_, _, d = Evaluate(r, s, fast())
	if d.Action != ActionResolve || d.Cause != CauseDegraded {
		t.Fatalf("got %+v, want resolve/degraded", d)
	}
}

func TestDegradedEscalatesToDown(t *testing.T) {
	r := Rule{FailureThreshold: 2, LatencyThresholdMs: 100, RecoveryThreshold: 1}
	s, ds := run(r, State{}, slow(150), slow(150), down())
	if ds[1].Action != ActionOpen || ds[1].Cause != CauseDegraded {
		t.Fatalf("expected degraded open, got %+v", ds[1])
	}
	if ds[2].Action != ActionNone {
		t.Fatalf("single failure must not escalate, got %+v", ds[2])
	}
	s, _, d := Evaluate(r, s, down())
	if d.Action != ActionEscalate || d.Cause != CauseDown || s.Active != CauseDown {
		t.Fatalf("got %+v / %+v, want escalate to down", d, s)
	}
}

func TestDegradedDoesNotResolveOnSlowProbes(t *testing.T) {
	r := Rule{FailureThreshold: 1, LatencyThresholdMs: 100, RecoveryThreshold: 1}
	s, _ := run(r, State{}, slow(150))
	if s.Active != CauseDegraded {
		t.Fatalf("expected degraded incident, got %+v", s)
	}
	_, ds := run(r, s, slow(150), slow(150))
	for _, d := range ds {
		if d.Action != ActionNone {
			t.Fatalf("slow probes must not resolve a degraded incident: %+v", d)
		}
	}
}

func TestLatencyRuleDisabledNeverOpensDegraded(t *testing.T) {
	r := Rule{FailureThreshold: 1, RecoveryThreshold: 1}
	_, ds := run(r, State{}, slow(5000), slow(5000), slow(5000))
	for _, d := range ds {
		if d.Action != ActionNone {
			t.Fatalf("unexpected %+v", d)
		}
	}
}

func TestZeroThresholdsAreNormalised(t *testing.T) {
	_, _, d := Evaluate(Rule{}, State{}, down())
	if d.Action != ActionOpen {
		t.Fatalf("threshold 0 should behave as 1, got %+v", d)
	}
}

func TestOutcomeReturned(t *testing.T) {
	r := Rule{FailureThreshold: 3, LatencyThresholdMs: 100}
	for _, tc := range []struct {
		p    Probe
		want Outcome
	}{{down(), OutcomeDown}, {slow(101), OutcomeDegraded}, {fast(), OutcomeUp}} {
		if _, got, _ := Evaluate(r, State{}, tc.p); got != tc.want {
			t.Fatalf("outcome = %s, want %s", got, tc.want)
		}
	}
}
