// Package metrics is a small, dependency-free Prometheus exposition (text format 0.0.4): counters,
// gauges, histograms and scrape-time collectors. It exists so the API and the worker can be scraped
// without pulling a client library into a security-sensitive binary.
//
// Serve it on a separate, internal listener (METRICS_ADDR), never on the public mux.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const maxSeries = 500 // per metric: label values must never come from user input, this is the backstop

type Registry struct {
	mu         sync.Mutex
	metrics    []writer
	collectors []func(*Writer)
}

type writer interface{ write(*Writer) }

// Writer renders metric lines for scrape-time collectors.
type Writer struct{ w io.Writer }

func (w *Writer) Gauge(name, help string, v float64, labels ...string) {
	fmt.Fprintf(w.w, "# HELP %s %s\n# TYPE %s gauge\n%s%s %s\n", name, help, name, name, fmtLabels(nil, labels), fmtFloat(v))
}

// GaugeVec writes several samples of one gauge: each sample is a (labels..., value) pair.
func (w *Writer) GaugeSamples(name, help string, samples []Sample) {
	fmt.Fprintf(w.w, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name)
	for _, s := range samples {
		fmt.Fprintf(w.w, "%s%s %s\n", name, fmtLabels(nil, s.Labels), fmtFloat(s.Value))
	}
}

type Sample struct {
	Labels []string // name, value, name, value ...
	Value  float64
}

func NewRegistry() *Registry {
	r := &Registry{}
	r.AddCollector(func(w *Writer) {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		w.Gauge("go_goroutines", "Number of goroutines.", float64(runtime.NumGoroutine()))
		w.Gauge("go_memstats_alloc_bytes", "Bytes of allocated heap objects.", float64(m.Alloc))
		w.Gauge("go_memstats_sys_bytes", "Bytes obtained from the system.", float64(m.Sys))
	})
	return r
}

// AddCollector registers a function called on every scrape (queue depth, pool stats...).
func (r *Registry) AddCollector(f func(*Writer)) {
	r.mu.Lock()
	r.collectors = append(r.collectors, f)
	r.mu.Unlock()
}

// AddCounters exposes an in-process Counters (see counters.go) as one labelled counter family:
// name{event="<counter name>"} value. Counter names are fixed in code, never user input.
func (r *Registry) AddCounters(name, help string, c *Counters) {
	r.AddCollector(func(w *Writer) {
		snap, names := c.Snapshot()
		fmt.Fprintf(w.w, "# HELP %s %s\n# TYPE %s counter\n", name, help, name)
		for _, n := range names {
			fmt.Fprintf(w.w, "%s%s %d\n", name, fmtLabels(nil, []string{"event", n}), snap[n])
		}
	})
}

func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		mw := &Writer{w: w}
		r.mu.Lock()
		ms := append([]writer{}, r.metrics...)
		cs := append([]func(*Writer){}, r.collectors...)
		r.mu.Unlock()
		for _, m := range ms {
			m.write(mw)
		}
		for _, c := range cs {
			c(mw)
		}
	})
}

func (r *Registry) add(m writer) {
	r.mu.Lock()
	r.metrics = append(r.metrics, m)
	r.mu.Unlock()
}

// ---------- counter ----------

type Counter struct {
	name, help string
	labels     []string
	mu         sync.Mutex
	v          map[string]*float64
	keys       map[string][]string
}

func (r *Registry) NewCounter(name, help string, labelNames ...string) *Counter {
	c := &Counter{name: name, help: help, labels: labelNames, v: map[string]*float64{}, keys: map[string][]string{}}
	r.add(c)
	return c
}

func (c *Counter) Add(n float64, labelValues ...string) {
	k := strings.Join(labelValues, "\xff")
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.v[k]
	if !ok {
		if len(c.v) >= maxSeries {
			return
		}
		p = new(float64)
		c.v[k], c.keys[k] = p, labelValues
	}
	*p += n
}

func (c *Counter) Inc(labelValues ...string) { c.Add(1, labelValues...) }

func (c *Counter) write(w *Writer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprintf(w.w, "# HELP %s %s\n# TYPE %s counter\n", c.name, c.help, c.name)
	for _, k := range sortedKeys(c.v) {
		fmt.Fprintf(w.w, "%s%s %s\n", c.name, fmtLabels(c.labels, c.keys[k]), fmtFloat(*c.v[k]))
	}
}

// ---------- histogram ----------

type Histogram struct {
	name, help string
	labels     []string
	buckets    []float64
	mu         sync.Mutex
	s          map[string]*hist
}

type hist struct {
	vals   []string
	counts []uint64 // per bucket, non-cumulative; the last slot is +Inf
	sum    float64
	n      uint64
}

var DefaultBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}

func (r *Registry) NewHistogram(name, help string, buckets []float64, labelNames ...string) *Histogram {
	if len(buckets) == 0 {
		buckets = DefaultBuckets
	}
	h := &Histogram{name: name, help: help, labels: labelNames, buckets: buckets, s: map[string]*hist{}}
	r.add(h)
	return h
}

func (h *Histogram) Observe(v float64, labelValues ...string) {
	k := strings.Join(labelValues, "\xff")
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.s[k]
	if !ok {
		if len(h.s) >= maxSeries {
			return
		}
		s = &hist{vals: labelValues, counts: make([]uint64, len(h.buckets)+1)}
		h.s[k] = s
	}
	i := sort.SearchFloat64s(h.buckets, v) // first bucket with upper bound >= v
	s.counts[i]++
	s.sum += v
	s.n++
}

func (h *Histogram) write(w *Writer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	fmt.Fprintf(w.w, "# HELP %s %s\n# TYPE %s histogram\n", h.name, h.help, h.name)
	for _, k := range sortedKeys(h.s) {
		s := h.s[k]
		var cum uint64
		for i, b := range h.buckets {
			cum += s.counts[i]
			fmt.Fprintf(w.w, "%s_bucket%s %d\n", h.name, fmtLabels(append(append([]string{}, h.labels...), "le"), append(append([]string{}, s.vals...), fmtFloat(b))), cum)
		}
		cum += s.counts[len(h.buckets)]
		fmt.Fprintf(w.w, "%s_bucket%s %d\n", h.name, fmtLabels(append(append([]string{}, h.labels...), "le"), append(append([]string{}, s.vals...), "+Inf")), cum)
		fmt.Fprintf(w.w, "%s_sum%s %s\n%s_count%s %d\n", h.name, fmtLabels(h.labels, s.vals), fmtFloat(s.sum), h.name, fmtLabels(h.labels, s.vals), s.n)
	}
}

// ---------- helpers ----------

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func fmtFloat(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

// fmtLabels renders {a="x",b="y"}. With names == nil, vals is a flat name,value,... list.
func fmtLabels(names, vals []string) string {
	if len(vals) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('{')
	if names == nil {
		for i := 0; i+1 < len(vals); i += 2 {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "%s=\"%s\"", vals[i], escape(vals[i+1]))
		}
	} else {
		for i, n := range names {
			if i >= len(vals) {
				break
			}
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "%s=\"%s\"", n, escape(vals[i]))
		}
	}
	b.WriteByte('}')
	return b.String()
}

func escape(s string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`).Replace(s)
}
