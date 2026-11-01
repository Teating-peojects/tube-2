package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/redxtube/api/internal/apperr"
	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/captions"
	"github.com/redxtube/api/internal/media"
	"github.com/redxtube/api/internal/model"
	"github.com/redxtube/api/internal/repository"
	"github.com/redxtube/api/internal/storage"
)

const (
	UploadPartSize    = 16 << 20 // bytes per part: 5 GiB needs 320 parts, far below S3's 10,000
	MaxUploadBytes    = 5 << 30
	MaxOpenUploads    = 5
	UploadTTL         = 24 * time.Hour
	PresignPartTTL    = 12 * time.Hour // must stay below UploadTTL
	PresignAssetTTL   = 30 * time.Minute
	ConsentVersion    = 1 // the wording of the 18+ / consent / rights declaration the uploader saw
	NewVideoFanOutMax = 5000
	MaxThumbBytes     = 2 << 20
	maxCustomThumbs   = 10
	maxCaptionLangs   = 12
	maxTags           = 15
	janitorBatch      = 50
)

var (
	errUploadNotFound = apperr.NotFound("upload_not_found", "Upload not found")
	errUploadsOff     = apperr.New(http.StatusServiceUnavailable, "uploads_disabled", "Uploads are not available right now")
	errStorage        = apperr.New(http.StatusBadGateway, "storage_unavailable", "Storage is temporarily unavailable, try again")
)

// containerByExt maps the allowed extensions to the content type given to storage.
// The client's own Content-Type is never trusted or stored.
var containerByExt = map[string]string{
	".mp4": "video/mp4", ".mov": "video/quicktime", ".mkv": "video/x-matroska", ".webm": "video/webm", ".avi": "video/x-msvideo",
}

var imageTypes = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}

var captionTypes = map[string]string{"text/vtt": ".vtt", "application/x-subrip": ".srt"}

var languageNames = map[string]string{
	"en": "English", "bn": "Bengali", "hi": "Hindi", "es": "Spanish", "fr": "French", "de": "German",
	"pt": "Portuguese", "ru": "Russian", "ar": "Arabic", "ja": "Japanese", "ko": "Korean", "zh": "Chinese",
}

var langRe = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

// Notifier pushes "something changed" to a user's open sockets. Optional.
type Notifier interface {
	Notify(ctx context.Context, userID string)
}

// UploadStore is implemented by repository.Upload (and by fakes in tests).
type UploadStore interface {
	OpenUploads(ctx context.Context, userID string) (int, error)
	CreateUpload(ctx context.Context, n repository.UploadInit) error
	UploadByID(ctx context.Context, id, userID string) (*repository.UploadSession, error)
	BeginComplete(ctx context.Context, id string) (bool, error)
	RevertComplete(ctx context.Context, id string) error
	FinishComplete(ctx context.Context, s *repository.UploadSession, a repository.AuditEntry) error
	DiscardUpload(ctx context.Context, s *repository.UploadSession, a *repository.AuditEntry) error
	ExpireUploads(ctx context.Context, limit int) ([]repository.ExpiredUpload, error)
	Progress(ctx context.Context, videoID string) (repository.Progress, error)
	GeneratedThumbs(ctx context.Context, videoID string) ([]repository.ThumbRow, error)
	CountThumbs(ctx context.Context, videoID string) (int, error)
	AddThumb(ctx context.Context, videoID, key string) (string, error)
	CountCaptions(ctx context.Context, videoID string) (int, error)
	CaptionExists(ctx context.Context, videoID, lang string) (bool, error)
	AddCaption(ctx context.Context, videoID, lang, label, key string) (string, error)
	ThumbKey(ctx context.Context, videoID, id string) (string, error)
	CaptionKey(ctx context.Context, videoID, id string) (lang, key string, err error)
	CategoryID(ctx context.Context, name string) (int32, error)
	UpdateMetadata(ctx context.Context, videoID, userID string, m repository.MetaPatch, a repository.AuditEntry) error
	Publish(ctx context.Context, p repository.PublishParams) (string, error)
	FanOutNewVideo(ctx context.Context, videoID, creatorID string, limit int) ([]string, error)
}

