// Package worker runs PulseOps' background loops: the probe scheduler and
// worker pool, the notification dispatcher, and housekeeping.
package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/temitopeagbola/pulseops/apps/api/internal/alert"
	"github.com/temitopeagbola/pulseops/apps/api/internal/metrics"
	"github.com/temitopeagbola/pulseops/apps/api/internal/probe"
	"github.com/temitopeagbola/pulseops/apps/api/internal/store"
)

// DrillError is the error recorded for probes during a chaos drill.
const DrillError = "Simulated outage (chaos drill)"

// ProbeRunner executes a probe; *probe.Prober satisfies it.
type ProbeRunner interface {
	Run(ctx context.Context, spec probe.Spec) probe.Result
}

// Scheduler claims due checks from Postgres and fans them out to a fixed
// pool of probe workers.
type Scheduler struct {
	Store        *store.Store
	Prober       ProbeRunner
	Metrics      *metrics.Metrics
	Concurrency  int
	PollInterval time.Duration
	Now          func() time.Time

	inflight atomic.Int64
}

// Run blocks until ctx is cancelled, then waits for in-flight probes.
func (s *Scheduler) Run(ctx context.Context) {
	if s.Concurrency < 1 {
		s.Concurrency = 1
	}
	if s.PollInterval <= 0 {
		s.PollInterval = time.Second
	}
	jobs := make(chan store.ClaimedCheck, s.Concurrency)
	var wg sync.WaitGroup
	for i := 0; i < s.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range jobs {
				if _, err := s.Execute(ctx, c); err != nil && !errors.Is(err, context.Canceled) {
					slog.Error("probe failed to record", "check", c.ID, "err", err)
				}
				s.inflight.Add(-1)
			}
		}()
	}
	slog.Info("probe scheduler started", "workers", s.Concurrency)

	ticker := time.NewTicker(s.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			slog.Info("probe scheduler stopped")
			return
		case <-ticker.C:
			free := s.Concurrency - int(s.inflight.Load())
			if free <= 0 {
				continue
			}
			due, err := s.Store.ClaimDueChecks(ctx, free)
			if err != nil {
				if ctx.Err() == nil {
					slog.Error("claim due checks", "err", err)
				}
				continue
			}
			for _, c := range due {
				s.inflight.Add(1)
				jobs <- c
			}
		}
	}
}

func (s *Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Execute probes one claimed check and records the result. It is exported so
// tests (and a future "run now, synchronously" endpoint) can drive it.
func (s *Scheduler) Execute(ctx context.Context, c store.ClaimedCheck) (store.ProbeOutcome, error) {
	start := s.now()
	if s.Metrics != nil && !c.DueAt.IsZero() {
		if lag := start.Sub(c.DueAt); lag > 0 {
			s.Metrics.SchedulerLag.Observe(lag.Seconds())
		}
	}

	var res probe.Result
	if c.DrillUntil != nil && start.Before(*c.DrillUntil) {
		res = probe.Result{OK: false, Error: DrillError}
	} else {
		res = s.Prober.Run(ctx, probe.Spec{
			Kind:           probe.Kind(c.Kind),
			Target:         c.Target,
			Timeout:        time.Duration(c.TimeoutMs) * time.Millisecond,
			ExpectedStatus: derefInt(c.ExpectedStatus),
		})
	}

	out, err := s.Store.RecordProbe(ctx, store.ProbeRecord{
		CheckID:    c.ID,
		CheckedAt:  start,
		OK:         res.OK,
		LatencyMs:  int(res.Latency.Milliseconds()),
		StatusCode: res.StatusCode,
		Error:      res.Error,
	})
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrConflict) {
		return out, nil // deleted or paused while probing
	}
	if err != nil {
		return out, err
	}

	if s.Metrics != nil {
		s.Metrics.ObserveProbe(c.ID, c.Name, c.Kind, string(out.Outcome), res.Latency)
		switch out.Decision.Action {
		case alert.ActionOpen, alert.ActionEscalate:
			s.Metrics.IncidentsOpened.WithLabelValues(string(out.Decision.Cause)).Inc()
		case alert.ActionResolve:
			s.Metrics.IncidentsResolved.WithLabelValues("auto").Inc()
		}
	}
	if out.Decision.Action != alert.ActionNone {
		slog.Info("alert decision", "check", c.ID, "action", out.Decision.Action, "cause", out.Decision.Cause, "incident", out.IncidentID)
	}
	return out, nil
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
