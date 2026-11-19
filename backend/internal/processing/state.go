// Package processing is the job lifecycle of the media worker: claiming, leasing, heartbeats,
// retry with backoff, cancellation and stale-lease recovery over the existing
// video_processing_jobs table (PostgreSQL, FOR UPDATE SKIP LOCKED). It contains no FFmpeg.
package processing

import (
	"errors"
	"math/rand/v2"
	"time"
)

// Job statuses, equal to the job_status enum in the database.
const (
	Queued     = "QUEUED"
	Processing = "PROCESSING"
	Completed  = "COMPLETED"
	Failed     = "FAILED"
)

// Safe error codes (the only error text a user may ever see). Raw tool output goes to logs, never here.
const (
	CodeCancelled    = "cancelled"
	CodeLeaseExpired = "lease_expired"
	CodeVideoGone    = "video_gone"
	CodeInternal     = "internal"
)

// transitions is the whole state machine. Anything not listed is refused.
//
//	QUEUED     -> PROCESSING  claim
//	QUEUED     -> FAILED      cancelled before it started, or orphaned
//	PROCESSING -> COMPLETED   finished
//	PROCESSING -> FAILED      permanent failure, retries used up, cancelled, lease expired for good
//	PROCESSING -> QUEUED      transient failure (backoff), stale lease recovered, graceful release
//
// COMPLETED and FAILED are terminal: a retry of a failed video creates a new job row.
var transitions = map[string]map[string]bool{
	Queued:     {Processing: true, Failed: true},
	Processing: {Completed: true, Failed: true, Queued: true},
	Completed:  {},
	Failed:     {},
}

// CanTransition reports whether a job may move from one status to another.
func CanTransition(from, to string) bool { return transitions[from][to] }

// Errors returned by the store.
var (
	ErrLeaseLost         = errors.New("processing: the job is no longer leased to this worker")
	ErrInvalidTransition = errors.New("processing: invalid job transition")
	ErrNotFound          = errors.New("processing: job not found")
)

// Job is a claimed job.
type Job struct {
	ID                string
	VideoID           string
	Type              string
	Attempts          int
	MaxAttempts       int
	ProcessingVersion int
	CancelRequestedAt *time.Time
	WorkerID          string
}

// PermanentError marks a failure that retrying cannot fix. Code must be one of the safe codes.
type PermanentError struct {
	Code string
	Err  error
}

func (e *PermanentError) Error() string {
	if e.Err != nil {
		return "permanent failure " + e.Code + ": " + e.Err.Error()
	}
	return "permanent failure " + e.Code
}
func (e *PermanentError) Unwrap() error { return e.Err }

// Permanent wraps err as a permanent failure with a safe code.
func Permanent(code string, err error) error { return &PermanentError{Code: code, Err: err} }

// Backoff returns the delay before attempt number `attempt` (1-based: the delay after the first failure is for
// attempt 1) using exponential growth with full jitter: a random value in [0, min(max, base*2^(attempt-1))].
// A minimum of one second is kept so a failing job never spins. rnd returns a value in [0,1).
func Backoff(attempt int, base, max time.Duration, rnd func() float64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if rnd == nil {
		rnd = rand.Float64
	}
	ceil := base
	for i := 1; i < attempt && ceil < max; i++ {
		ceil *= 2
	}
	if ceil > max {
		ceil = max
	}
	d := time.Duration(rnd() * float64(ceil))
	if d < time.Second {
		d = time.Second
	}
	return d
}
