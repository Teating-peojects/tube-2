package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ffprobe format_name -> container. Anything else (hls, concat, image2, ...) is rejected before
// ffmpeg is allowed to decode it.
var allowedFormats = map[string]string{"mov,mp4,m4a,3gp,3g2,mj2": "mp4", "matroska,webm": "webm"}

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
	Streams       int    // number of streams of any kind
	SAR           string // sample aspect ratio as reported ("16:11"); Width is already display-corrected
}

type ffprobeOut struct {
	Streams []struct {
		CodecType    string            `json:"codec_type"`
		CodecName    string            `json:"codec_name"`
		Width        int               `json:"width"`
		Height       int               `json:"height"`
		SAR          string            `json:"sample_aspect_ratio"`
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
	p.Streams = len(o.Streams)
	seenVideo := false
	for _, s := range o.Streams {
		switch s.CodecType {
		case "video":
			if seenVideo || s.Disposition.AttachedPic == 1 {
				continue // cover art / second video track
			}
			seenVideo = true
			p.VideoCodec, p.Width, p.Height = s.CodecName, s.Width, s.Height
			// Non-square pixels (anamorphic DVD/broadcast sources): players show the display size, so
			// plan and encode for it, not for the stored pixel grid.
			if n, d, ok := parseRatio(s.SAR); ok && n != d {
				p.SAR = s.SAR
				p.Width = int(math.Round(float64(p.Width) * float64(n) / float64(d)))
			}
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

// parseRatio reads ffprobe's "16:11" (0:1 and 0:0 mean unknown).
func parseRatio(s string) (n, d int, ok bool) {
	a, b, found := strings.Cut(s, ":")
	if !found {
		return 0, 0, false
	}
	n, e1 := strconv.Atoi(a)
	d, e2 := strconv.Atoi(b)
	if e1 != nil || e2 != nil || n <= 0 || d <= 0 {
		return 0, 0, false
	}
	return n, d, true
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

// Validate applies the default policy (see Policy.Validate for the configurable one).
func (p Probe) Validate(size int64) (videoType string, seconds int, err *JobError) {
	return DefaultPolicy().Validate(p, size)
}

// probeTimeout bounds one ffprobe run; a variable so tests can shorten it. probeMaxOutput bounds the JSON it may print.
var probeTimeout = 60 * time.Second

const probeMaxOutput = 4 << 20

// probeArgs is the whole ffprobe command line. "file:" keeps a path from being read as an option or a protocol; the two
// whitelists mean a playlist, a concat script or a reference movie cannot make ffprobe open another file or the network.
func probeArgs(path string) []string {
	return []string{"-v", "error", "-probesize", "10000000", "-analyzeduration", "10000000",
		"-format_whitelist", demuxerWhitelist, "-protocol_whitelist", "file",
		"-print_format", "json", "-show_format", "-show_streams", "-i", "file:" + path}
}

func unreadable(detail string) *JobError {
	je := permanent("invalid_file", "That file could not be read as a video")
	je.Err = errors.New(detail)
	return je
}

// ProbeFile runs ffprobe on a local file. The child gets a scrubbed environment and its own process group, is
// killed at the timeout and may print only a bounded amount. Only the file protocol and the allowed demuxers are
// permitted, so a crafted "video" that is really a playlist cannot make ffprobe read other files or reach the
// network. A file whose declared structure is longer than the file itself is refused as damaged.
func ProbeFile(ctx context.Context, ffprobe, path, ext string) (Probe, error) {
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out := &boundedWriter{max: probeMaxOutput, stop: cancel}
	errOut := &tailWriter{max: 4096}
	cmd := exec.CommandContext(pctx, ffprobe, probeArgs(path)...)
	isolate(cmd)
	cmd.Stdout, cmd.Stderr = out, errOut
	runErr := cmd.Run()
	switch {
	case ctx.Err() != nil:
		return Probe{}, ctx.Err() // shutting down or cancelled: not the file's fault
	case out.exceeded:
		return Probe{}, unreadable("ffprobe output exceeded the limit")
	case errors.Is(pctx.Err(), context.DeadlineExceeded):
		return Probe{}, transient("ffprobe_failed", fmt.Errorf("ffprobe exceeded %s", probeTimeout))
	}
	if runErr != nil {
		var ee *exec.ExitError
		if asExit(runErr, &ee) {
			return Probe{}, unreadable("ffprobe failed: " + sanitizeDiag(errOut.String()))
		}
		return Probe{}, runErr
	}
	pr, err := ParseProbe(out.buf.Bytes(), ext)
	if err != nil {
		return Probe{}, err
	}
	if truncationHint(errOut.String()) {
		return Probe{}, unreadable("ffprobe reports a truncated file: " + sanitizeDiag(errOut.String()))
	}
	if st, err := os.Stat(path); err == nil {
		var bad bool
		var why string
		switch pr.Container {
		case "mp4", "mov":
			bad, why = isoTruncated(path, st.Size())
		case "webm", "mkv": // Matroska and WebM share one ffprobe format; the label depends on the extension
			bad, why = mkvTruncated(path, st.Size())
		}
		if bad {
			return Probe{}, unreadable("truncated or damaged container: " + why)
		}
	}
	return pr, nil
}
