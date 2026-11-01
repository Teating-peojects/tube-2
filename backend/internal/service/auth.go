package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/redxtube/api/internal/apperr"
	"github.com/redxtube/api/internal/auth"
	appmail "github.com/redxtube/api/internal/mail"
	"github.com/redxtube/api/internal/media"
	"github.com/redxtube/api/internal/model"
	"github.com/redxtube/api/internal/repository"
)

const (
	SessionTTL          = 30 * 24 * time.Hour // sliding
	SessionAbsoluteTTL  = 90 * 24 * time.Hour // hard cap
	SessionRefreshAfter = time.Hour           // slide at most once an hour (no write per request)
	absoluteSecs        = float64(SessionAbsoluteTTL / time.Second)
	OTPTTL              = 10 * time.Minute
	ResetTTL            = 30 * time.Minute
	OTPMaxAttempts      = 5
	AgeCookieTTL        = 30 * 24 * time.Hour
	AgeAccountTTL       = 5 * 365 * 24 * time.Hour
)

// Limiter is the Redis rate limiter (see internal/ratelimit).
type Limiter interface {
	Hit(ctx context.Context, key string, window time.Duration) (int64, time.Duration, error)
	Peek(ctx context.Context, key string) (int64, time.Duration, error)
	Reset(ctx context.Context, key string) error
}

// ClientInfo is request metadata the service needs (resolved by the handler).
type ClientInfo struct {
	IP, UserAgent, RequestID, Country string
}

type SessionResult struct {
	Token   string
	Expires time.Time
	User    model.User
	Age     *AgeCookie // set when the account already has a verified age
}

type AgeCookie struct {
	Token   string
	Expires time.Time
}

type Auth struct {
	repo    *repository.Auth
	hasher  *auth.Hasher
	limiter Limiter
	mailer  appmail.Mailer
	keys    auth.Keys
	age     auth.AgeSigner
	urls    media.URLs
	appURL  string
	log     *slog.Logger
	now     func() time.Time
}

func NewAuth(repo *repository.Auth, hasher *auth.Hasher, limiter Limiter, mailer appmail.Mailer, secret []byte, urls media.URLs, appURL string, log *slog.Logger) *Auth {
	keys := auth.DeriveKeys(secret)
	return &Auth{repo: repo, hasher: hasher, limiter: limiter, mailer: mailer, keys: keys, age: auth.NewAgeSigner(keys.Age),
		urls: urls, appURL: strings.TrimRight(appURL, "/"), log: log, now: time.Now}
}

// AgeSigner lets the age-gate middleware verify cookies without a database call.
func (s *Auth) AgeSigner() auth.AgeSigner { return s.age }

// ---------- errors ----------

var (
	errInvalidCredentials = apperr.New(401, "invalid_credentials", "Incorrect email/username or password")
	errUnavailable        = apperr.New(503, "service_unavailable", "Service temporarily unavailable")
	errAgeRequired        = apperr.Forbidden("age_verification_required", "Age verification required")
)

func internal(err error) error { return apperr.Internal(err) }

// ---------- rate limiting (fails closed: if Redis is down, auth endpoints refuse) ----------

// hit counts an event and rejects it once the limit is exceeded.
func (s *Auth) hit(ctx context.Context, key string, limit int64, window time.Duration) error {
	n, ttl, err := s.limiter.Hit(ctx, key, window)
	if err != nil {
		s.log.ErrorContext(ctx, "rate limiter unavailable", "err", err.Error())
		return errUnavailable
	}
	if n > limit {
		return apperr.TooManyRequests(ttl)
	}
	return nil
}

// blocked rejects when the counter already reached the limit (no counting).
func (s *Auth) blocked(ctx context.Context, key string, limit int64) error {
	n, ttl, err := s.limiter.Peek(ctx, key)
	if err != nil {
		s.log.ErrorContext(ctx, "rate limiter unavailable", "err", err.Error())
		return errUnavailable
	}
	if n >= limit {
		return apperr.TooManyRequests(ttl)
	}
	return nil
}

