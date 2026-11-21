package worker

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/redxtube/api/internal/storage"
)

func TestPolicyIsConfigurableButNeverBeyondTheDatabase(t *testing.T) {
	po := DefaultPolicy()
	if err := po.Check(); err != nil {
		t.Fatal(err)
	}
	ok := Probe{Width: 1280, Height: 720, Duration: 30, VideoCodec: "h264", Container: "mp4", FPS: 30, Streams: 2}
	// lowering is allowed and enforced
	po.MaxDurationSec = 20 * 60
	po.MaxFPS = 30
	po.MaxStreams = 2
	po.VideoCodecs = map[string]bool{"h264": true}
	cases := []struct {
		name string
		mut  func(*Probe)
		code string
	}{
		{"within limits", func(p *Probe) {}, ""},
		{"too long for this policy", func(p *Probe) { p.Duration = 21 * 60 }, "too_long"},
		{"fps over policy", func(p *Probe) { p.FPS = 60 }, "frame_rate_too_high"},
		{"too many streams", func(p *Probe) { p.Streams = 3 }, "too_many_streams"},
		{"codec not allowed here", func(p *Probe) { p.VideoCodec = "hevc" }, "unsupported_codec"},
	}
	for _, c := range cases {
		p := ok
		c.mut(&p)
		_, _, err := po.Validate(p, 1<<20)
		switch {
		case c.code == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.code != "" && (err == nil || err.Code != c.code || !err.Permanent):
			t.Errorf("%s: got %v, want %s", c.name, err, c.code)
		}
	}
	// the message tells the user the real limit
	if _, _, err := po.Validate(Probe{Width: 640, Height: 360, Duration: 3600, VideoCodec: "h264", Container: "mp4"}, 1<<20); err == nil || !strings.Contains(err.Message, "20 minutes") {
		t.Errorf("limit not shown to the user: %v", err)
	}
	// raising past what the database enforces is refused at startup
	for name, mut := range map[string]func(*Policy){
		"short bytes": func(p *Policy) { p.ShortMaxBytes = 300 << 20 },
		"short secs":  func(p *Policy) { p.ShortMaxSec = 120 },
		"long bytes":  func(p *Policy) { p.LongMaxBytes = int64(6) << 30 },
		"short side":  func(p *Policy) { p.MaxShortSide = 2160 },
		"long side":   func(p *Policy) { p.MaxLongSide = 3840 },
		"streams":     func(p *Policy) { p.MaxStreams = 0 },
	} {
		p := DefaultPolicy()
		mut(&p)
		if p.Check() == nil {
			t.Errorf("%s: an out-of-range policy was accepted", name)
		}
	}
}

func TestProfileAndLadder(t *testing.T) {
	pr := DefaultProfile()
	if err := pr.Check(); err != nil || pr.Version != "h264-aac-hls-v1" {
		t.Fatalf("%v %q", err, pr.Version)
	}
	rungs, err := ParseLadder("360:700,720:2500")
	if err != nil {
		t.Fatal(err)
	}
	pr.Rungs = rungs
	pr.Version = "custom-v2"
	if err := pr.Check(); err != nil {
		t.Fatal(err)
	}
	if got := pr.Plan(1920, 1080); len(got) != 2 || got[1].Name != "720p" || got[1].VideoKbps != 2500 {
		t.Fatalf("custom ladder ignored: %+v", got)
	}
	for _, bad := range []string{"540:1000", "360:800,360:900", "720:50", "x:y", "480:1000,360:800"} {
		r, perr := ParseLadder(bad)
		p := DefaultProfile()
		if perr == nil {
			p.Rungs = r
			perr = p.Check()
		}
		if perr == nil {
			t.Errorf("ladder %q accepted", bad)
		}
	}
	p := DefaultProfile()
	p.Preset = "placebo; rm -rf /"
	if p.Check() == nil {
		t.Error("arbitrary x264 preset accepted")
	}
	p = DefaultProfile()
	p.SegmentSeconds = 60
	if p.Check() == nil {
		t.Error("absurd segment length accepted")
	}
}

