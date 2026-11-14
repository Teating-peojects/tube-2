package worker

import (
	"context"
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
	jobs *metrics.Counter
	dur  *metrics.Histogram
}

var jobTypes = []string{"METADATA", "THUMBNAIL", "TRANSCODE", "HLS"}

// NewMetrics registers the worker metrics and a scrape-time queue collector.
func NewMetrics(reg *metrics.Registry, store interface {
	QueueStats(context.Context) ([]QueueStat, error)
}) *Metrics {
	m := &Metrics{
		jobs: reg.NewCounter("worker_jobs_total", "Finished job attempts by type and result (completed, retry, failed, released, lease_lost).", "type", "result"),
		dur: reg.NewHistogram("worker_job_duration_seconds", "Wall time of a job attempt.",
			[]float64{1, 5, 15, 30, 60, 120, 300, 600, 1200, 3600}, "type"),
	}
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

func (m *Metrics) observe(j Job, result string, d time.Duration) {
	if m == nil {
		return
	}
	m.jobs.Inc(j.Type, result)
	m.dur.Observe(d.Seconds(), j.Type)
}
