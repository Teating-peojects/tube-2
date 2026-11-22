package worker

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseScannerEnv(t *testing.T) {
	good := map[string][]string{
		"":                           nil,
		"  ":                         nil,
		"CLAMD_CONF=/etc/clamd.conf": {"CLAMD_CONF=/etc/clamd.conf"},
		"A=1, B=two ,_C=":            {"A=1", "B=two", "_C="},
		"URL=http://h/p?x=1":         {"URL=http://h/p?x=1"},
	}
	for in, want := range good {
		got, err := ParseScannerEnv(in)
		if err != nil || strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%q: got %v err %v, want %v", in, got, err, want)
		}
	}
	many := make([]string, 33)
	for i := range many {
		many[i] = "K" + strconv.Itoa(i) + "=v"
	}
	for _, in := range []string{"NOEQUALS", "=value", "1BAD=x", "BAD NAME=x", "A=1,A=2", "A=1,,B=2", "A=line\nbreak", "A=nul\x00", strings.Join(many, ","), "BAD-DASH=x"} {
		if got, err := ParseScannerEnv(in); err == nil {
			t.Errorf("%q must be refused, got %v", in, got)
		}
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func TestFFmpegRunsWithALowerPriority(t *testing.T) {
	out := filepath.Join(t.TempDir(), "nice.txt")
	bin := fakeBin(t, "sleep 0.4\nawk '{print $19}' /proc/$$/stat > \"$1\"\n") // field 19 of /proc/<pid>/stat is the niceness
	if err := runFFmpegLimited(context.Background(), bin, []string{out}, 20*time.Second, Limits{Nice: 11}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, out); got != "11" {
		t.Fatalf("niceness of the child = %s, want 11", got)
	}
	// control: without limits the child keeps the niceness it inherited, so 11 above comes from the limit
	if err := runFFmpeg(context.Background(), bin, []string{out}, 20*time.Second); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, out); got == "11" {
		t.Fatalf("an unlimited run must not be niced to 11 (got %s)", got)
	}
}

func TestFFmpegAddressSpaceCanBeCapped(t *testing.T) {
	out := filepath.Join(t.TempDir(), "mem.txt")
	bin := fakeBin(t, "sleep 0.4\nulimit -v > \"$1\"\n")
	if err := runFFmpegLimited(context.Background(), bin, []string{out}, 20*time.Second, Limits{MemoryMB: 2048}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, out); got != strconv.Itoa(2048*1024) { // ulimit -v reports KiB
		t.Fatalf("address-space limit = %s, want %d", got, 2048*1024)
	}
	if err := runFFmpeg(context.Background(), bin, []string{out}, 20*time.Second); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, out); got != "unlimited" {
		t.Fatalf("without a limit the child is unlimited, got %s", got)
	}
}

func TestPipelineAppliesItsLimitsToEveryFFmpegRun(t *testing.T) {
	needFFmpeg(t)
	const vid = "0190aaaa-bbbb-7ccc-8ddd-999999999999"
	st, mem := limitsFixture(t, vid)
	log := filepath.Join(t.TempDir(), "nice.log")
	wrapper := fakeBin(t, "sleep 0.3\nawk '{print $19}' /proc/$$/stat >> '"+log+"'\nexec ffmpeg \"$@\"\n")
	p := newPipe(t, st, mem)
	p.FFmpeg = wrapper
	p.Limits = Limits{Nice: 7}
	for _, typ := range []string{"THUMBNAIL", "TRANSCODE"} {
		if err := p.Run(context.Background(), Job{ID: "j", VideoID: vid, Type: typ}); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
	}
	lines := strings.Fields(readFile(t, log))
	if len(lines) < 6 { // five thumbnails and at least one rendition
		t.Fatalf("only %d ffmpeg runs were observed: %v", len(lines), lines)
	}
	for i, l := range lines {
		if l != "7" {
			t.Errorf("ffmpeg run %d had niceness %s, want 7 (a call site forgot the pipeline's limits)", i, l)
		}
	}
}

func limitsFixture(t *testing.T, vid string) (*fakeStore, *memStorage) {
	t.Helper()
	src := makeVideo(t, "-f", "lavfi", "-i", "testsrc=size=320x240:rate=10", "-t", "2", "-c:v", "libx264", "-pix_fmt", "yuv420p")
	mem := newMem()
	key := "uploads/" + vid + "/original.mp4"
	mem.objs[key] = src
	return &fakeStore{video: VideoRow{CreatorID: "c", Status: "PROCESSING", Moderation: "OK", OriginalKey: key, Size: int64(len(src))},
		meta: MetaRow{Width: 320, Height: 240, FPS: 10, Duration: 2}}, mem
}
