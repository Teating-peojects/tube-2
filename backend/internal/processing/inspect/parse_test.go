package inspect

import (
	"math"
	"regexp"
	"strings"
	"testing"
)

const std = `{"streams":[
 {"index":0,"codec_type":"video","codec_name":"h264","profile":"High","width":640,"height":360,"pix_fmt":"yuv420p","r_frame_rate":"25/1","avg_frame_rate":"25/1","sample_aspect_ratio":"1:1","bit_rate":"400000"},
 {"index":1,"codec_type":"audio","codec_name":"aac","sample_rate":"44100","channels":2,"channel_layout":"stereo","bit_rate":"128000"}],
 "format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"3.040000","bit_rate":"520000"}}`

func mustParse(t *testing.T, js string) *Info {
	t.Helper()
	i, err := Parse([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func TestParseStandardFile(t *testing.T) {
	i := mustParse(t, std)
	if i.Container != "mp4" || i.Streams != 2 || i.VideoStreams != 1 || i.AudioStreams != 1 || i.DurationSeconds != 3.04 || i.BitRate != 520000 {
		t.Fatalf("%+v", i)
	}
	v := i.Video
	if v.Codec != "h264" || v.Width != 640 || v.Height != 360 || v.DisplayWidth != 640 || v.DisplayHeight != 360 || v.FPS != 25 || v.Rotation != 0 || v.BitRate != 400000 {
		t.Fatalf("%+v", v)
	}
	if a := i.Audio; a.Codec != "aac" || a.SampleRate != 44100 || a.Channels != 2 || a.Layout != "stereo" {
		t.Fatalf("%+v", a)
	}
	if i.DurationSecondsInt() != 4 || i.VideoType() != "SHORT" {
		t.Fatalf("duration %d type %s", i.DurationSecondsInt(), i.VideoType())
	}
}

func TestParseRotationFromSideDataAndTag(t *testing.T) {
	cases := []struct {
		name, extra string
		want        int
	}{
		{"display matrix -90 (ccw) = 90 clockwise", `"side_data_list":[{"side_data_type":"Display Matrix","rotation":-90}]`, 90},
		{"display matrix +90 (ccw) = 270 clockwise", `"side_data_list":[{"side_data_type":"Display Matrix","rotation":90}]`, 270},
		{"display matrix 180", `"side_data_list":[{"side_data_type":"Display Matrix","rotation":-180}]`, 180},
		{"legacy rotate tag", `"tags":{"rotate":"90"}`, 90},
		{"legacy rotate tag 270", `"tags":{"rotate":"270"}`, 270},
		{"odd angle snaps to a quarter turn", `"tags":{"rotate":"88"}`, 90},
		{"garbage tag", `"tags":{"rotate":"banana"}`, 0},
		{"none", `"tags":{}`, 0},
	}
	for _, c := range cases {
		js := `{"streams":[{"index":0,"codec_type":"video","codec_name":"h264","width":640,"height":360,"avg_frame_rate":"25/1",` + c.extra + `}],"format":{"format_name":"mov,mp4","duration":"2"}}`
		v := mustParse(t, js).Video
		if v.Rotation != c.want {
			t.Errorf("%s: rotation %d, want %d", c.name, v.Rotation, c.want)
		}
		wantW, wantH := 640, 360
		if c.want == 90 || c.want == 270 {
			wantW, wantH = 360, 640
		}
		if v.DisplayWidth != wantW || v.DisplayHeight != wantH {
			t.Errorf("%s: display %dx%d, want %dx%d", c.name, v.DisplayWidth, v.DisplayHeight, wantW, wantH)
		}
	}
}

func TestParseSampleAspectRatio(t *testing.T) {
	for _, c := range []struct {
		sar          string
		wantW, wantH int
	}{
		{"1:1", 720, 480}, {"0:1", 720, 480}, {"N/A", 720, 480}, {"", 720, 480}, {"0:0", 720, 480},
		{"32:27", 853, 480}, {"2:1", 1440, 480}, {"1:2", 720, 960}, {"8:9", 720, 540},
	} {
		js := `{"streams":[{"index":0,"codec_type":"video","codec_name":"h264","width":720,"height":480,"avg_frame_rate":"25/1","sample_aspect_ratio":"` + c.sar + `"}],"format":{"format_name":"mov,mp4","duration":"2"}}`
		v := mustParse(t, js).Video
		if v.DisplayWidth != c.wantW || v.DisplayHeight != c.wantH {
			t.Errorf("SAR %q: display %dx%d, want %dx%d", c.sar, v.DisplayWidth, v.DisplayHeight, c.wantW, c.wantH)
		}
	}
}

func TestParseFrameRateFallbacks(t *testing.T) {
	for _, c := range []struct {
		avg, r string
		want   float64
	}{
		{"25/1", "90000/1", 25}, // variable frame rate: r_frame_rate is a timebase artefact
		{"0/0", "30000/1001", 30000.0 / 1001}, {"", "24/1", 24}, {"N/A", "N/A", 0}, {"0/0", "0/0", 0}, {"30/0", "", 0},
	} {
		js := `{"streams":[{"index":0,"codec_type":"video","codec_name":"h264","width":640,"height":360,"avg_frame_rate":"` + c.avg + `","r_frame_rate":"` + c.r + `"}],"format":{"format_name":"mov,mp4","duration":"2"}}`
		if got := mustParse(t, js).Video.FPS; math.Abs(got-c.want) > 1e-9 {
			t.Errorf("avg %q r %q: fps %v, want %v", c.avg, c.r, got, c.want)
		}
	}
}

func TestParseCoverArtIsNotAVideoStreamAndDurationFallback(t *testing.T) {
	js := `{"streams":[{"index":0,"codec_type":"audio","codec_name":"mp3","duration":"180.5"},
 {"index":1,"codec_type":"video","codec_name":"mjpeg","width":500,"height":500,"disposition":{"attached_pic":1}}],"format":{"format_name":"mp3","duration":"N/A"}}`
	i := mustParse(t, js)
	if i.Video != nil || i.VideoStreams != 0 || i.AudioStreams != 1 || i.Streams != 2 {
		t.Fatalf("%+v", i)
	}
	if i.DurationSeconds != 180.5 {
		t.Fatalf("duration should fall back to the stream value, got %v", i.DurationSeconds)
	}
}

func TestParseToleratesJunkValues(t *testing.T) {
	js := `{"streams":[{"index":0,"codec_type":"video","codec_name":"h264","width":-5,"height":0,"bit_rate":"N/A","avg_frame_rate":"abc"}],"format":{"format_name":"","duration":"-3","bit_rate":"-1"}}`
	i := mustParse(t, js)
	if i.Container != "" || i.DurationSeconds != 0 || i.BitRate != 0 || i.Video.BitRate != 0 || i.Video.FPS != 0 {
		t.Fatalf("%+v %+v", i, i.Video)
	}
	for _, bad := range []string{"", "not json", "{", "[]"} {
		if _, err := Parse([]byte(bad)); err == nil && bad != "[]" {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
	if i := mustParse(t, `{"streams":[],"format":{"duration":"NaN"}}`); i.DurationSeconds != 0 {
		t.Fatalf("NaN duration must become 0, got %v", i.DurationSeconds)
	}
	if i := mustParse(t, `{"streams":[],"format":{"duration":"1e999"}}`); i.DurationSeconds != 0 {
		t.Fatalf("infinite duration must become 0, got %v", i.DurationSeconds)
	}
}

func TestVideoTypeUsesTheSameIntegerAsTheDatabase(t *testing.T) {
	for _, c := range []struct {
		d   float64
		sec int
		typ string
	}{{0.2, 1, "SHORT"}, {59.2, 60, "SHORT"}, {60, 60, "SHORT"}, {60.01, 61, "LONG"}, {61, 61, "LONG"}, {3600, 3600, "LONG"}} {
		i := &Info{DurationSeconds: c.d}
		if i.DurationSecondsInt() != c.sec || i.VideoType() != c.typ {
			t.Errorf("%v s -> %d %s, want %d %s", c.d, i.DurationSecondsInt(), i.VideoType(), c.sec, c.typ)
		}
	}
}

func TestEveryCodeIsSafeForTheDatabaseAndHasAFixedMessage(t *testing.T) {
	re := regexp.MustCompile(`^[a-z][a-z_]{0,39}$`) // the CHECK on video_processing_jobs.error_code
	seen := map[string]bool{}
	for _, c := range AllCodes {
		if !re.MatchString(c) || seen[c] || messages[c] == "" {
			t.Errorf("bad or duplicate code %q", c)
		}
		seen[c] = true
	}
	if len(messages) != len(AllCodes) {
		t.Errorf("%d messages for %d codes", len(messages), len(AllCodes))
	}
	r := reject(CodeCorruptInput, "secret detail /tmp/job-1/input")
	if strings.Contains(r.Error(), "secret") || strings.Contains(r.Message, "/tmp") {
		t.Fatalf("detail must never leak into the error text: %q", r.Error())
	}
}

func TestSanitize(t *testing.T) {
	got := Sanitize("open /tmp/job-7/input: bad\x00data\x1b[31m\n\n  next\tline", "/tmp/job-7/input", "/tmp/job-7")
	if strings.Contains(got, "/tmp") || strings.ContainsAny(got, "\x00\x1b\n\t") || !strings.Contains(got, "<path>") || !strings.Contains(got, "next line") {
		t.Fatalf("%q", got)
	}
	if long := Sanitize(strings.Repeat("a", 5000)); len(long) > 401 {
		t.Fatalf("not truncated: %d", len(long))
	}
}
