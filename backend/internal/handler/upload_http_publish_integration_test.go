package handler_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// The part of the upload API after the file is in: thumbnails and captions, draft metadata, and publishing, which
// must never happen by itself (the owner declares consent and the video must be READY).

var pngHeader = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0x0D, 'I', 'H', 'D', 'R'}

func (c *client) asset(uploadID string, body map[string]any) *resp {
	c.e.t.Helper()
	return c.post("/api/v1/upload/"+uploadID+"/assets", body)
}

func (c *client) patchMeta(videoID string, body map[string]any) *resp {
	c.e.t.Helper()
	return c.patch("/api/v1/videos/"+videoID+"/metadata", body)
}

func (c *client) publish(videoID string, body map[string]any) *resp {
	c.e.t.Helper()
	return c.post("/api/v1/videos/"+videoID+"/publish", body)
}

// finished plays the whole upload and returns it; the video then waits for the (future) worker in PROCESSING.
func (e *authEnv) finished(t *testing.T, v *client) initResp {
	t.Helper()
	data := fakeVideo(11 * mib)
	in := v.mustInit(len(data))
	v.complete(in, e.send(t, in, data)).wantNoContent(t)
	return in
}

// workerDone does what the processing worker will do: the video becomes READY with its HLS master and duration.
func (e *authEnv) workerDone(videoID string) {
	e.t.Helper()
	e.exec(`UPDATE videos SET status = 'READY', video_type = 'LONG', duration_seconds = 120, hls_master_key = 'hls/' || id::text || '/master.m3u8' WHERE id = $1::uuid`, videoID)
}

func TestUploadHTTPAssets(t *testing.T) {
	e, v := uploadEnv(t)
	in := v.mustInit(11 * mib)

	r := v.asset(in.UploadID, map[string]any{"kind": "thumbnail", "contentType": "image/png", "size": 1000})
	if r.code != 201 || r.str("assetId") == "" || !strings.HasPrefix(r.str("url"), "https://mem.test/thumbs/"+in.VideoID+"/") || !strings.HasSuffix(r.str("url"), ".png?put") {
		t.Fatalf("thumbnail: %d %s", r.code, r.body)
	}
	r = v.asset(in.UploadID, map[string]any{"kind": "caption", "contentType": "text/vtt", "size": 500, "lang": "bn"})
	if r.code != 201 || !strings.Contains(r.str("url"), "/captions/"+in.VideoID+"/bn.vtt") {
		t.Fatalf("caption: %d %s", r.code, r.body)
	}

	for name, c := range map[string]struct {
		body  map[string]any
		field string
	}{
		"gif is not an accepted image": {map[string]any{"kind": "thumbnail", "contentType": "image/gif", "size": 100}, "thumbnail"},
		"thumbnail over 2 MB":          {map[string]any{"kind": "thumbnail", "contentType": "image/png", "size": 2<<20 + 1}, "thumbnail"},
		"empty thumbnail":              {map[string]any{"kind": "thumbnail", "contentType": "image/png", "size": 0}, "thumbnail"},
		"caption of the wrong type":    {map[string]any{"kind": "caption", "contentType": "text/html", "size": 100, "lang": "en"}, "caption"},
		"caption with a bad language":  {map[string]any{"kind": "caption", "contentType": "text/vtt", "size": 100, "lang": "!!"}, "lang"},
		"unknown kind":                 {map[string]any{"kind": "video", "contentType": "video/mp4", "size": 100}, "kind"},
	} {
		r := v.asset(in.UploadID, c.body)
		fields, _ := r.m["fields"].(map[string]any)
		if r.code != 422 || r.str("code") != "validation_failed" || fields[c.field] == nil {
			t.Errorf("%s: %d %s", name, r.code, r.body)
		}
	}

	other, _, _ := e.verifiedUser()
	if r := other.asset(in.UploadID, map[string]any{"kind": "thumbnail", "contentType": "image/png", "size": 10}); r.code != 404 {
		t.Errorf("a stranger may not add files to someone else's upload: %d %s", r.code, r.body)
	}

	// at most 10 custom thumbnails (one is already there)
	for i := 0; i < 9; i++ {
		if r := v.asset(in.UploadID, map[string]any{"kind": "thumbnail", "contentType": "image/jpeg", "size": 10}); r.code != 201 {
			t.Fatalf("thumbnail %d: %d %s", i+2, r.code, r.body)
		}
	}
	v.asset(in.UploadID, map[string]any{"kind": "thumbnail", "contentType": "image/jpeg", "size": 10}).wantErr(t, 422, "limit_reached")

	// a closed upload takes no more files
	v.del("/api/v1/upload/" + in.UploadID).wantNoContent(t)
	v.asset(in.UploadID, map[string]any{"kind": "thumbnail", "contentType": "image/png", "size": 10}).wantErr(t, 409, "upload_closed")
}

