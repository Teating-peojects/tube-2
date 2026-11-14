package worker

import (
	"strings"
	"testing"
	"time"
)

func TestPlanNeverUpscalesAndKeepsAspect(t *testing.T) {
	names := func(rs []Rendition) string {
		var s []string
		for _, r := range rs {
			s = append(s, r.Name)
		}
		return strings.Join(s, ",")
	}
	cases := []struct {
		w, h int
		want string
	}{
		{1920, 1080, "360p,480p,720p,1080p"},
		{1280, 720, "360p,480p,720p"},
		{1080, 1920, "360p,480p,720p,1080p"}, // portrait: short side is the width
		{854, 480, "360p,480p"},
		{426, 240, "360p"}, // below the ladder: one rendition at native size
		{1920, 800, "360p,480p,720p"},
	}
	for _, c := range cases {
		if got := names(Plan(c.w, c.h)); got != c.want {
			t.Errorf("Plan(%d,%d) = %s, want %s", c.w, c.h, got, c.want)
		}
	}
	r := Plan(426, 240)[0]
	if r.W != 426 || r.H != 240 {
		t.Errorf("native size not kept: %dx%d", r.W, r.H)
	}
	for _, r := range Plan(1080, 1920) {
		if r.W%2 != 0 || r.H%2 != 0 || r.H < r.W {
			t.Errorf("portrait rendition %s is %dx%d", r.Name, r.W, r.H)
		}
	}
	if p := Plan(1280, 720)[2]; p.W != 1280 || p.H != 720 {
		t.Errorf("720p of 1280x720 = %dx%d", p.W, p.H)
	}
}

func TestMasterPlaylist(t *testing.T) {
	m := MasterPlaylist(Plan(1280, 720), true)
	for _, want := range []string{"#EXTM3U", "360p/index.m3u8", "720p/index.m3u8", "RESOLUTION=1280x720", "mp4a.40.2"} {
		if !strings.Contains(m, want) {
			t.Errorf("master missing %q:\n%s", want, m)
		}
	}
	if strings.Contains(MasterPlaylist(Plan(1280, 720), false), "mp4a") {
		t.Error("audio codec advertised for a silent video")
	}
}

func TestValidate(t *testing.T) {
	ok := Probe{Width: 1280, Height: 720, Duration: 30, VideoCodec: "h264", Container: "mp4"}
	cases := []struct {
		name string
		mut  func(*Probe)
		size int64
		typ  string
		code string
	}{
		{"short", func(p *Probe) {}, 10 << 20, "SHORT", ""},
		{"exactly 60s is short", func(p *Probe) { p.Duration = 60 }, 10 << 20, "SHORT", ""},
		{"60.4s is long", func(p *Probe) { p.Duration = 60.4 }, 10 << 20, "LONG", ""},
		{"short too large", func(p *Probe) {}, 201 << 20, "", "short_too_large"},
		{"long up to 5GiB", func(p *Probe) { p.Duration = 600 }, LongMaxBytes, "LONG", ""},
		{"long over 5GiB", func(p *Probe) { p.Duration = 600 }, LongMaxBytes + 1, "", "too_large"},
		{"61 minutes", func(p *Probe) { p.Duration = 3661 }, 1 << 20, "", "too_long"},
		{"4k", func(p *Probe) { p.Width, p.Height = 3840, 2160 }, 1 << 20, "", "resolution_too_high"},
		{"1080p portrait", func(p *Probe) { p.Width, p.Height = 1080, 1920 }, 1 << 20, "SHORT", ""},
		{"bad codec", func(p *Probe) { p.VideoCodec = "prores" }, 1 << 20, "", "unsupported_codec"},
		{"bad container", func(p *Probe) { p.Container = "" }, 1 << 20, "", "unsupported_container"},
		{"no video", func(p *Probe) { p.VideoCodec = "" }, 1 << 20, "", "no_video_stream"},
		{"no duration", func(p *Probe) { p.Duration = 0 }, 1 << 20, "", "invalid_duration"},
	}
	for _, c := range cases {
		p := ok
		c.mut(&p)
		typ, _, err := p.Validate(c.size)
		switch {
		case c.code == "" && err != nil:
			t.Errorf("%s: unexpected %v", c.name, err)
		case c.code != "" && (err == nil || err.Code != c.code || !err.Permanent):
			t.Errorf("%s: got %v, want permanent %s", c.name, err, c.code)
		case typ != c.typ:
			t.Errorf("%s: type %q, want %q", c.name, typ, c.typ)
		}
	}
}

