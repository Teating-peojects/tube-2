package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/redxtube/api/internal/apperr"
	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/repository"
)

// ProcessingStore is implemented by repository.Processing (and by fakes in tests).
type ProcessingStore interface {
	Cancel(ctx context.Context, videoID, actorID string, staff bool, a repository.AuditEntry) error
	Retry(ctx context.Context, videoID string, a repository.AuditEntry) error
	Info(ctx context.Context, videoID string) (repository.ProcInfo, error)
}

// Processing lets a creator cancel their own video's processing and staff cancel or retry any
// (Phase 6). Every action is audited and rate limited.
type Processing struct {
	repo    ProcessingStore
	limiter Limiter
	ipKey   []byte
	log     *slog.Logger
}

func NewProcessing(repo ProcessingStore, limiter Limiter, ipKey []byte, log *slog.Logger) *Processing {
	return &Processing{repo: repo, limiter: limiter, ipKey: ipKey, log: log}
}

func (s *Processing) entry(p *auth.Principal, ci ClientInfo, action, videoID string, meta map[string]any) repository.AuditEntry {
	t := "video"
	return repository.AuditEntry{ActorID: &p.UserID, Role: string(p.Role), Action: action, TargetType: &t, TargetID: &videoID,
		IPHash: auth.HashIP(s.ipKey, ci.IP), RequestID: ci.RequestID, Meta: meta}
}

// Cancel stops processing of a video. The reason is optional for creators and recorded when given.
func (s *Processing) Cancel(ctx context.Context, p *auth.Principal, ci ClientInfo, videoID, reason string) error {
	if !IsUUID(videoID) {
		return errTargetMissing
	}
	meta := map[string]any{}
	if reason != "" {
		r, err := reasonOf(reason)
		if err != nil {
			return err
		}
		meta["reason"] = r
	}
	if err := rateLimit(ctx, s.limiter, s.log, "processing:cancel:"+p.UserID, 20, time.Minute); err != nil {
		return err
	}
	staff := p.Role.AtLeast(auth.RoleModerator)
	meta["by"] = map[bool]string{true: "staff", false: "owner"}[staff]
	return mapErr(s.repo.Cancel(ctx, videoID, p.UserID, staff, s.entry(p, ci, "processing.cancelled", videoID, meta)))
}

// Retry re-queues a FAILED video. Staff only (the route is gated, this re-checks); a reason is required.
func (s *Processing) Retry(ctx context.Context, p *auth.Principal, ci ClientInfo, videoID, reason string) error {
	if !p.Role.AtLeast(auth.RoleModerator) {
		return apperr.Forbidden("forbidden", "Not allowed")
	}
	if !IsUUID(videoID) {
		return errTargetMissing
	}
	r, err := reasonOf(reason)
	if err != nil {
		return err
	}
	if err := rateLimit(ctx, s.limiter, s.log, "processing:retry:"+p.UserID, 30, time.Minute); err != nil {
		return err
	}
	return mapErr(s.repo.Retry(ctx, videoID, s.entry(p, ci, "processing.retried", videoID, map[string]any{"reason": r})))
}

// ProcessingInfo is the body of GET /api/v1/videos/{id}/processing. It never contains storage keys, paths,
// logs or URLs: only a state, a stage, a progress number, a stable code with its creator-facing sentence, and
// what exists so far.
type ProcessingInfo struct {
	Status     string    `json:"status"`          // queued | processing | ready | published | failed | cancelled | removed
	Stage      string    `json:"stage,omitempty"` // inspecting | thumbnails | encoding | packaging
	Progress   int       `json:"progress"`        // 0..100
	ErrorCode  string    `json:"errorCode,omitempty"`
	Message    string    `json:"message,omitempty"`
	Renditions []string  `json:"renditions"`
	Thumbnails int       `json:"thumbnails"`
	Profile    string    `json:"profile,omitempty"` // encoding recipe version that produced the renditions
	CanCancel  bool      `json:"canCancel"`
	CanRetry   bool      `json:"canRetry"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

var stageOf = map[string]string{"METADATA": "inspecting", "THUMBNAIL": "thumbnails", "TRANSCODE": "encoding", "HLS": "packaging"}

// ProcessingInfo shows where a video is in the pipeline to its owner or to staff. Anyone else gets
// target_not_found, exactly like a video that does not exist.
func (s *Processing) ProcessingInfo(ctx context.Context, p *auth.Principal, videoID string) (ProcessingInfo, error) {
	if !IsUUID(videoID) {
		return ProcessingInfo{}, errTargetMissing
	}
	v, err := s.repo.Info(ctx, videoID)
	if err != nil {
		return ProcessingInfo{}, mapErr(err)
	}
	staff := p.Role.AtLeast(auth.RoleModerator)
	if v.Deleted || (!staff && v.CreatorID != p.UserID) {
		return ProcessingInfo{}, errTargetMissing
	}
	out := ProcessingInfo{Renditions: v.Renditions, Thumbnails: v.Thumbnails, Profile: v.Profile, UpdatedAt: v.UpdatedAt}
	if out.Renditions == nil {
		out.Renditions = []string{}
	}
	switch {
	case v.Moderation == "REMOVED":
		out.Status, out.ErrorCode, out.Message = "removed", "removed", "This video was removed by moderation."
	case v.VideoStatus == "PUBLISHED":
		out.Status, out.Progress = "published", 100
	case v.VideoStatus == "READY":
		out.Status, out.Progress = "ready", 100
	case v.VideoStatus == "FAILED":
		out.Status, out.ErrorCode, out.Message = "failed", v.FailCode, failureMessage(v.FailCode)
		if v.FailCode == "cancelled" {
			out.Status = "cancelled"
		}
		out.CanRetry = staff
	case v.VideoStatus == "PROCESSING":
		out.Status, out.Stage = "queued", stageOf[v.ActiveJob]
		if v.ActiveStatus == "PROCESSING" {
			out.Status = "processing"
		}
		out.Progress = min(99, v.JobsDone*100/pipelineJobs)
		out.CanCancel = true
	default: // UPLOADING: the upload screen shows its own byte progress
		out.Status = "queued"
	}
	if out.ErrorCode == "" {
		out.Message = ""
	}
	return out, nil
}
