// Package stream serves HLS objects (B7). A request is
//
//	GET /stream/<exp>.<sig>/hls/<videoId>/master.m3u8
//
// The token is issued only by GET /api/v1/videos/{id} (after the age gate and visibility rules) and
// is the first path segment, so every relative URL inside a playlist (renditions, segments) inherits
// it with no playlist rewriting. This handler verifies the token, asks the database whether the
// video may still be served (so a moderation takedown stops playback within seconds, not at token
// expiry), and copies the object from private storage. Nothing here is cacheable by a shared cache:
// responses are `private`.
//
// Behind a CDN you can instead verify the same HMAC at the edge (see internal/playback); this handler
// is the origin and the self-hosted path.
package stream

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/redxtube/api/internal/playback"
	"github.com/redxtube/api/internal/storage"
)

const (
	defaultGateTTL = 15 * time.Second
	maxGated       = 20000
)

// Store is the part of storage.API the stream needs.
type Store interface {
	Head(ctx context.Context, key string) (storage.ObjectInfo, error)
	Download(ctx context.Context, key string, w io.Writer) (int64, error)
}

// Gate reports whether a video may be streamed right now (not removed, not deleted, finished).
type Gate func(ctx context.Context, videoID string) (bool, error)

type gated struct {
	ok  bool
	exp time.Time
}

type Handler struct {
	key   []byte
	store Store
	gate  Gate
	ttl   time.Duration
	log   *slog.Logger
	now   func() time.Time

	mu    sync.Mutex
	cache map[string]gated
}

func New(key []byte, store Store, gate Gate, log *slog.Logger) *Handler {
	return &Handler{key: key, store: store, gate: gate, ttl: defaultGateTTL, log: log, now: time.Now, cache: map[string]gated{}}
}

// WithGateTTL sets how long a "may be served" answer is cached (default 15 s): this is also the
// worst-case delay between a moderation takedown and playback stopping.
func (h *Handler) WithGateTTL(d time.Duration) *Handler { h.ttl = d; return h }

// servable caches the gate for a few seconds: a player fetches hundreds of segments per video.
func (h *Handler) servable(ctx context.Context, videoID string) (bool, error) {
	now := h.now()
	h.mu.Lock()
	if g, ok := h.cache[videoID]; ok && now.Before(g.exp) {
		h.mu.Unlock()
		return g.ok, nil
	}
	h.mu.Unlock()
	ok, err := h.gate(ctx, videoID)
	if err != nil {
		return false, err
	}
	h.mu.Lock()
	if len(h.cache) >= maxGated {
		h.cache = map[string]gated{} // bounded: a flood of ids cannot grow memory
	}
	h.cache[videoID] = gated{ok: ok, exp: now.Add(h.ttl)}
	h.mu.Unlock()
	return ok, nil
}

func contentType(key string) (ct, cache string) {
	if strings.HasSuffix(key, ".m3u8") {
		return "application/vnd.apple.mpegurl", "private, max-age=60"
	}
	return "video/mp2t", "private, max-age=3600"
}

func deny(w http.ResponseWriter, code int) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = io.WriteString(w, http.StatusText(code))
}

// lazyWriter sets the response headers just before the first body byte, so a missing object can
// still become a clean 404.
type lazyWriter struct {
	w       http.ResponseWriter
	prepare func()
	started bool
}

func (l *lazyWriter) Write(p []byte) (int, error) {
	if !l.started {
		l.started = true
		l.prepare()
	}
	return l.w.Write(p)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD, OPTIONS")
		deny(w, http.StatusMethodNotAllowed)
		return
	}
	token, key := r.PathValue("token"), r.PathValue("key")
	videoID, err := playback.Verify(h.key, token, key, h.now())
	if err != nil {
		deny(w, http.StatusForbidden) // bad, expired or foreign token; also any non-HLS key
		return
	}
	ok, err := h.servable(r.Context(), videoID)
	if err != nil {
		h.log.ErrorContext(r.Context(), "stream gate failed", "err", err.Error())
		deny(w, http.StatusBadGateway)
		return
	}
	if !ok {
		deny(w, http.StatusNotFound)
		return
	}
	ct, cache := contentType(key)
	set := func(size int64) {
		hd := w.Header()
		hd.Set("Content-Type", ct)
		hd.Set("Cache-Control", cache)
		hd.Set("X-Content-Type-Options", "nosniff")
		if size >= 0 {
			hd.Set("Content-Length", itoa(size))
		}
	}
	if r.Method == http.MethodHead {
		info, err := h.store.Head(r.Context(), key)
		if err != nil {
			h.storageFailure(w, r, key, err)
			return
		}
		set(info.Size)
		w.WriteHeader(http.StatusOK)
		return
	}
	lw := &lazyWriter{w: w, prepare: func() { set(-1) }}
	if _, err := h.store.Download(r.Context(), key, lw); err != nil {
		if lw.started {
			if r.Context().Err() == nil { // not just the player going away
				h.log.WarnContext(r.Context(), "stream interrupted", "key", key, "err", err.Error())
			}
			return
		}
		h.storageFailure(w, r, key, err)
		return
	}
	if !lw.started { // empty object
		set(0)
		w.WriteHeader(http.StatusOK)
	}
}

func (h *Handler) storageFailure(w http.ResponseWriter, r *http.Request, key string, err error) {
	if errors.Is(err, storage.ErrNotFound) {
		deny(w, http.StatusNotFound)
		return
	}
	h.log.ErrorContext(r.Context(), "stream storage failed", "key", key, "err", err.Error())
	deny(w, http.StatusBadGateway)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
