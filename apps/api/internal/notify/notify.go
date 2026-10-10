// Package notify formats incident alerts and delivers them through a
// provider: Slack incoming webhooks, or a mock provider for keyless setups.
package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Severity drives colouring and emoji in notifications.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityResolved Severity = "resolved"
	SeverityInfo     Severity = "info"
)

// Field is a short label/value pair rendered alongside the message.
type Field struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Message is the provider-neutral alert payload stored in the outbox.
type Message struct {
	Event      string   `json:"event"`
	Severity   Severity `json:"severity"`
	Title      string   `json:"title"`
	Text       string   `json:"text"`
	CheckName  string   `json:"checkName,omitempty"`
	IncidentID int64    `json:"incidentId,omitempty"`
	URL        string   `json:"url,omitempty"`
	Fields     []Field  `json:"fields,omitempty"`
}

// Provider delivers a message to a destination.
type Provider interface {
	Name() string
	Send(ctx context.Context, destination string, msg Message) error
}

// Mode selects how the Router picks a provider.
type Mode string

const (
	ModeMock  Mode = "mock"
	ModeSlack Mode = "slack"
)

// Router chooses the provider for each delivery. In slack mode a workspace's
// own webhook wins over the process-wide default; with neither configured the
// message falls back to the mock provider so alerts are never silently lost.
type Router struct {
	mode           Mode
	defaultWebhook string
	slack          Provider
	mock           Provider
}

// NewRouter builds a Router.
func NewRouter(mode Mode, defaultWebhook string, slack, mock Provider) *Router {
	return &Router{mode: mode, defaultWebhook: defaultWebhook, slack: slack, mock: mock}
}

// Route returns the provider and destination for a workspace webhook.
func (r *Router) Route(workspaceWebhook string) (Provider, string) {
	if r.mode == ModeSlack {
		if workspaceWebhook != "" {
			return r.slack, workspaceWebhook
		}
		if r.defaultWebhook != "" {
			return r.slack, r.defaultWebhook
		}
	}
	return r.mock, "mock://pulseops"
}

// Mode reports the configured mode.
func (r *Router) Mode() Mode { return r.mode }

// ValidateSlackWebhook only allows genuine Slack incoming-webhook URLs, so
// the setting cannot be abused to make the server POST to arbitrary hosts.
func ValidateSlackWebhook(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return errors.New("must be an https://hooks.slack.com/services/… URL")
	}
	host := strings.ToLower(u.Hostname())
	if host != "hooks.slack.com" && host != "hooks.slack-gov.com" {
		return errors.New("must be an https://hooks.slack.com/services/… URL")
	}
	if !strings.HasPrefix(u.Path, "/services/") {
		return errors.New("must be a Slack incoming webhook URL (/services/…)")
	}
	return nil
}

// RedactWebhook hides the secret part of a webhook URL for display.
func RedactWebhook(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) > 0 {
		last := parts[len(parts)-1]
		if len(last) > 4 {
			parts[len(parts)-1] = strings.Repeat("•", 6) + last[len(last)-4:]
		}
	}
	return u.Scheme + "://" + u.Host + "/" + strings.Join(parts, "/")
}

// MockProvider records messages in memory and logs them. It stands in for
// Slack when no webhook is configured, in local development and in tests.
type MockProvider struct {
	mu   sync.Mutex
	sent []Message
	// FailWith, when non-nil, makes Send return this error (tests).
	FailWith error
}

// Name implements Provider.
func (m *MockProvider) Name() string { return "mock" }

// Send implements Provider.
func (m *MockProvider) Send(_ context.Context, _ string, msg Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailWith != nil {
		return m.FailWith
	}
	m.sent = append(m.sent, msg)
	if len(m.sent) > 200 {
		m.sent = m.sent[len(m.sent)-200:]
	}
	slog.Info("mock notification", "event", msg.Event, "title", msg.Title, "incident", msg.IncidentID)
	return nil
}

// Sent returns a copy of the recorded messages.
func (m *MockProvider) Sent() []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Message(nil), m.sent...)
}

// IncidentInfo is the incident data needed to render an alert.
type IncidentInfo struct {
	ID        int64
	Title     string
	CheckName string
	Target    string
	Cause     string
	OpenedAt  time.Time
}

// BuildIncidentMessage renders the alert for an incident lifecycle event.
func BuildIncidentMessage(event string, inc IncidentInfo, detail, actor, appURL string) Message {
	msg := Message{
		Event:      event,
		CheckName:  inc.CheckName,
		IncidentID: inc.ID,
		Text:       detail,
	}
	if appURL != "" {
		msg.URL = fmt.Sprintf("%s/app/incidents/%d", strings.TrimRight(appURL, "/"), inc.ID)
	}
	causeLabel := "Outage"
	if inc.Cause == "degraded" {
		causeLabel = "Degraded performance"
	}
	switch event {
	case "opened":
		msg.Title = fmt.Sprintf("Incident #%d opened: %s", inc.ID, inc.Title)
		msg.Severity = SeverityCritical
		if inc.Cause == "degraded" {
			msg.Severity = SeverityWarning
		}
	case "escalated":
		msg.Title = fmt.Sprintf("Incident #%d escalated: %s", inc.ID, inc.Title)
		msg.Severity = SeverityCritical
	case "acknowledged":
		msg.Title = fmt.Sprintf("Incident #%d acknowledged by %s", inc.ID, actor)
		msg.Severity = SeverityInfo
	case "resolved":
		who := "automatically"
		if actor != "" {
			who = "by " + actor
		}
		msg.Title = fmt.Sprintf("Incident #%d resolved %s: %s", inc.ID, who, inc.CheckName)
		msg.Severity = SeverityResolved
	default:
		msg.Title = fmt.Sprintf("Incident #%d: %s", inc.ID, inc.Title)
		msg.Severity = SeverityInfo
	}
	msg.Fields = []Field{
		{Label: "Check", Value: inc.CheckName},
		{Label: "Type", Value: causeLabel},
	}
	if inc.Target != "" {
		msg.Fields = append(msg.Fields, Field{Label: "Target", Value: inc.Target})
	}
	if !inc.OpenedAt.IsZero() {
		msg.Fields = append(msg.Fields, Field{Label: "Opened", Value: inc.OpenedAt.UTC().Format("2006-01-02 15:04:05 UTC")})
	}
	return msg
}

// TestMessage is sent from the settings page to verify the integration.
func TestMessage(workspace, actor, appURL string) Message {
	return Message{
		Event:    "test",
		Severity: SeverityInfo,
		Title:    "PulseOps test notification",
		Text:     fmt.Sprintf("%s sent a test alert from the %q workspace. If you can read this, alert delivery works.", actor, workspace),
		URL:      strings.TrimRight(appURL, "/") + "/app/settings",
	}
}
