package repository

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrConflict is returned when an email or username is already taken.
var ErrConflict = errors.New("email or username already in use")

type Auth struct{ pool *pgxpool.Pool }

func NewAuth(pool *pgxpool.Pool) *Auth { return &Auth{pool: pool} }

type UserRow struct {
	ID, Email, Username, DisplayName, Bio, Role, Status string
	AvatarKey                                           *string
	EmailVerified, IsCreator                            bool
	JoinedAt                                            time.Time
}

// UserAuth is what login and password checks need.
type UserAuth struct {
	ID, Email, Username, PasswordHash, Status, Role string
	EmailVerified, IsCreator                        bool
}

type SessionRow struct {
	ID, UserID, Username, Role, Status string
	EmailVerified, IsCreator           bool
	LastUsedAt, ExpiresAt, CreatedAt   time.Time
}

type NewSession struct {
	ID        string
	TokenHash []byte
	UserAgent *string
	IPPrefix  *string // /24 (IPv4) or /56 (IPv6): all the schema allows
	ExpiresAt time.Time
}

// AuditEntry is one append-only audit_logs row. Role and request id go into the
// metadata object; the schema stores only an HMAC of the IP.
type AuditEntry struct {
	ActorID    *string
	Role       string
	Action     string
	TargetType *string
	TargetID   *string
	IPHash     []byte
	RequestID  string
	Meta       map[string]any // extra, non-secret facts (the table rejects keys like password/token)
}

