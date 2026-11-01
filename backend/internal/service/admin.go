package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/redxtube/api/internal/apperr"
	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/model"
	"github.com/redxtube/api/internal/pagination"
	"github.com/redxtube/api/internal/repository"
)

const (
	AdminPageSize     = 25
	MaxSuspendDays    = 365
	DefaultSuspendDay = 7
)

// AdminStore is implemented by repository.Admin (and by fakes in tests).
type AdminStore interface {
	Queue(ctx context.Context, after *repository.ReportCursor, limit int) ([]repository.ReportRow, error)
	Closed(ctx context.Context, after *repository.Keyset, limit int) ([]repository.ReportRow, error)
	AssignReport(ctx context.Context, reportID, moderatorID string, a repository.AuditEntry) error
	RejectReport(ctx context.Context, reportID, moderatorID string, a repository.AuditEntry) error
	ReportTarget(ctx context.Context, reportID string) (string, string, error)
	UserRole(ctx context.Context, userID string) (role, status string, err error)
	Apply(ctx context.Context, in repository.ActionInput) error
	RestoreVideo(ctx context.Context, videoID string, a repository.AuditEntry) error
	RestoreUser(ctx context.Context, userID string, allowBanned bool, a repository.AuditEntry) error
	SetRole(ctx context.Context, userID, role string, a repository.AuditEntry) error
	Users(ctx context.Context, q, status string, after *repository.Keyset, limit int) ([]repository.AdminUserRow, error)
	Audit(ctx context.Context, actionPrefix string, actor *string, beforeID int64, limit int) ([]repository.AuditRow, error)
	BlockHash(ctx context.Context, sha []byte, reason, adminID string, a repository.AuditEntry) (int64, error)
	UnblockHash(ctx context.Context, sha []byte, a repository.AuditEntry) error
	BlockedHashes(ctx context.Context, limit int) ([]repository.BlockedHash, error)
	ExpireSuspensions(ctx context.Context, limit int) (int, error)
}

// Admin implements moderation and administration. Roles (enforced here as well as by the
// routes): MODERATOR handles reports and content/comment/video actions and short suspensions;
// ADMIN also bans, reads users and the audit log, and manages the hash blocklist; SUPER_ADMIN
// also changes roles. Nobody can act on themselves or on an account of equal or higher role.
type Admin struct {
	repo    AdminStore
	limiter Limiter
	ipKey   []byte
	log     *slog.Logger
}

func NewAdmin(repo AdminStore, limiter Limiter, ipKey []byte, log *slog.Logger) *Admin {
	return &Admin{repo: repo, limiter: limiter, ipKey: ipKey, log: log}
}

var (
	errReportNotFound = apperr.NotFound("report_not_found", "Report not found")
	errTargetMissing  = apperr.NotFound("target_not_found", "Target not found")
	errBadState       = apperr.Conflict("invalid_state", "That doesn't apply to the current state")
	errRank           = apperr.Forbidden("forbidden", "You can't act on this account")
)

func (s *Admin) audit(p *auth.Principal, ci ClientInfo, action, targetType, targetID string, meta map[string]any) repository.AuditEntry {
	return repository.AuditEntry{ActorID: &p.UserID, Role: string(p.Role), Action: action, TargetType: &targetType, TargetID: &targetID,
		IPHash: auth.HashIP(s.ipKey, ci.IP), RequestID: ci.RequestID, Meta: meta}
}

func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, repository.ErrTargetNotFound):
		return errTargetMissing
	case errors.Is(err, repository.ErrBadState):
		return errBadState
	}
	return apperr.Internal(err)
}

func (s *Admin) throttle(ctx context.Context, p *auth.Principal) error {
	return rateLimit(ctx, s.limiter, s.log, "admin:write:"+p.UserID, 120, time.Minute)
}

func reasonOf(s string) (string, error) {
	r := strings.TrimSpace(cleanText(s, false))
	if n := utf8.RuneCountInString(r); n < 1 || n > 500 {
		return "", apperr.Validation(map[string]string{"reason": "1-500 characters"})
	}
	return r, nil
}

// ---------- report queue ----------

type reportCursor struct {
	P int    `json:"p"`
	T int64  `json:"t"`
	I string `json:"i"`
}

func shapeReport(r repository.ReportRow) model.AdminReport {
	return model.AdminReport{ID: r.ID, TargetType: strings.ToLower(r.TargetType), TargetID: r.TargetID, Reason: r.Reason, Description: r.Description,
		Status: strings.ToLower(r.Status), Priority: r.Priority, AssignedTo: r.AssignedTo, CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
		Reporter: r.Reporter, OpenForTarget: r.OpenForTarget, Preview: clip(r.Preview, 200)}
}

