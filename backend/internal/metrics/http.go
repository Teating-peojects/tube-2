package metrics

import (
	"net/http"
	"strings"
	"time"
)

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (s *statusWriter) WriteHeader(c int) { s.code = c; s.ResponseWriter.WriteHeader(c) }
func (s *statusWriter) Write(b []byte) (int, error) {
	if s.code == 0 {
		s.code = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter } // websockets, flushing

// Route reduces a path to a bounded label: "/api/v1/videos/123/comments" -> "videos", the stream and
// health endpoints keep their names, everything else is "other". Ids and tokens never become labels.
func Route(path string) string {
	switch {
	case strings.HasPrefix(path, "/api/v1/"):
		seg := strings.SplitN(strings.TrimPrefix(path, "/api/v1/"), "/", 2)[0]
		if seg == "" {
			return "other"
		}
		return seg
	case strings.HasPrefix(path, "/stream/"):
		return "stream"
	case path == "/health" || path == "/ready":
		return path[1:]
	}
	return "other"
}

func class(code int) string {
	if code < 100 || code > 599 {
		return "unknown"
	}
	return string(rune('0'+code/100)) + "xx"
}

// HTTP records request counts and latency per method, route group and status class.
func (r *Registry) HTTP() func(http.Handler) http.Handler {
	total := r.NewCounter("http_requests_total", "HTTP requests by method, route group and status class.", "method", "route", "class")
	dur := r.NewHistogram("http_request_duration_seconds", "HTTP request latency.", nil, "route")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, req)
			if sw.code == 0 {
				sw.code = http.StatusOK
			}
			route := Route(req.URL.Path)
			method := req.Method
			switch method {
			case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
			default:
				method = "OTHER"
			}
			total.Inc(method, route, class(sw.code))
			dur.Observe(time.Since(start).Seconds(), route)
		})
	}
}