// IPPtr returns nil unless s is a valid IP.
func IPPtr(s string) *string {
	if net.ParseIP(s) == nil {
		return nil
	}
	return &s
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func audit(ctx context.Context, q execer, e AuditEntry) error {
	meta := map[string]any{}
	for k, v := range e.Meta {
		meta[k] = v
	}
	if e.RequestID != "" {
		meta["request_id"] = e.RequestID
	}
	if e.Role != "" {
		meta["role"] = e.Role
	}
	_, err := q.Exec(ctx, `INSERT INTO audit_logs (actor_user_id, action, target_type, target_id, metadata, ip_hash)
VALUES ($1::uuid, $2, $3, $4, $5, $6)`, e.ActorID, e.Action, e.TargetType, e.TargetID, meta, e.IPHash)
	return err
}

func (r *Auth) Audit(ctx context.Context, e AuditEntry) error { return audit(ctx, r.pool, e) }

func insertSession(ctx context.Context, q execer, userID string, s NewSession) error {
	_, err := q.Exec(ctx, `INSERT INTO sessions (id, user_id, session_token_hash, ip_prefix, user_agent, expires_at)
VALUES ($1::uuid, $2::uuid, $3, $4::cidr, $5, $6)`, s.ID, userID, s.TokenHash, s.IPPrefix, s.UserAgent, s.ExpiresAt)
	return err
}

func isUnique(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

// ---------- registration ----------

type RegisterParams struct {
	ID, Email, Username, PasswordHash string
	OTPID                             string
	OTPDigest                         []byte
	OTPExpires                        time.Time
	Session                           NewSession
	AgeExpires                        time.Time
	IPHash                            []byte
	RequestID                         string
}

// Register creates the account (status PENDING until the email is verified),
// the age record, the verification code and the first session in one
// transaction. Triggers create the profile and settings rows.
func (r *Auth) Register(ctx context.Context, p RegisterParams) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err = tx.Exec(ctx, `INSERT INTO users (id, username, email, password_hash, display_name) VALUES ($1::uuid, $2, $3, $4, $5)`,
		p.ID, p.Username, p.Email, p.PasswordHash, p.Username); err != nil {
		if isUnique(err) {
			return ErrConflict
		}
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO age_verifications (user_id, status, method, verified_at, expires_at)
VALUES ($1::uuid, 'VERIFIED', 'SELF_DECLARATION', now(), $2)`, p.ID, p.AgeExpires); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO verification_tokens (id, user_id, purpose, token_hash, target, max_attempts, expires_at)
VALUES ($1::uuid, $2::uuid, 'EMAIL_VERIFY', $3, $4, 5, $5)`, p.OTPID, p.ID, p.OTPDigest, p.Email, p.OTPExpires); err != nil {
		return err
	}
	if err = insertSession(ctx, tx, p.ID, p.Session); err != nil {
		return err
	}
	target := "user"
	if err = audit(ctx, tx, AuditEntry{ActorID: &p.ID, Role: "USER", Action: "auth.register", TargetType: &target, TargetID: &p.ID, IPHash: p.IPHash, RequestID: p.RequestID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ---------- lookups ----------

const userAuthCols = `u.id::text, u.email::text, u.username::text, u.password_hash, u.status::text, u.role::text, u.is_verified, u.is_creator`

func scanUserAuth(row pgx.Row) (*UserAuth, error) {
	var u UserAuth
	err := row.Scan(&u.ID, &u.Email, &u.Username, &u.PasswordHash, &u.Status, &u.Role, &u.EmailVerified, &u.IsCreator)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &u, err
}

// UserByEmail ignores deleted accounts.
func (r *Auth) UserByEmail(ctx context.Context, email string) (*UserAuth, error) {
	return scanUserAuth(r.pool.QueryRow(ctx, `SELECT `+userAuthCols+` FROM users u WHERE u.email = $1 AND u.deleted_at IS NULL`, email))
}

func (r *Auth) UserByUsername(ctx context.Context, username string) (*UserAuth, error) {
	return scanUserAuth(r.pool.QueryRow(ctx, `SELECT `+userAuthCols+` FROM users u WHERE u.username = $1 AND u.deleted_at IS NULL`, username))
}

func (r *Auth) UserAuthByID(ctx context.Context, id string) (*UserAuth, error) {
	return scanUserAuth(r.pool.QueryRow(ctx, `SELECT `+userAuthCols+` FROM users u WHERE u.id = $1::uuid AND u.deleted_at IS NULL`, id))
}

func (r *Auth) UserByID(ctx context.Context, id string) (*UserRow, error) {
	var u UserRow
	err := r.pool.QueryRow(ctx, `SELECT u.id::text, u.email::text, u.username::text, u.display_name, p.bio, u.role::text, u.status::text,
  p.avatar_storage_key, u.is_verified, u.is_creator, u.created_at
FROM users u JOIN user_profiles p ON p.user_id = u.id WHERE u.id = $1::uuid AND u.deleted_at IS NULL`, id).
		Scan(&u.ID, &u.Email, &u.Username, &u.DisplayName, &u.Bio, &u.Role, &u.Status, &u.AvatarKey, &u.EmailVerified, &u.IsCreator, &u.JoinedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &u, err
}

// ---------- sessions ----------

func (r *Auth) CreateSession(ctx context.Context, userID string, s NewSession) error {
	return insertSession(ctx, r.pool, userID, s)
}

func (r *Auth) MarkLogin(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1::uuid`, userID)
	return err
}

// Session resolves a live session by the SHA-256 of its cookie token. A session
// also dies absoluteSecs after it was created, no matter how often it slides.
func (r *Auth) Session(ctx context.Context, tokenHash []byte, absoluteSecs float64) (*SessionRow, error) {
	var s SessionRow
	err := r.pool.QueryRow(ctx, `SELECT s.id::text, u.id::text, u.username::text, u.role::text, u.status::text,
  u.is_verified, u.is_creator, s.last_used_at, s.expires_at, s.created_at
FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.session_token_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > now()
  AND s.created_at + make_interval(secs => $2) > now() AND u.deleted_at IS NULL`, tokenHash, absoluteSecs).
		Scan(&s.ID, &s.UserID, &s.Username, &s.Role, &s.Status, &s.EmailVerified, &s.IsCreator, &s.LastUsedAt, &s.ExpiresAt, &s.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}

// TouchSession slides the expiry forward (never past the absolute cap).
func (r *Auth) TouchSession(ctx context.Context, id string, newExpiry time.Time, absoluteSecs float64) error {
	_, err := r.pool.Exec(ctx, `UPDATE sessions SET last_used_at = now(),
  expires_at = LEAST($2, created_at + make_interval(secs => $3)) WHERE id = $1::uuid`, id, newExpiry, absoluteSecs)
	return err
}

func (r *Auth) RevokeSession(ctx context.Context, tokenHash []byte) error {
	_, err := r.pool.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE session_token_hash = $1 AND revoked_at IS NULL`, tokenHash)
	return err
}

// RevokeUserSessions signs a user out everywhere (optionally keeping one session).
func (r *Auth) RevokeUserSessions(ctx context.Context, userID string, except *string, a AuditEntry) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1::uuid AND revoked_at IS NULL AND ($2::uuid IS NULL OR id <> $2::uuid)`, userID, except); err != nil {
		return err
	}
	if err = audit(ctx, tx, a); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ---------- one-time tokens ----------

// ReplaceToken invalidates any live token of this purpose and stores a new one.
// target is the email being verified (nil for password resets).
func (r *Auth) ReplaceToken(ctx context.Context, id, userID, purpose string, digest []byte, target *string, expires time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE verification_tokens SET consumed_at = now() WHERE user_id = $1::uuid AND purpose = $2::verification_purpose AND consumed_at IS NULL`, userID, purpose); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO verification_tokens (id, user_id, purpose, token_hash, target, max_attempts, expires_at)
VALUES ($1::uuid, $2::uuid, $3::verification_purpose, $4, $5, 5, $6)`, id, userID, purpose, digest, target, expires); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type VerifyOutcome int

const (
	VerifyOK VerifyOutcome = iota
	VerifyInvalid
	VerifyLocked
)

// VerifyEmailCode checks a code under a row lock so attempts are counted
// exactly even with parallel guesses. A wrong guess is committed (attempts+1)
// before returning; reaching max_attempts locks the code until it is resent.
func (r *Auth) VerifyEmailCode(ctx context.Context, userID string, match func(stored []byte) bool, a AuditEntry) (VerifyOutcome, time.Duration, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return VerifyInvalid, 0, err
	}
	defer tx.Rollback(ctx)

	var id string
	var digest []byte
	var attempts, maxAttempts int
	var expires time.Time
	err = tx.QueryRow(ctx, `SELECT id::text, token_hash, attempts, max_attempts, expires_at FROM verification_tokens
WHERE user_id = $1::uuid AND purpose = 'EMAIL_VERIFY' AND consumed_at IS NULL AND expires_at > now()
ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, userID).Scan(&id, &digest, &attempts, &maxAttempts, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return VerifyInvalid, 0, nil
	}
	if err != nil {
		return VerifyInvalid, 0, err
	}
	if attempts >= maxAttempts {
		return VerifyLocked, time.Until(expires), nil
	}
	if !match(digest) {
		if _, err = tx.Exec(ctx, `UPDATE verification_tokens SET attempts = attempts + 1 WHERE id = $1::uuid`, id); err != nil {
			return VerifyInvalid, 0, err
		}
		if attempts+1 >= maxAttempts {
			return VerifyLocked, time.Until(expires), tx.Commit(ctx)
		}
		return VerifyInvalid, 0, tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `UPDATE verification_tokens SET consumed_at = now() WHERE id = $1::uuid`, id); err != nil {
		return VerifyInvalid, 0, err
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET is_verified = true, status = CASE WHEN status = 'PENDING' THEN 'ACTIVE'::user_status ELSE status END WHERE id = $1::uuid`, userID); err != nil {
		return VerifyInvalid, 0, err
	}
	if err = audit(ctx, tx, a); err != nil {
		return VerifyInvalid, 0, err
	}
	return VerifyOK, 0, tx.Commit(ctx)
}

// ---------- passwords ----------

// ResetPassword consumes a reset token and sets the new password, signs out
// every session and marks the email verified (the user just proved they own it).
// The link carries the token row id; match checks the secret against the stored
// digest (which is bound to the user id).
func (r *Auth) ResetPassword(ctx context.Context, tokenID string, match func(userID string, stored []byte) bool, newHash string, ipHash []byte, requestID string) (userID, email string, found bool, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", "", false, err
	}
	defer tx.Rollback(ctx)

	var digest []byte
	err = tx.QueryRow(ctx, `SELECT user_id::text, token_hash FROM verification_tokens
WHERE id = $1::uuid AND purpose = 'PASSWORD_RESET' AND consumed_at IS NULL AND expires_at > now() FOR UPDATE`, tokenID).Scan(&userID, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	if !match(userID, digest) {
		return "", "", false, nil
	}
	if err = tx.QueryRow(ctx, `UPDATE users SET password_hash = $2, is_verified = true,
  status = CASE WHEN status = 'PENDING' THEN 'ACTIVE'::user_status ELSE status END
WHERE id = $1::uuid AND deleted_at IS NULL RETURNING email::text`, userID, newHash).Scan(&email); err != nil {
		return "", "", false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE verification_tokens SET consumed_at = now() WHERE id = $1::uuid`, tokenID); err != nil {
		return "", "", false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1::uuid AND revoked_at IS NULL`, userID); err != nil {
		return "", "", false, err
	}
	target := "user"
	if err = audit(ctx, tx, AuditEntry{ActorID: &userID, Action: "auth.password_reset", TargetType: &target, TargetID: &userID, IPHash: ipHash, RequestID: requestID}); err != nil {
		return "", "", false, err
	}
	return userID, email, true, tx.Commit(ctx)
}

// SetPassword changes the password and signs out every other session.
func (r *Auth) SetPassword(ctx context.Context, userID, newHash string, keepSession string, a AuditEntry) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1::uuid`, userID, newHash); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1::uuid AND revoked_at IS NULL AND id <> $2::uuid`, userID, keepSession); err != nil {
		return err
	}
	if err = audit(ctx, tx, a); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UpdateHash stores an upgraded hash after a successful login.
func (r *Auth) UpdateHash(ctx context.Context, userID, hash string) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1::uuid`, userID, hash)
	return err
}

