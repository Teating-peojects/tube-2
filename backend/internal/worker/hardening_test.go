package worker

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Tests for the subprocess confinement and the truncation checks (see subproc.go and container.go).

func fakeBin(t *testing.T, script string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

func validISO(t *testing.T) string { // a complete, tiny ftyp box: passes the box walk
	t.Helper()
	p := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(p, []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func asJob(t *testing.T, err error) *JobError {
	t.Helper()
	var je *JobError
	if !errors.As(err, &je) {
		t.Fatalf("want a *JobError, got %v", err)
	}
	return je
}

func TestChildProcessesGetNoSecretsFromTheWorkersEnvironment(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://secret")
	t.Setenv("STORAGE_SECRET_KEY", "secret")
	const count = `leaked=$(env | grep -c -E 'DATABASE_URL|STORAGE_SECRET_KEY'); locale=$(env | grep -c '^LC_ALL=')`

	t.Run("ffprobe", func(t *testing.T) {
		bin := fakeBin(t, count+"\nprintf '{\"format\":{\"format_name\":\"mov,mp4,m4a,3gp,3g2,mj2\",\"duration\":\"%s\",\"bit_rate\":\"%s\"},\"streams\":[]}' \"$leaked\" \"$locale\"\n")
		pr, err := ProbeFile(context.Background(), bin, validISO(t), ".mp4")
		if err != nil {
			t.Fatal(err)
		}
		if pr.Duration != 0 || pr.Bitrate != 1 {
			t.Fatalf("leaked=%v locale=%d: the child must get a controlled environment and nothing else", pr.Duration, pr.Bitrate)
		}
	})
	t.Run("ffmpeg", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "env.txt")
		bin := fakeBin(t, count+"\necho \"$leaked $locale\" > \"$1\"\n")
		if err := runFFmpeg(context.Background(), bin, []string{out}, 10*time.Second); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(out); strings.TrimSpace(string(b)) != "0 1" {
			t.Fatalf("leaked/locale = %q", b)
		}
	})
	t.Run("scanner, with an explicit pass-through", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "env.txt")
		bin := fakeBin(t, count+"\nextra=$(env | grep -c '^SCAN_DB=/db$')\necho \"$leaked $locale $extra\" > \"$1\"\n")
		sc := &ExecScanner{Args: []string{bin, "{file}"}, Timeout: 10 * time.Second, Env: []string{"SCAN_DB=/db"}}
		if _, _, err := sc.Scan(context.Background(), out); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(out); strings.TrimSpace(string(b)) != "0 1 1" {
			t.Fatalf("leaked/locale/extra = %q: only the listed variables may reach the scanner", b)
		}
	})
}

