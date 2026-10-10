package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/isaacpyo/pulseops/apps/api/internal/metrics"
	"github.com/isaacpyo/pulseops/apps/api/internal/notify"
	"github.com/isaacpyo/pulseops/apps/api/internal/store"
)

// Dispatcher drains the notification outbox.
type Dispatcher struct {
	Store        *store.Store
	Router       *notify.Router
	Metrics      *metrics.Metrics
	PollInterval time.Duration
	MaxAttempts  int
	Now          func() time.Time
}

// Run polls the outbox until ctx is cancelled.
func (d *Dispatcher) Run(ctx context.Context) {
	if d.PollInterval <= 0 {
		d.PollInterval = 2 * time.Second
	}
	t := time.NewTicker(d.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := d.DispatchOnce(ctx); err != nil && ctx.Err() == nil {
				slog.Error("dispatch notifications", "err", err)
			}
		}
	}
}

// Backoff returns the retry delay after the given attempt (1-based):
// 15s, 30s, 1m, 2m … capped at 10m.
func Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := 15 * time.Second << (attempt - 1)
	if d > 10*time.Minute || d <= 0 {
		d = 10 * time.Minute
	}
	return d
}

// DispatchOnce delivers one batch and returns how many were attempted.
func (d *Dispatcher) DispatchOnce(ctx context.Context) (int, error) {
	maxAttempts := d.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	batch, err := d.Store.ClaimNotifications(ctx, 20)
	if err != nil {
		return 0, err
	}
	for _, n := range batch {
		provider, dest := d.Router.Route(n.WorkspaceWebhook)
		sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		sendErr := provider.Send(sendCtx, dest, n.Message)
		cancel()

		if sendErr == nil {
			if err := d.Store.MarkNotificationSent(ctx, n, provider.Name(), timelineMessage(provider.Name(), n.Event)); err != nil {
				return len(batch), err
			}
			d.count(provider.Name(), "sent")
			continue
		}

		slog.Warn("notification delivery failed", "id", n.ID, "provider", provider.Name(), "attempt", n.Attempts, "err", sendErr)
		var retryAt *time.Time
		result := "failed"
		if n.Attempts < maxAttempts {
			t := now().Add(Backoff(n.Attempts))
			retryAt, result = &t, "retry"
		}
		if err := d.Store.MarkNotificationFailed(ctx, n, provider.Name(), sendErr.Error(), retryAt); err != nil {
			return len(batch), err
		}
		d.count(provider.Name(), result)
	}
	return len(batch), nil
}

func (d *Dispatcher) count(provider, result string) {
	if d.Metrics != nil {
		d.Metrics.NotificationsTotal.WithLabelValues(provider, result).Inc()
	}
}

func timelineMessage(provider, event string) string {
	if provider == "slack" {
		return fmt.Sprintf("Sent %q alert to Slack.", event)
	}
	return fmt.Sprintf("Sent %q alert via the mock notifier (no Slack webhook configured).", event)
}
