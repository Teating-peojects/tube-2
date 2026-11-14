package stream

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redxtube/api/internal/playback"
	"github.com/redxtube/api/internal/storage"
)

const (
	vid   = "0190aaaa-bbbb-7ccc-8ddd-eeeeeeeeeeee"
	other = "0190aaaa-bbbb-7ccc-8ddd-111111111111"
	base  = "https://stream.test/stream"
)

var signKey = bytes.Repeat([]byte("k"), 32)

type mem map[string][]byte

func (m mem) Head(_ context.Context, key string) (storage.ObjectInfo, error) {
	b, ok := m[key]
	if !ok {
		return storage.ObjectInfo{}, storage.ErrNotFound
	}
	return storage.ObjectInfo{Size: int64(len(b))}, nil
}
func (m mem) Download(_ context.Context, key string, w io.Writer) (int64, error) {
	b, ok := m[key]
	if !ok {
		return 0, storage.ErrNotFound
	}
	n, err := w.Write(b)
	return int64(n), err
}

type fixture struct {
	h     *Handler
	sg    *playback.Signer
	gates atomic.Int32
	live  atomic.Bool
	now   time.Time
	mux   *http.ServeMux
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{now: time.Unix(1_800_000_000, 0)}
	f.live.Store(true)
	store := mem{
		"hls/" + vid + "/master.m3u8":       []byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=928000,RESOLUTION=640x360\n360p/index.m3u8\n"),
		"hls/" + vid + "/360p/index.m3u8":   []byte("#EXTM3U\n#EXTINF:6.0,\nseg_00000.ts\n#EXT-X-ENDLIST\n"),
		"hls/" + vid + "/360p/seg_00000.ts": []byte("TSDATA"),
		"uploads/" + vid + "/original.mp4":  []byte("SECRET ORIGINAL"),
		"hls/" + other + "/master.m3u8":     []byte("#EXTM3U\n"),
	}
	gate := func(_ context.Context, id string) (bool, error) {
		f.gates.Add(1)
		if id == "boom" {
			return false, errors.New("db down")
		}
		return f.live.Load(), nil
	}
	f.h = New(signKey, store, gate, slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.h.now = func() time.Time { return f.now }
	sg, err := playback.NewSigner(signKey, base, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	f.sg = sg
	f.mux = http.NewServeMux()
	f.mux.Handle("GET /stream/{token}/{key...}", f.h)
	return f
}

// signer clock follows the fixture clock via a fresh signer per call
func (f *fixture) url(t *testing.T, id, key string) string {
	sg, _ := playback.NewSigner(signKey, base, time.Hour)
	// Signer uses time.Now: tokens are valid 1h from the real clock; align the handler clock with it.
	f.h.now = time.Now
	u := sg.URL(id, key)
	if u == "" {
		t.Fatalf("no url for %s %s", id, key)
	}
	return u
}

func (f *fixture) do(method, rawURL string) *httptest.ResponseRecorder {
	u, _ := url.Parse(rawURL)
	rr := httptest.NewRecorder()
	f.mux.ServeHTTP(rr, httptest.NewRequest(method, u.Path, nil))
	return rr
}

func TestServesPlaylistsAndSegments(t *testing.T) {
	f := newFixture(t)
	master := f.url(t, vid, "hls/"+vid+"/master.m3u8")
	rr := f.do("GET", master)
	if rr.Code != 200 || rr.Header().Get("Content-Type") != "application/vnd.apple.mpegurl" || !strings.HasPrefix(rr.Body.String(), "#EXTM3U") {
		t.Fatalf("master: %d %v %q", rr.Code, rr.Header(), rr.Body.String())
	}
	if cc := rr.Header().Get("Cache-Control"); !strings.HasPrefix(cc, "private") {
		t.Errorf("a tokenised URL must never be cacheable by shared caches: %q", cc)
	}
	// follow the relative URLs exactly like a player: they must inherit the token
	mu, _ := url.Parse(master)
	var rend string
	for _, l := range strings.Split(rr.Body.String(), "\n") {
		if strings.HasSuffix(l, ".m3u8") {
			rend = l
		}
	}
	ru := mu.ResolveReference(&url.URL{Path: rend})
	rr = f.do("GET", ru.String())
	if rr.Code != 200 {
		t.Fatalf("rendition via relative url: %d", rr.Code)
	}
	var seg string
	for _, l := range strings.Split(rr.Body.String(), "\n") {
		if strings.HasSuffix(l, ".ts") {
			seg = l
		}
	}
	su := ru.ResolveReference(&url.URL{Path: seg})
	rr = f.do("GET", su.String())
	if rr.Code != 200 || rr.Header().Get("Content-Type") != "video/mp2t" || rr.Body.String() != "TSDATA" {
		t.Fatalf("segment: %d %v %q", rr.Code, rr.Header(), rr.Body.String())
	}
	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("nosniff missing")
	}
	// HEAD reports the size without a body
	rr = f.do("HEAD", su.String())
	if rr.Code != 200 || rr.Header().Get("Content-Length") != "6" || rr.Body.Len() != 0 {
		t.Errorf("HEAD: %d %v", rr.Code, rr.Header())
	}
}

func TestRejectsBadTokens(t *testing.T) {
	f := newFixture(t)
	good := f.url(t, vid, "hls/"+vid+"/master.m3u8")
	gu, _ := url.Parse(good)
	parts := strings.SplitN(strings.TrimPrefix(gu.Path, "/stream/"), "/", 2)
	token := parts[0]

	cases := map[string]string{
		"tampered signature":          "/stream/" + token[:len(token)-1] + "0/hls/" + vid + "/master.m3u8",
		"no signature":                "/stream/1900000000./hls/" + vid + "/master.m3u8",
		"garbage":                     "/stream/x/hls/" + vid + "/master.m3u8",
		"token of video A on video B": "/stream/" + token + "/hls/" + other + "/master.m3u8",
		"original upload":             "/stream/" + token + "/uploads/" + vid + "/original.mp4",
		"traversal":                   "/stream/" + token + "/hls/" + vid + "/../../uploads/" + vid + "/original.mp4",
		"thumbs prefix":               "/stream/" + token + "/thumbs/" + vid + "/x.jpg",
	}
	for name, p := range cases {
		rr := httptest.NewRecorder()
		f.mux.ServeHTTP(rr, httptest.NewRequest("GET", p, nil))
		if rr.Code != 403 && rr.Code != 404 && rr.Code != 301 {
			t.Errorf("%s: got %d, want 403/404", name, rr.Code)
		}
		if strings.Contains(rr.Body.String(), "SECRET") || strings.Contains(rr.Body.String(), "EXTM3U") {
			t.Errorf("%s: leaked content", name)
		}
	}
	// expired
	f.h.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if rr := f.do("GET", good); rr.Code != 403 {
		t.Errorf("expired token: %d", rr.Code)
	}
	// wrong method
	f.h.now = time.Now
	rr := httptest.NewRecorder()
	f.mux.ServeHTTP(rr, httptest.NewRequest("POST", gu.Path, nil))
	if rr.Code == 200 {
		t.Error("POST accepted")
	}
}

func TestTakedownStopsPlaybackWithinTheGateTTL(t *testing.T) {
	f := newFixture(t)
	u := f.url(t, vid, "hls/"+vid+"/360p/seg_00000.ts")
	if rr := f.do("GET", u); rr.Code != 200 {
		t.Fatalf("%d", rr.Code)
	}
	// many segment requests, one database check
	for i := 0; i < 20; i++ {
		f.do("GET", u)
	}
	if n := f.gates.Load(); n != 1 {
		t.Errorf("gate hit the database %d times for 21 requests", n)
	}
	// moderation removes the video; after the TTL the same valid token stops working
	f.live.Store(false)
	clock := time.Now().Add(defaultGateTTL + time.Second)
	f.h.now = func() time.Time { return clock }
	if rr := f.do("GET", u); rr.Code != 404 {
		t.Errorf("removed video still served: %d", rr.Code)
	}
}

func TestGateErrorsAndMissingObjects(t *testing.T) {
	f := newFixture(t)
	if rr := f.do("GET", f.url(t, vid, "hls/"+vid+"/480p/index.m3u8")); rr.Code != 404 {
		t.Errorf("missing object: %d", rr.Code)
	}
	if rr := f.do("HEAD", f.url(t, vid, "hls/"+vid+"/480p/index.m3u8")); rr.Code != 404 {
		t.Errorf("missing object HEAD: %d", rr.Code)
	}
	// database failure: fail closed, never serve
	g := New(signKey, mem{}, func(context.Context, string) (bool, error) { return false, errors.New("down") }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	mux := http.NewServeMux()
	mux.Handle("GET /stream/{token}/{key...}", g)
	u, _ := url.Parse(f.url(t, vid, "hls/"+vid+"/master.m3u8"))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", u.Path, nil))
	if rr.Code != 502 {
		t.Errorf("gate error: %d", rr.Code)
	}
}
