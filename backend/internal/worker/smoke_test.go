package worker

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redxtube/api/internal/playback"
	"github.com/redxtube/api/internal/repository"
	"github.com/redxtube/api/internal/storage"
	"github.com/redxtube/api/internal/stream"
)

// Whole worker, no fakes: the real Run loop, PostgreSQL (as redxtube_worker) and an S3-compatible
// server. Needs TEST_S3_ENDPOINT (e.g. a local moto/MinIO), TEST_S3_BUCKET and the TEST_*_DATABASE_URLs.
func TestWorkerSmoke(t *testing.T) {
	needFFmpeg(t)
	endpoint, bucket := os.Getenv("TEST_S3_ENDPOINT"), os.Getenv("TEST_S3_BUCKET")
	if endpoint == "" || bucket == "" {
		t.Skip("TEST_S3_ENDPOINT / TEST_S3_BUCKET not set")
	}
	e := newPGEnv(t)
	st, err := storage.New(storage.Config{Endpoint: endpoint, Region: "us-east-1", AccessKey: "test", SecretKey: "test", Bucket: bucket, PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}

	mk := func(content []byte) (vid string) {
		vid = uuid.NewString()
		key := "uploads/" + vid + "/original.mp4"
		if err := st.PutBytes(ctxb, key, "video/mp4", "", content); err != nil {
			t.Fatal(err)
		}
		e.exec(`INSERT INTO videos (id, creator_id, title, original_filename, original_storage_key, file_size_bytes, status)
VALUES ($1::uuid, $2::uuid, 'Smoke', 'smoke.mp4', $3, $4, 'PROCESSING')`, vid, e.user, key, len(content))
		e.exec(`INSERT INTO video_processing_jobs (video_id, job_type) VALUES ($1::uuid, 'METADATA')`, vid)
		return vid
	}
	good := mk(makeVideo(t, "-f", "lavfi", "-i", "testsrc=size=854x480:rate=25", "-f", "lavfi", "-i", "sine=frequency=330",
		"-t", "8", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest"))
	bad := mk([]byte("definitely not a video"))

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	pg := NewPG(e.worker, "smoke")
	pipe := &Pipeline{Store: pg, Storage: st, Log: log, WorkDir: t.TempDir(), FFmpeg: "ffmpeg", FFprobe: "ffprobe", Threads: 2}
	w := New(Config{Concurrency: 2, PollInterval: 100 * time.Millisecond, WorkDir: pipe.WorkDir, HeartbeatEvery: time.Second}, pg, pipe, log)
	ctx, cancel := context.WithCancel(ctxb)
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if e.str(`SELECT status::text FROM videos WHERE id = $1::uuid`, good) == "READY" &&
			e.str(`SELECT status::text FROM videos WHERE id = $1::uuid`, bad) == "FAILED" {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if got := e.str(`SELECT status::text FROM videos WHERE id = $1::uuid`, good); got != "READY" {
		t.Fatalf("good video is %s", got)
	}
	if got := e.str(`SELECT status::text FROM videos WHERE id = $1::uuid`, bad); got != "FAILED" {
		t.Fatalf("junk video is %s", got)
	}

	// good: 4 jobs done, 5 thumbnails (1 primary), 3 variants (854x480 source => 360p, 480p), objects in S3
	if n := e.num(`SELECT count(*) FROM video_processing_jobs WHERE video_id = $1::uuid AND status = 'COMPLETED'`, good); n != 4 {
		t.Errorf("%d completed jobs", n)
	}
	if e.num(`SELECT count(*) FROM video_thumbnails WHERE video_id = $1::uuid AND is_primary`, good) != 1 ||
		e.num(`SELECT count(*) FROM video_thumbnails WHERE video_id = $1::uuid`, good) != 5 {
		t.Error("thumbnails")
	}
	if n := e.num(`SELECT count(*) FROM video_variants WHERE video_id = $1::uuid AND status = 'READY'`, good); n != 2 {
		t.Errorf("%d variants", n)
	}
	for _, k := range []string{"hls/" + good + "/master.m3u8", "hls/" + good + "/480p/index.m3u8", "hls/" + good + "/480p/seg_00000.ts", "hls/" + good + "/360p/index.m3u8"} {
		if _, err := st.Head(ctxb, k); err != nil {
			t.Errorf("missing object %s: %v", k, err)
		}
	}
	if e.num(`SELECT count(*) FROM notifications WHERE video_id = $1::uuid AND type = 'VIDEO_READY'`, good) != 1 {
		t.Error("VIDEO_READY")
	}
	// bad: failed on the first attempt (permanent), creator told, nothing published
	if e.num(`SELECT attempts FROM video_processing_jobs WHERE video_id = $1::uuid AND job_type = 'METADATA'`, bad) != 1 ||
		e.num(`SELECT count(*) FROM notifications WHERE video_id = $1::uuid AND type = 'VIDEO_FAILED'`, bad) != 1 {
		t.Error("junk upload must fail once, permanently, and notify")
	}
	if ks, _ := st.DeletePrefix(ctxb, "hls/"+bad+"/"); ks != 0 {
		t.Error("junk upload produced HLS output")
	}
}

// The whole B6 -> B7 chain: the worker encodes an upload, the catalog would hand out a signed URL,
// and a real ffmpeg "player" plays it through the stream handler. Then moderation removes the video
// and playback stops.
func TestPlaybackEndToEnd(t *testing.T) {
	needFFmpeg(t)
	endpoint, bucket := os.Getenv("TEST_S3_ENDPOINT"), os.Getenv("TEST_S3_BUCKET")
	if endpoint == "" || bucket == "" {
		t.Skip("TEST_S3_ENDPOINT / TEST_S3_BUCKET not set")
	}
	e := newPGEnv(t)
	st, err := storage.New(storage.Config{Endpoint: endpoint, Region: "us-east-1", AccessKey: "test", SecretKey: "test", Bucket: bucket, PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	vid := uuid.NewString()
	src := makeVideo(t, "-f", "lavfi", "-i", "testsrc=size=640x360:rate=25", "-f", "lavfi", "-i", "sine=frequency=550",
		"-t", "9", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest")
	key := "uploads/" + vid + "/original.mp4"
	if err := st.PutBytes(ctxb, key, "video/mp4", "", src); err != nil {
		t.Fatal(err)
	}
	e.exec(`INSERT INTO videos (id, creator_id, title, original_filename, original_storage_key, file_size_bytes, status)
VALUES ($1::uuid, $2::uuid, 'E2E', 'e2e.mp4', $3, $4, 'PROCESSING')`, vid, e.user, key, len(src))
	e.exec(`INSERT INTO video_processing_jobs (video_id, job_type) VALUES ($1::uuid, 'METADATA')`, vid)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	pg := NewPG(e.worker, "e2e")
	pipe := &Pipeline{Store: pg, Storage: st, Log: log, WorkDir: t.TempDir(), FFmpeg: "ffmpeg", FFprobe: "ffprobe", Threads: 2}
	w := New(Config{Concurrency: 1, PollInterval: 100 * time.Millisecond, WorkDir: pipe.WorkDir, HeartbeatEvery: time.Second}, pg, pipe, log)
	wctx, stop := context.WithCancel(ctxb)
	done := make(chan struct{})
	go func() { w.Run(wctx); close(done) }()
	defer func() { stop(); <-done }()
	for deadline := time.Now().Add(2 * time.Minute); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
		if e.str(`SELECT status::text FROM videos WHERE id = $1::uuid`, vid) == "READY" {
			break
		}
	}
	if e.str(`SELECT status::text FROM videos WHERE id = $1::uuid`, vid) != "READY" {
		t.Fatal("video never became READY")
	}
	master := e.str(`SELECT hls_master_key FROM videos WHERE id = $1::uuid`, vid)

	// stream server with the real repository gate
	signKey := bytes.Repeat([]byte("s"), 32)
	catalog := repository.NewCatalog(e.admin)
	mux := http.NewServeMux()
	mux.Handle("GET /stream/{token}/{key...}", stream.New(signKey, st, catalog.VideoServable, log).WithGateTTL(100*time.Millisecond))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	sg, err := playback.NewSigner(signKey, srv.URL+"/stream", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	streamURL := sg.URL(vid, master)
	if streamURL == "" {
		t.Fatal("no signed URL")
	}

	// 1. a real player decodes the whole thing through the token URL
	play := func(u string) ([]byte, error) {
		return exec.Command("ffmpeg", "-v", "error", "-protocol_whitelist", "http,tcp,file,crypto",
			"-i", u, "-f", "null", "-").CombinedOutput()
	}
	if out, err := play(streamURL); err != nil {
		t.Fatalf("playback failed: %v\n%s", err, out)
	}
	if out, err := exec.Command("ffprobe", "-v", "error", "-protocol_whitelist", "http,tcp,file,crypto",
		"-show_entries", "stream=codec_type", "-of", "csv=p=0", streamURL).Output(); err != nil || !strings.Contains(string(out), "audio") || !strings.Contains(string(out), "video") {
		t.Fatalf("streams over HTTP: %v %q", err, out)
	}

	// 2. the same token cannot reach the original upload or anything outside hls/
	tok := strings.Split(strings.TrimPrefix(strings.TrimPrefix(streamURL, srv.URL), "/stream/"), "/")[0]
	for _, p := range []string{"/stream/" + tok + "/uploads/" + vid + "/original.mp4", "/stream/" + tok + "/hls/" + uuid.NewString() + "/master.m3u8"} {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Errorf("%s -> %d, want 403", p, resp.StatusCode)
		}
	}

	// 3. moderation takedown stops playback (gate TTL is 100 ms here)
	e.exec(`UPDATE videos SET moderation_status = 'REMOVED' WHERE id = $1::uuid`, vid)
	time.Sleep(300 * time.Millisecond)
	resp, err := http.Get(streamURL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("removed video still streams: %d", resp.StatusCode)
	}
	if out, err := play(streamURL); err == nil {
		t.Fatalf("player still decodes a removed video: %s", out)
	}
}
