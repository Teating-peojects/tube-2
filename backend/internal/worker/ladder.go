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

var rungs = []struct {
	name        string
	short, kbps int
	level       string
}{
	{"360p", 360, 800, "3.1"},
	{"480p", 480, 1400, "3.1"},
	{"720p", 720, 2800, "3.1"},
	{"1080p", 1080, 5000, "4.0"},
}

const audioKbps = 128

func even(n int) int {
	if n < 2 {
		return 2
	}
	return n &^ 1
}

// Plan picks the renditions for a source of w x h: every rung up to the source's short side, never
// upscaling. A source below 360p gets a single "360p" rendition at its native size.
func Plan(w, h int) []Rendition {
	short := min(w, h)
	mk := func(name string, target, kbps int, level string) Rendition {
		r := Rendition{Name: name, VideoKbps: kbps, AudioKbps: audioKbps, Level: level}
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
	for _, g := range rungs {
		if g.short <= short {
			out = append(out, mk(g.name, g.short, g.kbps, g.level))
		}
	}
	if len(out) == 0 {
		out = append(out, mk("360p", short, rungs[0].kbps, rungs[0].level))
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
