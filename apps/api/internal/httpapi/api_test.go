package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/temitopeagbola/pulseops/apps/api/internal/config"
	"github.com/temitopeagbola/pulseops/apps/api/internal/metrics"
	"github.com/temitopeagbola/pulseops/apps/api/internal/notify"
	"github.com/temitopeagbola/pulseops/apps/api/internal/seed"
	"github.com/temitopeagbola/pulseops/apps/api/internal/store"
	"github.com/temitopeagbola/pulseops/apps/api/internal/testdb"
)

type apiClient struct {
	t     *testing.T
	base  string
	token string
}

type apiResp struct {
	Status int
	Body   map[string]any
	Header http.Header
}

func (r apiResp) errField(name string) string {
	e, _ := r.Body["error"].(map[string]any)
	fields, _ := e["fields"].(map[string]any)
	s, _ := fields[name].(string)
	return s
}

func (r apiResp) errCode() string {
	e, _ := r.Body["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func (c *apiClient) do(method, path string, body any) apiResp {
	c.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	out := apiResp{Status: res.StatusCode, Header: res.Header, Body: map[string]any{}}
	raw, _ := io.ReadAll(res.Body)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out.Body); err != nil {
			c.t.Fatalf("%s %s: invalid JSON %q", method, path, raw)
		}
	}
	return out
}

type testEnv struct {
	srv   *httptest.Server
	store *store.Store
}

func newTestEnv(t *testing.T, opts ...func(*Server)) *testEnv {
	t.Helper()
	pool := testdb.New(t)
	if err := seed.EnsureDemo(context.Background(), pool, false, "https://app.test"); err != nil {
		t.Fatal(err)
	}
	st := store.New(pool, "https://app.test")
	s := &Server{
		Store:   st,
		Metrics: metrics.New(),
		Notify:  notify.NewRouter(notify.ModeMock, "", notify.NewSlackProvider(), &notify.MockProvider{}),
		Config: config.Config{
			CORSOrigins: []string{"https://app.test"}, PublicAppURL: "https://app.test",
			SessionTTL: time.Hour, MaxChecksPerSpace: 3, DemoEnabled: true,
		},
		DemoEmail: seed.DemoEmail,
	}
	for _, o := range opts {
		o(s)
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return &testEnv{srv: srv, store: st}
}

func (e *testEnv) client(t *testing.T) *apiClient { return &apiClient{t: t, base: e.srv.URL} }

func (e *testEnv) register(t *testing.T, email string) *apiClient {
	t.Helper()
	c := e.client(t)
	res := c.do("POST", "/api/auth/register", map[string]any{
		"name": "Ada", "email": email, "password": "s3cure-password", "workspaceName": "Ada Ops",
	})
	if res.Status != http.StatusCreated {
		t.Fatalf("register: %d %v", res.Status, res.Body)
	}
	c.token = res.Body["token"].(string)
	return c
}

func TestHealthReadyAndMetrics(t *testing.T) {
	e := newTestEnv(t)
	c := e.client(t)
	if r := c.do("GET", "/healthz", nil); r.Status != 200 || r.Body["status"] != "ok" {
		t.Fatalf("healthz: %+v", r)
	}
	if r := c.do("GET", "/readyz", nil); r.Status != 200 {
		t.Fatalf("readyz: %+v", r)
	}
	res, err := http.Get(e.srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(body), "pulseops_http_requests_total") {
		t.Fatal("metrics endpoint missing API request counter")
	}
}

func TestAuthFlow(t *testing.T) {
	e := newTestEnv(t)
	c := e.register(t, "ada@example.com")

	if r := c.do("GET", "/api/me", nil); r.Status != 200 || r.Body["user"].(map[string]any)["email"] != "ada@example.com" {
		t.Fatalf("me: %+v", r)
	}
	if _, leaked := c.do("GET", "/api/me", nil).Body["user"].(map[string]any)["passwordHash"]; leaked {
		t.Fatal("password hash leaked")
	}

	anon := e.client(t)
	if r := anon.do("POST", "/api/auth/register", map[string]any{"name": "B", "email": "ADA@example.com", "password": "another-pass"}); r.Status != 422 || r.errField("email") != "is already registered" {
		t.Fatalf("duplicate email: %+v", r)
	}
	if r := anon.do("POST", "/api/auth/register", map[string]any{"name": "", "email": "nope", "password": "short"}); r.Status != 422 ||
		r.errField("name") == "" || r.errField("email") == "" || r.errField("password") == "" {
		t.Fatalf("validation: %+v", r)
	}
	if r := anon.do("POST", "/api/auth/login", map[string]any{"email": "ada@example.com", "password": "wrong-password"}); r.Status != 401 {
		t.Fatalf("bad password: %+v", r)
	}
	if r := anon.do("POST", "/api/auth/login", map[string]any{"email": "nobody@example.com", "password": "whatever-pass"}); r.Status != 401 {
		t.Fatalf("unknown email: %+v", r)
	}
	login := anon.do("POST", "/api/auth/login", map[string]any{"email": "Ada@Example.com", "password": "s3cure-password"})
	if login.Status != 200 || login.Body["token"] == "" {
		t.Fatalf("login: %+v", login)
	}

	if r := c.do("POST", "/api/auth/logout", nil); r.Status != 204 {
		t.Fatalf("logout: %+v", r)
	}
	if r := c.do("GET", "/api/me", nil); r.Status != 401 {
		t.Fatalf("token should be revoked: %+v", r)
	}
	if r := e.client(t).do("GET", "/api/checks", nil); r.Status != 401 || r.errCode() != "unauthenticated" {
		t.Fatalf("anonymous access: %+v", r)
	}
}

func TestStrictJSONDecoding(t *testing.T) {
	e := newTestEnv(t)
	c := e.client(t)
	if r := c.do("POST", "/api/auth/login", map[string]any{"email": "a@b.co", "password": "x", "admin": true}); r.Status != 400 || r.errCode() != "invalid_json" {
		t.Fatalf("unknown field: %+v", r)
	}
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/auth/login", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	res, _ := http.DefaultClient.Do(req)
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("malformed JSON: %d", res.StatusCode)
	}
}

