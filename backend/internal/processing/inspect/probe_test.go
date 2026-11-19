package inspect

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// ---- fixtures: generated with ffmpeg's lavfi sources, so no binary files live in the repository ----

var (
	fxOnce sync.Once
	fxDir  string
	fxErr  error
)

func ffmpeg(args ...string) error {
	out, err := exec.Command("ffmpeg", append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg %v: %v\n%s", args, err, out)
	}
	return nil
}

const h264 = "-c:v libx264 -preset ultrafast -pix_fmt yuv420p"

func fx(t *testing.T, name string) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	fxOnce.Do(func() { fxDir, fxErr = buildFixtures() })
	if fxErr != nil {
		t.Fatal(fxErr)
	}
	return filepath.Join(fxDir, name)
}

func video(size string, rate int, secs int) []string {
	return []string{"-f", "lavfi", "-i", fmt.Sprintf("testsrc=size=%s:rate=%d:duration=%d", size, rate, secs)}
}

func sine(secs int) []string {
	return []string{"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=440:duration=%d", secs)}
}

func run(args ...[]string) []string {
	var all []string
	for _, a := range args {
		all = append(all, a...)
	}
	return all
}

func buildFixtures() (string, error) {
	dir, err := os.MkdirTemp("", "inspect-fixtures-*")
	if err != nil {
		return "", err
	}
	enc := strings.Fields(h264)
	steps := []struct {
		out  string
		args []string
	}{
		{"landscape.mp4", run(video("640x360", 25, 3), sine(3), enc, []string{"-c:a", "aac", "-shortest"})},
		{"portrait.mp4", run(video("360x640", 25, 3), sine(3), enc, []string{"-c:a", "aac", "-shortest"})},
		{"silent.mp4", run(video("640x360", 25, 3), enc)},
		{"sar2.mp4", run(video("640x360", 25, 2), enc, []string{"-vf", "setsar=2/1"})},
		{"tiny.mp4", run(video("64x48", 25, 2), enc)},
		{"uhd.mp4", run([]string{"-f", "lavfi", "-i", "color=c=black:s=3840x2160:r=1:d=2"}, enc)},
		{"fps300.mp4", run(video("160x120", 300, 1), enc)},
		{"audio_only.m4a", run(sine(2), []string{"-c:a", "aac"})},
	}
	for _, s := range steps {
		if err := ffmpeg(append(s.args, filepath.Join(dir, s.out))...); err != nil {
			return "", err
		}
	}
	// derived files
	if err := ffmpeg("-display_rotation", "90", "-i", filepath.Join(dir, "landscape.mp4"), "-c", "copy", filepath.Join(dir, "rotated.mp4")); err != nil {
		return "", err
	}
	if err := ffmpeg("-i", filepath.Join(dir, "landscape.mp4"), "-c", "copy", filepath.Join(dir, "landscape.mkv")); err != nil {
		return "", err
	}
	// stream-count bomb: a Matroska file with 40 audio streams
	bomb := run(video("160x120", 10, 1))
	maps := []string{"-map", "0:v"}
	for n := 1; n <= 40; n++ {
		bomb = append(bomb, "-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=%d:duration=1", 200+n))
		maps = append(maps, "-map", strconv.Itoa(n)+":a")
	}
	bomb = append(bomb, "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac")
	bomb = append(bomb, maps...)
	if err := ffmpeg(append(bomb, filepath.Join(dir, "bomb.mkv"))...); err != nil {
		return "", err
	}
	good, err := os.ReadFile(filepath.Join(dir, "landscape.mp4"))
	if err != nil {
		return "", err
	}
	write := func(name string, b []byte) error { return os.WriteFile(filepath.Join(dir, name), b, 0o600) }
	garbage := bytes.Repeat([]byte{0xde, 0xad, 0xbe, 0xef, 0x13, 0x37}, 700)
	files := map[string][]byte{
		"garbage_signature.mp4":  append([]byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom"), garbage...),
		"garbage_ebml.mkv":       append([]byte{0x1A, 0x45, 0xDF, 0xA3, 0x9F, 0x42}, garbage...),
		"text.mp4":               []byte("This is not a video, just a note I renamed.\nHello.\n"),
		"empty.mp4":              {},
		"truncated.mp4":          good[:len(good)/3],
		"playlist.mp4":           []byte("#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:1,\nfile:///etc/passwd\n#EXT-X-ENDLIST\n"),
		"playlist.m3u8":          []byte("#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:1,\nfile:///etc/passwd\n#EXT-X-ENDLIST\n"),
		"concat.mp4":             []byte("ffconcat version 1.0\nfile '/etc/passwd'\n"),
		"actually_mp4.txt":       good,
		"actually_mp4.webm":      good,
		"a b\";touch pwned;.mp4": good,
	}
	for n, b := range files {
		if err := write(n, b); err != nil {
			return "", err
		}
	}
	mkv, err := os.ReadFile(filepath.Join(dir, "landscape.mkv"))
	if err != nil {
		return "", err
	}
	if err := write("actually_mkv.mp4", mkv); err != nil {
		return "", err
	}
	if err := write("truncated.mkv", mkv[:len(mkv)/3]); err != nil {
		return "", err
	}
	// "no_moov.mp4": the ftyp and the media data but not the index; "tail_cut.mp4": only the last 40 bytes are missing;
	// "padded.mp4": valid, followed by zero padding, which is legal.
	if err := write("no_moov.mp4", append(append([]byte{}, good[:32]...), good[len(good)-96230:]...)); err != nil {
		return "", err
	}
	if err := write("tail_cut.mp4", good[:len(good)-40]); err != nil {
		return "", err
	}
	if err := write("padded.mp4", append(append([]byte{}, good...), make([]byte, 64)...)); err != nil {
		return "", err
	}
	return dir, nil
}

