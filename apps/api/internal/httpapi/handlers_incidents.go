package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/temitopeagbola/pulseops/apps/api/internal/store"
)

func (s *Server) handleListIncidents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.IncidentFilter{Status: q.Get("status"), Limit: 100}
	switch f.Status {
	case "", "all":
		f.Status = ""
	case "active", "resolved":
	default:
		writeValidation(w, fieldErrors{"status": `must be "active", "resolved" or "all"`})
		return
	}
	if v := q.Get("checkId"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			writeValidation(w, fieldErrors{"checkId": "must be a positive integer"})
			return
		}
		f.CheckID = id
	}
	incidents, err := s.Store.ListIncidents(r.Context(), currentWorkspace(r).ID, f)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"incidents": incidents})
}

func (s *Server) handleGetIncident(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	inc, events, err := s.Store.GetIncident(r.Context(), currentWorkspace(r).ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Incident not found.")
		return
	}
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"incident": inc, "events": events})
}

func (s *Server) handleAcknowledge(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	inc, err := s.Store.AcknowledgeIncident(r.Context(), currentWorkspace(r).ID, id, currentUser(r))
	s.respondIncidentChange(w, r, inc, err, "Only open incidents can be acknowledged.")
}

type resolveRequest struct {
	Note string `json:"note"`
}

func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	var req resolveRequest
	if r.ContentLength != 0 {
		if !decodeJSON(w, r, &req) {
			return
		}
	}
	req.Note = strings.TrimSpace(req.Note)
	if utf8.RuneCountInString(req.Note) > 2000 {
		writeValidation(w, fieldErrors{"note": "is too long"})
		return
	}
	inc, err := s.Store.ResolveIncident(r.Context(), currentWorkspace(r).ID, id, currentUser(r), req.Note)
	if err == nil {
		s.Metrics.IncidentsResolved.WithLabelValues("manual").Inc()
	}
	s.respondIncidentChange(w, r, inc, err, "This incident is already resolved.")
}

func (s *Server) respondIncidentChange(w http.ResponseWriter, r *http.Request, inc store.Incident, err error, conflictMsg string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Incident not found.")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "invalid_state", conflictMsg)
	case err != nil:
		writeInternal(w, r, err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"incident": inc})
	}
}

type noteRequest struct {
	Message string `json:"message"`
}

func (s *Server) handleAddNote(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r, "id")
	if !ok {
		return
	}
	var req noteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	errs := fieldErrors{}
	checkText(errs, "message", req.Message, 1, 2000)
	if len(errs) > 0 {
		writeValidation(w, errs)
		return
	}
	ev, err := s.Store.AddIncidentNote(r.Context(), currentWorkspace(r).ID, id, currentUser(r), req.Message)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "Incident not found.")
		return
	}
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"event": ev})
}
