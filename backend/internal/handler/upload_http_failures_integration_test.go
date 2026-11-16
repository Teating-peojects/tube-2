package handler_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// Failure and recovery paths of the upload API through the real router (see upload_http_integration_test.go).

func TestUploadHTTPRejectedFiles(t *testing.T) {
	t.Run("not a video", func(t *testing.T) {
		e, v := uploadEnv(t)
		data := fakeHTML(11 * mib)
		in := v.mustInit(len(data))
		v.complete(in, e.send(t, in, data)).wantErr(t, 422, "invalid_file")

		if s := e.db(t, in.UploadID); s != (dbState{Session: "ABORTED", Video: "FAILED", VideoDeleted: true}) {
			t.Fatalf("a refused file closes the upload and discards the draft: %+v", s)
		}
		if e.store.hasObject("uploads/" + in.VideoID + "/original.mp4") {
			t.Fatal("the refused file must not stay in storage")
		}
		if r := v.uploadStatus(in.UploadID); r.code != 200 || r.str("status") != "failed" {
			t.Fatalf("status: %d %s", r.code, r.body)
		}
		if r := v.uploadStatus(in.UploadID); r.m["retryable"] != nil {
			t.Fatalf("a refused file is not retryable: %s", r.body)
		}
		v.post("/api/v1/upload/"+in.UploadID+"/retry", nil).wantErr(t, 409, "not_retryable")
		if r := v.complete(in, nil); r.code < 400 {
			t.Fatalf("a closed upload cannot be completed again: %d %s", r.code, r.body)
		}

		// whatever the immediate paths missed, the sweeper frees: the video's objects under all three prefixes
		res, err := e.uploads.SweepStorage(context.Background())
		if err != nil || res.Cleaned != 1 || res.Failed != 0 {
			t.Fatalf("sweep: %+v %v", res, err)
		}
		got := strings.Join(e.store.deletedPrefixes(), " ")
		for _, p := range []string{"uploads/", "thumbs/", "captions/"} {
			if !strings.Contains(got, p+in.VideoID+"/") {
				t.Errorf("prefix %s%s/ not deleted (deleted: %s)", p, in.VideoID, got)
			}
		}
		if !e.db(t, in.UploadID).Cleaned {
			t.Error("the session is not marked as cleaned")
		}
	})

	t.Run("a part that arrived short is caught and can be sent again", func(t *testing.T) {
		e, v := uploadEnv(t)
		data := fakeVideo(11 * mib)
		in := v.mustInit(len(data))
		key := "uploads/" + in.VideoID + "/original.mp4"
		v.complete(in, e.send(t, in, data[:len(data)-1])).wantErr(t, 422, "parts_mismatch") // last part one byte short
		if e.store.hasObject(key) {
			t.Fatal("a truncated file must not be assembled")
		}
		if s := e.db(t, in.UploadID); s.Session != "INITIATED" || s.Video != "UPLOADING" {
			t.Fatalf("a damaged part leaves the upload open: %+v", s)
		}
		v.complete(in, e.send(t, in, data)).wantNoContent(t) // the browser sends everything again
		if obj, ok := e.store.object(key); !ok || !bytes.Equal(obj.data, data) {
			t.Fatal("the corrected upload did not produce the object")
		}
	})
}

func TestUploadHTTPRecoverableCompleteFailures(t *testing.T) {
	t.Run("parts that do not match what the server received", func(t *testing.T) {
		e, v := uploadEnv(t)
		data := fakeVideo(11 * mib)
		in := v.mustInit(len(data))
		parts := e.send(t, in, data)

		v.complete(in, nil).wantErr(t, 422, "validation_failed")       // no parts at all
		v.complete(in, parts[:2]).wantErr(t, 422, "validation_failed") // a part missing from the list
		bad := append([]map[string]any(nil), parts...)
		bad[1] = map[string]any{"partNumber": 2, "etag": "00000000000000000000000000000000"}
		v.complete(in, bad).wantErr(t, 422, "parts_mismatch") // a list that does not match what storage received

		// none of that closed the upload: the right list still completes it
		if s := e.db(t, in.UploadID); s.Session != "INITIATED" || s.Video != "UPLOADING" {
			t.Fatalf("a wrong part list must leave the upload open: %+v", s)
		}
		v.complete(in, parts).wantNoContent(t)
		if s := e.db(t, in.UploadID); s.Session != "COMPLETED" || s.Video != "PROCESSING" {
			t.Fatalf("%+v", s)
		}
	})

	t.Run("object storage is down while verifying", func(t *testing.T) {
		e, v := uploadEnv(t)
		data := fakeVideo(11 * mib)
		in := v.mustInit(len(data))
		parts := e.send(t, in, data)

		e.store.set(func(m *memStore) { m.failListParts = true })
		r := v.complete(in, parts)
		if r.code < 500 || r.str("code") != "storage_unavailable" {
			t.Fatalf("a storage outage must be a retryable 5xx, got %d %s", r.code, r.body)
		}
		if s := e.db(t, in.UploadID); s.Session != "INITIATED" {
			t.Fatalf("the outage must not poison the upload: %+v", s)
		}
		e.store.set(func(m *memStore) { m.failListParts = false })
		v.complete(in, parts).wantNoContent(t) // the browser's retry succeeds
		if s := e.db(t, in.UploadID); s.Session != "COMPLETED" {
			t.Fatalf("%+v", s)
		}
	})
}

