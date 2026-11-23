package worker

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/redxtube/api/internal/storage"
)

// These tests close gaps found by mutation testing the worker's own suite: each one fails if the guard it names is
// weakened, and passed unnoticed before.

var regexpBandwidth = regexp.MustCompile(`BANDWIDTH=\d+`)

func TestPolicyBoundaries(t *testing.T) {
	po := DefaultPolicy()
	base := Probe{Width: 1280, Height: 720, Duration: 30, VideoCodec: "h264", Container: "mp4", FPS: 30, Streams: 2}
	for _, c := range []struct {
		name string
		mut  func(*Probe)
		size int64
		typ  string
		code string
	}{
		{"duration exactly at the limit", func(p *Probe) { p.Duration = float64(po.MaxDurationSec) }, 1 << 20, "LONG", ""},
		{"half a second over the duration limit", func(p *Probe) { p.Duration = float64(po.MaxDurationSec) + 0.5 }, 1 << 20, "", "too_long"},
		{"exactly 1920x1080", func(p *Probe) { p.Width, p.Height = 1920, 1080 }, 1 << 20, "SHORT", ""},
		{"1921 wide", func(p *Probe) { p.Width, p.Height = 1921, 1080 }, 1 << 20, "", "resolution_too_high"},
		{"portrait, 1921 tall", func(p *Probe) { p.Width, p.Height = 1080, 1921 }, 1 << 20, "", "resolution_too_high"},
		{"wide and short: long side over the limit, short side fine", func(p *Probe) { p.Width, p.Height = 2000, 500 }, 1 << 20, "", "resolution_too_high"},
		{"smallest accepted side", func(p *Probe) { p.Width, p.Height = po.MinSide, po.MinSide }, 1 << 20, "SHORT", ""},
		{"one pixel below the smallest side", func(p *Probe) { p.Width, p.Height = po.MinSide-1, po.MinSide }, 1 << 20, "", "resolution_too_low"},
		{"zero width is no video, not a small one", func(p *Probe) { p.Width = 0 }, 1 << 20, "", "no_video_stream"},
		{"zero height is no video, not a small one", func(p *Probe) { p.Height = 0 }, 1 << 20, "", "no_video_stream"},
		{"negative width", func(p *Probe) { p.Width = -5 }, 1 << 20, "", "no_video_stream"},
		{"streams exactly at the limit", func(p *Probe) { p.Streams = po.MaxStreams }, 1 << 20, "SHORT", ""},
		{"one stream over the limit", func(p *Probe) { p.Streams = po.MaxStreams + 1 }, 1 << 20, "", "too_many_streams"},
		{"frame rate exactly at the limit", func(p *Probe) { p.FPS = po.MaxFPS }, 1 << 20, "SHORT", ""},
		{"frame rate just over the limit", func(p *Probe) { p.FPS = po.MaxFPS + 0.5 }, 1 << 20, "", "frame_rate_too_high"},
		{"SHORT exactly at its size limit", func(p *Probe) {}, po.ShortMaxBytes, "SHORT", ""},
		{"SHORT one byte over its size limit", func(p *Probe) {}, po.ShortMaxBytes + 1, "", "short_too_large"},
		{"61 s of 201 MiB is a LONG video and fine", func(p *Probe) { p.Duration = 61 }, 201 << 20, "LONG", ""},
	} {
		p := base
		c.mut(&p)
		typ, _, err := po.Validate(p, c.size)
		switch {
		case c.code == "" && err != nil:
			t.Errorf("%s: unexpectedly refused: %v", c.name, err)
		case c.code != "" && (err == nil || err.Code != c.code || !err.Permanent):
			t.Errorf("%s: got %v, want permanent %s", c.name, err, c.code)
		case c.code == "" && typ != c.typ:
			t.Errorf("%s: type %q, want %q", c.name, typ, c.typ)
		}
	}
}

