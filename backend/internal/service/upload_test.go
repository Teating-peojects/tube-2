package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/redxtube/api/internal/apperr"
	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/media"
	"github.com/redxtube/api/internal/repository"
	"github.com/redxtube/api/internal/storage"
)

// ---------- fakes ----------

type okLimiter struct{ n int64 }

func (l *okLimiter) Hit(context.Context, string, time.Duration) (int64, time.Duration, error) {
	l.n++
	return 1, time.Minute, nil
}
func (l *okLimiter) Peek(context.Context, string) (int64, time.Duration, error) { return 0, 0, nil }
func (l *okLimiter) Reset(context.Context, string) error                        { return nil }

type fakeStorage struct {
	mu        sync.Mutex
	objects   map[string][]byte
	parts     []storage.Part
	aborted   []string
	deleted   []string
	completed bool
	size      int64 // object size after completion
	head      []byte
}

func (f *fakeStorage) CreateMultipart(context.Context, string, string) (string, error) {
	return "prov-1", nil
}
func (f *fakeStorage) PresignUploadPart(key, id string, n int, _ time.Duration) string {
	return "https://s3.test/" + key + "?part=" + string(rune('0'+n))
}
func (f *fakeStorage) ListParts(context.Context, string, string) ([]storage.Part, error) {
	return f.parts, nil
}
func (f *fakeStorage) CompleteMultipart(context.Context, string, string, []storage.Part) error {
	f.completed = true
	return nil
}
func (f *fakeStorage) AbortMultipart(_ context.Context, key, _ string) error {
	f.aborted = append(f.aborted, key)
	return nil
}
func (f *fakeStorage) PresignPut(key, _ string, _ int64, _ time.Duration) string {
	return "https://s3.test/put/" + key
}
func (f *fakeStorage) Head(_ context.Context, key string) (storage.ObjectInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if b, ok := f.objects[key]; ok {
		return storage.ObjectInfo{Size: int64(len(b))}, nil
	}
	if f.completed {
		return storage.ObjectInfo{Size: f.size}, nil
	}
	return storage.ObjectInfo{}, storage.ErrNotFound
}
func (f *fakeStorage) GetRange(_ context.Context, key string, s, e int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.objects[key]
	if !ok {
		return f.head, nil
	}
	if e >= int64(len(b)) {
		e = int64(len(b)) - 1
	}
	return b[s : e+1], nil
}
func (f *fakeStorage) Download(context.Context, string, io.Writer) (int64, error) { return 0, nil }
func (f *fakeStorage) Put(context.Context, string, string, string, io.Reader, int64) error {
	return nil
}
func (f *fakeStorage) PutBytes(_ context.Context, key, _, _ string, b []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.objects == nil {
		f.objects = map[string][]byte{}
	}
	f.objects[key] = b
	return nil
}
func (f *fakeStorage) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, key)
	delete(f.objects, key)
	return nil
}
func (f *fakeStorage) DeletePrefix(context.Context, string) (int, error) { return 0, nil }

type fakeUploads struct {
	sess      *repository.UploadSession
	open      int
	created   *repository.UploadInit
	begun     bool
	reverted  bool
	finished  bool
	discarded bool
	pub       *repository.PublishParams
	pubErr    error
	pubVis    string
	fanned    bool
	thumbs    map[string]string // id -> key
	caps      map[string][2]string
}