func TestUploadHTTPRetryOfAnExpiredUpload(t *testing.T) {
	t.Run("reopen, upload again, complete", func(t *testing.T) {
		e, v := uploadEnv(t)
		data := fakeVideo(11 * mib)
		in := v.mustInit(len(data))
		v.post("/api/v1/upload/"+in.UploadID+"/retry", nil).wantErr(t, 409, "upload_active")

		e.expire(t, in.UploadID)
		st := v.uploadStatus(in.UploadID)
		if st.code != 200 || st.str("status") != "failed" || st.m["retryable"] != true {
			t.Fatalf("status of an expired upload: %d %s", st.code, st.body)
		}
		if status, _ := e.store.put(in.URLs[0], data[:in.PartSize]); status != 404 {
			t.Fatalf("a part sent to the expired upload answered %d, the browser expects a 4xx", status)
		}

		r := v.post("/api/v1/upload/"+in.UploadID+"/retry", nil)
		if r.code != 200 {
			t.Fatalf("retry: %d %s", r.code, r.body)
		}
		again := readInit(t, r)
		if again.UploadID != in.UploadID || again.VideoID != in.VideoID || len(again.URLs) != len(in.URLs) || again.URLs[0] == in.URLs[0] {
			t.Fatalf("same ids and layout, new URLs expected: %+v vs %+v", in, again)
		}
		if s := e.db(t, in.UploadID); s != (dbState{Session: "INITIATED", Video: "UPLOADING", Retries: 1}) {
			t.Fatalf("after retry: %+v", s)
		}
		if st := v.uploadStatus(in.UploadID); st.m["retryable"] != nil {
			t.Fatalf("a reopened upload is not 'retryable': %s", st.body)
		}

		v.complete(again, e.send(t, again, data)).wantNoContent(t)
		if s := e.db(t, in.UploadID); s != (dbState{Session: "COMPLETED", Video: "PROCESSING", Retries: 1}) {
			t.Fatalf("after the second upload: %+v", s)
		}
		if obj, ok := e.store.object("uploads/" + in.VideoID + "/original.mp4"); !ok || !bytes.Equal(obj.data, data) {
			t.Fatal("the reopened upload did not produce the object")
		}
	})

	t.Run("the retry budget and the retry window", func(t *testing.T) {
		e, v := uploadEnv(t)
		in := v.mustInit(11 * mib)
		for i := 1; i <= 3; i++ {
			e.expire(t, in.UploadID)
			if r := v.post("/api/v1/upload/"+in.UploadID+"/retry", nil); r.code != 200 {
				t.Fatalf("retry %d: %d %s", i, r.code, r.body)
			}
		}
		e.expire(t, in.UploadID)
		v.post("/api/v1/upload/"+in.UploadID+"/retry", nil).wantErr(t, 409, "retry_limit")
		if st := v.uploadStatus(in.UploadID); st.str("status") != "failed" || st.m["retryable"] != nil {
			t.Fatalf("a spent budget must not be advertised: %s", st.body)
		}

		// an upload that expired long ago is past its window: final, and the janitor deletes its storage
		old := v.mustInit(11 * mib)
		e.expire(t, old.UploadID)
		e.exec(`UPDATE upload_sessions SET created_at = now() - interval '10 days', expires_at = now() - interval '8 days' WHERE id = $1::uuid`, old.UploadID)
		if st := v.uploadStatus(old.UploadID); st.m["retryable"] != nil {
			t.Fatalf("past the window: %s", st.body)
		}
		v.post("/api/v1/upload/"+old.UploadID+"/retry", nil).wantErr(t, 409, "not_retryable")
		res, err := e.uploads.SweepStorage(context.Background())
		if err != nil || res.Finalized != 1 {
			t.Fatalf("sweep: %+v %v", res, err)
		}
		if s := e.db(t, old.UploadID); s.Session != "ABORTED" || !s.Cleaned {
			t.Fatalf("a finalized upload is closed and cleaned: %+v", s)
		}
	})
}

