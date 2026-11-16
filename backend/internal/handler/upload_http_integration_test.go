package handler_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redxtube/api/internal/service"
)

// The upload API through the real router (CSRF, sessions, verified-email guard, JSON) on a real PostgreSQL, with an
// in-memory object store that behaves like S3 (see memstore_test.go). The test plays the browser: it PUTs every
// part to the presigned URLs it was given, then calls complete.

const mib = 1 << 20

// fakeVideo looks like an MP4 to the signature check (an ftyp box at offset 4).
func fakeVideo(n int) []byte {
	b := bytes.Repeat([]byte{0xAB}, n)
	copy(b, []byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'm', 'p', '4', '2'})
	return b
}

func fakeHTML(n int) []byte {
	b := bytes.Repeat([]byte{' '}, n)
	copy(b, "<!doctype html><html><body>not a video</body></html>")
	return b
}

type initResp struct {
	UploadID, VideoID string
	PartSize          int
	URLs              []string
}

func uploadEnv(t *testing.T) (*authEnv, *client) {
	t.Helper()
	limits := service.DefaultUploadLimits()
	limits.PartSize = 5 * mib // the S3 minimum, keeps the test files small
	limits.CleanupDelay = 0
	e := setupAuthFull(t, nil, newMemStore(), limits)
	c, _, _ := e.verifiedUser()
	return e, c
}

func (c *client) initUpload(filename string, size int) *resp {
	c.e.t.Helper()
	return c.post("/api/v1/upload/init", map[string]any{"filename": filename, "size": size, "contentType": "video/mp4"})
}

func (c *client) mustInit(size int) initResp {
	c.e.t.Helper()
	r := c.initUpload("clip.mp4", size)
	if r.code != 201 {
		c.e.t.Fatalf("init: %d %s", r.code, r.body)
	}
	return readInit(c.e.t, r)
}

func readInit(t *testing.T, r *resp) initResp {
	t.Helper()
	out := initResp{UploadID: r.str("uploadId"), VideoID: r.str("videoId")}
	if f, ok := r.m["partSize"].(float64); ok {
		out.PartSize = int(f)
	}
	parts, _ := r.m["parts"].([]any)
	for i, p := range parts {
		pm, _ := p.(map[string]any)
		if n, _ := pm["partNumber"].(float64); int(n) != i+1 {
			t.Fatalf("part %d is numbered %v", i+1, pm["partNumber"])
		}
		out.URLs = append(out.URLs, fmt.Sprint(pm["url"]))
	}
	if out.UploadID == "" || out.VideoID == "" || len(out.URLs) == 0 || out.PartSize == 0 {
		t.Fatalf("incomplete init response: %s", r.body)
	}
	return out
}

// send is the browser uploading: every part to its URL, returning the list complete expects.
func (e *authEnv) send(t *testing.T, in initResp, data []byte) []map[string]any {
	t.Helper()
	var parts []map[string]any
	for i, u := range in.URLs {
		from, to := i*in.PartSize, (i+1)*in.PartSize
		if to > len(data) {
			to = len(data)
		}
		status, etag := e.store.put(u, data[from:to])
		if status != 200 {
			t.Fatalf("PUT of part %d answered %d", i+1, status)
		}
		parts = append(parts, map[string]any{"partNumber": i + 1, "etag": etag})
	}
	return parts
}

func (c *client) complete(in initResp, parts []map[string]any) *resp {
	c.e.t.Helper()
	return c.post("/api/v1/upload/"+in.UploadID+"/complete", map[string]any{"parts": parts})
}

func (c *client) uploadStatus(id string) *resp { return c.get("/api/v1/upload/" + id + "/status") }

type dbState struct {
	Session, Video string
	VideoDeleted   bool
	Retries        int
	Cleaned        bool
}

func (e *authEnv) db(t *testing.T, uploadID string) dbState {
	t.Helper()
	var s dbState
	var cleaned *time.Time
	if err := e.pool.QueryRow(context.Background(), `SELECT s.status::text, v.status::text, v.deleted_at IS NOT NULL, s.retry_count, s.storage_cleaned_at
FROM upload_sessions s JOIN videos v ON v.id = s.video_id WHERE s.id = $1::uuid`, uploadID).Scan(&s.Session, &s.Video, &s.VideoDeleted, &s.Retries, &cleaned); err != nil {
		t.Fatal(err)
	}
	s.Cleaned = cleaned != nil
	return s
}

func (e *authEnv) expire(t *testing.T, uploadID string) {
	t.Helper()
	e.exec(`UPDATE upload_sessions SET created_at = now() - interval '2 hours', expires_at = now() - interval '1 hour' WHERE id = $1::uuid`, uploadID)
	if n, err := e.uploads.ExpireStale(context.Background()); err != nil || n != 1 {
		t.Fatalf("janitor expired %d sessions, err=%v", n, err)
	}
}