func TestAnamorphicSourceUsesDisplaySize(t *testing.T) {
	raw := `{"streams":[{"codec_type":"video","codec_name":"h264","width":720,"height":576,"sample_aspect_ratio":"16:11","avg_frame_rate":"25/1"}],
	 "format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"10"}}`
	p, err := ParseProbe([]byte(raw), ".mp4")
	if err != nil {
		t.Fatal(err)
	}
	if p.Width != 1047 || p.Height != 576 || p.SAR != "16:11" {
		t.Fatalf("display size %dx%d SAR %q", p.Width, p.Height, p.SAR)
	}
	rs := Plan(p.Width, p.Height)
	if last := rs[len(rs)-1]; last.Name != "480p" || last.W != 872 || last.H != 480 {
		t.Fatalf("anamorphic plan wrong: %+v", rs)
	}
	if args := strings.Join(hlsArgs("/in", "/out", rs[0], p, DefaultProfile(), 0), " "); !strings.Contains(args, "setsar=1") {
		t.Error("output must declare square pixels")
	}
	// unknown or square SAR leaves the size alone
	for _, sar := range []string{"1:1", "0:1", "", "N/A"} {
		q, _ := ParseProbe([]byte(strings.Replace(raw, `"16:11"`, `"`+sar+`"`, 1)), ".mp4")
		if q.Width != 720 {
			t.Errorf("SAR %q changed the width to %d", sar, q.Width)
		}
	}
}

func TestSanitizeDiagHidesPathsAndControlChars(t *testing.T) {
	in := "Error opening input file /work/0190abc/original.mp4: Invalid data\x00\x1b[31m in /home/claude/tube/x.ts\n\n  more"
	got := sanitizeDiag(in)
	for _, bad := range []string{"/work", "original.mp4", "/home", "\x00", "\x1b"} {
		if strings.Contains(got, bad) {
			t.Errorf("%q leaked in %q", bad, got)
		}
	}
	if !strings.Contains(got, "Invalid data") || !strings.Contains(got, "<path>") {
		t.Errorf("useful part lost: %q", got)
	}
	if len(sanitizeDiag(strings.Repeat("a ", 1000))) > 410 {
		t.Error("not capped")
	}
}

func writeRendition(t *testing.T, segs []string, durs []string, ended bool, empty string) string {
	t.Helper()
	d := t.TempDir()
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:6\n#EXT-X-PLAYLIST-TYPE:VOD\n")
	for i, s := range segs {
		b.WriteString("#EXTINF:" + durs[i] + ",\n" + s + "\n")
		data := []byte("tsdata")
		if s == empty {
			data = nil
		}
		_ = os.WriteFile(filepath.Join(d, s), data, 0o600)
	}
	if ended {
		b.WriteString("#EXT-X-ENDLIST\n")
	}
	_ = os.WriteFile(filepath.Join(d, "index.m3u8"), []byte(b.String()), 0o600)
	return d
}

