package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/temitopeagbola/pulseops/apps/api/internal/probe"
	"github.com/temitopeagbola/pulseops/apps/api/internal/store"
	"github.com/temitopeagbola/pulseops/apps/api/internal/uptime"
)

// Limits enforced at the API boundary.
const (
	MinIntervalSeconds = 30
	MaxIntervalSeconds = 3600
	MinTimeoutMs       = 500
	MaxTimeoutMs       = 30000
	MaxDrillSeconds    = 15 * 60
	StatusPageDays     = 90
)

type checkRequest struct {
	Name               string `json:"name"`
	Kind               string `json:"kind"`
	Target             string `json:"target"`
	IntervalSeconds    *int   `json:"intervalSeconds"`
	TimeoutMs          *int   `json:"timeoutMs"`
	ExpectedStatus     *int   `json:"expectedStatus"`
	LatencyThresholdMs *int   `json:"latencyThresholdMs"`
	FailureThreshold   *int   `json:"failureThreshold"`
	RecoveryThreshold  *int   `json:"recoveryThreshold"`
	Enabled            *bool  `json:"enabled"`
	IsPublic           *bool  `json:"isPublic"`
}

func intOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// ValidateCheck validates a create/update body and returns the normalised
// store input. Omitted optional fields take sensible defaults.
func ValidateCheck(req checkRequest, allowPrivate bool) (store.CheckInput, fieldErrors) {
	errs := fieldErrors{}
	in := store.CheckInput{
		Name:               strings.TrimSpace(req.Name),
		Kind:               strings.ToLower(strings.TrimSpace(req.Kind)),
		IntervalSeconds:    intOr(req.IntervalSeconds, 60),
		TimeoutMs:          intOr(req.TimeoutMs, 10000),
		ExpectedStatus:     req.ExpectedStatus,
		LatencyThresholdMs: req.LatencyThresholdMs,
		FailureThreshold:   intOr(req.FailureThreshold, 3),
		RecoveryThreshold:  intOr(req.RecoveryThreshold, 2),
		Enabled:            boolOr(req.Enabled, true),
		IsPublic:           boolOr(req.IsPublic, true),
	}

	checkText(errs, "name", in.Name, 1, 80)
	if in.Kind != string(probe.KindHTTP) && in.Kind != string(probe.KindTCP) {
		errs.add("kind", `must be "http" or "tcp"`)
	} else {
		target, err := probe.ValidateTarget(probe.Kind(in.Kind), req.Target, allowPrivate)
		if err != nil {
			errs.add("target", err.Error())
		}
		in.Target = target
	}
	if in.IntervalSeconds < MinIntervalSeconds || in.IntervalSeconds > MaxIntervalSeconds {
		errs.add("intervalSeconds", fmt.Sprintf("must be between %d and %d", MinIntervalSeconds, MaxIntervalSeconds))
	}
	if in.TimeoutMs < MinTimeoutMs || in.TimeoutMs > MaxTimeoutMs {
		errs.add("timeoutMs", fmt.Sprintf("must be between %d and %d", MinTimeoutMs, MaxTimeoutMs))
	}
	if in.Kind == string(probe.KindTCP) {
		in.ExpectedStatus = nil
	} else if in.ExpectedStatus != nil && (*in.ExpectedStatus < 100 || *in.ExpectedStatus > 599) {
		errs.add("expectedStatus", "must be a valid HTTP status (100-599)")
	}
	if in.LatencyThresholdMs != nil && (*in.LatencyThresholdMs < 1 || *in.LatencyThresholdMs > 60000) {
		errs.add("latencyThresholdMs", "must be between 1 and 60000")
	}
	if in.FailureThreshold < 1 || in.FailureThreshold > 10 {
		errs.add("failureThreshold", "must be between 1 and 10")
	}
	if in.RecoveryThreshold < 1 || in.RecoveryThreshold > 10 {
		errs.add("recoveryThreshold", "must be between 1 and 10")
	}
	return in, errs
}

func (s *Server) handleListChecks(w http.ResponseWriter, r *http.Request) {
	checks, err := s.Store.ListChecks(r.Context(), currentWorkspace(r).ID)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"checks": checks})
}