func playlistDir(t *testing.T, body string, segments map[string]int) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, size := range segments {
		if err := os.WriteFile(filepath.Join(dir, name), bytes.Repeat([]byte{'x'}, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestPlaylistValidation(t *testing.T) {
	const head = "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:6\n"
	segs := map[string]int{"seg_00000.ts": 10, "seg_00001.ts": 10}
	for _, c := range []struct {
		name   string
		body   string
		files  map[string]int
		source float64
		ok     bool
	}{
		{"two segments", head + "#EXTINF:6.0,\nseg_00000.ts\n#EXTINF:3.0,\nseg_00001.ts\n#EXT-X-ENDLIST\n", segs, 9, true},
		{"a segment at the longest tolerated length", head + "#EXTINF:8.0,\nseg_00000.ts\n#EXT-X-ENDLIST\n", segs, 8, true},
		{"a segment longer than the target allows", head + "#EXTINF:8.5,\nseg_00000.ts\n#EXT-X-ENDLIST\n", segs, 8.5, false},
		{"no segments at all for a very short source", "#EXTM3U\n#EXT-X-ENDLIST\n", nil, 1.5, false},
		{"an empty segment file", head + "#EXTINF:6.0,\nseg_00000.ts\n#EXT-X-ENDLIST\n", map[string]int{"seg_00000.ts": 0}, 6, false},
		{"a missing segment file", head + "#EXTINF:6.0,\nseg_00009.ts\n#EXT-X-ENDLIST\n", segs, 6, false},
		{"no end marker (an interrupted encode)", head + "#EXTINF:6.0,\nseg_00000.ts\n", segs, 6, false},
		{"a path traversal as a segment", head + "#EXTINF:6.0,\n../seg_00000.ts\n#EXT-X-ENDLIST\n", segs, 6, false},
		{"an absolute segment path", head + "#EXTINF:6.0,\n/etc/passwd\n#EXT-X-ENDLIST\n", segs, 6, false},
		{"a segment without its duration line", head + "seg_00000.ts\n#EXT-X-ENDLIST\n", segs, 6, false},
		{"a zero duration", head + "#EXTINF:0,\nseg_00000.ts\n#EXT-X-ENDLIST\n", segs, 6, false},
		{"a playlist that is much shorter than the source", head + "#EXTINF:6.0,\nseg_00000.ts\n#EXT-X-ENDLIST\n", segs, 60, false},
		{"not a playlist", "hello\n", segs, 6, false},
	} {
		_, err := ValidatePlaylist(playlistDir(t, c.body, c.files), c.source, 6)
		if c.ok && err != nil {
			t.Errorf("%s: unexpectedly refused: %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}
	if _, err := ValidatePlaylist(t.TempDir(), 6, 6); err == nil {
		t.Error("a missing playlist must be refused")
	}
}

func TestMasterValidation(t *testing.T) {
	plan := Plan(1280, 720) // 360p, 480p, 720p
	good := MasterPlaylist(plan, true)
	if err := ValidateMaster(good, plan); err != nil {
		t.Fatalf("control: %v", err)
	}
	lines := strings.Split(good, "\n")
	variant := func(name string) int { // index of the STREAM-INF line of a rendition
		for i, l := range lines {
			if i+1 < len(lines) && strings.HasPrefix(l, "#EXT-X-STREAM-INF:") && lines[i+1] == name+"/index.m3u8" {
				return i
			}
		}
		t.Fatalf("no variant %s in\n%s", name, good)
		return -1
	}
	edit := func(f func(ls []string) []string) string { return strings.Join(f(append([]string{}, lines...)), "\n") }
	v480 := variant("480p")
	for name, body := range map[string]string{
		"a variant without BANDWIDTH": edit(func(ls []string) []string {
			ls[v480] = strings.Join(strings.FieldsFunc(ls[v480], func(r rune) bool { return r == ',' }), ",")
			ls[v480] = strings.Replace(ls[v480], "BANDWIDTH=", "AVERAGE-BANDWIDTH=", 1)
			i := strings.Index(ls[v480], "AVERAGE-BANDWIDTH=")
			j := strings.IndexByte(ls[v480][i:], ',')
			if j < 0 {
				ls[v480] = ls[v480][:i]
			} else {
				ls[v480] = ls[v480][:i] + ls[v480][i+j+1:]
			}
			return ls
		}),
		"a variant with BANDWIDTH=0": edit(func(ls []string) []string {
			ls[v480] = regexpBandwidth.ReplaceAllString(ls[v480], "BANDWIDTH=0")
			return ls
		}),
		"a rendition listed twice and another one missing": edit(func(ls []string) []string {
			ls[v480+1] = "360p/index.m3u8"
			ls[v480] = strings.Replace(ls[v480], "RESOLUTION="+resolution(plan, "480p"), "RESOLUTION="+resolution(plan, "360p"), 1)
			return ls
		}),
		"the wrong RESOLUTION": edit(func(ls []string) []string {
			ls[v480] = strings.Replace(ls[v480], "RESOLUTION="+resolution(plan, "480p"), "RESOLUTION=1x1", 1)
			return ls
		}),
		"a reference to an unknown playlist": edit(func(ls []string) []string { ls[v480+1] = "../../etc/index.m3u8"; return ls }),
		"a rendition missing": edit(func(ls []string) []string {
			v := variant("720p")
			return append(append([]string{}, ls[:v]...), ls[v+2:]...)
		}),
		"a variant without a reference line": strings.TrimRight(good, "\n") + "\n#EXT-X-STREAM-INF:BANDWIDTH=1,RESOLUTION=1x1",
		"not a master playlist":              "<html></html>",
	} {
		if err := ValidateMaster(body, plan); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func resolution(plan []Rendition, name string) string {
	for _, r := range plan {
		if r.Name == name {
			return strconv.Itoa(r.W) + "x" + strconv.Itoa(r.H)
		}
	}
	return ""
}

// skewStorage reports another size than was stored for the keys it matches, in either direction.
type skewStorage struct {
	*memStorage
	match func(key string) bool
	delta int64
}

func (s skewStorage) Head(ctx context.Context, key string) (storage.ObjectInfo, error) {
	info, err := s.memStorage.Head(ctx, key)
	if s.match(key) {
		info.Size += s.delta
	}
	return info, err
}

func verifyFixture(t *testing.T, vid string) (*fakeStore, *memStorage, []byte) {
	t.Helper()
	needFFmpeg(t)
	src := makeVideo(t, "-f", "lavfi", "-i", "testsrc=size=320x240:rate=10", "-t", "2", "-c:v", "libx264", "-pix_fmt", "yuv420p")
	mem := newMem()
	key := "uploads/" + vid + "/original.mp4"
	mem.objs[key] = src
	st := &fakeStore{video: VideoRow{CreatorID: "c", Status: "PROCESSING", Moderation: "OK", OriginalKey: key, Size: int64(len(src))},
		meta: MetaRow{Width: 320, Height: 240, FPS: 10, Duration: 2}}
	return st, mem, src
}

func wantVerifyFailure(t *testing.T, err error) {
	t.Helper()
	var je *JobError
	if !errors.As(err, &je) || je.Code != "storage_verify" || je.Permanent {
		t.Fatalf("a size mismatch after upload must fail the step and be retryable: %v", err)
	}
}

func TestEveryStoredOutputIsVerifiedInBothDirections(t *testing.T) {
	const vid = "0190aaaa-bbbb-7ccc-8ddd-666666666666"
	for _, c := range []struct {
		name  string
		job   string
		match func(string) bool
		pre   func(*fakeStore)
		done  func(*fakeStore) bool // true when the step wrongly went on to record its result
	}{
		{"HLS segments", "TRANSCODE", func(k string) bool { return strings.HasSuffix(k, ".ts") }, nil, func(s *fakeStore) bool { return len(s.variants) != 0 }},
		{"rendition playlists", "TRANSCODE", func(k string) bool { return strings.HasSuffix(k, "/index.m3u8") }, nil, func(s *fakeStore) bool { return len(s.variants) != 0 }},
		{"thumbnails", "THUMBNAIL", func(k string) bool { return strings.HasPrefix(k, "thumbs/") }, nil, func(s *fakeStore) bool { return len(s.thumbs) != 0 }},
		{"the master playlist", "HLS", func(k string) bool { return strings.HasSuffix(k, "/master.m3u8") }, func(s *fakeStore) { s.variants = map[string]bool{"360p": true} }, func(s *fakeStore) bool { return s.master != "" }},
	} {
		for _, delta := range []int64{+1, -1} {
			st, mem, _ := verifyFixture(t, vid)
			if c.pre != nil {
				c.pre(st)
			}
			p := newPipe(t, st, mem)
			p.Storage = skewStorage{mem, c.match, delta}
			err := p.Run(context.Background(), Job{ID: "j", VideoID: vid, Type: c.job})
			wantVerifyFailure(t, err)
			if c.done(st) {
				t.Errorf("%s (size %+d): the step recorded its result although verification failed", c.name, delta)
			}
		}
		// control: with honest storage the same step succeeds, so the failures above come from verification alone
		st, mem, _ := verifyFixture(t, vid)
		if c.pre != nil {
			c.pre(st)
		}
		if err := newPipe(t, st, mem).Run(context.Background(), Job{ID: "j", VideoID: vid, Type: c.job}); err != nil {
			t.Fatalf("%s control: %v", c.name, err)
		}
		if !c.done(st) {
			t.Errorf("%s control: the step did not record its result", c.name)
		}
	}
}

func TestACachedOriginalOfTheWrongSizeIsDownloadedAgain(t *testing.T) {
	const vid = "0190aaaa-bbbb-7ccc-8ddd-777777777777"
	data := bytes.Repeat([]byte("real video bytes "), 200)
	mem := newMem()
	key := "uploads/" + vid + "/original.mp4"
	mem.objs[key] = data
	v := VideoRow{OriginalKey: key, Size: int64(len(data))}
	p := newPipe(t, &fakeStore{video: v}, mem)
	path, _, err := p.original(context.Background(), vid, v, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("stale or swapped"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, _, err = p.original(context.Background(), vid, v, false)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, data) {
		t.Fatalf("a cached original that does not have the recorded size was trusted (%d bytes)", len(got))
	}
}

func TestPGClaimSkipsAJobThatHasUsedItsAttempts(t *testing.T) {
	e := newPGEnv(t)
	vid := e.video(2)
	s := e.store("w")
	e.exec(`UPDATE video_processing_jobs SET attempts = max_attempts WHERE video_id = $1::uuid`, vid)
	got, err := claimVideo(ctxb, s, vid)
	if err != nil || got != nil {
		t.Fatalf("a job with no attempts left must be skipped quietly, got %+v err %v", got, err)
	}
}
