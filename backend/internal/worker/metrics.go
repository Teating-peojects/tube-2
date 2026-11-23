package worker

import (
	"context"
	"io/fs"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/redxtube/api/internal/metrics"
)

// QueueStat is one (job type, status) bucket of the queue.
type QueueStat struct {
	Type, Status string
	Count        int
	OldestSec    float64 // age of the oldest scheduled_at in the bucket
}

// QueueStats reports the open queue (QUEUED, PROCESSING) and the failures of the last 24 hours.
func (s *PG) QueueStats(ctx context.Context) ([]QueueStat, error) {
	rows, err := s.pool.Query(ctx, `SELECT job_type::text, status::text, count(*)::int,
  COALESCE(EXTRACT(EPOCH FROM (now() - min(scheduled_at))), 0)::float8
FROM video_processing_jobs
WHERE status IN ('QUEUED', 'PROCESSING') OR (status = 'FAILED' AND updated_at > now() - interval '24 hours')
GROUP BY 1, 2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QueueStat
	for rows.Next() {
		var q QueueStat
		if err := rows.Scan(&q.Type, &q.Status, &q.Count, &q.OldestSec); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// Metrics instruments the worker. All methods are safe on a nil *Metrics.
type Metrics struct {
	jobs   *metrics.Counter
	dur    *metrics.Histogram
	clean  *metrics.Counter
	wait   *metrics.Histogram
	fails  *metrics.Counter
	rate   *metrics.Histogram
	media  *metrics.Counter
	active atomic.Int64
}

var jobTypes = []string{"METADATA", "THUMBNAIL", "TRANSCODE", "HLS"}

// NewMetrics registers the worker metrics and a scrape-time queue collector.
func NewMetrics(reg *metrics.Registry, store interface {
	QueueStats(context.Context) ([]QueueStat, error)
}, workDir string, concurrency int) *Metrics {
	m := &Metrics{
		jobs: reg.NewCounter("worker_jobs_total", "Finished job attempts by type and result (completed, retry, failed, released, lease_lost).", "type", "result"),
		dur: reg.NewHistogram("worker_job_duration_seconds", "Wall time of a job attempt.",
			[]float64{1, 5, 15, 30, 60, 120, 300, 600, 1200, 3600}, "type"),
		clean: reg.NewCounter("worker_cleanup_total", "Cleanups of failed videos' partial HLS output by result (ok, error).", "result"),
		wait: reg.NewHistogram("worker_job_queue_wait_seconds", "Time a job waited in the queue before a worker claimed it.",
			[]float64{1, 5, 15, 30, 60, 300, 900, 3600}, "type"),
		fails: reg.NewCounter("worker_failures_total", "Failed job attempts by kind (ffprobe, ffmpeg, storage, validation_output, scanner, disk, input_rejected, blocked, unavailable, other).", "kind"),
		rate: reg.NewHistogram("worker_encode_seconds_per_media_minute", "Encode time per minute of source video (TRANSCODE job); above 60 means slower than real time.",
			[]float64{1, 2, 5, 10, 20, 40, 60, 120, 300}),
		media: reg.NewCounter("worker_media_seconds_total", "Seconds of source video that went through TRANSCODE."),
	}
	reg.AddCollector(func(w *metrics.Writer) {
		w.Gauge("worker_active_jobs", "Jobs being processed by this worker right now.", float64(m.active.Load()))
		w.Gauge("worker_concurrency", "Configured number of parallel jobs.", float64(max(concurrency, 1)))
		if free, err := freeBytes(workDir); err == nil {
			w.Gauge("worker_scratch_free_bytes", "Free space on the scratch filesystem.", float64(free))
		}
		var used int64
		_ = filepath.WalkDir(workDir, func(_ string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if info, ierr := d.Info(); ierr == nil {
					used += info.Size()
				}
			}
			return nil
		})
		w.Gauge("worker_scratch_used_bytes", "Bytes of scratch space held by in-flight and cached jobs.", float64(used))
	})
	reg.AddCollector(func(w *metrics.Writer) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		stats, err := store.QueueStats(ctx)
		if err != nil {
			w.Gauge("worker_queue_scrape_ok", "1 when the queue could be read from the database.", 0)
			return
		}
		w.Gauge("worker_queue_scrape_ok", "1 when the queue could be read from the database.", 1)
		count := map[[2]string]QueueStat{}
		for _, q := range stats {
			count[[2]string{q.Type, q.Status}] = q
		}
		var n, age []metrics.Sample
		for _, t := range jobTypes { // explicit zeros: alerts need a value, not an absent series
			for _, st := range []string{"QUEUED", "PROCESSING", "FAILED"} {
				q := count[[2]string{t, st}]
				l := []string{"type", t, "status", st}
				n = append(n, metrics.Sample{Labels: l, Value: float64(q.Count)})
				age = append(age, metrics.Sample{Labels: l, Value: q.OldestSec})
			}
		}
		w.GaugeSamples("worker_queue_jobs", "Jobs in the queue (FAILED = last 24 h).", n)
		w.GaugeSamples("worker_queue_oldest_seconds", "Age of the oldest job per bucket (QUEUED: time waiting).", age)
	})
	return m
}

// started/finished bracket a job attempt: queue wait and the active-jobs gauge.
func (m *Metrics) started(j Job) {
	if m == nil {
		return
	}
	m.active.Add(1)
	m.wait.Observe(j.WaitSec, j.Type)
}

func (m *Metrics) finished() {
	if m != nil {
		m.active.Add(-1)
	}
}

// failure counts a failed attempt under a bounded kind derived from the error code.
func (m *Metrics) failure(je *JobError) {
	if m != nil {
		m.fails.Inc(failureKind(je))
	}
}

// encoded records TRANSCODE throughput: wall time per minute of source video.
func (m *Metrics) Encoded(mediaSec float64, elapsed time.Duration) {
	if m == nil || mediaSec <= 0 {
		return
	}
	m.media.Add(mediaSec)
	m.rate.Observe(elapsed.Seconds() / (mediaSec / 60))
}

func failureKind(je *JobError) string {
	switch je.Code {
	case "ffprobe_failed":
		return "ffprobe"
	case "ffmpeg_failed", "ffmpeg_timeout":
		return "ffmpeg"
	case "storage_failed", "storage_verify", "download_failed", "original_missing":
		return "storage"
	case "output_invalid", "thumbnail_invalid", "variants_missing":
		return "validation_output"
	case "scanner_unavailable":
		return "scanner"
	case "disk_full", "workdir":
		return "disk"
	case "blocked_content":
		return "blocked"
	case "video_unavailable":
		return "unavailable"
	}
	if je.Permanent {
		return "input_rejected" // the upload broke a limit or is not a video: not a platform fault
	}
	return "other"
}

func (m *Metrics) observe(j Job, result string, d time.Duration) {
	if m == nil {
		return
	}
	m.jobs.Inc(j.Type, result)
	m.dur.Observe(d.Seconds(), j.Type)
}

func (m *Metrics) cleanup(result string) {
	if m != nil {
		m.clean.Inc(result)
	}
}
