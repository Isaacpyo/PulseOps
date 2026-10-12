package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/temitopeagbola/pulseops/apps/api/internal/store"
	"github.com/temitopeagbola/pulseops/apps/api/internal/uptime"
)

// Component and overall states shown on the public status page. They are
// driven by active incidents (not single probe blips), like a real status page.
const (
	StateOperational   = "operational"
	StateDegraded      = "degraded"
	StatePartialOutage = "partial_outage"
	StateMajorOutage   = "major_outage"
	StateMaintenance   = "paused"
	StatePending       = "pending"
)

type publicComponent struct {
	ID       int64        `json:"id"`
	Name     string       `json:"name"`
	Kind     string       `json:"kind"`
	State    string       `json:"state"`
	Uptime90 *float64     `json:"uptime90"`
	Bars     []uptime.Bar `json:"bars"`
}

type publicStatus struct {
	Workspace struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"workspace"`
	State       string                 `json:"state"`
	Components  []publicComponent      `json:"components"`
	Active      []store.PublicIncident `json:"activeIncidents"`
	Recent      []store.PublicIncident `json:"recentIncidents"`
	GeneratedAt time.Time              `json:"generatedAt"`
}

// ComponentState derives a component's public state from its check and the
// cause of its active incident ("" when none).
func ComponentState(c store.PublicCheck, activeCause string) string {
	switch {
	case !c.Enabled:
		return StateMaintenance
	case activeCause == "down":
		return StateMajorOutage
	case activeCause == "degraded":
		return StateDegraded
	case c.Status == "pending":
		return StatePending
	default:
		return StateOperational
	}
}

// OverallState rolls component states up into the page banner: any outage
// is a partial outage, and it becomes major when at least half the
// monitored components are down.
func OverallState(states []string) string {
	monitored, down, degraded := 0, 0, 0
	for _, s := range states {
		switch s {
		case StateMaintenance:
			continue
		case StateMajorOutage:
			down++
		case StateDegraded:
			degraded++
		}
		monitored++
	}
	switch {
	case down > 0 && down*2 >= monitored:
		return StateMajorOutage
	case down > 0:
		return StatePartialOutage
	case degraded > 0:
		return StateDegraded
	default:
		return StateOperational
	}
}

func (s *Server) handlePublicStatus(w http.ResponseWriter, r *http.Request) {
	ws, err := s.Store.WorkspaceBySlug(r.Context(), chiParam(r, "slug"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Status page not found.")
		return
	}
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	now := time.Now().UTC()
	checks, err := s.Store.PublicChecks(r.Context(), ws.ID)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	incidents, err := s.Store.PublicIncidents(r.Context(), ws.ID, now.AddDate(0, 0, -14))
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	ids := make([]int64, len(checks))
	for i, c := range checks {
		ids[i] = c.ID
	}
	rollups, err := s.Store.DailyRollups(r.Context(), ids, now.AddDate(0, 0, -(StatusPageDays-1)))
	if err != nil {
		writeInternal(w, r, err)
		return
	}

	activeByCheck := map[int64]string{}
	resp := publicStatus{GeneratedAt: now, Active: []store.PublicIncident{}, Recent: []store.PublicIncident{}}
	resp.Workspace.Name, resp.Workspace.Slug = ws.Name, ws.Slug
	for _, inc := range incidents {
		if inc.Status == "resolved" {
			resp.Recent = append(resp.Recent, inc)
			continue
		}
		resp.Active = append(resp.Active, inc)
		activeByCheck[inc.CheckID] = inc.Cause
	}

	states := make([]string, 0, len(checks))
	resp.Components = make([]publicComponent, 0, len(checks))
	for _, c := range checks {
		state := ComponentState(c, activeByCheck[c.ID])
		states = append(states, state)
		resp.Components = append(resp.Components, publicComponent{
			ID: c.ID, Name: c.Name, Kind: c.Kind, State: state,
			Uptime90: uptime.Overall(rollups[c.ID]),
			Bars:     uptime.Bars(rollups[c.ID], now, StatusPageDays),
		})
	}
	resp.State = OverallState(states)

	// Public, unauthenticated and identical for every viewer: let browsers
	// and CDNs cache it briefly.
	w.Header().Set("Cache-Control", "public, max-age=30")
	writeJSON(w, http.StatusOK, resp)
}