func probe(t *testing.T, name string) (*Info, error) {
	t.Helper()
	p := &Prober{Timeout: 20 * time.Second}
	return p.Probe(context.Background(), fx(t, name))
}

func wantInfo(t *testing.T, name string) *Info {
	t.Helper()
	i, err := probe(t, name)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if err := DefaultPolicy().Validate(i); err != nil {
		t.Fatalf("%s: policy refused a good file: %v", name, err)
	}
	return i
}

func wantReject(t *testing.T, name, code string) {
	t.Helper()
	i, err := probe(t, name)
	if err == nil {
		err = DefaultPolicy().Validate(i)
	}
	r, ok := AsRejection(err)
	if !ok || r.Code != code {
		t.Fatalf("%s: got %v, want code %s", name, err, code)
	}
}

func TestFixturesThatMustBeAccepted(t *testing.T) {
	l := wantInfo(t, "landscape.mp4")
	if l.Container != "mp4" || l.Video.Codec != "h264" || l.Video.DisplayWidth != 640 || l.Video.DisplayHeight != 360 || l.Audio == nil || l.Audio.Codec != "aac" ||
		l.Video.Rotation != 0 || l.Video.FPS < 24.9 || l.Video.FPS > 25.1 || l.DurationSecondsInt() != 4 && l.DurationSecondsInt() != 3 || l.VideoType() != "SHORT" {
		t.Fatalf("landscape: %+v %+v %+v", l, l.Video, l.Audio)
	}
	if l.SizeBytes <= 0 {
		t.Fatal("the size must be the real size on disk")
	}
	p := wantInfo(t, "portrait.mp4")
	if p.Video.DisplayWidth != 360 || p.Video.DisplayHeight != 640 {
		t.Fatalf("portrait: %+v", p.Video)
	}
	r := wantInfo(t, "rotated.mp4")
	if r.Video.Width != 640 || r.Video.Height != 360 || r.Video.DisplayWidth != 360 || r.Video.DisplayHeight != 640 || (r.Video.Rotation != 90 && r.Video.Rotation != 270) {
		t.Fatalf("rotation metadata must swap the display size: %+v", r.Video)
	}
	s := wantInfo(t, "silent.mp4")
	if s.Audio != nil || s.AudioStreams != 0 {
		t.Fatalf("a silent video is valid and has no audio: %+v", s.Audio)
	}
	sar := wantInfo(t, "sar2.mp4")
	if sar.Video.Width != 640 || sar.Video.DisplayWidth != 1280 || sar.Video.DisplayHeight != 360 || sar.Video.SARNum != 2 || sar.Video.SARDen != 1 {
		t.Fatalf("non-square pixels: %+v", sar.Video)
	}
	tiny := wantInfo(t, "tiny.mp4")
	if tiny.Video.DisplayWidth != 64 || tiny.Video.DisplayHeight != 48 {
		t.Fatalf("tiny: %+v", tiny.Video)
	}
	if k := wantInfo(t, "landscape.mkv"); k.Container != "matroska" || k.Video.Codec != "h264" {
		t.Fatalf("mkv: %+v", k)
	}
	wantInfo(t, "padded.mp4") // zero padding after the last box is legal
}

