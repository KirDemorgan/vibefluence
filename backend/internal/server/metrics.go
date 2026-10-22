package server

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var durationBuckets = []float64{0.005, 0.025, 0.1, 0.25, 0.5, 1, 2.5, 10}

type metricKey struct {
	route  string
	method string
	status int
}

type series struct {
	count   uint64
	sum     float64
	buckets []uint64
}

type metrics struct {
	mu     sync.Mutex
	series map[metricKey]*series
}

func newMetrics() *metrics {
	return &metrics{series: make(map[metricKey]*series)}
}

func routeClass(path string) string {
	switch {
	case path == "/healthz" || path == "/readyz":
		return "probe"
	case path == "/metrics":
		return "metrics"
	case path == "/mcp":
		return "mcp"
	case strings.HasPrefix(path, "/api/"):
		return "api"
	default:
		return "static"
	}
}

func (m *metrics) observe(key metricKey, elapsed time.Duration) {
	seconds := elapsed.Seconds()
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.series[key]
	if !ok {
		s = &series{buckets: make([]uint64, len(durationBuckets))}
		m.series[key] = s
	}
	s.count++
	s.sum += seconds
	for i, bound := range durationBuckets {
		if seconds <= bound {
			s.buckets[i]++
		}
	}
}

func (m *metrics) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		m.observe(metricKey{route: routeClass(r.URL.Path), method: r.Method, status: status}, time.Since(started))
	})
}

func (m *metrics) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write(m.render())
	})
}

func (m *metrics) render() []byte {
	m.mu.Lock()
	keys := make([]metricKey, 0, len(m.series))
	snapshot := make(map[metricKey]series, len(m.series))
	for key, s := range m.series {
		keys = append(keys, key)
		snapshot[key] = series{count: s.count, sum: s.sum, buckets: append([]uint64(nil), s.buckets...)}
	}
	m.mu.Unlock()
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.route != b.route {
			return a.route < b.route
		}
		if a.method != b.method {
			return a.method < b.method
		}
		return a.status < b.status
	})

	var out strings.Builder
	out.WriteString("# HELP vibefluence_http_requests_total Completed HTTP requests.\n")
	out.WriteString("# TYPE vibefluence_http_requests_total counter\n")
	for _, key := range keys {
		fmt.Fprintf(&out, "vibefluence_http_requests_total{%s} %d\n", labels(key), snapshot[key].count)
	}
	out.WriteString("# HELP vibefluence_http_request_duration_seconds HTTP request latency.\n")
	out.WriteString("# TYPE vibefluence_http_request_duration_seconds histogram\n")
	for _, key := range keys {
		s := snapshot[key]
		for i, bound := range durationBuckets {
			fmt.Fprintf(&out, "vibefluence_http_request_duration_seconds_bucket{%s,le=\"%s\"} %d\n",
				labels(key), strconv.FormatFloat(bound, 'g', -1, 64), s.buckets[i])
		}
		fmt.Fprintf(&out, "vibefluence_http_request_duration_seconds_bucket{%s,le=\"+Inf\"} %d\n", labels(key), s.count)
		fmt.Fprintf(&out, "vibefluence_http_request_duration_seconds_sum{%s} %g\n", labels(key), s.sum)
		fmt.Fprintf(&out, "vibefluence_http_request_duration_seconds_count{%s} %d\n", labels(key), s.count)
	}
	return []byte(out.String())
}

func labels(key metricKey) string {
	return fmt.Sprintf("route=%q,method=%q,status=\"%d\"", key.route, key.method, key.status)
}
