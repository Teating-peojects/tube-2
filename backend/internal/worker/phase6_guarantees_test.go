package worker

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redxtube/api/internal/repository"
)

// The guarantees Phase 6 asks for that are about behaviour, not about a single function.

// A phone video is stored sideways with a rotation flag. The player must get it upright.
func TestRotatedPhoneVideoIsEncodedUpright(t *testing.T) {
	needFFmpeg(t)
	const vid = "0190aaaa-bbbb-7ccc-8ddd-777777777777"
	tmp := t.TempDir()
	landscape := filepath.Join(tmp, "land.mp4")
	rotated := filepath.Join(tmp, "rot.mp4")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=size=640x360:rate=10", "-t", "3",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", landscape).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	// the camera wrote 640x360 pixels and a flag: "display this turned by 90 degrees" => 360x640 portrait
	if out, err := exec.Command("ffmpeg", "-v", "error", "-y", "-display_rotation", "90", "-i", landscape, "-c", "copy", rotated).CombinedOutput(); err != nil {
		t.Skipf("this ffmpeg cannot write a display rotation: %v %s", err, out)
	}
	src, _ := os.ReadFile(rotated)

	mem := newMem()
	key := "uploads/" + vid + "/original.mp4"
	mem.objs[key] = src
	st := &fakeStore{video: VideoRow{CreatorID: "c", Status: "PROCESSING", Moderation: "OK", OriginalKey: key, Size: int64(len(src))}}
	p := newPipe(t, st, mem)
	for _, typ := range []string{"METADATA", "THUMBNAIL", "TRANSCODE", "HLS"} {
		if err := p.Run(context.Background(), Job{ID: "j", VideoID: vid, Type: typ}); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
	}
	if st.meta.Width != 360 || st.meta.Height != 640 {
		t.Fatalf("stored size %dx%d, want the displayed 360x640", st.meta.Width, st.meta.Height)
	}
	if master := string(mem.objs["hls/"+vid+"/master.m3u8"]); !strings.Contains(master, "RESOLUTION=360x640") {
		t.Fatalf("master is not portrait:\n%s", master)
	}
	// the encoded frames themselves are upright, with no rotation flag left to apply twice
	dir := t.TempDir()
	for _, k := range mem.keys("hls/" + vid + "/360p/") {
		_ = os.WriteFile(filepath.Join(dir, filepath.Base(k)), mem.objs[k], 0o600)
	}
	out, err := exec.Command("ffprobe", "-v", "error", "-protocol_whitelist", "file", "-show_entries", "stream=width,height:stream_side_data=rotation",
		"-of", "json", filepath.Join(dir, "index.m3u8")).Output()
	if err != nil {
		t.Fatal(err)
	}
	flat := strings.Join(strings.Fields(string(out)), "")
	if !strings.Contains(flat, `"width":360`) || !strings.Contains(flat, `"height":640`) || strings.Contains(flat, "rotation") {
		t.Fatalf("encoded stream is not an upright 360x640: %s", flat)
	}
	// thumbnails are upright too
	for _, th := range st.thumbs {
		if th.W != 360 || th.H != 640 {
			t.Errorf("thumbnail %dx%d, want 360x640", th.W, th.H)
		}
	}
}

func appPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_WORKER_APP_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_WORKER_APP_DATABASE_URL not set")
	}
	app, err := pgxpool.New(ctxb, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	return app
}