// short returns a stable, non-reversible key fragment so no email or
// username is ever stored in Redis.
func short(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:16])
}

// ---------- validation ----------

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_]{3,24}$`)

var reservedUsernames = map[string]bool{
	"admin": true, "administrator": true, "root": true, "support": true, "redxtube": true, "moderator": true, "mod": true,
	"staff": true, "system": true, "official": true, "help": true, "api": true, "www": true, "null": true, "undefined": true,
	"security": true, "abuse": true, "legal": true, "dmca": true, "privacy": true, "terms": true, "trust": true, "safety": true,
}

func normalizeEmail(raw string) (string, bool) {
	e := strings.ToLower(strings.TrimSpace(raw))
	if e == "" || len(e) > 254 || strings.ContainsAny(e, " \t\r\n<>,;\"()[]\\") {
		return "", false
	}
	addr, err := mail.ParseAddress(e)
	if err != nil || addr.Address != e {
		return "", false
	}
	at := strings.LastIndexByte(e, '@')
	local, domain := e[:at], e[at+1:]
	if len(local) == 0 || len(local) > 64 || !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || strings.Contains(domain, "..") {
		return "", false
	}
	return e, true
}

// spoofing reports invisible/bidirectional formatting characters that can be
// used to impersonate other users or reorder text (e.g. U+202E "right-to-left
// override"). U+200C/U+200D (ZWNJ/ZWJ) are deliberately kept: Bengali, Persian
// and emoji sequences need them.
func spoofing(r rune) bool {
	switch {
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	}
	switch r {
	case 0x200B, 0x200E, 0x200F, 0x2060, 0xFEFF, 0x061C, 0x00AD:
		return true
	}
	return false
}

// cleanText removes control and spoofing characters and invalid UTF-8.
func cleanText(s string, keepNewlines bool) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if (unicode.IsControl(r) && !(keepNewlines && r == '\n')) || spoofing(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

func userDTO(r *repository.UserRow, urls media.URLs) model.User {
	return model.User{
		ID: r.ID, Username: r.Username, DisplayName: r.DisplayName, Email: r.Email, AvatarURL: urls.Asset(r.AvatarKey),
		EmailVerified: r.EmailVerified, IsCreator: r.IsCreator,
		JoinedAt: r.JoinedAt.UTC().Format(time.RFC3339), Bio: r.Bio,
	}
}

func (s *Auth) user(ctx context.Context, id string) (model.User, error) {
	row, err := s.repo.UserByID(ctx, id)
	if err != nil {
		return model.User{}, internal(err)
	}
	if row == nil {
		return model.User{}, apperr.Unauthorized("Not signed in")
	}
	return userDTO(row, s.urls), nil
}

func trunc(s string, n int) *string {
	if s == "" {
		return nil
	}
	for len(s) > n {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return &s
}

func (s *Auth) newSession(ci ClientInfo) (token string, ns repository.NewSession) {
	token = auth.RandomToken(32)
	return token, repository.NewSession{
		ID: auth.NewUUIDv7(), TokenHash: auth.HashToken(token), UserAgent: trunc(ci.UserAgent, 512), IPPrefix: auth.IPPrefix(ci.IP),
		ExpiresAt: s.now().Add(SessionTTL),
	}
}

// ipHash is the keyed hash stored in audit_logs (raw IPs are never stored).
func (s *Auth) ipHash(ci ClientInfo) []byte { return auth.HashIP(s.keys.IP, ci.IP) }

func (s *Auth) sendMail(ctx context.Context, m appmail.Message) {
	if err := s.mailer.Send(ctx, m); err != nil {
		s.log.ErrorContext(ctx, "could not queue email", "subject", m.Subject, "err", err.Error())
	}
}

func countryPtr(c string) *string {
	c = strings.ToUpper(strings.TrimSpace(c))
	if len(c) != 2 || c == "XX" || c == "T1" {
		return nil
	}
	for _, r := range c {
		if r < 'A' || r > 'Z' {
			return nil
		}
	}
	return &c
}

// ---------- register ----------

type RegisterInput struct{ Email, Username, Password string }

// Register creates an account. ageCookie must be a valid signed age cookie:
// the age gate has to be passed before an account can exist.
func (s *Auth) Register(ctx context.Context, in RegisterInput, ageCookie string, ci ClientInfo) (*SessionResult, error) {
	fields := map[string]string{}
	email, ok := normalizeEmail(in.Email)
	if !ok {
		fields["email"] = "Enter a valid email address"
	}
	username := strings.TrimSpace(in.Username)
	switch {
	case !usernameRe.MatchString(username):
		fields["username"] = "3-24 characters: letters, numbers and underscores"
	case reservedUsernames[strings.ToLower(username)]:
		fields["username"] = "That username is reserved"
	}
	if reason := auth.ValidatePassword(in.Password, username, email); reason != "" {
		fields["password"] = reason
	}
	if len(fields) > 0 {
		return nil, apperr.Validation(fields)
	}
	if _, ok := s.age.Verify(ageCookie); !ok {
		return nil, errAgeRequired
	}
	if err := s.hit(ctx, "register:ip:"+ci.IP, 10, time.Hour); err != nil {
		return nil, err
	}

	hash, err := s.hasher.Hash(ctx, in.Password)
	if err != nil {
		return nil, internal(err)
	}
	id := auth.NewUUIDv7()
	code := auth.GenerateOTP()
	token, ns := s.newSession(ci)
	now := s.now()

	if err := s.repo.Register(ctx, repository.RegisterParams{
		ID: id, Email: email, Username: username, PasswordHash: hash,
		OTPID: auth.NewUUIDv7(), OTPDigest: auth.OTPDigest(s.keys.OTP, id, code), OTPExpires: now.Add(OTPTTL),
		Session: ns, AgeExpires: now.Add(AgeAccountTTL), IPHash: s.ipHash(ci), RequestID: ci.RequestID,
	}); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, apperr.Conflict("conflict", "Email or username already in use")
		}
		return nil, internal(err)
	}
	s.sendMail(ctx, appmail.VerifyCode(email, code, int(OTPTTL.Minutes())))

	u, err := s.user(ctx, id)
	if err != nil {
		return nil, err
	}
	return &SessionResult{Token: token, Expires: ns.ExpiresAt, User: u}, nil
}

// ---------- login ----------

// Login verifies credentials. When the email is not verified yet a session is
// still created and BOTH the result and a 403 verification_required error are
// returned, so the handler can set the cookie (the verify screen needs it).
func (s *Auth) Login(ctx context.Context, identifier, password string, ci ClientInfo) (*SessionResult, error) {
	id := strings.ToLower(strings.TrimSpace(identifier))
	if id == "" || len(id) > 254 || password == "" || len(password) > 512 {
		return nil, errInvalidCredentials
	}
	idKey, ipKey := "login:id:"+short(id), "login:ip:"+ci.IP
	if err := s.blocked(ctx, idKey, 5); err != nil {
		return nil, err
	}
	if err := s.blocked(ctx, ipKey, 30); err != nil {
		return nil, err
	}
	fail := func() (*SessionResult, error) {
		for _, k := range []string{idKey, ipKey} {
			if _, _, err := s.limiter.Hit(ctx, k, 15*time.Minute); err != nil {
				s.log.ErrorContext(ctx, "rate limiter unavailable", "err", err.Error())
			}
		}
		return nil, errInvalidCredentials
	}

	var u *repository.UserAuth
	var err error
	if strings.Contains(id, "@") {
		u, err = s.repo.UserByEmail(ctx, id)
	} else {
		u, err = s.repo.UserByUsername(ctx, id)
	}
	if err != nil {
		return nil, internal(err)
	}
	if u == nil {
		s.hasher.Burn(ctx, password) // same cost as a real check: no account enumeration by timing
		return fail()
	}
	ok, rehash, verr := s.hasher.Verify(ctx, password, u.PasswordHash)
	if verr != nil && !errors.Is(verr, context.Canceled) && !errors.Is(verr, context.DeadlineExceeded) {
		ok = false // malformed stored hash (e.g. seed accounts) behaves like a wrong password
		s.hasher.Burn(ctx, password)
	} else if verr != nil {
		return nil, internal(verr)
	}
	if !ok {
		return fail()
	}
	_ = s.limiter.Reset(ctx, idKey)

	if u.Status == "SUSPENDED" || u.Status == "BANNED" {
		return nil, apperr.Forbidden("account_suspended", "This account is not available")
	}
	if rehash {
		if nh, err := s.hasher.Hash(ctx, password); err == nil {
			if err := s.repo.UpdateHash(ctx, u.ID, nh); err != nil {
				s.log.WarnContext(ctx, "password rehash not saved", "err", err.Error())
			}
		}
	}

	token, ns := s.newSession(ci) // always a fresh token: no session fixation
	if err := s.repo.CreateSession(ctx, u.ID, ns); err != nil {
		return nil, internal(err)
	}
	_ = s.repo.MarkLogin(ctx, u.ID)
	target := "session"
	_ = s.repo.Audit(ctx, repository.AuditEntry{ActorID: &u.ID, Role: u.Role, Action: "auth.login", TargetType: &target, TargetID: &ns.ID, IPHash: s.ipHash(ci), RequestID: ci.RequestID})

	row, err := s.user(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	res := &SessionResult{Token: token, Expires: ns.ExpiresAt, User: row}
	// An account that already proved it is 18+ gets its age cookie back on any device.
	if has, err := s.repo.HasAgeVerification(ctx, u.ID); err == nil && has {
		t, exp := s.age.Issue(u.ID, AgeCookieTTL)
		res.Age = &AgeCookie{Token: t, Expires: exp}
	}
	if !u.EmailVerified {
		return res, apperr.Forbidden("verification_required", "Verify your email to continue")
	}
	return res, nil
}

// ---------- sessions ----------

// Authenticate resolves the session cookie. It returns (nil, nil, nil) for
// anonymous, expired, revoked or suspended callers. When the session was
// slid forward, newExpiry is set so the cookie can be re-issued.
func (s *Auth) Authenticate(ctx context.Context, token string) (p *auth.Principal, newExpiry *time.Time, err error) {
	if token == "" || len(token) > 100 {
		return nil, nil, nil
	}
	row, err := s.repo.Session(ctx, auth.HashToken(token), absoluteSecs)
	if err != nil {
		return nil, nil, err
	}
	if row == nil || row.Status == "SUSPENDED" || row.Status == "BANNED" {
		return nil, nil, nil
	}
	if now := s.now(); now.Sub(row.LastUsedAt) > SessionRefreshAfter {
		exp := now.Add(SessionTTL)
		if abs := row.CreatedAt.Add(SessionAbsoluteTTL); exp.After(abs) {
			exp = abs
		}
		if err := s.repo.TouchSession(ctx, row.ID, exp, absoluteSecs); err != nil {
			s.log.WarnContext(ctx, "session touch failed", "err", err.Error())
		} else {
			newExpiry = &exp
		}
	}
	return &auth.Principal{UserID: row.UserID, SessionID: row.ID, Username: row.Username, Role: auth.Role(row.Role), EmailVerified: row.EmailVerified, IsCreator: row.IsCreator}, newExpiry, nil
}

// Logout revokes the session behind token (idempotent).
func (s *Auth) Logout(ctx context.Context, token string) error {
	if token == "" || len(token) > 100 {
		return nil
	}
	if err := s.repo.RevokeSession(ctx, auth.HashToken(token)); err != nil {
		return internal(err)
	}
	return nil
}

func (s *Auth) LogoutAll(ctx context.Context, p *auth.Principal, ci ClientInfo) error {
	err := s.repo.RevokeUserSessions(ctx, p.UserID, nil, repository.AuditEntry{ActorID: &p.UserID, Role: string(p.Role), Action: "auth.logout_all", IPHash: s.ipHash(ci), RequestID: ci.RequestID})
	if err != nil {
		return internal(err)
	}
	return nil
}

func (s *Auth) Me(ctx context.Context, p *auth.Principal) (model.User, error) {
	return s.user(ctx, p.UserID)
}

// ---------- email verification ----------

var codeRe = regexp.MustCompile(`^\d{6}$`)

func (s *Auth) Verify(ctx context.Context, p *auth.Principal, code string, ci ClientInfo) (model.User, error) {
	if p.EmailVerified {
		return s.user(ctx, p.UserID)
	}
	code = strings.TrimSpace(code)
	if !codeRe.MatchString(code) {
		return model.User{}, apperr.BadRequest("invalid_code", "Invalid code")
	}
	if err := s.hit(ctx, "verify:ip:"+ci.IP, 30, 10*time.Minute); err != nil {
		return model.User{}, err
	}
	want := auth.OTPDigest(s.keys.OTP, p.UserID, code)
	outcome, retry, err := s.repo.VerifyEmailCode(ctx, p.UserID,
		func(stored []byte) bool { return auth.Equal(stored, want) },
		repository.AuditEntry{ActorID: &p.UserID, Role: string(p.Role), Action: "auth.email_verified", IPHash: s.ipHash(ci), RequestID: ci.RequestID})
	if err != nil {
		return model.User{}, internal(err)
	}
	switch outcome {
	case repository.VerifyOK:
		return s.user(ctx, p.UserID)
	case repository.VerifyLocked:
		return model.User{}, apperr.TooManyRequests(retry)
	default:
		return model.User{}, apperr.BadRequest("invalid_code", "Invalid or expired code")
	}
}

// ResendCode issues a fresh code (the old one stops working and attempts reset).
func (s *Auth) ResendCode(ctx context.Context, p *auth.Principal, ci ClientInfo) error {
	if p.EmailVerified {
		return nil
	}
	if err := s.hit(ctx, "resend:cd:"+p.UserID, 1, time.Minute); err != nil {
		return err
	}
	if err := s.hit(ctx, "resend:hr:"+p.UserID, 5, time.Hour); err != nil {
		return err
	}
	u, err := s.repo.UserAuthByID(ctx, p.UserID)
	if err != nil {
		return internal(err)
	}
	if u == nil {
		return apperr.Unauthorized("Not signed in")
	}
	code := auth.GenerateOTP()
	if err := s.repo.ReplaceToken(ctx, auth.NewUUIDv7(), u.ID, "EMAIL_VERIFY", auth.OTPDigest(s.keys.OTP, u.ID, code), &u.Email, s.now().Add(OTPTTL)); err != nil {
		return internal(err)
	}
	s.sendMail(ctx, appmail.VerifyCode(u.Email, code, int(OTPTTL.Minutes())))
	return nil
}

// ---------- forgot / reset / change password ----------

// Forgot always succeeds from the caller's view (no account enumeration).
// Only a per-IP flood returns 429.
func (s *Auth) Forgot(ctx context.Context, rawEmail string, ci ClientInfo) error {
	if err := s.hit(ctx, "forgot:ip:"+ci.IP, 10, time.Hour); err != nil {
		return err
	}
	email, ok := normalizeEmail(rawEmail)
	if !ok {
		return nil
	}
	n, _, err := s.limiter.Hit(ctx, "forgot:email:"+short(email), time.Hour)
	if err != nil {
		s.log.ErrorContext(ctx, "rate limiter unavailable", "err", err.Error())
		return errUnavailable
	}
	if n > 3 {
		return nil // silently stop mailing one address more than 3 times an hour
	}
	u, err := s.repo.UserByEmail(ctx, email)
	if err != nil {
		return internal(err)
	}
	if u == nil || u.Status == "SUSPENDED" || u.Status == "BANNED" {
		return nil
	}
	// the link carries "<token row id>.<secret>"; only an HMAC bound to purpose+user is stored
	tokenID, secret := auth.NewUUIDv7(), auth.RandomToken(32)
	token := tokenID + "." + secret
	if err := s.repo.ReplaceToken(ctx, tokenID, u.ID, "PASSWORD_RESET", auth.TokenDigest(s.keys.OTP, "PASSWORD_RESET", u.ID, secret), nil, s.now().Add(ResetTTL)); err != nil {
		return internal(err)
	}
	s.sendMail(ctx, appmail.ResetPassword(u.Email, s.appURL+"/reset-password?token="+token, int(ResetTTL.Minutes())))
	return nil
}

func (s *Auth) Reset(ctx context.Context, token, password string, ci ClientInfo) error {
	tokenID, secret, ok := strings.Cut(token, ".")
	if !ok || !IsUUID(tokenID) || len(secret) < 20 || len(secret) > 64 {
		return apperr.BadRequest("invalid_token", "Invalid or expired link")
	}
	if err := s.hit(ctx, "reset:ip:"+ci.IP, 20, time.Hour); err != nil {
		return err
	}
	if reason := auth.ValidatePassword(password, "", ""); reason != "" {
		return apperr.Validation(map[string]string{"password": reason})
	}
	hash, err := s.hasher.Hash(ctx, password)
	if err != nil {
		return internal(err)
	}
	_, email, found, err := s.repo.ResetPassword(ctx, tokenID,
		func(userID string, stored []byte) bool {
			return auth.Equal(stored, auth.TokenDigest(s.keys.OTP, "PASSWORD_RESET", userID, secret))
		}, hash, s.ipHash(ci), ci.RequestID)
	if err != nil {
		return internal(err)
	}
	if !found {
		return apperr.BadRequest("invalid_token", "Invalid or expired link")
	}
	s.sendMail(ctx, appmail.PasswordChanged(email))
	return nil
}

// checkPassword verifies the caller's current password with a failure limit.
func (s *Auth) checkPassword(ctx context.Context, p *auth.Principal, password, limitKey string) (*repository.UserAuth, error) {
	key := limitKey + ":" + p.UserID
	if err := s.blocked(ctx, key, 5); err != nil {
		return nil, err
	}
	u, err := s.repo.UserAuthByID(ctx, p.UserID)
	if err != nil {
		return nil, internal(err)
	}
	if u == nil {
		return nil, apperr.Unauthorized("Not signed in")
	}
	ok, _, verr := s.hasher.Verify(ctx, password, u.PasswordHash)
	if verr != nil || !ok {
		if _, _, err := s.limiter.Hit(ctx, key, 15*time.Minute); err != nil {
			s.log.ErrorContext(ctx, "rate limiter unavailable", "err", err.Error())
		}
		return nil, apperr.Forbidden("invalid_credentials", "Incorrect password")
	}
	_ = s.limiter.Reset(ctx, key)
	return u, nil
}

func (s *Auth) ChangePassword(ctx context.Context, p *auth.Principal, current, next string, ci ClientInfo) error {
	u, err := s.checkPassword(ctx, p, current, "chpass")
	if err != nil {
		return err
	}
	if reason := auth.ValidatePassword(next, u.Username, u.Email); reason != "" {
		return apperr.Validation(map[string]string{"password": reason})
	}
	if next == current {
		return apperr.Validation(map[string]string{"password": "Choose a different password"})
	}
	hash, err := s.hasher.Hash(ctx, next)
	if err != nil {
		return internal(err)
	}
	target := "user"
	if err := s.repo.SetPassword(ctx, p.UserID, hash, p.SessionID, repository.AuditEntry{
		ActorID: &p.UserID, Role: string(p.Role), Action: "auth.password_change", TargetType: &target, TargetID: &p.UserID, IPHash: s.ipHash(ci), RequestID: ci.RequestID}); err != nil {
		return internal(err)
	}
	s.sendMail(ctx, appmail.PasswordChanged(u.Email))
	return nil
}

// ---------- account ----------

func (s *Auth) UpdateAccount(ctx context.Context, p *auth.Principal, displayName, bio string) (model.User, error) {
	displayName, bio = strings.Join(strings.Fields(cleanText(displayName, false)), " "), cleanText(bio, true)
	fields := map[string]string{}
	if n := utf8.RuneCountInString(displayName); n < 2 || n > 40 {
		fields["displayName"] = "2-40 characters"
	}
	if utf8.RuneCountInString(bio) > 300 {
		fields["bio"] = "At most 300 characters"
	}
	if len(fields) > 0 {
		return model.User{}, apperr.Validation(fields)
	}
	if err := s.repo.UpdateProfile(ctx, p.UserID, displayName, bio); err != nil {
		return model.User{}, internal(err)
	}
	return s.user(ctx, p.UserID)
}

func (s *Auth) DeleteAccount(ctx context.Context, p *auth.Principal, password string, ci ClientInfo) error {
	if _, err := s.checkPassword(ctx, p, password, "delacct"); err != nil {
		return err
	}
	target := "user"
	if err := s.repo.DeleteAccount(ctx, p.UserID, repository.AuditEntry{
		ActorID: &p.UserID, Role: string(p.Role), Action: "account.delete", TargetType: &target, TargetID: &p.UserID, IPHash: s.ipHash(ci), RequestID: ci.RequestID}); err != nil {
		return internal(err)
	}
	return nil
}

// ---------- settings ----------

func (s *Auth) Settings(ctx context.Context, p *auth.Principal) (model.Settings, error) {
	r, err := s.repo.GetSettings(ctx, p.UserID)
	if err != nil {
		return model.Settings{}, internal(err)
	}
	return model.Settings{
		Privacy:       model.PrivacySettings{ShowSubscriptions: r.ShowSubscriptions, ShowLikedVideos: r.ShowLikedVideos, SaveWatchHistory: r.SaveWatchHistory},
		Notifications: model.NotificationSettings{NewVideo: r.NewVideo, Replies: r.Replies, NewSubscriber: r.NewSubscriber, System: r.System, Email: r.EmailNotifs},
	}, nil
}

func (s *Auth) PatchSettings(ctx context.Context, p *auth.Principal, in model.SettingsPatch) error {
	var patch repository.SettingsPatch
	if in.Privacy != nil {
		patch.ShowSubscriptions, patch.ShowLikedVideos, patch.SaveWatchHistory = in.Privacy.ShowSubscriptions, in.Privacy.ShowLikedVideos, in.Privacy.SaveWatchHistory
	}
	if in.Notifications != nil {
		n := in.Notifications
		patch.NewVideo, patch.Replies, patch.NewSubscriber, patch.System, patch.EmailNotifs = n.NewVideo, n.Replies, n.NewSubscriber, n.System, n.Email
	}
	if err := s.repo.PatchSettings(ctx, p.UserID, patch); err != nil {
		return internal(err)
	}
	return nil
}

// ---------- age gate ----------

// VerifyAge issues the signed age cookie after an explicit 18+ confirmation.
// Anonymous visitors are not written to the database (age_verifications is per
// account and the signed cookie is the proof); accounts get their own record at
// registration. A valid existing cookie is simply re-issued (extending it).
func (s *Auth) VerifyAge(ctx context.Context, existing string, ci ClientInfo) (AgeCookie, error) {
	if err := s.hit(ctx, "age:ip:"+ci.IP, 30, time.Hour); err != nil {
		return AgeCookie{}, err
	}
	subject, ok := s.age.Verify(existing)
	if !ok {
		subject = auth.RandomUUID()
	}
	t, exp := s.age.Issue(subject, AgeCookieTTL)
	return AgeCookie{Token: t, Expires: exp}, nil
}