func TestUploadHTTPCancel(t *testing.T) {
	e, v := uploadEnv(t)
	data := fakeVideo(11 * mib)

	t.Run("an open upload", func(t *testing.T) {
		in := v.mustInit(len(data))
		urls := in.URLs
		before := e.store.openUploads()
		v.del("/api/v1/upload/" + in.UploadID).wantNoContent(t)
		if e.store.openUploads() != before-1 {
			t.Fatal("cancelling must abort the multipart upload in storage")
		}
		if s := e.db(t, in.UploadID); s != (dbState{Session: "ABORTED", Video: "FAILED", VideoDeleted: true}) {
			t.Fatalf("%+v", s)
		}
		if status, _ := e.store.put(urls[0], data[:in.PartSize]); status != 404 {
			t.Fatalf("a part sent after the cancel answered %d, want 404", status)
		}
		if r := v.uploadStatus(in.UploadID); r.str("status") != "failed" {
			t.Fatalf("%s", r.body)
		}
		v.del("/api/v1/upload/" + in.UploadID).wantNoContent(t) // cancelling twice is harmless
		v.post("/api/v1/upload/"+in.UploadID+"/retry", nil).wantErr(t, 409, "not_retryable")
	})

	t.Run("an upload that was already completed but never published", func(t *testing.T) {
		in := v.mustInit(len(data))
		v.complete(in, e.send(t, in, data)).wantNoContent(t)
		key := "uploads/" + in.VideoID + "/original.mp4"
		if !e.store.hasObject(key) {
			t.Fatal("setup: the object should exist")
		}
		if n := e.countRows(`SELECT count(*) FROM video_processing_jobs WHERE video_id = $1::uuid AND status = 'QUEUED'`, in.VideoID); n != 1 {
			t.Fatalf("setup: expected one queued job, found %d", n)
		}
		v.del("/api/v1/upload/" + in.UploadID).wantNoContent(t)
		if e.store.hasObject(key) {
			t.Fatal("cancelling a completed, unpublished upload deletes its file")
		}
		if s := e.db(t, in.UploadID); s.Session != "ABORTED" || !s.VideoDeleted {
			t.Fatalf("%+v", s)
		}
		if n := e.countRows(`SELECT count(*) FROM upload_sessions WHERE id = $1::uuid AND completed_at IS NULL`, in.UploadID); n != 1 {
			t.Fatal("only a COMPLETED session may carry completed_at")
		}
		if n := e.countRows(`SELECT count(*) FROM video_processing_jobs WHERE video_id = $1::uuid AND status = 'QUEUED'`, in.VideoID); n != 0 {
			t.Fatalf("a cancelled upload must not stay queued for the worker (%d queued)", n)
		}
		if n := e.countRows(`SELECT count(*) FROM video_processing_jobs WHERE video_id = $1::uuid AND status = 'FAILED' AND error_message = 'upload cancelled'`, in.VideoID); n != 1 {
			t.Fatalf("the job should be closed as cancelled, found %d", n)
		}
		v.del("/api/v1/upload/" + in.UploadID).wantNoContent(t) // and cancelling again stays harmless
	})
}

func TestUploadHTTPOpenUploadsFreeTheirSlotWhenClosed(t *testing.T) {
	e, v := uploadEnv(t)
	var ids []initResp
	for i := 0; i < 5; i++ {
		ids = append(ids, v.mustInit(11*mib))
	}
	v.initUpload("clip.mp4", 11*mib).wantErr(t, 422, "limit_reached")
	v.del("/api/v1/upload/" + ids[0].UploadID).wantNoContent(t)
	if r := v.initUpload("clip.mp4", 11*mib); r.code != 201 {
		t.Fatalf("a cancelled upload must free its slot: %d %s", r.code, r.body)
	}
	_ = e
}

func TestReadyReportsObjectStorageWithoutFailingReadiness(t *testing.T) {
	e, _ := uploadEnv(t)
	c := e.client()
	r := c.get("/ready")
	checks, _ := r.m["checks"].(map[string]any)
	if r.code != 200 || checks["storage"] != "ok" || r.m["degraded"] != nil {
		t.Fatalf("healthy: %d %s", r.code, r.body)
	}
	e.store.set(func(m *memStore) { m.pingErr = context.DeadlineExceeded })
	r = c.get("/ready")
	checks, _ = r.m["checks"].(map[string]any)
	deg, _ := r.m["degraded"].([]any)
	if r.code != 200 || r.str("status") != "ok" || checks["storage"] != "error" || len(deg) != 1 || deg[0] != "storage" {
		t.Fatalf("storage down must degrade, not fail: %d %s", r.code, r.body)
	}
	if strings.Contains(string(r.body), "deadline") {
		t.Fatalf("error details leaked: %s", r.body)
	}
}
