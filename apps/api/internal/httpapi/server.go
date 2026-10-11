// Package httpapi exposes the PulseOps JSON API.
package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/isaacpyo/pulseops/apps/api/internal/config"
	"github.com/isaacpyo/pulseops/apps/api/internal/metrics"
	"github.com/isaacpyo/pulseops/apps/api/internal/notify"
	"github.com/isaacpyo/pulseops/apps/api/internal/store"
)

// Server holds handler dependencies.
type Server struct {
	Store   *store.Store
	Metrics *metrics.Metrics
	Notify  *notify.Router
	Config  config.Config
	// DemoEmail is the account "Try the demo" signs into.
	DemoEmail string
	// AuthRateLimit and WriteRateLimit default to 20/min (burst 10) and
	// 120/min (burst 60) per client IP when zero.
	AuthRateLimit  RateLimit
	WriteRateLimit RateLimit

	authLimiter  *ipRateLimiter
	writeLimiter *ipRateLimiter
}

// Handler builds the HTTP router.
func (s *Server) Handler() http.Handler {
	s.authLimiter = s.AuthRateLimit.orDefault(20, 10).limiter()
	s.writeLimiter = s.WriteRateLimit.orDefault(120, 60).limiter()

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(clientIPMiddleware(s.Config.TrustProxyHeaders))
	r.Use(s.Metrics.Middleware)
	r.Use(requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: s.Config.CORSOrigins,
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowedHeaders: []string{"Authorization", "Content-Type"},
		ExposedHeaders: []string{"Retry-After"},
		MaxAge:         600,
	}))
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"name": "PulseOps API", "health": "/healthz", "metrics": "/metrics"})
	})
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", s.handleReady)
	r.Method(http.MethodGet, "/metrics", s.Metrics.Handler(s.Config.MetricsToken))

	r.Route("/api", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(s.authLimiter.middleware)
			r.Post("/auth/register", s.handleRegister)
			r.Post("/auth/login", s.handleLogin)
			r.Post("/auth/demo", s.handleDemoLogin)
		})

		r.Get("/public/status/{slug}", s.handlePublicStatus)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Use(s.limitWrites)

			r.Post("/auth/logout", s.handleLogout)
			r.Get("/me", s.handleMe)

			r.Get("/checks", s.handleListChecks)
			r.Post("/checks", s.handleCreateCheck)
			r.Get("/checks/{id}", s.handleGetCheck)
			r.Put("/checks/{id}", s.handleUpdateCheck)
			r.Delete("/checks/{id}", s.handleDeleteCheck)
			r.Post("/checks/{id}/run", s.handleRunCheck)
			r.Post("/checks/{id}/drill", s.handleStartDrill)
			r.Delete("/checks/{id}/drill", s.handleStopDrill)

			r.Get("/incidents", s.handleListIncidents)
			r.Get("/incidents/{id}", s.handleGetIncident)
			r.Post("/incidents/{id}/acknowledge", s.handleAcknowledge)
			r.Post("/incidents/{id}/resolve", s.handleResolve)
			r.Post("/incidents/{id}/notes", s.handleAddNote)

			r.Get("/settings", s.handleGetSettings)
			r.Put("/settings", s.handleUpdateSettings)
			r.Post("/settings/test-notification", s.handleTestNotification)
			r.Get("/notifications", s.handleListNotifications)
		})
	})

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "Not found.")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	})
	return r
}

// RateLimit is a per-client token bucket: PerMinute refill, Burst capacity.
type RateLimit struct {
	PerMinute float64
	Burst     int
}

func (r RateLimit) orDefault(perMinute float64, burst int) RateLimit {
	if r.PerMinute <= 0 || r.Burst <= 0 {
		return RateLimit{PerMinute: perMinute, Burst: burst}
	}
	return r
}

func (r RateLimit) limiter() *ipRateLimiter { return newIPRateLimiter(r.PerMinute, r.Burst) }

// limitWrites rate-limits mutating requests per client.
func (s *Server) limitWrites(next http.Handler) http.Handler {
	limited := s.writeLimiter.middleware(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		limited.ServeHTTP(w, r)
	})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database_unavailable", "Database is not reachable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