// Phase 5 hands over to Phase 6 here: completing an upload must queue exactly one job, even when the
// completion is delivered twice (a retried request, a double click).
func TestCompletingAnUploadTwiceQueuesOneJob(t *testing.T) {
	e := newPGEnv(t)
	up := repository.NewUpload(appPool(t))
	vid, sid := uuid.NewString(), uuid.NewString()
	e.exec(`INSERT INTO videos (id, creator_id, title, original_filename, original_storage_key, file_size_bytes, status)
VALUES ($1::uuid, $2::uuid, 'Handover', 'h.mp4', $3, 1000, 'UPLOADING')`, vid, e.user, "uploads/"+vid+"/original.mp4")
	e.exec(`INSERT INTO upload_sessions (id, user_id, video_id, storage_key, provider_upload_id, declared_size_bytes, declared_content_type,
  part_size_bytes, part_count, expires_at, status) VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'p', 1000, 'video/mp4', 5242880, 1, now() + interval '1 day', 'COMPLETING')`,
		sid, e.user, vid, "uploads/"+vid+"/original.mp4")

	sess := &repository.UploadSession{ID: sid, UserID: e.user, VideoID: vid}
	t1, t2 := "video", vid
	audit := repository.AuditEntry{ActorID: &e.user, Role: "USER", Action: "upload.completed", TargetType: &t1, TargetID: &t2}
	for i := 0; i < 2; i++ {
		if err := up.FinishComplete(ctxb, sess, audit); err != nil {
			t.Fatalf("delivery %d: %v", i+1, err)
		}
	}
	if n := e.num(`SELECT count(*) FROM video_processing_jobs WHERE video_id = $1::uuid`, vid); n != 1 {
		t.Fatalf("%d jobs queued by two completions", n)
	}
	if e.str(`SELECT status::text FROM videos WHERE id = $1::uuid`, vid) != "PROCESSING" ||
		e.str(`SELECT status::text FROM upload_sessions WHERE id = $1::uuid`, sid) != "COMPLETED" {
		t.Fatal("video not PROCESSING / session not COMPLETED")
	}
	// the worker takes that one job, and a second worker finds nothing to take
	if j := claimFor(t, e.store("a"), vid); j.Type != "METADATA" {
		t.Fatalf("%+v", j)
	}
	if j, _ := claimVideo(ctxb, e.store("b"), vid); j != nil {
		t.Fatal("two workers hold the same job")
	}
}

func TestDuplicateJobsAndIllegalTransitionsAreRefused(t *testing.T) {
	e := newPGEnv(t)
	vid := e.video(3)
	s := e.store("w")

	// delivering the same job again is a no-op while one is queued or running
	for i := 0; i < 2; i++ {
		if _, err := s.pool.Exec(ctxb, qEnqueue, vid, "METADATA"); err != nil {
			t.Fatal(err)
		}
	}
	if e.num(`SELECT count(*) FROM video_processing_jobs WHERE video_id = $1::uuid AND job_type = 'METADATA'`, vid) != 1 {
		t.Fatal("a duplicate job was queued")
	}
	j := claimFor(t, s, vid)
	if _, err := s.pool.Exec(ctxb, qEnqueue, vid, "METADATA"); err != nil {
		t.Fatal(err)
	}
	if e.num(`SELECT count(*) FROM video_processing_jobs WHERE video_id = $1::uuid AND job_type = 'METADATA'`, vid) != 1 {
		t.Fatal("a duplicate job was queued while the first one runs")
	}

	// a finished job cannot be finished again (a duplicate delivery of the result)
	if err := s.FinishTranscode(ctxb, j); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishTranscode(ctxb, j); err != ErrLeaseLost {
		t.Fatalf("second completion of the same job: %v", err)
	}

	// results cannot be written for a video that is not PROCESSING any more
	e.exec(`UPDATE videos SET status = 'FAILED' WHERE id = $1::uuid`, vid)
	hj := claimFor(t, s, vid) // the HLS job that FinishTranscode queued
	if _, err := s.FinishHLS(ctxb, hj, "hls/"+vid+"/master.m3u8", "p"); err != ErrVideoGone {
		t.Fatalf("READY on a FAILED video: %v", err)
	}
	if e.str(`SELECT status::text FROM videos WHERE id = $1::uuid`, vid) != "FAILED" {
		t.Fatal("a FAILED video was moved to READY")
	}
	// and the database itself refuses READY without a playable package
	v2 := e.video(3)
	if _, err := e.admin.Exec(ctxb, `UPDATE videos SET status = 'READY' WHERE id = $1::uuid`, v2); err == nil {
		t.Fatal("the database accepted READY without a master playlist")
	}
}