func TestUploadHTTPDraftMetadata(t *testing.T) {
	e, v := uploadEnv(t)
	in := e.finished(t, v)

	r := v.patchMeta(in.VideoID, map[string]any{"title": "  My first clip ", "description": "hello", "tags": []string{"Music", "live", "music"},
		"visibility": "unlisted", "comments": "review", "language": "bn", "ageGate": true, "category": "music"})
	r.wantNoContent(t)
	var title, vis, comments string
	var tags, age int
	if err := e.pool.QueryRow(context.Background(), `SELECT title, visibility::text, comments_mode::text, age_restricted::int, (SELECT count(*) FROM video_tags WHERE video_id = v.id) FROM videos v WHERE id = $1::uuid`,
		in.VideoID).Scan(&title, &vis, &comments, &age, &tags); err != nil {
		t.Fatal(err)
	}
	if title != "My first clip" || vis != "UNLISTED" || comments != "REVIEW" || age != 1 || tags != 2 {
		t.Fatalf("stored: title=%q visibility=%s comments=%s age=%d tags=%d (the duplicate tag must collapse)", title, vis, comments, age, tags)
	}

	for name, c := range map[string]struct {
		body  map[string]any
		field string
	}{
		"empty title":        {map[string]any{"title": "  "}, "title"},
		"title over 100":     {map[string]any{"title": strings.Repeat("x", 101)}, "title"},
		"unknown visibility": {map[string]any{"visibility": "secret"}, "visibility"},
		"unknown comments":   {map[string]any{"comments": "maybe"}, "comments"},
		"unknown category":   {map[string]any{"category": "no-such-category"}, "category"},
		"too many tags":      {map[string]any{"tags": strings.Split(strings.Repeat("t,", 16)+"u", ",")}, "tags"},
		"tag with a comma":   {map[string]any{"tags": []string{"a,b"}}, "tags"},
		"bad language":       {map[string]any{"language": "english!"}, "language"},
	} {
		r := v.patchMeta(in.VideoID, c.body)
		fields, _ := r.m["fields"].(map[string]any)
		if r.code != 422 || fields[c.field] == nil {
			t.Errorf("%s: %d %s", name, r.code, r.body)
		}
	}

	other, _, _ := e.verifiedUser()
	other.patchMeta(in.VideoID, map[string]any{"title": "stolen"}).wantErr(t, 404, "video_not_found")
	v.patchMeta("not-a-uuid", map[string]any{"title": "x"}).wantErr(t, 404, "video_not_found")
	var after string
	_ = e.pool.QueryRow(context.Background(), `SELECT title FROM videos WHERE id = $1::uuid`, in.VideoID).Scan(&after)
	if after != "My first clip" {
		t.Fatalf("a refused or foreign edit changed the title to %q", after)
	}
}

