package processing_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/redxtube/api/internal/processing"
	"github.com/redxtube/api/internal/processing/inspect"
	"github.com/redxtube/api/internal/storage"
)

// ---- a storage stub and media fixtures ----

type fakeSource struct {
	mu        sync.Mutex
	objects   map[string][]byte
	headErr   error
	dlErr     error
	sizeLie   int64  // added to the announced size by Head
	extra     []byte // appended by Download beyond the object (a lying storage)
	downloads int
	block     chan struct{} // Download waits for ctx cancellation when set
}

func (f *fakeSource) Head(_ context.Context, key string) (storage.ObjectInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.headErr != nil {
		return storage.ObjectInfo{}, f.headErr
	}
	b, ok := f.objects[key]
	if !ok {
		return storage.ObjectInfo{}, storage.ErrNotFound
	}
	return storage.ObjectInfo{Size: int64(len(b)) + f.sizeLie, ContentType: "video/mp4"}, nil
}

func (f *fakeSource) Download(ctx context.Context, key string, w io.Writer) (int64, error) {
	f.mu.Lock()
	f.downloads++
	b, ok := f.objects[key]
	dlErr, extra, block := f.dlErr, f.extra, f.block
	f.mu.Unlock()
	if block != nil {
		<-ctx.Done()
		return 0, ctx.Err()
	}
	if dlErr != nil {
		return 0, dlErr
	}
	if !ok {
		return 0, storage.ErrNotFound
	}
	n, err := w.Write(append(append([]byte{}, b...), extra...))
	return int64(n), err
}

func (f *fakeSource) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.downloads }

var (
	mediaOnce sync.Once
	mediaDir  string
	mediaErr  error
)

func ffmpegRun(args ...string) error {
	out, err := exec.Command("ffmpeg", append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg %v: %v\n%s", args, err, out)
	}
	return nil
}

