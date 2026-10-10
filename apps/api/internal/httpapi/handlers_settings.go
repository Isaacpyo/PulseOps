package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/isaacpyo/pulseops/apps/api/internal/notify"
	"github.com/isaacpyo/pulseops/apps/api/internal/store"
)

type settingsResponse struct {
	Workspace     store.Workspace `json:"workspace"`
	StatusPageURL string          `json:"statusPageUrl"`
	Notifications notifySettings  `json:"notifications"`
}

type notifySettings struct {
	// Mode is the server-wide NOTIFIER setting ("mock" or "slack").
	Mode string `json:"mode"`
	// EffectiveProvider is where this workspace's alerts actually go.
	EffectiveProvider  string `json:"effectiveProvider"`
	WebhookConfigured  bool   `json:"webhookConfigured"`
	WebhookPreview     string `json:"webhookPreview"`
	UsingServerWebhook bool   `json:"usingServerWebhook"`
	ReadOnly           bool   `json:"readOnly"`
}

func (s *Server) settingsFor(ws store.Workspace) settingsResponse {
	webhook := ""
	if ws.SlackWebhookURL != nil {
		webhook = *ws.SlackWebhookURL
	}
	provider, _ := s.Notify.Route(webhook)
	return settingsResponse{
		Workspace:     ws,
		StatusPageURL: s.Config.PublicAppURL + "/status/" + ws.Slug,
		Notifications: notifySettings{
			Mode:               string(s.Notify.Mode()),
			EffectiveProvider:  provider.Name(),
			WebhookConfigured:  webhook != "",
			WebhookPreview:     notify.RedactWebhook(webhook),
			UsingServerWebhook: webhook == "" && provider.Name() == "slack",
			ReadOnly:           ws.IsDemo,
		},
	}
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	ws, err := s.Store.Workspace(r.Context(), currentWorkspace(r).ID)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.settingsFor(ws))
}

type settingsRequest struct {
	WorkspaceName   string         `json:"workspaceName"`
	SlackWebhookURL optionalString `json:"slackWebhookUrl"`
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	ws := currentWorkspace(r)
	if ws.IsDemo {
		// The demo workspace is shared by every visitor; letting one visitor
		// rename it or point its alerts at their own Slack would affect others.
		writeError(w, http.StatusForbidden, "demo_read_only", "Settings are read-only in the shared demo workspace. Create an account to configure Slack.")
		return
	}
	var req settingsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.WorkspaceName = strings.TrimSpace(req.WorkspaceName)
	errs := fieldErrors{}
	checkText(errs, "workspaceName", req.WorkspaceName, 1, 80)
	var webhook *string
	if req.SlackWebhookURL.Set {
		v := strings.TrimSpace(req.SlackWebhookURL.Value)
		if v != "" {
			if err := notify.ValidateSlackWebhook(v); err != nil {
				errs.add("slackWebhookUrl", err.Error())
			}
		}
		webhook = &v
	}
	if len(errs) > 0 {
		writeValidation(w, errs)
		return
	}
	updated, err := s.Store.UpdateWorkspaceSettings(r.Context(), ws.ID, req.WorkspaceName, webhook)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.settingsFor(updated))
}

func (s *Server) handleTestNotification(w http.ResponseWriter, r *http.Request) {
	ws := currentWorkspace(r)
	msg := notify.TestMessage(ws.Name, currentUser(r).Name, s.Config.PublicAppURL)
	if err := s.Store.EnqueueNotification(r.Context(), ws.ID, msg); err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			writeValidation(w, fieldErrors{"limit": "must be between 1 and 200"})
			return
		}
		limit = n
	}
	list, err := s.Store.ListNotifications(r.Context(), currentWorkspace(r).ID, limit)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notifications": list})
}