func TestUploadHTTPPublishNeverHappensByItself(t *testing.T) {
	e, v := uploadEnv(t)
	in := e.finished(t, v)
	state := func() (status string, published bool) {
		if err := e.pool.QueryRow(context.Background(), `SELECT status::text, published_at IS NOT NULL FROM videos WHERE id = $1::uuid`, in.VideoID).Scan(&status, &published); err != nil {
			t.Fatal(err)
		}
		return
	}
	if s, p := state(); s != "PROCESSING" || p {
		t.Fatalf("a completed upload must wait, got %s published=%v", s, p)
	}

	// no consent, or consent withheld
	for name, body := range map[string]map[string]any{"no consent field": {"category": "music"}, "consent false": {"category": "music", "consent": false}} {
		r := v.publish(in.VideoID, body)
		fields, _ := r.m["fields"].(map[string]any)
		if r.code != 422 || fields["consent"] == nil {
			t.Errorf("%s: %d %s", name, r.code, r.body)
		}
	}
	// consent given, but the worker has not finished
	v.publish(in.VideoID, map[string]any{"category": "music", "consent": true}).wantErr(t, 409, "not_ready")
	if s, p := state(); s != "PROCESSING" || p {
		t.Fatalf("a refused publish changed the video: %s %v", s, p)
	}
	if n := e.countRows(`SELECT count(*) FROM upload_consents WHERE video_id = $1::uuid`, in.VideoID); n != 0 {
		t.Fatalf("a refused publish recorded a consent (%d)", n)
	}

	e.workerDone(in.VideoID)
	// READY still does not publish; and a category is required
	if s, p := state(); s != "READY" || p {
		t.Fatalf("%s %v", s, p)
	}
	r := v.publish(in.VideoID, map[string]any{"consent": true})
	if fields, _ := r.m["fields"].(map[string]any); r.code != 422 || fields["category"] == nil {
		t.Fatalf("publishing without a category: %d %s", r.code, r.body)
	}
	other, _, _ := e.verifiedUser()
	other.publish(in.VideoID, map[string]any{"category": "music", "consent": true}).wantErr(t, 404, "video_not_found")

	v.publish(in.VideoID, map[string]any{"title": "Published clip", "category": "music", "visibility": "public", "consent": true}).wantNoContent(t)
	var title, vis, cat string
	var consents, audits, hash int
	if err := e.pool.QueryRow(context.Background(), `SELECT v.title, v.visibility::text, c.slug, (SELECT count(*) FROM upload_consents WHERE video_id = v.id),
  (SELECT count(*) FROM upload_consents WHERE video_id = v.id AND statement_version = 1 AND octet_length(ip_hash) = 32),
  (SELECT count(*) FROM audit_logs WHERE action = 'video.publish' AND target_id::text = v.id::text)
FROM videos v JOIN categories c ON c.id = v.category_id WHERE v.id = $1::uuid`, in.VideoID).Scan(&title, &vis, &cat, &consents, &hash, &audits); err != nil {
		t.Fatal(err)
	}
	if s, p := state(); s != "PUBLISHED" || !p || title != "Published clip" || vis != "PUBLIC" || cat != "music" || consents != 1 || hash != 1 || audits != 1 {
		t.Fatalf("after publish: status=%s published=%v title=%q vis=%s cat=%s consents=%d hashedIP=%d audit=%d", s, p, title, vis, cat, consents, hash, audits)
	}
	v.publish(in.VideoID, map[string]any{"consent": true}).wantErr(t, 409, "already_published")

	// a published video's upload can no longer be cancelled or given files
	v.del("/api/v1/upload/"+in.UploadID).wantErr(t, 409, "already_published")
	if s, _ := state(); s != "PUBLISHED" {
		t.Fatalf("the cancel touched a published video: %s", s)
	}
	v.asset(in.UploadID, map[string]any{"kind": "thumbnail", "contentType": "image/png", "size": 10}).wantErr(t, 409, "upload_closed")
}

