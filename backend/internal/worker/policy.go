package worker

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Policy is what an original must satisfy before it is transcoded. Every field is configurable
// (see cmd/worker) except where the database enforces the same limit (SHORT <= 60 s / 200 MiB,
// <= 1080p source): those can be lowered, never raised.
type Policy struct {
	MaxDurationSec int
	ShortMaxSec    int
	ShortMaxBytes  int64
	LongMaxBytes   int64
	MaxShortSide   int
	MaxLongSide    int
	MinSide        int
	MaxFPS         float64
	MaxStreams     int
	VideoCodecs    map[string]bool
	// MinFreeBytes is the free scratch space that must remain after a download (it also covers the
	// renditions): a job waits (retries) instead of filling the disk.
	MinFreeBytes uint64
}

// Defaults: the product limits (60 min, 5 GiB, 1080p) with sane guards for hostile input.
func DefaultPolicy() Policy {
	return Policy{
		MaxDurationSec: 60 * 60, ShortMaxSec: 60, ShortMaxBytes: 200 << 20, LongMaxBytes: int64(5) << 30,
		MaxShortSide: 1080, MaxLongSide: 1920, MinSide: 16, MaxFPS: 120, MaxStreams: 16,
		VideoCodecs:  map[string]bool{"h264": true, "hevc": true, "vp8": true, "vp9": true, "av1": true},
		MinFreeBytes: 1 << 30,
	}
}

// Check rejects a policy the database or the pipeline cannot honour.
func (po Policy) Check() error {
	switch {
	case po.MaxDurationSec < 1 || po.MaxDurationSec > 24*3600:
		return fmt.Errorf("max duration must be 1s..24h")
	case po.ShortMaxSec < 1 || po.ShortMaxSec > 60:
		return fmt.Errorf("short max seconds must be 1..60 (database limit)")
	case po.ShortMaxBytes < 1 || po.ShortMaxBytes > 200<<20:
		return fmt.Errorf("short max bytes must be at most 200 MiB (database limit)")
	case po.LongMaxBytes < 1 || po.LongMaxBytes > int64(5)<<30:
		return fmt.Errorf("long max bytes must be at most 5 GiB (database limit)")
	case po.MaxShortSide < po.MinSide || po.MaxShortSide > 1080:
		return fmt.Errorf("max short side must be %d..1080 (database limit)", po.MinSide)
	case po.MaxLongSide < po.MaxShortSide || po.MaxLongSide > 1920:
		return fmt.Errorf("max long side must be %d..1920 (database limit)", po.MaxShortSide)
	case po.MaxFPS < 1 || po.MaxFPS > 240:
		return fmt.Errorf("max fps must be 1..240 (database limit)")
	case po.MaxStreams < 1 || po.MaxStreams > 256:
		return fmt.Errorf("max streams must be 1..256")
	case len(po.VideoCodecs) == 0:
		return fmt.Errorf("at least one video codec must be allowed")
	}
	return nil
}

// Validate applies the policy to a probed original. size is the verified byte size. It returns the
// video type (SHORT/LONG) and the duration in whole seconds (rounded up, so a 60.4 s clip is LONG,
// never a SHORT that overruns the limit). Every error is permanent and carries a user-safe message.
func (po Policy) Validate(p Probe, size int64) (videoType string, seconds int, err *JobError) {
	switch {
	case p.Container == "":
		return "", 0, permanent("unsupported_container", "Only MP4, MOV, MKV and WebM files are supported")
	case p.Streams > po.MaxStreams:
		return "", 0, permanent("too_many_streams", "That file has too many streams")
	case p.VideoCodec == "" || p.Width <= 0 || p.Height <= 0:
		return "", 0, permanent("no_video_stream", "That file does not contain a video track")
	case !po.VideoCodecs[p.VideoCodec]:
		return "", 0, permanent("unsupported_codec", "That video codec is not supported")
	case math.IsNaN(p.Duration) || p.Duration <= 0:
		return "", 0, permanent("invalid_duration", "We couldn't read the length of that video")
	case p.Duration > float64(po.MaxDurationSec):
		return "", 0, permanent("too_long", "That video is longer than the maximum of "+humanDuration(po.MaxDurationSec))
	case min(p.Width, p.Height) > po.MaxShortSide || max(p.Width, p.Height) > po.MaxLongSide:
		return "", 0, permanent("resolution_too_high", fmt.Sprintf("Maximum resolution is %dx%d", po.MaxLongSide, po.MaxShortSide))
	case min(p.Width, p.Height) < po.MinSide:
		return "", 0, permanent("resolution_too_low", "That video is too small")
	case p.FPS > po.MaxFPS:
		return "", 0, permanent("frame_rate_too_high", fmt.Sprintf("Maximum frame rate is %.0f fps", po.MaxFPS))
	}
	seconds = max(1, int(math.Ceil(p.Duration)))
	if seconds <= po.ShortMaxSec {
		if size > po.ShortMaxBytes {
			return "", 0, permanent("short_too_large", fmt.Sprintf("Videos up to %d seconds can be at most %d MB", po.ShortMaxSec, po.ShortMaxBytes>>20))
		}
		return "SHORT", seconds, nil
	}
	if size > po.LongMaxBytes {
		return "", 0, permanent("too_large", fmt.Sprintf("Videos can be at most %d GB", po.LongMaxBytes>>30))
	}
	return "LONG", seconds, nil
}

