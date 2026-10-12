// Package metrics defines PulseOps' Prometheus instrumentation and a
// remote_write pusher for hosted Prometheus (Grafana Cloud).
package metrics

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds every collector the API exports.
type Metrics struct {
	Registry *prometheus.Registry

	ProbeDuration       *prometheus.HistogramVec
	ProbesTotal         *prometheus.CounterVec
	CheckUp             *prometheus.GaugeVec
	CheckLastLatency    *prometheus.GaugeVec
	SchedulerLag        prometheus.Histogram
	IncidentsOpened     *prometheus.CounterVec
	IncidentsResolved   *prometheus.CounterVec
	IncidentsActive     *prometheus.GaugeVec
	NotificationsTotal  *prometheus.CounterVec
	HTTPRequests        *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec
}

var checkLabels = []string{"check_id", "check", "kind"}

// New registers all collectors on a fresh registry.
func New() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		Registry: reg,
		ProbeDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "pulseops_probe_duration_seconds",
			Help:    "Duration of probes against check targets.",
			Buckets: []float64{0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		}, checkLabels),
		ProbesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pulseops_probes_total",
			Help: "Probes executed, by outcome (up, degraded, down).",
		}, append(checkLabels, "outcome")),
		CheckUp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pulseops_check_up",
			Help: "1 if the last probe reached the target (up or degraded), 0 if it failed.",
		}, checkLabels),
		CheckLastLatency: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pulseops_check_latency_seconds",
			Help: "Latency of the most recent probe.",
		}, checkLabels),
		SchedulerLag: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "pulseops_scheduler_lag_seconds",
			Help:    "Delay between a probe's scheduled time and when a worker started it.",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
		}),
		IncidentsOpened: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pulseops_incidents_opened_total",
			Help: "Incidents opened (or escalated) by the alert engine, by cause.",
		}, []string{"cause"}),
		IncidentsResolved: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pulseops_incidents_resolved_total",
			Help: "Incidents resolved, by how (auto or manual).",
		}, []string{"how"}),
		IncidentsActive: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pulseops_incidents_active",
			Help: "Currently open or acknowledged incidents, by cause.",
		}, []string{"cause"}),
		NotificationsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pulseops_notifications_total",
			Help: "Notification delivery attempts, by provider and result (sent, retry, failed).",
		}, []string{"provider", "result"}),
		HTTPRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pulseops_http_requests_total",
			Help: "API requests served.",
		}, []string{"method", "route", "code"}),
		HTTPRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "pulseops_http_request_duration_seconds",
			Help:    "API request latency.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
	}
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.ProbeDuration, m.ProbesTotal, m.CheckUp, m.CheckLastLatency, m.SchedulerLag,
		m.IncidentsOpened, m.IncidentsResolved, m.IncidentsActive, m.NotificationsTotal,
		m.HTTPRequests, m.HTTPRequestDuration,
	)
	return m
}

// ObserveProbe records a completed probe.
func (m *Metrics) ObserveProbe(checkID int64, name, kind, outcome string, latency time.Duration) {
	id := strconv.FormatInt(checkID, 10)
	m.ProbeDuration.WithLabelValues(id, name, kind).Observe(latency.Seconds())
	m.ProbesTotal.WithLabelValues(id, name, kind, outcome).Inc()
	up := 0.0
	if outcome != "down" {
		up = 1
	}
	m.CheckUp.WithLabelValues(id, name, kind).Set(up)
	m.CheckLastLatency.WithLabelValues(id, name, kind).Set(latency.Seconds())
}

// ForgetCheck drops per-check series after a check is deleted or renamed.
func (m *Metrics) ForgetCheck(checkID int64) {
	l := prometheus.Labels{"check_id": strconv.FormatInt(checkID, 10)}
	m.ProbeDuration.DeletePartialMatch(l)
	m.ProbesTotal.DeletePartialMatch(l)
	m.CheckUp.DeletePartialMatch(l)
	m.CheckLastLatency.DeletePartialMatch(l)
}

// Handler serves the registry in the Prometheus exposition format. When
// token is non-empty requests must carry "Authorization: Bearer <token>".
func (m *Metrics) Handler(token string) http.Handler {
	h := promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{Registry: m.Registry})
	if token == "" {
		return h
	}
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// Middleware records request counts and latency labelled by chi route
// pattern (not raw path) to keep cardinality bounded.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		route := "unmatched"
		if rc := chi.RouteContext(r.Context()); rc != nil {
			if p := rc.RoutePattern(); p != "" {
				route = strings.TrimSuffix(p, "/*")
				if route == "" {
					route = "/"
				}
			}
		}
		m.HTTPRequests.WithLabelValues(r.Method, route, strconv.Itoa(rw.status)).Inc()
		m.HTTPRequestDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }
