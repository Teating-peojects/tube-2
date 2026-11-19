// Command worker is the media-processing worker. B6.2 only ships the job lifecycle: the only processor is the
// no-op one, which marks jobs COMPLETED WITHOUT processing any video. It therefore refuses to start unless
// WORKER_PROCESSOR=noop is set explicitly; real processors arrive in B6.3-B6.6.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redxtube/api/internal/processing"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "worker:", err)
		os.Exit(1)
	}
}

func envDur(name string, def, lo, hi time.Duration) (time.Duration, error) {
	v := os.Getenv(name)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < lo || d > hi {
		return 0, fmt.Errorf("%s must be a duration between %s and %s (got %q)", name, lo, hi, v)
	}
	return d, nil
}

func run() error {
	dsn := os.Getenv("WORKER_DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("WORKER_DATABASE_URL is required (connect as the redxtube_worker role, not redxtube_app)")
	}
	var proc processing.Processor
	switch p := os.Getenv("WORKER_PROCESSOR"); p {
	case "noop":
		d, err := envDur("WORKER_NOOP_DURATION", 2*time.Second, 0, time.Hour)
		if err != nil {
			return err
		}
		proc = processing.NoopProcessor{Duration: d}
	default:
		return fmt.Errorf("WORKER_PROCESSOR must be set; only \"noop\" exists yet and it does not process videos (got %q)", p)
	}
	conc := 1
	if v := os.Getenv("WORKER_CONCURRENCY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 32 {
			return fmt.Errorf("WORKER_CONCURRENCY must be 1-32 (got %q)", v)
		}
		conc = n
	}
	lease, err := envDur("WORKER_LEASE", 2*time.Minute, 10*time.Second, time.Hour)
	if err != nil {
		return err
	}
	poll, err := envDur("WORKER_POLL_INTERVAL", 2*time.Second, 100*time.Millisecond, time.Minute)
	if err != nil {
		return err
	}
	grace, err := envDur("WORKER_SHUTDOWN_GRACE", 30*time.Second, 0, 30*time.Minute)
	if err != nil {
		return err
	}
	host, _ := os.Hostname()
	id := fmt.Sprintf("%s-%d", host, os.Getpid())

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("database: %w", err)
	}

	r := processing.NewRunner(processing.NewStore(pool), proc, processing.Options{
		WorkerID: id, Types: []string{"METADATA", "HLS"}, Concurrency: conc, Lease: lease, Poll: poll, ShutdownGrace: grace,
	}, log)
	log.Info("worker started", "id", id, "concurrency", conc, "lease", lease.String(), "processor", "noop")
	r.Run(ctx)
	log.Info("worker stopped")
	return nil
}
