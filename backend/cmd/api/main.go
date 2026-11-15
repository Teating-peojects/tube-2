// Command api is the REDXTUBE HTTP API server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redxtube/api/internal/auth"
	"github.com/redxtube/api/internal/cache"
	"github.com/redxtube/api/internal/config"
	"github.com/redxtube/api/internal/db"
	"github.com/redxtube/api/internal/handler"
	"github.com/redxtube/api/internal/httpx"
	"github.com/redxtube/api/internal/mail"
	"github.com/redxtube/api/internal/media"
	"github.com/redxtube/api/internal/metrics"
	"github.com/redxtube/api/internal/middleware"
	"github.com/redxtube/api/internal/playback"
	"github.com/redxtube/api/internal/ratelimit"
	"github.com/redxtube/api/internal/realtime"
	"github.com/redxtube/api/internal/repository"
	"github.com/redxtube/api/internal/server"
	"github.com/redxtube/api/internal/service"
	"github.com/redxtube/api/internal/storage"
	"github.com/redxtube/api/internal/stream"
	"github.com/redxtube/api/internal/ws"
)

func main() {
	// `api healthcheck` is used by Docker HEALTHCHECK (distroless has no curl).
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return fmt.Errorf("invalid configuration:\n%w", err)
	}

	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	var h slog.Handler = slog.NewJSONHandler(os.Stdout, opts)
	if cfg.Env == "development" {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	log := slog.New(h)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()

	rdb, err := cache.Connect(ctx, cfg.RedisURL)
	if err != nil {
		return err
	}
	defer rdb.Close()

	if cfg.AuthSecretEphemeral {
		log.Warn("AUTH_SECRET not set: using a random per-process secret (development only); age cookies stop working on restart")
	}
	if cfg.Mail.Driver == "log" {
		log.Warn("MAIL_DRIVER=log: verification codes and reset links are printed to the log (development only)")
	}

	var mailer mail.Mailer
	switch cfg.Mail.Driver {
	case "resend":
		mailer = mail.NewResend(cfg.Mail.ResendAPIKey, cfg.Mail.From)
	case "log":
		mailer = mail.Log{L: log}
	default:
		mailer = mail.Discard{}
	}
	asyncMail := mail.NewAsync(mailer, log, 2, 256, []time.Duration{time.Second, 4 * time.Second, 15 * time.Second})

	urls := media.New(cfg.CDNURL)
	limiter := ratelimit.New(rdb, "rl:")
	authSvc := service.NewAuth(repository.NewAuth(pool), auth.NewHasher(8), limiter, asyncMail, cfg.AuthSecret, urls, cfg.AppURL, log)
	socialRepo := repository.NewSocial(pool)
	catalogRepo := repository.NewCatalog(pool)
	catalogSvc := service.NewCatalog(catalogRepo, urls).WithViewerData(socialRepo)
	socialSvc := service.NewSocial(socialRepo, catalogSvc, limiter, auth.DeriveKeys(cfg.AuthSecret).IP, log)
	// A nil storage.API (not a typed nil) is what switches uploads off.
	var store storage.API
	if cfg.Storage.Enabled() {
		c, err := storage.New(storage.Config{Endpoint: cfg.Storage.Endpoint, PublicEndpoint: cfg.Storage.PublicEndpoint, Region: cfg.Storage.Region,
			AccessKey: cfg.Storage.AccessKey, SecretKey: cfg.Storage.SecretKey, Bucket: cfg.Storage.Bucket, PathStyle: cfg.Storage.PathStyle})
		if err != nil {
			return fmt.Errorf("storage: %w", err)
		}
		store = c
	} else {
		log.Warn("STORAGE_* not set: upload endpoints answer 503 uploads_disabled")
	}
	// B7: signed HLS. Without STREAM_BASE_URL the plain CDN URL is used (development only).
	var streamHandler http.Handler
	if cfg.StreamBaseURL != "" {
		key := cfg.PlaybackKey
		if len(key) == 0 {
			key = auth.DeriveKeys(cfg.AuthSecret).Playback
		}
		signer, err := playback.NewSigner(key, cfg.StreamBaseURL, playback.DefaultTTL)
		if err != nil {
			return fmt.Errorf("playback: %w", err)
		}
		catalogSvc = catalogSvc.WithPlayback(signer)
		if store != nil {
			streamHandler = stream.New(key, store, catalogRepo.VideoServable, log)
		} else {
			log.Warn("STREAM_BASE_URL is set but STORAGE_* is not: stream URLs are issued but nothing serves them")
		}
	}
	counters := metrics.New()
	optional := map[string]handler.Pinger{}
	if p, ok := store.(handler.Pinger); ok { // object storage is optional for readiness: only uploads depend on it
		optional["storage"] = p
	}
	bus := realtime.NewBus(rdb, log)
	hub := ws.NewHub()
	go bus.Run(ctx, hub)
	ipKey := auth.DeriveKeys(cfg.AuthSecret).IP
	uploadSvc := service.NewUpload(repository.NewUpload(pool), store, limiter, ipKey, urls, bus, log).WithLimits(service.UploadLimits{
		MaxBytes: cfg.Upload.MaxBytes, PartSize: cfg.Upload.PartSize, MaxOpen: cfg.Upload.MaxOpen, SessionTTL: cfg.Upload.SessionTTL, MaxRetries: cfg.Upload.MaxRetries,
		ExpiredRetention: cfg.Upload.ExpiredRetention, CleanupDelay: cfg.Upload.CleanupDelay}).WithMetrics(counters)
	notifRepo := repository.NewNotifications(pool)
	notifSvc := service.NewNotifications(notifRepo, limiter, log)
	adminSvc := service.NewAdmin(repository.NewAdmin(pool), limiter, ipKey, log)
	studioSvc := service.NewStudio(repository.NewStudio(pool), limiter, ipKey, urls, log)
	cookies := middleware.Cookies{Session: cfg.SessionCookieName, Age: "xh_age", AgeMark: "xh_age_ok", Domain: cfg.CookieDomain, Secure: cfg.CookieSecure}

	var metricsMW httpx.Middleware
	if cfg.MetricsAddr != "" {
		reg := metrics.NewRegistry()
		metricsMW = reg.HTTP()
		reg.AddCollector(func(w *metrics.Writer) {
			st := pool.Stat()
			w.Gauge("db_pool_acquired_conns", "Connections in use.", float64(st.AcquiredConns()))
			w.Gauge("db_pool_idle_conns", "Idle connections.", float64(st.IdleConns()))
			w.Gauge("db_pool_total_conns", "Open connections.", float64(st.TotalConns()))
			w.Gauge("db_pool_max_conns", "Pool size limit.", float64(st.MaxConns()))
		})
		reg.AddCounters("app_events_total", "Application event counters (uploads, cleanup).", counters)
		mux := http.NewServeMux()
		mux.Handle("GET /metrics", reg.Handler())
		ms := &http.Server{Addr: cfg.MetricsAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if err := ms.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("metrics server", "err", err.Error())
			}
		}()
		defer ms.Close()
		log.Info("metrics listening (keep this port internal)", "addr", cfg.MetricsAddr)
	}

	srv := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: server.New(server.Deps{
			Cfg:     cfg,
			Log:     log,
			Auth:    &handler.Auth{Svc: authSvc, Cookies: cookies, TrustProxy: cfg.TrustProxyHeaders},
			Authn:   middleware.Authenticate(authSvc, cookies),
			Catalog: &handler.Catalog{Svc: catalogSvc},
			Stream:  streamHandler,
			Metrics: metricsMW,
			Social:  &handler.Social{Svc: socialSvc, TrustProxy: cfg.TrustProxyHeaders},
			Upload:  &handler.Upload{Svc: uploadSvc, TrustProxy: cfg.TrustProxyHeaders},

			Notifications: &handler.Notifications{Svc: notifSvc},
			Studio:        &handler.Studio{Svc: studioSvc, TrustProxy: cfg.TrustProxyHeaders},
			Admin:         &handler.Admin{Svc: adminSvc, TrustProxy: cfg.TrustProxyHeaders},
			Realtime:      &handler.Realtime{Hub: hub, Owner: notifRepo, Origins: cfg.CORSOrigins, Log: log},
			Checks: map[string]handler.Pinger{
				"postgres": pool,
				"redis":    cache.Pinger{C: rdb},
			},
			Optional: optional,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	go janitor(ctx, log, uploadSvc, adminSvc, counters)

	errCh := make(chan error, 1)
	go func() {
		log.Info("api listening", "addr", srv.Addr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
	}

	sctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	err = srv.Shutdown(sctx)
	if cerr := asyncMail.Close(sctx); cerr != nil { // let queued emails finish
		log.Warn("mail queue not fully drained", "err", cerr.Error())
	}
	return err
}

func healthcheck() int {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + port + "/health")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

// janitor closes expired upload sessions (freeing their storage) and lifts finished suspensions. Every API
// instance runs it; the database hands each expired row to exactly one of them.
func janitor(ctx context.Context, log *slog.Logger, up *service.Upload, adm *service.Admin, counters *metrics.Counters) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for tick := 1; ; tick++ {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			jctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			if n, err := up.ExpireStale(jctx); err != nil {
				log.Warn("upload janitor failed", "err", err.Error())
			} else if n > 0 {
				log.Info("expired uploads closed", "count", n)
			}
			if r, err := up.SweepStorage(jctx); err != nil {
				log.Warn("upload storage sweep failed", "err", err.Error())
			} else if r.Finalized+r.Cleaned+r.Failed > 0 {
				log.Info("upload storage sweep", "finalized", r.Finalized, "cleaned", r.Cleaned, "failed", r.Failed)
			}
			if n, err := adm.ExpireSuspensions(jctx); err != nil {
				log.Warn("suspension expiry failed", "err", err.Error())
			} else if n > 0 {
				log.Info("suspensions lifted", "count", n)
			}
			cancel()
			if tick%10 == 0 { // every ten minutes: the counters since start, as one structured log line
				logCounters(log, counters)
			}
		}
	}
}

// logCounters writes the in-process counters as a single log line (there is no scrape endpoint yet).
func logCounters(log *slog.Logger, c *metrics.Counters) {
	values, names := c.Snapshot()
	if len(names) == 0 {
		return
	}
	args := make([]any, 0, 2*len(names))
	for _, n := range names {
		args = append(args, n, values[n])
	}
	log.Info("upload counters since start", args...)
}
