package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// apiError is the JSON error envelope: {"error": {...}}.
type apiError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("encode response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]apiError{"error": {Code: code, Message: message}})
}

func writeValidation(w http.ResponseWriter, fields fieldErrors) {
	writeJSON(w, http.StatusUnprocessableEntity, map[string]apiError{"error": {
		Code: "validation_failed", Message: "Some fields are invalid.", Fields: fields,
	}})
}

func writeInternal(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("internal error", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "Something went wrong on our side.")
}

const maxBodyBytes = 64 << 10

// decodeJSON strictly decodes a request body into dst. It rejects unknown
// fields, trailing data and oversized bodies.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var (
			syntaxErr *json.SyntaxError
			typeErr   *json.UnmarshalTypeError
			maxErr    *http.MaxBytesError
		)
		msg := "Request body must be valid JSON."
		switch {
		case errors.As(err, &maxErr):
			writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "Request body is too large.")
			return false
		case errors.As(err, &typeErr):
			writeValidation(w, fieldErrors{typeErr.Field: fmt.Sprintf("must be a %s", jsonType(typeErr.Type.Kind().String()))})
			return false
		case errors.As(err, &syntaxErr), errors.Is(err, io.ErrUnexpectedEOF):
		case errors.Is(err, io.EOF):
			msg = "Request body is required."
		case strings.HasPrefix(err.Error(), "json: unknown field"):
			msg = "Unknown field " + strings.TrimPrefix(err.Error(), "json: unknown field ") + "."
		}
		writeError(w, http.StatusBadRequest, "invalid_json", msg)
		return false
	}
	if dec.More() {
		writeError(w, http.StatusBadRequest, "invalid_json", "Request body must contain a single JSON object.")
		return false
	}
	return true
}

func jsonType(kind string) string {
	switch kind {
	case "int", "int64", "int32", "float64":
		return "number"
	case "bool":
		return "boolean"
	case "ptr":
		return "valid value"
	default:
		return kind
	}
}

// idParam parses a positive integer URL parameter.
func idParam(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "not_found", "Not found.")
		return 0, false
	}
	return id, true
}

// fieldErrors maps JSON field names to human-readable problems.
type fieldErrors map[string]string

func (f fieldErrors) add(field, msg string) {
	if _, exists := f[field]; !exists {
		f[field] = msg
	}
}

// optionalString distinguishes "absent" from "null"/"" in PATCH-like bodies.
type optionalString struct {
	Set   bool
	Value string
}

func (o *optionalString) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = ""
		return nil
	}
	return json.Unmarshal(b, &o.Value)
}

func chiParam(r *http.Request, name string) string { return chi.URLParam(r, name) }
