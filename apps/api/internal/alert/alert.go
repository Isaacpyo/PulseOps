// Package alert implements the alert-rule engine: it classifies each probe
// result and decides when an incident should open, escalate or resolve.
//
// The engine is a pure function over (rule, state, probe) so it can be unit
// tested exhaustively; the worker persists the returned state.
package alert

// Outcome is the classification of a single probe.
type Outcome string

const (
	OutcomeUp       Outcome = "up"
	OutcomeDegraded Outcome = "degraded" // reachable but slower than the latency threshold
	OutcomeDown     Outcome = "down"
)

// Cause is why an incident is open.
type Cause string

const (
	CauseNone     Cause = ""
	CauseDown     Cause = "down"
	CauseDegraded Cause = "degraded"
)

// Action is what the caller must do after evaluating a probe.
type Action string

const (
	ActionNone     Action = "none"
	ActionOpen     Action = "open"     // open a new incident with Decision.Cause
	ActionEscalate Action = "escalate" // active degraded incident is now an outage
	ActionResolve  Action = "resolve"  // active incident recovered
)

// Rule is a check's alerting configuration.
type Rule struct {
	// FailureThreshold is the number of consecutive failing (or, for the
	// latency rule, slow) probes needed to open an incident.
	FailureThreshold int
	// LatencyThresholdMs marks a successful probe as degraded when exceeded.
	// Zero disables the latency rule.
	LatencyThresholdMs int
	// RecoveryThreshold is the number of consecutive good probes needed to
	// auto-resolve an incident.
	RecoveryThreshold int
}

// State is the per-check streak bookkeeping persisted between probes.
type State struct {
	ConsecutiveFailures  int // consecutive down probes
	ConsecutiveSlow      int // consecutive degraded probes
	ConsecutiveHealthy   int // consecutive up (fast) probes
	ConsecutiveReachable int // consecutive up-or-degraded probes
	// Active is the cause of the check's currently active incident, if any.
	Active Cause
}

// Probe is the subset of a probe result the engine needs.
type Probe struct {
	OK        bool
	LatencyMs int
}

// Decision is the engine's verdict for one probe.
type Decision struct {
	Action Action
	Cause  Cause
}

func (r Rule) normalized() Rule {
	if r.FailureThreshold < 1 {
		r.FailureThreshold = 1
	}
	if r.RecoveryThreshold < 1 {
		r.RecoveryThreshold = 1
	}
	if r.LatencyThresholdMs < 0 {
		r.LatencyThresholdMs = 0
	}
	return r
}

// Classify maps a probe to an outcome under the rule.
func Classify(rule Rule, p Probe) Outcome {
	rule = rule.normalized()
	switch {
	case !p.OK:
		return OutcomeDown
	case rule.LatencyThresholdMs > 0 && p.LatencyMs > rule.LatencyThresholdMs:
		return OutcomeDegraded
	default:
		return OutcomeUp
	}
}

// Evaluate advances the streak counters with a new probe and decides what
// should happen to the check's incident.
//
// Rules:
//   - no active incident: open "down" after FailureThreshold consecutive
//     failures, otherwise open "degraded" after FailureThreshold consecutive
//     slow probes;
//   - active "degraded": escalate to "down" once the failure rule trips,
//     resolve after RecoveryThreshold consecutive fast probes;
//   - active "down": resolve after RecoveryThreshold consecutive reachable
//     probes. If the target is back but still slow, the latency rule then
//     opens a fresh "degraded" incident on a later probe.
func Evaluate(rule Rule, s State, p Probe) (State, Outcome, Decision) {
	rule = rule.normalized()
	outcome := Classify(rule, p)

	switch outcome {
	case OutcomeDown:
		s.ConsecutiveFailures++
		s.ConsecutiveSlow, s.ConsecutiveHealthy, s.ConsecutiveReachable = 0, 0, 0
	case OutcomeDegraded:
		s.ConsecutiveSlow++
		s.ConsecutiveReachable++
		s.ConsecutiveFailures, s.ConsecutiveHealthy = 0, 0
	case OutcomeUp:
		s.ConsecutiveHealthy++
		s.ConsecutiveReachable++
		s.ConsecutiveFailures, s.ConsecutiveSlow = 0, 0
	}

	none := Decision{Action: ActionNone, Cause: s.Active}

	switch s.Active {
	case CauseNone:
		if s.ConsecutiveFailures >= rule.FailureThreshold {
			s.Active = CauseDown
			return s, outcome, Decision{Action: ActionOpen, Cause: CauseDown}
		}
		if rule.LatencyThresholdMs > 0 && s.ConsecutiveSlow >= rule.FailureThreshold {
			s.Active = CauseDegraded
			return s, outcome, Decision{Action: ActionOpen, Cause: CauseDegraded}
		}
	case CauseDegraded:
		if s.ConsecutiveFailures >= rule.FailureThreshold {
			s.Active = CauseDown
			return s, outcome, Decision{Action: ActionEscalate, Cause: CauseDown}
		}
		if s.ConsecutiveHealthy >= rule.RecoveryThreshold {
			s.Active = CauseNone
			return s, outcome, Decision{Action: ActionResolve, Cause: CauseDegraded}
		}
	case CauseDown:
		if s.ConsecutiveReachable >= rule.RecoveryThreshold {
			s.Active = CauseNone
			return s, outcome, Decision{Action: ActionResolve, Cause: CauseDown}
		}
	}
	return s, outcome, none
}
