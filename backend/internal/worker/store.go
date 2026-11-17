package worker

import (
	"context"
	"time"
)

// Job is a claimed row of video_processing_jobs.
type Job struct {
	ID, VideoID, Type string
	Attempts          int
	MaxAttempts       int
	WaitSec           float64 // how long the job waited in the queue (since scheduled_at) when it was claimed
}

type VideoRow struct {
	CreatorID, Status, Moderation, OriginalKey string
	Size                                       int64
}

// MetaRow is what METADATA stored; later jobs read it instead of probing again.
type MetaRow struct {
	Width, Height int
	FPS           float64
	HasAudio      bool
	Duration      int // seconds, rounded up
}

type MetadataResult struct {
	Probe     Probe
	VideoType string
	Seconds   int
	SHA256    []byte
}

type ThumbOut struct {
	Key  string
	W, H int
}

type VariantOut struct {
	R     Rendition
	Key   string // directory prefix, e.g. hls/<id>/720p/
	Bytes int64
}

type FailResult struct {
	Final     bool // no more retries: the video is FAILED
	CreatorID string
}

// Store is everything the pipeline needs from PostgreSQL. The PostgreSQL implementation is PG;
// tests use an in-memory fake.
type Store interface {
	Claim(ctx context.Context) (*Job, error)
	Heartbeat(ctx context.Context, j Job) error
	Release(ctx context.Context, j Job) error
	Fail(ctx context.Context, j Job, je *JobError) (FailResult, error)
	RequeueStale(ctx context.Context, after time.Duration) (int, error)

	Video(ctx context.Context, id string) (VideoRow, error)
	Meta(ctx context.Context, id string) (MetaRow, error)

	// SaveMetadata stores the probe result and the file hash, completes METADATA and enqueues
	// THUMBNAIL. blocked is true when the hash is on the blocklist: the database has then already
	// removed the video, the job is NOT completed and the caller must fail it.
	SaveMetadata(ctx context.Context, j Job, m MetadataResult) (blocked bool, err error)
	// RemoveVideo takes a video down because a scanner matched it: moderation_status becomes REMOVED
	// and the match is written to the audit log. The caller then fails the job.
	RemoveVideo(ctx context.Context, j Job, reason string) error
	// ReplaceThumbnails swaps the generated thumbnails, completes THUMBNAIL, enqueues TRANSCODE
	// and returns the storage keys of the thumbnails it replaced.
	ReplaceThumbnails(ctx context.Context, j Job, items []ThumbOut, primary int) (old []string, err error)
	SaveVariant(ctx context.Context, videoID string, v VariantOut) error
	FinishTranscode(ctx context.Context, j Job) error
	Variants(ctx context.Context, videoID string) (ready []string, err error)
	// FinishHLS sets hls_master_key and the encoding profile version, moves the video to READY,
	// completes HLS and notifies the creator.
	FinishHLS(ctx context.Context, j Job, masterKey, profile string) (creatorID string, err error)

	// ClaimFailedForCleanup marks up to limit FAILED videos (failed longer than olderThan ago, no
	// active job, not yet cleaned) as being cleaned and returns their ids. The caller deletes their
	// partial HLS output, or calls ReleaseCleanup to try again later. The original is never touched.
	ClaimFailedForCleanup(ctx context.Context, olderThan time.Duration, limit int) ([]string, error)
	ReleaseCleanup(ctx context.Context, videoID string) error
}

// backoff is 30s, 60s, 120s ... capped at 10 minutes.
func backoff(attempts int) time.Duration {
	d := 30 * time.Second
	for i := 1; i < attempts && d < 10*time.Minute; i++ {
		d *= 2
	}
	return min(d, 10*time.Minute)
}
