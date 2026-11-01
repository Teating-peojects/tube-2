package repo

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/redxtube/api/internal/db/models"
)

// ---------- shared types ----------

// TxManager runs fn inside one transaction. Repositories obtained from the ctx passed to fn
// must use that transaction (e.g. store the pgx.Tx in the context).
type TxManager interface {
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// KeysetCursor is an opaque position for keyset pagination. Encode it (base64 JSON) before sending to clients.
type KeysetCursor struct {
	Time *time.Time
	ID   string
	Num  *int64 // for like_count / numeric sorts
}

type Page[T any] struct {
	Items      []T
	NextCursor *KeysetCursor // nil when there are no more rows
}

// ---------- identity ----------

type UserRepository interface {
	Create(ctx context.Context, u *models.User) error // profile + settings rows are created by trigger
	GetByID(ctx context.Context, id uuid.UUID) (*models.User, error)
	GetByLogin(ctx context.Context, usernameOrEmail string) (*models.User, error) // case-insensitive (citext)
	UpdateLastLogin(ctx context.Context, id uuid.UUID, at time.Time) error
	SetVerified(ctx context.Context, id uuid.UUID) error // sets is_verified and status ACTIVE
	SetPasswordHash(ctx context.Context, id uuid.UUID, hash string) error
	SetStatus(ctx context.Context, id uuid.UUID, status models.UserStatus) error
	SoftDelete(ctx context.Context, id uuid.UUID, at time.Time) error
	GetProfile(ctx context.Context, userID uuid.UUID) (*models.UserProfile, error)
	UpdateProfile(ctx context.Context, p *models.UserProfile) error
	GetChannel(ctx context.Context, channelID, viewerID uuid.UUID) (*models.ChannelSummary, error)
	SearchChannels(ctx context.Context, q string, limit int) ([]models.ChannelSummary, error)
}

type SettingsRepository interface {
	Get(ctx context.Context, userID uuid.UUID) (*models.UserSettings, error)
	Update(ctx context.Context, s *models.UserSettings) error
}

type SessionRepository interface {
	Create(ctx context.Context, s *models.Session) error
	GetActiveByTokenHash(ctx context.Context, hash []byte) (*models.Session, *models.User, error)
	Touch(ctx context.Context, id uuid.UUID, at time.Time) error
	Revoke(ctx context.Context, id uuid.UUID) error
	RevokeAllForUser(ctx context.Context, userID uuid.UUID) error
	ListActiveForUser(ctx context.Context, userID uuid.UUID) ([]models.Session, error)
}

type TokenRepository interface {
	Create(ctx context.Context, t *models.VerificationToken) error
	GetByID(ctx context.Context, id uuid.UUID) (*models.VerificationToken, error)
	LatestOpen(ctx context.Context, userID uuid.UUID, purpose models.VerificationPurpose) (*models.VerificationToken, error)
	// Consume atomically marks the token used. Returns ErrNotFound if it is expired, used, locked out or the hash differs.
	Consume(ctx context.Context, id uuid.UUID, hash []byte) (*models.VerificationToken, error)
	RegisterFailedAttempt(ctx context.Context, id uuid.UUID) error
}

type AgeVerificationRepository interface {
	Create(ctx context.Context, v *models.AgeVerification) error
	Latest(ctx context.Context, userID uuid.UUID) (*models.AgeVerification, error)
	MarkVerified(ctx context.Context, id uuid.UUID, expiresAt *time.Time) error
	MarkFailed(ctx context.Context, id uuid.UUID, status models.AgeVerificationStatus) error
}

// ---------- catalog ----------

type CategoryRepository interface {
	ListActive(ctx context.Context) ([]models.Category, error)
	GetBySlug(ctx context.Context, slug string) (*models.Category, error)
}

type TagRepository interface {
	// Upsert returns the tag ids for the given normalised slugs, creating missing tags.
	Upsert(ctx context.Context, names []string) ([]models.Tag, error)
	SetVideoTags(ctx context.Context, videoID uuid.UUID, tagIDs []int64) error // replace set, then call RefreshSearch
	Suggest(ctx context.Context, prefix string, limit int) ([]models.Tag, error)
}

type VideoFilter struct {
	CategoryID *int16
	CreatorID  *uuid.UUID
	Type       *models.VideoType
	ViewerID   *uuid.UUID // excludes blocked creators / not-interested videos
}

type StudioFilter string

const (
	StudioAll        StudioFilter = "all"
	StudioPublished  StudioFilter = "published"
	StudioDraft      StudioFilter = "draft"
	StudioProcessing StudioFilter = "processing"
	StudioPrivate    StudioFilter = "private"
	StudioUnlisted   StudioFilter = "unlisted"
)

type VideoRepository interface {
	Create(ctx context.Context, v *models.Video) error // status UPLOADING; stats row created by trigger
	GetByID(ctx context.Context, id uuid.UUID) (*models.Video, error)
	GetDetail(ctx context.Context, id, viewerID uuid.UUID) (*models.VideoDetail, error) // applies visibility rules
	UpdateMetadata(ctx context.Context, v *models.Video) error                          // title, description, category, language, visibility, comments_mode, age_restricted
	MarkProcessed(ctx context.Context, id uuid.UUID, m *models.VideoMetadata, vt models.VideoType, durationSeconds int32, hlsMasterKey string) error
	Publish(ctx context.Context, id uuid.UUID, at time.Time) error
	MarkFailed(ctx context.Context, id uuid.UUID) error
	SetVisibility(ctx context.Context, id, ownerID uuid.UUID, v models.VideoVisibility) error
	SoftDelete(ctx context.Context, id, ownerID uuid.UUID) error
	SetModeration(ctx context.Context, id uuid.UUID, s models.ModerationStatus) error

	ListRecent(ctx context.Context, f VideoFilter, after *KeysetCursor, limit int) (Page[models.VideoCard], error)
	ListByCreator(ctx context.Context, creatorID uuid.UUID, after *KeysetCursor, limit int) (Page[models.VideoCard], error)
	ListShorts(ctx context.Context, after *KeysetCursor, limit int) (Page[models.VideoCard], error)
	ListSubscriptionFeed(ctx context.Context, userID uuid.UUID, after *KeysetCursor, limit int) (Page[models.VideoCard], error)
	ListForStudio(ctx context.Context, creatorID uuid.UUID, f StudioFilter, after *KeysetCursor, limit int) (Page[models.Video], error)
	ListRelated(ctx context.Context, videoID uuid.UUID, limit int) ([]models.VideoCard, error)

	Variants(ctx context.Context, videoID uuid.UUID) ([]models.VideoVariant, error)
	Thumbnails(ctx context.Context, videoID uuid.UUID) ([]models.VideoThumbnail, error)
	Captions(ctx context.Context, videoID uuid.UUID) ([]models.VideoCaption, error)
	SetPrimaryThumbnail(ctx context.Context, videoID, thumbnailID uuid.UUID) error // clears the old primary in the same tx
}

type UploadRepository interface {
	Create(ctx context.Context, s *models.UploadSession) error
	GetByID(ctx context.Context, id uuid.UUID) (*models.UploadSession, error)
	CountOpenForUser(ctx context.Context, userID uuid.UUID) (int, error) // limit concurrent uploads
	SetStatus(ctx context.Context, id uuid.UUID, status models.UploadStatus) error
	ExpireStale(ctx context.Context, now time.Time) ([]models.UploadSession, error) // caller aborts the S3 multipart uploads
	AddAsset(ctx context.Context, videoID uuid.UUID, kind string, storageKey, language string) (uuid.UUID, error)
}

type JobRepository interface {
	Enqueue(ctx context.Context, videoID uuid.UUID, t models.JobType) (*models.ProcessingJob, error) // ErrConflict if one is already active
	// Claim uses FOR UPDATE SKIP LOCKED on the queue index; returns ErrNotFound when idle.
	Claim(ctx context.Context, workerID string, types []models.JobType) (*models.ProcessingJob, error)
	Heartbeat(ctx context.Context, id uuid.UUID, workerID string) error
	Complete(ctx context.Context, id uuid.UUID) error
	Fail(ctx context.Context, id uuid.UUID, msg string, retryAfter *time.Duration) error // re-queues until max_attempts
	RequeueStale(ctx context.Context, olderThan time.Duration) (int, error)
}

// ---------- engagement ----------

type ReactionRepository interface {
	SetVideoReaction(ctx context.Context, userID, videoID uuid.UUID, r *models.ReactionType) error // nil removes
	GetVideoReaction(ctx context.Context, userID, videoID uuid.UUID) (*models.ReactionType, error)
	ListLiked(ctx context.Context, userID uuid.UUID, after *KeysetCursor, limit int) (Page[models.VideoCard], error)
	SetCommentReaction(ctx context.Context, userID, commentID uuid.UUID, liked bool) error
}

type CommentSort string

const (
	CommentsNewest CommentSort = "new"
	CommentsTop    CommentSort = "top"
)

type CommentRepository interface {
	Create(ctx context.Context, c *models.Comment) error // status from videos.comments_mode; ErrInvalid if comments are OFF
	Get(ctx context.Context, id uuid.UUID) (*models.Comment, error)
	ListTopLevel(ctx context.Context, videoID, viewerID uuid.UUID, sort CommentSort, after *KeysetCursor, limit int) (Page[models.CommentView], error)
	ListReplies(ctx context.Context, parentID, viewerID uuid.UUID, limit int) ([]models.CommentView, error)
	Pinned(ctx context.Context, videoID uuid.UUID) (*models.CommentView, error)
	Edit(ctx context.Context, id, userID uuid.UUID, content string) error
	SoftDelete(ctx context.Context, id uuid.UUID, byUser uuid.UUID) error // author, video owner or staff
	SetStatus(ctx context.Context, id uuid.UUID, s models.CommentStatus) error
	Pin(ctx context.Context, videoID, commentID uuid.UUID) error // unpins the previous one in the same tx
	ListForCreator(ctx context.Context, creatorID uuid.UUID, heldOnly bool, after *KeysetCursor, limit int) (Page[models.Comment], error)
}

type SubscriptionRepository interface {
	Subscribe(ctx context.Context, subscriber, creator uuid.UUID) (changed bool, err error) // idempotent
	Unsubscribe(ctx context.Context, subscriber, creator uuid.UUID) (changed bool, err error)
	ListChannels(ctx context.Context, subscriber uuid.UUID, limit int) ([]models.ChannelSummary, error)
	IsSubscribed(ctx context.Context, subscriber, creator uuid.UUID) (bool, error)
	Block(ctx context.Context, user, blocked uuid.UUID) error
	Unblock(ctx context.Context, user, blocked uuid.UUID) error
	ListBlocked(ctx context.Context, user uuid.UUID) ([]models.ChannelSummary, error)
	NotInterested(ctx context.Context, user, video uuid.UUID) error
}

type LibraryRepository interface {
	SaveProgress(ctx context.Context, h *models.WatchHistory) error // honour user_settings.save_watch_history in the service
	ListHistory(ctx context.Context, userID uuid.UUID, after *KeysetCursor, limit int) (Page[models.WatchHistory], error)
	ContinueWatching(ctx context.Context, userID uuid.UUID, limit int) ([]models.WatchHistory, error)
	RemoveHistoryItem(ctx context.Context, userID, videoID uuid.UUID) error
	ClearHistory(ctx context.Context, userID uuid.UUID) error
	AddWatchLater(ctx context.Context, userID, videoID uuid.UUID) error
	RemoveWatchLater(ctx context.Context, userID, videoID uuid.UUID) error
	WatchLaterIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
	ListWatchLater(ctx context.Context, userID uuid.UUID, after *KeysetCursor, limit int) (Page[models.VideoCard], error)
}

type PlaylistRepository interface {
	Create(ctx context.Context, p *models.Playlist) error
	Get(ctx context.Context, id uuid.UUID) (*models.Playlist, error)
	Update(ctx context.Context, p *models.Playlist) error
	SoftDelete(ctx context.Context, id, ownerID uuid.UUID) error
	ListByUser(ctx context.Context, userID uuid.UUID, publicOnly bool) ([]models.Playlist, error)
	ListWithMembership(ctx context.Context, userID, videoID uuid.UUID) ([]PlaylistMembership, error)
	Videos(ctx context.Context, playlistID uuid.UUID, limit int) ([]models.VideoCard, error)
	AddVideo(ctx context.Context, playlistID, videoID uuid.UUID) error // appends at the end
	RemoveVideo(ctx context.Context, playlistID, videoID uuid.UUID) error
	Reorder(ctx context.Context, playlistID uuid.UUID, orderedVideoIDs []uuid.UUID) error // one UPDATE ... unnest WITH ORDINALITY
	VideoCount(ctx context.Context, playlistIDs []uuid.UUID) (map[uuid.UUID]int, error)
}

type PlaylistMembership struct {
	models.Playlist
	Contains bool
}

type NotificationRepository interface {
	Create(ctx context.Context, n *models.Notification) error
	List(ctx context.Context, userID uuid.UUID, unreadOnly bool, afterID *int64, limit int) ([]models.Notification, error)
	UnreadCount(ctx context.Context, userID uuid.UUID) (int, error)    // capped; prefer the Redis counter
	MarkRead(ctx context.Context, userID uuid.UUID, ids []int64) error // empty = all
}

type SearchRepository interface {
	Videos(ctx context.Context, q string, f SearchFilters, limit, offset int) ([]models.VideoCard, error)
	Suggest(ctx context.Context, prefix string, limit int) ([]string, error)
	RefreshVideo(ctx context.Context, videoID uuid.UUID) error // SELECT refresh_video_search($1)
	RememberQuery(ctx context.Context, userID uuid.UUID, q string) error
	RecentQueries(ctx context.Context, userID uuid.UUID, limit int) ([]string, error)
	ClearQueries(ctx context.Context, userID uuid.UUID) error
}

type SearchFilters struct {
	Duration string // "", "short", "medium", "long"
	Since    *time.Time
	Sort     string // "relevance", "latest", "views"
}

// ---------- safety ----------

type ReportRepository interface {
	Create(ctx context.Context, r *models.Report) error // ErrConflict + ConstraintReportOpen* = already reported
	Queue(ctx context.Context, status []models.ReportStatus, limit int) ([]models.Report, error)
	Assign(ctx context.Context, id, moderatorID uuid.UUID) error
	Resolve(ctx context.Context, id, moderatorID uuid.UUID, status models.ReportStatus) error
}

type ModerationRepository interface {
	// Apply records the action, updates the target's moderation state and resolves the report in ONE transaction.
	Apply(ctx context.Context, a *models.ModerationAction) error
	History(ctx context.Context, targetType models.ReportTarget, targetID uuid.UUID) ([]models.ModerationAction, error)
}

type AuditRepository interface {
	Log(ctx context.Context, e *models.AuditLog) error // append-only
	Search(ctx context.Context, actor *uuid.UUID, action string, from, to time.Time, limit int) ([]models.AuditLog, error)
}

// ---------- analytics ----------

type ViewEvent = models.VideoView

type AnalyticsRepository interface {
	InsertViews(ctx context.Context, events []ViewEvent) error // COPY into the monthly partitions; failures must not break playback
	FlushVideoCounters(ctx context.Context, videoIDs []uuid.UUID, views, watchSeconds []int64) error
	UpsertVideoDaily(ctx context.Context, day time.Time, rows []models.VideoDailyAnalytics) error
	RollupCreatorDay(ctx context.Context, day time.Time) error
	CreatorSeries(ctx context.Context, creatorID uuid.UUID, from time.Time) ([]models.CreatorDailyAnalytics, error)
	CreatorBreakdown(ctx context.Context, creatorID uuid.UUID, dim models.BreakdownDimension, from time.Time, limit int) ([]models.CreatorDailyBreakdown, error)
	TopVideos(ctx context.Context, creatorID uuid.UUID, from time.Time, limit int) ([]models.VideoDailyAnalytics, error)
	Trending(ctx context.Context, days, limit int) ([]uuid.UUID, error)
}

// Maintenance wraps the SECURITY DEFINER functions from migration 000009 (run with the redxtube_maint role).
type Maintenance interface {
	PurgeExpiredSessions(ctx context.Context, limit int) (int, error)
	PurgeExpiredVerificationTokens(ctx context.Context, limit int) (int, error)
	PurgeOldSearchHistory(ctx context.Context, limit, days int) (int, error)
	PurgeOldNotifications(ctx context.Context, limit int) (int, error)
	PurgeOldAuditLogs(ctx context.Context, limit, days int) (int, error)
	EnsurePartitions(ctx context.Context, parent string, monthsAhead int) (int, error)
	DropOldPartitions(ctx context.Context, parent string, keepMonths int) (int, error)
	RecountVideo(ctx context.Context, videoID uuid.UUID) error
	RecountCreator(ctx context.Context, userID uuid.UUID) error
}
