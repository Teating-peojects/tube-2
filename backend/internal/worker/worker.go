package worker

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	Concurrency    int           // jobs in parallel (ffmpeg is CPU-bound: 1 per 4+ cores is typical)
	PollInterval   time.Duration // idle sleep between claims
	HeartbeatEvery time.Duration
	StaleAfter     time.Duration // a PROCESSING job without a heartbeat for this long is re-queued
	ShutdownGrace  time.Duration // on SIGTERM, how long running jobs may keep going before they are released
	WorkDir        string
	AliveFile      string // touched while the worker is healthy (container HEALTHCHECK)
}

func (c *Config) defaults() {
	if c.Concurrency < 1 {
		c.Concurrency = 1
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 3 * time.Second
	}
	if c.HeartbeatEvery <= 0 {
		c.HeartbeatEvery = 15 * time.Second
	}
	if c.StaleAfter <= 0 {
		c.StaleAfter = 2 * time.Minute
	}
	if c.ShutdownGrace <= 0 {
		c.ShutdownGrace = 30 * time.Second
	}
}

type Worker struct {
	cfg   Config
	store Store
	pipe  *Pipeline
	log   *slog.Logger
	met   *Metrics
}

func New(cfg Config, store Store, pipe *Pipeline, log *slog.Logger) *Worker {
	cfg.defaults()
	return &Worker{cfg: cfg, store: store, pipe: pipe, log: log}
}

// WithMetrics enables Prometheus instrumentation (optional).
func (w *Worker) WithMetrics(m *Metrics) *Worker { w.met = m; return w }

func (w *Worker) touch() {
	if w.cfg.AliveFile != "" {
		now := time.Now()
		if err := os.Chtimes(w.cfg.AliveFile, now, now); err != nil {
			_ = os.WriteFile(w.cfg.AliveFile, nil, 0o600)
		}
	}
}

// Run blocks until ctx is cancelled and the running jobs have finished or been released.
func (w *Worker) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); w.janitor(ctx) }()
	for i := 0; i < w.cfg.Concurrency; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); w.loop(ctx) }()
	}
	wg.Wait()
}

func (w *Worker) sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d + time.Duration(rand.Int63n(int64(d)/4+1))) // jitter: workers do not poll in lockstep
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func (w *Worker) loop(ctx context.Context) {
	for ctx.Err() == nil {
		w.touch()
		j, err := w.store.Claim(ctx)
		if err != nil {
			if ctx.Err() == nil {
				w.log.Error("claim failed", "err", err.Error())
			}
			w.sleep(ctx, 5*time.Second)
			continue
		}
		if j == nil {
			w.sleep(ctx, w.cfg.PollInterval)
			continue
		}
		w.process(ctx, *j)
	}
}

func (w *Worker) process(parent context.Context, j Job) {
	log := w.log.With("job", j.ID, "video", j.VideoID, "type", j.Type, "attempt", j.Attempts)
	start := time.Now()
	log.Info("job started")

	// The job outlives SIGTERM for ShutdownGrace, then it is cancelled and handed back.
	jctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	defer cancel()
	var lost atomic.Bool
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-parent.Done():
			select {
			case <-time.After(w.cfg.ShutdownGrace):
				cancel()
			case <-stop:
			}
		case <-stop:
		}
	}()
	go func() {
		t := time.NewTicker(w.cfg.HeartbeatEvery)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				w.touch()
				if err := w.store.Heartbeat(context.WithoutCancel(jctx), j); errors.Is(err, ErrLeaseLost) {
					lost.Store(true)
					cancel()
					return
				} else if err != nil {
					log.Warn("heartbeat failed", "err", err.Error())
				}
			}
		}
	}()

	err := w.pipe.Run(jctx, j)
	bg := context.WithoutCancel(parent)
	switch {
	case err == nil:
		w.met.observe(j, "completed", time.Since(start))
		log.Info("job completed", "ms", time.Since(start).Milliseconds())
	case lost.Load() || errors.Is(err, ErrLeaseLost):
		w.met.observe(j, "lease_lost", time.Since(start))
		log.Warn("job lease lost, result dropped")
	case errors.Is(err, context.Canceled):
		w.met.observe(j, "released", time.Since(start))
		if rerr := w.store.Release(bg, j); rerr != nil {
			log.Warn("release failed", "err", rerr.Error())
		} else {
			log.Info("job released for another worker")
		}
	default:
		je := asJobError(err)
		res, ferr := w.store.Fail(bg, j, je)
		switch {
		case errors.Is(ferr, ErrLeaseLost):
			log.Warn("job lease lost while failing")
		case ferr != nil:
			log.Error("could not record job failure", "err", ferr.Error(), "cause", je.Error())
		case res.Final:
			w.met.observe(j, "failed", time.Since(start))
			log.Error("job failed permanently", "code", je.Code, "err", je.Error())
			if res.CreatorID != "" && w.pipe.Notify != nil {
				w.pipe.Notify(bg, res.CreatorID, j.VideoID)
			}
		default:
			w.met.observe(j, "retry", time.Since(start))
			log.Warn("job failed, will retry", "code", je.Code, "in", backoff(j.Attempts).String(), "err", je.Error())
		}
	}
}

// janitor re-queues jobs whose worker died and deletes work directories nobody needs any more.
func (w *Worker) janitor(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		if n, err := w.store.RequeueStale(ctx, w.cfg.StaleAfter); err != nil {
			if ctx.Err() == nil {
				w.log.Error("requeue stale failed", "err", err.Error())
			}
		} else if n > 0 {
			w.log.Warn("re-queued jobs of dead workers", "count", n)
		}
		w.cleanWorkDir()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (w *Worker) cleanWorkDir() {
	ents, err := os.ReadDir(w.cfg.WorkDir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > 24*time.Hour {
			_ = os.RemoveAll(filepath.Join(w.cfg.WorkDir, e.Name()))
		}
	}
}
