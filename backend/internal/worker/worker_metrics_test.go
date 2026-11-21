package worker

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/redxtube/api/internal/metrics"
)

type statsStub struct{ stats []QueueStat }

func (s statsStub) QueueStats(context.Context) ([]QueueStat, error) { return s.stats, nil }

func scrape(reg *metrics.Registry) string {
	rr := httptest.NewRecorder()
	reg.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/metrics", nil))
	return rr.Body.String()
}

func TestWorkerMetricsCoverTheOperationalSignals(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "scratch.bin"), make([]byte, 4096), 0o600)
	reg := metrics.NewRegistry()
	m := NewMetrics(reg, statsStub{stats: []QueueStat{{Type: "HLS", Status: "QUEUED", Count: 3, OldestSec: 42}}}, dir, 2)

	j := Job{Type: "TRANSCODE", WaitSec: 12}
	m.started(j)
	if out := scrape(reg); !strings.Contains(out, "worker_active_jobs 1") {
		t.Errorf("active gauge not 1:\n%s", out)
	}
	m.finished()
	m.observe(j, "completed", 90*time.Second)
	m.Encoded(120, 4*time.Minute) // 4 min for 2 media minutes = 120 s per media minute
	m.failure(&JobError{Code: "ffmpeg_failed"})
	m.failure(&JobError{Code: "too_long", Permanent: true})
	m.failure(&JobError{Code: "storage_verify"})
	m.cleanup("ok")

	out := scrape(reg)
	for _, want := range []string{
		"worker_active_jobs 0", "worker_concurrency 2",
		`worker_job_queue_wait_seconds_count{type="TRANSCODE"} 1`,
		`worker_jobs_total{type="TRANSCODE",result="completed"} 1`,
		"worker_media_seconds_total 120",
		"worker_encode_seconds_per_media_minute_count 1",
		`worker_encode_seconds_per_media_minute_bucket{le="120"} 1`,
		`worker_failures_total{kind="ffmpeg"} 1`,
		`worker_failures_total{kind="input_rejected"} 1`,
		`worker_failures_total{kind="storage"} 1`,
		`worker_cleanup_total{result="ok"} 1`,
		"worker_scratch_used_bytes 4096",
		"worker_scratch_free_bytes",
		`worker_queue_jobs{type="HLS",status="QUEUED"} 3`,
		`worker_queue_oldest_seconds{type="HLS",status="QUEUED"} 42`,
		`worker_queue_jobs{type="METADATA",status="FAILED"} 0`, // explicit zeros
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// a nil *Metrics (metrics disabled) must be safe everywhere
	var none *Metrics
	none.started(j)
	none.finished()
	none.failure(&JobError{})
	none.Encoded(1, time.Second)
	none.observe(j, "completed", time.Second)
	none.cleanup("ok")
}

func TestFailureKindIsBounded(t *testing.T) {
	cases := map[string]*JobError{
		"ffprobe": {Code: "ffprobe_failed"}, "ffmpeg": {Code: "ffmpeg_timeout"}, "storage": {Code: "download_failed"},
		"validation_output": {Code: "output_invalid"}, "scanner": {Code: "scanner_unavailable"}, "disk": {Code: "disk_full"},
		"blocked": {Code: "blocked_content", Permanent: true}, "unavailable": {Code: "video_unavailable", Permanent: true},
		"input_rejected": {Code: "resolution_too_high", Permanent: true}, "other": {Code: "something_new", Err: errors.New("x")},
	}
	for want, je := range cases {
		if got := failureKind(je); got != want {
			t.Errorf("%s -> %q, want %q", je.Code, got, want)
		}
	}
}