func TestParseProbeRotationAndStreams(t *testing.T) {
	raw := `{"streams":[
	 {"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"avg_frame_rate":"30000/1001","side_data_list":[{"rotation":-90}]},
	 {"codec_type":"video","codec_name":"mjpeg","width":300,"height":300,"disposition":{"attached_pic":1}},
	 {"codec_type":"audio","codec_name":"aac"}],
	 "format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"12.5","bit_rate":"4000000"}}`
	p, err := ParseProbe([]byte(raw), ".MOV")
	if err != nil {
		t.Fatal(err)
	}
	if p.Width != 1080 || p.Height != 1920 {
		t.Errorf("rotation not applied: %dx%d", p.Width, p.Height)
	}
	if p.VideoCodec != "h264" || !p.HasAudio || p.Container != "mov" || p.Duration != 12.5 {
		t.Errorf("unexpected probe %+v", p)
	}
	if p.FPS < 29.9 || p.FPS > 30 {
		t.Errorf("fps %v", p.FPS)
	}
	// an HLS playlist renamed to .mp4 must not pass the container allow-list
	bad, _ := ParseProbe([]byte(`{"streams":[{"codec_type":"video","codec_name":"h264","width":640,"height":360}],"format":{"format_name":"hls","duration":"5"}}`), ".mp4")
	if _, _, je := bad.Validate(1 << 20); je == nil || je.Code != "unsupported_container" {
		t.Errorf("hls disguised as mp4 was accepted: %v", je)
	}
}

func TestHLSArgsAreSafe(t *testing.T) {
	r := Plan(1920, 1080)[3]
	args := strings.Join(hlsArgs("/in.mp4", "/out", r, Probe{HasAudio: true, FPS: 120}, 2), " ")
	for _, want := range []string{"-protocol_whitelist file", "-map_metadata -1", "fps=60", "scale=1920:1080", "-hls_playlist_type vod", "seg_%05d.ts", "-threads 2"} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q: %s", want, args)
		}
	}
	if !strings.Contains(strings.Join(hlsArgs("/in.mp4", "/out", r, Probe{}, 0), " "), "-an") {
		t.Error("silent source must not map audio")
	}
}

func TestThumbTimesClamped(t *testing.T) {
	ts := thumbTimes(2)
	if len(ts) != thumbCount {
		t.Fatalf("len %d", len(ts))
	}
	for _, x := range ts {
		if x < 0 || x > 1.5 {
			t.Errorf("time %v outside the clip", x)
		}
	}
	if thumbTimes(0.3)[4] != 0 {
		t.Error("sub-second clip must clamp to 0")
	}
}

func TestBackoff(t *testing.T) {
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute}
	for i, w := range want {
		if got := backoff(i + 1); got != w {
			t.Errorf("backoff(%d) = %v, want %v", i+1, got, w)
		}
	}
	if backoff(20) != 10*time.Minute {
		t.Error("backoff not capped")
	}
}

func TestParseProbeContainerLabels(t *testing.T) {
	mk := func(format, ext string) string {
		p, err := ParseProbe([]byte(`{"streams":[{"codec_type":"video","codec_name":"h264","width":640,"height":360}],"format":{"format_name":"`+format+`","duration":"5"}}`), ext)
		if err != nil {
			t.Fatal(err)
		}
		return p.Container
	}
	for _, c := range []struct{ format, ext, want string }{
		{"mov,mp4,m4a,3gp,3g2,mj2", ".mp4", "mp4"},
		{"mov,mp4,m4a,3gp,3g2,mj2", ".m4v", "mp4"},
		{"mov,mp4,m4a,3gp,3g2,mj2", ".MOV", "mov"},
		{"matroska,webm", ".webm", "webm"},
		{"matroska,webm", ".mkv", "mkv"},
	} {
		if got := mk(c.format, c.ext); got != c.want {
			t.Errorf("%s %s -> %q, want %q", c.format, c.ext, got, c.want)
		}
	}
}