// Upload implements the creator upload flow. The video bytes never pass through
// the API: the browser PUTs parts straight to object storage with presigned
// URLs, and the API checks the result (sizes, ETags, file signature) before the
// media worker ever sees the file.
type Upload struct {
	repo    UploadStore
	store   storage.API // nil: uploads are disabled
	limiter Limiter
	ipKey   []byte
	urls    media.URLs
	notify  Notifier
	log     *slog.Logger
	now     func() time.Time
}

func NewUpload(repo UploadStore, store storage.API, limiter Limiter, ipKey []byte, urls media.URLs, notify Notifier, log *slog.Logger) *Upload {
	return &Upload{repo: repo, store: store, limiter: limiter, ipKey: ipKey, urls: urls, notify: notify, log: log, now: time.Now}
}

func (s *Upload) limit(ctx context.Context, key string, max int64, window time.Duration) error {
	return rateLimit(ctx, s.limiter, s.log, key, max, window)
}

func (s *Upload) auditEntry(p *auth.Principal, ci ClientInfo, action, targetType, targetID string, meta map[string]any) repository.AuditEntry {
	return repository.AuditEntry{ActorID: &p.UserID, Role: string(p.Role), Action: action, TargetType: &targetType, TargetID: &targetID,
		IPHash: auth.HashIP(s.ipKey, ci.IP), RequestID: ci.RequestID, Meta: meta}
}

func (s *Upload) session(ctx context.Context, p *auth.Principal, id string) (*repository.UploadSession, error) {
	if !IsUUID(id) {
		return nil, errUploadNotFound
	}
	u, err := s.repo.UploadByID(ctx, id, p.UserID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if u == nil || u.VideoDeleted && u.Status != "ABORTED" && u.Status != "EXPIRED" {
		return nil, errUploadNotFound
	}
	return u, nil
}

// ---------- init ----------

type InitInput struct {
	Filename    string
	Size        int64
	ContentType string
}

func extOf(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i < 0 {
		return ""
	}
	return strings.ToLower(name[i:])
}

// titleFromFilename turns "my clip.final.mp4" into "my clip.final" (1-100 runes).
func titleFromFilename(name string) string {
	t := name
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		t = name[:i]
	}
	t = strings.TrimSpace(t)
	if t == "" {
		return "Untitled"
	}
	if utf8.RuneCountInString(t) > 100 {
		t = string([]rune(t)[:100])
	}
	return strings.TrimSpace(t)
}

// Init starts a multipart upload and returns one presigned URL per part.
func (s *Upload) Init(ctx context.Context, p *auth.Principal, ci ClientInfo, in InitInput) (model.UploadInit, error) {
	if s.store == nil {
		return model.UploadInit{}, errUploadsOff
	}
	fields := map[string]string{}
	name := cleanText(in.Filename, false)
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	ext := extOf(name)
	ctype, okExt := containerByExt[ext]
	switch {
	case name == "" || utf8.RuneCountInString(name) > 255:
		fields["filename"] = "1-255 characters"
	case !okExt:
		fields["filename"] = "Use .mp4, .mov, .mkv, .webm or .avi"
	}
	if in.Size <= 0 {
		fields["size"] = "The file is empty"
	} else if in.Size > MaxUploadBytes {
		fields["size"] = "The maximum size is 5 GiB"
	}
	if ct := strings.ToLower(strings.TrimSpace(in.ContentType)); ct != "" && !strings.HasPrefix(ct, "video/") && ct != "application/octet-stream" {
		fields["contentType"] = "That doesn't look like a video"
	}
	if len(fields) > 0 {
		return model.UploadInit{}, apperr.Validation(fields)
	}

	if err := s.limit(ctx, "upload:init:"+p.UserID, 10, time.Hour); err != nil {
		return model.UploadInit{}, err
	}
	open, err := s.repo.OpenUploads(ctx, p.UserID)
	if err != nil {
		return model.UploadInit{}, apperr.Internal(err)
	}
	if open >= MaxOpenUploads {
		return model.UploadInit{}, errLimitReached("unfinished uploads")
	}

	uploadID, videoID := auth.NewUUIDv7(), auth.NewUUIDv7()
	key := "uploads/" + videoID + "/original" + ext
	partCount := int32((in.Size + UploadPartSize - 1) / UploadPartSize)
	providerID, err := s.store.CreateMultipart(ctx, key, ctype)
	if err != nil {
		s.log.ErrorContext(ctx, "create multipart failed", "err", err.Error())
		return model.UploadInit{}, errStorage
	}
	err = s.repo.CreateUpload(ctx, repository.UploadInit{
		UploadID: uploadID, VideoID: videoID, UserID: p.UserID, Title: titleFromFilename(name), Filename: name, StorageKey: key,
		ProviderUploadID: providerID, ContentType: ctype, Size: in.Size, PartSize: UploadPartSize, PartCount: partCount,
		ExpiresAt: s.now().Add(UploadTTL),
		Audit:     s.auditEntry(p, ci, "upload.init", "video", videoID, map[string]any{"size": in.Size, "parts": partCount}),
	})
	if err != nil {
		_ = s.store.AbortMultipart(context.WithoutCancel(ctx), key, providerID) // do not leave the orphan behind
		return model.UploadInit{}, apperr.Internal(err)
	}

	parts := make([]model.UploadPart, 0, partCount)
	for n := 1; n <= int(partCount); n++ {
		parts = append(parts, model.UploadPart{PartNumber: n, URL: s.store.PresignUploadPart(key, providerID, n, PresignPartTTL)})
	}
	return model.UploadInit{UploadID: uploadID, VideoID: videoID, PartSize: UploadPartSize, Parts: parts}, nil
}

