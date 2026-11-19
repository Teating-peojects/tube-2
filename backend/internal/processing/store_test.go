package processing_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redxtube/api/internal/processing"
)

var meta = []string{"METADATA"}

func TestClaimOnlyTakesRunnableJobs(t *testing.T) {
	e := setup(t)
	ctx := context.Background()

	_, other := e.queued()
	e.exec(`UPDATE video_processing_jobs SET job_type = 'TRANSCODE' WHERE id = $1::uuid`, other)
	_, later := e.queued()
	e.exec(`UPDATE video_processing_jobs SET scheduled_at = now() + interval '1 hour' WHERE id = $1::uuid`, later)
	_, spent := e.queued()
	e.exec(`UPDATE video_processing_jobs SET attempts = max_attempts WHERE id = $1::uuid`, spent)
	gone, _ := e.queued()
	e.exec(`UPDATE videos SET deleted_at = now() WHERE id = $1::uuid`, gone)
	notProc := e.video("UPLOADING")
	e.job(notProc, "METADATA", 3)

	if j, err := e.store.Claim(ctx, "w1", meta); err != nil || j != nil {
		t.Fatalf("nothing should be claimable (wrong type, future, spent, deleted video, video not PROCESSING), got %v, %v", j, err)
	}
	if _, err := e.store.Claim(ctx, "", meta); err == nil {
		t.Fatal("an empty worker id must be refused")
	}

	vid, id := e.queued()
	j, err := e.store.Claim(ctx, "w1", meta)
	if err != nil || j == nil {
		t.Fatalf("claim: %v, %v", j, err)
	}
	if j.ID != id || j.VideoID != vid || j.Type != "METADATA" || j.Attempts != 1 || j.MaxAttempts != 3 || j.ProcessingVersion != 1 || j.WorkerID != "w1" {
		t.Fatalf("unexpected claim: %+v", j)
	}
	r := e.row(id)
	if r.Status != "PROCESSING" || r.WorkerID != "w1" || r.Attempts != 1 {
		t.Fatalf("row after claim: %+v", r)
	}
}

