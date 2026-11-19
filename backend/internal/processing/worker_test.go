package processing_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redxtube/api/internal/processing"
)

func runner(e *tenv, id string, p processing.Processor, mod func(*processing.Options)) *processing.Runner {
	o := processing.Options{
		WorkerID: id, Types: meta, Concurrency: 3, Lease: 5 * time.Second, Heartbeat: 50 * time.Millisecond,
		Poll: 20 * time.Millisecond, Maintenance: 100 * time.Millisecond, BackoffBase: time.Millisecond, BackoffMax: time.Millisecond,
		ShutdownGrace: 200 * time.Millisecond, Rand: func() float64 { return 0 },
	}
	if mod != nil {
		mod(&o)
	}
	return processing.NewRunner(e.store, p, o, discard())
}

// start runs the runner until the returned stop function is called, which waits for Run to return.
func start(r *processing.Runner) (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	return func() { cancel(); <-done }
}

type counting struct {
	mu    sync.Mutex
	calls map[string]int
	inner processing.Processor
}

func (c *counting) Process(ctx context.Context, j processing.Job, p processing.Progress) error {
	c.mu.Lock()
	c.calls[j.ID]++
	c.mu.Unlock()
	return c.inner.Process(ctx, j, p)
}

type fn func(ctx context.Context, j processing.Job, p processing.Progress) error

func (f fn) Process(ctx context.Context, j processing.Job, p processing.Progress) error {
	return f(ctx, j, p)
}

func TestTwoRunnersDrainTheQueueExactlyOnce(t *testing.T) {
	e := setup(t)
	const jobs = 40
	ids := make([]string, 0, jobs)
	for i := 0; i < jobs; i++ {
		_, id := e.queued()
		ids = append(ids, id)
	}
	c := &counting{calls: map[string]int{}, inner: processing.NoopProcessor{Duration: 5 * time.Millisecond}}
	stopA, stopB := start(runner(e, "runner-a", c, nil)), start(runner(e, "runner-b", c, nil))
	defer stopA()
	defer stopB()
	waitFor(t, 20*time.Second, "all jobs completed", func() bool {
		var n int
		_ = e.admin.QueryRow(context.Background(), `SELECT count(*) FROM video_processing_jobs WHERE status = 'COMPLETED'`).Scan(&n)
		return n == jobs
	})
	stopA()
	stopB()
	for _, id := range ids {
		if c.calls[id] != 1 {
			t.Fatalf("job %s was processed %d times", id, c.calls[id])
		}
		if r := e.row(id); r.Status != "COMPLETED" || r.Attempts != 1 || r.Progress != 100 {
			t.Fatalf("job %s: %+v", id, r)
		}
	}
}

func TestRunnerRecoversAJobOfAKilledWorker(t *testing.T) {
	e := setup(t)
	_, id := e.queued()
	if j, err := e.store.Claim(context.Background(), "killed", meta); err != nil || j == nil { // never heartbeats again
		t.Fatalf("%v %v", j, err)
	}
	e.exec(`UPDATE video_processing_jobs SET heartbeat_at = now() - interval '1 hour' WHERE id = $1::uuid`, id)
	stop := start(runner(e, "survivor", processing.NoopProcessor{Duration: time.Millisecond}, nil))
	defer stop()
	waitFor(t, 10*time.Second, "the abandoned job to complete", func() bool { return e.row(id).Status == "COMPLETED" })
	if r := e.row(id); r.Attempts != 2 || r.WorkerID != "survivor" {
		t.Fatalf("%+v", r)
	}
}

func TestRunnerRetriesTransientErrorsUntilTheBudgetIsUsed(t *testing.T) {
	e := setup(t)
	_, id := e.queued()
	var calls atomic.Int32
	stop := start(runner(e, "w", fn(func(context.Context, processing.Job, processing.Progress) error {
		calls.Add(1)
		return errors.New("disk on fire")
	}), nil))
	defer stop()
	waitFor(t, 15*time.Second, "the job to fail for good", func() bool { return e.row(id).Status == "FAILED" })
	stop()
	if calls.Load() != 3 {
		t.Fatalf("processed %d times, want max_attempts = 3", calls.Load())
	}
	if r := e.row(id); r.ErrorCode != "internal" || r.Attempts != 3 {
		t.Fatalf("%+v", r)
	}
}