func TestExtensionLiesDoNotMatterBecauseContentDecides(t *testing.T) {
	for _, n := range []string{"actually_mp4.txt", "actually_mp4.webm"} {
		if i := wantInfo(t, n); i.Container != "mp4" {
			t.Fatalf("%s: %+v", n, i)
		}
	}
	if i := wantInfo(t, "actually_mkv.mp4"); i.Container != "matroska" {
		t.Fatalf("an mkv named .mp4 is a Matroska file: %+v", i)
	}
}

func TestFixturesThatMustBeRefusedWithTheRightCode(t *testing.T) {
	for name, code := range map[string]string{
		"uhd.mp4":               CodeResolutionTooHigh,
		"fps300.mp4":            CodeFPSTooHigh,
		"audio_only.m4a":        CodeNoVideoStream,
		"bomb.mkv":              CodeTooManyStreams,
		"garbage_signature.mp4": CodeCorruptInput,
		"garbage_ebml.mkv":      CodeCorruptInput,
		"truncated.mp4":         CodeCorruptInput, // index in front, media data cut: only the box walk sees it
		"truncated.mkv":         CodeCorruptInput, // ffprobe reports "ended prematurely"
		"no_moov.mp4":           CodeCorruptInput,
		"tail_cut.mp4":          CodeCorruptInput,
		"text.mp4":              CodeUnsupportedMedia,
		"empty.mp4":             CodeUnsupportedMedia,
		"playlist.mp4":          CodeUnsupportedMedia,
		"playlist.m3u8":         CodeUnsupportedMedia,
		"concat.mp4":            CodeUnsupportedMedia,
	} {
		wantReject(t, name, code)
	}
}

func TestDurationAndSizeLimitsApplyToRealFiles(t *testing.T) {
	i := wantInfo(t, "landscape.mp4")
	p := DefaultPolicy()
	p.MaxDuration = 2 * time.Second
	if r, _ := AsRejection(p.Validate(i)); r == nil || r.Code != CodeTooLong {
		t.Fatalf("%v", r)
	}
	p = DefaultPolicy()
	p.MaxSizeBytes = i.SizeBytes - 1
	if r, _ := AsRejection(p.Validate(i)); r == nil || r.Code != CodeTooLarge {
		t.Fatalf("%v", r)
	}
	p = DefaultPolicy()
	p.ShortMaxSizeBytes = i.SizeBytes - 1
	if r, _ := AsRejection(p.Validate(i)); r == nil || r.Code != CodeShortTooLarge {
		t.Fatalf("%v", r)
	}
}

func TestHostileFileNameCannotRunAnything(t *testing.T) {
	name := "a b\";touch pwned;.mp4"
	i := wantInfo(t, name)
	if i.Container != "mp4" {
		t.Fatalf("%+v", i)
	}
	for _, where := range []string{fxDir, "."} {
		if _, err := os.Stat(filepath.Join(where, "pwned")); err == nil {
			t.Fatal("the file name was interpreted by a shell")
		}
	}
}