// Reports lists the open queue (urgent first, oldest first) or the closed reports.
func (s *Admin) Reports(ctx context.Context, status, cursor string) (model.Page[model.AdminReport], error) {
	page := model.Page[model.AdminReport]{Items: []model.AdminReport{}}
	var rows []repository.ReportRow
	var err error
	switch status {
	case "", "open":
		var after *repository.ReportCursor
		if cursor != "" {
			var c reportCursor
			if pagination.Decode(cursor, &c) != nil || c.P < 0 || c.P > 2 || c.T <= 0 || !IsUUID(c.I) {
				return page, badCursor()
			}
			after = &repository.ReportCursor{Priority: c.P, Time: time.UnixMicro(c.T).UTC(), ID: c.I}
		}
		rows, err = s.repo.Queue(ctx, after, AdminPageSize+1)
	case "closed":
		var after *repository.Keyset
		if after, err = decodePage(cursor); err != nil {
			return page, err
		}
		rows, err = s.repo.Closed(ctx, after, AdminPageSize+1)
	default:
		return page, apperr.Validation(map[string]string{"status": "must be open or closed"})
	}
	if err != nil {
		return page, apperr.Internal(err)
	}
	if len(rows) > AdminPageSize {
		rows = rows[:AdminPageSize]
		last := rows[len(rows)-1]
		var next string
		if status == "closed" {
			next, err = pagination.Encode(pageCursor{T: last.CreatedAt.UnixMicro(), I: last.ID})
		} else {
			next, err = pagination.Encode(reportCursor{P: last.Priority, T: last.CreatedAt.UnixMicro(), I: last.ID})
		}
		if err != nil {
			return page, apperr.Internal(err)
		}
		page.NextCursor = &next
	}
	for _, r := range rows {
		page.Items = append(page.Items, shapeReport(r))
	}
	return page, nil
}

func (s *Admin) AssignReport(ctx context.Context, p *auth.Principal, ci ClientInfo, reportID string) error {
	if !IsUUID(reportID) {
		return errReportNotFound
	}
	if err := s.throttle(ctx, p); err != nil {
		return err
	}
	err := s.repo.AssignReport(ctx, reportID, p.UserID, s.audit(p, ci, "report.assign", "report", reportID, nil))
	if errors.Is(err, repository.ErrBadState) {
		return errBadState
	}
	return mapErr(err)
}

// ---------- actions ----------

type ActionInput struct {
	Action string // warn | remove | restrict | suspend | ban | dismiss (reports only)
	Reason string
	Notes  *string
	Days   *int // suspend only
}

// actionType maps the API verb onto the schema's action for a target type ("" = not allowed).
func actionType(target, verb string) string {
	switch target + ":" + verb {
	case "VIDEO:remove":
		return "CONTENT_REMOVED"
	case "VIDEO:restrict":
		return "VIDEO_RESTRICTED"
	case "COMMENT:remove":
		return "COMMENT_REMOVED"
	case "USER:suspend":
		return "USER_SUSPENDED"
	case "USER:ban":
		return "USER_BANNED"
	case "VIDEO:warn", "COMMENT:warn", "USER:warn":
		return "WARNING"
	}
	return ""
}

func (s *Admin) rankCheck(ctx context.Context, p *auth.Principal, userID string) error {
	if userID == p.UserID {
		return errRank
	}
	role, _, err := s.repo.UserRole(ctx, userID)
	if err != nil {
		return apperr.Internal(err)
	}
	if role == "" {
		return errTargetMissing
	}
	if auth.Role(role).AtLeast(p.Role) { // equal or higher
		return errRank
	}
	return nil
}

// Act records a moderation decision, optionally as the resolution of a report.
func (s *Admin) Act(ctx context.Context, p *auth.Principal, ci ClientInfo, reportID *string, targetType, targetID string, in ActionInput) error {
	if reportID != nil && !IsUUID(*reportID) {
		return errReportNotFound
	}
	targetType = strings.ToUpper(targetType)
	if !IsUUID(targetID) {
		return errTargetMissing
	}
	reason, err := reasonOf(in.Reason)
	if err != nil {
		return err
	}
	if in.Notes != nil {
		n := strings.TrimSpace(cleanText(*in.Notes, true))
		if utf8.RuneCountInString(n) > 5000 {
			return apperr.Validation(map[string]string{"notes": "At most 5,000 characters"})
		}
		in.Notes = &n
	}
	if err := s.throttle(ctx, p); err != nil {
		return err
	}

	if in.Action == "dismiss" {
		if reportID == nil {
			return apperr.Validation(map[string]string{"action": "dismiss needs a report"})
		}
		return mapErr(s.repo.RejectReport(ctx, *reportID, p.UserID, s.audit(p, ci, "report.reject", "report", *reportID, map[string]any{"reason": reason})))
	}

	act := actionType(targetType, in.Action)
	if act == "" {
		return apperr.Validation(map[string]string{"action": "That action doesn't apply to a " + strings.ToLower(targetType)})
	}
	var expires *time.Time
	if targetType == "USER" {
		if act == "USER_BANNED" && !p.Role.AtLeast(auth.RoleAdmin) {
			return apperr.Forbidden("forbidden", "Only admins can ban accounts")
		}
		if err := s.rankCheck(ctx, p, targetID); err != nil {
			return err
		}
		if act == "USER_SUSPENDED" {
			days := DefaultSuspendDay
			if in.Days != nil {
				days = *in.Days
			}
			if days < 1 || days > MaxSuspendDays {
				return apperr.Validation(map[string]string{"days": "1-365"})
			}
			t := time.Now().Add(time.Duration(days) * 24 * time.Hour)
			expires = &t
		}
	}
	err = s.repo.Apply(ctx, repository.ActionInput{ModeratorID: p.UserID, ReportID: reportID, TargetType: targetType, TargetID: targetID, Action: act,
		Reason: reason, Notes: in.Notes, ExpiresAt: expires,
		Audit: s.audit(p, ci, "moderation."+strings.ToLower(act), strings.ToLower(targetType), targetID, map[string]any{"reason": reason})})
	return mapErr(err)
}