func humanDuration(sec int) string {
	switch {
	case sec%3600 == 0:
		return strconv.Itoa(sec/3600) + " hour(s)"
	case sec%60 == 0:
		return strconv.Itoa(sec/60) + " minutes"
	}
	return strconv.Itoa(sec) + " seconds"
}

// Rung is one step of the encoding ladder. Name is the short side; the database enum allows
// exactly 360p, 480p, 720p and 1080p.
type Rung struct {
	Name        string
	Short, Kbps int
	Level       string
}

// Profile is the versioned encoding recipe. Version is stored on every video it produced
// (videos.processing_profile), so a future change of settings stays traceable.
type Profile struct {
	Version        string
	Preset         string
	SegmentSeconds int
	AudioKbps      int
	MaxOutFPS      float64 // sources above this are resampled
	Rungs          []Rung
	ThumbCount     int
	ThumbWidth     int
}

func DefaultProfile() Profile {
	return Profile{
		Version: "h264-aac-hls-v1", Preset: "veryfast", SegmentSeconds: 6, AudioKbps: 128, MaxOutFPS: 60,
		Rungs: []Rung{
			{"360p", 360, 800, "3.1"}, {"480p", 480, 1400, "3.1"}, {"720p", 720, 2800, "3.1"}, {"1080p", 1080, 5000, "4.0"},
		},
		ThumbCount: 5, ThumbWidth: 640,
	}
}

var presets = map[string]bool{"ultrafast": true, "superfast": true, "veryfast": true, "faster": true, "fast": true, "medium": true, "slow": true}

func (pr Profile) Check() error {
	if pr.Version == "" || len(pr.Version) > 64 {
		return fmt.Errorf("profile version must be 1..64 characters")
	}
	if !presets[pr.Preset] {
		return fmt.Errorf("unknown x264 preset %q", pr.Preset)
	}
	if pr.SegmentSeconds < 2 || pr.SegmentSeconds > 10 {
		return fmt.Errorf("segment seconds must be 2..10")
	}
	if pr.AudioKbps < 32 || pr.AudioKbps > 320 || pr.MaxOutFPS < 10 || pr.MaxOutFPS > 120 {
		return fmt.Errorf("audio kbps must be 32..320 and max output fps 10..120")
	}
	if pr.ThumbCount < 1 || pr.ThumbCount > 6 || pr.ThumbWidth < 160 || pr.ThumbWidth > 1920 {
		return fmt.Errorf("thumbnails: 1..6 frames, width 160..1920")
	}
	if len(pr.Rungs) == 0 {
		return fmt.Errorf("the ladder needs at least one rung")
	}
	prev := 0
	for _, r := range pr.Rungs {
		if r.Name != strconv.Itoa(r.Short)+"p" || (r.Short != 360 && r.Short != 480 && r.Short != 720 && r.Short != 1080) {
			return fmt.Errorf("rung %q: only 360p, 480p, 720p and 1080p exist in the database", r.Name)
		}
		if r.Short <= prev || r.Kbps < 100 || r.Kbps > 20000 {
			return fmt.Errorf("rung %s: rungs must ascend and use 100..20000 kbps", r.Name)
		}
		prev = r.Short
	}
	return nil
}

// ParseLadder reads "360:800,480:1400,720:2800,1080:5000" (short side : video kbps).
func ParseLadder(s string) ([]Rung, error) {
	var out []Rung
	for _, part := range strings.Split(s, ",") {
		a, b, ok := strings.Cut(strings.TrimSpace(part), ":")
		short, e1 := strconv.Atoi(a)
		kbps, e2 := strconv.Atoi(b)
		if !ok || e1 != nil || e2 != nil {
			return nil, fmt.Errorf("ladder entries look like 720:2800 (got %q)", part)
		}
		level := "3.1"
		if short >= 1080 {
			level = "4.0"
		}
		out = append(out, Rung{Name: strconv.Itoa(short) + "p", Short: short, Kbps: kbps, Level: level})
	}
	return out, nil
}
