// Command pulseops runs the PulseOps API, probe scheduler, notification
// dispatcher and (optionally) the Grafana Cloud remote_write pusher in a
// single process.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/temitopeagbola/pulseops/apps/api/internal/config"
	"github.com/temitopeagbola/pulseops/apps/api/internal/db"
	"github.com/temitopeagbola/pulseops/apps/api/internal/httpapi"
	"github.com/temitopeagbola/pulseops/apps/api/internal/metrics"
	"github.com/temitopeagbola/pulseops/apps/api/internal/notify"
	"github.com/temitopeagbola/pulseops/apps/api/internal/probe"
	"github.com/temitopeagbola/pulseops/apps/api/internal/seed"
	"github.com/temitopeagbola/pulseops/apps/api/internal/store"
	"github.com/temitopeagbola/pulseops/apps/api/internal/worker"
)

func main() {
	// `pulseops healthcheck` lets container runtimes probe the distroless image,
	// which has no shell or curl.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	setupLogging(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}

	if cfg.DemoEnabled {
		if err := seed.EnsureDemo(ctx, pool, cfg.DemoResetOnBoot, cfg.PublicAppURL); err != nil {
			return err
		}
	}

	st := store.New(pool, cfg.PublicAppURL)
	m := metrics.New()
	router := notify.NewRouter(notify.Mode(cfg.Notifier), cfg.SlackWebhookURL, notify.NewSlackProvider(), &notify.MockProvider{})
	if cfg.Notifier == "slack" && cfg.SlackWebhookURL == "" {
		slog.Warn("NOTIFIER=slack but SLACK_WEBHOOK_URL is empty; workspaces without their own webhook fall back to the mock provider")
	}

	var wg sync.WaitGroup
	start := func(name string, fn func(context.Context)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn(ctx)
			slog.Info("stopped", "component", name)
		}()
	}

	if cfg.SchedulerEnabled {
		sched := &worker.Scheduler{
			Store: st, Prober: probe.New(cfg.AllowPrivateTargets), Metrics: m,
			Concurrency: cfg.WorkerConcurrency, PollInterval: time.Second,
		}
		start("scheduler", sched.Run)
	} else {
		slog.Warn("probe scheduler disabled (SCHEDULER_ENABLED=false)")
	}
	start("dispatcher", (&worker.Dispatcher{Store: st, Router: router, Metrics: m}).Run)
	start("maintenance", (&worker.Maintenance{Store: st, Metrics: m, ResultRetention: cfg.ResultRetention}).Run)

	if cfg.RemoteWriteURL != "" {
		host, _ := os.Hostname()
		rw := metrics.NewRemoteWriter(m, cfg.RemoteWriteURL, cfg.RemoteWriteUsername, cfg.RemoteWritePassword,
			cfg.RemoteWriteInterval, map[string]string{"job": "pulseops-api", "instance": host})
		start("remote_write", rw.Run)
	}

	api := &httpapi.Server{Store: st, Metrics: m, Notify: router, Config: cfg, DemoEmail: seed.DemoEmail}
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      40 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("api listening", "addr", srv.Addr, "notifier", cfg.Notifier, "scheduler", cfg.SchedulerEnabled)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		stop()
		wg.Wait()
		return err
	case <-ctx.Done():
	}

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Warn("http shutdown", "err", err)
	}
	wg.Wait()
	return nil
}

func healthcheck() int {
	port := 8080
	if p, err := strconv.Atoi(os.Getenv("PORT")); err == nil && p > 0 && p < 65536 {
		port = p
	}
	client := &http.Client{Timeout: 3 * time.Second}
	// Loopback only, numeric port: not attacker-controlled.
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port)) //nolint:gosec
	if err != nil {
		return 1
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func setupLogging(level string) {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})))
}