func TestPlaylistsCannotReachOtherFilesOrTheNetwork(t *testing.T) {
	// Both are text files that ffprobe would happily follow without the demuxer and protocol whitelists.
	for _, n := range []string{"playlist.mp4", "playlist.m3u8", "concat.mp4"} {
		i, err := probe(t, n)
		if err == nil {
			t.Fatalf("%s must be refused, got %+v", n, i)
		}
		if r, ok := AsRejection(err); !ok || r.Code != CodeUnsupportedMedia {
			t.Fatalf("%s: %v", n, err)
		}
	}
}

func TestRejectionDetailIsSanitised(t *testing.T) {
	_, err := probe(t, "garbage_signature.mp4")
	r, ok := AsRejection(err)
	if !ok {
		t.Fatal(err)
	}
	if strings.Contains(r.Detail, fxDir) || strings.ContainsAny(r.Detail, "\n\r\x00") {
		t.Fatalf("detail leaks a path or control characters: %q", r.Detail)
	}
	if strings.Contains(r.Error(), fxDir) || strings.Contains(r.Message, "ffprobe") {
		t.Fatalf("the visible text must be the fixed message: %q", r.Error())
	}
}

// ---- hostile tool behaviour, simulated with fake ffprobe scripts ----

func fake(t *testing.T, script string) (bin, input string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "ffprobe")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	input = filepath.Join(dir, "input")
	if err := os.WriteFile(input, []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom"), 0o600); err != nil {
		t.Fatal(err)
	}
	return bin, input
}

