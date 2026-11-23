// Command worker is the media pipeline: it claims jobs from video_processing_jobs and runs
// ffprobe/ffmpeg against uploaded originals (METADATA -> THUMBNAIL -> TRANSCODE -> HLS).
//
// It connects as the least-privilege redxtube_worker role (no access to credentials, sessions or
// identity data) and needs the ffmpeg and ffprobe binaries. Run as many replicas as you have CPUs
// to spare: jobs are claimed with FOR UPDATE SKIP LOCKED, so replicas never collide.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/redxtube/api/internal/cache"
	"github.com/redxtube/api/internal/db"
	"github.com/redxtube/api/internal/metrics"
	"github.com/redxtube/api/internal/realtime"
	"github.com/redxtube/api/internal/storage"
	"github.com/redxtube/api/internal/worker"
)

func main() {
	// `worker healthcheck` is used by the Docker HEALTHCHECK: the loop and the job heartbeat touch the file.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "worker:", err)
		os.Exit(1)
	}
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) (int, error) {
	v := env(k, "")
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", k)
	}
	return n, nil
}

func envDur(k string, def time.Duration) (time.Duration, error) {
	v := env(k, "")
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a duration like 30s or 2m", k)
	}
	return d, nil
}

func healthcheck() int {
	st, err := os.Stat(env("WORKER_ALIVE_FILE", "/tmp/worker.alive"))
	if err != nil || time.Since(st.ModTime()) > 90*time.Second {
		return 1
	}
	return 0
}