func TestUploadHTTPAccessControl(t *testing.T) {
	e, v := uploadEnv(t)
	body := map[string]any{"filename": "clip.mp4", "size": 10, "contentType": "video/mp4"}

	anon := e.client()
	if r := anon.post("/api/v1/upload/init", body); r.code != 401 {
		t.Errorf("anonymous: %d %s", r.code, r.body)
	}
	unverified, _, _ := e.newUser()
	unverified.post("/api/v1/upload/init", body).wantErr(t, 403, "verification_required")

	evil := e.client()
	evil.jar = v.jar
	evil.origin = "https://evil.test"
	if r := evil.post("/api/v1/upload/init", body); r.code != 403 {
		t.Errorf("a cross-site request with the user's cookie must be refused: %d %s", r.code, r.body)
	}
	if e.countRows(`SELECT count(*) FROM upload_sessions`) != 0 {
		t.Fatal("a refused request created an upload")
	}

	// another user's upload does not exist for you, on every endpoint
	in := v.mustInit(11 * mib)
	other, _, _ := e.verifiedUser()
	for name, r := range map[string]*resp{
		"status":   other.uploadStatus(in.UploadID),
		"complete": other.complete(in, nil),
		"retry":    other.post("/api/v1/upload/"+in.UploadID+"/retry", nil),
		"abort":    other.del("/api/v1/upload/" + in.UploadID),
	} {
		if r.code != 404 || r.str("code") != "upload_not_found" {
			t.Errorf("%s by a stranger: %d %s", name, r.code, r.body)
		}
	}
	if s := e.db(t, in.UploadID); s.Session != "INITIATED" || s.Video != "UPLOADING" {
		t.Errorf("a stranger changed the upload: %+v", s)
	}
	for _, id := range []string{"not-a-uuid", "00000000-0000-0000-0000-000000000000"} {
		if r := v.uploadStatus(id); r.code != 404 {
			t.Errorf("status of %q: %d", id, r.code)
		}
	}
}

func TestUploadHTTPInitValidation(t *testing.T) {
	_, v := uploadEnv(t)
	for name, c := range map[string]struct {
		file  string
		size  int
		field string
	}{
		"unknown extension":   {"clip.exe", 10, "filename"},
		"avi is not accepted": {"clip.avi", 10, "filename"},
		"no name":             {"", 10, "filename"},
		"empty file":          {"clip.mp4", 0, "size"},
		"over 5 GiB":          {"clip.mp4", 5<<30 + 1, "size"},
	} {
		r := v.initUpload(c.file, c.size)
		fields, _ := r.m["fields"].(map[string]any)
		if r.code != 422 || r.str("code") != "validation_failed" || fields[c.field] == nil {
			t.Errorf("%s: %d %s", name, r.code, r.body)
		}
	}
	// the five accepted formats (this also fills the per-user cap of open uploads exactly)
	for _, f := range []string{"a.mp4", "b.m4v", "c.MOV", "d.mkv", "e.webm"} {
		if r := v.initUpload(f, 10); r.code != 201 {
			t.Errorf("%s: %d %s", f, r.code, r.body)
		}
	}
	v.initUpload("f.mp4", 10).wantErr(t, 422, "limit_reached")
}

func TestUploadHTTPHappyPath(t *testing.T) {
	e, v := uploadEnv(t)
	data := fakeVideo(11 * mib)
	in := v.mustInit(len(data))
	if len(in.URLs) != 3 || in.PartSize != 5*mib {
		t.Fatalf("11 MiB in 5 MiB parts must be 3 parts: %+v", in)
	}
	key := "uploads/" + in.VideoID + "/original.mp4"
	for _, u := range in.URLs {
		if want := "https://mem.test/" + key + "?uploadId="; len(u) < len(want) || u[:len(want)] != want {
			t.Fatalf("part URL %q does not point at the video's key", u)
		}
	}
	if r := v.uploadStatus(in.UploadID); r.code != 200 || r.str("status") != "processing" {
		t.Fatalf("status before upload: %d %s", r.code, r.body)
	}

	parts := e.send(t, in, data)
	v.complete(in, parts).wantNoContent(t)

	if s := e.db(t, in.UploadID); s != (dbState{Session: "COMPLETED", Video: "PROCESSING"}) {
		t.Fatalf("after complete: %+v", s)
	}
	if n := e.countRows(`SELECT count(*) FROM video_processing_jobs WHERE video_id = $1::uuid`, in.VideoID); n != 1 {
		t.Fatalf("the hand-off to the processing worker needs exactly one job row, found %d", n)
	}
	obj, ok := e.store.object(key)
	if !ok || len(obj.data) != len(data) || obj.contentType != "video/mp4" || !bytes.Equal(obj.data, data) {
		t.Fatalf("stored object wrong: exists=%v", ok)
	}
	if r := v.uploadStatus(in.UploadID); r.code != 200 || r.str("status") != "processing" {
		t.Fatalf("status after complete: %d %s", r.code, r.body)
	}
	// the browser retries a complete whose answer got lost: same result, nothing duplicated
	v.complete(in, parts).wantNoContent(t)
	if n := e.countRows(`SELECT count(*) FROM video_processing_jobs WHERE video_id = $1::uuid`, in.VideoID); n != 1 {
		t.Fatalf("a repeated complete created %d jobs", n)
	}
	// a finished upload cannot be reopened
	v.post("/api/v1/upload/"+in.UploadID+"/retry", nil).wantErr(t, 409, "not_retryable")
}

func (r *resp) wantNoContent(t *testing.T) {
	t.Helper()
	if r.code != 204 {
		t.Fatalf("got %d %s, want 204", r.code, r.body)
	}
}
