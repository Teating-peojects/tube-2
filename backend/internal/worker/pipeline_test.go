package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/redxtube/api/internal/playback"
	"github.com/redxtube/api/internal/storage"
)

// ---------- fakes ----------

type memStorage struct {
	storage.API
	mu   sync.Mutex
	objs map[string][]byte
	ct   map[string]string
}

func newMem() *memStorage { return &memStorage{objs: map[string][]byte{}, ct: map[string]string{}} }

func (m *memStorage) Download(_ context.Context, key string, w io.Writer) (int64, error) {
	m.mu.Lock()
	b, ok := m.objs[key]
	m.mu.Unlock()
	if !ok {
		return 0, storage.ErrNotFound
	}
	n, err := w.Write(b)
	return int64(n), err
}
func (m *memStorage) Put(_ context.Context, key, ctype, _ string, r io.Reader, _ int64) error {
	b, err := io.ReadAll(r)
	m.mu.Lock()
	m.objs[key], m.ct[key] = b, ctype
	m.mu.Unlock()
	return err
}
func (m *memStorage) PutBytes(ctx context.Context, key, ctype, cc string, b []byte) error {
	return m.Put(ctx, key, ctype, cc, bytes.NewReader(b), int64(len(b)))
}
func (m *memStorage) Head(_ context.Context, key string) (storage.ObjectInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objs[key]
	if !ok {
		return storage.ObjectInfo{}, storage.ErrNotFound
	}
	return storage.ObjectInfo{Size: int64(len(b))}, nil
}
func (m *memStorage) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.objs, key)
	m.mu.Unlock()
	return nil
}
func (m *memStorage) DeletePrefix(_ context.Context, prefix string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for k := range m.objs {
		if strings.HasPrefix(k, prefix) {
			delete(m.objs, k)
			n++
		}
	}
	return n, nil
}
func (m *memStorage) keys(prefix string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for k := range m.objs {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

type fakeStore struct {
	Store
	video    VideoRow
	meta     MetaRow
	blocked  map[string]bool
	variants map[string]bool
	thumbs   []ThumbOut
	primary  int
	master   string
	done     []string
	gotMeta  MetadataResult
	removed  string
	profile  string
}

func (f *fakeStore) Video(context.Context, string) (VideoRow, error) { return f.video, nil }
func (f *fakeStore) Meta(context.Context, string) (MetaRow, error)   { return f.meta, nil }
func (f *fakeStore) SaveMetadata(_ context.Context, _ Job, m MetadataResult) (bool, error) {
	f.gotMeta = m
	if f.blocked[string(m.SHA256)] {
		return true, nil
	}
	p := m.Probe
	f.meta = MetaRow{Width: p.Width, Height: p.Height, FPS: p.FPS, HasAudio: p.HasAudio, Duration: m.Seconds}
	f.done = append(f.done, "METADATA")
	return false, nil
}
func (f *fakeStore) ReplaceThumbnails(_ context.Context, _ Job, it []ThumbOut, primary int) ([]string, error) {
	f.thumbs, f.primary = it, primary
	f.done = append(f.done, "THUMBNAIL")
	return nil, nil
}
func (f *fakeStore) SaveVariant(_ context.Context, _ string, v VariantOut) error {
	if f.variants == nil {
		f.variants = map[string]bool{}
	}
	f.variants[v.R.Name] = true
	return nil
}
func (f *fakeStore) FinishTranscode(context.Context, Job) error {
	f.done = append(f.done, "TRANSCODE")
	return nil
}
func (f *fakeStore) Variants(context.Context, string) ([]string, error) {
	var out []string
	for k := range f.variants {
		out = append(out, k)
	}
	return out, nil
}
func (f *fakeStore) FinishHLS(_ context.Context, _ Job, key, profile string) (string, error) {
	f.master = key
	f.profile = profile
	f.done = append(f.done, "HLS")
	return "creator-1", nil
}

// ---------- helpers ----------

func needFFmpeg(t *testing.T) {
	t.Helper()
	for _, b := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(b); err != nil {
			t.Skipf("%s not installed", b)
		}
	}
}

func makeVideo(t *testing.T, args ...string) []byte {
	t.Helper()
	out := filepath.Join(t.TempDir(), "in.mp4")
	a := append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)
	a = append(a, out)
	if b, err := exec.Command("ffmpeg", a...).CombinedOutput(); err != nil {
		t.Fatalf("make test video: %v: %s", err, b)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newPipe(t *testing.T, st Store, mem *memStorage) *Pipeline {
	return &Pipeline{Store: st, Storage: mem, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		WorkDir: t.TempDir(), FFmpeg: "ffmpeg", FFprobe: "ffprobe", Threads: 2}
}

// ---------- tests ----------

func TestPipelineEndToEnd(t *testing.T) {
	needFFmpeg(t)
	const vid = "0190aaaa-bbbb-7ccc-8ddd-eeeeeeeeeeee"
	src := makeVideo(t, "-f", "lavfi", "-i", "testsrc=size=1280x720:rate=30", "-f", "lavfi", "-i", "sine=frequency=440",
		"-t", "14", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest")
	mem := newMem()
	key := "uploads/" + vid + "/original.mp4"
	mem.objs[key] = src
	st := &fakeStore{video: VideoRow{CreatorID: "creator-1", Status: "PROCESSING", Moderation: "OK", OriginalKey: key, Size: int64(len(src))}}
	p := newPipe(t, st, mem)
	var notified string
	p.Notify = func(_ context.Context, user, video string) { notified = user + "/" + video }
	ctx := context.Background()

	for _, typ := range []string{"METADATA", "THUMBNAIL", "TRANSCODE", "HLS"} {
		if err := p.Run(ctx, Job{ID: "j-" + typ, VideoID: vid, Type: typ}); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
	}
	if strings.Join(st.done, ",") != "METADATA,THUMBNAIL,TRANSCODE,HLS" {
		t.Fatalf("steps %v", st.done)
	}
	// metadata
	sum := sha256.Sum256(src)
	if !bytes.Equal(st.gotMeta.SHA256, sum[:]) {
		t.Error("stored hash is not the SHA-256 of the original")
	}
	if st.gotMeta.VideoType != "SHORT" || st.gotMeta.Seconds != 14 || st.meta.Width != 1280 || !st.meta.HasAudio {
		t.Errorf("metadata %+v / %+v", st.gotMeta, st.meta)
	}
	// thumbnails
	if len(st.thumbs) != 5 || st.primary != 2 {
		t.Fatalf("thumbs %d primary %d", len(st.thumbs), st.primary)
	}
	for _, th := range st.thumbs {
		b := mem.objs[th.Key]
		if len(b) < 1000 || b[0] != 0xFF || b[1] != 0xD8 {
			t.Errorf("thumbnail %s is not a JPEG", th.Key)
		}
		if th.W != 640 || th.H != 360 {
			t.Errorf("thumb dims %dx%d", th.W, th.H)
		}
	}
	// HLS layout, and every object must be reachable through the playback token path
	if st.profile != "h264-aac-hls-v1" {
		t.Errorf("profile version not recorded: %q", st.profile)
	}
	if st.master != "hls/"+vid+"/master.m3u8" || notified != "creator-1/"+vid {
		t.Errorf("master %q notified %q", st.master, notified)
	}
	for _, k := range mem.keys("hls/") {
		if id, ok := playback.ObjectVideo(k); !ok || id != vid {
			t.Errorf("key %s is not servable by the playback path", k)
		}
	}
	for _, res := range []string{"360p", "480p", "720p"} {
		if len(mem.keys("hls/"+vid+"/"+res+"/seg_")) != 3 {
			t.Errorf("%s: want 3 segments, got %v", res, mem.keys("hls/"+vid+"/"+res+"/"))
		}
		if mem.ct["hls/"+vid+"/"+res+"/index.m3u8"] != "application/vnd.apple.mpegurl" {
			t.Errorf("%s playlist content type", res)
		}
	}
	if len(mem.keys("hls/"+vid+"/1080p")) != 0 {
		t.Error("1080p must not exist for a 720p source (no upscaling)")
	}
	// segment boundaries must line up across renditions
	if strings.Join(extinf(mem, vid, "360p"), ",") != strings.Join(extinf(mem, vid, "720p"), ",") {
		t.Errorf("segment durations differ: %v vs %v", extinf(mem, vid, "360p"), extinf(mem, vid, "720p"))
	}
	// the 720p rendition really is 1280x720 H.264 + AAC and plays back
	dir := t.TempDir()
	for _, k := range mem.keys("hls/" + vid + "/720p/") {
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(k)), mem.objs[k], 0o600); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command("ffprobe", "-v", "error", "-protocol_whitelist", "file", "-show_entries", "stream=codec_name,width,height",
		"-of", "csv=p=0", filepath.Join(dir, "index.m3u8")).Output()
	if err != nil {
		t.Fatalf("ffprobe rendition: %v", err)
	}
	if got := string(out); !strings.Contains(got, "h264,1280,720") || !strings.Contains(got, "aac") {
		t.Errorf("rendition streams: %q", got)
	}
	if _, err := os.Stat(p.dir(vid)); !os.IsNotExist(err) {
		t.Error("work directory not cleaned after HLS")
	}
}

var extRe = regexp.MustCompile(`#EXTINF:([0-9.]+)`)

func extinf(m *memStorage, vid, res string) []string {
	var out []string
	for _, g := range extRe.FindAllStringSubmatch(string(m.objs["hls/"+vid+"/"+res+"/index.m3u8"]), -1) {
		out = append(out, g[1])
	}
	return out
}

func TestMetadataRejectsNonVideoAndBlockedHash(t *testing.T) {
	needFFmpeg(t)
	const vid = "0190aaaa-bbbb-7ccc-8ddd-ffffffffffff"
	mem := newMem()
	key := "uploads/" + vid + "/original.mp4"
	ctx := context.Background()

	// 1. text file named .mp4
	junk := []byte("this is not a video at all")
	mem.objs[key] = junk
	st := &fakeStore{video: VideoRow{CreatorID: "c", Status: "PROCESSING", Moderation: "OK", OriginalKey: key, Size: int64(len(junk))}}
	err := newPipe(t, st, mem).Run(ctx, Job{ID: "j", VideoID: vid, Type: "METADATA"})
	var je *JobError
	if !errors.As(err, &je) || !je.Permanent || je.Code != "invalid_file" {
		t.Fatalf("junk file: %v", err)
	}

	// 2. a playlist disguised as .mp4 must never reach the transcoder
	pl := []byte("#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:5,\n/etc/passwd\n#EXT-X-ENDLIST\n")
	mem.objs[key] = pl
	st.video.Size = int64(len(pl))
	err = newPipe(t, st, mem).Run(ctx, Job{ID: "j", VideoID: vid, Type: "METADATA"})
	if !errors.As(err, &je) || !je.Permanent {
		t.Fatalf("disguised playlist accepted: %v", err)
	}

	// 3. valid video whose hash is on the blocklist
	src := makeVideo(t, "-f", "lavfi", "-i", "testsrc=size=320x240:rate=10", "-t", "2", "-c:v", "libx264", "-pix_fmt", "yuv420p")
	mem.objs[key] = src
	sum := sha256.Sum256(src)
	st = &fakeStore{video: VideoRow{CreatorID: "c", Status: "PROCESSING", Moderation: "OK", OriginalKey: key, Size: int64(len(src))},
		blocked: map[string]bool{string(sum[:]): true}}
	err = newPipe(t, st, mem).Run(ctx, Job{ID: "j", VideoID: vid, Type: "METADATA"})
	if !errors.As(err, &je) || je.Code != "blocked_content" || !je.Permanent || !je.Silent {
		t.Fatalf("blocked hash: %v", err)
	}

	// 4. size mismatch between storage and the verified declaration
	st.video.Size++
	st.blocked = nil
	err = newPipe(t, st, mem).Run(ctx, Job{ID: "j", VideoID: vid, Type: "METADATA"})
	if !errors.As(err, &je) || je.Code != "size_mismatch" {
		t.Fatalf("size mismatch: %v", err)
	}
}

func TestRunSkipsUnavailableVideos(t *testing.T) {
	for _, v := range []VideoRow{
		{Status: "PROCESSING", Moderation: "REMOVED", OriginalKey: "k"},
		{Status: "FAILED", Moderation: "OK", OriginalKey: "k"},
	} {
		st := &fakeStore{video: v}
		err := newPipe(t, st, newMem()).Run(context.Background(), Job{VideoID: "x", Type: "HLS"})
		var je *JobError
		if !errors.As(err, &je) || !je.Permanent || !je.Silent {
			t.Errorf("%+v: %v", v, err)
		}
	}
}

func TestPortraitAndSilentSource(t *testing.T) {
	needFFmpeg(t)
	const vid = "0190aaaa-bbbb-7ccc-8ddd-111111111111"
	src := makeVideo(t, "-f", "lavfi", "-i", "testsrc=size=720x1280:rate=24", "-t", "3", "-c:v", "libx264", "-pix_fmt", "yuv420p")
	mem := newMem()
	key := "uploads/" + vid + "/original.mp4"
	mem.objs[key] = src
	st := &fakeStore{video: VideoRow{CreatorID: "c", Status: "PROCESSING", Moderation: "OK", OriginalKey: key, Size: int64(len(src))}}
	p := newPipe(t, st, mem)
	for _, typ := range []string{"METADATA", "THUMBNAIL", "TRANSCODE", "HLS"} {
		if err := p.Run(context.Background(), Job{ID: "j", VideoID: vid, Type: typ}); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
	}
	if st.meta.HasAudio {
		t.Error("silent source reported audio")
	}
	master := string(mem.objs["hls/"+vid+"/master.m3u8"])
	if strings.Contains(master, "mp4a") || !strings.Contains(master, "RESOLUTION=720x1280") || strings.Contains(master, "1080p") {
		t.Errorf("master:\n%s", master)
	}
}