// ResolveReport applies a decision to the target the report is about.
func (s *Admin) ResolveReport(ctx context.Context, p *auth.Principal, ci ClientInfo, reportID string, in ActionInput) error {
	if !IsUUID(reportID) {
		return errReportNotFound
	}
	tt, tid, err := s.repo.ReportTarget(ctx, reportID)
	if errors.Is(err, repository.ErrTargetNotFound) {
		return errReportNotFound
	}
	if err != nil {
		return apperr.Internal(err)
	}
	return s.Act(ctx, p, ci, &reportID, tt, tid, in)
}

func (s *Admin) RestoreVideo(ctx context.Context, p *auth.Principal, ci ClientInfo, videoID, reason string) error {
	if !IsUUID(videoID) {
		return errTargetMissing
	}
	r, err := reasonOf(reason)
	if err != nil {
		return err
	}
	if err := s.throttle(ctx, p); err != nil {
		return err
	}
	return mapErr(s.repo.RestoreVideo(ctx, videoID, s.audit(p, ci, "moderation.video_restored", "video", videoID, map[string]any{"reason": r})))
}

func (s *Admin) RestoreUser(ctx context.Context, p *auth.Principal, ci ClientInfo, userID, reason string) error {
	if !IsUUID(userID) {
		return errTargetMissing
	}
	r, err := reasonOf(reason)
	if err != nil {
		return err
	}
	if err := s.rankCheck(ctx, p, userID); err != nil {
		return err
	}
	if err := s.throttle(ctx, p); err != nil {
		return err
	}
	return mapErr(s.repo.RestoreUser(ctx, userID, p.Role.AtLeast(auth.RoleAdmin), s.audit(p, ci, "moderation.user_restored", "user", userID, map[string]any{"reason": r})))
}

// ---------- admin only ----------

func (s *Admin) Users(ctx context.Context, q, status, cursor string) (model.Page[model.AdminUser], error) {
	page := model.Page[model.AdminUser]{Items: []model.AdminUser{}}
	q = strings.TrimSpace(q)
	if len(q) > 40 || strings.ContainsAny(q, "%_\\") {
		return page, apperr.Validation(map[string]string{"q": "Use up to 40 letters or digits"})
	}
	status = strings.ToUpper(status)
	if status != "" && status != "ACTIVE" && status != "SUSPENDED" && status != "BANNED" && status != "PENDING" {
		return page, apperr.Validation(map[string]string{"status": "Unknown status"})
	}
	after, err := decodePage(cursor)
	if err != nil {
		return page, err
	}
	rows, err := s.repo.Users(ctx, q, status, after, AdminPageSize+1)
	if err != nil {
		return page, apperr.Internal(err)
	}
	if len(rows) > AdminPageSize {
		rows = rows[:AdminPageSize]
		last := rows[len(rows)-1]
		next, err := pagination.Encode(pageCursor{T: last.CreatedAt.UnixMicro(), I: last.ID})
		if err != nil {
			return page, apperr.Internal(err)
		}
		page.NextCursor = &next
	}
	for _, u := range rows {
		page.Items = append(page.Items, model.AdminUser{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Role: u.Role, Status: u.Status,
			IsCreator: u.IsCreator, CreatedAt: u.CreatedAt.UTC().Format(time.RFC3339)})
	}
	return page, nil
}