func media(t *testing.T, name string) []byte {
	t.Helper()
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " not installed")
		}
	}
	mediaOnce.Do(func() {
		dir, err := os.MkdirTemp("", "processing-media-*")
		if err != nil {
			mediaErr = err
			return
		}
		mediaDir = dir
		enc := []string{"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p"}
		for out, args := range map[string][]string{
			"good.mp4": append(append([]string{"-f", "lavfi", "-i", "testsrc=size=640x360:rate=25:duration=3", "-f", "lavfi", "-i", "sine=frequency=440:duration=3"}, enc...), "-c:a", "aac", "-shortest"),
			"uhd.mp4":  append([]string{"-f", "lavfi", "-i", "color=c=black:s=3840x2160:r=1:d=2"}, enc...),
		} {
			if err := ffmpegRun(append(args, filepath.Join(dir, out))...); err != nil {
				mediaErr = err
				return
			}
		}
	})
	if mediaErr != nil {
		t.Fatal(mediaErr)
	}
	switch name {
	case "text.mp4":
		return []byte("just some text renamed to .mp4\n")
	case "garbage.mp4":
		return append([]byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom"), bytes.Repeat([]byte{0xde, 0xad, 0xbe, 0xef}, 900)...)
	}
	b, err := os.ReadFile(filepath.Join(mediaDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type rec struct {
	mu     sync.Mutex
	stages []string
}

func (r *rec) Report(stage string, _ int) {
	r.mu.Lock()
	r.stages = append(r.stages, stage)
	r.mu.Unlock()
}

// metaEnv wires a MetadataProcessor to the database, a fake storage, a private temp dir and the real ffprobe.
type metaEnv struct {
	*tenv
	src  *fakeSource
	proc *processing.MetadataProcessor
	tmp  string
}

func newMeta(t *testing.T) *metaEnv {
	t.Helper()
	e := setup(t)
	m := &metaEnv{tenv: e, src: &fakeSource{objects: map[string][]byte{}}, tmp: t.TempDir()}
	m.proc = &processing.MetadataProcessor{Store: e.store, Src: m.src, Prober: &inspect.Prober{Timeout: 20 * time.Second}, Policy: inspect.DefaultPolicy(), TempDir: m.tmp, Log: discard()}
	return m
}

// upload stores the bytes as the original of a new PROCESSING video with a queued METADATA job and claims the job.
func (m *metaEnv) upload(data []byte) (videoID string, job processing.Job) {
	m.t.Helper()
	videoID = m.video("PROCESSING")
	key := "uploads/" + videoID + "/original.mp4"
	m.src.objects[key] = data
	m.exec(`UPDATE videos SET original_storage_key = $2, file_size_bytes = $3 WHERE id = $1::uuid`, videoID, key, len(data))
	m.job(videoID, "METADATA", 3)
	j, err := m.store.Claim(context.Background(), "w1", meta)
	if err != nil || j == nil {
		m.t.Fatalf("claim: %v %v", j, err)
	}
	return videoID, *j
}

func (m *metaEnv) tempEntries() int {
	ents, _ := os.ReadDir(m.tmp)
	return len(ents)
}

func (m *metaEnv) count(sql string, args ...any) (n int) {
	m.t.Helper()
	if err := m.admin.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		m.t.Fatal(err)
	}
	return n
}

func permanentCode(err error) string {
	var p *processing.PermanentError
	if errors.As(err, &p) {
		return p.Code
	}
	return ""
}

// ---- tests ----

func TestMetadataRecordsTheFileAndQueuesExactlyOneHLSJob(t *testing.T) {
	m := newMeta(t)
	vid, job := m.upload(media(t, "good.mp4"))
	r := &rec{}
	if err := m.proc.Process(context.Background(), job, r); err != nil {
		t.Fatal(err)
	}
	var w, h int
	var fps float64
	var vc, ac, container string
	var hasAudio bool
	if err := m.admin.QueryRow(context.Background(), `SELECT width, height, fps::float8, video_codec, audio_codec, container, has_audio FROM video_metadata WHERE video_id = $1::uuid`, vid).
		Scan(&w, &h, &fps, &vc, &ac, &container, &hasAudio); err != nil {
		t.Fatal(err)
	}
	if w != 640 || h != 360 || fps < 24.9 || fps > 25.1 || vc != "h264" || ac != "aac" || container != "mp4" || !hasAudio {
		t.Fatalf("metadata: %dx%d %v %s %s %s %v", w, h, fps, vc, ac, container, hasAudio)
	}
	if n := m.count(`SELECT count(*) FROM video_processing_jobs WHERE video_id = $1::uuid AND job_type = 'HLS' AND status = 'QUEUED' AND processing_version = 1`, vid); n != 1 {
		t.Fatalf("%d queued HLS jobs, want 1", n)
	}
	if m.tempEntries() != 0 {
		t.Fatalf("the temp directory was not removed: %d entries", m.tempEntries())
	}
	want := []string{"checking", "downloading", "probing", "saving"}
	if fmt.Sprint(r.stages) != fmt.Sprint(want) {
		t.Fatalf("stages %v, want %v", r.stages, want)
	}
	// running it again (a redelivery after a crash before Complete) changes nothing but the timestamp
	if err := m.proc.Process(context.Background(), job, &rec{}); err != nil {
		t.Fatal(err)
	}
	if n := m.count(`SELECT count(*) FROM video_processing_jobs WHERE video_id = $1::uuid AND job_type = 'HLS'`, vid); n != 1 {
		t.Fatalf("a repeated METADATA job queued %d HLS jobs", n)
	}
	if n := m.count(`SELECT count(*) FROM video_metadata WHERE video_id = $1::uuid`, vid); n != 1 {
		t.Fatal("metadata duplicated")
	}
}

func TestMetadataRefusesBadInputWithSafeCodesAndLeavesNothingBehind(t *testing.T) {
	for name, c := range map[string]struct {
		file string
		code string
		mod  func(*inspect.Policy)
	}{
		"text file named mp4":      {"text.mp4", inspect.CodeUnsupportedMedia, nil},
		"garbage behind a header":  {"garbage.mp4", inspect.CodeCorruptInput, nil},
		"4K":                       {"uhd.mp4", inspect.CodeResolutionTooHigh, nil},
		"too long for the policy":  {"good.mp4", inspect.CodeTooLong, func(p *inspect.Policy) { p.MaxDuration = time.Second }},
		"SHORT above its size cap": {"good.mp4", inspect.CodeShortTooLarge, func(p *inspect.Policy) { p.ShortMaxSizeBytes = 1000 }},
	} {
		t.Run(name, func(t *testing.T) {
			m := newMeta(t)
			if c.mod != nil {
				c.mod(&m.proc.Policy)
			}
			vid, job := m.upload(media(t, c.file))
			err := m.proc.Process(context.Background(), job, &rec{})
			if permanentCode(err) != c.code {
				t.Fatalf("got %v, want a permanent %s", err, c.code)
			}
			if n := m.count(`SELECT count(*) FROM video_metadata WHERE video_id = $1::uuid`, vid); n != 0 {
				t.Fatal("metadata of a refused file was saved")
			}
			if n := m.count(`SELECT count(*) FROM video_processing_jobs WHERE video_id = $1::uuid AND job_type = 'HLS'`, vid); n != 0 {
				t.Fatal("an HLS job was queued for a refused file")
			}
			if m.tempEntries() != 0 {
				t.Fatal("temp files left behind")
			}
		})
	}
}

func TestMetadataThroughTheRunnerEndsInCompletedOrFailedWithTheCode(t *testing.T) {
	m := newMeta(t)
	good := m.video("PROCESSING")
	bad := m.video("PROCESSING")
	for vid, data := range map[string][]byte{good: media(t, "good.mp4"), bad: media(t, "text.mp4")} {
		key := "uploads/" + vid + "/original.mp4"
		m.src.objects[key] = data
		m.exec(`UPDATE videos SET original_storage_key = $2, file_size_bytes = $3 WHERE id = $1::uuid`, vid, key, len(data))
	}
	jg, jb := m.job(good, "METADATA", 3), m.job(bad, "METADATA", 3)
	stop := start(runner(m.tenv, "meta-runner", m.proc, nil))
	defer stop()
	waitFor(t, 30*time.Second, "both jobs to finish", func() bool { return m.row(jg).Status == "COMPLETED" && m.row(jb).Status == "FAILED" })
	stop()
	if r := m.row(jb); r.ErrorCode != "unsupported_media" || r.Attempts != 1 {
		t.Fatalf("%+v", r)
	}
	var msg *string
	_ = m.admin.QueryRow(context.Background(), `SELECT error_message FROM video_processing_jobs WHERE id = $1::uuid`, jb).Scan(&msg)
	if msg != nil {
		t.Fatalf("no free text may reach error_message: %q", *msg)
	}
	if n := m.count(`SELECT count(*) FROM video_processing_jobs WHERE job_type = 'HLS' AND video_id = $1::uuid`, good); n != 1 {
		t.Fatalf("good video: %d HLS jobs", n)
	}
}

func TestMetadataStorageProblems(t *testing.T) {
	t.Run("object missing is permanent", func(t *testing.T) {
		m := newMeta(t)
		_, job := m.upload(media(t, "good.mp4"))
		m.src.objects = map[string][]byte{}
		if err := m.proc.Process(context.Background(), job, &rec{}); permanentCode(err) != processing.CodeSourceMissing {
			t.Fatalf("%v", err)
		}
	})
	t.Run("a storage outage is transient", func(t *testing.T) {
		m := newMeta(t)
		_, job := m.upload(media(t, "good.mp4"))
		m.src.headErr = errors.New("connection reset")
		if err := m.proc.Process(context.Background(), job, &rec{}); err == nil || permanentCode(err) != "" {
			t.Fatalf("want a plain (retryable) error, got %v", err)
		}
		m.src.headErr, m.src.dlErr = nil, errors.New("timeout")
		if err := m.proc.Process(context.Background(), job, &rec{}); err == nil || permanentCode(err) != "" {
			t.Fatalf("download failure must be retryable, got %v", err)
		}
		if m.tempEntries() != 0 {
			t.Fatal("temp files left behind after a failed download")
		}
	})
	t.Run("size differs from what the upload recorded", func(t *testing.T) {
		m := newMeta(t)
		vid, job := m.upload(media(t, "good.mp4"))
		m.exec(`UPDATE videos SET file_size_bytes = file_size_bytes + 1 WHERE id = $1::uuid`, vid)
		if err := m.proc.Process(context.Background(), job, &rec{}); permanentCode(err) != inspect.CodeCorruptInput || m.src.count() != 0 {
			t.Fatalf("%v (downloads %d)", err, m.src.count())
		}
	})
	t.Run("object above the policy is refused before downloading", func(t *testing.T) {
		m := newMeta(t)
		m.proc.Policy.MaxSizeBytes = 1000
		_, job := m.upload(media(t, "good.mp4"))
		if err := m.proc.Process(context.Background(), job, &rec{}); permanentCode(err) != inspect.CodeTooLarge || m.src.count() != 0 {
			t.Fatalf("%v (downloads %d)", err, m.src.count())
		}
	})
	t.Run("a storage that sends more bytes than it announced is stopped", func(t *testing.T) {
		m := newMeta(t)
		_, job := m.upload(media(t, "good.mp4"))
		m.src.extra = bytes.Repeat([]byte{1}, 1<<20)
		if err := m.proc.Process(context.Background(), job, &rec{}); permanentCode(err) != inspect.CodeCorruptInput {
			t.Fatalf("%v", err)
		}
		if m.tempEntries() != 0 {
			t.Fatal("temp files left behind")
		}
	})
	t.Run("a short download is retryable", func(t *testing.T) {
		m := newMeta(t)
		_, job := m.upload(media(t, "good.mp4"))
		m.src.sizeLie = 5 // Head announces 5 bytes more than Download delivers; also differs from the recorded size
		m.exec(`UPDATE videos SET file_size_bytes = file_size_bytes + 5 WHERE id = $1::uuid`, job.VideoID)
		if err := m.proc.Process(context.Background(), job, &rec{}); err == nil || permanentCode(err) != "" {
			t.Fatalf("%v", err)
		}
	})
	t.Run("not enough disk is retryable and nothing is downloaded", func(t *testing.T) {
		m := newMeta(t)
		m.proc.FreeBytes = func(string) (uint64, error) { return 1 << 20, nil }
		_, job := m.upload(media(t, "good.mp4"))
		if err := m.proc.Process(context.Background(), job, &rec{}); err == nil || permanentCode(err) != "" || m.src.count() != 0 {
			t.Fatalf("%v (downloads %d)", err, m.src.count())
		}
	})
}

func TestMetadataStopsForGoneVideosAndMissingTools(t *testing.T) {
	m := newMeta(t)
	vid, job := m.upload(media(t, "good.mp4"))
	m.exec(`UPDATE videos SET deleted_at = now() WHERE id = $1::uuid`, vid)
	if err := m.proc.Process(context.Background(), job, &rec{}); permanentCode(err) != processing.CodeVideoGone {
		t.Fatalf("deleted video: %v", err)
	}
	m.exec(`UPDATE videos SET deleted_at = NULL, status = 'UPLOADING' WHERE id = $1::uuid`, vid)
	if err := m.proc.Process(context.Background(), job, &rec{}); permanentCode(err) != processing.CodeVideoGone {
		t.Fatalf("video no longer PROCESSING: %v", err)
	}
	if err := m.proc.Process(context.Background(), processing.Job{ID: job.ID, VideoID: "00000000-0000-0000-0000-00000000dead", WorkerID: "w1"}, &rec{}); permanentCode(err) != processing.CodeVideoGone {
		t.Fatalf("unknown video: %v", err)
	}

	m2 := newMeta(t)
	m2.proc.Prober = &inspect.Prober{Bin: "/nonexistent/ffprobe"}
	_, job2 := m2.upload(media(t, "good.mp4"))
	err := m2.proc.Process(context.Background(), job2, &rec{})
	if err == nil || permanentCode(err) != "" {
		t.Fatalf("a missing ffprobe is the worker's problem, not the file's: %v", err)
	}
	if m2.tempEntries() != 0 {
		t.Fatal("temp files left behind")
	}
}

func TestSaveInspectionIsFencedByTheLease(t *testing.T) {
	m := newMeta(t)
	vid, job := m.upload(media(t, "good.mp4"))
	info := &inspect.Info{Container: "mp4", DurationSeconds: 3, SizeBytes: 10, Video: &inspect.VideoStream{Codec: "h264", Width: 640, Height: 360, DisplayWidth: 640, DisplayHeight: 360}}
	thief := job
	thief.WorkerID = "someone-else"
	if err := m.store.SaveInspection(context.Background(), thief, info); !errors.Is(err, processing.ErrLeaseLost) {
		t.Fatalf("%v", err)
	}
	if m.count(`SELECT count(*) FROM video_metadata WHERE video_id = $1::uuid`, vid)+m.count(`SELECT count(*) FROM video_processing_jobs WHERE job_type = 'HLS'`) != 0 {
		t.Fatal("a worker without the lease wrote data")
	}
}

func TestMetadataCancellationDuringDownloadCleansUp(t *testing.T) {
	m := newMeta(t)
	vid := m.video("PROCESSING")
	data := media(t, "good.mp4")
	key := "uploads/" + vid + "/original.mp4"
	m.src.objects[key] = data
	m.src.block = make(chan struct{}) // Download blocks until its context ends
	m.exec(`UPDATE videos SET original_storage_key = $2, file_size_bytes = $3 WHERE id = $1::uuid`, vid, key, len(data))
	id := m.job(vid, "METADATA", 3)
	stop := start(runner(m.tenv, "w", m.proc, func(o *processing.Options) { o.Concurrency = 1 }))
	defer stop()
	waitFor(t, 10*time.Second, "the download to start", func() bool { return m.src.count() == 1 })
	if _, _, err := m.store.Cancel(context.Background(), vid); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "FAILED(cancelled)", func() bool { r := m.row(id); return r.Status == "FAILED" && r.ErrorCode == "cancelled" })
	stop()
	if m.tempEntries() != 0 {
		t.Fatal("temp files left behind after cancellation")
	}
}

type pathRecorder struct {
	inner processing.MediaProber
	path  string
	dirOK bool
}

func (p *pathRecorder) Probe(ctx context.Context, path string) (*inspect.Info, error) {
	p.path = path
	st, err := os.Stat(path)
	p.dirOK = err == nil && st.Mode().IsRegular() && st.Mode().Perm() == 0o600
	return p.inner.Probe(ctx, path)
}

func TestTheInspectedFileHasNoExtensionAndPrivatePermissions(t *testing.T) {
	m := newMeta(t)
	rp := &pathRecorder{inner: m.proc.Prober}
	m.proc.Prober = rp
	_, job := m.upload(media(t, "good.mp4"))
	if err := m.proc.Process(context.Background(), job, &rec{}); err != nil {
		t.Fatal(err)
	}
	if filepath.Base(rp.path) != "input" || filepath.Ext(rp.path) != "" {
		t.Fatalf("the name of the file must not steer the demuxer, got %q", rp.path)
	}
	if !rp.dirOK {
		t.Fatal("the downloaded original must be a regular file readable only by the worker (0600)")
	}
	if rel, _ := filepath.Rel(m.tmp, rp.path); filepath.IsAbs(rel) || len(rel) == 0 || rel[0] == '.' {
		t.Fatalf("the file must live inside the configured temp dir, got %q", rp.path)
	}
}
