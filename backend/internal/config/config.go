// Package config loads configuration from environment variables only.
// No secrets are ever hardcoded; production requires explicit values.
package config

import (
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type MailConfig struct {
	Driver       string // resend | log | none
	ResendAPIKey string // secret: never log
	From         string // e.g. "REDXTUBE <no-reply@redxtube.com>"
}

// StorageConfig points at the S3-compatible bucket that holds uploads (AWS S3,
// Cloudflare R2, MinIO...). The zero value means "not configured": the upload
// endpoints then answer 503 uploads_disabled and the rest of the API is unaffected.
type StorageConfig struct {
	Endpoint       string // what the API itself uses, e.g. http://minio:9000
	PublicEndpoint string // what the BROWSER uses for presigned URLs (defaults to Endpoint)
	Region         string
	AccessKey      string // secret: never log
	SecretKey      string // secret: never log
	Bucket         string
	PathStyle      bool
}

// UploadConfig holds the tunable upload limits. Defaults equal the previous constants.
type UploadConfig struct {
	MaxBytes   int64         // UPLOAD_MAX_BYTES, 1 MiB..5 GiB (database cap)
	PartSize   int64         // UPLOAD_PART_SIZE_BYTES, 5 MiB..1 GiB (at most 1,024 parts per file)
	MaxOpen    int           // UPLOAD_MAX_OPEN, unfinished uploads per user, 1..50
	SessionTTL time.Duration // UPLOAD_SESSION_TTL, 1h..168h
	MaxRetries int           // UPLOAD_MAX_RETRIES, reopenings of one expired upload, 0..10 (0 disables retry)
}

// Enabled reports whether uploads are configured.
func (s StorageConfig) Enabled() bool { return s.Endpoint != "" }

type Config struct {
	Env             string // development | staging | production
	Port            string
	LogLevel        slog.Level
	DatabaseURL     string
	RedisURL        string
	CORSOrigins     []string // exact origins, e.g. https://redxtube.com
	CDNURL          string   // public base URL for thumbnails/avatars/HLS (optional in dev)
	MaxBodyBytes    int64
	ShutdownTimeout time.Duration
	DBMaxConns      int32

	AppURL              string // frontend base URL (links in emails)
	CookieDomain        string // e.g. .redxtube.com so redxtube.com and api.redxtube.com share cookies
	CookieSecure        bool
	SessionCookieName   string
	AuthSecret          []byte // master secret; per-purpose keys are derived from it
	AuthSecretEphemeral bool   // dev only: generated at startup, cookies die on restart
	TrustProxyHeaders   bool   // honour CF-Connecting-IP / X-Forwarded-For (only behind a trusted proxy)
	Mail                MailConfig
	Storage             StorageConfig
	// Playback (B7): HLS is served through signed, expiring URLs. StreamBaseURL is where players
	// reach the stream handler, for example https://api.redxtube.com/stream. Empty = HLS URLs are
	// the plain public CDN_URL ones (development only). PlaybackKey overrides the key derived from
	// AuthSecret (set it when an edge/CDN verifies tokens too).
	StreamBaseURL string
	// MetricsAddr, when set (host:port), serves /metrics on a separate listener. Keep it internal.
	MetricsAddr string
	PlaybackKey []byte

	Upload UploadConfig
}

func (c Config) IsProduction() bool { return c.Env == "production" }

var (
	cookieNameRe   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)
	cookieDomainRe = regexp.MustCompile(`^\.?([a-z0-9-]+\.)+[a-z]{2,}$`)
	bucketRe       = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
)

