package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Admin is the moderation and administration data layer. Every state change runs in
// one transaction together with its append-only record (moderation_actions and/or
// audit_logs), so an action can never exist without its evidence.
type Admin struct{ pool *pgxpool.Pool }

func NewAdmin(pool *pgxpool.Pool) *Admin { return &Admin{pool: pool} }

var (
	ErrTargetNotFound = errors.New("target not found")
	ErrBadState       = errors.New("action does not apply in the current state")
)

const reportTarget = `COALESCE(r.target_video_id, r.target_comment_id, r.target_user_id)`

const reportCols = `r.id::text, r.target_type::text, ` + reportTarget + `::text, r.reason::text, COALESCE(r.description, ''), r.status::text,
  r.priority::int, r.assigned_to::text, r.created_at, u.username,
  (SELECT count(*) FROM reports o WHERE o.status IN ('PENDING', 'UNDER_REVIEW') AND o.target_type = r.target_type
     AND COALESCE(o.target_video_id, o.target_comment_id, o.target_user_id) = ` + reportTarget + `)::int,
  COALESCE(v.title, left(c.content, 200), tu.username, '')`

const reportFrom = ` FROM reports r
LEFT JOIN users u ON u.id = r.reporter_user_id
LEFT JOIN videos v ON v.id = r.target_video_id
LEFT JOIN comments c ON c.id = r.target_comment_id
LEFT JOIN users tu ON tu.id = r.target_user_id`

const (
	// Open queue: urgent first, oldest first (matches the reports_queue index). $1 priority, $2 time, $3 id, $4 limit.
	qAdminQueue = `SELECT ` + reportCols + reportFrom + `
WHERE r.status IN ('PENDING', 'UNDER_REVIEW')
  AND ($2::timestamptz IS NULL OR (r.priority, r.created_at, r.id) > ($1::smallint, $2::timestamptz, $3::uuid))
ORDER BY r.priority, r.created_at, r.id LIMIT $4::int`

	// Closed reports, newest resolution first. $1 time, $2 id, $3 limit.
	qAdminClosed = `SELECT ` + reportCols + reportFrom + `
WHERE r.status IN ('RESOLVED', 'REJECTED')
  AND ($1::timestamptz IS NULL OR (r.resolved_at, r.id) < ($1::timestamptz, $2::uuid))
ORDER BY r.resolved_at DESC, r.id DESC LIMIT $3::int`

	qAdminAssign = `UPDATE reports SET status = 'UNDER_REVIEW', assigned_to = $2::uuid, updated_at = now()
WHERE id = $1::uuid AND status IN ('PENDING', 'UNDER_REVIEW')`

	qAdminCloseReport = `UPDATE reports SET status = $2::report_status, resolved_by = $3::uuid, resolved_at = now(), updated_at = now()
WHERE id = $1::uuid AND status IN ('PENDING', 'UNDER_REVIEW')`

	// every other open report about the same target is settled by the same decision
	qAdminCloseSiblings = `UPDATE reports SET status = 'RESOLVED', resolved_by = $3::uuid, resolved_at = now(), updated_at = now()
WHERE status IN ('PENDING', 'UNDER_REVIEW') AND target_type = $1::report_target
  AND COALESCE(target_video_id, target_comment_id, target_user_id) = $2::uuid`

	qAdminReportTarget = `SELECT target_type::text, COALESCE(target_video_id, target_comment_id, target_user_id)::text FROM reports WHERE id = $1::uuid`

	// who owns the target (to notify them, and to check roles for user actions)
	qAdminOwnerVideo   = `SELECT creator_id::text FROM videos WHERE id = $1::uuid`
	qAdminOwnerComment = `SELECT user_id::text FROM comments WHERE id = $1::uuid`
	qAdminUserRole     = `SELECT role::text, status::text FROM users WHERE id = $1::uuid AND deleted_at IS NULL`

	qAdminRemoveVideo   = `UPDATE videos SET moderation_status = 'REMOVED', updated_at = now() WHERE id = $1::uuid AND moderation_status <> 'REMOVED'`
	qAdminRestrictVideo = `UPDATE videos SET moderation_status = 'RESTRICTED', updated_at = now() WHERE id = $1::uuid AND moderation_status = 'OK'`
	qAdminRestoreVideo  = `UPDATE videos SET moderation_status = 'OK', updated_at = now() WHERE id = $1::uuid AND moderation_status <> 'OK'`
	qAdminRemoveComment = `UPDATE comments SET status = 'DELETED', deleted_at = now(), updated_at = now() WHERE id = $1::uuid AND status <> 'DELETED'`
	qAdminSetUserStatus = `UPDATE users SET status = $2::user_status, updated_at = now() WHERE id = $1::uuid AND deleted_at IS NULL AND status = ANY($3::user_status[])`
	qAdminRevoke        = `UPDATE sessions SET revoked_at = now() WHERE user_id = $1::uuid AND revoked_at IS NULL`

	qAdminInsertAction = `INSERT INTO moderation_actions (moderator_id, report_id, target_type, target_video_id, target_comment_id, target_user_id, action, reason, notes, expires_at)
VALUES ($1::uuid, $2::uuid, $3::report_target, $4::uuid, $5::uuid, $6::uuid, $7::moderation_action_type, $8, $9, $10)`

	qAdminNotify = `INSERT INTO notifications (user_id, type, video_id, payload) VALUES ($1::uuid, 'MODERATION', $2::uuid, $3::jsonb)`

	qAdminUsers = `SELECT id::text, username, display_name, role::text, status::text, is_creator, created_at FROM users
WHERE deleted_at IS NULL AND ($1::text = '' OR username ILIKE $1::text || '%') AND ($2::text = '' OR status::text = $2::text)
  AND ($3::timestamptz IS NULL OR (created_at, id) < ($3::timestamptz, $4::uuid))
ORDER BY created_at DESC, id DESC LIMIT $5::int`

	qAdminSetRole = `UPDATE users SET role = $2::user_role, updated_at = now() WHERE id = $1::uuid AND deleted_at IS NULL AND role <> 'SUPER_ADMIN'`

	qAdminAudit = `SELECT a.id, a.actor_user_id::text, u.username, a.action, a.target_type, a.target_id, a.metadata::text, a.created_at
FROM audit_logs a LEFT JOIN users u ON u.id = a.actor_user_id
WHERE ($1::text = '' OR a.action LIKE $1::text || '%') AND ($2::uuid IS NULL OR a.actor_user_id = $2::uuid)
  AND ($3::bigint = 0 OR a.id < $3::bigint)
ORDER BY a.id DESC LIMIT $4::int`

	qAdminHashAdd          = `INSERT INTO blocked_hashes (sha256, reason, added_by) VALUES ($1, $2, $3::uuid) ON CONFLICT (sha256) DO NOTHING`
	qAdminHashRemove       = `DELETE FROM blocked_hashes WHERE sha256 = $1`
	qAdminHashRemoveVideos = `UPDATE videos SET moderation_status = 'REMOVED', updated_at = now() WHERE content_sha256 = $1 AND moderation_status <> 'REMOVED'`
	qAdminHashList         = `SELECT sha256, reason, created_at FROM blocked_hashes ORDER BY created_at DESC LIMIT $1::int`
)

