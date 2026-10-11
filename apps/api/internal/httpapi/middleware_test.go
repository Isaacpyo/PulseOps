package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIPRateLimiterIsPerClient(t *testing.T) {
	l := newIPRateLimiter(1, 2)
	for i := 0; i < 2; i++ {
		if ok, _ := l.reserve("a"); !ok {
			t.Fatalf("request %d for a should pass", i+1)
		}
	}
	if ok, retry := l.reserve("a"); ok || retry <= 0 {
		t.Fatalf("third request for a should be limited with a retry delay, got ok=%v retry=%v", ok, retry)
	}
	if ok, _ := l.reserve("b"); !ok {
		t.Fatal("another client must have its own bucket")
	}
}

func TestClientIPHonoursForwardedForOnlyWhenTrusted(t *testing.T) {
	var got string
	h := func(trust bool) http.Handler {
		return clientIPMiddleware(trust)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = clientIP(r) }))
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.9:5555"
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")

	h(false).ServeHTTP(httptest.NewRecorder(), req)
	if got != "10.0.0.9" {
		t.Fatalf("untrusted: got %q, want remote addr", got)
	}
	h(true).ServeHTTP(httptest.NewRecorder(), req)
	if got != "203.0.113.7" {
		t.Fatalf("trusted: got %q, want first forwarded address", got)
	}
}

func TestBearerToken(t *testing.T) {
	for header, want := range map[string]string{
		"Bearer abc":   "abc",
		"bearer abc":   "abc",
		"Basic abc":    "",
		"Bearer ":      "",
		"":             "",
		"Bearer  abc ": "abc",
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", header)
		if got, _ := bearerToken(req); got != want {
			t.Errorf("bearerToken(%q) = %q, want %q", header, got, want)
		}
	}
}
