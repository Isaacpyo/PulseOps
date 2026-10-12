package httpapi

import (
	"testing"

	"github.com/temitopeagbola/pulseops/apps/api/internal/store"
)

func ptr[T any](v T) *T { return &v }

func TestValidateCheckDefaultsAndNormalisation(t *testing.T) {
	in, errs := ValidateCheck(checkRequest{Name: "  API ", Kind: "HTTP", Target: " https://example.com/health "}, false)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors %v", errs)
	}
	if in.Name != "API" || in.Kind != "http" || in.Target != "https://example.com/health" {
		t.Fatalf("not normalised: %+v", in)
	}
	if in.IntervalSeconds != 60 || in.TimeoutMs != 10000 || in.FailureThreshold != 3 || in.RecoveryThreshold != 2 || !in.Enabled || !in.IsPublic {
		t.Fatalf("defaults not applied: %+v", in)
	}
}

func TestValidateCheckRejectsBadInput(t *testing.T) {
	cases := []struct {
		name  string
		req   checkRequest
		field string
	}{
		{"missing name", checkRequest{Kind: "http", Target: "https://example.com"}, "name"},
		{"long name", checkRequest{Name: string(make([]byte, 81)), Kind: "http", Target: "https://example.com"}, "name"},
		{"bad kind", checkRequest{Name: "x", Kind: "icmp", Target: "1.1.1.1"}, "kind"},
		{"bad url", checkRequest{Name: "x", Kind: "http", Target: "example.com"}, "target"},
		{"private target", checkRequest{Name: "x", Kind: "tcp", Target: "10.0.0.1:22"}, "target"},
		{"interval too low", checkRequest{Name: "x", Kind: "http", Target: "https://e.com", IntervalSeconds: ptr(5)}, "intervalSeconds"},
		{"timeout too high", checkRequest{Name: "x", Kind: "http", Target: "https://e.com", TimeoutMs: ptr(31000)}, "timeoutMs"},
		{"timeout too low", checkRequest{Name: "x", Kind: "http", Target: "https://e.com", TimeoutMs: ptr(100)}, "timeoutMs"},
		{"bad status", checkRequest{Name: "x", Kind: "http", Target: "https://e.com", ExpectedStatus: ptr(700)}, "expectedStatus"},
		{"bad latency", checkRequest{Name: "x", Kind: "http", Target: "https://e.com", LatencyThresholdMs: ptr(0)}, "latencyThresholdMs"},
		{"bad failure threshold", checkRequest{Name: "x", Kind: "http", Target: "https://e.com", FailureThreshold: ptr(11)}, "failureThreshold"},
		{"bad recovery threshold", checkRequest{Name: "x", Kind: "http", Target: "https://e.com", RecoveryThreshold: ptr(0)}, "recoveryThreshold"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := ValidateCheck(tc.req, false)
			if _, ok := errs[tc.field]; !ok {
				t.Fatalf("expected error on %s, got %v", tc.field, errs)
			}
		})
	}
}

func TestValidateCheckDropsExpectedStatusForTCP(t *testing.T) {
	in, errs := ValidateCheck(checkRequest{Name: "db", Kind: "tcp", Target: "db.example.com:5432", ExpectedStatus: ptr(200)}, false)
	if len(errs) > 0 || in.ExpectedStatus != nil {
		t.Fatalf("got %+v, %v", in, errs)
	}
}

func TestComponentAndOverallState(t *testing.T) {
	up := store.PublicCheck{Enabled: true, Status: "up"}
	if s := ComponentState(up, ""); s != StateOperational {
		t.Fatalf("got %s", s)
	}
	// A single failed probe without an incident is not an outage.
	if s := ComponentState(store.PublicCheck{Enabled: true, Status: "down"}, ""); s != StateOperational {
		t.Fatalf("got %s", s)
	}
	if s := ComponentState(up, "down"); s != StateMajorOutage {
		t.Fatalf("got %s", s)
	}
	if s := ComponentState(up, "degraded"); s != StateDegraded {
		t.Fatalf("got %s", s)
	}
	if s := ComponentState(store.PublicCheck{Enabled: false}, "down"); s != StateMaintenance {
		t.Fatalf("paused check: %s", s)
	}
	if s := ComponentState(store.PublicCheck{Enabled: true, Status: "pending"}, ""); s != StatePending {
		t.Fatalf("got %s", s)
	}

	cases := []struct {
		states []string
		want   string
	}{
		{nil, StateOperational},
		{[]string{StateOperational, StateOperational}, StateOperational},
		{[]string{StateOperational, StateDegraded}, StateDegraded},
		{[]string{StateOperational, StateOperational, StateMajorOutage}, StatePartialOutage},
		{[]string{StateOperational, StateMajorOutage}, StateMajorOutage},
		{[]string{StateMajorOutage, StateMaintenance, StateMaintenance}, StateMajorOutage},
		{[]string{StateDegraded, StateMajorOutage, StateOperational, StateOperational, StateOperational}, StatePartialOutage},
	}
	for _, tc := range cases {
		if got := OverallState(tc.states); got != tc.want {
			t.Errorf("OverallState(%v) = %s, want %s", tc.states, got, tc.want)
		}
	}
}

func TestValidEmail(t *testing.T) {
	for _, e := range []string{"a@b.co", "first.last@example.com"} {
		if !validEmail(e) {
			t.Errorf("%s should be valid", e)
		}
	}
	for _, e := range []string{"", "a", "a@b", "Ada <a@b.co>", "a@@b.co"} {
		if validEmail(e) {
			t.Errorf("%s should be invalid", e)
		}
	}
}