// Load reads the environment via getenv (os.Getenv in production code).
func Load(getenv func(string) string) (Config, error) {
	get := func(k, def string) string {
		if v := strings.TrimSpace(getenv(k)); v != "" {
			return v
		}
		return def
	}

	c := Config{
		Env:             strings.ToLower(get("APP_ENV", "development")),
		Port:            get("PORT", "8080"),
		DatabaseURL:     get("DATABASE_URL", ""),
		RedisURL:        get("REDIS_URL", ""),
		MaxBodyBytes:    1 << 20, // 1 MiB; uploads never pass through the API
		ShutdownTimeout: 15 * time.Second,
		DBMaxConns:      10,
	}

	var errs []error
	fail := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	switch c.Env {
	case "development", "staging", "production":
	default:
		fail("APP_ENV must be development, staging or production (got %q)", c.Env)
	}

	if p, err := strconv.Atoi(c.Port); err != nil || p < 1 || p > 65535 {
		fail("PORT must be 1-65535 (got %q)", c.Port)
	}

	if err := c.LogLevel.UnmarshalText([]byte(get("LOG_LEVEL", "info"))); err != nil {
		fail("LOG_LEVEL: %v", err)
	}

	if v := get("DB_MAX_CONNS", ""); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			fail("DB_MAX_CONNS must be 1-200 (got %q)", v)
		} else {
			c.DBMaxConns = int32(n)
		}
	}

	for _, o := range strings.Split(get("CORS_ALLOWED_ORIGINS", ""), ",") {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		u, err := url.Parse(o)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" {
			fail("CORS_ALLOWED_ORIGINS: %q must look like https://example.com", o)
			continue
		}
		c.CORSOrigins = append(c.CORSOrigins, o)
	}

	if v := strings.TrimRight(get("CDN_URL", ""), "/"); v != "" {
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			fail("CDN_URL must be an http(s) URL (got %q)", v)
		} else if c.IsProduction() && u.Scheme != "https" {
			fail("CDN_URL must be https in production")
		} else {
			c.CDNURL = v
		}
	}

	// ----- auth / cookies / email -----
	c.AppURL = strings.TrimRight(get("APP_URL", ""), "/")
	if c.AppURL == "" && !c.IsProduction() {
		c.AppURL = "http://localhost:3000"
	}
	if u, err := url.Parse(c.AppURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" {
		fail("APP_URL must look like https://redxtube.com (got %q)", c.AppURL)
	} else {
		if c.IsProduction() && u.Scheme != "https" {
			fail("APP_URL must be https in production")
		}
		c.CookieSecure = u.Scheme == "https"
	}

	if d := strings.ToLower(get("COOKIE_DOMAIN", "")); d != "" {
		if !cookieDomainRe.MatchString(d) {
			fail("COOKIE_DOMAIN must be a bare domain like .redxtube.com (got %q)", d)
		} else {
			c.CookieDomain = d
		}
	}

	c.SessionCookieName = get("SESSION_COOKIE_NAME", "xh_session")
	if !cookieNameRe.MatchString(c.SessionCookieName) {
		fail("SESSION_COOKIE_NAME may only contain letters, digits, - and _")
	}

	switch secret := getenv("AUTH_SECRET"); {
	case len(secret) >= 32:
		c.AuthSecret = []byte(secret)
	case secret != "":
		fail("AUTH_SECRET must be at least 32 characters (generate one with: openssl rand -base64 48)")
	case c.IsProduction():
		fail("AUTH_SECRET is required in production")
	default:
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			fail("cannot generate dev AUTH_SECRET: %v", err)
		}
		c.AuthSecret, c.AuthSecretEphemeral = b, true
	}

	if v := strings.ToLower(get("TRUST_PROXY_HEADERS", "false")); v == "true" || v == "1" {
		c.TrustProxyHeaders = true
	} else if v != "false" && v != "0" {
		fail("TRUST_PROXY_HEADERS must be true or false")
	}

	defaultDriver := "log"
	if c.IsProduction() {
		defaultDriver = "resend"
	}
	c.Mail = MailConfig{
		Driver:       strings.ToLower(get("MAIL_DRIVER", defaultDriver)),
		ResendAPIKey: get("RESEND_API_KEY", ""),
		From:         get("MAIL_FROM", ""),
	}
	switch c.Mail.Driver {
	case "resend":
		if c.Mail.ResendAPIKey == "" {
			fail("RESEND_API_KEY is required when MAIL_DRIVER=resend")
		}
		if c.Mail.From == "" {
			fail("MAIL_FROM is required when MAIL_DRIVER=resend")
		}
	case "log", "none":
		if c.IsProduction() {
			fail("MAIL_DRIVER=%s is not allowed in production (it would drop or print one-time codes)", c.Mail.Driver)
		}
	default:
		fail("MAIL_DRIVER must be resend, log or none (got %q)", c.Mail.Driver)
	}

	// ----- object storage (uploads) -----
	st := StorageConfig{
		Endpoint:       strings.TrimRight(get("STORAGE_ENDPOINT", ""), "/"),
		PublicEndpoint: strings.TrimRight(get("STORAGE_PUBLIC_ENDPOINT", ""), "/"),
		Region:         get("STORAGE_REGION", "us-east-1"),
		AccessKey:      get("STORAGE_ACCESS_KEY", ""),
		SecretKey:      get("STORAGE_SECRET_KEY", ""),
		Bucket:         get("STORAGE_BUCKET", ""),
	}
	if st.Endpoint != "" || st.AccessKey != "" || st.SecretKey != "" || st.Bucket != "" {
		for k, v := range map[string]string{"STORAGE_ENDPOINT": st.Endpoint, "STORAGE_ACCESS_KEY": st.AccessKey, "STORAGE_SECRET_KEY": st.SecretKey, "STORAGE_BUCKET": st.Bucket} {
			if v == "" {
				fail("%s is required when any STORAGE_* variable is set", k)
			}
		}
		checkURL := func(name, raw string) {
			u, err := url.Parse(raw)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") {
				fail("%s must look like https://host[:port] (got %q)", name, raw)
				return
			}
			if c.IsProduction() && u.Scheme != "https" {
				fail("%s must be https in production", name)
			}
		}
		if st.Endpoint != "" {
			checkURL("STORAGE_ENDPOINT", st.Endpoint)
		}
		if st.PublicEndpoint != "" {
			checkURL("STORAGE_PUBLIC_ENDPOINT", st.PublicEndpoint)
		}
		if !bucketRe.MatchString(st.Bucket) && st.Bucket != "" {
			fail("STORAGE_BUCKET %q is not a valid bucket name", st.Bucket)
		}
		// Path-style addressing is right for MinIO and most self-hosted stores; AWS S3 uses virtual-hosted style.
		st.PathStyle = !strings.Contains(st.Endpoint, ".amazonaws.com")
		switch v := strings.ToLower(get("STORAGE_PATH_STYLE", "")); v {
		case "":
		case "true", "1":
			st.PathStyle = true
		case "false", "0":
			st.PathStyle = false
		default:
			fail("STORAGE_PATH_STYLE must be true or false")
		}
	}
	c.Storage = st

	if a := get("METRICS_ADDR", ""); a != "" {
		if _, port, err := net.SplitHostPort(a); err != nil {
			fail("METRICS_ADDR must be host:port, for example 127.0.0.1:9090 (got %q)", a)
		} else if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
			fail("METRICS_ADDR port must be 1-65535 (got %q)", a)
		} else {
			c.MetricsAddr = a
		}
	}

	// ----- playback (B7) -----
	if v := strings.TrimRight(get("STREAM_BASE_URL", ""), "/"); v != "" {
		u, err := url.Parse(v)
		switch {
		case err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "":
			fail("STREAM_BASE_URL must look like https://host[/path] (got %q)", v)
		case c.IsProduction() && u.Scheme != "https":
			fail("STREAM_BASE_URL must be https in production")
		default:
			c.StreamBaseURL = v
		}
	}
	if k := get("PLAYBACK_SIGNING_KEY", ""); k != "" {
		if len(k) < 32 {
			fail("PLAYBACK_SIGNING_KEY must be at least 32 characters (openssl rand -base64 48)")
		} else {
			c.PlaybackKey = []byte(k)
		}
	}
	if c.IsProduction() && c.Storage.Enabled() && c.StreamBaseURL == "" {
		fail("STREAM_BASE_URL is required in production when STORAGE_* is set: HLS must be served through signed URLs, not a public bucket")
	}

	// ----- upload limits -----
	up := UploadConfig{MaxBytes: 5 << 30, PartSize: 16 << 20, MaxOpen: 5, SessionTTL: 24 * time.Hour, MaxRetries: 3}
	int64Env := func(name string, dst *int64, lo, hi int64) {
		v := get(name, "")
		if v == "" {
			return
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < lo || n > hi {
			fail("%s must be a whole number of bytes between %d and %d (got %q)", name, lo, hi, v)
			return
		}
		*dst = n
	}
	int64Env("UPLOAD_MAX_BYTES", &up.MaxBytes, 1<<20, 5<<30)
	int64Env("UPLOAD_PART_SIZE_BYTES", &up.PartSize, 5<<20, 1<<30)
	if v := get("UPLOAD_MAX_OPEN", ""); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 50 {
			fail("UPLOAD_MAX_OPEN must be 1-50 (got %q)", v)
		} else {
			up.MaxOpen = n
		}
	}
	if v := get("UPLOAD_MAX_RETRIES", ""); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 10 {
			fail("UPLOAD_MAX_RETRIES must be 0-10, 0 disables retry (got %q)", v)
		} else {
			up.MaxRetries = n
		}
	}
	if v := get("UPLOAD_SESSION_TTL", ""); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < time.Hour || d > 168*time.Hour {
			fail("UPLOAD_SESSION_TTL must be a duration between 1h and 168h, e.g. 24h (got %q)", v)
		} else {
			up.SessionTTL = d
		}
	}
	// With the bounds above a file needs at most 5 GiB / 5 MiB = 1,024 parts, far below S3's 10,000-part limit.
	c.Upload = up

	if c.DatabaseURL == "" {
		fail("DATABASE_URL is required")
	}
	if c.RedisURL == "" {
		fail("REDIS_URL is required")
	}
	if c.IsProduction() {
		if len(c.CORSOrigins) == 0 {
			fail("CORS_ALLOWED_ORIGINS is required in production")
		}
		for _, o := range c.CORSOrigins {
			if !strings.HasPrefix(o, "https://") {
				fail("CORS_ALLOWED_ORIGINS: %q must be https in production", o)
			}
		}
	}

	return c, errors.Join(errs...)
}

// FromEnv is Load(os.Getenv).
func FromEnv() (Config, error) { return Load(os.Getenv) }
