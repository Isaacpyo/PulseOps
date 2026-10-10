package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/isaacpyo/pulseops/apps/api/internal/metrics"
	"github.com/isaacpyo/pulseops/apps/api/internal/store"
)

// Maintenance prunes old data and refreshes database-derived gauges.
type Maintenance struct {
	Store           *store.Store
	Metrics         *metrics.Metrics
	ResultRetention time.Duration
}

// RollupRetention keeps a little more than the 90 days the status page shows.
const RollupRetention = 100 * 24 * time.Hour

// Run blocks until ctx is cancelled.
func (m *Maintenance) Run(ctx context.Context) {
	gauges := time.NewTicker(15 * time.Second)
	prune := time.NewTicker(10 * time.Minute)
	defer gauges.Stop()
	defer prune.Stop()

	m.refreshGauges(ctx)
	m.prune(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-gauges.C:
			m.refreshGauges(ctx)
		case <-prune.C:
			m.prune(ctx)
		}
	}
}

func (m *Maintenance) refreshGauges(ctx context.Context) {
	counts, err := m.Store.ActiveIncidentCounts(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("refresh incident gauge", "err", err)
		}
		return
	}
	for cause, n := range counts {
		m.Metrics.IncidentsActive.WithLabelValues(cause).Set(float64(n))
	}
}

func (m *Maintenance) prune(ctx context.Context) {
	now := time.Now()
	if n, err := m.Store.PruneResults(ctx, now.Add(-m.ResultRetention)); err != nil {
		slog.Warn("prune results", "err", err)
	} else if n > 0 {
		slog.Info("pruned probe results", "rows", n)
	}
	if _, err := m.Store.PruneRollups(ctx, now.Add(-RollupRetention)); err != nil {
		slog.Warn("prune rollups", "err", err)
	}
	if _, err := m.Store.DeleteExpiredSessions(ctx); err != nil {
		slog.Warn("prune sessions", "err", err)
	}
}
