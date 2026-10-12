// Package config loads runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the fully-resolved runtime configuration for the API process.
type Config struct {
	Port        string
	DatabaseURL string
	LogLevel    string

	// CORSOrigins lists browser origins allowed to call the API.
	CORSOrigins []string
	// PublicAppURL is the web console base URL, used for links in notifications.
	PublicAppURL string
	// TrustProxyHeaders makes the API read the client IP from X-Forwarded-For
	// (enable only behind a proxy that sets it, e.g. Render).
	TrustProxyHeaders bool

	SessionTTL time.Duration

	// SchedulerEnabled turns the probe scheduler + notification dispatcher on.
	SchedulerEnabled  bool
	WorkerConcurrency int
	// AllowPrivateTargets permits probing loopback/private/link-local addresses.
	// Keep false on any internet-facing deployment (SSRF protection).
	AllowPrivateTargets bool
	ResultRetention     time.Duration
	MaxChecksPerSpace   int

	// Notifier selects the alert delivery provider: "mock" or "slack".
	Notifier        string
	SlackWebhookURL string

	DemoEnabled     bool
	DemoResetOnBoot bool

	// MetricsToken, when set, protects GET /metrics with a bearer token.
	MetricsToken string

	RemoteWriteURL      string
	RemoteWriteUsername string
	RemoteWritePassword string
	RemoteWriteInterval time.Duration
}

// Load reads configuration from the environment and validates it.
func Load() (Config, error) {
	c := Config{
		Port:                env("PORT", "8080"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		LogLevel:            env("LOG_LEVEL", "info"),
		CORSOrigins:         splitList(env("CORS_ORIGINS", "http://localhost:5173,http://localhost:3000")),
		PublicAppURL:        strings.TrimRight(env("PUBLIC_APP_URL", "http://localhost:5173"), "/"),
		Notifier:            strings.ToLower(env("NOTIFIER", "mock")),
		SlackWebhookURL:     os.Getenv("SLACK_WEBHOOK_URL"),
		MetricsToken:        os.Getenv("METRICS_TOKEN"),
		RemoteWriteURL:      os.Getenv("REMOTE_WRITE_URL"),
		RemoteWriteUsername: os.Getenv("REMOTE_WRITE_USERNAME"),
		RemoteWritePassword: os.Getenv("REMOTE_WRITE_PASSWORD"),
	}

	var errs []error
	parseBool := func(key string, def bool) bool {
		v, err := boolEnv(key, def)
		if err != nil {
			errs = append(errs, err)
		}
		return v
	}
	parseInt := func(key string, def int) int {
		v, err := intEnv(key, def)
		if err != nil {
			errs = append(errs, err)
		}
		return v
	}
	parseDur := func(key string, def time.Duration) time.Duration {
		v, err := durationEnv(key, def)
		if err != nil {
			errs = append(errs, err)
		}
		return v
	}

	c.TrustProxyHeaders = parseBool("TRUST_PROXY_HEADERS", false)
	c.SchedulerEnabled = parseBool("SCHEDULER_ENABLED", true)
	c.AllowPrivateTargets = parseBool("ALLOW_PRIVATE_TARGETS", false)
	c.DemoEnabled = parseBool("DEMO_ENABLED", true)
	c.DemoResetOnBoot = parseBool("DEMO_RESET_ON_BOOT", false)
	c.WorkerConcurrency = parseInt("WORKER_CONCURRENCY", 8)
	c.MaxChecksPerSpace = parseInt("MAX_CHECKS_PER_WORKSPACE", 25)
	c.SessionTTL = parseDur("SESSION_TTL", 7*24*time.Hour)
	c.ResultRetention = time.Duration(parseInt("RESULT_RETENTION_DAYS", 7)) * 24 * time.Hour
	c.RemoteWriteInterval = parseDur("REMOTE_WRITE_INTERVAL", 30*time.Second)

	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if c.Notifier != "mock" && c.Notifier != "slack" {
		errs = append(errs, fmt.Errorf("NOTIFIER must be \"mock\" or \"slack\", got %q", c.Notifier))
	}
	if c.WorkerConcurrency < 1 {
		errs = append(errs, errors.New("WORKER_CONCURRENCY must be >= 1"))
	}
	if c.RemoteWriteInterval < 5*time.Second {
		errs = append(errs, errors.New("REMOTE_WRITE_INTERVAL must be >= 5s"))
	}
	return c, errors.Join(errs...)
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func boolEnv(key string, def bool) (bool, error) {
	v := env(key, "")
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def, fmt.Errorf("%s: invalid boolean %q", key, v)
	}
	return b, nil
}

func intEnv(key string, def int) (int, error) {
	v := env(key, "")
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def, fmt.Errorf("%s: invalid integer %q", key, v)
	}
	return n, nil
}

func durationEnv(key string, def time.Duration) (time.Duration, error) {
	v := env(key, "")
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def, fmt.Errorf("%s: invalid duration %q", key, v)
	}
	return d, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimRight(strings.TrimSpace(p), "/"); p != "" {
			out = append(out, p)
		}
	}
	return out
}
