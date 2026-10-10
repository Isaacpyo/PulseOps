package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRouter(t *testing.T) {
	slack, mock := NewSlackProvider(), &MockProvider{}

	r := NewRouter(ModeMock, "https://hooks.slack.com/services/T/B/X", slack, mock)
	if p, _ := r.Route("https://hooks.slack.com/services/T/B/Y"); p != mock {
		t.Fatal("mock mode must always use the mock provider")
	}

	r = NewRouter(ModeSlack, "https://hooks.slack.com/services/default", slack, mock)
	if p, dest := r.Route("https://hooks.slack.com/services/ws"); p != slack || !strings.HasSuffix(dest, "/ws") {
		t.Fatalf("workspace webhook should win, got %s %s", p.Name(), dest)
	}
	if p, dest := r.Route(""); p != slack || !strings.HasSuffix(dest, "/default") {
		t.Fatalf("default webhook should be used, got %s %s", p.Name(), dest)
	}

	r = NewRouter(ModeSlack, "", slack, mock)
	if p, _ := r.Route(""); p != mock {
		t.Fatal("slack mode without any webhook should fall back to mock")
	}
}

func TestValidateSlackWebhook(t *testing.T) {
	ok := []string{
		"https://hooks.slack.com/services/T000/B000/XXXX",
		"https://hooks.slack-gov.com/services/T000/B000/XXXX",
	}
	bad := []string{
		"http://hooks.slack.com/services/T/B/X",
		"https://evil.com/services/T/B/X",
		"https://hooks.slack.com.evil.com/services/x",
		"https://hooks.slack.com:8443/services/x",
		"https://user@hooks.slack.com/services/x",
		"https://hooks.slack.com/api/x",
		"not a url",
	}
	for _, u := range ok {
		if err := ValidateSlackWebhook(u); err != nil {
			t.Errorf("%s: unexpected error %v", u, err)
		}
	}
	for _, u := range bad {
		if err := ValidateSlackWebhook(u); err == nil {
			t.Errorf("%s: expected rejection", u)
		}
	}
}

func TestRedactWebhook(t *testing.T) {
	got := RedactWebhook("https://hooks.slack.com/services/T000/B000/abcdefghWXYZ")
	if got != "https://hooks.slack.com/services/T000/B000/••••••WXYZ" {
		t.Fatalf("got %q", got)
	}
	if RedactWebhook("") != "" {
		t.Fatal("empty input should give empty output")
	}
}

func TestBuildIncidentMessage(t *testing.T) {
	inc := IncidentInfo{ID: 42, Title: "API is down", CheckName: "API", Target: "https://api.example.com", Cause: "down", OpenedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}

	m := BuildIncidentMessage("opened", inc, "3 consecutive failures", "", "https://app.example.com/")
	if m.Severity != SeverityCritical || m.Title != "Incident #42 opened: API is down" {
		t.Fatalf("unexpected opened message %+v", m)
	}
	if m.URL != "https://app.example.com/app/incidents/42" {
		t.Fatalf("url = %s", m.URL)
	}
	if len(m.Fields) != 4 || m.Fields[3].Value != "2026-01-02 03:04:05 UTC" {
		t.Fatalf("fields = %+v", m.Fields)
	}

	inc.Cause = "degraded"
	if m := BuildIncidentMessage("opened", inc, "", "", ""); m.Severity != SeverityWarning || m.URL != "" {
		t.Fatalf("degraded open should warn: %+v", m)
	}
	if m := BuildIncidentMessage("resolved", inc, "", "", ""); m.Severity != SeverityResolved || !strings.Contains(m.Title, "automatically") {
		t.Fatalf("auto resolve: %+v", m)
	}
	if m := BuildIncidentMessage("resolved", inc, "", "Ada", ""); !strings.Contains(m.Title, "by Ada") {
		t.Fatalf("manual resolve: %+v", m)
	}
	if m := BuildIncidentMessage("acknowledged", inc, "", "Ada", ""); m.Severity != SeverityInfo || !strings.Contains(m.Title, "acknowledged by Ada") {
		t.Fatalf("ack: %+v", m)
	}
}

func TestSlackProviderPostsBlockKit(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected request %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("invalid json: %v", err)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	msg := Message{Title: "Incident #1 opened: <API> & co", Severity: SeverityCritical, Text: "boom", URL: "https://x/app/incidents/1", Fields: []Field{{"Check", "API"}}}
	if err := NewSlackProvider().Send(context.Background(), srv.URL, msg); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got["text"] != msg.Title {
		t.Fatalf("fallback text = %v", got["text"])
	}
	att := got["attachments"].([]any)[0].(map[string]any)
	if att["color"] != "#dc2626" {
		t.Fatalf("color = %v", att["color"])
	}
	blocks := att["blocks"].([]any)
	if len(blocks) != 4 {
		t.Fatalf("want header, text, fields, actions blocks; got %d", len(blocks))
	}
	header := blocks[0].(map[string]any)["text"].(map[string]any)["text"].(string)
	if !strings.Contains(header, "&lt;API&gt; &amp; co") {
		t.Fatalf("mrkdwn not escaped: %s", header)
	}
}

func TestSlackProviderSurfacesErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("no_service"))
	}))
	defer srv.Close()
	err := NewSlackProvider().Send(context.Background(), srv.URL, Message{Title: "x"})
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "no_service") {
		t.Fatalf("err = %v", err)
	}
}

func TestMockProvider(t *testing.T) {
	m := &MockProvider{}
	_ = m.Send(context.Background(), "", Message{Title: "a"})
	if len(m.Sent()) != 1 {
		t.Fatal("message not recorded")
	}
	m.FailWith = errors.New("nope")
	if err := m.Send(context.Background(), "", Message{}); err == nil {
		t.Fatal("expected failure")
	}
}