func TestCheckCRUDAndLimits(t *testing.T) {
	e := newTestEnv(t)
	c := e.register(t, "ada@example.com")

	r := c.do("POST", "/api/checks", map[string]any{"name": "Site", "kind": "http", "target": "https://example.com", "latencyThresholdMs": 800})
	if r.Status != 201 {
		t.Fatalf("create: %+v", r)
	}
	id := int64(r.Body["check"].(map[string]any)["id"].(float64))

	if r := c.do("POST", "/api/checks", map[string]any{"name": "Metadata", "kind": "http", "target": "http://169.254.169.254/latest"}); r.Status != 422 || r.errField("target") == "" {
		t.Fatalf("SSRF target accepted: %+v", r)
	}

	detail := c.do("GET", fmt.Sprintf("/api/checks/%d", id), nil)
	if detail.Status != 200 || len(detail.Body["uptime"].(map[string]any)["bars"].([]any)) != 90 {
		t.Fatalf("detail: %+v", detail)
	}

	upd := c.do("PUT", fmt.Sprintf("/api/checks/%d", id), map[string]any{"name": "Site v2", "kind": "http", "target": "https://example.com", "enabled": false})
	if upd.Status != 200 || upd.Body["check"].(map[string]any)["enabled"] != false {
		t.Fatalf("update: %+v", upd)
	}
	if r := c.do("POST", fmt.Sprintf("/api/checks/%d/run", id), nil); r.Status != 409 {
		t.Fatalf("run on paused check: %+v", r)
	}
	if r := c.do("POST", fmt.Sprintf("/api/checks/%d/drill", id), map[string]any{"durationSeconds": 5}); r.Status != 422 {
		t.Fatalf("drill validation: %+v", r)
	}

	list := c.do("GET", "/api/checks", nil)
	if checks := list.Body["checks"].([]any); len(checks) != 1 {
		t.Fatalf("list: %+v", list)
	}

	for i := 0; i < 2; i++ {
		c.do("POST", "/api/checks", map[string]any{"name": fmt.Sprint("c", i), "kind": "tcp", "target": "example.com:443"})
	}
	if r := c.do("POST", "/api/checks", map[string]any{"name": "one too many", "kind": "tcp", "target": "example.com:443"}); r.Status != 409 || r.errCode() != "limit_reached" {
		t.Fatalf("limit: %+v", r)
	}

	if r := c.do("DELETE", fmt.Sprintf("/api/checks/%d", id), nil); r.Status != 204 {
		t.Fatalf("delete: %+v", r)
	}
	if r := c.do("GET", fmt.Sprintf("/api/checks/%d", id), nil); r.Status != 404 {
		t.Fatalf("deleted check still visible: %+v", r)
	}
}

