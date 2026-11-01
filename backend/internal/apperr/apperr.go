// Package apperr defines the application error type. Services return *Error
// (or wrap one); the HTTP layer turns it into a safe JSON response.
package apperr

import (
	"errors"
	"net/http"
	"time"
)

// Error is a user-safe error. Err (if set) is the internal cause and is only
// ever logged, never sent to clients.
type Error struct {
	Status  int               // HTTP status
	Code    string            // stable machine-readable code (lower_snake, e.g. rate_limited)
	Message string            // safe, human-readable message
	Fields  map[string]string // optional per-field validation messages
	Err     error             // internal cause (logged only)

	RetryAfter time.Duration // set on 429s; sent as the Retry-After header
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Code + ": " + e.Err.Error()
	}
	return e.Code + ": " + e.Message
}

func (e *Error) Unwrap() error { return e.Err }

// WithCause attaches an internal cause without changing what the client sees.
func (e *Error) WithCause(err error) *Error {
	c := *e
	c.Err = err
	return &c
}

func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

func BadRequest(code, msg string) *Error { return New(http.StatusBadRequest, code, msg) }
func Unauthorized(msg string) *Error     { return New(http.StatusUnauthorized, "unauthorized", msg) }
func Forbidden(code, msg string) *Error  { return New(http.StatusForbidden, code, msg) }
func NotFound(code, msg string) *Error   { return New(http.StatusNotFound, code, msg) }
func Conflict(code, msg string) *Error   { return New(http.StatusConflict, code, msg) }

// TooManyRequests builds a 429. retryAfter <= 0 falls back to 30 seconds.
func TooManyRequests(retryAfter time.Duration) *Error {
	if retryAfter <= 0 {
		retryAfter = 30 * time.Second
	}
	e := New(http.StatusTooManyRequests, "rate_limited", "Too many requests")
	e.RetryAfter = retryAfter
	return e
}

// Validation builds a 422 with per-field messages.
func Validation(fields map[string]string) *Error {
	return &Error{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: "Invalid input", Fields: fields}
}

// Internal wraps an unexpected error. The client only sees a generic message.
func Internal(err error) *Error {
	return &Error{Status: http.StatusInternalServerError, Code: "internal", Message: "Something went wrong", Err: err}
}

// From converts any error into *Error (unknown errors become Internal).
func From(err error) *Error {
	var ae *Error
	if errors.As(err, &ae) {
		return ae
	}
	return Internal(err)
}
