package processing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
)

// Progress lets a processor report where it is. The runner sends it to the database with every heartbeat.
type Progress interface {
	Report(stage string, percent int)
}

// Processor does the real work of one job. It must stop soon after ctx is cancelled (cancel request, lost lease or
// shutdown) and return an error; return Permanent(...) for failures that a retry cannot fix.
type Processor interface {
	Process(ctx context.Context, job Job, p Progress) error
}

// Options configure a Runner.
type Options struct {
	WorkerID      string
	Types         []string      // job types this worker accepts
	Concurrency   int           // jobs processed at the same time
	Lease         time.Duration // a running job without a heartbeat for this long is taken back
	Heartbeat     time.Duration // how often a running job refreshes its lease (should be well below Lease)
	Poll          time.Duration // wait between claims when the queue is empty
	Maintenance   time.Duration // how often stale leases and orphaned jobs are cleaned up
	BackoffBase   time.Duration
	BackoffMax    time.Duration
	ShutdownGrace time.Duration // how long running jobs may keep going after shutdown starts
	Rand          func() float64
}

func (o *Options) defaults() {
	if o.Concurrency < 1 {
		o.Concurrency = 1
	}
	if o.Lease <= 0 {
		o.Lease = 2 * time.Minute
	}
	if o.Heartbeat <= 0 {
		o.Heartbeat = o.Lease / 4
	}
	if o.Poll <= 0 {
		o.Poll = 2 * time.Second
	}
	if o.Maintenance <= 0 {
		o.Maintenance = 30 * time.Second
	}
	if o.BackoffBase <= 0 {
		o.BackoffBase = 30 * time.Second
	}
	if o.BackoffMax <= 0 {
		o.BackoffMax = 15 * time.Minute
	}
}

// Runner claims jobs and runs them through a Processor with heartbeats, retries and graceful shutdown.
type Runner struct {
	store *Store
	proc  Processor
	opt   Options
	log   *slog.Logger
}

// NewRunner returns a Runner.
func NewRunner(store *Store, proc Processor, opt Options, log *slog.Logger) *Runner {
	opt.defaults()
	if log == nil {
		log = slog.Default()
	}
	return &Runner{store: store, proc: proc, opt: opt, log: log.With("worker", opt.WorkerID)}
}

// Run blocks until ctx is cancelled (shutdown). Running jobs then get ShutdownGrace to finish; whatever is still
// running after that is stopped and released back to the queue without using up an attempt.
func (r *Runner) Run(ctx context.Context) {
	workCtx, stopWork := context.WithCancelCause(context.Background())
	defer stopWork(nil)
	var wg sync.WaitGroup
	for i := 0; i < r.opt.Concurrency; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r.loop(ctx, workCtx) }()
	}
	wg.Add(1)
	go func() { defer wg.Done(); r.maintain(ctx) }()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		select {
		case <-done:
		case <-time.After(r.opt.ShutdownGrace):
			r.log.Warn("shutdown grace expired, stopping running jobs")
			stopWork(errShutdown)
			<-done
		}
	}
}

var (
	errShutdown        = errors.New("worker shutting down")
	errCancelRequested = errors.New("cancel requested")
)

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (r *Runner) loop(ctx, workCtx context.Context) {
	for ctx.Err() == nil {
		job, err := r.store.Claim(ctx, r.opt.WorkerID, r.opt.Types)
		if err != nil {
			if ctx.Err() == nil {
				r.log.Error("claim failed", "err", err)
			}
			sleep(ctx, r.opt.Poll)
			continue
		}
		if job == nil {
			sleep(ctx, r.opt.Poll)
			continue
		}
		r.run(workCtx, *job)
	}
}

func (r *Runner) maintain(ctx context.Context) {
	for {
		if n, f, err := r.store.RecoverStale(ctx, r.opt.Lease, 100); err != nil {
			if ctx.Err() != nil {
				return
			}
			r.log.Error("stale recovery failed", "err", err)
		} else if n+f > 0 {
			r.log.Warn("recovered stale jobs", "requeued", n, "failed", f)
		}
		if f, s, err := r.store.FailOrphans(ctx); err != nil {
			if ctx.Err() == nil {
				r.log.Error("orphan cleanup failed", "err", err)
			}
		} else if f+s > 0 {
			r.log.Warn("orphaned jobs", "failed", f, "stop_requested", s)
		}
		if !sleep(ctx, r.opt.Maintenance) {
			return
		}
	}
}

