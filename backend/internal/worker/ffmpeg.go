package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

const (
	segmentSeconds = 6
	thumbCount     = 5
	thumbMaxWidth  = 640
)

func asExit(err error, target **exec.ExitError) bool { return errors.As(err, target) }

// hlsArgs encodes one rendition as VOD HLS (MPEG-TS segments) into outDir.
//   - protocol_whitelist=file: the input can never pull in other files or network resources.
//   - map_metadata/chapters -1: GPS, device and edit-tool tags in the original never reach the CDN.
//   - keyframes are forced on a time grid, so every rendition has identical segment boundaries and
//     players can switch rendition cleanly.
func hlsArgs(in, outDir string, r Rendition, p Probe, threads int) []string {
	vf := fmt.Sprintf("scale=%d:%d,format=yuv420p", r.W, r.H)
	if p.FPS > 60 {
		vf = "fps=60," + vf
	}
	vk := strconv.Itoa(r.VideoKbps)
	a := []string{"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-protocol_whitelist", "file", "-i", in,
		"-map", "0:v:0", "-map_metadata", "-1", "-map_chapters", "-1", "-sn", "-dn",
		"-vf", vf,
		"-c:v", "libx264", "-preset", "veryfast", "-profile:v", "high", "-level:v", r.Level,
		"-b:v", vk + "k", "-maxrate", strconv.Itoa(r.VideoKbps*107/100) + "k", "-bufsize", strconv.Itoa(r.VideoKbps*2) + "k",
		"-sc_threshold", "0", "-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", segmentSeconds),
	}
	if p.HasAudio {
		a = append(a, "-map", "0:a:0", "-c:a", "aac", "-b:a", strconv.Itoa(r.AudioKbps)+"k", "-ac", "2", "-ar", "48000")
	} else {
		a = append(a, "-an")
	}
	if threads > 0 {
		a = append(a, "-threads", strconv.Itoa(threads))
	}
	return append(a,
		"-f", "hls", "-hls_time", strconv.Itoa(segmentSeconds), "-hls_playlist_type", "vod",
		"-hls_segment_type", "mpegts", "-hls_flags", "independent_segments",
		"-hls_segment_filename", outDir+"/seg_%05d.ts", outDir+"/index.m3u8")
}

// thumbArgs grabs one JPEG frame at the given time (fast seek before the input).
func thumbArgs(in, out string, at float64, w int) []string {
	return []string{"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-protocol_whitelist", "file", "-ss", strconv.FormatFloat(at, 'f', 3, 64), "-i", in,
		"-map", "0:v:0", "-map_metadata", "-1", "-frames:v", "1",
		"-vf", fmt.Sprintf("scale='min(%d,iw)':-2", w), "-q:v", "3", out}
}

// thumbTimes spreads thumbCount frames over the video (10%..90%), clamped away from the very end.
func thumbTimes(duration float64) []float64 {
	last := duration - 0.5
	if last < 0 {
		last = 0
	}
	out := make([]float64, thumbCount)
	for i := range out {
		t := duration * (0.1 + 0.2*float64(i))
		if t > last {
			t = last
		}
		out[i] = t
	}
	return out
}

// encodeTimeout scales with the video: generous, but a stuck ffmpeg can never hold a worker forever.
func encodeTimeout(duration float64) time.Duration {
	return 20*time.Minute + time.Duration(2*duration)*time.Second
}

// runFFmpeg runs a command and returns the tail of stderr on failure.
func runFFmpeg(ctx context.Context, bin string, args []string, timeout time.Duration) error {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, args...)
	detach(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err() // shutting down: not the file's fault
		}
		tail := stderr.Bytes()
		if len(tail) > 600 {
			tail = tail[len(tail)-600:]
		}
		if cctx.Err() != nil {
			return transient("ffmpeg_timeout", fmt.Errorf("timed out after %s", timeout))
		}
		return transient("ffmpeg_failed", fmt.Errorf("%w: %s", err, bytes.TrimSpace(tail)))
	}
	return nil
}
