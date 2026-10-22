package server

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMetricsEndpointReportsRequestsByRouteClass(t *testing.T) {
	handler := NewHandler(Options{
		API: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }),
	})
	do(t, handler, http.MethodGet, "/healthz")
	do(t, handler, http.MethodGet, "/healthz")
	do(t, handler, http.MethodGet, "/api/v1/repos")
	recorder := do(t, handler, http.MethodGet, "/metrics")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain; version=0.0.4") {
		t.Errorf("content type = %q", got)
	}
	body := recorder.Body.String()
	for _, want := range []string{
		`vibefluence_http_requests_total{route="probe",method="GET",status="200"} 2`,
		`vibefluence_http_requests_total{route="api",method="GET",status="418"} 1`,
		`vibefluence_http_request_duration_seconds_bucket{route="probe",method="GET",status="200",le="+Inf"} 2`,
		`vibefluence_http_request_duration_seconds_count{route="probe",method="GET",status="200"} 2`,
		"# TYPE vibefluence_http_request_duration_seconds histogram",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics lack %q\n%s", want, body)
		}
	}
}

func TestMetricsHistogramBucketsAreCumulative(t *testing.T) {
	m := newMetrics()
	key := metricKey{route: "api", method: "GET", status: 200}
	m.observe(key, 20*time.Millisecond)
	m.observe(key, 300*time.Millisecond)
	m.observe(key, 30*time.Second)
	body := string(m.render())
	for _, want := range []string{
		`le="0.005"} 0`,
		`le="0.025"} 1`,
		`le="0.5"} 2`,
		`le="10"} 2`,
		`le="+Inf"} 3`,
		`_count{route="api",method="GET",status="200"} 3`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q\n%s", want, body)
		}
	}
}

func TestRouteClassCoversEveryArea(t *testing.T) {
	cases := map[string]string{
		"/healthz": "probe", "/readyz": "probe", "/metrics": "metrics", "/mcp": "mcp",
		"/api/v1/x": "api", "/": "static", "/r/platform/docs/a.md": "static",
	}
	for path, want := range cases {
		if got := routeClass(path); got != want {
			t.Errorf("routeClass(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestMetricsRenderIsSortedAndStable(t *testing.T) {
	m := newMetrics()
	m.observe(metricKey{route: "b", method: "GET", status: 200}, time.Millisecond)
	m.observe(metricKey{route: "a", method: "POST", status: 500}, time.Millisecond)
	m.observe(metricKey{route: "a", method: "GET", status: 404}, time.Millisecond)
	m.observe(metricKey{route: "a", method: "GET", status: 200}, time.Millisecond)
	body := string(m.render())
	order := []string{
		`route="a",method="GET",status="200"} 1`,
		`route="a",method="GET",status="404"} 1`,
		`route="a",method="POST",status="500"} 1`,
		`route="b",method="GET",status="200"} 1`,
	}
	last := -1
	for _, fragment := range order {
		index := strings.Index(body, "requests_total{"+fragment)
		if index < last {
			t.Fatalf("%q out of order", fragment)
		}
		last = index
	}
}