func TestUploadHTTPPublishChecksTheUploadedAssets(t *testing.T) {
	t.Run("valid thumbnail and caption", func(t *testing.T) {
		e, v := uploadEnv(t)
		in := e.finished(t, v)
		e.workerDone(in.VideoID)
		th := v.asset(in.UploadID, map[string]any{"kind": "thumbnail", "contentType": "image/png", "size": len(pngHeader)})
		cap := v.asset(in.UploadID, map[string]any{"kind": "caption", "contentType": "text/vtt", "size": 50, "lang": "bn"})
		// the browser PUTs to the presigned URLs
		_ = e.store.PutBytes(context.Background(), keyOf(th.str("url")), "image/png", "", pngHeader)
		_ = e.store.PutBytes(context.Background(), keyOf(cap.str("url")), "text/vtt", "", []byte("WEBVTT\n\n00:00.000 --> 00:01.000\nhello\n"))

		v.publish(in.VideoID, map[string]any{"category": "music", "consent": true, "thumbnail": th.str("assetId"),
			"captions": []map[string]any{{"lang": "bn", "assetId": cap.str("assetId")}}}).wantNoContent(t)
		if n := e.countRows(`SELECT count(*) FROM video_thumbnails WHERE video_id = $1::uuid AND is_primary AND status = 'READY' AND id = $2::uuid`, in.VideoID, th.str("assetId")); n != 1 {
			t.Fatalf("the chosen thumbnail is not the primary one (%d)", n)
		}
		if n := e.countRows(`SELECT count(*) FROM video_captions WHERE video_id = $1::uuid AND language = 'bn' AND status = 'READY'`, in.VideoID); n != 1 {
			t.Fatalf("the caption is not ready (%d)", n)
		}
		if !e.store.hasObject("captions/" + in.VideoID + "/bn.vtt") {
			t.Fatal("the normalized caption file is missing")
		}
	})

	t.Run("a thumbnail that is not an image is refused and deleted", func(t *testing.T) {
		e, v := uploadEnv(t)
		in := e.finished(t, v)
		e.workerDone(in.VideoID)
		th := v.asset(in.UploadID, map[string]any{"kind": "thumbnail", "contentType": "image/png", "size": 100})
		key := keyOf(th.str("url"))
		_ = e.store.PutBytes(context.Background(), key, "image/png", "", bytes.Repeat([]byte("<html>"), 10))
		r := v.publish(in.VideoID, map[string]any{"category": "music", "consent": true, "thumbnail": th.str("assetId")})
		if fields, _ := r.m["fields"].(map[string]any); r.code != 422 || fields["thumbnail"] == nil {
			t.Fatalf("%d %s", r.code, r.body)
		}
		if e.store.hasObject(key) {
			t.Fatal("the refused file must be deleted")
		}
		if n := e.countRows(`SELECT count(*) FROM videos WHERE id = $1::uuid AND status = 'READY'`, in.VideoID); n != 1 {
			t.Fatal("a refused publish must leave the video READY")
		}
	})

	t.Run("an asset that was never uploaded is refused", func(t *testing.T) {
		e, v := uploadEnv(t)
		in := e.finished(t, v)
		e.workerDone(in.VideoID)
		cap := v.asset(in.UploadID, map[string]any{"kind": "caption", "contentType": "text/vtt", "size": 50, "lang": "bn"})
		r := v.publish(in.VideoID, map[string]any{"category": "music", "consent": true, "captions": []map[string]any{{"lang": "bn", "assetId": cap.str("assetId")}}})
		if fields, _ := r.m["fields"].(map[string]any); r.code != 422 || fields["captions"] == nil {
			t.Fatalf("%d %s", r.code, r.body)
		}
	})
}

// keyOf extracts the object key from the stub's presigned PUT URL (https://mem.test/<key>?put).
func keyOf(u string) string {
	u = strings.TrimPrefix(u, "https://mem.test/")
	if i := strings.Index(u, "?"); i >= 0 {
		u = u[:i]
	}
	return u
}
