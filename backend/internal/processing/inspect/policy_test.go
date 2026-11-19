package inspect

import (
	"testing"
	"time"
)

func good() *Info {
	return &Info{
		Container: "mp4", FormatNames: []string{"mov", "mp4"}, DurationSeconds: 30, SizeBytes: 50 << 20, Streams: 2, VideoStreams: 1, AudioStreams: 1,
		Video: &VideoStream{Codec: "h264", Width: 1280, Height: 720, DisplayWidth: 1280, DisplayHeight: 720, SARNum: 1, SARDen: 1, FPS: 30},
		Audio: &AudioStream{Codec: "aac", SampleRate: 48000, Channels: 2},
	}
}

func TestPolicyAcceptsAGoodFileAndTheEdges(t *testing.T) {
	p := DefaultPolicy()
	if err := p.Validate(good()); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(*Info){
		"silent video":       func(i *Info) { i.Audio, i.AudioStreams, i.Streams = nil, 0, 1 },
		"exactly 1920x1080":  func(i *Info) { i.Video.DisplayWidth, i.Video.DisplayHeight = 1920, 1080 },
		"portrait 1080x1920": func(i *Info) { i.Video.DisplayWidth, i.Video.DisplayHeight = 1080, 1920 },
		"tiny 64x48 (below every rung)": func(i *Info) {
			i.Video.Width, i.Video.Height, i.Video.DisplayWidth, i.Video.DisplayHeight = 64, 48, 64, 48
		},
		"exactly 240 fps":              func(i *Info) { i.Video.FPS = 240 },
		"matroska":                     func(i *Info) { i.Container = "matroska" },
		"exactly 5 GiB, long":          func(i *Info) { i.SizeBytes, i.DurationSeconds = 5<<30, 3600 },
		"60 s and exactly 200 MiB":     func(i *Info) { i.DurationSeconds, i.SizeBytes = 60, 200<<20 },
		"60.5 s and 300 MiB is a LONG": func(i *Info) { i.DurationSeconds, i.SizeBytes = 60.5, 300<<20 },
		"unknown frame rate":           func(i *Info) { i.Video.FPS = 0 },
		"exactly the stream limit":     func(i *Info) { i.Streams = 16 },
		"exactly the minimum duration": func(i *Info) { i.DurationSeconds = 0.5 },
		"exactly the maximum duration": func(i *Info) { i.DurationSeconds = (4 * time.Hour).Seconds() },
	} {
		i := good()
		mut(i)
		if err := p.Validate(i); err != nil {
			t.Errorf("%s: unexpectedly refused: %v", name, err)
		}
	}
}