func TestAHungToolIsKilledWithItsChildrenAtTheTimeout(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	bin, input := fake(t, "sleep 60 &\necho $! > '"+pidFile+"'\nwait\n")
	start := time.Now()
	_, err := (&Prober{Bin: bin, Timeout: 400 * time.Millisecond}).Probe(context.Background(), input)
	if r, ok := AsRejection(err); !ok || r.Code != CodeProcessingTimeout {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("took %v: the timeout did not stop the tool", time.Since(start))
	}
	b, rerr := os.ReadFile(pidFile)
	if rerr != nil {
		t.Fatal(rerr)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	waitGone := time.Now().Add(3 * time.Second)
	for time.Now().Before(waitGone) {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("the child process %d of the hung tool survived", pid)
}

func TestAToolThatFloodsItsOutputIsStoppedAndBounded(t *testing.T) {
	bin, input := fake(t, "yes aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n") // never ends on its own
	start := time.Now()
	_, err := (&Prober{Bin: bin, Timeout: 30 * time.Second, MaxOutputBytes: 1 << 20}).Probe(context.Background(), input)
	if r, ok := AsRejection(err); !ok || r.Code != CodeUnsupportedMedia {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > 8*time.Second {
		t.Fatalf("took %v: the flood was not cut off at the output limit", time.Since(start))
	}
}

func TestTheToolDoesNotInheritSecretsFromTheEnvironment(t *testing.T) {
	t.Setenv("WORKER_DATABASE_URL", "postgres://secret")
	t.Setenv("STORAGE_SECRET_KEY", "secret")
	script := `leaked=$(env | grep -c -E 'WORKER_DATABASE_URL|STORAGE_SECRET_KEY')
locale=$(env | grep -c '^LC_ALL=')
printf '{"format":{"format_name":"mov,mp4","duration":"%s","bit_rate":"%s"},"streams":[]}' "$leaked" "$locale"
`
	bin, input := fake(t, script)
	i, err := (&Prober{Bin: bin}).Probe(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if i.DurationSeconds != 0 {
		t.Fatalf("%v secret variables reached the child", i.DurationSeconds)
	}
	if i.BitRate != 1 {
		t.Fatalf("the controlled environment was not applied (LC_ALL count %d)", i.BitRate)
	}
}

func TestMissingToolAndCancellationAreNotVerdictsAboutTheFile(t *testing.T) {
	_, input := fake(t, "exit 0\n")
	_, err := (&Prober{Bin: "/nonexistent/ffprobe"}).Probe(context.Background(), input)
	if err == nil {
		t.Fatal("expected an error")
	}
	if _, ok := AsRejection(err); ok {
		t.Fatalf("a missing ffprobe must not blame the file: %v", err)
	}
	bin, input2 := fake(t, "sleep 30\n")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	start := time.Now()
	_, err = (&Prober{Bin: bin, Timeout: 20 * time.Second}).Probe(ctx, input2)
	if err != context.Canceled {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("cancellation took %v", time.Since(start))
	}
}

func TestNotARegularFileIsRefused(t *testing.T) {
	if _, err := (&Prober{}).Probe(context.Background(), t.TempDir()); err == nil {
		t.Fatal("a directory must not be probed")
	}
}

func TestBoxWalkOnHandMadeFiles(t *testing.T) {
	box := func(typ string, payload int) []byte {
		b := make([]byte, 8+payload)
		b[0], b[1], b[2], b[3] = byte((8+payload)>>24), byte((8+payload)>>16), byte((8+payload)>>8), byte(8+payload)
		copy(b[4:], typ)
		return b
	}
	write := func(b []byte) (string, int64) {
		p := filepath.Join(t.TempDir(), "f.mp4")
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p, int64(len(b))
	}
	cat := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
	big := func(declared uint64) []byte { // 64-bit box header claiming `declared` bytes, without the payload
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
		p, size := write(c.data)
		if bad, why := isoTruncated(p, size); bad != c.bad {
			t.Errorf("%s: truncated=%v (%s), want %v", name, bad, why, c.bad)
		}
	}
}

func TestEBMLVintAndSegmentWalkOnHandMadeFiles(t *testing.T) {
	ebml := []byte{0x1A, 0x45, 0xDF, 0xA3, 0x84, 0x42, 0x86, 0x81, 0x01} // header with a 4-byte payload
	seg := func(size []byte, payload int) []byte {
		return append(append(append([]byte{}, ebml...), append([]byte{0x18, 0x53, 0x80, 0x67}, size...)...), make([]byte, payload)...)
	}
	size8 := func(v uint64) []byte { // 8-byte size vint
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
		"zero first byte":         {[]byte{0x1A, 0x45, 0xDF, 0xA3, 0x00, 0x00}, true},
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

func TestTheCommandLineKeepsBothWhitelistsAndTheFilePrefix(t *testing.T) {
	args := strings.Join(probeArgs("/tmp/job-1/input", demuxers), " ")
	for _, want := range []string{"-format_whitelist " + demuxers, "-protocol_whitelist file", "-i file:/tmp/job-1/input", "-v error", "-print_format json"} {
		if !strings.Contains(args, want) {
			t.Errorf("the ffprobe command line lacks %q:\n%s", want, args)
		}
	}
	for _, banned := range []string{"-f ", "|", ";", "&&", "$("} {
		if strings.Contains(args, banned) {
			t.Errorf("unexpected %q in the command line", banned)
		}
	}
}

func TestATruncationMessageFromFFprobeRejectsAFileThatLooksComplete(t *testing.T) {
	// valid JSON, exit status 0 and an intact box structure, but ffprobe says the end of the file is missing
	bin, input := fake(t, `echo "[matroska,webm @ 0x1] File ended prematurely" >&2
printf '{"format":{"format_name":"mov,mp4","duration":"3"},"streams":[{"index":0,"codec_type":"video","codec_name":"h264","width":64,"height":64}]}'
`)
	_, err := (&Prober{Bin: bin}).Probe(context.Background(), input)
	if r, ok := AsRejection(err); !ok || r.Code != CodeCorruptInput {
		t.Fatalf("got %v", err)
	}
	// the same output without the message is accepted, so the rejection above comes from the message and nothing else
	bin2, input2 := fake(t, `printf '{"format":{"format_name":"mov,mp4","duration":"3"},"streams":[{"index":0,"codec_type":"video","codec_name":"h264","width":64,"height":64}]}'
`)
	if _, err := (&Prober{Bin: bin2}).Probe(context.Background(), input2); err != nil {
		t.Fatalf("control: %v", err)
	}
}
