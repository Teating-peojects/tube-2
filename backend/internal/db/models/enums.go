// Package models holds plain structs that map 1:1 to REDXTUBE tables (pgx `db` tags).
// Enum values must match db/migrations/000001_foundation.up.sql exactly.
package models

type (
	UserRole              string
	UserStatus            string
	ThemePref             string
	VerificationPurpose   string
	AgeVerificationStatus string
	AgeVerificationMethod string
	VideoType             string
	VideoVisibility       string
	VideoStatus           string
	ModerationStatus      string
	CommentMode           string
	VideoResolution       string
	VariantStatus         string
	ThumbnailType         string
	AssetSource           string
	AssetStatus           string
	JobType               string
	JobStatus             string
	UploadStatus          string
	ReactionType          string
	CommentStatus         string
	PlaylistVisibility    string
	NotificationType      string
	ReportTarget          string
	ReportReason          string
	ReportStatus          string
	ModerationActionType  string
	DeviceType            string
	TrafficSource         string
	BreakdownDimension    string
)

const (
	RoleUser       UserRole = "USER"
	RoleCreator    UserRole = "CREATOR"
	RoleModerator  UserRole = "MODERATOR"
	RoleAdmin      UserRole = "ADMIN"
	RoleSuperAdmin UserRole = "SUPER_ADMIN"

	UserActive    UserStatus = "ACTIVE"
	UserSuspended UserStatus = "SUSPENDED"
	UserBanned    UserStatus = "BANNED"
	UserDeleted   UserStatus = "DELETED"
	UserPending   UserStatus = "PENDING"

	ThemeDark   ThemePref = "DARK"
	ThemeLight  ThemePref = "LIGHT"
	ThemeSystem ThemePref = "SYSTEM"

	PurposeEmailVerify   VerificationPurpose = "EMAIL_VERIFY"
	PurposePhoneVerify   VerificationPurpose = "PHONE_VERIFY"
	PurposePasswordReset VerificationPurpose = "PASSWORD_RESET"

	AgePending  AgeVerificationStatus = "PENDING"
	AgeVerified AgeVerificationStatus = "VERIFIED"
	AgeFailed   AgeVerificationStatus = "FAILED"
	AgeExpired  AgeVerificationStatus = "EXPIRED"
	AgeRejected AgeVerificationStatus = "REJECTED"

	AgeMethodSelf     AgeVerificationMethod = "SELF_DECLARATION"
	AgeMethodProvider AgeVerificationMethod = "THIRD_PARTY"
	AgeMethodDocument AgeVerificationMethod = "DOCUMENT"
	AgeMethodOther    AgeVerificationMethod = "OTHER"

	VideoShort VideoType = "SHORT"
	VideoLong  VideoType = "LONG"

	VisibilityPublic   VideoVisibility = "PUBLIC"
	VisibilityUnlisted VideoVisibility = "UNLISTED"
	VisibilityPrivate  VideoVisibility = "PRIVATE"

	VideoUploading  VideoStatus = "UPLOADING"
	VideoProcessing VideoStatus = "PROCESSING"
	VideoReady      VideoStatus = "READY"
	VideoPublished  VideoStatus = "PUBLISHED"
	VideoFailed     VideoStatus = "FAILED"

	ModerationOK         ModerationStatus = "OK"
	ModerationRestricted ModerationStatus = "RESTRICTED"
	ModerationRemoved    ModerationStatus = "REMOVED"

	CommentsOn     CommentMode = "ON"
	CommentsReview CommentMode = "REVIEW"
	CommentsOff    CommentMode = "OFF"

	Res360  VideoResolution = "360p"
	Res480  VideoResolution = "480p"
	Res720  VideoResolution = "720p"
	Res1080 VideoResolution = "1080p"

	VariantPending    VariantStatus = "PENDING"
	VariantProcessing VariantStatus = "PROCESSING"
	VariantReady      VariantStatus = "READY"
	VariantFailed     VariantStatus = "FAILED"

	ThumbPoster    ThumbnailType = "POSTER"
	ThumbThumbnail ThumbnailType = "THUMBNAIL"
	ThumbPreview   ThumbnailType = "PREVIEW"

	AssetGenerated AssetSource = "GENERATED"
	AssetCustom    AssetSource = "CUSTOM"

	AssetPending AssetStatus = "PENDING"
	AssetReady   AssetStatus = "READY"

	JobMetadata  JobType = "METADATA"
	JobTranscode JobType = "TRANSCODE"
	JobHLS       JobType = "HLS"
	JobThumbnail JobType = "THUMBNAIL"

	JobQueued     JobStatus = "QUEUED"
	JobProcessing JobStatus = "PROCESSING"
	JobCompleted  JobStatus = "COMPLETED"
	JobFailed     JobStatus = "FAILED"

	UploadInitiated  UploadStatus = "INITIATED"
	UploadCompleting UploadStatus = "COMPLETING"
	UploadCompleted  UploadStatus = "COMPLETED"
	UploadAborted    UploadStatus = "ABORTED"
	UploadExpired    UploadStatus = "EXPIRED"

	ReactionLike    ReactionType = "LIKE"
	ReactionDislike ReactionType = "DISLIKE"

	CommentVisible     CommentStatus = "VISIBLE"
	CommentHidden      CommentStatus = "HIDDEN"
	CommentDeleted     CommentStatus = "DELETED"
	CommentUnderReview CommentStatus = "UNDER_REVIEW"

	PlaylistPublic   PlaylistVisibility = "PUBLIC"
	PlaylistUnlisted PlaylistVisibility = "UNLISTED"
	PlaylistPrivate  PlaylistVisibility = "PRIVATE"

	NotifNewSubscriber NotificationType = "NEW_SUBSCRIBER"
	NotifNewVideo      NotificationType = "NEW_VIDEO"
	NotifComment       NotificationType = "COMMENT"
	NotifCommentReply  NotificationType = "COMMENT_REPLY"
	NotifVideoLike     NotificationType = "VIDEO_LIKE"
	NotifCommentLike   NotificationType = "COMMENT_LIKE"
	NotifVideoReady    NotificationType = "VIDEO_READY"
	NotifVideoFailed   NotificationType = "VIDEO_FAILED"
	NotifModeration    NotificationType = "MODERATION"
	NotifSecurity      NotificationType = "SECURITY"
	NotifSystem        NotificationType = "SYSTEM"

	TargetVideo   ReportTarget = "VIDEO"
	TargetComment ReportTarget = "COMMENT"
	TargetUser    ReportTarget = "USER"

	ReasonUnderage      ReportReason = "UNDERAGE_CONTENT"
	ReasonNonConsensual ReportReason = "NON_CONSENSUAL_CONTENT"
	ReasonCopyright     ReportReason = "COPYRIGHT"
	ReasonHarassment    ReportReason = "HARASSMENT"
	ReasonSpam          ReportReason = "SPAM"
	ReasonIllegal       ReportReason = "ILLEGAL_CONTENT"
	ReasonOther         ReportReason = "OTHER"

	ReportPending     ReportStatus = "PENDING"
	ReportUnderReview ReportStatus = "UNDER_REVIEW"
	ReportResolved    ReportStatus = "RESOLVED"
	ReportRejected    ReportStatus = "REJECTED"

	ActionWarning         ModerationActionType = "WARNING"
	ActionContentRemoved  ModerationActionType = "CONTENT_REMOVED"
	ActionVideoRestricted ModerationActionType = "VIDEO_RESTRICTED"
	ActionUserSuspended   ModerationActionType = "USER_SUSPENDED"
	ActionUserBanned      ModerationActionType = "USER_BANNED"
	ActionCommentRemoved  ModerationActionType = "COMMENT_REMOVED"

	DeviceMobile  DeviceType = "MOBILE"
	DeviceDesktop DeviceType = "DESKTOP"
	DeviceTablet  DeviceType = "TABLET"
	DeviceTV      DeviceType = "TV"
	DeviceOther   DeviceType = "OTHER"

	SourceHome          TrafficSource = "HOME"
	SourceSearch        TrafficSource = "SEARCH"
	SourceSubscriptions TrafficSource = "SUBSCRIPTIONS"
	SourceExternal      TrafficSource = "EXTERNAL"
	SourceDirect        TrafficSource = "DIRECT"
	SourceOther         TrafficSource = "OTHER"

	DimCountry       BreakdownDimension = "COUNTRY"
	DimDevice        BreakdownDimension = "DEVICE"
	DimTrafficSource BreakdownDimension = "TRAFFIC_SOURCE"
)

// IsStaff reports whether the role may use moderation/admin endpoints. Authorise uploads with User.IsCreator,
// not with Role: a moderator can also be a creator.
func (r UserRole) IsStaff() bool {
	return r == RoleModerator || r == RoleAdmin || r == RoleSuperAdmin
}
