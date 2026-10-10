package metrics

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang/snappy"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/protobuf/encoding/protowire"
)

// decodeWriteRequest is a minimal protobuf decoder used to verify the
// hand-rolled encoder round-trips.
func decodeWriteRequest(t *testing.T, b []byte) []TimeSeries {
	t.Helper()
	var out []TimeSeries
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 || num != 1 || typ != protowire.BytesType {
			t.Fatalf("bad WriteRequest tag %d/%d", num, typ)
		}
		b = b[n:]
		tsb, n := protowire.ConsumeBytes(b)
		b = b[n:]
		var ts TimeSeries
		for len(tsb) > 0 {
			num, _, n := protowire.ConsumeTag(tsb)
			tsb = tsb[n:]
			inner, n := protowire.ConsumeBytes(tsb)
			tsb = tsb[n:]
			switch num {
			case 1:
				var l Label
				for len(inner) > 0 {
					f, _, n := protowire.ConsumeTag(inner)
					inner = inner[n:]
					s, n := protowire.ConsumeString(inner)
					inner = inner[n:]
					if f == 1 {
						l.Name = s
					} else {
						l.Value = s
					}
				}
				ts.Labels = append(ts.Labels, l)
			case 2:
				var s Sample
				for len(inner) > 0 {
					f, _, n := protowire.ConsumeTag(inner)
					inner = inner[n:]
					if f == 1 {
						v, n := protowire.ConsumeFixed64(inner)
						inner = inner[n:]
						s.Value = math.Float64frombits(v)
					} else {
						v, n := protowire.ConsumeVarint(inner)
						inner = inner[n:]
						s.Timestamp = int64(v)
					}
				}
				ts.Samples = append(ts.Samples, s)
			}
		}
		out = append(out, ts)
	}
	return out
}

func labelMap(ts TimeSeries) map[string]string {
	m := map[string]string{}
	for _, l := range ts.Labels {
		m[l.Name] = l.Value
	}
	return m
}

func find(series []TimeSeries, name string, match map[string]string) *TimeSeries {
	for i := range series {
		lm := labelMap(series[i])
		if lm["__name__"] != name {
			continue
		}
		ok := true
		for k, v := range match {
			if lm[k] != v {
				ok = false
			}
		}
		if ok {
			return &series[i]
		}
	}
	return nil
}

func TestToTimeSeriesAndEncodingRoundTrip(t *testing.T) {
	m := New()
	m.ObserveProbe(7, "API", "http", "up", 120*time.Millisecond)
	m.ObserveProbe(7, "API", "http", "down", 2*time.Second)
	m.IncidentsOpened.WithLabelValues("down").Inc()

	mfs, err := m.Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1_700_000_000_000)
	series := ToTimeSeries(mfs, now, map[string]string{"job": "pulseops-api", "instance": "test"})
	decoded := decodeWriteRequest(t, EncodeWriteRequest(series))

	if len(decoded) != len(series) {
		t.Fatalf("decoded %d series, encoded %d", len(decoded), len(series))
	}
	for _, ts := range decoded {
		for i := 1; i < len(ts.Labels); i++ {
			if ts.Labels[i-1].Name >= ts.Labels[i].Name {
				t.Fatalf("labels not sorted: %+v", ts.Labels)
			}
		}
		if lm := labelMap(ts); lm["job"] != "pulseops-api" || lm["instance"] != "test" {
			t.Fatalf("extra labels missing: %+v", lm)
		}
		if ts.Samples[0].Timestamp != now.UnixMilli() {
			t.Fatalf("timestamp = %d", ts.Samples[0].Timestamp)
		}
	}

	if s := find(decoded, "pulseops_probes_total", map[string]string{"check_id": "7", "outcome": "down"}); s == nil || s.Samples[0].Value != 1 {
		t.Fatalf("counter series missing or wrong: %+v", s)
	}
	if s := find(decoded, "pulseops_check_up", map[string]string{"check": "API"}); s == nil || s.Samples[0].Value != 0 {
		t.Fatalf("gauge should reflect last (down) probe: %+v", s)
	}
	if s := find(decoded, "pulseops_probe_duration_seconds_bucket", map[string]string{"le": "+Inf"}); s == nil || s.Samples[0].Value != 2 {
		t.Fatalf("+Inf bucket: %+v", s)
	}
	if s := find(decoded, "pulseops_probe_duration_seconds_bucket", map[string]string{"le": "0.25"}); s == nil || s.Samples[0].Value != 1 {
		t.Fatalf("0.25 bucket: %+v", s)
	}
	if s := find(decoded, "pulseops_probe_duration_seconds_sum", nil); s == nil || math.Abs(s.Samples[0].Value-2.12) > 1e-9 {
		t.Fatalf("sum: %+v", s)
	}
	if s := find(decoded, "pulseops_probe_duration_seconds_count", nil); s == nil || s.Samples[0].Value != 2 {
		t.Fatalf("count: %+v", s)
	}
}