// Processing a video must never publish it: it ends READY (private, waiting for the creator), not PUBLISHED.
func TestProcessingNeverPublishes(t *testing.T) {
	e := newPGEnv(t)
	vid := e.video(3)
	s := e.store("w")
	e.exec(`UPDATE videos SET video_type = 'SHORT', duration_seconds = 5 WHERE id = $1::uuid`, vid)
	j := claimFor(t, s, vid)
	if err := s.FinishTranscode(ctxb, j); err != nil {
		t.Fatal(err)
	}
	hj := claimFor(t, s, vid)
	if _, err := s.FinishHLS(ctxb, hj, "hls/"+vid+"/master.m3u8", "h264-aac-hls-v1"); err != nil {
		t.Fatal(err)
	}
	if e.str(`SELECT status::text FROM videos WHERE id = $1::uuid`, vid) != "READY" ||
		e.num(`SELECT count(*) FROM videos WHERE id = $1::uuid AND published_at IS NOT NULL`, vid) != 0 {
		t.Fatal("processing published the video")
	}
	if e.str(`SELECT visibility::text FROM videos WHERE id = $1::uuid`, vid) == "PUBLIC" &&
		e.num(`SELECT count(*) FROM videos WHERE id = $1::uuid AND status = 'PUBLISHED'`, vid) != 0 {
		t.Fatal("public and published without the creator's action")
	}
}

// SIGTERM during a job: the job gets the grace period, then it is stopped, its process tree is killed and the
// job goes back to the queue WITHOUT using up an attempt, so another worker can take it.
func TestShutdownReleasesTheRunningJob(t *testing.T) {
	e := newPGEnv(t)
	vid := e.video(3)
	e.exec(`UPDATE video_processing_jobs SET status = 'CANCELLED' WHERE status = 'QUEUED' AND video_id <> $1::uuid`, vid)
	mem := newMem()
	content := []byte("a file the scanner never finishes reading")
	mem.objs["uploads/"+vid+"/original.mp4"] = content
	e.exec(`UPDATE videos SET file_size_bytes = $2 WHERE id = $1::uuid`, vid, len(content))

	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	script := filepath.Join(dir, "slow.sh")
	_ = os.WriteFile(script, []byte("#!/bin/sh\necho $$ > "+pidFile+"\nsleep 120\n"), 0o700)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	pg := NewPG(e.worker, "shutdown-test")
	pipe := &Pipeline{Store: pg, Storage: mem, Log: log, WorkDir: t.TempDir(), FFmpeg: "ffmpeg", FFprobe: "ffprobe",
		Scanner: &ExecScanner{Args: []string{script, "{file}"}, Timeout: 3 * time.Minute}}
	w := New(Config{Concurrency: 1, PollInterval: 50 * time.Millisecond, HeartbeatEvery: 200 * time.Millisecond,
		ShutdownGrace: 500 * time.Millisecond, WorkDir: pipe.WorkDir}, pg, pipe, log)
	ctx, sigterm := context.WithCancel(ctxb)
	stopped := make(chan struct{})
	go func() { w.Run(ctx); close(stopped) }()

	var pid int
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if b, err := os.ReadFile(pidFile); err == nil && strings.TrimSpace(string(b)) != "" {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
			break
		}
	}
	if pid == 0 {
		t.Fatal("the job never started")
	}
	began := time.Now()
	sigterm()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("the worker did not stop after SIGTERM")
	}
	t.Logf("worker stopped %v after SIGTERM (grace 500ms)", time.Since(began).Round(time.Millisecond))
	if syscall.Kill(pid, 0) == nil {
		t.Fatal("the job's process was left running after shutdown")
	}
	if e.str(`SELECT status::text FROM video_processing_jobs WHERE video_id = $1::uuid`, vid) != "QUEUED" ||
		e.num(`SELECT attempts FROM video_processing_jobs WHERE video_id = $1::uuid`, vid) != 0 ||
		e.str(`SELECT worker_id FROM video_processing_jobs WHERE video_id = $1::uuid`, vid) != "<null>" {
		t.Fatal("the interrupted job was not released back to the queue untouched")
	}
	if e.str(`SELECT status::text FROM videos WHERE id = $1::uuid`, vid) != "PROCESSING" {
		t.Fatal("a shutdown must not fail the video")
	}
}
