package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SlackProvider posts messages to Slack incoming webhooks using Block Kit.
type SlackProvider struct {
	Client *http.Client
}

// NewSlackProvider returns a provider with a sensible timeout.
func NewSlackProvider() *SlackProvider {
	return &SlackProvider{Client: &http.Client{Timeout: 10 * time.Second}}
}

// Name implements Provider.
func (s *SlackProvider) Name() string { return "slack" }

// Send implements Provider.
func (s *SlackProvider) Send(ctx context.Context, webhookURL string, msg Message) error {
	body, err := json.Marshal(SlackPayload(msg))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Client.Do(req)
	if err != nil {
		return fmt.Errorf("slack webhook request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack webhook returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

var severityStyle = map[Severity]struct{ emoji, color string }{
	SeverityCritical: {":red_circle:", "#dc2626"},
	SeverityWarning:  {":large_orange_circle:", "#d97706"},
	SeverityResolved: {":large_green_circle:", "#16a34a"},
	SeverityInfo:     {":large_blue_circle:", "#2563eb"},
}

// SlackPayload converts a Message into an incoming-webhook payload.
func SlackPayload(msg Message) map[string]any {
	style, ok := severityStyle[msg.Severity]
	if !ok {
		style = severityStyle[SeverityInfo]
	}
	blocks := []map[string]any{
		{
			"type": "section",
			"text": map[string]any{"type": "mrkdwn", "text": fmt.Sprintf("%s *%s*", style.emoji, escape(msg.Title))},
		},
	}
	if msg.Text != "" {
		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": map[string]any{"type": "mrkdwn", "text": escape(msg.Text)},
		})
	}
	if len(msg.Fields) > 0 {
		fields := make([]map[string]any, 0, len(msg.Fields))
		for _, f := range msg.Fields {
			fields = append(fields, map[string]any{"type": "mrkdwn", "text": fmt.Sprintf("*%s*\n%s", escape(f.Label), escape(f.Value))})
		}
		blocks = append(blocks, map[string]any{"type": "section", "fields": fields})
	}
	if msg.URL != "" {
		blocks = append(blocks, map[string]any{
			"type": "actions",
			"elements": []map[string]any{{
				"type": "button",
				"text": map[string]any{"type": "plain_text", "text": "Open in PulseOps"},
				"url":  msg.URL,
			}},
		})
	}
	return map[string]any{
		"text": msg.Title, // notification fallback
		"attachments": []map[string]any{{
			"color":  style.color,
			"blocks": blocks,
		}},
	}
}

// escape applies Slack's mrkdwn control-character escaping.
func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