func TestRunnerPermanentErrorFailsAtOnceWithItsSafeCode(t *testing.T) {
	e := setup(t)
	_, id := e.queued()
	var calls atomic.Int32
	stop := start(runner(e, "w", fn(func(context.Context, processing.Job, processing.Progress) error {
		calls.Add(1)
		return processing.Permanent("unsupported_media", errors.New("raw ffprobe output must never reach the database"))
	}), nil))
	defer stop()
	waitFor(t, 10*time.Second, "FAILED", func() bool { return e.row(id).Status == "FAILED" })
	stop()
	if r := e.row(id); r.ErrorCode != "unsupported_media" || r.Attempts != 1 || calls.Load() != 1 {
		t.Fatalf("%+v calls=%d", r, calls.Load())
	}
	var msg *string
	_ = e.admin.QueryRow(context.Background(), `SELECT error_message FROM video_processing_jobs WHERE id = $1::uuid`, id).Scan(&msg)
	if msg != nil {
		t.Fatalf("error_message must stay empty, got %q", *msg)
	}
}

func TestRunnerSurvivesAPanic(t *testing.T) {
	e := setup(t)
	_, bad := e.queued()
	var once sync.Once
	var calls atomic.Int32
	stop := start(runner(e, "w", fn(func(_ context.Context, j processing.Job, _ processing.Progress) error {
		calls.Add(1)
		if j.ID == bad {
			once.Do(func() { panic("boom") })
		}
		return nil
	}), nil))
	defer stop()
	waitFor(t, 10*time.Second, "the job to complete on its second attempt", func() bool { return e.row(bad).Status == "COMPLETED" })
	if r := e.row(bad); r.Attempts != 2 {
		t.Fatalf("a panic is a transient failure that consumes an attempt: %+v", r)
	}
	_, other := e.queued()
	waitFor(t, 10*time.Second, "the runner to keep working after the panic", func() bool { return e.row(other).Status == "COMPLETED" })
}

func TestGracefulShutdownReleasesARunningJob(t *testing.T) {
	e := setup(t)
	_, id := e.queued()
	started := make(chan struct{})
	stop := start(runner(e, "w", fn(func(ctx context.Context, _ processing.Job, _ processing.Progress) error {
		close(started)
		<-ctx.Done() // a long job that does not finish within the grace period
		return context.Cause(ctx)
	}), func(o *processing.Options) { o.Concurrency = 1 }))
	<-started
	waitFor(t, 5*time.Second, "PROCESSING", func() bool { return e.row(id).Status == "PROCESSING" })
	stop()
	if r := e.row(id); r.Status != "QUEUED" || r.Attempts != 0 || r.WorkerID != "" {
		t.Fatalf("the interrupted job goes back to the queue without losing an attempt: %+v", r)
	}
}

func TestRunnerStopsAJobWhenCancellationIsRequested(t *testing.T) {
	e := setup(t)
	vid, id := e.queued()
	saw := make(chan error, 1)
	started := make(chan struct{})
	stop := start(runner(e, "w", fn(func(ctx context.Context, _ processing.Job, p processing.Progress) error {
		p.Report("transcode", 30)
		close(started)
		<-ctx.Done()
		saw <- context.Cause(ctx)
		return context.Cause(ctx)
	}), func(o *processing.Options) { o.Concurrency = 1 }))
	defer stop()
	<-started
	waitFor(t, 5*time.Second, "progress to be stored", func() bool { r := e.row(id); return r.Stage == "transcode" && r.Progress == 30 })
	if _, _, err := e.store.Cancel(context.Background(), vid); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "FAILED(cancelled)", func() bool { r := e.row(id); return r.Status == "FAILED" && r.ErrorCode == "cancelled" })
	if err := <-saw; err == nil {
		t.Fatal("the processor must have been told to stop")
	}
}

func TestRunnerAbandonsAJobWhoseLeaseWasLost(t *testing.T) {
	e := setup(t)
	_, id := e.queued()
	started := make(chan struct{})
	stopped := make(chan struct{})
	stop := start(runner(e, "w", fn(func(ctx context.Context, _ processing.Job, _ processing.Progress) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return context.Cause(ctx)
	}), func(o *processing.Options) { o.Concurrency = 1; o.Maintenance = time.Hour }))
	defer stop()
	<-started
	e.exec(`UPDATE video_processing_jobs SET worker_id = 'thief' WHERE id = $1::uuid`, id) // someone else took the lease
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the processor was not stopped after the lease was lost")
	}
	time.Sleep(100 * time.Millisecond)
	if r := e.row(id); r.Status != "PROCESSING" || r.WorkerID != "thief" {
		t.Fatalf("the old worker must not touch a job it lost: %+v", r)
	}
}