func TestPolicyRefusalsAndTheirCodes(t *testing.T) {
	p := DefaultPolicy()
	cases := []struct {
		name string
		mut  func(*Info)
		want string
	}{
		{"bigger than 5 GiB", func(i *Info) { i.SizeBytes = 5<<30 + 1 }, CodeTooLarge},
		{"empty file", func(i *Info) { i.SizeBytes = 0 }, CodeCorruptInput},
		{"stream bomb", func(i *Info) { i.Streams = 100000 }, CodeTooManyStreams},
		{"one over the stream limit", func(i *Info) { i.Streams = 17 }, CodeTooManyStreams},
		{"avi container", func(i *Info) { i.Container = "avi" }, CodeUnsupportedMedia},
		{"no container", func(i *Info) { i.Container = "" }, CodeUnsupportedMedia},
		{"no video stream", func(i *Info) { i.Video, i.VideoStreams = nil, 0 }, CodeNoVideoStream},
		{"video codec outside the list", func(i *Info) { i.Video.Codec = "wmv3" }, CodeUnsupportedCodec},
		{"audio codec outside the list", func(i *Info) { i.Audio.Codec = "wmav2" }, CodeUnsupportedCodec},
		{"zero width", func(i *Info) { i.Video.Width = 0 }, CodeDimensionsInvalid},
		{"negative height", func(i *Info) { i.Video.Height = -4 }, CodeDimensionsInvalid},
		{"zero display width", func(i *Info) { i.Video.DisplayWidth = 0 }, CodeDimensionsInvalid},
		{"absurd coded width", func(i *Info) { i.Video.Width = 1 << 30 }, CodeResolutionTooHigh},
		{"absurd display width (SAR bomb)", func(i *Info) { i.Video.DisplayWidth = 1 << 30 }, CodeResolutionTooHigh},
		{"4K", func(i *Info) {
			i.Video.Width, i.Video.Height, i.Video.DisplayWidth, i.Video.DisplayHeight = 3840, 2160, 3840, 2160
		}, CodeResolutionTooHigh},
		{"1081 short side", func(i *Info) { i.Video.DisplayWidth, i.Video.DisplayHeight = 1920, 1081 }, CodeResolutionTooHigh},
		{"1921 long side", func(i *Info) { i.Video.DisplayWidth, i.Video.DisplayHeight = 1921, 1080 }, CodeResolutionTooHigh},
		{"portrait 4K", func(i *Info) { i.Video.DisplayWidth, i.Video.DisplayHeight = 2160, 3840 }, CodeResolutionTooHigh},
		{"a sliver 2x1000", func(i *Info) { i.Video.DisplayWidth, i.Video.DisplayHeight = 2, 1000 }, CodeDimensionsInvalid},
		{"fps over the limit", func(i *Info) { i.Video.FPS = 241 }, CodeFPSTooHigh},
		{"fps absurd", func(i *Info) { i.Video.FPS = 1e9 }, CodeFPSTooHigh},
		{"no duration", func(i *Info) { i.DurationSeconds = 0 }, CodeCorruptInput},
		{"negative duration", func(i *Info) { i.DurationSeconds = -1 }, CodeCorruptInput},
		{"too short", func(i *Info) { i.DurationSeconds = 0.3 }, CodeTooShort},
		{"too long", func(i *Info) { i.DurationSeconds = (4*time.Hour + time.Second).Seconds() }, CodeTooLong},
		{"duration far beyond any int", func(i *Info) { i.DurationSeconds = 1e300 }, CodeTooLong},
		{"60 s but 201 MiB", func(i *Info) { i.DurationSeconds, i.SizeBytes = 60, 200<<20+1 }, CodeShortTooLarge},
		{"59.2 s rounds up to 60 and 300 MiB", func(i *Info) { i.DurationSeconds, i.SizeBytes = 59.2, 300<<20 }, CodeShortTooLarge},
	}
	for _, c := range cases {
		i := good()
		c.mut(i)
		err := p.Validate(i)
		r, ok := AsRejection(err)
		if !ok || r.Code != c.want {
			t.Errorf("%s: got %v, want code %s", c.name, err, c.want)
			continue
		}
		if r.Message == "" || r.Detail == "" {
			t.Errorf("%s: a rejection needs a fixed message and a log detail: %+v", c.name, r)
		}
	}
}

func TestPolicyIsConfigurable(t *testing.T) {
	p := DefaultPolicy()
	p.MaxDuration = 2 * time.Second
	p.Containers = []string{"matroska"}
	p.VideoCodecs = []string{"vp9"}
	i := good()
	i.DurationSeconds = 3
	if r, _ := AsRejection(p.Validate(i)); r == nil || r.Code != CodeUnsupportedMedia { // container is checked before duration
		t.Fatalf("%v", r)
	}
	i.Container = "matroska"
	if r, _ := AsRejection(p.Validate(i)); r == nil || r.Code != CodeUnsupportedCodec {
		t.Fatalf("%v", r)
	}
	i.Video.Codec = "vp9"
	if r, _ := AsRejection(p.Validate(i)); r == nil || r.Code != CodeTooLong {
		t.Fatalf("%v", r)
	}
}