type ReportRow struct {
	ID, TargetType, TargetID, Reason, Description, Status string
	Priority                                              int
	AssignedTo                                            *string
	CreatedAt                                             time.Time
	Reporter                                              *string
	OpenForTarget                                         int
	Preview                                               string
}

func scanReports(rows pgx.Rows) ([]ReportRow, error) {
	defer rows.Close()
	var out []ReportRow
	for rows.Next() {
		var x ReportRow
		if err := rows.Scan(&x.ID, &x.TargetType, &x.TargetID, &x.Reason, &x.Description, &x.Status, &x.Priority, &x.AssignedTo, &x.CreatedAt, &x.Reporter, &x.OpenForTarget, &x.Preview); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// ReportCursor is the position in the open queue.
type ReportCursor struct {
	Priority int
	Time     time.Time
	ID       string
}

func (r *Admin) Queue(ctx context.Context, after *ReportCursor, limit int) ([]ReportRow, error) {
	var ts *time.Time
	var pr int
	var id *string
	if after != nil {
		ts, pr, id = &after.Time, after.Priority, &after.ID
	}
	rows, err := r.pool.Query(ctx, qAdminQueue, pr, ts, id, limit)
	if err != nil {
		return nil, err
	}
	return scanReports(rows)
}

func (r *Admin) Closed(ctx context.Context, after *Keyset, limit int) ([]ReportRow, error) {
	var ts *time.Time
	var id *string
	if after != nil {
		ts, id = &after.Time, &after.ID
	}
	rows, err := r.pool.Query(ctx, qAdminClosed, ts, id, limit)
	if err != nil {
		return nil, err
	}
	return scanReports(rows)
}

// AssignReport claims a report for a moderator.
func (r *Admin) AssignReport(ctx context.Context, reportID, moderatorID string, a AuditEntry) error {
	return r.one(ctx, a, ErrBadState, qAdminAssign, reportID, moderatorID)
}

// RejectReport closes a report with no action ("not a violation").
func (r *Admin) RejectReport(ctx context.Context, reportID, moderatorID string, a AuditEntry) error {
	return r.one(ctx, a, ErrBadState, qAdminCloseReport, reportID, "REJECTED", moderatorID)
}

// one runs a single UPDATE that must touch exactly one row, plus its audit record.
func (r *Admin) one(ctx context.Context, a AuditEntry, miss error, sql string, args ...any) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return miss
	}
	if err = audit(ctx, tx, a); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ReportTarget returns what a report is about.
func (r *Admin) ReportTarget(ctx context.Context, reportID string) (targetType, targetID string, err error) {
	err = r.pool.QueryRow(ctx, qAdminReportTarget, reportID).Scan(&targetType, &targetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrTargetNotFound
	}
	return
}

// UserRole returns the role and status of an account ("" when it does not exist).
func (r *Admin) UserRole(ctx context.Context, userID string) (role, status string, err error) {
	err = r.pool.QueryRow(ctx, qAdminUserRole, userID).Scan(&role, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	return
}

// ActionInput is one moderation decision.
type ActionInput struct {
	ModeratorID string
	ReportID    *string // closes this report (and every other open report on the same target)
	TargetType  string  // VIDEO | COMMENT | USER
	TargetID    string
	Action      string // moderation_action_type
	Reason      string
	Notes       *string
	ExpiresAt   *time.Time
	Audit       AuditEntry
}

// Apply performs the action's effect, records it (append-only), closes the report(s) and tells
// the affected owner, all in one transaction. ErrTargetNotFound / ErrBadState mean nothing changed.
func (r *Admin) Apply(ctx context.Context, in ActionInput) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var owner string
	var video, comment, user any
	switch in.TargetType {
	case "VIDEO":
		video = in.TargetID
		err = tx.QueryRow(ctx, qAdminOwnerVideo, in.TargetID).Scan(&owner)
	case "COMMENT":
		comment = in.TargetID
		err = tx.QueryRow(ctx, qAdminOwnerComment, in.TargetID).Scan(&owner)
	default:
		user = in.TargetID
		owner = in.TargetID
		var role string
		err = tx.QueryRow(ctx, qAdminUserRole, in.TargetID).Scan(&role, new(string))
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTargetNotFound
	}
	if err != nil {
		return err
	}

	var tag interface{ RowsAffected() int64 }
	switch in.Action {
	case "CONTENT_REMOVED":
		if in.TargetType == "VIDEO" {
			tag, err = tx.Exec(ctx, qAdminRemoveVideo, in.TargetID)
		} else {
			tag, err = tx.Exec(ctx, qAdminRemoveComment, in.TargetID)
		}
	case "COMMENT_REMOVED":
		tag, err = tx.Exec(ctx, qAdminRemoveComment, in.TargetID)
	case "VIDEO_RESTRICTED":
		tag, err = tx.Exec(ctx, qAdminRestrictVideo, in.TargetID)
	case "USER_SUSPENDED":
		tag, err = tx.Exec(ctx, qAdminSetUserStatus, in.TargetID, "SUSPENDED", []string{"ACTIVE", "PENDING"})
	case "USER_BANNED":
		tag, err = tx.Exec(ctx, qAdminSetUserStatus, in.TargetID, "BANNED", []string{"ACTIVE", "PENDING", "SUSPENDED"})
	case "WARNING":
		tag = rowsOne{} // no state change: a warning is a recorded message
	default:
		return ErrBadState
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrBadState
	}
	if in.Action == "USER_SUSPENDED" || in.Action == "USER_BANNED" {
		if _, err = tx.Exec(ctx, qAdminRevoke, in.TargetID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, qAdminInsertAction, in.ModeratorID, in.ReportID, in.TargetType, video, comment, user, in.Action, in.Reason, in.Notes, in.ExpiresAt); err != nil {
		return err
	}
	if in.ReportID != nil {
		if _, err = tx.Exec(ctx, qAdminCloseReport, *in.ReportID, "RESOLVED", in.ModeratorID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, qAdminCloseSiblings, in.TargetType, in.TargetID, in.ModeratorID); err != nil {
		return err
	}
	var vid any
	if in.TargetType == "VIDEO" {
		vid = in.TargetID
	}
	if _, err = tx.Exec(ctx, qAdminNotify, owner, vid, notifyPayload(in.Action, in.Reason)); err != nil {
		return err
	}
	if err = audit(ctx, tx, in.Audit); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type rowsOne struct{}

func (rowsOne) RowsAffected() int64 { return 1 }

var noticeTitle = map[string]string{
	"WARNING": "You received a warning", "CONTENT_REMOVED": "Your content was removed", "COMMENT_REMOVED": "Your comment was removed",
	"VIDEO_RESTRICTED": "Your video was restricted", "USER_SUSPENDED": "Your account was suspended", "USER_BANNED": "Your account was banned",
}

func notifyPayload(action, reason string) map[string]string {
	return map[string]string{"title": noticeTitle[action], "body": reason}
}

// RestoreVideo lifts a restriction or removal (audit only: there is no "restored" action type).
func (r *Admin) RestoreVideo(ctx context.Context, videoID string, a AuditEntry) error {
	return r.one(ctx, a, ErrBadState, qAdminRestoreVideo, videoID)
}

// RestoreUser reactivates a suspended account. Bans are lifted only when allowBanned is set.
func (r *Admin) RestoreUser(ctx context.Context, userID string, allowBanned bool, a AuditEntry) error {
	from := []string{"SUSPENDED"}
	if allowBanned {
		from = append(from, "BANNED")
	}
	return r.one(ctx, a, ErrBadState, qAdminSetUserStatus, userID, "ACTIVE", from)
}

// SetRole changes an account's role (never touches SUPER_ADMIN accounts).
func (r *Admin) SetRole(ctx context.Context, userID, role string, a AuditEntry) error {
	return r.one(ctx, a, ErrTargetNotFound, qAdminSetRole, userID, role)
}

type AdminUserRow struct {
	ID, Username, DisplayName, Role, Status string
	IsCreator                               bool
	CreatedAt                               time.Time
}

func (r *Admin) Users(ctx context.Context, q, status string, after *Keyset, limit int) ([]AdminUserRow, error) {
	var ts *time.Time
	var id *string
	if after != nil {
		ts, id = &after.Time, &after.ID
	}
	rows, err := r.pool.Query(ctx, qAdminUsers, q, status, ts, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminUserRow
	for rows.Next() {
		var u AdminUserRow
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.Status, &u.IsCreator, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

type AuditRow struct {
	ID                                       int64
	ActorID, ActorName, TargetType, TargetID *string
	Action, Metadata                         string
	CreatedAt                                time.Time
}

func (r *Admin) Audit(ctx context.Context, actionPrefix string, actor *string, beforeID int64, limit int) ([]AuditRow, error) {
	rows, err := r.pool.Query(ctx, qAdminAudit, actionPrefix, actor, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditRow
	for rows.Next() {
		var a AuditRow
		if err := rows.Scan(&a.ID, &a.ActorID, &a.ActorName, &a.Action, &a.TargetType, &a.TargetID, &a.Metadata, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type BlockedHash struct {
	SHA256    []byte
	Reason    string
	CreatedAt time.Time
}

// BlockHash puts a file hash on the blocklist and removes every existing video with it.
// It returns how many videos were removed.
func (r *Admin) BlockHash(ctx context.Context, sha []byte, reason, adminID string, a AuditEntry) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, qAdminHashAdd, sha, reason, adminID); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, qAdminHashRemoveVideos, sha)
	if err != nil {
		return 0, err
	}
	if a.Meta == nil {
		a.Meta = map[string]any{}
	}
	a.Meta["videos_removed"] = tag.RowsAffected()
	if err = audit(ctx, tx, a); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), tx.Commit(ctx)
}

func (r *Admin) UnblockHash(ctx context.Context, sha []byte, a AuditEntry) error {
	return r.one(ctx, a, ErrTargetNotFound, qAdminHashRemove, sha)
}

func (r *Admin) BlockedHashes(ctx context.Context, limit int) ([]BlockedHash, error) {
	rows, err := r.pool.Query(ctx, qAdminHashList, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BlockedHash
	for rows.Next() {
		var h BlockedHash
		if err := rows.Scan(&h.SHA256, &h.Reason, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ExpireSuspensions lifts suspensions whose end date has passed (database function, safe on every instance).
func (r *Admin) ExpireSuspensions(ctx context.Context, limit int) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT expire_user_suspensions($1::int)`, limit).Scan(&n)
	return n, err
}
