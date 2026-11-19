// Command worker is the media-processing worker. WORKER_PROCESSOR selects what it does and must be set explicitly:
//
//	metadata  the METADATA job (B6.3): download the original, inspect it with ffprobe, check the input policy, record
//	          the technical metadata and queue the HLS job. Needs STORAGE_* and ffprobe.
//	noop      completes jobs WITHOUT processing any video; only for testing the job lifecycle (B6.2).
//
// The transcoding processors (HLS, thumbnails) arrive in B6.4-B6.6; until then HLS jobs stay queued.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redxtube/api/internal/processing"
	"github.com/redxtube/api/internal/processing/inspect"
	"github.com/redxtube/api/internal/storage"
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
	kind := os.Getenv("WORKER_PROCESSOR")
	switch kind {
	case "noop", "metadata":
	default:
		return fmt.Errorf("WORKER_PROCESSOR must be \"metadata\" or \"noop\" (got %q); noop does not process videos", kind)
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

	var proc processing.Processor
	types := []string{"METADATA", "HLS"}
	switch kind {
	case "noop":
		d, err := envDur("WORKER_NOOP_DURATION", 2*time.Second, 0, time.Hour)
		if err != nil {
			return err
		}
		proc = processing.NoopProcessor{Duration: d}
	case "metadata":
		types = []string{"METADATA"}
		mp, err := metadataProcessor(processing.NewStore(pool), log)
		if err != nil {
			return err
		}
		proc = mp
	}
	r := processing.NewRunner(processing.NewStore(pool), proc, processing.Options{
		WorkerID: id, Types: types, Concurrency: conc, Lease: lease, Poll: poll, ShutdownGrace: grace,
	}, log)
	log.Info("worker started", "id", id, "concurrency", conc, "lease", lease.String(), "processor", kind, "types", strings.Join(types, ","))
	r.Run(ctx)
	log.Info("worker stopped")
	return nil
}

func metadataProcessor(store *processing.Store, log *slog.Logger) (*processing.MetadataProcessor, error) {
	get := os.Getenv
	for _, k := range []string{"STORAGE_ENDPOINT", "STORAGE_ACCESS_KEY", "STORAGE_SECRET_KEY", "STORAGE_BUCKET"} {
		if get(k) == "" {
			return nil, fmt.Errorf("%s is required for WORKER_PROCESSOR=metadata", k)
		}
	}
	endpoint := strings.TrimRight(get("STORAGE_ENDPOINT"), "/")
	region := get("STORAGE_REGION")
	if region == "" {
		region = "us-east-1"
	}
	pathStyle := !strings.Contains(endpoint, ".amazonaws.com")
	switch strings.ToLower(get("STORAGE_PATH_STYLE")) {
	case "true", "1":
		pathStyle = true
	case "false", "0":
		pathStyle = false
	}
	st, err := storage.New(storage.Config{Endpoint: endpoint, Region: region, AccessKey: get("STORAGE_ACCESS_KEY"), SecretKey: get("STORAGE_SECRET_KEY"),
		Bucket: get("STORAGE_BUCKET"), PathStyle: pathStyle})
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}
	pol := inspect.DefaultPolicy()
	if pol.MaxDuration, err = envDur("WORKER_MAX_DURATION", pol.MaxDuration, time.Minute, 24*time.Hour); err != nil {
		return nil, err
	}
	probeTimeout, err := envDur("WORKER_FFPROBE_TIMEOUT", 30*time.Second, time.Second, 10*time.Minute)
	if err != nil {
		return nil, err
	}
	bin := get("WORKER_FFPROBE_PATH")
	if bin == "" {
		bin = "ffprobe"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("ffprobe not found (%s): %w", bin, err)
	}
	return &processing.MetadataProcessor{Store: store, Src: st, Prober: &inspect.Prober{Bin: bin, Timeout: probeTimeout}, Policy: pol, TempDir: get("WORKER_TMPDIR"), Log: log}, nil
}
