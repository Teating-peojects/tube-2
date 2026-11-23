package worker

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Integration tests against a real PostgreSQL built from db/migrations. They run as the real
// least-privilege role (redxtube_worker), so a missing GRANT shows up here and not in production.
//
//	TEST_WORKER_ADMIN_DATABASE_URL  superuser on the WORKER database (fixtures); falls back to
//	                                TEST_ADMIN_DATABASE_URL
//	TEST_WORKER_DATABASE_URL        redxtube_worker on the same database
//
// Use a database of their own (db/scripts/test-db.sh builds it): `go test ./...` runs packages in
// parallel and the handler tests TRUNCATE the tables they share.
type pgEnv struct {
	t      *testing.T
	admin  *pgxpool.Pool
	worker *pgxpool.Pool
	user   string
}

func newPGEnv(t *testing.T) *pgEnv {
	adminURL, workerURL := os.Getenv("TEST_WORKER_ADMIN_DATABASE_URL"), os.Getenv("TEST_WORKER_DATABASE_URL")
	if adminURL == "" {
		adminURL = os.Getenv("TEST_ADMIN_DATABASE_URL")
	}
	if adminURL == "" || workerURL == "" {
		t.Skip("TEST_WORKER_ADMIN_DATABASE_URL (or TEST_ADMIN_DATABASE_URL) / TEST_WORKER_DATABASE_URL not set")
	}
	ctx := context.Background()
	a, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	w, err := pgxpool.New(ctx, workerURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(); w.Close() })
	e := &pgEnv{t: t, admin: a, worker: w, user: uuid.NewString()}
	e.exec(`INSERT INTO users (id, username, email, password_hash, display_name, is_creator, is_verified)
VALUES ($1::uuid, $2, $3, '$argon2id$v=19$m=8192,t=1,p=1$c2FsdA$a2V5', 'Test Studio', true, true)`,
		e.user, "u_"+e.user[:8], e.user[:8]+"@test.local")
	return e
}

func (e *pgEnv) exec(q string, args ...any) {
	e.t.Helper()
	if _, err := e.admin.Exec(context.Background(), q, args...); err != nil {
		e.t.Fatalf("%v\n%s", err, q)
	}
}