func (s *Server) handleCreateCheck(w http.ResponseWriter, r *http.Request) {
	var req checkRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in, errs := ValidateCheck(req, s.Config.AllowPrivateTargets)
	if len(errs) > 0 {
		writeValidation(w, errs)
		return
	}
	ws := currentWorkspace(r)
	n, err := s.Store.CountChecks(r.Context(), ws.ID)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	if n >= s.Config.MaxChecksPerSpace {
		writeError(w, http.StatusConflict, "limit_reached",
			fmt.Sprintf("This workspace already has the maximum of %d checks.", s.Config.MaxChecksPerSpace))
		return
	}
	c, err := s.Store.CreateCheck(r.Context(), ws.ID, in)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"check": c})
}

type checkDetail struct {
	Check     store.Check         `json:"check"`
	Results   []store.CheckResult `json:"results"`
	Uptime    uptimeSummary       `json:"uptime"`
	Incidents []store.Incident    `json:"incidents"`
}

type uptimeSummary struct {
	Overall *float64     `json:"overall"`
	Bars    []uptime.Bar `json:"bars"`
}

func (s *Server) handleGetCheck(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	ws := currentWorkspace(r)
	c, err := s.Store.GetCheck(r.Context(), ws.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Check not found.")
		return
	}
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	now := time.Now().UTC()
	results, err := s.Store.ListResults(r.Context(), c.ID, now.Add(-24*time.Hour), 500)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	from := now.AddDate(0, 0, -(StatusPageDays - 1))
	rollups, err := s.Store.DailyRollups(r.Context(), []int64{c.ID}, from)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	incidents, err := s.Store.ListIncidents(r.Context(), ws.ID, store.IncidentFilter{CheckID: c.ID, Limit: 20})
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, checkDetail{
		Check:   c,
		Results: results,
		Uptime: uptimeSummary{
			Overall: uptime.Overall(rollups[c.ID]),
			Bars:    uptime.Bars(rollups[c.ID], now, StatusPageDays),
		},
		Incidents: incidents,
	})
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	var req checkRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in, errs := ValidateCheck(req, s.Config.AllowPrivateTargets)
	if len(errs) > 0 {
		writeValidation(w, errs)
		return
	}
	before, err := s.Store.GetCheck(r.Context(), currentWorkspace(r).ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Check not found.")
		return
	}
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	c, err := s.Store.UpdateCheck(r.Context(), currentWorkspace(r).ID, id, in)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Check not found.")
		return
	}
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	if before.Name != c.Name || before.Kind != c.Kind {
		s.Metrics.ForgetCheck(c.ID) // drop series carrying the old labels
	}
	writeJSON(w, http.StatusOK, map[string]any{"check": c})
}

func (s *Server) handleDeleteCheck(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	err := s.Store.DeleteCheck(r.Context(), currentWorkspace(r).ID, id, currentUser(r))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Check not found.")
		return
	}
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	s.Metrics.ForgetCheck(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRunCheck(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	c, err := s.Store.TriggerCheck(r.Context(), currentWorkspace(r).ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Check not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "check_paused", "Resume the check before running it.")
	case err != nil:
		writeInternal(w, r, err)
	default:
		writeJSON(w, http.StatusAccepted, map[string]any{"check": c})
	}
}

type drillRequest struct {
	DurationSeconds int `json:"durationSeconds"`
}

func (s *Server) handleStartDrill(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	var req drillRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.DurationSeconds < 30 || req.DurationSeconds > MaxDrillSeconds {
		writeValidation(w, fieldErrors{"durationSeconds": fmt.Sprintf("must be between 30 and %d", MaxDrillSeconds)})
		return
	}
	until := time.Now().Add(time.Duration(req.DurationSeconds) * time.Second)
	s.respondDrill(w, r, id, &until)
}

func (s *Server) handleStopDrill(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	s.respondDrill(w, r, id, nil)
}

func (s *Server) respondDrill(w http.ResponseWriter, r *http.Request, id int64, until *time.Time) {
	c, err := s.Store.SetDrill(r.Context(), currentWorkspace(r).ID, id, until)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Check not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "check_paused", "Resume the check before running a drill.")
	case err != nil:
		writeInternal(w, r, err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"check": c})
	}
}