func TestPushOnceSendsSnappyProtobufWithAuth(t *testing.T) {
	var (
		gotSeries []TimeSeries
		gotUser   string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Encoding") != "snappy" || r.Header.Get("Content-Type") != "application/x-protobuf" {
			t.Errorf("bad headers: %v", r.Header)
		}
		if r.Header.Get("X-Prometheus-Remote-Write-Version") != "0.1.0" {
			t.Errorf("missing remote write version header")
		}
		user, pass, ok := r.BasicAuth()
		if !ok || pass != "secret" {
			t.Errorf("bad basic auth")
		}
		gotUser = user
		body, _ := io.ReadAll(r.Body)
		raw, err := snappy.Decode(nil, body)
		if err != nil {
			t.Errorf("snappy: %v", err)
		}
		gotSeries = decodeWriteRequest(t, raw)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	m := New()
	m.ObserveProbe(1, "Web", "http", "up", time.Millisecond)
	rw := NewRemoteWriter(m, srv.URL, "12345", "secret", time.Minute, map[string]string{"job": "pulseops-api"})
	if err := rw.PushOnce(context.Background()); err != nil {
		t.Fatalf("push: %v", err)
	}
	if gotUser != "12345" || find(gotSeries, "pulseops_check_up", map[string]string{"check": "Web"}) == nil {
		t.Fatalf("push payload incomplete (user %q, %d series)", gotUser, len(gotSeries))
	}
	if testutil.ToFloat64(rw.Pushes) != 1 {
		t.Fatal("push counter not incremented")
	}
}

func TestPushOnceReportsServerErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "invalid token", http.StatusUnauthorized)
	}))
	defer srv.Close()
	m := New()
	rw := NewRemoteWriter(m, srv.URL, "u", "p", time.Minute, nil)
	err := rw.PushOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v", err)
	}
	if testutil.ToFloat64(rw.Failures) != 1 {
		t.Fatal("failure counter not incremented")
	}
}

func TestHandlerTokenProtection(t *testing.T) {
	m := New()
	h := m.Handler("tok")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "go_goroutines") {
		t.Fatalf("with token: %d", rec.Code)
	}
}

func TestMiddlewareUsesRoutePattern(t *testing.T) {
	m := New()
	r := chi.NewRouter()
	r.Use(m.Middleware)
	r.Get("/api/checks/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/checks/42", nil))

	got := testutil.ToFloat64(m.HTTPRequests.With(prometheus.Labels{"method": "GET", "route": "/api/checks/{id}", "code": "418"}))
	if got != 1 {
		t.Fatalf("request counter = %v", got)
	}
}

func TestForgetCheck(t *testing.T) {
	m := New()
	m.ObserveProbe(3, "X", "tcp", "up", time.Millisecond)
	m.ForgetCheck(3)
	if n := testutil.CollectAndCount(m.CheckUp); n != 0 {
		t.Fatalf("series remain: %d", n)
	}
}