// ---------- profile / account ----------

func (r *Auth) UpdateProfile(ctx context.Context, userID, displayName, bio string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE users SET display_name = $2 WHERE id = $1::uuid`, userID, displayName); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE user_profiles SET bio = $2 WHERE user_id = $1::uuid`, userID, bio); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DeleteAccount soft-deletes (status DELETED): the account becomes unreachable,
// personal data is erased, the email/username are freed for reuse, the person's
// comments and the creator's videos disappear everywhere, and subscription /
// reaction counters are corrected by triggers. Audit and age records stay as evidence.
func (r *Auth) DeleteAccount(ctx context.Context, userID string, a AuditEntry) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	steps := []string{
		`UPDATE users SET status = 'DELETED', deleted_at = now(), email = 'deleted+' || id::text || '@deleted.invalid',
		   username = 'deleted_' || substr(replace(id::text, '-', ''), 1, 12), password_hash = '$argon2id$deleted',
		   display_name = 'Deleted user', phone = NULL, is_verified = false WHERE id = $1::uuid`,
		`UPDATE user_profiles SET bio = '', avatar_storage_key = NULL, banner_storage_key = NULL, website = NULL, location = NULL, social_links = '[]'::jsonb WHERE user_id = $1::uuid`,
		`UPDATE videos SET deleted_at = now() WHERE creator_id = $1::uuid AND deleted_at IS NULL`,
		`UPDATE playlists SET deleted_at = now() WHERE user_id = $1::uuid AND deleted_at IS NULL`,
		`UPDATE comments SET status = 'DELETED', deleted_at = now(), is_pinned = false WHERE user_id = $1::uuid AND status <> 'DELETED'`,
		`DELETE FROM sessions WHERE user_id = $1::uuid`,
		`DELETE FROM verification_tokens WHERE user_id = $1::uuid`,
		`DELETE FROM notifications WHERE user_id = $1::uuid`,
		`DELETE FROM search_history WHERE user_id = $1::uuid`,
		`DELETE FROM watch_history WHERE user_id = $1::uuid`,
		`DELETE FROM watch_later WHERE user_id = $1::uuid`,
		`DELETE FROM not_interested WHERE user_id = $1::uuid`,
		`DELETE FROM video_reactions WHERE user_id = $1::uuid`,
		`DELETE FROM comment_reactions WHERE user_id = $1::uuid`,
		`DELETE FROM blocked_users WHERE user_id = $1::uuid OR blocked_user_id = $1::uuid`,
		`DELETE FROM subscriptions WHERE subscriber_id = $1::uuid OR creator_id = $1::uuid`,
	}
	for _, q := range steps {
		if _, err = tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	if err = audit(ctx, tx, a); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ---------- settings ----------

// SettingsRow maps the frontend's settings onto user_settings columns:
// newVideo -> subscription_notifications, replies -> comment_notifications,
// newSubscriber -> subscriber_notifications, system -> system_notifications.
type SettingsRow struct {
	ShowSubscriptions, ShowLikedVideos, SaveWatchHistory  bool
	NewVideo, Replies, NewSubscriber, System, EmailNotifs bool
}

type SettingsPatch struct {
	ShowSubscriptions, ShowLikedVideos, SaveWatchHistory  *bool
	NewVideo, Replies, NewSubscriber, System, EmailNotifs *bool
}

func (r *Auth) GetSettings(ctx context.Context, userID string) (SettingsRow, error) {
	var s SettingsRow
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(s.show_subscriptions, false), COALESCE(s.show_liked_videos, false), COALESCE(s.save_watch_history, true),
  COALESCE(s.subscription_notifications, true), COALESCE(s.comment_notifications, true), COALESCE(s.subscriber_notifications, true),
  COALESCE(s.system_notifications, true), COALESCE(s.email_notifications, false)
FROM users u LEFT JOIN user_settings s ON s.user_id = u.id WHERE u.id = $1::uuid`, userID).
		Scan(&s.ShowSubscriptions, &s.ShowLikedVideos, &s.SaveWatchHistory, &s.NewVideo, &s.Replies, &s.NewSubscriber, &s.System, &s.EmailNotifs)
	return s, err
}

// PatchSettings applies only the fields that are set (nil = unchanged).
func (r *Auth) PatchSettings(ctx context.Context, userID string, p SettingsPatch) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO user_settings (user_id) VALUES ($1::uuid) ON CONFLICT DO NOTHING`, userID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE user_settings SET
  show_subscriptions = COALESCE($2::boolean, show_subscriptions), show_liked_videos = COALESCE($3::boolean, show_liked_videos),
  save_watch_history = COALESCE($4::boolean, save_watch_history), subscription_notifications = COALESCE($5::boolean, subscription_notifications),
  comment_notifications = COALESCE($6::boolean, comment_notifications), subscriber_notifications = COALESCE($7::boolean, subscriber_notifications),
  system_notifications = COALESCE($8::boolean, system_notifications), email_notifications = COALESCE($9::boolean, email_notifications)
WHERE user_id = $1::uuid`, userID, p.ShowSubscriptions, p.ShowLikedVideos, p.SaveWatchHistory, p.NewVideo, p.Replies, p.NewSubscriber, p.System, p.EmailNotifs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ---------- age verification ----------

// HasAgeVerification: the account declared 18+ at registration (status VERIFIED, not expired).
func (r *Auth) HasAgeVerification(ctx context.Context, userID string) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM age_verifications WHERE user_id = $1::uuid AND status = 'VERIFIED' AND (expires_at IS NULL OR expires_at > now()))`, userID).Scan(&ok)
	return ok, err
}