func (f *fakeUploads) OpenUploads(context.Context, string) (int, error) { return f.open, nil }
func (f *fakeUploads) CreateUpload(_ context.Context, n repository.UploadInit) error {
	f.created = &n
	return nil
}
func (f *fakeUploads) UploadByID(context.Context, string, string) (*repository.UploadSession, error) {
	return f.sess, nil
}
func (f *fakeUploads) BeginComplete(context.Context, string) (bool, error) {
	if f.sess.Status != "INITIATED" {
		return false, nil
	}
	f.begun = true
	return true, nil
}
func (f *fakeUploads) RevertComplete(context.Context, string) error { f.reverted = true; return nil }
func (f *fakeUploads) FinishComplete(context.Context, *repository.UploadSession, repository.AuditEntry) error {
	f.finished = true
	return nil
}
func (f *fakeUploads) DiscardUpload(context.Context, *repository.UploadSession, *repository.AuditEntry) error {
	f.discarded = true
	return nil
}
func (f *fakeUploads) ExpireUploads(context.Context, int) ([]repository.ExpiredUpload, error) {
	return []repository.ExpiredUpload{{ID: "u1", StorageKey: "k", ProviderUploadID: "p"}}, nil
}
func (f *fakeUploads) Progress(context.Context, string) (repository.Progress, error) {
	return repository.Progress{Moderation: "ACTIVE"}, nil
}
func (f *fakeUploads) GeneratedThumbs(context.Context, string) ([]repository.ThumbRow, error) {
	return nil, nil
}
func (f *fakeUploads) CountThumbs(context.Context, string) (int, error)   { return 0, nil }
func (f *fakeUploads) CountCaptions(context.Context, string) (int, error) { return 0, nil }
func (f *fakeUploads) AddThumb(context.Context, string, string) (string, error) {
	return auth.NewUUIDv7(), nil
}
func (f *fakeUploads) CaptionExists(context.Context, string, string) (bool, error) { return false, nil }
func (f *fakeUploads) AddCaption(context.Context, string, string, string, string) (string, error) {
	return auth.NewUUIDv7(), nil
}
func (f *fakeUploads) ThumbKey(_ context.Context, _, id string) (string, error) {
	return f.thumbs[id], nil
}
func (f *fakeUploads) CaptionKey(_ context.Context, _, id string) (string, string, error) {
	c := f.caps[id]
	return c[0], c[1], nil
}
func (f *fakeUploads) CategoryID(context.Context, string) (int32, error) { return 3, nil }
func (f *fakeUploads) UpdateMetadata(context.Context, string, string, repository.MetaPatch, repository.AuditEntry) error {
	return nil
}
func (f *fakeUploads) Publish(_ context.Context, p repository.PublishParams) (string, error) {
	f.pub = &p
	return f.pubVis, f.pubErr
}
func (f *fakeUploads) FanOutNewVideo(context.Context, string, string, int) ([]string, error) {
	f.fanned = true
	return []string{"a", "b"}, nil
}

type countNotifier struct{ ids []string }

func (c *countNotifier) Notify(_ context.Context, id string) { c.ids = append(c.ids, id) }

// ---------- helpers ----------

var (
	testUser = &auth.Principal{UserID: "11111111-1111-7111-8111-111111111111", Role: auth.RoleUser, EmailVerified: true}
	testCI   = ClientInfo{IP: "203.0.113.9", RequestID: "r1"}
	vidID    = "22222222-2222-7222-8222-222222222222"
	upID     = "33333333-3333-7333-8333-333333333333"
)

func newUploadSvc(db *fakeUploads, st storage.API) (*Upload, *countNotifier) {
	n := &countNotifier{}
	return NewUpload(db, st, &okLimiter{}, []byte("0123456789abcdef0123456789abcdef"), media.New("https://cdn.test"), n,
		slog.New(slog.NewTextHandler(io.Discard, nil))), n
}

func ecode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		return ""
	}
	return apperr.From(err).Code
}

func mp4Head() []byte { return append([]byte{0, 0, 0, 0x20}, []byte("ftypisom0000")...) }

func session(status string, size int64) *repository.UploadSession {
	return &repository.UploadSession{ID: upID, UserID: testUser.UserID, VideoID: vidID, StorageKey: "uploads/" + vidID + "/original.mp4",
		ProviderUploadID: "prov-1", Status: status, VideoStatus: "PROCESSING", DeclaredSize: size,
		PartSize: UploadPartSize, PartCount: int32((size + UploadPartSize - 1) / UploadPartSize), ExpiresAt: time.Now().Add(time.Hour)}
}