func TestAHungProbeIsKilledWithItsChildrenAtTheTimeout(t *testing.T) {
	old := probeTimeout
	probeTimeout = 400 * time.Millisecond
	t.Cleanup(func() { probeTimeout = old })
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	bin := fakeBin(t, "sleep 60 &\necho $! > '"+pidFile+"'\nwait\n")
	start := time.Now()
	_, err := ProbeFile(context.Background(), bin, validISO(t), ".mp4")
	if je := asJob(t, err); je.Code != "ffprobe_failed" || je.Permanent {
		t.Fatalf("a hung probe is retryable and carries the ffprobe code: %+v", je)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
	b, rerr := os.ReadFile(pidFile)
	if rerr != nil {
		t.Fatal(rerr)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
	}
	t.Fatalf("the child process %d of the hung probe survived", pid)
}

func TestAProbeThatFloodsItsOutputIsStopped(t *testing.T) {
	bin := fakeBin(t, "yes aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n") // never ends on its own
	start := time.Now()
	_, err := ProbeFile(context.Background(), bin, validISO(t), ".mp4")
	if je := asJob(t, err); je.Code != "invalid_file" || !je.Permanent {
		t.Fatalf("%+v", je)
	}
	if time.Since(start) > 8*time.Second {
		t.Fatalf("took %v: the flood was not cut off at the output limit", time.Since(start))
	}
}

func TestCancellationOfAProbeIsNotAVerdictAboutTheFile(t *testing.T) {
	bin := fakeBin(t, "sleep 30\n")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	if _, err := ProbeFile(ctx, bin, validISO(t), ".mp4"); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestTailWriterAndBoundedWriterNeverGrowWithoutLimit(t *testing.T) {
	tw := &tailWriter{max: 100}
	chunk := bytes.Repeat([]byte("x"), 7919)
	for i := 0; i < 2000; i++ {
		tw.Write(chunk)
	}
	tw.Write([]byte("THE-END"))
	if len(tw.buf) != 100 || cap(tw.buf) > 20000 || !strings.HasSuffix(tw.String(), "THE-END") {
		t.Fatalf("len %d cap %d tail %q", len(tw.buf), cap(tw.buf), tw.String()[90:])
	}
	stopped := 0
	bw := &boundedWriter{max: 1000, stop: func() { stopped++ }}
	for i := 0; i < 100; i++ {
		bw.Write(chunk)
	}
	if bw.buf.Len() > 1000 || !bw.exceeded || stopped != 1 {
		t.Fatalf("len %d exceeded %v stopped %d (stop must run exactly once)", bw.buf.Len(), bw.exceeded, stopped)
	}
}

func TestAnFFprobeTruncationMessageRejectsAnOtherwiseValidProbe(t *testing.T) {
	json := `printf '{"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"3"},"streams":[{"codec_type":"video","codec_name":"h264","width":64,"height":64,"avg_frame_rate":"25/1"}]}'`
	bad := fakeBin(t, "echo '[matroska,webm @ 0x1] File ended prematurely' >&2\n"+json+"\n")
	_, err := ProbeFile(context.Background(), bad, validISO(t), ".mp4")
	if je := asJob(t, err); je.Code != "invalid_file" || !je.Permanent {
		t.Fatalf("%+v", je)
	}
	if _, err := ProbeFile(context.Background(), fakeBin(t, json+"\n"), validISO(t), ".mp4"); err != nil {
		t.Fatalf("control: the same output without the message must be accepted: %v", err)
	}
}

func TestCommandLinesKeepBothWhitelistsAndTheFilePrefix(t *testing.T) {
	r := Plan(1920, 1080)[3]
	for name, args := range map[string][]string{
		"ffprobe": probeArgs("/w/original"),
		"hls":     hlsArgs("/w/original", "/out", r, Probe{HasAudio: true}, DefaultProfile(), 2),
		"thumb":   thumbArgs("/w/original", "/out/t.jpg", 1, 320),
	} {
		line := strings.Join(args, " ")
		for _, want := range []string{"-format_whitelist " + demuxerWhitelist, "-protocol_whitelist file", "-i file:/w/original"} {
			if !strings.Contains(line, want) {
				t.Errorf("%s: command line lacks %q:\n%s", name, want, line)
			}
		}
	}
	for _, f := range []string{"mov", "mp4", "matroska", "webm"} {
		if !strings.Contains(","+demuxerWhitelist+",", ","+f+",") {
			t.Errorf("demuxer whitelist lacks %q: %s", f, demuxerWhitelist)
		}
	}
	for _, banned := range []string{"hls", "concat", "image2", "rtsp", "http"} {
		if strings.Contains(","+demuxerWhitelist+",", ","+banned+",") {
			t.Errorf("demuxer whitelist must not contain %q", banned)
		}
	}
}

func TestTruncatedAndDamagedMediaIsRefusedAtProbeTime(t *testing.T) {
	needFFmpeg(t)
	// 640x360 at 25 fps with audio: large enough that a Matroska file cut to a third still opens in ffprobe, so only the
	// segment walk can notice (a smaller clip makes ffprobe itself fail and hides a missing walk).
	mp4 := makeVideo(t, "-f", "lavfi", "-i", "testsrc=size=640x360:rate=25", "-f", "lavfi", "-i", "sine=frequency=440",
		"-t", "3", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest")
	dir := t.TempDir()
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	src := write("src.mp4", mp4)
	mkvPath := filepath.Join(dir, "src.mkv")
	if out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-i", src, "-c", "copy", mkvPath).CombinedOutput(); err != nil {
		t.Fatalf("remux: %v %s", err, out)
	}
	mkv, _ := os.ReadFile(mkvPath)
	var moov, mdat int // box layout: find mdat for the "no index" file
	for off := 0; off+8 <= len(mp4); {
		sz := int(uint32(mp4[off])<<24 | uint32(mp4[off+1])<<16 | uint32(mp4[off+2])<<8 | uint32(mp4[off+3]))
		switch string(mp4[off+4 : off+8]) {
		case "moov":
			moov = off
		case "mdat":
			mdat = off
		}
		if sz < 8 {
			break
		}
		off += sz
	}
	_ = moov
	good := map[string]struct {
		data []byte
		ext  string
	}{
		"complete.mp4": {mp4, ".mp4"}, "complete.mkv": {mkv, ".mkv"}, "padded.mp4": {append(append([]byte{}, mp4...), make([]byte, 64)...), ".mp4"},
	}
	for name, c := range good {
		if _, err := ProbeFile(context.Background(), "ffprobe", write(name, c.data), c.ext); err != nil {
			t.Errorf("%s must be accepted: %v", name, err)
		}
	}
	bad := map[string]struct {
		data []byte
		ext  string
	}{
		"index in front, media data cut": {mp4[:len(mp4)/3], ".mp4"},
		"only the last 40 bytes missing": {mp4[:len(mp4)-40], ".mp4"},
		"no index at all":                {append(append([]byte{}, mp4[:32]...), mp4[mdat:]...), ".mp4"},
		"mkv cut to a third":             {mkv[:len(mkv)/3], ".mkv"},
		"header followed by garbage":     {append([]byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom"), bytes.Repeat([]byte{0xde, 0xad, 0xbe, 0xef}, 900)...), ".mp4"},
		"empty":                          {nil, ".mp4"},
	}
	for name, c := range bad {
		_, err := ProbeFile(context.Background(), "ffprobe", write(strings.ReplaceAll(name, " ", "_")+c.ext, c.data), c.ext)
		if err == nil {
			t.Errorf("%s: accepted, want a permanent invalid_file", name)
			continue
		}
		if je := asJob(t, err); je.Code != "invalid_file" || !je.Permanent {
			t.Errorf("%s: got %+v, want a permanent invalid_file", name, je)
		}
	}
}

func TestBoxWalkOnHandMadeFiles(t *testing.T) {
	box := func(typ string, payload int) []byte {
		b := make([]byte, 8+payload)
		n := 8 + payload
		b[0], b[1], b[2], b[3] = byte(n>>24), byte(n>>16), byte(n>>8), byte(n)
		copy(b[4:], typ)
		return b
	}
	cat := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
	big := func(declared uint64) []byte {
		b := make([]byte, 16)
		b[3] = 1
		copy(b[4:], "mdat")
		for i := 0; i < 8; i++ {
			b[8+i] = byte(declared >> (56 - 8*i))
		}
		return b
	}
	for name, c := range map[string]struct {
		data []byte
		bad  bool
	}{
		"complete":                      {cat(box("ftyp", 16), box("moov", 40), box("mdat", 100)), false},
		"last box one byte short":       {cat(box("ftyp", 16), box("moov", 40), box("mdat", 100))[:8+16+8+40+8+99], true},
		"header cut off":                {cat(box("ftyp", 16), []byte{0, 0, 0})[:8+16+3], true},
		"size 0 runs to the end":        {cat(box("ftyp", 16), []byte{0, 0, 0, 0, 'm', 'd', 'a', 't', 1, 2, 3}), false},
		"box smaller than its header":   {cat(box("ftyp", 16), []byte{0, 0, 0, 4, 'f', 'r', 'e', 'e'}), true},
		"64-bit size beyond the file":   {cat(box("ftyp", 16), big(1<<40)), true},
		"64-bit size overflowing int64": {cat(box("ftyp", 16), big(^uint64(0))), true},
		"64-bit size that fits":         {cat(box("ftyp", 16), big(16+10), make([]byte, 10)), false},
		"thousands of empty boxes":      {bytes.Repeat(box("free", 0), 100002), true},
	} {
		p := filepath.Join(t.TempDir(), "f.mp4")
		if err := os.WriteFile(p, c.data, 0o600); err != nil {
			t.Fatal(err)
		}
		if bad, why := isoTruncated(p, int64(len(c.data))); bad != c.bad {
			t.Errorf("%s: truncated=%v (%s), want %v", name, bad, why, c.bad)
		}
	}
}

func TestEBMLSegmentWalkOnHandMadeFiles(t *testing.T) {
	ebml := []byte{0x1A, 0x45, 0xDF, 0xA3, 0x84, 0x42, 0x86, 0x81, 0x01}
	seg := func(size []byte, payload int) []byte {
		return append(append(append([]byte{}, ebml...), append([]byte{0x18, 0x53, 0x80, 0x67}, size...)...), make([]byte, payload)...)
	}
	size8 := func(v uint64) []byte {
		b := []byte{0x01, 0, 0, 0, 0, 0, 0, 0}
		for i := 1; i < 8; i++ {
			b[i] = byte(v >> (8 * (7 - i)))
		}
		return b
	}
	for name, c := range map[string]struct {
		data []byte
		bad  bool
	}{
		"segment fits exactly":    {seg([]byte{0x8A}, 10), false},
		"segment one byte short":  {seg([]byte{0x8A}, 9), true},
		"unknown segment size":    {seg([]byte{0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, 5), false},
		"huge claimed segment":    {seg(size8(1<<50), 5), true},
		"not matroska":            {[]byte("hello world, this is text"), false},
		"truncated inside header": {ebml[:6], true},
		"zero size byte":          {[]byte{0x1A, 0x45, 0xDF, 0xA3, 0x00, 0x00}, true},
	} {
		p := filepath.Join(t.TempDir(), "f.mkv")
		if err := os.WriteFile(p, c.data, 0o600); err != nil {
			t.Fatal(err)
		}
		if bad, why := mkvTruncated(p, int64(len(c.data))); bad != c.bad {
			t.Errorf("%s: truncated=%v (%s), want %v", name, bad, why, c.bad)
		}
	}
}

func TestTheLocalOriginalHasNoExtensionAndPrivatePermissions(t *testing.T) {
	const vid = "0190cccc-dddd-7eee-8fff-444444444444"
	mem := newMem()
	key := "uploads/" + vid + "/original.mov"
	mem.objs[key] = bytes.Repeat([]byte("v"), 2048)
	p := newPipe(t, &fakeStore{video: VideoRow{OriginalKey: key, Size: 2048}}, mem)
	path, _, err := p.original(context.Background(), vid, VideoRow{OriginalKey: key, Size: 2048}, true)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "original" || filepath.Ext(path) != "" {
		t.Fatalf("what the uploader called the file must not steer the demuxer, got %q", path)
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0o600 {
		t.Fatalf("the local original must be a regular file readable only by the worker: %v %v", st, err)
	}
	if ents, _ := os.ReadDir(filepath.Dir(path)); len(ents) != 1 {
		t.Fatalf("a partial download file was left behind: %d entries", len(ents))
	}
}