func TestValidatePlaylist(t *testing.T) {
	segs := []string{"seg_00000.ts", "seg_00001.ts", "seg_00002.ts"}
	good := []string{"6.000000", "6.000000", "2.000000"}
	if n, err := ValidatePlaylist(writeRendition(t, segs, good, true, ""), 14, 6); err != nil || n != 3 {
		t.Fatalf("good rendition: %d %v", n, err)
	}
	cases := map[string]struct {
		dir func() string
		src float64
	}{
		"no ENDLIST":       {func() string { return writeRendition(t, segs, good, false, "") }, 14},
		"empty segment":    {func() string { return writeRendition(t, segs, good, true, "seg_00001.ts") }, 14},
		"wrong total":      {func() string { return writeRendition(t, segs, good, true, "") }, 60},
		"segment too long": {func() string { return writeRendition(t, segs, []string{"6", "6", "30"}, true, "") }, 42},
		"traversal":        {func() string { return writeRendition(t, []string{"../x.ts"}, []string{"5"}, true, "") }, 5},
		"foreign name":     {func() string { return writeRendition(t, []string{"evil.ts"}, []string{"5"}, true, "") }, 5},
		"missing playlist": {func() string { return t.TempDir() }, 14},
	}
	for name, c := range cases {
		if _, err := ValidatePlaylist(c.dir(), c.src, 6); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// a segment listed but deleted
	d := writeRendition(t, segs, good, true, "")
	_ = os.Remove(filepath.Join(d, "seg_00001.ts"))
	if _, err := ValidatePlaylist(d, 14, 6); err == nil {
		t.Error("missing segment accepted")
	}
}

func TestValidateMaster(t *testing.T) {
	plan := Plan(1280, 720)
	if err := ValidateMaster(MasterPlaylist(plan, true), plan); err != nil {
		t.Fatal(err)
	}
	good := MasterPlaylist(plan, true)
	for name, bad := range map[string]string{
		"missing rendition": strings.Replace(good, "480p/index.m3u8", "x/index.m3u8", 1),
		"wrong resolution":  strings.Replace(good, "RESOLUTION=1280x720", "RESOLUTION=1x1", 1),
		"no bandwidth":      strings.Replace(good, "BANDWIDTH=", "BANDWIDTHX=", 1),
		"truncated":         good[:strings.Index(good, "480p")-60],
		"no header":         strings.Replace(good, "#EXTM3U", "", 1),
	} {
		if ValidateMaster(bad, plan) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func jpegFile(t *testing.T, w, h int, c color.Gray) string {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = c.Y
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "t.jpg")
	_ = os.WriteFile(p, b.Bytes(), 0o600)
	return p
}

func TestThumbnailInspectionAndBlackFrameAvoidance(t *testing.T) {
	w, h, l, err := inspectJPEG(jpegFile(t, 640, 360, color.Gray{Y: 200}))
	if err != nil || w != 640 || h != 360 || l < 190 || l > 210 {
		t.Fatalf("%d %d %v %v", w, h, l, err)
	}
	if _, _, l, _ := inspectJPEG(jpegFile(t, 640, 360, color.Gray{Y: 0})); l > 5 {
		t.Errorf("black frame luma %v", l)
	}
	junk := filepath.Join(t.TempDir(), "x.jpg")
	_ = os.WriteFile(junk, []byte("not a jpeg"), 0o600)
	if _, _, _, err := inspectJPEG(junk); err == nil {
		t.Error("non-JPEG accepted")
	}
	// default (middle) frame wins when it is not black; a black middle frame is replaced by the brightest
	if got := pickPrimary([]float64{90, 100, 110, 100, 90}, 2); got != 2 {
		t.Errorf("kept the default? got %d", got)
	}
	if got := pickPrimary([]float64{60, 120, 2, 80, 40}, 2); got != 1 {
		t.Errorf("black middle frame not replaced: %d", got)
	}
	if got := pickPrimary([]float64{1, 2, 0, 1, 1}, 2); got != 1 {
		t.Errorf("all dark: brightest still wins, got %d", got)
	}
}

func TestPipelineWaitsInsteadOfFillingTheDisk(t *testing.T) {
	const vid = "0190aaaa-bbbb-7ccc-8ddd-333333333333"
	mem := newMem()
	key := "uploads/" + vid + "/original.mp4"
	mem.objs[key] = bytes.Repeat([]byte("x"), 1000)
	st := &fakeStore{video: VideoRow{CreatorID: "c", Status: "PROCESSING", Moderation: "OK", OriginalKey: key, Size: 1000}}
	p := newPipe(t, st, mem)
	p.Free = func(string) (uint64, error) { return 10, nil } // 10 bytes free
	err := p.Run(context.Background(), Job{ID: "j", VideoID: vid, Type: "METADATA"})
	var je *JobError
	if !errors.As(err, &je) || je.Code != "disk_full" || je.Permanent {
		t.Fatalf("a full disk must be a retryable condition, not the upload's fault: %v", err)
	}
	if ents, _ := os.ReadDir(p.dir(vid)); len(ents) != 0 {
		t.Errorf("the download started anyway: %d entries in the work dir", len(ents))
	}
}

// lyingStorage reports a different size than was stored: verification must catch it.
type lyingStorage struct{ *memStorage }

func (l lyingStorage) Head(ctx context.Context, key string) (storage.ObjectInfo, error) {
	info, err := l.memStorage.Head(ctx, key)
	if strings.HasPrefix(key, "thumbs/") {
		info.Size++
	}
	return info, err
}

func TestStoredObjectsAreVerified(t *testing.T) {
	needFFmpeg(t)
	const vid = "0190aaaa-bbbb-7ccc-8ddd-444444444444"
	src := makeVideo(t, "-f", "lavfi", "-i", "testsrc=size=320x240:rate=10", "-t", "2", "-c:v", "libx264", "-pix_fmt", "yuv420p")
	mem := newMem()
	key := "uploads/" + vid + "/original.mp4"
	mem.objs[key] = src
	st := &fakeStore{video: VideoRow{CreatorID: "c", Status: "PROCESSING", Moderation: "OK", OriginalKey: key, Size: int64(len(src))},
		meta: MetaRow{Width: 320, Height: 240, FPS: 10, Duration: 2}}
	p := newPipe(t, st, mem)
	p.Storage = lyingStorage{mem}
	err := p.Run(context.Background(), Job{ID: "j", VideoID: vid, Type: "THUMBNAIL"})
	var je *JobError
	if !errors.As(err, &je) || je.Code != "storage_verify" || je.Permanent {
		t.Fatalf("a size mismatch after upload must fail the step (retryable): %v", err)
	}
	if len(st.thumbs) != 0 {
		t.Error("thumbnails were recorded although verification failed")
	}
}

// A broken encode must be caught locally: nothing may be uploaded or recorded.
func TestBrokenEncodeIsNeverUploaded(t *testing.T) {
	needFFmpeg(t)
	const vid = "0190aaaa-bbbb-7ccc-8ddd-555555555555"
	src := makeVideo(t, "-f", "lavfi", "-i", "testsrc=size=320x240:rate=10", "-t", "8", "-c:v", "libx264", "-pix_fmt", "yuv420p")
	// ffmpeg wrapper that deletes a segment after encoding a rendition, as a failing disk would
	wrapper := filepath.Join(t.TempDir(), "ffmpeg-wrap.sh")
	_ = os.WriteFile(wrapper, []byte("#!/bin/sh\nffmpeg \"$@\" || exit $?\nfor last; do :; done\ncase \"$last\" in *index.m3u8) rm -f \"$(dirname \"$last\")/seg_00000.ts\";; esac\n"), 0o700)

	mem := newMem()
	key := "uploads/" + vid + "/original.mp4"
	mem.objs[key] = src
	st := &fakeStore{video: VideoRow{CreatorID: "c", Status: "PROCESSING", Moderation: "OK", OriginalKey: key, Size: int64(len(src))},
		meta: MetaRow{Width: 320, Height: 240, FPS: 10, Duration: 8}}
	p := newPipe(t, st, mem)
	p.FFmpeg = wrapper
	err := p.Run(context.Background(), Job{ID: "j", VideoID: vid, Type: "TRANSCODE"})
	var je *JobError
	if !errors.As(err, &je) || je.Code != "output_invalid" || je.Permanent {
		t.Fatalf("expected retryable output_invalid, got %v", err)
	}
	if n := len(mem.keys("hls/")); n != 0 {
		t.Fatalf("%d objects uploaded from an invalid encode: %v", n, mem.keys("hls/"))
	}
	if len(st.variants) != 0 {
		t.Error("a variant was recorded for an invalid encode")
	}
}

func TestCleanupOutputsKeepsTheOriginal(t *testing.T) {
	const vid = "0190aaaa-bbbb-7ccc-8ddd-666666666666"
	mem := newMem()
	for _, k := range []string{"uploads/" + vid + "/original.mp4", "hls/" + vid + "/master.m3u8", "hls/" + vid + "/360p/seg_00000.ts", "hls/other/master.m3u8", "thumbs/" + vid + "/a.jpg"} {
		mem.objs[k] = []byte("x")
	}
	p := newPipe(t, &fakeStore{}, mem)
	_ = os.MkdirAll(p.dir(vid), 0o700)
	if err := p.CleanupOutputs(context.Background(), vid); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(mem.keys(""), ","); got != "hls/other/master.m3u8,thumbs/"+vid+"/a.jpg,uploads/"+vid+"/original.mp4" {
		t.Fatalf("cleanup removed the wrong things: %s", got)
	}
	if _, err := os.Stat(p.dir(vid)); !os.IsNotExist(err) {
		t.Error("scratch directory left behind")
	}
	if err := p.CleanupOutputs(context.Background(), vid); err != nil { // idempotent
		t.Fatal(err)
	}
}