// ---------- init ----------

func TestInitValidation(t *testing.T) {
	cases := []struct {
		name string
		in   InitInput
	}{
		{"bad extension", InitInput{Filename: "x.exe", Size: 10}},
		{"empty", InitInput{Filename: "x.mp4", Size: 0}},
		{"too big", InitInput{Filename: "x.mp4", Size: MaxUploadBytes + 1}},
		{"not video", InitInput{Filename: "x.mp4", Size: 10, ContentType: "text/html"}},
	}
	for _, c := range cases {
		db := &fakeUploads{}
		svc, _ := newUploadSvc(db, &fakeStorage{})
		_, err := svc.Init(context.Background(), testUser, testCI, c.in)
		if ecode(t, err) != "validation_failed" || db.created != nil {
			t.Errorf("%s: code=%q created=%v", c.name, ecode(t, err), db.created != nil)
		}
	}
}

func TestInitPartsAndKey(t *testing.T) {
	db := &fakeUploads{}
	svc, _ := newUploadSvc(db, &fakeStorage{})
	out, err := svc.Init(context.Background(), testUser, testCI, InitInput{Filename: `C:\fake\my clip.final.MP4`, Size: 2*UploadPartSize + 1, ContentType: "video/mp4"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Parts) != 3 || out.PartSize != UploadPartSize {
		t.Fatalf("parts=%d partSize=%d", len(out.Parts), out.PartSize)
	}
	if db.created.Title != "my clip.final" || db.created.Filename != "my clip.final.MP4" {
		t.Errorf("title/filename: %q / %q", db.created.Title, db.created.Filename)
	}
	if want := "uploads/" + out.VideoID + "/original.mp4"; db.created.StorageKey != want {
		t.Errorf("key %q want %q", db.created.StorageKey, want)
	}
	if db.created.ContentType != "video/mp4" {
		t.Errorf("content type must come from the extension, got %q", db.created.ContentType)
	}
}

func TestInitLimitsAndDisabled(t *testing.T) {
	db := &fakeUploads{open: MaxOpenUploads}
	svc, _ := newUploadSvc(db, &fakeStorage{})
	_, err := svc.Init(context.Background(), testUser, testCI, InitInput{Filename: "a.mp4", Size: 5})
	if ecode(t, err) != "limit_reached" {
		t.Errorf("open cap: %q", ecode(t, err))
	}
	off, _ := newUploadSvc(&fakeUploads{}, nil)
	_, err = off.Init(context.Background(), testUser, testCI, InitInput{Filename: "a.mp4", Size: 5})
	if apperr.From(err).Status != http.StatusServiceUnavailable || ecode(t, err) != "uploads_disabled" {
		t.Errorf("disabled: %v", err)
	}
}

// ---------- complete ----------

func goodParts(size int64) ([]storage.Part, []PartIn) {
	n := int((size + UploadPartSize - 1) / UploadPartSize)
	var have []storage.Part
	var in []PartIn
	for i := 1; i <= n; i++ {
		sz := int64(UploadPartSize)
		if i == n {
			sz = size - int64(n-1)*UploadPartSize
		}
		have = append(have, storage.Part{Number: i, ETag: `"e` + string(rune('0'+i)) + `"`, Size: sz})
		in = append(in, PartIn{PartNumber: i, ETag: "e" + string(rune('0'+i))})
	}
	return have, in
}

func TestCompleteHappyPath(t *testing.T) {
	size := int64(UploadPartSize + 100)
	have, in := goodParts(size)
	st := &fakeStorage{parts: have, size: size, head: mp4Head()}
	db := &fakeUploads{sess: session("INITIATED", size)}
	svc, _ := newUploadSvc(db, st)
	if err := svc.Complete(context.Background(), testUser, testCI, upID, in); err != nil {
		t.Fatal(err)
	}
	if !st.completed || !db.finished || db.reverted || db.discarded {
		t.Errorf("completed=%v finished=%v reverted=%v discarded=%v", st.completed, db.finished, db.reverted, db.discarded)
	}
}

func TestCompleteIsIdempotent(t *testing.T) {
	db := &fakeUploads{sess: session("COMPLETED", 10)}
	svc, _ := newUploadSvc(db, &fakeStorage{})
	if err := svc.Complete(context.Background(), testUser, testCI, upID, []PartIn{{1, "x"}}); err != nil {
		t.Fatal(err)
	}
	if db.begun {
		t.Error("a completed upload must not be completed again")
	}
}

func TestCompletePartsMismatch(t *testing.T) {
	size := int64(UploadPartSize + 100)
	have, in := goodParts(size)
	cases := map[string]func(){
		"missing part": func() { have = have[:1] },
		"wrong etag":   func() { have[0].ETag = `"other"` },
		"wrong size":   func() { have[1].Size = 7 },
	}
	names := make([]string, 0, len(cases))
	for n := range cases {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		have, in = goodParts(size)
		cases[name]()
		st := &fakeStorage{parts: have, size: size, head: mp4Head()}
		db := &fakeUploads{sess: session("INITIATED", size)}
		svc, _ := newUploadSvc(db, st)
		err := svc.Complete(context.Background(), testUser, testCI, upID, in)
		if ecode(t, err) != "parts_mismatch" || !db.reverted || st.completed || db.finished {
			t.Errorf("%s: code=%q reverted=%v completed=%v finished=%v", name, ecode(t, err), db.reverted, st.completed, db.finished)
		}
	}
}

func TestCompleteRejectsBadPartList(t *testing.T) {
	size := int64(UploadPartSize + 100)
	for name, in := range map[string][]PartIn{
		"too few":   {{1, "a"}},
		"duplicate": {{1, "a"}, {1, "b"}},
		"range":     {{1, "a"}, {3, "b"}},
		"no etag":   {{1, "a"}, {2, ""}},
	} {
		db := &fakeUploads{sess: session("INITIATED", size)}
		svc, _ := newUploadSvc(db, &fakeStorage{})
		if err := svc.Complete(context.Background(), testUser, testCI, upID, in); ecode(t, err) != "validation_failed" || db.begun {
			t.Errorf("%s: %q begun=%v", name, ecode(t, err), db.begun)
		}
	}
}

func TestCompleteRejectsNonVideoAndSizeMismatch(t *testing.T) {
	size := int64(100)
	have, in := goodParts(size)

	st := &fakeStorage{parts: have, size: size, head: []byte("<html>not a video at all</html>")}
	db := &fakeUploads{sess: session("INITIATED", size)}
	svc, _ := newUploadSvc(db, st)
	err := svc.Complete(context.Background(), testUser, testCI, upID, in)
	if ecode(t, err) != "invalid_file" || !db.discarded || len(st.deleted) != 1 || db.finished {
		t.Errorf("non-video: %q discarded=%v deleted=%v", ecode(t, err), db.discarded, st.deleted)
	}

	st = &fakeStorage{parts: have, size: size + 1, head: mp4Head()}
	db = &fakeUploads{sess: session("INITIATED", size)}
	svc, _ = newUploadSvc(db, st)
	err = svc.Complete(context.Background(), testUser, testCI, upID, in)
	if ecode(t, err) != "size_mismatch" || !db.discarded {
		t.Errorf("size: %q", ecode(t, err))
	}
}

func TestCompleteInProgressAndClosed(t *testing.T) {
	for status, want := range map[string]string{"COMPLETING": "upload_in_progress", "ABORTED": "upload_closed", "EXPIRED": "upload_closed"} {
		db := &fakeUploads{sess: session(status, 10)}
		svc, _ := newUploadSvc(db, &fakeStorage{})
		if err := svc.Complete(context.Background(), testUser, testCI, upID, []PartIn{{1, "x"}}); ecode(t, err) != want {
			t.Errorf("%s: %q", status, ecode(t, err))
		}
	}
	db := &fakeUploads{}
	svc, _ := newUploadSvc(db, &fakeStorage{})
	if err := svc.Complete(context.Background(), testUser, testCI, "not-a-uuid", nil); ecode(t, err) != "upload_not_found" {
		t.Errorf("bad id: %q", ecode(t, err))
	}
	db.sess = nil
	if err := svc.Complete(context.Background(), testUser, testCI, upID, nil); ecode(t, err) != "upload_not_found" {
		t.Errorf("unknown/other user's upload: %q", ecode(t, err))
	}
}

func TestValidContainer(t *testing.T) {
	ok := [][]byte{mp4Head(), {0x1A, 0x45, 0xDF, 0xA3, 1, 2}, []byte("RIFF\x00\x00\x00\x00AVI LIST")}
	for i, b := range ok {
		if !validContainer(b) {
			t.Errorf("valid #%d rejected", i)
		}
	}
	for i, b := range [][]byte{nil, []byte("MZ\x90\x00"), []byte("GIF89a......."), []byte("RIFF\x00\x00\x00\x00WAVEfmt ")} {
		if validContainer(b) {
			t.Errorf("invalid #%d accepted", i)
		}
	}
}

// ---------- abort / status ----------

func TestAbort(t *testing.T) {
	st := &fakeStorage{}
	db := &fakeUploads{sess: session("INITIATED", 10)}
	svc, _ := newUploadSvc(db, st)
	if err := svc.Abort(context.Background(), testUser, testCI, upID); err != nil {
		t.Fatal(err)
	}
	if len(st.aborted) != 1 || !db.discarded {
		t.Errorf("aborted=%v discarded=%v", st.aborted, db.discarded)
	}

	db = &fakeUploads{sess: session("COMPLETED", 10)}
	db.sess.VideoStatus = "PUBLISHED"
	svc, _ = newUploadSvc(db, st)
	if err := svc.Abort(context.Background(), testUser, testCI, upID); ecode(t, err) != "already_published" || db.discarded {
		t.Errorf("published: %q", ecode(t, err))
	}
}

func TestStatusMapping(t *testing.T) {
	cases := []struct {
		sess        string
		video       string
		wantStatus  string
		wantErrText bool
	}{
		{"INITIATED", "PROCESSING", "processing", false},
		{"COMPLETED", "PROCESSING", "processing", false},
		{"COMPLETED", "READY", "ready", false},
		{"COMPLETED", "PUBLISHED", "published", false},
		{"COMPLETED", "FAILED", "failed", true},
		{"EXPIRED", "PROCESSING", "failed", true},
	}
	for _, c := range cases {
		db := &fakeUploads{sess: session(c.sess, 10)}
		db.sess.VideoStatus = c.video
		svc, _ := newUploadSvc(db, &fakeStorage{})
		out, err := svc.Status(context.Background(), testUser, upID)
		if err != nil {
			t.Fatal(err)
		}
		if out.Status != c.wantStatus || (out.Error != nil) != c.wantErrText || out.Thumbnails == nil {
			t.Errorf("%s/%s -> %+v", c.sess, c.video, out)
		}
	}
}

// ---------- assets ----------

func TestAssetValidation(t *testing.T) {
	svc, _ := newUploadSvc(&fakeUploads{sess: session("COMPLETED", 10)}, &fakeStorage{})
	bad := []AssetInput{
		{Kind: "thumbnail", ContentType: "image/gif", Size: 100},
		{Kind: "thumbnail", ContentType: "image/png", Size: MaxThumbBytes + 1},
		{Kind: "caption", ContentType: "text/vtt", Size: 100, Lang: "EN US"},
		{Kind: "caption", ContentType: "text/html", Size: 100, Lang: "en"},
		{Kind: "video", ContentType: "video/mp4", Size: 5},
	}
	for i, in := range bad {
		if _, err := svc.Asset(context.Background(), testUser, testCI, upID, in); ecode(t, err) != "validation_failed" {
			t.Errorf("bad #%d: %q", i, ecode(t, err))
		}
	}
	out, err := svc.Asset(context.Background(), testUser, testCI, upID, AssetInput{Kind: "caption", ContentType: "text/vtt", Size: 100, Lang: "bn"})
	if err != nil || out.AssetID == "" || out.URL == "" {
		t.Errorf("good caption: %+v %v", out, err)
	}
}

// ---------- publish ----------

func yes() *bool           { b := true; return &b }
func str(s string) *string { return &s }

func TestPublishRequiresConsent(t *testing.T) {
	db := &fakeUploads{}
	svc, _ := newUploadSvc(db, &fakeStorage{})
	for _, c := range []*bool{nil, new(bool)} {
		err := svc.Publish(context.Background(), testUser, testCI, vidID, MetaInput{Consent: c})
		if ecode(t, err) != "validation_failed" || db.pub != nil {
			t.Errorf("consent=%v -> %q published=%v", c, ecode(t, err), db.pub != nil)
		}
	}
}

func TestPublishErrorMapping(t *testing.T) {
	cases := map[error]struct {
		status int
		code   string
	}{
		repository.ErrNotReady:         {409, "not_ready"},
		repository.ErrAlreadyPublished: {409, "already_published"},
		repository.ErrModerated:        {403, "video_removed"},
		repository.ErrShortTooLarge:    {422, "file_too_large_for_short"},
		repository.ErrVideoNotFound:    {404, "video_not_found"},
	}
	for in, want := range cases {
		db := &fakeUploads{pubErr: in}
		svc, _ := newUploadSvc(db, &fakeStorage{})
		err := svc.Publish(context.Background(), testUser, testCI, vidID, MetaInput{Consent: yes()})
		if e := apperr.From(err); e.Status != want.status || e.Code != want.code {
			t.Errorf("%v -> %d %s", in, e.Status, e.Code)
		}
	}
	db := &fakeUploads{pubErr: errors.New("boom")}
	svc, _ := newUploadSvc(db, &fakeStorage{})
	if err := svc.Publish(context.Background(), testUser, testCI, vidID, MetaInput{Consent: yes()}); apperr.From(err).Status != 500 {
		t.Errorf("unknown error must be 500: %v", err)
	}
}

func TestPublishPublicFansOut(t *testing.T) {
	for vis, wantFan := range map[string]bool{"PUBLIC": true, "UNLISTED": false, "PRIVATE": false} {
		db := &fakeUploads{pubVis: vis}
		svc, n := newUploadSvc(db, &fakeStorage{})
		if err := svc.Publish(context.Background(), testUser, testCI, vidID, MetaInput{Consent: yes(), Title: str("Hello")}); err != nil {
			t.Fatal(err)
		}
		if db.fanned != wantFan || (len(n.ids) == 2) != wantFan {
			t.Errorf("%s: fanned=%v notified=%v", vis, db.fanned, n.ids)
		}
		if db.pub.ConsentVersion != ConsentVersion || len(db.pub.IPHash) != 32 || *db.pub.Patch.Title != "Hello" {
			t.Errorf("%s: params %+v", vis, db.pub)
		}
	}
}

func TestPublishValidatesMetadata(t *testing.T) {
	long := make([]rune, 101)
	for i := range long {
		long[i] = 'a'
	}
	tags := []string{"ok", "bad,tag"}
	vis := "everyone"
	cases := []MetaInput{
		{Consent: yes(), Title: str("")},
		{Consent: yes(), Title: str(string(long))},
		{Consent: yes(), Tags: &tags},
		{Consent: yes(), Visibility: &vis},
	}
	for i, in := range cases {
		db := &fakeUploads{}
		svc, _ := newUploadSvc(db, &fakeStorage{})
		if err := svc.Publish(context.Background(), testUser, testCI, vidID, in); ecode(t, err) != "validation_failed" || db.pub != nil {
			t.Errorf("case %d: %q", i, ecode(t, err))
		}
	}
}

func TestPublishChecksThumbnailImage(t *testing.T) {
	thumbID := auth.NewUUIDv7()
	key := "thumbs/" + vidID + "/x.png"
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0}

	st := &fakeStorage{objects: map[string][]byte{key: []byte("<script>alert(1)</script>")}}
	db := &fakeUploads{thumbs: map[string]string{thumbID: key}}
	svc, _ := newUploadSvc(db, st)
	err := svc.Publish(context.Background(), testUser, testCI, vidID, MetaInput{Consent: yes(), Thumbnail: str(thumbID)})
	if ecode(t, err) != "validation_failed" || db.pub != nil || len(st.deleted) != 1 {
		t.Errorf("fake image: %q deleted=%v", ecode(t, err), st.deleted)
	}

	st = &fakeStorage{objects: map[string][]byte{key: png}}
	db = &fakeUploads{thumbs: map[string]string{thumbID: key}, pubVis: "PRIVATE"}
	svc, _ = newUploadSvc(db, st)
	if err := svc.Publish(context.Background(), testUser, testCI, vidID, MetaInput{Consent: yes(), Thumbnail: str(thumbID)}); err != nil {
		t.Fatal(err)
	}
	if db.pub.PrimaryThumbID != thumbID {
		t.Errorf("primary thumb %q", db.pub.PrimaryThumbID)
	}
}