// backdate moves updated_at into the past. A trigger rewrites updated_at on every UPDATE, so the
// triggers are switched off for this one transaction (superuser fixture only).
func (e *pgEnv) backdate(ids []string, ago string) {
	e.t.Helper()
	tx, err := e.admin.Begin(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(context.Background(), `SET LOCAL session_replication_role = replica`); err != nil {
		e.t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `UPDATE videos SET updated_at = now() - $2::interval WHERE id = ANY($1::uuid[])`, ids, ago); err != nil {
		e.t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

func (e *pgEnv) scan(q string, args ...any) []any {
	e.t.Helper()
	rows, err := e.admin.Query(context.Background(), q, args...)
	if err != nil {
		e.t.Fatalf("%v\n%s", err, q)
	}
	defer rows.Close()
	if !rows.Next() {
		e.t.Fatalf("no row: %s", q)
	}
	v, err := rows.Values()
	if err != nil {
		e.t.Fatal(err)
	}
	return v
}

func (e *pgEnv) str(q string, args ...any) string {
	v := e.scan(q, args...)[0]
	if v == nil {
		return "<null>"
	}
	return v.(string)
}

func (e *pgEnv) num(q string, args ...any) int {
	switch n := e.scan(q, args...)[0].(type) {
	case int64:
		return int(n)
	case int32:
		return int(n)
	case int16:
		return int(n)
	}
	e.t.Fatalf("not a number: %s", q)
	return 0
}

// video creates a PROCESSING video with a queued METADATA job.
func (e *pgEnv) video(maxAttempts int) string {
	id := uuid.NewString()
	e.exec(`INSERT INTO videos (id, creator_id, title, original_filename, original_storage_key, file_size_bytes, status)
VALUES ($1::uuid, $2::uuid, 'Clip', 'clip.mp4', $3, 1000, 'PROCESSING')`, id, e.user, "uploads/"+id+"/original.mp4")
	e.exec(`INSERT INTO video_processing_jobs (video_id, job_type, max_attempts) VALUES ($1::uuid, 'METADATA', $2)`, id, maxAttempts)
	return id
}

func (e *pgEnv) store(name string) *PG { return NewPG(e.worker, name) }

// claimVideo is Claim restricted to one video: the production SQL plus a video filter, so a test
// never claims (or livelocks on) jobs that belong to other tests or earlier runs.
func claimVideo(ctx context.Context, s *PG, video string) (*Job, error) {
	q := strings.Replace(qClaim, "WHERE q.status = 'QUEUED'", "WHERE q.video_id = $2::uuid AND q.status = 'QUEUED'", 1)
	if q == qClaim {
		panic("qClaim changed: update claimVideo")
	}
	var j Job
	err := s.pool.QueryRow(ctx, q, s.workerID, video).Scan(&j.ID, &j.VideoID, &j.Type, &j.Attempts, &j.MaxAttempts, &j.WaitSec)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func claimFor(t *testing.T, s *PG, video string) Job {
	t.Helper()
	j, err := claimVideo(context.Background(), s, video)
	if err != nil {
		t.Fatal(err)
	}
	if j == nil {
		t.Fatalf("no claimable job for %s", video)
	}
	return *j
}

var ctxb = context.Background()

func TestPGFullChain(t *testing.T) {
	e := newPGEnv(t)
	vid := e.video(3)

	// two workers race for one job: exactly one wins
	var wins int
	var mu sync.Mutex
	var wg sync.WaitGroup
	var won Job
	for _, w := range []*PG{e.store("a"), e.store("b")} {
		wg.Add(1)
		go func(w *PG) {
			defer wg.Done()
			j, err := claimVideo(ctxb, w, vid)
			if err != nil {
				t.Error(err)
				return
			}
			if j != nil {
				mu.Lock()
				wins++
				won = *j
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d workers claimed the same job", wins)
	}
	owner := e.store(e.str(`SELECT worker_id FROM video_processing_jobs WHERE id = $1::uuid`, won.ID))
	if won.Type != "METADATA" || won.Attempts != 1 {
		t.Fatalf("claimed %+v", won)
	}
	other := e.store("intruder")
	if err := other.Heartbeat(ctxb, won); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("heartbeat by a non-owner: %v", err)
	}
	if err := owner.Heartbeat(ctxb, won); err != nil {
		t.Fatal(err)
	}

	// METADATA -> THUMBNAIL queued
	sum := sha256.Sum256([]byte("clip"))
	blocked, err := owner.SaveMetadata(ctxb, won, MetadataResult{
		Probe:     Probe{Width: 1280, Height: 720, FPS: 29.97, VideoCodec: "h264", AudioCodec: "aac", Container: "mp4", Bitrate: 2_000_000, HasAudio: true},
		VideoType: "SHORT", Seconds: 14, SHA256: sum[:]})
	if err != nil || blocked {
		t.Fatalf("SaveMetadata: blocked=%v err=%v", blocked, err)
	}
	if e.str(`SELECT status::text FROM video_processing_jobs WHERE id = $1::uuid`, won.ID) != "COMPLETED" ||
		e.str(`SELECT status::text FROM video_processing_jobs WHERE video_id = $1::uuid AND job_type = 'THUMBNAIL'`, vid) != "QUEUED" {
		t.Fatal("METADATA did not complete and enqueue THUMBNAIL")
	}
	if e.num(`SELECT width FROM video_metadata WHERE video_id = $1::uuid`, vid) != 1280 ||
		e.str(`SELECT video_type::text FROM videos WHERE id = $1::uuid`, vid) != "SHORT" {
		t.Fatal("metadata not stored")
	}
	m, err := owner.Meta(ctxb, vid)
	if err != nil || m.Duration != 14 || !m.HasAudio || m.FPS < 29.9 {
		t.Fatalf("Meta %+v %v", m, err)
	}

	// THUMBNAIL replaces earlier generated thumbnails
	e.exec(`INSERT INTO video_thumbnails (video_id, storage_key, type, source, status) VALUES ($1::uuid, 'thumbs/old.jpg', 'THUMBNAIL', 'GENERATED', 'READY')`, vid)
	tj := claimFor(t, owner, vid)
	if tj.Type != "THUMBNAIL" {
		t.Fatalf("got %s", tj.Type)
	}
	var items []ThumbOut
	for i := 0; i < 5; i++ {
		items = append(items, ThumbOut{Key: "thumbs/" + vid + "/" + uuid.NewString() + ".jpg", W: 640, H: 360})
	}
	old, err := owner.ReplaceThumbnails(ctxb, tj, items, 2)
	if err != nil || len(old) != 1 || old[0] != "thumbs/old.jpg" {
		t.Fatalf("ReplaceThumbnails old=%v err=%v", old, err)
	}
	if e.num(`SELECT count(*) FROM video_thumbnails WHERE video_id = $1::uuid`, vid) != 5 ||
		e.num(`SELECT count(*) FROM video_thumbnails WHERE video_id = $1::uuid AND is_primary`, vid) != 1 {
		t.Fatal("thumbnails / single primary not stored")
	}

	// TRANSCODE -> variants -> HLS queued
	xj := claimFor(t, owner, vid)
	if xj.Type != "TRANSCODE" {
		t.Fatalf("got %s", xj.Type)
	}
	for _, r := range Plan(1280, 720) {
		if err := owner.SaveVariant(ctxb, vid, VariantOut{R: r, Key: "hls/" + vid + "/" + r.Name + "/", Bytes: 1234}); err != nil {
			t.Fatal(err)
		}
	}
	if err := owner.SaveVariant(ctxb, vid, VariantOut{R: Plan(1280, 720)[0], Key: "hls/" + vid + "/360p/", Bytes: 99}); err != nil {
		t.Fatalf("variant upsert must be idempotent: %v", err)
	}
	if err := owner.FinishTranscode(ctxb, xj); err != nil {
		t.Fatal(err)
	}
	hj := claimFor(t, owner, vid)
	if hj.Type != "HLS" {
		t.Fatalf("got %s", hj.Type)
	}
	rv, err := owner.Variants(ctxb, vid)
	if err != nil || len(rv) != 3 {
		t.Fatalf("variants %v %v", rv, err)
	}

	// HLS -> READY (satisfies videos_ready_requirements) + notification
	creator, err := owner.FinishHLS(ctxb, hj, "hls/"+vid+"/master.m3u8", "h264-aac-hls-v1")
	if err != nil || creator != e.user {
		t.Fatalf("FinishHLS %q %v", creator, err)
	}
	if e.str(`SELECT status::text FROM videos WHERE id = $1::uuid`, vid) != "READY" ||
		e.str(`SELECT hls_master_key FROM videos WHERE id = $1::uuid`, vid) != "hls/"+vid+"/master.m3u8" {
		t.Fatal("video not READY")
	}
	if e.str(`SELECT processing_profile FROM videos WHERE id = $1::uuid`, vid) != "h264-aac-hls-v1" {
		t.Fatal("encoding profile version not stored on the video")
	}
	if e.num(`SELECT count(*) FROM notifications WHERE video_id = $1::uuid AND type = 'VIDEO_READY' AND user_id = $2::uuid`, vid, e.user) != 1 {
		t.Fatal("VIDEO_READY notification missing")
	}
	if e.num(`SELECT count(*) FROM video_processing_jobs WHERE video_id = $1::uuid AND status = 'COMPLETED'`, vid) != 4 {
		t.Fatal("the upload status endpoint counts 4 completed jobs")
	}
}

func TestPGRetryBackoffThenFail(t *testing.T) {
	e := newPGEnv(t)
	vid := e.video(2)
	s := e.store("w")

	j := claimFor(t, s, vid)
	res, err := s.Fail(ctxb, j, transient("ffmpeg_failed", errors.New("boom")))
	if err != nil || res.Final {
		t.Fatalf("first failure must retry: %+v %v", res, err)
	}
	if e.str(`SELECT status::text FROM video_processing_jobs WHERE id = $1::uuid`, j.ID) != "QUEUED" {
		t.Fatal("not re-queued")
	}
	if e.num(`SELECT EXTRACT(EPOCH FROM scheduled_at - now())::int FROM video_processing_jobs WHERE id = $1::uuid`, j.ID) < 20 {
		t.Fatal("no backoff applied")
	}
	if got, _ := claimVideo(ctxb, s, vid); got != nil {
		t.Fatal("job claimed before its backoff elapsed")
	}
	e.exec(`UPDATE video_processing_jobs SET scheduled_at = now() WHERE id = $1::uuid`, j.ID)
	j2 := claimFor(t, s, vid)
	if j2.Attempts != 2 {
		t.Fatalf("attempts %d", j2.Attempts)
	}
	res, err = s.Fail(ctxb, j2, transient("ffmpeg_failed", errors.New("boom")))
	if err != nil || !res.Final || res.CreatorID != e.user {
		t.Fatalf("last attempt must fail for good: %+v %v", res, err)
	}
	if e.str(`SELECT status::text FROM videos WHERE id = $1::uuid`, vid) != "FAILED" ||
		e.num(`SELECT count(*) FROM notifications WHERE video_id = $1::uuid AND type = 'VIDEO_FAILED'`, vid) != 1 {
		t.Fatal("video not FAILED / creator not told")
	}
}

func TestPGPermanentAndSilentFailure(t *testing.T) {
	e := newPGEnv(t)
	vid := e.video(3)
	s := e.store("w")
	j := claimFor(t, s, vid)
	res, err := s.Fail(ctxb, j, &JobError{Code: "video_unavailable", Message: "gone", Permanent: true, Silent: true})
	if err != nil || !res.Final || res.CreatorID != "" {
		t.Fatalf("%+v %v", res, err)
	}
	if e.str(`SELECT status::text FROM videos WHERE id = $1::uuid`, vid) != "FAILED" ||
		e.num(`SELECT count(*) FROM notifications WHERE video_id = $1::uuid`, vid) != 0 {
		t.Fatal("silent failure must not notify")
	}
}

func TestPGBlockedHashRemovesVideo(t *testing.T) {
	e := newPGEnv(t)
	vid := e.video(3)
	s := e.store("w")
	sum := sha256.Sum256([]byte("blocked-" + vid))
	e.exec(`INSERT INTO blocked_hashes (sha256, reason) VALUES ($1, 'test')`, sum[:])
	j := claimFor(t, s, vid)
	blocked, err := s.SaveMetadata(ctxb, j, MetadataResult{
		Probe: Probe{Width: 640, Height: 360, VideoCodec: "h264", Container: "mp4"}, VideoType: "SHORT", Seconds: 5, SHA256: sum[:]})
	if err != nil || !blocked {
		t.Fatalf("blocked=%v err=%v", blocked, err)
	}
	if e.str(`SELECT moderation_status::text FROM videos WHERE id = $1::uuid`, vid) != "REMOVED" {
		t.Fatal("trigger did not remove the video")
	}
	if e.num(`SELECT count(*) FROM audit_logs WHERE action = 'moderation.hash_match' AND target_id = $1`, vid) != 1 {
		t.Fatal("hash match not audited (the worker role must be able to write audit_logs)")
	}
	if e.str(`SELECT status::text FROM video_processing_jobs WHERE id = $1::uuid`, j.ID) != "PROCESSING" ||
		e.num(`SELECT count(*) FROM video_processing_jobs WHERE video_id = $1::uuid AND job_type = 'THUMBNAIL'`, vid) != 0 {
		t.Fatal("a blocked video must not advance in the pipeline")
	}
}

func TestPGLeaseGuardAndStaleRequeue(t *testing.T) {
	e := newPGEnv(t)
	vid := e.video(2)
	a, b := e.store("A"), e.store("B")
	ja := claimFor(t, a, vid)

	// A stops heartbeating; the janitor re-queues; B takes over
	e.exec(`UPDATE video_processing_jobs SET heartbeat_at = now() - interval '10 minutes' WHERE id = $1::uuid`, ja.ID)
	if n, err := b.RequeueStale(ctxb, 2*time.Minute); err != nil || n < 1 {
		t.Fatalf("requeue stale: n=%d err=%v", n, err)
	}
	jb := claimFor(t, b, vid)
	if jb.Attempts != 2 {
		t.Fatalf("attempts %d", jb.Attempts)
	}

	// the zombie A finishes late: its result must be rejected and leave nothing behind
	_, err := a.SaveMetadata(ctxb, ja, MetadataResult{Probe: Probe{Width: 640, Height: 360, VideoCodec: "h264", Container: "mp4"}, VideoType: "SHORT", Seconds: 5, SHA256: make([]byte, 32)})
	if !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("zombie result accepted: %v", err)
	}
	if e.num(`SELECT count(*) FROM video_metadata WHERE video_id = $1::uuid`, vid) != 0 {
		t.Fatal("zombie wrote video_metadata")
	}
	if _, err := a.Fail(ctxb, ja, permanent("x", "y")); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("zombie failure accepted: %v", err)
	}

	// B dies too: attempts are exhausted, so the video fails and the creator is told
	e.exec(`UPDATE video_processing_jobs SET heartbeat_at = now() - interval '10 minutes' WHERE id = $1::uuid`, jb.ID)
	if _, err := b.RequeueStale(ctxb, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	if e.str(`SELECT status::text FROM video_processing_jobs WHERE id = $1::uuid`, jb.ID) != "FAILED" ||
		e.str(`SELECT status::text FROM videos WHERE id = $1::uuid`, vid) != "FAILED" ||
		e.num(`SELECT count(*) FROM notifications WHERE video_id = $1::uuid AND type = 'VIDEO_FAILED'`, vid) != 1 {
		t.Fatal("exhausted stale job did not fail the video")
	}
}

func TestPGReleaseKeepsAttempt(t *testing.T) {
	e := newPGEnv(t)
	vid := e.video(3)
	s := e.store("w")
	j := claimFor(t, s, vid)
	if err := s.Release(ctxb, j); err != nil {
		t.Fatal(err)
	}
	if e.num(`SELECT attempts FROM video_processing_jobs WHERE id = $1::uuid`, j.ID) != 0 {
		t.Fatal("a release on shutdown must not consume an attempt")
	}
}

func TestPGVideoGone(t *testing.T) {
	e := newPGEnv(t)
	vid := e.video(3)
	e.exec(`UPDATE videos SET deleted_at = now() WHERE id = $1::uuid`, vid)
	if _, err := e.store("w").Video(ctxb, vid); !errors.Is(err, ErrVideoGone) {
		t.Fatalf("%v", err)
	}
}

func TestPGRemoveVideoAudits(t *testing.T) {
	e := newPGEnv(t)
	vid := e.video(3)
	s := e.store("w")
	j := claimFor(t, s, vid)
	if err := s.RemoveVideo(ctxb, j, "Test-Signature FOUND"); err != nil {
		t.Fatal(err)
	}
	if e.str(`SELECT moderation_status::text FROM videos WHERE id = $1::uuid`, vid) != "REMOVED" {
		t.Fatal("not removed")
	}
	if e.num(`SELECT count(*) FROM audit_logs WHERE action = 'moderation.scan_match' AND target_id = $1`, vid) != 1 {
		t.Fatal("scan match not audited")
	}
}

func TestPGQueueStats(t *testing.T) {
	e := newPGEnv(t)
	vid := e.video(3)
	s := e.store("w")
	stats, err := s.QueueStats(ctxb)
	if err != nil {
		t.Fatal(err)
	}
	var queued QueueStat
	for _, q := range stats {
		if q.Type == "METADATA" && q.Status == "QUEUED" {
			queued = q
		}
	}
	if queued.Count < 1 {
		t.Fatalf("queued METADATA jobs not counted: %+v", stats)
	}
	j := claimFor(t, s, vid)
	if _, err := s.Fail(ctxb, j, permanent("x", "y")); err != nil {
		t.Fatal(err)
	}
	stats, _ = s.QueueStats(ctxb)
	failed := 0
	for _, q := range stats {
		if q.Type == "METADATA" && q.Status == "FAILED" {
			failed = q.Count
		}
	}
	if failed < 1 {
		t.Fatalf("recent FAILED jobs not counted: %+v", stats)
	}
}

func TestPGCleanupClaimsOnlyOldFailedVideosOnce(t *testing.T) {
	e := newPGEnv(t)
	s := e.store("w")
	failed := func() string { // a FAILED video with no active job
		vid := e.video(3)
		e.exec(`DELETE FROM video_processing_jobs WHERE video_id = $1::uuid`, vid)
		e.exec(`UPDATE videos SET status = 'FAILED' WHERE id = $1::uuid`, vid)
		return vid
	}
	old, fresh, withJob, processing := failed(), failed(), failed(), e.video(3)
	e.backdate([]string{old, withJob}, "3 days")
	e.exec(`INSERT INTO video_processing_jobs (video_id, job_type) VALUES ($1::uuid, 'METADATA')`, withJob) // a retry is queued

	ids, err := s.ClaimFailedForCleanup(ctxb, 24*time.Hour, 50)
	if err != nil {
		t.Fatal(err)
	}
	has := func(list []string, id string) bool {
		for _, x := range list {
			if x == id {
				return true
			}
		}
		return false
	}
	if !has(ids, old) {
		t.Fatalf("old failed video not claimed: %v", ids)
	}
	if has(ids, fresh) || has(ids, withJob) || has(ids, processing) {
		t.Fatalf("claimed a video that must be kept (fresh=%v withJob=%v processing=%v)", has(ids, fresh), has(ids, withJob), has(ids, processing))
	}
	// claimed exactly once
	again, _ := s.ClaimFailedForCleanup(ctxb, 24*time.Hour, 50)
	if has(again, old) {
		t.Fatal("the same video was claimed twice")
	}
	// a failed delete releases the claim: the video is claimable again once the retention period has
	// passed again (the claim itself touches updated_at, so a flapping store cannot cause a hot loop)
	if err := s.ReleaseCleanup(ctxb, old); err != nil {
		t.Fatal(err)
	}
	if again, _ = s.ClaimFailedForCleanup(ctxb, 24*time.Hour, 50); has(again, old) {
		t.Fatal("released video retried immediately")
	}
	e.backdate([]string{old}, "3 days")
	if again, _ = s.ClaimFailedForCleanup(ctxb, 24*time.Hour, 50); !has(again, old) {
		t.Fatal("released video was not claimable again")
	}
}
