package worker

import (
	"fmt"
	"strings"
)

// Rendition is one rung of the HLS ladder. Names are the short side ("720p" for 1280x720 and for
// 720x1280), which is also what internal/playback allows in object keys.
type Rendition struct {
	Name      string
	W, H      int
	VideoKbps int
	AudioKbps int
	Level     string // H.264 level
}

func even(n int) int {
	if n < 2 {
		return 2
	}
	return n &^ 1
}

// Plan is Profile.Plan for the default profile.
func Plan(w, h int) []Rendition { return DefaultProfile().Plan(w, h) }

// Plan picks the renditions for a source of w x h (display size): every rung up to the source's short
// side, never upscaling. A source below the first rung gets a single rendition at its native size.
func (pr Profile) Plan(w, h int) []Rendition {
	short := min(w, h)
	mk := func(name string, target, kbps int, level string) Rendition {
		r := Rendition{Name: name, VideoKbps: kbps, AudioKbps: pr.AudioKbps, Level: level}
		if target > short {
			target = short
		}
		if w >= h { // landscape or square: the short side is the height
			r.H, r.W = even(target), even(w*target/h)
		} else {
			r.W, r.H = even(target), even(h*target/w)
		}
		return r
	}
	var out []Rendition
	for _, g := range pr.Rungs {
		if g.Short <= short {
			out = append(out, mk(g.Name, g.Short, g.Kbps, g.Level))
		}
	}
	if len(out) == 0 {
		g := pr.Rungs[0]
		out = append(out, mk(g.Name, short, g.Kbps, g.Level))
	}
	return out
}

func (r Rendition) bandwidth() int { return (r.VideoKbps + r.AudioKbps) * 1000 }

func (r Rendition) codecs(hasAudio bool) string {
	v := "avc1.64001f" // High, level 3.1
	if r.Level == "4.0" {
		v = "avc1.640028"
	}
	if hasAudio {
		return v + ",mp4a.40.2"
	}
	return v
}

// MasterPlaylist renders master.m3u8. Rendition playlists are relative (<name>/index.m3u8), so a
// player resolves them, and every segment, against the master's own URL (and its playback token).
func MasterPlaylist(rs []Rendition, hasAudio bool) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-INDEPENDENT-SEGMENTS\n")
	for _, r := range rs {
		fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d,CODECS=%q\n%s/index.m3u8\n",
			r.bandwidth(), r.W, r.H, r.codecs(hasAudio), r.Name)
	}
	return b.String()
}
