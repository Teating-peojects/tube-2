package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func scrape(r *Registry) string {
	rr := httptest.NewRecorder()
	r.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/metrics", nil))
	return rr.Body.String()
}

func TestCounterGaugeHistogramFormat(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("jobs_total", "Jobs.", "type", "result")
	c.Inc("HLS", "ok")
	c.Add(2, "HLS", "ok")
	c.Inc("METADATA", "failed")
	h := r.NewHistogram("job_seconds", "Duration.", []float64{1, 5}, "type")
	h.Observe(0.5, "HLS")
	h.Observe(3, "HLS")
	h.Observe(99, "HLS")
	r.AddCollector(func(w *Writer) {
		w.GaugeSamples("queue_jobs", "Queue.", []Sample{{Labels: []string{"status", "QUEUED"}, Value: 4}})
	})
	out := scrape(r)
	for _, want := range []string{
		"# TYPE jobs_total counter",
		`jobs_total{type="HLS",result="ok"} 3`,
		`jobs_total{type="METADATA",result="failed"} 1`,
		`job_seconds_bucket{type="HLS",le="1"} 1`,
		`job_seconds_bucket{type="HLS",le="5"} 2`,
		`job_seconds_bucket{type="HLS",le="+Inf"} 3`,
		`job_seconds_count{type="HLS"} 3`,
		`queue_jobs{status="QUEUED"} 4`,
		"go_goroutines",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestLabelEscapingAndCardinalityCap(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("x_total", "x", "v")
	c.Inc("a\"b\\c\nd")
	if out := scrape(r); !strings.Contains(out, `x_total{v="a\"b\\c\nd"} 1`) {
		t.Errorf("label not escaped:\n%s", out)
	}
	for i := 0; i < maxSeries*2; i++ {
		c.Inc(strings.Repeat("x", 1) + string(rune('A'+i%26)) + string(rune('a'+i/26%26)) + string(rune('0'+i/676)))
	}
	if n := strings.Count(scrape(r), "x_total{"); n > maxSeries {
		t.Errorf("%d series, cap is %d", n, maxSeries)
	}
}

func TestHTTPMiddlewareUsesBoundedLabels(t *testing.T) {
	r := NewRegistry()
	h := r.HTTP()(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "missing") {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	for _, p := range []string{"/api/v1/videos/0190aaaa-bbbb/comments", "/api/v1/videos/xyz", "/stream/123.secrettoken/hls/v/master.m3u8", "/health", "/api/v1/missing", "/weird/path"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", p, nil))
	}
	out := scrape(r)
	for _, want := range []string{`route="videos",class="2xx"} 2`, `route="stream"`, `route="health"`, `route="missing",class="4xx"`, `route="other"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "secrettoken") || strings.Contains(out, "0190aaaa") {
		t.Error("ids or tokens leaked into labels")
	}
}
