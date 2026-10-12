package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/time/rate"

	"github.com/isaacpyo/pulseops/apps/api/internal/auth"
	"github.com/isaacpyo/pulseops/apps/api/internal/store"
)

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxWorkspace
	ctxClientIP
)

// clientIP resolves the caller's IP. X-Forwarded-For is only honoured when
// the deployment says a trusted proxy sets it (Render does).
func clientIPMiddleware(trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ""
			if trustProxy {
				if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
					ip = strings.TrimSpace(strings.Split(xff, ",")[0])
				}
			}
			if ip == "" {
				host, _, err := net.SplitHostPort(r.RemoteAddr)
				if err != nil {
					host = r.RemoteAddr
				}
				ip = host
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxClientIP, ip)))
		})
	}
}

func clientIP(r *http.Request) string {
	if ip, ok := r.Context().Value(ctxClientIP).(string); ok {
		return ip
	}
	return r.RemoteAddr
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// requestLogger logs one structured line per request, skipping probes from
// load balancers and scrapers to keep logs readable.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		slog.Info("request",
			"method", r.Method, "path", r.URL.Path, "status", ww.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()), "ip", clientIP(r))
	})
}

// requireAuth resolves the bearer token to a user and workspace.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "Sign in to continue.")
			return
		}
		user, ws, err := s.Store.SessionUser(r.Context(), auth.HashToken(token))
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "Your session has expired. Sign in again.")
			return
		}
		if err != nil {
			writeInternal(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), ctxUser, user)
		ctx = context.WithValue(ctx, ctxWorkspace, ws)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	t := strings.TrimSpace(h[len(prefix):])
	return t, t != "" && len(t) <= 128
}

func currentUser(r *http.Request) store.User {
	u, _ := r.Context().Value(ctxUser).(store.User)
	return u
}

func currentWorkspace(r *http.Request) store.Workspace {
	w, _ := r.Context().Value(ctxWorkspace).(store.Workspace)
	return w
}

// ipRateLimiter is a per-client token bucket. It is in-memory, so limits are
// per instance — adequate abuse protection for a single free-tier instance,
// not a security boundary.
type ipRateLimiter struct {
	mu      sync.Mutex
	limit   rate.Limit
	burst   int
	clients map[string]*limiterEntry
}

type limiterEntry struct {
	lim  *rate.Limiter
	seen time.Time
}

func newIPRateLimiter(perMinute float64, burst int) *ipRateLimiter {
	return &ipRateLimiter{limit: rate.Limit(perMinute / 60), burst: burst, clients: map[string]*limiterEntry{}}
}

func (l *ipRateLimiter) reserve(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.clients) > 10_000 {
		for k, e := range l.clients {
			if now.Sub(e.seen) > 10*time.Minute {
				delete(l.clients, k)
			}
		}
	}
	e, ok := l.clients[key]
	if !ok {
		e = &limiterEntry{lim: rate.NewLimiter(l.limit, l.burst)}
		l.clients[key] = e
	}
	e.seen = now
	res := e.lim.ReserveN(now, 1)
	if delay := res.DelayFrom(now); delay > 0 {
		res.CancelAt(now)
		return false, delay
	}
	return true, 0
}

func (l *ipRateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok, retry := l.reserve(clientIP(r))
		if !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retry.Seconds()))))
			writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many attempts. Try again shortly.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