func TestTenantIsolation(t *testing.T) {
	e := newTestEnv(t)
	alice := e.register(t, "alice@example.com")
	bob := e.register(t, "bob@example.com")

	r := alice.do("POST", "/api/checks", map[string]any{"name": "Alice API", "kind": "http", "target": "https://example.com"})
	id := int64(r.Body["check"].(map[string]any)["id"].(float64))

	for _, call := range []struct{ method, path string }{
		{"GET", fmt.Sprintf("/api/checks/%d", id)},
		{"DELETE", fmt.Sprintf("/api/checks/%d", id)},
		{"POST", fmt.Sprintf("/api/checks/%d/run", id)},
	} {
		if r := bob.do(call.method, call.path, nil); r.Status != 404 {
			t.Fatalf("%s %s as another tenant: %d", call.method, call.path, r.Status)
		}
	}
	if r := bob.do("PUT", fmt.Sprintf("/api/checks/%d", id), map[string]any{"name": "pwned", "kind": "http", "target": "https://example.com"}); r.Status != 404 {
		t.Fatalf("cross-tenant update: %d", r.Status)
	}
	if checks := bob.do("GET", "/api/checks", nil).Body["checks"].([]any); len(checks) != 0 {
		t.Fatalf("bob sees alice's checks: %v", checks)
	}
}

func TestDemoLoginIncidentFlowAndStatusPage(t *testing.T) {
	e := newTestEnv(t)
	c := e.client(t)
	r := c.do("POST", "/api/auth/demo", nil)
	if r.Status != 200 {
		t.Fatalf("demo login: %+v", r)
	}
	c.token = r.Body["token"].(string)

	checks := c.do("GET", "/api/checks", nil).Body["checks"].([]any)
	if len(checks) != 6 {
		t.Fatalf("demo checks = %d", len(checks))
	}

	active := c.do("GET", "/api/incidents?status=active", nil).Body["incidents"].([]any)
	if len(active) != 1 {
		t.Fatalf("expected one seeded active incident, got %d", len(active))
	}
	incID := int64(active[0].(map[string]any)["id"].(float64))
	path := fmt.Sprintf("/api/incidents/%d", incID)

	if r := c.do("POST", path+"/acknowledge", nil); r.Status != 200 || r.Body["incident"].(map[string]any)["status"] != "acknowledged" {
		t.Fatalf("ack: %+v", r)
	}
	if r := c.do("POST", path+"/acknowledge", nil); r.Status != 409 {
		t.Fatalf("double ack: %+v", r)
	}
	if r := c.do("POST", path+"/notes", map[string]any{"message": "  "}); r.Status != 422 {
		t.Fatalf("empty note: %+v", r)
	}
	if r := c.do("POST", path+"/notes", map[string]any{"message": "Investigating upstream latency"}); r.Status != 201 {
		t.Fatalf("note: %+v", r)
	}
	if r := c.do("POST", path+"/resolve", map[string]any{"note": "Upstream recovered."}); r.Status != 200 || r.Body["incident"].(map[string]any)["status"] != "resolved" {
		t.Fatalf("resolve: %+v", r)
	}
	timeline := c.do("GET", path, nil).Body["events"].([]any)
	var kinds []string
	for _, ev := range timeline {
		kinds = append(kinds, ev.(map[string]any)["kind"].(string))
	}
	if got := strings.Join(kinds, ","); got != "opened,notified,acknowledged,note,resolved" {
		t.Fatalf("timeline = %s", got)
	}

	// Demo settings are read-only, but test notifications work.
	if r := c.do("PUT", "/api/settings", map[string]any{"workspaceName": "Hacked"}); r.Status != 403 {
		t.Fatalf("demo settings writable: %+v", r)
	}
	if r := c.do("POST", "/api/settings/test-notification", nil); r.Status != 202 {
		t.Fatalf("test notification: %+v", r)
	}

	pub := e.client(t).do("GET", "/api/public/status/demo", nil)
	if pub.Status != 200 || pub.Header.Get("Cache-Control") != "public, max-age=30" {
		t.Fatalf("public status: %+v", pub)
	}
	if pub.Body["state"] != StateOperational {
		t.Fatalf("state after resolve = %v", pub.Body["state"])
	}
	comps := pub.Body["components"].([]any)
	if len(comps) != 6 {
		t.Fatalf("components = %d", len(comps))
	}
	first := comps[0].(map[string]any)
	if _, exposed := first["target"]; exposed {
		t.Fatal("public status must not expose targets")
	}
	bars := first["bars"].([]any)
	if len(bars) != 90 || bars[89].(map[string]any)["uptime"] == nil {
		t.Fatalf("bars = %d", len(bars))
	}
	if recent := pub.Body["recentIncidents"].([]any); len(recent) == 0 {
		t.Fatal("expected recently resolved incidents on the status page")
	}
	if r := e.client(t).do("GET", "/api/public/status/does-not-exist", nil); r.Status != 404 {
		t.Fatalf("unknown slug: %d", r.Status)
	}
}