func TestTenClaimersNeverShareAJob(t *testing.T) {
	e := setup(t)
	const jobs = 60
	want := map[string]bool{}
	for i := 0; i < jobs; i++ {
		_, id := e.queued()
		want[id] = true
	}
	var mu sync.Mutex
	got := map[string]int{}
	var wg sync.WaitGroup
	for w := 0; w < 10; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for {
				j, err := e.store.Claim(context.Background(), "w"+string(rune('a'+w)), meta)
				if err != nil {
					t.Error(err)
					return
				}
				if j == nil {
					return
				}
				mu.Lock()
				got[j.ID]++
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()
	if len(got) != jobs {
		t.Fatalf("claimed %d distinct jobs, want %d", len(got), jobs)
	}
	for id, n := range got {
		if n != 1 || !want[id] {
			t.Fatalf("job %s claimed %d times", id, n)
		}
		if r := e.row(id); r.Attempts != 1 || r.Status != "PROCESSING" {
			t.Fatalf("job %s: %+v", id, r)
		}
	}
}

func TestTwoClaimersOneJobExactlyOneWins(t *testing.T) {
	e := setup(t)
	for round := 0; round < 25; round++ {
		_, id := e.queued()
		start := make(chan struct{})
		res := make(chan *processing.Job, 2)
		for _, w := range []string{"left", "right"} {
			go func(w string) {
				<-start
				j, err := e.store.Claim(context.Background(), w, meta)
				if err != nil {
					t.Error(err)
				}
				res <- j
			}(w)
		}
		close(start)
		a, b := <-res, <-res
		if (a == nil) == (b == nil) {
			t.Fatalf("round %d: exactly one claimer must win (got %v and %v)", round, a, b)
		}
		if r := e.row(id); r.Attempts != 1 {
			t.Fatalf("round %d: attempts = %d", round, r.Attempts)
		}
	}
}

func TestHeartbeatRecordsProgressAndIsFenced(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	vid, id := e.queued()
	if _, err := e.store.Claim(ctx, "w1", meta); err != nil {
		t.Fatal(err)
	}
	if c, err := e.store.Heartbeat(ctx, id, "w1", "probe", 40); err != nil || c {
		t.Fatalf("heartbeat: %v %v", c, err)
	}
	if _, err := e.store.Heartbeat(ctx, id, "w1", "", 10); err != nil { // lower progress and no stage keep the old values
		t.Fatal(err)
	}
	if r := e.row(id); r.Progress != 40 || r.Stage != "probe" {
		t.Fatalf("progress must never go backwards: %+v", r)
	}
	if _, err := e.store.Heartbeat(ctx, id, "intruder", "x", 99); !errors.Is(err, processing.ErrLeaseLost) {
		t.Fatalf("a worker that does not hold the lease must be refused, got %v", err)
	}
	if _, _, err := e.store.Cancel(ctx, vid); err != nil {
		t.Fatal(err)
	}
	if c, err := e.store.Heartbeat(ctx, id, "w1", "", 0); err != nil || !c {
		t.Fatalf("the heartbeat must report the cancel request: %v %v", c, err)
	}
}

func TestCompleteIsIdempotentAndFenced(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	_, id := e.queued()
	if err := e.store.Complete(ctx, id, "w1"); !errors.Is(err, processing.ErrLeaseLost) {
		t.Fatalf("completing a job that was never claimed must fail, got %v", err)
	}
	if _, err := e.store.Claim(ctx, "w1", meta); err != nil {
		t.Fatal(err)
	}
	if err := e.store.Complete(ctx, id, "w2"); !errors.Is(err, processing.ErrLeaseLost) {
		t.Fatalf("another worker must not complete it, got %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := e.store.Complete(ctx, id, "w1"); err != nil {
			t.Fatalf("complete #%d: %v", i+1, err)
		}
	}
	if r := e.row(id); r.Status != "COMPLETED" || r.Progress != 100 || !r.HasCompleted || r.Attempts != 1 {
		t.Fatalf("row: %+v", r)
	}
	if err := e.store.Complete(ctx, id, "w2"); !errors.Is(err, processing.ErrLeaseLost) {
		t.Fatalf("another worker must not 'complete' a finished job, got %v", err)
	}
	if _, err := e.store.Fail(ctx, id, "w1", "internal", true, 0); !errors.Is(err, processing.ErrLeaseLost) {
		t.Fatalf("a completed job cannot fail (illegal transition), got %v", err)
	}
	if err := e.store.Complete(ctx, "00000000-0000-0000-0000-00000000dead", "w1"); !errors.Is(err, processing.ErrNotFound) {
		t.Fatalf("unknown job: %v", err)
	}
	if r := e.row(id); r.Status != "COMPLETED" {
		t.Fatalf("a refused transition must change nothing: %+v", r)
	}
}

func TestRetryBackoffAndTheAttemptBudget(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	_, id := e.queued() // max_attempts = 3

	for attempt := 1; attempt <= 2; attempt++ {
		j, err := e.store.Claim(ctx, "w1", meta)
		if err != nil || j == nil || j.Attempts != attempt {
			t.Fatalf("claim #%d: %v %v", attempt, j, err)
		}
		st, err := e.store.Fail(ctx, id, "w1", "storage_error", false, time.Hour)
		if err != nil || st != "QUEUED" {
			t.Fatalf("transient failure #%d: %q %v", attempt, st, err)
		}
		r := e.row(id)
		if r.Status != "QUEUED" || r.WorkerID != "" || !r.ScheduledInFuture || r.ErrorCode != "storage_error" {
			t.Fatalf("after failure #%d: %+v", attempt, r)
		}
		if j, _ := e.store.Claim(ctx, "w1", meta); j != nil {
			t.Fatalf("a job waiting for its backoff must not be claimable")
		}
		e.exec(`UPDATE video_processing_jobs SET scheduled_at = now() WHERE id = $1::uuid`, id)
	}
	if j, err := e.store.Claim(ctx, "w1", meta); err != nil || j == nil || j.Attempts != 3 {
		t.Fatalf("third claim: %v %v", j, err)
	}
	st, err := e.store.Fail(ctx, id, "w1", "storage_error", false, time.Hour)
	if err != nil || st != "FAILED" {
		t.Fatalf("the last attempt must end FAILED, got %q %v", st, err)
	}
	if r := e.row(id); r.Status != "FAILED" || r.ErrorCode != "storage_error" || r.Attempts != 3 {
		t.Fatalf("row: %+v", r)
	}
	if j, _ := e.store.Claim(ctx, "w1", meta); j != nil {
		t.Fatal("FAILED is terminal")
	}
}

func TestPermanentFailureAndSafeCodes(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	_, id := e.queued()
	if _, err := e.store.Claim(ctx, "w1", meta); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.Fail(ctx, id, "w1", "ffmpeg said: /tmp/x.mp4: Invalid data!", true, 0); err == nil {
		t.Fatal("free text must not be accepted as an error code (the database CHECK is the last line of defence)")
	}
	if r := e.row(id); r.Status != "PROCESSING" {
		t.Fatalf("a refused failure must leave the job untouched: %+v", r)
	}
	st, err := e.store.Fail(ctx, id, "w1", "unsupported_media", true, 0)
	if err != nil || st != "FAILED" {
		t.Fatalf("%q %v", st, err)
	}
	if r := e.row(id); r.Status != "FAILED" || r.ErrorCode != "unsupported_media" || r.Attempts != 1 {
		t.Fatalf("a permanent failure ends at once: %+v", r)
	}
	if _, err := e.store.Fail(ctx, id, "w1", "internal", true, 0); !errors.Is(err, processing.ErrLeaseLost) {
		t.Fatalf("FAILED is terminal: %v", err)
	}
}

func TestStaleLeaseRecoveryAndFencing(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	_, id := e.queued()
	old := func() {
		e.exec(`UPDATE video_processing_jobs SET heartbeat_at = now() - interval '10 minutes' WHERE id = $1::uuid`, id)
	}

	if _, err := e.store.Claim(ctx, "dead", meta); err != nil {
		t.Fatal(err)
	}
	if n, f, err := e.store.RecoverStale(ctx, 2*time.Minute, 100); err != nil || n+f != 0 {
		t.Fatalf("a fresh lease must be left alone: %d %d %v", n, f, err)
	}
	old()
	if n, f, err := e.store.RecoverStale(ctx, 2*time.Minute, 100); err != nil || n != 1 || f != 0 {
		t.Fatalf("stale lease: requeued %d failed %d %v", n, f, err)
	}
	if r := e.row(id); r.Status != "QUEUED" || r.WorkerID != "" || r.Attempts != 1 || r.ErrorCode != "lease_expired" {
		t.Fatalf("after recovery (the dead worker keeps its attempt): %+v", r)
	}
	if err := e.store.Complete(ctx, id, "dead"); !errors.Is(err, processing.ErrLeaseLost) {
		t.Fatalf("the revived worker must not complete a job it lost: %v", err)
	}
	if j, err := e.store.Claim(ctx, "w2", meta); err != nil || j == nil || j.Attempts != 2 {
		t.Fatalf("re-claim: %v %v", j, err)
	}
	old()
	e.store.RecoverStale(ctx, 2*time.Minute, 100)
	if j, _ := e.store.Claim(ctx, "w3", meta); j == nil || j.Attempts != 3 {
		t.Fatalf("third claim: %v", j)
	}
	old()
	if n, f, err := e.store.RecoverStale(ctx, 2*time.Minute, 100); err != nil || n != 0 || f != 1 {
		t.Fatalf("budget used up: requeued %d failed %d %v", n, f, err)
	}
	if r := e.row(id); r.Status != "FAILED" || r.ErrorCode != "lease_expired" {
		t.Fatalf("row: %+v", r)
	}
}

func TestCancel(t *testing.T) {
	e := setup(t)
	ctx := context.Background()

	vq, queued := e.queued()
	if q, r, err := e.store.Cancel(ctx, vq); err != nil || q != 1 || r != 0 {
		t.Fatalf("cancel queued: %d %d %v", q, r, err)
	}
	if r := e.row(queued); r.Status != "FAILED" || r.ErrorCode != "cancelled" {
		t.Fatalf("a queued job is cancelled at once: %+v", r)
	}

	vr, running := e.queued()
	if _, err := e.store.Claim(ctx, "w1", meta); err != nil {
		t.Fatal(err)
	}
	if q, r, err := e.store.Cancel(ctx, vr); err != nil || q != 0 || r != 1 {
		t.Fatalf("cancel running: %d %d %v", q, r, err)
	}
	if r := e.row(running); r.Status != "PROCESSING" || !r.Cancel {
		t.Fatalf("a running job only gets a marker: %+v", r)
	}
	st, err := e.store.Fail(ctx, running, "w1", "storage_error", false, time.Minute)
	if err != nil || st != "FAILED" {
		t.Fatalf("a transient failure of a cancelled job must not retry: %q %v", st, err)
	}
	if r := e.row(running); r.ErrorCode != "cancelled" {
		t.Fatalf("row: %+v", r)
	}

	// release and stale recovery honour the marker as well
	vr2, j2 := e.queued()
	e.store.Claim(ctx, "w1", meta)
	e.store.Cancel(ctx, vr2)
	if st, err := e.store.Release(ctx, j2, "w1"); err != nil || st != "FAILED" {
		t.Fatalf("release of a cancelled job: %q %v", st, err)
	}
	vr3, j3 := e.queued()
	e.store.Claim(ctx, "w1", meta)
	e.store.Cancel(ctx, vr3)
	e.exec(`UPDATE video_processing_jobs SET heartbeat_at = now() - interval '1 hour' WHERE id = $1::uuid`, j3)
	if _, f, err := e.store.RecoverStale(ctx, time.Minute, 10); err != nil || f != 1 {
		t.Fatalf("stale + cancelled: %d %v", f, err)
	}
	if r := e.row(j3); r.Status != "FAILED" || r.ErrorCode != "cancelled" {
		t.Fatalf("row: %+v", r)
	}
}

func TestReleaseRefundsTheAttempt(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	_, id := e.queued()
	if _, err := e.store.Claim(ctx, "w1", meta); err != nil {
		t.Fatal(err)
	}
	if st, err := e.store.Release(ctx, id, "w1"); err != nil || st != "QUEUED" {
		t.Fatalf("%q %v", st, err)
	}
	if r := e.row(id); r.Status != "QUEUED" || r.Attempts != 0 || r.WorkerID != "" || r.ScheduledInFuture {
		t.Fatalf("a graceful release must not cost an attempt or a delay: %+v", r)
	}
	if _, err := e.store.Release(ctx, id, "w1"); !errors.Is(err, processing.ErrLeaseLost) {
		t.Fatalf("releasing a job one does not hold: %v", err)
	}
	if j, _ := e.store.Claim(ctx, "w2", meta); j == nil || j.Attempts != 1 {
		t.Fatalf("the released job is claimable again: %v", j)
	}
}

func TestFailOrphans(t *testing.T) {
	e := setup(t)
	ctx := context.Background()

	runDel, jRun := e.queued() // claimed first, so it is the running one
	if j, _ := e.store.Claim(ctx, "w1", meta); j == nil || j.ID != jRun {
		t.Fatalf("expected to claim %s, got %v", jRun, j)
	}
	e.exec(`UPDATE videos SET deleted_at = now() WHERE id = $1::uuid`, runDel)

	deleted, jDeleted := e.queued()
	e.exec(`UPDATE videos SET deleted_at = now() WHERE id = $1::uuid`, deleted)
	left := e.video("UPLOADING")
	jLeft := e.job(left, "METADATA", 3)
	_, jHealthy := e.queued()

	f, s, err := e.store.FailOrphans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if f != 2 || s != 1 {
		t.Fatalf("failed %d queued orphans (want 2), asked %d running jobs to stop (want 1)", f, s)
	}
	for _, id := range []string{jDeleted, jLeft} {
		if r := e.row(id); r.Status != "FAILED" || r.ErrorCode != "video_gone" {
			t.Fatalf("%s: %+v", id, r)
		}
	}
	if r := e.row(jHealthy); r.Status != "QUEUED" || r.ErrorCode != "" || r.Cancel {
		t.Fatalf("a healthy job must be untouched: %+v", r)
	}
	if r := e.row(jRun); r.Status != "PROCESSING" || !r.Cancel {
		t.Fatalf("a running job of a deleted video is asked to stop, not killed: %+v", r)
	}
}

// SKIP LOCKED is about liveness, not about correctness: under READ COMMITTED a claimer that waits for a locked row
// still ends up with a different job. This test fails if a claimer ever waits behind a row that someone else holds.
func TestClaimDoesNotWaitForARowThatAnotherTransactionHolds(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	_, locked := e.queued()
	_, free := e.queued()

	tx, err := e.admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT 1 FROM video_processing_jobs WHERE id = $1::uuid FOR UPDATE`, locked); err != nil {
		t.Fatal(err)
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	start := time.Now()
	j, err := e.store.Claim(cctx, "w1", meta)
	if err != nil || j == nil {
		t.Fatalf("the claim must not wait for the locked row: %v %v (after %v)", j, err, time.Since(start))
	}
	if j.ID != free {
		t.Fatalf("claimed %s, want the unlocked job %s", j.ID, free)
	}
}
