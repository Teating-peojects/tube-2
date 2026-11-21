package worker

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	segName  = regexp.MustCompile(`^seg_\d{5}\.ts$`)
	streamRe = regexp.MustCompile(`^#EXT-X-STREAM-INF:(.*)$`)
)

// ValidatePlaylist checks a freshly encoded rendition directory BEFORE anything is uploaded:
// a well-formed VOD playlist, every referenced segment present and non-empty, segment lengths
// consistent with the configured duration, and a total that matches the source. It returns the
// segment count. A failure here means the encode is bad (never publish a partial package).
func ValidatePlaylist(dir string, sourceSec float64, segSec int) (int, error) {
	b, err := os.ReadFile(filepath.Join(dir, "index.m3u8"))
	if err != nil {
		return 0, fmt.Errorf("playlist missing: %w", err)
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "#EXTM3U" {
		return 0, fmt.Errorf("playlist does not start with #EXTM3U")
	}
	var total float64
	var segs int
	ended := false
	pending := -1.0
	for _, l := range lines[1:] {
		l = strings.TrimSpace(l)
		switch {
		case l == "":
		case l == "#EXT-X-ENDLIST":
			ended = true
		case strings.HasPrefix(l, "#EXTINF:"):
			d, err := strconv.ParseFloat(strings.TrimSuffix(strings.SplitN(strings.TrimPrefix(l, "#EXTINF:"), ",", 2)[0], ","), 64)
			if err != nil || d <= 0 || math.IsNaN(d) || d > float64(segSec)+2 {
				return 0, fmt.Errorf("bad segment duration %q (target %ds)", l, segSec)
			}
			pending = d
		case strings.HasPrefix(l, "#"):
		default:
			if pending < 0 {
				return 0, fmt.Errorf("segment %q has no #EXTINF", l)
			}
			if !segName.MatchString(l) { // relative, flat, no traversal: the stream gate serves exactly this shape
				return 0, fmt.Errorf("unexpected segment reference %q", l)
			}
			st, err := os.Stat(filepath.Join(dir, l))
			if err != nil || st.Size() == 0 {
				return 0, fmt.Errorf("segment %s missing or empty", l)
			}
			total += pending
			pending = -1
			segs++
		}
	}
	if !ended {
		return 0, fmt.Errorf("playlist has no #EXT-X-ENDLIST (incomplete encode)")
	}
	if segs == 0 {
		return 0, fmt.Errorf("playlist references no segments")
	}
	if tol := math.Max(2, sourceSec*0.10); math.Abs(total-sourceSec) > tol {
		return 0, fmt.Errorf("rendition is %.1fs long, source is %.1fs", total, sourceSec)
	}
	return segs, nil
}

// ValidateMaster checks the master playlist against the planned renditions: one variant per
// rendition, correct RESOLUTION, positive BANDWIDTH, relative <name>/index.m3u8 references.
func ValidateMaster(body string, plan []Rendition) error {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "#EXTM3U" {
		return fmt.Errorf("master does not start with #EXTM3U")
	}
	want := map[string]Rendition{}
	for _, r := range plan {
		want[r.Name+"/index.m3u8"] = r
	}
	seen := 0
	for i := 1; i < len(lines); i++ {
		m := streamRe.FindStringSubmatch(strings.TrimSpace(lines[i]))
		if m == nil {
			continue
		}
		if i+1 >= len(lines) {
			return fmt.Errorf("variant without a playlist reference")
		}
		ref := strings.TrimSpace(lines[i+1])
		r, ok := want[ref]
		if !ok {
			return fmt.Errorf("master references unknown playlist %q", ref)
		}
		attrs := m[1]
		if !strings.Contains(attrs, fmt.Sprintf("RESOLUTION=%dx%d", r.W, r.H)) {
			return fmt.Errorf("variant %s has the wrong RESOLUTION", ref)
		}
		bw := regexp.MustCompile(`BANDWIDTH=(\d+)`).FindStringSubmatch(attrs)
		if bw == nil || bw[1] == "0" {
			return fmt.Errorf("variant %s has no BANDWIDTH", ref)
		}
		delete(want, ref)
		seen++
	}
	if seen != len(plan) || len(want) != 0 {
		return fmt.Errorf("master lists %d of %d renditions", seen, len(plan))
	}
	return nil
}
