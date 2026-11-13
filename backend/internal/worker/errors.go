// Package worker is the media pipeline (B6): it claims jobs from video_processing_jobs and turns an
// uploaded original into HLS renditions, thumbnails and metadata.
//
// Pipeline (each step enqueues the next in the same transaction that completes it):
//
//	METADATA   download, SHA-256, ffprobe, validate, video_metadata      (blocked hash => video removed)
//	THUMBNAIL  five frames, the middle one is the default primary
//	TRANSCODE  one HLS rendition per rung of the ladder, uploaded to hls/<id>/<res>/
//	HLS        master.m3u8, videos.status = READY, VIDEO_READY notification
package worker

import (
	"errors"
	"fmt"
)

// JobError is a failure the pipeline understands. Permanent errors (a file that is not a video, a
// blocked hash) are never retried; anything else is retried with backoff until max_attempts.
type JobError struct {
	Code      string // stable machine code, stored in error_message
	Message   string // safe to show to the uploader
	Permanent bool
	Silent    bool // do not notify the creator (the video was deleted or removed meanwhile)
	Err       error
}

func (e *JobError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return e.Code + ": " + e.Message
}

func (e *JobError) Unwrap() error { return e.Err }

func permanent(code, msg string) *JobError {
	return &JobError{Code: code, Message: msg, Permanent: true}
}

func transient(code string, err error) *JobError {
	return &JobError{Code: code, Message: "Temporary processing problem", Err: err}
}

// asJobError wraps any error; unknown errors are transient.
func asJobError(err error) *JobError {
	var je *JobError
	if errors.As(err, &je) {
		return je
	}
	return transient("internal", err)
}

// ErrLeaseLost means this worker no longer owns the job (it was requeued after a missed heartbeat).
var ErrLeaseLost = errors.New("worker: job lease lost")

// ErrVideoGone means the video was deleted, removed or is no longer PROCESSING.
var ErrVideoGone = errors.New("worker: video no longer processable")