func TestSettingsUpdateValidatesWebhook(t *testing.T) {
	e := newTestEnv(t)
	c := e.register(t, "ada@example.com")

	if r := c.do("PUT", "/api/settings", map[string]any{"workspaceName": "Ops", "slackWebhookUrl": "https://evil.example.com/hook"}); r.Status != 422 || r.errField("slackWebhookUrl") == "" {
		t.Fatalf("non-slack webhook accepted: %+v", r)
	}
	r := c.do("PUT", "/api/settings", map[string]any{"workspaceName": "Ops", "slackWebhookUrl": "https://hooks.slack.com/services/T0/B0/abcdefgh1234"})
	if r.Status != 200 {
		t.Fatalf("update: %+v", r)
	}
	n := r.Body["notifications"].(map[string]any)
	if n["webhookConfigured"] != true || strings.Contains(n["webhookPreview"].(string), "abcdefgh") {
		t.Fatalf("webhook not stored or not redacted: %+v", n)
	}
	// Omitting the field keeps it; null clears it.
	if r := c.do("PUT", "/api/settings", map[string]any{"workspaceName": "Ops 2"}); r.Body["notifications"].(map[string]any)["webhookConfigured"] != true {
		t.Fatalf("omitted webhook cleared: %+v", r)
	}
	if r := c.do("PUT", "/api/settings", map[string]any{"workspaceName": "Ops 2", "slackWebhookUrl": nil}); r.Body["notifications"].(map[string]any)["webhookConfigured"] != false {
		t.Fatalf("null did not clear webhook: %+v", r)
	}
}

func TestAuthRateLimit(t *testing.T) {
	// One token per minute with a burst of 3 makes the 4th attempt fail
	// deterministically, however slow the test machine is.
	e := newTestEnv(t, func(s *Server) { s.AuthRateLimit = RateLimit{PerMinute: 1, Burst: 3} })
	c := e.client(t)
	for i := 0; i < 3; i++ {
		if r := c.do("POST", "/api/auth/login", map[string]any{"email": "x@example.com", "password": "nope-nope"}); r.Status != 401 {
			t.Fatalf("attempt %d: got %d, want 401", i+1, r.Status)
		}
	}
	last := c.do("POST", "/api/auth/login", map[string]any{"email": "x@example.com", "password": "nope-nope"})
	if last.Status != http.StatusTooManyRequests || last.Header.Get("Retry-After") == "" {
		t.Fatalf("expected 429 with Retry-After, got %d", last.Status)
	}
	// Non-auth endpoints are not affected by the auth bucket.
	if r := c.do("GET", "/api/public/status/demo", nil); r.Status != 200 {
		t.Fatalf("public endpoint throttled: %d", r.Status)
	}
}

func TestCORSPreflight(t *testing.T) {
	e := newTestEnv(t)
	req, _ := http.NewRequest("OPTIONS", e.srv.URL+"/api/checks", nil)
	req.Header.Set("Origin", "https://app.test")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.Header.Get("Access-Control-Allow-Origin") != "https://app.test" {
		t.Fatalf("allowed origin not echoed: %v", res.Header)
	}

	req.Header.Set("Origin", "https://evil.example.com")
	res, _ = http.DefaultClient.Do(req)
	res.Body.Close()
	if res.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("disallowed origin was granted CORS")
	}
}