type progress struct {
	mu    sync.Mutex
	stage string
	pct   int
}

func (p *progress) Report(stage string, percent int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if stage != "" {
		p.stage = stage
	}
	p.pct = max(p.pct, min(100, max(0, percent)))
}
func (p *progress) get() (string, int) { p.mu.Lock(); defer p.mu.Unlock(); return p.stage, p.pct }

// run executes one claimed job. workCtx is cancelled only when the shutdown grace has expired.
func (r *Runner) run(workCtx context.Context, job Job) {
	log := r.log.With("job", job.ID, "video", job.VideoID, "type", job.Type, "attempt", job.Attempts)
	ctx, cancel := context.WithCancelCause(workCtx)
	defer cancel(nil)
	if job.CancelRequestedAt != nil {
		cancel(errCancelRequested)
	}
	prog := &progress{}

	hbDone := make(chan struct{})
	go func() { // lease heartbeat
		defer close(hbDone)
		t := time.NewTicker(r.opt.Heartbeat)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				stage, pct := prog.get()
				hctx, hc := context.WithTimeout(context.WithoutCancel(ctx), r.opt.Heartbeat)
				c, err := r.store.Heartbeat(hctx, job.ID, r.opt.WorkerID, stage, pct)
				hc()
				switch {
				case errors.Is(err, ErrLeaseLost):
					log.Warn("lease lost, stopping")
					cancel(ErrLeaseLost)
					return
				case err != nil:
					log.Error("heartbeat failed", "err", err) // transient; the lease is still valid until it runs out
				case c:
					cancel(errCancelRequested)
					return
				}
			}
		}
	}()

	err := r.safeProcess(ctx, job, prog)
	cause := context.Cause(ctx)
	cancel(nil)
	<-hbDone

	// Final writes must not depend on the job context (it may be cancelled) nor on the shutdown signal.
	fctx, fc := context.WithTimeout(context.Background(), 30*time.Second)
	defer fc()
	switch {
	case errors.Is(cause, ErrLeaseLost):
		log.Warn("job abandoned: lease lost")
	case err == nil && !errors.Is(cause, errCancelRequested):
		if e := r.store.Complete(fctx, job.ID, r.opt.WorkerID); e != nil {
			log.Error("complete failed", "err", e)
		} else {
			log.Info("job completed")
		}
	case errors.Is(cause, errCancelRequested):
		st, e := r.store.Fail(fctx, job.ID, r.opt.WorkerID, CodeCancelled, true, 0)
		log.Info("job cancelled", "status", st, "err", e)
	case errors.Is(cause, errShutdown):
		st, e := r.store.Release(fctx, job.ID, r.opt.WorkerID)
		log.Info("job released on shutdown", "status", st, "err", e)
	default:
		var perm *PermanentError
		permanent, code := false, CodeInternal
		if errors.As(err, &perm) {
			permanent, code = true, perm.Code
		}
		delay := Backoff(job.Attempts, r.opt.BackoffBase, r.opt.BackoffMax, r.opt.Rand)
		st, e := r.store.Fail(fctx, job.ID, r.opt.WorkerID, code, permanent, delay)
		log.Warn("job failed", "status", st, "code", code, "permanent", permanent, "retry_in", delay.String(), "err", err, "store_err", e)
	}
}

func (r *Runner) safeProcess(ctx context.Context, job Job, p Progress) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			r.log.Error("processor panic", "job", job.ID, "panic", fmt.Sprint(rec), "stack", string(debug.Stack()))
			err = fmt.Errorf("processor panic: %v", rec)
		}
	}()
	return r.proc.Process(ctx, job, p)
}

// NoopProcessor sleeps and succeeds. It exists to drive the state machine in tests and in a dry run; it does NOT
// process any video.
type NoopProcessor struct{ Duration time.Duration }

func (n NoopProcessor) Process(ctx context.Context, _ Job, p Progress) error {
	p.Report("noop", 50)
	if !sleep(ctx, n.Duration) {
		return context.Cause(ctx)
	}
	return nil
}
