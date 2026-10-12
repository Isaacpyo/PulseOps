package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("CORS_ORIGINS", " https://a.example.com/ , http://localhost:5173 ")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != "8080" || c.Notifier != "mock" || !c.SchedulerEnabled || c.AllowPrivateTargets || c.TrustProxyHeaders {
		t.Fatalf("unexpected defaults %+v", c)
	}
	if c.ResultRetention != 7*24*time.Hour || c.SessionTTL != 7*24*time.Hour {
		t.Fatalf("durations: %v %v", c.ResultRetention, c.SessionTTL)
	}
	if len(c.CORSOrigins) != 2 || c.CORSOrigins[0] != "https://a.example.com" {
		t.Fatalf("origins: %v", c.CORSOrigins)
	}
}

func TestLoadReportsAllErrors(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("NOTIFIER", "pager")
	t.Setenv("WORKER_CONCURRENCY", "lots")
	t.Setenv("SCHEDULER_ENABLED", "maybe")
	_, err := Load()
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"DATABASE_URL", "NOTIFIER", "WORKER_CONCURRENCY", "SCHEDULER_ENABLED"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}
