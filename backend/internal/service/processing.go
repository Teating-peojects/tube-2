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
