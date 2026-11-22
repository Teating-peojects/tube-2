package worker

import (
	"bytes"
	"os/exec"
	"sort"
	"strings"
)

// Everything the worker launches (ffprobe, ffmpeg, the upload scanner) reads files an uploader controls. These helpers
// keep such a child from seeing the worker's secrets and from growing the worker's memory.

// childEnv is the whole environment of a child process. The worker's own environment holds the database and storage
// credentials; none of it is passed on. extra are explicit KEY=VALUE additions (the scanner may need some).
func childEnv(extra ...string) []string {
	return append([]string{"LC_ALL=C", "PATH=/usr/local/bin:/usr/bin:/bin"}, extra...)
}

// isolate puts the child in its own process group (killed as a whole on cancel or timeout) and gives it a minimal
// environment.
func isolate(cmd *exec.Cmd, extraEnv ...string) {
	detach(cmd)
	cmd.Env = childEnv(extraEnv...)
}

// demuxerWhitelist is the list of ffmpeg demuxers a stored original may be opened with: exactly the formats
// allowedFormats accepts. Passed as -format_whitelist, it stops ffprobe/ffmpeg from even starting to parse a playlist,
// concat script or any other demuxer, whatever the file is called.
var demuxerWhitelist = func() string {
	var names []string
	for k := range allowedFormats {
		names = append(names, strings.Split(k, ",")...)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}()

// boundedWriter collects at most max bytes. When more arrives it calls stop (to kill the child) and drops the rest.
type boundedWriter struct {
	buf      bytes.Buffer
	max      int
	exceeded bool
	stop     func()
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.max {
		if !w.exceeded {
			w.exceeded = true
			if w.stop != nil {
				w.stop()
			}
		}
		return len(p), nil
	}
	return w.buf.Write(p)
}

// tailWriter keeps only the last max bytes written to it: enough to show why a tool failed, never more.
type tailWriter struct {
	max int
	buf []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) >= w.max {
		w.buf = append(w.buf[:0], p[len(p)-w.max:]...)
		return n, nil
	}
	w.buf = append(w.buf, p...)
	if over := len(w.buf) - w.max; over > 0 {
		copy(w.buf, w.buf[over:])
		w.buf = w.buf[:w.max]
	}
	return n, nil
}

func (w *tailWriter) String() string { return string(w.buf) }

// Limits are optional resource limits for a heavy child process (ffmpeg). The zero value means none.
type Limits struct {
	Nice     int // niceness of the child, 1-19 (a lower priority than the API and the database)
	MemoryMB int // address-space limit in MiB (0 = none; ffmpeg maps a lot of virtual memory, so choose generously)
}
