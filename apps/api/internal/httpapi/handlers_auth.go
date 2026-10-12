package httpapi

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/temitopeagbola/pulseops/apps/api/internal/auth"
	"github.com/temitopeagbola/pulseops/apps/api/internal/store"
)

type sessionResponse struct {
	Token     string          `json:"token"`
	ExpiresAt time.Time       `json:"expiresAt"`
	User      store.User      `json:"user"`
	Workspace store.Workspace `json:"workspace"`
}

type registerRequest struct {
	Name          string `json:"name"`
	Email         string `json:"email"`
	Password      string `json:"password"`
	WorkspaceName string `json:"workspaceName"`
}

func validEmail(s string) bool {
	if len(s) > 254 {
		return false
	}
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s && strings.Contains(s[strings.LastIndex(s, "@"):], ".")
}

func checkText(f fieldErrors, field, value string, minLen, maxLen int) {
	n := utf8.RuneCountInString(value)
	switch {
	case n < minLen && minLen == 1:
		f.add(field, "is required")
	case n < minLen:
		f.add(field, "is too short")
	case n > maxLen:
		f.add(field, "is too long")
	}
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.WorkspaceName = strings.TrimSpace(req.WorkspaceName)

	errs := fieldErrors{}
	checkText(errs, "name", req.Name, 1, 80)
	if !validEmail(req.Email) {
		errs.add("email", "must be a valid email address")
	}
	if n := utf8.RuneCountInString(req.Password); n < 8 {
		errs.add("password", "must be at least 8 characters")
	} else if len(req.Password) > 72 {
		errs.add("password", "must be at most 72 bytes")
	}
	if req.WorkspaceName == "" {
		req.WorkspaceName = req.Name + "'s workspace"
	}
	checkText(errs, "workspaceName", req.WorkspaceName, 1, 80)
	if len(errs) > 0 {
		writeValidation(w, errs)
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	user, ws, err := s.Store.Register(r.Context(), req.WorkspaceName, req.Email, req.Name, hash)
	if errors.Is(err, store.ErrEmailTaken) {
		writeValidation(w, fieldErrors{"email": "is already registered"})
		return
	}
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	s.issueSession(w, r, http.StatusCreated, user, ws, s.Config.SessionTTL)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	email := strings.TrimSpace(req.Email)
	if email == "" || req.Password == "" || len(req.Password) > 72 {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", auth.ErrInvalidCredentials.Error())
		return
	}
	user, err := s.Store.UserByEmail(r.Context(), email)
	if errors.Is(err, store.ErrNotFound) {
		auth.EqualiseTiming(req.Password)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", auth.ErrInvalidCredentials.Error())
		return
	}
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	if !auth.CheckPassword(user.PasswordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", auth.ErrInvalidCredentials.Error())
		return
	}
	ws, err := s.Store.Workspace(r.Context(), user.WorkspaceID)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	s.issueSession(w, r, http.StatusOK, user, ws, s.Config.SessionTTL)
}

// handleDemoLogin signs the caller into the shared, seeded demo account.
func (s *Server) handleDemoLogin(w http.ResponseWriter, r *http.Request) {
	if !s.Config.DemoEnabled || s.DemoEmail == "" {
		writeError(w, http.StatusNotFound, "demo_disabled", "The demo account is not enabled on this server.")
		return
	}
	user, err := s.Store.UserByEmail(r.Context(), s.DemoEmail)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusServiceUnavailable, "demo_not_seeded", "The demo account has not been seeded yet. Try again in a moment.")
		return
	}
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	ws, err := s.Store.Workspace(r.Context(), user.WorkspaceID)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	s.issueSession(w, r, http.StatusOK, user, ws, 24*time.Hour)
}

func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, status int, user store.User, ws store.Workspace, ttl time.Duration) {
	token, hash, err := auth.NewToken()
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	expires := time.Now().Add(ttl).UTC().Truncate(time.Second)
	if err := s.Store.CreateSession(r.Context(), hash, user.ID, expires); err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, status, sessionResponse{Token: token, ExpiresAt: expires, User: user, Workspace: ws})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token, _ := bearerToken(r)
	if err := s.Store.DeleteSession(r.Context(), auth.HashToken(token)); err != nil {
		writeInternal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"user": currentUser(r), "workspace": currentWorkspace(r)})
}