func TestPublishConvertsCaptionsToVTT(t *testing.T) {
	capID := auth.NewUUIDv7()
	key := "captions/" + vidID + "/en.srt"
	srt := "1\n00:00:01,000 --> 00:00:02,000\nHello\n"
	st := &fakeStorage{objects: map[string][]byte{key: []byte(srt)}}
	db := &fakeUploads{caps: map[string][2]string{capID: {"en", key}}, pubVis: "PRIVATE"}
	svc, _ := newUploadSvc(db, st)
	if err := svc.Publish(context.Background(), testUser, testCI, vidID, MetaInput{Consent: yes(), Captions: []CaptionRef{{Lang: "en", AssetID: capID}}}); err != nil {
		t.Fatal(err)
	}
	final := "captions/" + vidID + "/en.vtt"
	got := string(st.objects[final])
	if len(got) < 6 || got[:6] != "WEBVTT" || !contains(got, "00:00:01.000 --> 00:00:02.000") {
		t.Errorf("not converted: %q", got)
	}
	if len(db.pub.Captions) != 1 || db.pub.Captions[0].Key != final {
		t.Errorf("captions %+v", db.pub.Captions)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ---------- janitor / helpers ----------

func TestExpireStaleAbortsMultipart(t *testing.T) {
	st := &fakeStorage{}
	svc, _ := newUploadSvc(&fakeUploads{}, st)
	n, err := svc.ExpireStale(context.Background())
	if err != nil || n != 1 || len(st.aborted) != 1 {
		t.Errorf("n=%d err=%v aborted=%v", n, err, st.aborted)
	}
}

func TestTagSlugAndTitle(t *testing.T) {
	if got := tagSlug("  Hello   World_Foo "); got != "hello-world-foo" {
		t.Errorf("slug %q", got)
	}
	if got := titleFromFilename("noext"); got == "" {
		t.Error("empty title")
	}
	if got := titleFromFilename(".mp4"); got != "Untitled" {
		t.Errorf("got %q", got)
	}
}
