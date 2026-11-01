package models

import (
	"encoding/json"
	"net/netip"
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID           uuid.UUID  `db:"id"`
	Username     string     `db:"username"`
	Email        string     `db:"email"`
	Phone        *string    `db:"phone"`
	PasswordHash string     `db:"password_hash"` // argon2id/bcrypt encoded string; never log or serialise
	DisplayName  string     `db:"display_name"`
	Role         UserRole   `db:"role"`
	Status       UserStatus `db:"status"`
	IsVerified   bool       `db:"is_verified"`
	IsCreator    bool       `db:"is_creator"`
	CreatedAt    time.Time  `db:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at"`
	LastLoginAt  *time.Time `db:"last_login_at"`
	DeletedAt    *time.Time `db:"deleted_at"`
}

type UserProfile struct {
	UserID           uuid.UUID       `db:"user_id"`
	Bio              string          `db:"bio"`
	AvatarStorageKey *string         `db:"avatar_storage_key"`
	BannerStorageKey *string         `db:"banner_storage_key"`
	Website          *string         `db:"website"`
	Location         *string         `db:"location"`
	SocialLinks      json.RawMessage `db:"social_links"` // [{"label":"..","url":"https://.."}]
	SubscriberCount  int32           `db:"subscriber_count"`
	VideoCount       int32           `db:"video_count"`
	CreatedAt        time.Time       `db:"created_at"`
	UpdatedAt        time.Time       `db:"updated_at"`
}

type UserSettings struct {
	UserID                    uuid.UUID       `db:"user_id"`
	Language                  string          `db:"language"`
	Theme                     ThemePref       `db:"theme"`
	Autoplay                  bool            `db:"autoplay"`
	EmailNotifications        bool            `db:"email_notifications"`
	PushNotifications         bool            `db:"push_notifications"`
	CommentNotifications      bool            `db:"comment_notifications"`
	SubscriptionNotifications bool            `db:"subscription_notifications"`
	SubscriberNotifications   bool            `db:"subscriber_notifications"`
	SystemNotifications       bool            `db:"system_notifications"`
	ShowSubscriptions         bool            `db:"show_subscriptions"`
	ShowLikedVideos           bool            `db:"show_liked_videos"`
	SaveWatchHistory          bool            `db:"save_watch_history"`
	SaveSearchHistory         bool            `db:"save_search_history"`
	Extra                     json.RawMessage `db:"extra"`
	CreatedAt                 time.Time       `db:"created_at"`
	UpdatedAt                 time.Time       `db:"updated_at"`
}

type Session struct {
	ID               uuid.UUID       `db:"id"`
	UserID           uuid.UUID       `db:"user_id"`
	SessionTokenHash []byte          `db:"session_token_hash"` // SHA-256 (32 bytes)
	RefreshTokenHash []byte          `db:"refresh_token_hash"`
	IPPrefix         *netip.Prefix   `db:"ip_prefix"` // already truncated by the caller (/24 v4, /56 v6)
	UserAgent        *string         `db:"user_agent"`
	DeviceInfo       json.RawMessage `db:"device_info"`
	ExpiresAt        time.Time       `db:"expires_at"`
	CreatedAt        time.Time       `db:"created_at"`
	LastUsedAt       time.Time       `db:"last_used_at"`
	RevokedAt        *time.Time      `db:"revoked_at"`
}

type VerificationToken struct {
	ID          uuid.UUID           `db:"id"`
	UserID      uuid.UUID           `db:"user_id"`
	Purpose     VerificationPurpose `db:"purpose"`
	TokenHash   []byte              `db:"token_hash"` // HMAC-SHA256(pepper, purpose|user_id|token)
	Target      *string             `db:"target"`
	Attempts    int16               `db:"attempts"`
	MaxAttempts int16               `db:"max_attempts"`
	ExpiresAt   time.Time           `db:"expires_at"`
	ConsumedAt  *time.Time          `db:"consumed_at"`
	CreatedAt   time.Time           `db:"created_at"`
}

type AgeVerification struct {
	ID                uuid.UUID             `db:"id"`
	UserID            uuid.UUID             `db:"user_id"`
	Status            AgeVerificationStatus `db:"status"`
	Method            AgeVerificationMethod `db:"method"`
	Provider          *string               `db:"provider"`
	ProviderReference *string               `db:"provider_reference"` // opaque; never PII
	VerifiedAt        *time.Time            `db:"verified_at"`
	ExpiresAt         *time.Time            `db:"expires_at"`
	CreatedAt         time.Time             `db:"created_at"`
	UpdatedAt         time.Time             `db:"updated_at"`
}
