package inspect

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Prober runs ffprobe. The command is built from an argument array and never goes through a shell; the child gets an
// empty environment (no database or storage credentials), no stdin, a whitelist of demuxers and of protocols (so a
// playlist or "concat" file cannot make it read other files or the network), a bounded output and a hard timeout that
// kills its whole process group.
type Prober struct {
	Bin            string        // path or name of ffprobe (default "ffprobe")
	Timeout        time.Duration // default 30s
	MaxOutputBytes int           // default 4 MiB of JSON
	Containers     []string      // ffprobe demuxer names to allow, default the mov family and matroska/webm
}

const demuxers = "mov,mp4,m4a,3gp,3g2,mj2,matroska,webm"

// signature reports whether the first bytes look like a container the policy accepts. It only decides between
// "damaged" and "not a video at all" when ffprobe fails; it is never trusted to accept a file.
func signature(head []byte) bool {
	if len(head) >= 4 && bytes.Equal(head[:4], []byte{0x1A, 0x45, 0xDF, 0xA3}) {
		return true // EBML: Matroska / WebM
	}
	if len(head) >= 8 {
		switch string(head[4:8]) {
		case "ftyp", "moov", "mdat", "wide", "free", "skip", "pnot":
			return true // ISO base media / QuickTime
		}
	}
	return false
}

type limitWriter struct {
	buf      bytes.Buffer
	max      int
	exceeded bool
	stop     func()
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.max {
		if !w.exceeded {
			w.exceeded = true
			w.stop() // kill the child: it is producing more than any real file needs
		}
		return len(p), nil // swallow the rest; never grow
	}
	return w.buf.Write(p)
}

// probeArgs is the whole ffprobe command line. "file:" keeps a path from being read as a protocol or an option; the two
// whitelists mean that a playlist, a concat script or a reference movie cannot make ffprobe open another file or the network.
func probeArgs(abs, allowedDemuxers string) []string {
	return []string{
		"-hide_banner", "-v", "error",
		"-probesize", "10000000", "-analyzeduration", "10000000",
		"-format_whitelist", allowedDemuxers, "-protocol_whitelist", "file",
		"-print_format", "json", "-show_format", "-show_streams",
		"-i", "file:" + abs,
	}
}

// Probe inspects the file at path. It returns an *Info, an *Rejection (the file is refused), or a plain error
// for problems that are not the file's fault (ffprobe missing, context cancelled).
func (p *Prober) Probe(ctx context.Context, path string) (*Info, error) {
	bin := p.Bin
	if bin == "" {
		bin = "ffprobe"
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	maxOut := p.MaxOutputBytes
	if maxOut <= 0 {
		maxOut = 4 << 20
	}
	allow := demuxers
	if len(p.Containers) > 0 {
		allow = strings.Join(p.Containers, ",")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	head := make([]byte, 16)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	f.Close()
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("inspect: %s is not a regular file", filepath.Base(abs))
	}

	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out := &limitWriter{max: maxOut, stop: cancel}
	var stderr = &limitWriter{max: 64 << 10, stop: func() {}}
	cmd := exec.CommandContext(pctx, bin, probeArgs(abs, allow)...)
	cmd.Env = []string{"LC_ALL=C"}
	cmd.Stdin = nil
	cmd.Stdout, cmd.Stderr = out, stderr
	setProcessGroup(cmd)
	cmd.WaitDelay = 2 * time.Second
	runErr := cmd.Run()

	detail := Sanitize(stderr.buf.String(), abs, filepath.Dir(abs))
	switch {
	case ctx.Err() != nil:
		return nil, ctx.Err() // shutdown or job cancelled: not a verdict about the file
	case out.exceeded:
		return nil, reject(CodeUnsupportedMedia, "ffprobe output exceeded %d bytes", maxOut)
	case errors.Is(pctx.Err(), context.DeadlineExceeded):
		return nil, reject(CodeProcessingTimeout, "ffprobe exceeded %s", timeout)
	}
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		return nil, fmt.Errorf("inspect: cannot run ffprobe: %w", runErr)
	}
	if runErr != nil {
		if signature(head) {
			return nil, reject(CodeCorruptInput, "ffprobe failed: %s", detail)
		}
		return nil, reject(CodeUnsupportedMedia, "ffprobe failed: %s", detail)
	}
	info, err := Parse(out.buf.Bytes())
	if err != nil {
		return nil, reject(CodeCorruptInput, "unreadable ffprobe output: %v", err)
	}
	info.SizeBytes = st.Size()
	if truncationHint(stderr.buf.String()) {
		return nil, reject(CodeCorruptInput, "ffprobe reports a truncated file: %s", detail)
	}
	switch info.Container {
	case "mp4":
		if bad, why := isoTruncated(abs, info.SizeBytes); bad {
			return nil, reject(CodeCorruptInput, "truncated or damaged MP4: %s", why)
		}
	case "matroska":
		if bad, why := mkvTruncated(abs, info.SizeBytes); bad {
			return nil, reject(CodeCorruptInput, "truncated or damaged Matroska: %s", why)
		}
	}
	return info, nil
}
