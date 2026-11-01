// Package httpx holds HTTP helpers: JSON responses, centralized error
// handling and the middleware chain.
//
// Wire format (matches the existing frontend api() client):
//   - success: the JSON payload itself, e.g. {"items":[...],"nextCursor":null}
//     (204 with no body for actions that return nothing)
//   - error:   {"code":"video_not_found","message":"...","fields":{...}}
package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/redxtube/api/internal/apperr"
)

// JSON writes v with the given status.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// NoContent writes 204.
func NoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

type errorBody struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// Error writes a safe error response and logs internal causes server-side.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	ae := apperr.From(err)
	if ae.Status >= 500 {
		slog.ErrorContext(r.Context(), "request failed",
			"request_id", RequestIDFrom(r.Context()), "code", ae.Code, "err", err.Error())
	} else if ae.Err != nil {
		slog.DebugContext(r.Context(), "request rejected",
			"request_id", RequestIDFrom(r.Context()), "code", ae.Code, "err", ae.Err.Error())
	}
	if ae.Status == http.StatusTooManyRequests {
		// The frontend reads Retry-After (whole seconds) to show a countdown.
		secs := int64((ae.RetryAfter + time.Second - 1) / time.Second)
		if secs < 1 {
			secs = 30
		}
		w.Header().Set("Retry-After", strconv.FormatInt(secs, 10))
	}
	JSON(w, ae.Status, errorBody{Code: ae.Code, Message: ae.Message, Fields: ae.Fields})
}

// HandlerFunc is an http handler that returns an error. Handlers stay thin:
// parse input -> call a service -> write the result.
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// Handle adapts a HandlerFunc, routing returned errors through Error.
func Handle(h HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			Error(w, r, err)
		}
	}
}

// DecodeJSON parses a JSON body strictly (unknown fields rejected, single value).
func DecodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return apperr.New(http.StatusRequestEntityTooLarge, "too_large", "Request body too large")
		}
		return apperr.BadRequest("invalid_json", "Invalid JSON body").WithCause(err)
	}
	if dec.More() {
		return apperr.BadRequest("invalid_json", "Invalid JSON body")
	}
	return nil
}
