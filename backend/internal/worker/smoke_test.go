package worker

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redxtube/api/internal/storage"
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