// ---------- complete ----------

type PartIn struct {
	PartNumber int    `json:"partNumber"`
	ETag       string `json:"etag"`
}

// validContainer checks the first bytes of the file for a known video container.
// It is a cheap early filter, not a full validation (the worker runs ffprobe).
func validContainer(head []byte) bool {
	switch {
	case len(head) >= 12 && string(head[4:8]) == "ftyp": // MP4 / MOV
		return true
	case len(head) >= 4 && bytes.Equal(head[:4], []byte{0x1A, 0x45, 0xDF, 0xA3}): // Matroska / WebM (EBML)
		return true
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "AVI ": // AVI
		return true
	}
	return false
}

// Complete verifies the uploaded parts against object storage, assembles the
// object, checks its size and signature, and queues it for processing.
func (s *Upload) Complete(ctx context.Context, p *auth.Principal, ci ClientInfo, uploadID string, parts []PartIn) error {
	if s.store == nil {
		return errUploadsOff
	}
	u, err := s.session(ctx, p, uploadID)
	if err != nil {
		return err
	}
	switch u.Status {
	case "COMPLETED":
		return nil // idempotent: a retried request after a lost response
	case "COMPLETING":
		return apperr.Conflict("upload_in_progress", "This upload is already being finished")
	case "ABORTED", "EXPIRED":
		return apperr.Conflict("upload_closed", "This upload was cancelled or has expired, start a new one")
	}
	if !s.now().Before(u.ExpiresAt) {
		return apperr.Conflict("upload_closed", "This upload has expired, start a new one")
	}

	// the client's list must be exactly parts 1..N, each with an ETag
	if len(parts) != int(u.PartCount) {
		return apperr.Validation(map[string]string{"parts": "Expected every part of the file"})
	}
	claimed := make(map[int]string, len(parts))
	for _, pt := range parts {
		e := storage.NormalizeETag(pt.ETag)
		if pt.PartNumber < 1 || pt.PartNumber > int(u.PartCount) || e == "" || len(e) > 200 {
			return apperr.Validation(map[string]string{"parts": "Invalid part list"})
		}
		if _, dup := claimed[pt.PartNumber]; dup {
			return apperr.Validation(map[string]string{"parts": "Duplicate part"})
		}
		claimed[pt.PartNumber] = e
	}
	if err := s.limit(ctx, "upload:complete:"+p.UserID, 30, time.Hour); err != nil {
		return err
	}

	won, err := s.repo.BeginComplete(ctx, u.ID)
	if err != nil {
		return apperr.Internal(err)
	}
	if !won {
		return apperr.Conflict("upload_in_progress", "This upload is already being finished")
	}
	bg := context.WithoutCancel(ctx) // finish cleanly even if the client disconnects
	retryable := func(e error) error {
		_ = s.repo.RevertComplete(bg, u.ID)
		return e
	}
	reject := func(code, msg string) error {
		_ = s.store.Delete(bg, u.StorageKey)
		_ = s.repo.DiscardUpload(bg, u, ptrAudit(s.auditEntry(p, ci, "upload.rejected", "video", u.VideoID, map[string]any{"reason": code})))
		return apperr.New(http.StatusUnprocessableEntity, code, msg)
	}

	// What storage actually holds is the truth, not what the client says.
	listed, err := s.store.ListParts(ctx, u.StorageKey, u.ProviderUploadID)
	if err != nil {
		s.log.ErrorContext(ctx, "list parts failed", "err", err.Error())
		return retryable(errStorage)
	}
	have := make(map[int]storage.Part, len(listed))
	for _, lp := range listed {
		have[lp.Number] = lp
	}
	final := make([]storage.Part, 0, u.PartCount)
	var total int64
	for n := 1; n <= int(u.PartCount); n++ {
		lp, ok := have[n]
		want := int64(u.PartSize)
		if n == int(u.PartCount) {
			want = u.DeclaredSize - int64(n-1)*int64(u.PartSize)
		}
		if !ok || lp.Size != want || storage.NormalizeETag(lp.ETag) != claimed[n] {
			return retryable(apperr.New(http.StatusUnprocessableEntity, "parts_mismatch", "Some parts are missing or damaged, upload them again"))
		}
		total += lp.Size
		final = append(final, storage.Part{Number: n, ETag: lp.ETag, Size: lp.Size})
	}
	if total != u.DeclaredSize {
		return retryable(apperr.New(http.StatusUnprocessableEntity, "parts_mismatch", "Some parts are missing or damaged, upload them again"))
	}

	if err := s.store.CompleteMultipart(ctx, u.StorageKey, u.ProviderUploadID, final); err != nil {
		s.log.ErrorContext(ctx, "complete multipart failed", "err", err.Error())
		return retryable(errStorage)
	}
	info, err := s.store.Head(bg, u.StorageKey)
	if err != nil {
		return retryable(errStorage)
	}
	if info.Size != u.DeclaredSize {
		return reject("size_mismatch", "The uploaded file does not match its declared size")
	}
	head, err := s.store.GetRange(bg, u.StorageKey, 0, 4095)
	if err != nil {
		return retryable(errStorage)
	}
	if !validContainer(head) {
		return reject("invalid_file", "That file is not a supported video")
	}

	if err := s.repo.FinishComplete(bg, u, s.auditEntry(p, ci, "upload.complete", "video", u.VideoID, map[string]any{"size": u.DeclaredSize})); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

func ptrAudit(a repository.AuditEntry) *repository.AuditEntry { return &a }

// ---------- abort ----------

// Abort cancels an unfinished upload or discards an unpublished draft.
func (s *Upload) Abort(ctx context.Context, p *auth.Principal, ci ClientInfo, uploadID string) error {
	if s.store == nil {
		return errUploadsOff
	}
	u, err := s.session(ctx, p, uploadID)
	if err != nil {
		return err
	}
	if u.Status == "ABORTED" || u.Status == "EXPIRED" {
		return nil
	}
	if u.VideoStatus == "PUBLISHED" {
		return apperr.Conflict("already_published", "A published video can't be cancelled, delete it in Studio")
	}
	bg := context.WithoutCancel(ctx)
	switch u.Status {
	case "INITIATED", "COMPLETING":
		if err := s.store.AbortMultipart(bg, u.StorageKey, u.ProviderUploadID); err != nil {
			s.log.WarnContext(ctx, "abort multipart failed", "err", err.Error())
		}
	case "COMPLETED":
		if err := s.store.Delete(bg, u.StorageKey); err != nil {
			s.log.WarnContext(ctx, "delete original failed", "err", err.Error())
		}
	}
	if err := s.repo.DiscardUpload(bg, u, ptrAudit(s.auditEntry(p, ci, "upload.abort", "video", u.VideoID, nil))); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

// ---------- status ----------

const pipelineJobs = 4 // METADATA, TRANSCODE, HLS, THUMBNAIL

// Status maps the session, the video and the worker's jobs onto the five states the UI knows.
func (s *Upload) Status(ctx context.Context, p *auth.Principal, uploadID string) (model.UploadStatus, error) {
	if err := s.limit(ctx, "upload:status:"+p.UserID, 120, time.Minute); err != nil {
		return model.UploadStatus{}, err
	}
	u, err := s.session(ctx, p, uploadID)
	if err != nil {
		return model.UploadStatus{}, err
	}
	out := model.UploadStatus{Thumbnails: []string{}}
	fail := func(msg string) (model.UploadStatus, error) {
		out.Status, out.Progress, out.Error = "failed", 0, &msg
		return out, nil
	}
	if u.Status == "ABORTED" || u.Status == "EXPIRED" || u.VideoDeleted {
		return fail("This upload was cancelled or has expired.")
	}
	if u.Status != "COMPLETED" {
		out.Status = "processing" // still uploading: the browser shows its own byte progress
		return out, nil
	}
	pr, err := s.repo.Progress(ctx, u.VideoID)
	if err != nil {
		return model.UploadStatus{}, apperr.Internal(err)
	}
	switch {
	case pr.Moderation == "REMOVED":
		return fail("This video was removed by moderation.")
	case u.VideoStatus == "PUBLISHED":
		out.Status, out.Progress = "published", 100
	case u.VideoStatus == "READY":
		out.Status, out.Progress = "ready", 100
		for _, t := range pr.Thumbs {
			if url := s.urls.AssetString(t.Key); url != nil {
				out.Thumbnails = append(out.Thumbnails, *url)
			}
		}
	case u.VideoStatus == "FAILED" || (pr.Failed > 0 && pr.Active == nil):
		return fail("We couldn't process this video. Try uploading it again.")
	default:
		out.Status = "processing"
		if pr.Active != nil && (*pr.Active == "TRANSCODE" || *pr.Active == "HLS") {
			out.Status = "encoding"
		}
		out.Progress = min(99, pr.Done*100/pipelineJobs)
	}
	return out, nil
}

// ---------- assets (thumbnails, captions) ----------

type AssetInput struct {
	Kind        string
	ContentType string
	Size        int64
	Lang        string
}

// Asset prepares a direct-to-storage upload of a thumbnail or caption file.
// Assets live under their final public prefixes (thumbs/, captions/); the file
// is inspected again at publish time, so nothing unchecked is ever listed.
func (s *Upload) Asset(ctx context.Context, p *auth.Principal, ci ClientInfo, uploadID string, in AssetInput) (model.AssetInit, error) {
	if s.store == nil {
		return model.AssetInit{}, errUploadsOff
	}
	u, err := s.session(ctx, p, uploadID)
	if err != nil {
		return model.AssetInit{}, err
	}
	if u.Status == "ABORTED" || u.Status == "EXPIRED" || u.VideoStatus == "PUBLISHED" {
		return model.AssetInit{}, apperr.Conflict("upload_closed", "This upload can't take more files")
	}
	ct := strings.ToLower(strings.TrimSpace(in.ContentType))
	if err := s.limit(ctx, "upload:asset:"+p.UserID, 40, time.Hour); err != nil {
		return model.AssetInit{}, err
	}

	switch in.Kind {
	case "thumbnail":
		ext, ok := imageTypes[ct]
		if !ok || in.Size < 1 || in.Size > MaxThumbBytes {
			return model.AssetInit{}, apperr.Validation(map[string]string{"thumbnail": "Use a JPG, PNG or WebP image under 2 MB"})
		}
		n, err := s.repo.CountThumbs(ctx, u.VideoID)
		if err != nil {
			return model.AssetInit{}, apperr.Internal(err)
		}
		if n >= maxCustomThumbs {
			return model.AssetInit{}, errLimitReached("thumbnails")
		}
		key := "thumbs/" + u.VideoID + "/" + auth.NewUUIDv7() + ext
		id, err := s.repo.AddThumb(ctx, u.VideoID, key)
		if err != nil {
			return model.AssetInit{}, apperr.Internal(err)
		}
		return model.AssetInit{AssetID: id, URL: s.store.PresignPut(key, ct, in.Size, PresignAssetTTL)}, nil

	case "caption":
		ext, ok := captionTypes[ct]
		lang := strings.TrimSpace(in.Lang)
		fields := map[string]string{}
		if !ok || in.Size < 1 || in.Size > captions.MaxBytes {
			fields["caption"] = "Use a .vtt or .srt file under 1 MB"
		}
		if !langRe.MatchString(lang) || len(lang) > 12 {
			fields["lang"] = "Invalid language code"
		}
		if len(fields) > 0 {
			return model.AssetInit{}, apperr.Validation(fields)
		}
		exists, err := s.repo.CaptionExists(ctx, u.VideoID, lang)
		if err != nil {
			return model.AssetInit{}, apperr.Internal(err)
		}
		if !exists {
			n, err := s.repo.CountCaptions(ctx, u.VideoID)
			if err != nil {
				return model.AssetInit{}, apperr.Internal(err)
			}
			if n >= maxCaptionLangs {
				return model.AssetInit{}, errLimitReached("caption languages")
			}
		}
		label := languageNames[lang]
		if label == "" {
			label = lang
		}
		key := "captions/" + u.VideoID + "/" + lang + ext
		id, err := s.repo.AddCaption(ctx, u.VideoID, lang, label, key)
		if err != nil {
			return model.AssetInit{}, apperr.Internal(err)
		}
		return model.AssetInit{AssetID: id, URL: s.store.PresignPut(key, ct, in.Size, PresignAssetTTL)}, nil
	}
	return model.AssetInit{}, apperr.Validation(map[string]string{"kind": "must be thumbnail or caption"})
}

// ---------- metadata ----------

// MetaInput is the body of "save draft" (PATCH) and "publish" (POST). Pointers
// distinguish "not sent" from empty. Thumbnail, Captions and Consent only matter on publish.
type MetaInput struct {
	Title       *string      `json:"title"`
	Description *string      `json:"description"`
	Category    *string      `json:"category"`
	Tags        *[]string    `json:"tags"`
	Visibility  *string      `json:"visibility"`
	Comments    *string      `json:"comments"`
	Language    *string      `json:"language"`
	AgeGate     *bool        `json:"ageGate"`
	Thumbnail   *string      `json:"thumbnail"`
	Captions    []CaptionRef `json:"captions"`
	Consent     *bool        `json:"consent"`
}

type CaptionRef struct {
	Lang    string `json:"lang"`
	AssetID string `json:"assetId"`
}

func tagSlug(name string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(name) {
		if unicode.IsSpace(r) || r == '-' || r == '_' {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte('-')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// patch validates the shared fields and builds the database update.
func (s *Upload) patch(ctx context.Context, in MetaInput) (repository.MetaPatch, error) {
	var m repository.MetaPatch
	f := map[string]string{}

	if in.Title != nil {
		t := strings.TrimSpace(cleanText(*in.Title, false))
		if n := utf8.RuneCountInString(t); n < 1 || n > 100 {
			f["title"] = "1-100 characters"
		}
		m.Title = &t
	}
	if in.Description != nil {
		d := strings.TrimSpace(cleanText(*in.Description, true))
		if utf8.RuneCountInString(d) > 5000 {
			f["description"] = "At most 5,000 characters"
		}
		m.Description = &d
	}
	if in.Category != nil {
		id, err := s.repo.CategoryID(ctx, strings.TrimSpace(*in.Category))
		switch {
		case err != nil:
			return m, apperr.Internal(err)
		case id == 0:
			f["category"] = "Choose a category"
		default:
			m.CategoryID = &id
		}
	}
	if in.Tags != nil {
		if len(*in.Tags) > maxTags {
			f["tags"] = "At most 15 tags"
		}
		seen := map[string]bool{}
		tags := make([]repository.TagIn, 0, len(*in.Tags))
		for _, raw := range *in.Tags {
			name := strings.TrimSpace(cleanText(raw, false))
			slug := tagSlug(name)
			if n := utf8.RuneCountInString(name); n < 1 || n > 30 || utf8.RuneCountInString(slug) < 1 || utf8.RuneCountInString(slug) > 30 || strings.ContainsAny(name, ",#") {
				f["tags"] = "Tags are 1-30 characters without commas"
				continue
			}
			if !seen[slug] {
				seen[slug] = true
				tags = append(tags, repository.TagIn{Name: name, Slug: slug})
			}
		}
		m.Tags = &tags
	}
	if in.Visibility != nil {
		switch v := strings.ToLower(*in.Visibility); v {
		case "public", "unlisted", "private":
			u := strings.ToUpper(v)
			m.Visibility = &u
		default:
			f["visibility"] = "must be public, unlisted or private"
		}
	}
	if in.Comments != nil {
		switch v := strings.ToLower(*in.Comments); v {
		case "on", "off", "review":
			u := strings.ToUpper(v)
			m.CommentsMode = &u
		default:
			f["comments"] = "must be on, off or review"
		}
	}
	if in.Language != nil {
		if !langRe.MatchString(*in.Language) || len(*in.Language) > 12 {
			f["language"] = "Invalid language code"
		}
		m.Language = in.Language
	}
	m.AgeRestricted = in.AgeGate
	if len(f) > 0 {
		return m, apperr.Validation(f)
	}
	return m, nil
}

var errVideoMissing = apperr.NotFound("video_not_found", "Video not found")

// SaveDraft stores metadata without publishing.
func (s *Upload) SaveDraft(ctx context.Context, p *auth.Principal, ci ClientInfo, videoID string, in MetaInput) error {
	if !IsUUID(videoID) {
		return errVideoMissing
	}
	m, err := s.patch(ctx, in)
	if err != nil {
		return err
	}
	if err := s.limit(ctx, "video:meta:"+p.UserID, 60, time.Minute); err != nil {
		return err
	}
	err = s.repo.UpdateMetadata(ctx, videoID, p.UserID, m, s.auditEntry(p, ci, "video.metadata", "video", videoID, nil))
	switch {
	case errors.Is(err, repository.ErrVideoNotFound):
		return errVideoMissing
	case err != nil:
		return apperr.Internal(err)
	}
	return nil
}

// ---------- publish ----------

func sniffImage(b []byte) bool {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF: // JPEG
		return true
	case len(b) >= 8 && bytes.Equal(b[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}): // PNG
		return true
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return true
	}
	return false
}

// thumbnailChoice resolves the thumbnail the creator picked: an uploaded asset
// (checked for size and image signature here) or one of the generated ones.
func (s *Upload) thumbnailChoice(ctx context.Context, videoID, choice string) (string, error) {
	bad := func(msg string) error { return apperr.Validation(map[string]string{"thumbnail": msg}) }
	choice = strings.TrimSpace(choice)
	if choice == "" {
		return "", nil
	}
	if IsUUID(choice) {
		key, err := s.repo.ThumbKey(ctx, videoID, choice)
		if err != nil {
			return "", apperr.Internal(err)
		}
		if key != "" {
			info, err := s.store.Head(ctx, key)
			if errors.Is(err, storage.ErrNotFound) {
				return "", bad("The thumbnail was not uploaded")
			}
			if err != nil {
				return "", errStorage
			}
			if info.Size < 1 || info.Size > MaxThumbBytes {
				_ = s.store.Delete(ctx, key)
				return "", bad("The thumbnail is too large")
			}
			head, err := s.store.GetRange(ctx, key, 0, 15)
			if err != nil {
				return "", errStorage
			}
			if !sniffImage(head) {
				_ = s.store.Delete(ctx, key)
				return "", bad("The thumbnail is not a valid image")
			}
			return choice, nil
		}
	}
	gen, err := s.repo.GeneratedThumbs(ctx, videoID)
	if err != nil {
		return "", apperr.Internal(err)
	}
	for _, t := range gen {
		if t.ID == choice {
			return t.ID, nil
		}
		if u := s.urls.AssetString(t.Key); u != nil && *u == choice {
			return t.ID, nil
		}
	}
	return "", bad("Unknown thumbnail")
}

// captionFiles turns each uploaded caption into a clean WebVTT file at its final key.
func (s *Upload) captionFiles(ctx context.Context, videoID string, refs []CaptionRef) ([]repository.CaptionReady, error) {
	if len(refs) > maxCaptionLangs {
		return nil, apperr.Validation(map[string]string{"captions": "Too many caption files"})
	}
	out := make([]repository.CaptionReady, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		bad := func(msg string) error { return apperr.Validation(map[string]string{"captions": msg}) }
		if !IsUUID(ref.AssetID) {
			return nil, bad("Invalid caption reference")
		}
		lang, key, err := s.repo.CaptionKey(ctx, videoID, ref.AssetID)
		if err != nil {
			return nil, apperr.Internal(err)
		}
		if key == "" || seen[lang] {
			return nil, bad("Unknown caption file")
		}
		seen[lang] = true
		info, err := s.store.Head(ctx, key)
		if errors.Is(err, storage.ErrNotFound) {
			return nil, bad("The " + lang + " caption was not uploaded")
		}
		if err != nil {
			return nil, errStorage
		}
		if info.Size < 1 || info.Size > captions.MaxBytes {
			_ = s.store.Delete(ctx, key)
			return nil, bad("The " + lang + " caption file is too large")
		}
		raw, err := s.store.GetRange(ctx, key, 0, info.Size-1)
		if err != nil {
			return nil, errStorage
		}
		vtt, err := captions.ToVTT(raw, strings.HasSuffix(key, ".srt"))
		if err != nil {
			_ = s.store.Delete(ctx, key)
			return nil, bad("The " + lang + " caption file is not valid")
		}
		final := "captions/" + videoID + "/" + lang + ".vtt"
		if err := s.store.PutBytes(ctx, final, "text/vtt; charset=utf-8", "public, max-age=3600", vtt); err != nil {
			return nil, errStorage
		}
		if key != final {
			_ = s.store.Delete(ctx, key)
		}
		out = append(out, repository.CaptionReady{ID: ref.AssetID, Key: final})
	}
	return out, nil
}

// Publish makes a READY video live. The creator must confirm that everyone in
// the video is 18+, has consented, and that they hold the rights; the
// declaration is stored (append-only) with the video and the hashed IP.
func (s *Upload) Publish(ctx context.Context, p *auth.Principal, ci ClientInfo, videoID string, in MetaInput) error {
	if s.store == nil {
		return errUploadsOff
	}
	if !IsUUID(videoID) {
		return errVideoMissing
	}
	if in.Consent == nil || !*in.Consent {
		return apperr.Validation(map[string]string{"consent": "You must confirm that everyone in the video is 18+ and consented"})
	}
	m, err := s.patch(ctx, in)
	if err != nil {
		return err
	}
	if err := s.limit(ctx, "video:publish:"+p.UserID, 30, time.Hour); err != nil {
		return err
	}

	thumbID := ""
	if in.Thumbnail != nil {
		if thumbID, err = s.thumbnailChoice(ctx, videoID, *in.Thumbnail); err != nil {
			return err
		}
	}
	caps, err := s.captionFiles(ctx, videoID, in.Captions)
	if err != nil {
		return err
	}

	vis, err := s.repo.Publish(ctx, repository.PublishParams{
		VideoID: videoID, UserID: p.UserID, Patch: m, PrimaryThumbID: thumbID, Captions: caps,
		ConsentVersion: ConsentVersion, IPHash: auth.HashIP(s.ipKey, ci.IP),
		Audit: s.auditEntry(p, ci, "video.publish", "video", videoID, map[string]any{"consent_version": ConsentVersion}),
	})
	switch {
	case errors.Is(err, repository.ErrVideoNotFound):
		return errVideoMissing
	case errors.Is(err, repository.ErrNotReady):
		return apperr.Conflict("not_ready", "The video is still processing")
	case errors.Is(err, repository.ErrAlreadyPublished):
		return apperr.Conflict("already_published", "This video is already published")
	case errors.Is(err, repository.ErrModerated):
		return apperr.Forbidden("video_removed", "This video was removed by moderation")
	case errors.Is(err, repository.ErrCategoryRequired):
		return apperr.Validation(map[string]string{"category": "Choose a category"})
	case errors.Is(err, repository.ErrShortTooLarge):
		return apperr.New(http.StatusUnprocessableEntity, "file_too_large_for_short", "Videos of 60 seconds or less can be at most 200 MiB")
	case err != nil:
		return apperr.Internal(err)
	}

	if vis == "PUBLIC" { // after the commit: a slow fan-out must never delay or undo the publish
		bg := context.WithoutCancel(ctx)
		ids, err := s.repo.FanOutNewVideo(bg, videoID, p.UserID, NewVideoFanOutMax)
		if err != nil {
			s.log.WarnContext(ctx, "new-video notifications failed", "err", err.Error())
		} else if s.notify != nil {
			for _, id := range ids {
				s.notify.Notify(bg, id)
			}
		}
	}
	return nil
}

// ---------- janitor ----------

// ExpireStale closes upload sessions past their deadline and frees their storage.
// It is safe to run on every API instance (the database hands each row to one).
func (s *Upload) ExpireStale(ctx context.Context) (int, error) {
	exp, err := s.repo.ExpireUploads(ctx, janitorBatch)
	if err != nil {
		return 0, err
	}
	if s.store == nil {
		return len(exp), nil
	}
	for _, e := range exp {
		if err := s.store.AbortMultipart(ctx, e.StorageKey, e.ProviderUploadID); err != nil {
			s.log.WarnContext(ctx, "abort expired upload failed", "upload", e.ID, "err", err.Error())
		}
	}
	return len(exp), nil
}