func run() error {
	level := slog.LevelInfo
	if err := level.UnmarshalText([]byte(env("LOG_LEVEL", "info"))); err != nil {
		return errors.New("LOG_LEVEL must be debug, info, warn or error")
	}
	var h slog.Handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	if env("APP_ENV", "development") == "development" {
		h = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}
	log := slog.New(h).With("service", "worker")
	slog.SetDefault(log)

	// ----- configuration -----
	dsn := env("DATABASE_URL", "")
	if dsn == "" {
		return errors.New("DATABASE_URL is required (connect as redxtube_worker)")
	}
	if u, err := url.Parse(dsn); err == nil && u.User != nil && u.User.Username() != "redxtube_worker" {
		log.Warn("DATABASE_URL is not the redxtube_worker role: the worker needs no more than that role's privileges", "user", u.User.Username())
	}
	scfg := storage.Config{
		Endpoint: strings.TrimRight(env("STORAGE_ENDPOINT", ""), "/"), PublicEndpoint: strings.TrimRight(env("STORAGE_PUBLIC_ENDPOINT", ""), "/"),
		Region: env("STORAGE_REGION", "us-east-1"), AccessKey: env("STORAGE_ACCESS_KEY", ""),
		SecretKey: env("STORAGE_SECRET_KEY", ""), Bucket: env("STORAGE_BUCKET", ""),
	}
	for k, v := range map[string]string{"STORAGE_ENDPOINT": scfg.Endpoint, "STORAGE_ACCESS_KEY": scfg.AccessKey, "STORAGE_SECRET_KEY": scfg.SecretKey, "STORAGE_BUCKET": scfg.Bucket} {
		if v == "" {
			return fmt.Errorf("%s is required", k)
		}
	}
	scfg.PathStyle = !strings.Contains(scfg.Endpoint, ".amazonaws.com")
	switch strings.ToLower(env("STORAGE_PATH_STYLE", "")) {
	case "":
	case "true", "1":
		scfg.PathStyle = true
	case "false", "0":
		scfg.PathStyle = false
	default:
		return errors.New("STORAGE_PATH_STYLE must be true or false")
	}
	conc, err := envInt("WORKER_CONCURRENCY", 1)
	if err != nil {
		return err
	}
	threads, err := envInt("FFMPEG_THREADS", 0)
	if err != nil {
		return err
	}
	maxConns, err := envInt("DB_MAX_CONNS", conc+3)
	if err != nil {
		return err
	}
	poll, err := envDur("WORKER_POLL_INTERVAL", 3*time.Second)
	if err != nil {
		return err
	}
	stale, err := envDur("WORKER_STALE_AFTER", 2*time.Minute)
	if err != nil {
		return err
	}
	grace, err := envDur("WORKER_SHUTDOWN_GRACE", 30*time.Second)
	if err != nil {
		return err
	}
	policy, profile, retention, err := loadLimits(os.Getenv)
	if err != nil {
		return err
	}
	workDir := env("WORKER_WORKDIR", "/work")
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return fmt.Errorf("WORKER_WORKDIR: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, dsn, int32(maxConns))
	if err != nil {
		return err
	}
	defer pool.Close()
	store, err := storage.New(scfg)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}

	pipe := &worker.Pipeline{
		Store: nil, Storage: store, Log: log, WorkDir: workDir,
		FFmpeg: env("FFMPEG_PATH", "ffmpeg"), FFprobe: env("FFPROBE_PATH", "ffprobe"), Threads: threads,
		Policy: policy, Profile: profile,
	}
	// Realtime is a convenience (the UI polls the upload status too): Redis is optional here.
	if u := env("REDIS_URL", ""); u != "" {
		rdb, err := cache.Connect(ctx, u)
		if err != nil {
			log.Warn("redis unavailable: realtime pushes are off", "err", err.Error())
		} else {
			defer rdb.Close()
			bus := realtime.NewBus(rdb, log)
			pipe.Notify = func(c context.Context, user, video string) {
				bus.Notify(c, user)
				bus.Publish(c, user, realtime.TopicVideoPrefix+video, json.RawMessage(nil))
			}
		}
	}

	if c := env("SCANNER_COMMAND", ""); c != "" {
		sc, err := worker.ParseScannerCommand(c)
		if err != nil {
			return fmt.Errorf("SCANNER_COMMAND: %w", err)
		}
		pipe.Scanner = sc
		log.Info("upload scanner enabled (fail-closed)", "command", sc.Args[0])
	} else if env("APP_ENV", "development") == "production" {
		log.Warn("no SCANNER_COMMAND: uploads are NOT scanned for malware/CSAM; plug a scanner in before opening uploads (docs/worker.md)")
	}

	host, _ := os.Hostname()
	pg := worker.NewPG(pool, fmt.Sprintf("%s-%d", host, os.Getpid()))
	pipe.Store = pg
	w := worker.New(worker.Config{
		Concurrency: conc, PollInterval: poll, StaleAfter: stale, ShutdownGrace: grace, WorkDir: workDir, FailedOutputRetention: retention,
		AliveFile: env("WORKER_ALIVE_FILE", "/tmp/worker.alive"),
	}, pg, pipe, log)

	if addr := env("METRICS_ADDR", ""); addr != "" {
		reg := metrics.NewRegistry()
		wm := worker.NewMetrics(reg, pg, workDir, conc)
		w.WithMetrics(wm)
		pipe.OnTranscoded = wm.Encoded
		mux := http.NewServeMux()
		mux.Handle("GET /metrics", reg.Handler())
		// liveness: the process is up; readiness: the database answers (a worker that cannot reach it cannot claim jobs)
		mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
		mux.HandleFunc("GET /ready", func(rw http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			if err := pool.Ping(ctx); err != nil {
				http.Error(rw, "database unavailable", http.StatusServiceUnavailable)
				return
			}
			rw.WriteHeader(http.StatusOK)
		})
		ms := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if err := ms.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("metrics server", "err", err.Error())
			}
		}()
		defer ms.Close()
		log.Info("metrics listening (keep this port internal)", "addr", addr)
	}

	log.Info("worker started", "concurrency", max(conc, 1), "ffmpeg_threads", threads, "workdir", workDir, "profile", profile.Version)
	w.Run(ctx)
	log.Info("worker stopped")
	return nil
}
