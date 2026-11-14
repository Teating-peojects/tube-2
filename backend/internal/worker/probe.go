package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

// Product limits, enforced here regardless of what the browser claimed.
const (
	MaxDurationSec = 60 * 60        // 60 minutes
	ShortMaxSec    = 60             // SHORT <= 60 s, LONG > 60 s
	ShortMaxBytes  = 200 << 20      // 200 MiB
	LongMaxBytes   = int64(5) << 30 // 5 GiB
	MaxShortSide   = 1080           // "max 1080p": short side <= 1080 and long side <= 1920
	MaxLongSide    = 1920
)

var (
	allowedVideoCodecs = map[string]bool{"h264": true, "hevc": true, "vp8": true, "vp9": true, "av1": true}
	// ffprobe format_name -> container. Anything else (hls, concat, image2, ...) is rejected before
	// ffmpeg is allowed to decode it.
	allowedFormats = map[string]string{"mov,mp4,m4a,3gp,3g2,mj2": "mp4", "matroska,webm": "webm"}
)

// Probe is what the pipeline needs to know about the original.
type Probe struct {
	Width, Height int // after applying the rotation flag
	Duration      float64
	FPS           float64
	VideoCodec    string
	AudioCodec    string
	FormatName    string
	Container     string // mp4 | mov | webm
	Bitrate       int64
	HasAudio      bool
}

type ffprobeOut struct {
	Streams []struct {
		CodecType    string            `json:"codec_type"`
		CodecName    string            `json:"codec_name"`
		Width        int               `json:"width"`
		Height       int               `json:"height"`
		AvgFrameRate string            `json:"avg_frame_rate"`
		RFrameRate   string            `json:"r_frame_rate"`
		Tags         map[string]string `json:"tags"`
		Disposition  struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
		SideData []struct {
			Rotation float64 `json:"rotation"`
		} `json:"side_data_list"`
	} `json:"streams"`
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`
}

// ParseProbe reads `ffprobe -print_format json -show_format -show_streams` output.
func ParseProbe(raw []byte, ext string) (Probe, error) {
	var o ffprobeOut
	if err := json.Unmarshal(raw, &o); err != nil {
		return Probe{}, fmt.Errorf("ffprobe json: %w", err)
	}
	p := Probe{FormatName: o.Format.FormatName}
	p.Duration, _ = strconv.ParseFloat(o.Format.Duration, 64)
	p.Bitrate, _ = strconv.ParseInt(o.Format.BitRate, 10, 64)
	if c, ok := allowedFormats[p.FormatName]; ok {
		p.Container = c
		switch {
		case c == "mp4" && strings.EqualFold(ext, ".mov"):
			p.Container = "mov"
		case c == "webm" && !strings.EqualFold(ext, ".webm"):
			p.Container = "mkv" // matroska and webm share ffprobe's format name: the extension tells them apart
		}
	}
	seenVideo := false
	for _, s := range o.Streams {
		switch s.CodecType {
		case "video":
			if seenVideo || s.Disposition.AttachedPic == 1 {
				continue // cover art / second video track
			}
			seenVideo = true
			p.VideoCodec, p.Width, p.Height = s.CodecName, s.Width, s.Height
			p.FPS = parseRate(s.AvgFrameRate)
			if p.FPS == 0 {
				p.FPS = parseRate(s.RFrameRate)
			}
			rot := 0.0
			if len(s.SideData) > 0 {
				rot = s.SideData[0].Rotation
			} else if t, ok := s.Tags["rotate"]; ok {
				rot, _ = strconv.ParseFloat(t, 64)
			}
			if r := int(math.Abs(rot)) % 180; r == 90 {
				p.Width, p.Height = p.Height, p.Width
			}
		case "audio":
			if !p.HasAudio {
				p.HasAudio, p.AudioCodec = true, s.CodecName
			}
		}
	}
	if !seenVideo {
		return p, nil // Validate reports no_video_stream
	}
	return p, nil
}

func parseRate(s string) float64 {
	n, d, ok := strings.Cut(s, "/")
	if !ok {
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	a, _ := strconv.ParseFloat(n, 64)
	b, _ := strconv.ParseFloat(d, 64)
	if b == 0 {
		return 0
	}
	return a / b
}

// Validate applies the product limits to a probed original. size is the verified byte size.
// It returns the video type (SHORT/LONG) and the duration in whole seconds (rounded up, so a
// 60.4 s clip is LONG, never a SHORT that overruns the limit).
func (p Probe) Validate(size int64) (videoType string, seconds int, err *JobError) {
	switch {
	case p.Container == "":
		return "", 0, permanent("unsupported_container", "Only MP4, MOV and WebM files are supported")
	case p.VideoCodec == "" || p.Width <= 0 || p.Height <= 0:
		return "", 0, permanent("no_video_stream", "That file does not contain a video track")
	case !allowedVideoCodecs[p.VideoCodec]:
		return "", 0, permanent("unsupported_codec", "That video codec is not supported")
	case math.IsNaN(p.Duration) || p.Duration <= 0:
		return "", 0, permanent("invalid_duration", "We couldn't read the length of that video")
	case p.Duration > MaxDurationSec:
		return "", 0, permanent("too_long", "Videos can be at most 60 minutes long")
	case min(p.Width, p.Height) > MaxShortSide || max(p.Width, p.Height) > MaxLongSide:
		return "", 0, permanent("resolution_too_high", "Maximum resolution is 1080p (1920x1080)")
	case min(p.Width, p.Height) < 16:
		return "", 0, permanent("resolution_too_low", "That video is too small")
	}
	seconds = max(1, int(math.Ceil(p.Duration)))
	if seconds <= ShortMaxSec {
		if size > ShortMaxBytes {
			return "", 0, permanent("short_too_large", "Videos up to 60 seconds can be at most 200 MB")
		}
		return "SHORT", seconds, nil
	}
	if size > LongMaxBytes {
		return "", 0, permanent("too_large", "Videos can be at most 5 GB")
	}
	return "LONG", seconds, nil
}

// ProbeFile runs ffprobe on a local file. Only the file protocol is allowed, so a crafted
// "video" that is really a playlist cannot make ffprobe read other files or reach the network.
func ProbeFile(ctx context.Context, ffprobe, path, ext string) (Probe, error) {
	cmd := exec.CommandContext(ctx, ffprobe, "-v", "error", "-protocol_whitelist", "file",
		"-print_format", "json", "-show_format", "-show_streams", path)
	detach(cmd)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if asExit(err, &ee) {
			return Probe{}, permanent("invalid_file", "That file could not be read as a video")
		}
		return Probe{}, err
	}
	return ParseProbe(out, ext)
}