// SetRole changes a role between USER, MODERATOR and ADMIN. SUPER_ADMIN is only ever set in the database.
func (s *Admin) SetRole(ctx context.Context, p *auth.Principal, ci ClientInfo, userID, role string) error {
	if !IsUUID(userID) {
		return errTargetMissing
	}
	role = strings.ToUpper(role)
	if role != "USER" && role != "MODERATOR" && role != "ADMIN" {
		return apperr.Validation(map[string]string{"role": "must be user, moderator or admin"})
	}
	if userID == p.UserID {
		return errRank
	}
	if err := s.throttle(ctx, p); err != nil {
		return err
	}
	return mapErr(s.repo.SetRole(ctx, userID, role, s.audit(p, ci, "user.role_changed", "user", userID, map[string]any{"role": role})))
}

type auditCursor struct {
	I int64 `json:"i"`
}

func (s *Admin) Audit(ctx context.Context, action, actor, cursor string) (model.Page[model.AuditEntry], error) {
	page := model.Page[model.AuditEntry]{Items: []model.AuditEntry{}}
	action = strings.TrimSpace(action)
	if len(action) > 64 || strings.ContainsAny(action, "%_\\") {
		return page, apperr.Validation(map[string]string{"action": "Invalid filter"})
	}
	var actorID *string
	if actor != "" {
		if !IsUUID(actor) {
			return page, apperr.Validation(map[string]string{"actor": "Invalid id"})
		}
		actorID = &actor
	}
	var before int64
	if cursor != "" {
		var c auditCursor
		if pagination.Decode(cursor, &c) != nil || c.I <= 0 {
			return page, badCursor()
		}
		before = c.I
	}
	rows, err := s.repo.Audit(ctx, action, actorID, before, AdminPageSize+1)
	if err != nil {
		return page, apperr.Internal(err)
	}
	if len(rows) > AdminPageSize {
		rows = rows[:AdminPageSize]
		next, err := pagination.Encode(auditCursor{I: rows[len(rows)-1].ID})
		if err != nil {
			return page, apperr.Internal(err)
		}
		page.NextCursor = &next
	}
	for _, r := range rows {
		meta := map[string]any{}
		_ = json.Unmarshal([]byte(r.Metadata), &meta)
		page.Items = append(page.Items, model.AuditEntry{ID: strconv.FormatInt(r.ID, 10), ActorID: r.ActorID, Actor: r.ActorName, Action: r.Action,
			TargetType: r.TargetType, TargetID: r.TargetID, Metadata: meta, CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339)})
	}
	return page, nil
}

func parseHash(h string) ([]byte, error) {
	b, err := hex.DecodeString(strings.ToLower(strings.TrimSpace(h)))
	if err != nil || len(b) != 32 {
		return nil, apperr.Validation(map[string]string{"sha256": "Expected 64 hex characters"})
	}
	return b, nil
}

func (s *Admin) BlockHash(ctx context.Context, p *auth.Principal, ci ClientInfo, sha256hex, reason string) (model.HashBlocked, error) {
	b, err := parseHash(sha256hex)
	if err != nil {
		return model.HashBlocked{}, err
	}
	reason = strings.TrimSpace(cleanText(reason, false))
	if n := utf8.RuneCountInString(reason); n < 1 || n > 200 {
		return model.HashBlocked{}, apperr.Validation(map[string]string{"reason": "1-200 characters"})
	}
	if err := s.throttle(ctx, p); err != nil {
		return model.HashBlocked{}, err
	}
	n, err := s.repo.BlockHash(ctx, b, reason, p.UserID, s.audit(p, ci, "moderation.hash_blocked", "hash", hex.EncodeToString(b), map[string]any{"reason": reason}))
	if err != nil {
		return model.HashBlocked{}, apperr.Internal(err)
	}
	return model.HashBlocked{VideosRemoved: n}, nil
}

func (s *Admin) UnblockHash(ctx context.Context, p *auth.Principal, ci ClientInfo, sha256hex string) error {
	b, err := parseHash(sha256hex)
	if err != nil {
		return err
	}
	if err := s.throttle(ctx, p); err != nil {
		return err
	}
	return mapErr(s.repo.UnblockHash(ctx, b, s.audit(p, ci, "moderation.hash_unblocked", "hash", hex.EncodeToString(b), nil)))
}

func (s *Admin) BlockedHashes(ctx context.Context) ([]model.BlockedHash, error) {
	rows, err := s.repo.BlockedHashes(ctx, 500)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := make([]model.BlockedHash, 0, len(rows))
	for _, h := range rows {
		out = append(out, model.BlockedHash{SHA256: hex.EncodeToString(h.SHA256), Reason: h.Reason, CreatedAt: h.CreatedAt.UTC().Format(time.RFC3339)})
	}
	return out, nil
}

// ExpireSuspensions lifts finished temporary suspensions (called by the janitor every minute).
func (s *Admin) ExpireSuspensions(ctx context.Context) (int, error) {
	return s.repo.ExpireSuspensions(ctx, 500)
}
