package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

func newTestAssets() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           {Data: []byte("<html>app</html>")},
		"main-ABCD1234.js":     {Data: []byte("console.log(1)")},
		"favicon.ico":          {Data: []byte("ico")},
		"logo-vibefluence.svg": {Data: []byte("<svg/>")},
		".gitkeep":             {Data: []byte("")},
		"nested/page.txt":      {Data: []byte("nested")},
	}
}

func do(t *testing.T, handler http.Handler, method, target string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Set(headers[i], headers[i+1])
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decodeJSON(t *testing.T, recorder *httptest.ResponseRecorder, into any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), into); err != nil {
		t.Fatalf("body is not JSON: %v: %s", err, recorder.Body.String())
	}
}

func TestHealthz(t *testing.T) {
	handler := NewHandler(Options{})
	recorder := do(t, handler, http.MethodGet, "/healthz")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var body healthBody
	decodeJSON(t, recorder, &body)
	if body.Status != "ok" {
		t.Errorf("status = %q", body.Status)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("content type = %q", got)
	}
}

func TestReadyz(t *testing.T) {
	tests := []struct {
		name       string
		checks     []ReadinessCheck
		wantStatus int
		wantBody   string
		wantChecks map[string]string
	}{
		{
			name:       "no checks is ready",
			wantStatus: http.StatusOK,
			wantBody:   "ok",
			wantChecks: map[string]string{},
		},
		{
			name: "all checks pass",
			checks: []ReadinessCheck{
				{Name: "gitlab", Check: func(context.Context) error { return nil }},
			},
			wantStatus: http.StatusOK,
			wantBody:   "ok",
			wantChecks: map[string]string{"gitlab": "ok"},
		},
		{
			name: "failing check makes service unavailable",
			checks: []ReadinessCheck{
				{Name: "gitlab", Check: func(context.Context) error { return errors.New("token expired") }},
				{Name: "data", Check: func(context.Context) error { return nil }},
			},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "unavailable",
			wantChecks: map[string]string{"gitlab": "failed", "data": "ok"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := do(t, NewHandler(Options{Checks: tt.checks}), http.MethodGet, "/readyz")
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			var body healthBody
			decodeJSON(t, recorder, &body)
			if body.Status != tt.wantBody {
				t.Errorf("status = %q, want %q", body.Status, tt.wantBody)
			}
			for name, want := range tt.wantChecks {
				if body.Checks[name] != want {
					t.Errorf("check %s = %q, want %q", name, body.Checks[name], want)
				}
			}
		})
	}
}

func TestProbesRejectOtherMethods(t *testing.T) {
	handler := NewHandler(Options{})
	if recorder := do(t, handler, http.MethodPost, "/healthz"); recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d", recorder.Code)
	}
}

func TestRequestID(t *testing.T) {
	handler := NewHandler(Options{})

	generated := do(t, handler, http.MethodGet, "/healthz").Header().Get(requestIDHeader)
	if len(generated) != 32 {
		t.Errorf("generated id = %q", generated)
	}

	kept := do(t, handler, http.MethodGet, "/healthz", requestIDHeader, "trace-123.abc").Header().Get(requestIDHeader)
	if kept != "trace-123.abc" {
		t.Errorf("id was not propagated: %q", kept)
	}

	replaced := do(t, handler, http.MethodGet, "/healthz", requestIDHeader, "bad id\twith spaces").Header().Get(requestIDHeader)
	if replaced == "bad id\twith spaces" || len(replaced) != 32 {
		t.Errorf("invalid id was not replaced: %q", replaced)
	}
}

func TestRequestIDAvailableInContext(t *testing.T) {
	var seen string
	handler := withRequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = RequestID(r.Context())
	}))
	do(t, handler, http.MethodGet, "/", requestIDHeader, "abc")
	if seen != "abc" {
		t.Errorf("context id = %q", seen)
	}
	if RequestID(context.Background()) != "" {
		t.Error("empty context must have no id")
	}
}

func TestContentSecurityPolicyForbidsInlineScriptsAndFraming(t *testing.T) {
	for _, directive := range []string{"script-src 'self';", "object-src 'none'", "frame-ancestors 'none'", "base-uri 'self'"} {
		if !strings.Contains(contentSecurityPolicy, directive) {
			t.Errorf("policy lacks %q", directive)
		}
	}
	if strings.Contains(contentSecurityPolicy, "script-src 'self' 'unsafe") {
		t.Error("scripts must not allow unsafe sources")
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler := NewHandler(Options{Assets: newTestAssets()})
	recorder := do(t, handler, http.MethodGet, "/healthz")
	want := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "strict-origin-when-cross-origin",
		"Content-Security-Policy": contentSecurityPolicy,
	}
	for name, value := range want {
		if got := recorder.Header().Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
	if got := do(t, handler, http.MethodGet, "/api/anything").Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("api cache-control = %q", got)
	}
}

func TestAccessLog(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))
	handler := NewHandler(Options{Logger: logger, Assets: newTestAssets()})

	do(t, handler, http.MethodGet, "/nested/page.txt", requestIDHeader, "req-1")

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &record); err != nil {
		t.Fatalf("log is not JSON: %v: %s", err, out.String())
	}
	if record["msg"] != "request" || record["method"] != "GET" || record["path"] != "/nested/page.txt" {
		t.Errorf("unexpected record: %v", record)
	}
	if record["status"] != float64(200) || record["request_id"] != "req-1" || record["level"] != "INFO" {
		t.Errorf("unexpected record: %v", record)
	}
}

func TestAccessLogProbesAreDebug(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: slog.LevelInfo}))
	handler := NewHandler(Options{Logger: logger})

	do(t, handler, http.MethodGet, "/healthz")
	do(t, handler, http.MethodGet, "/readyz")

	if out.Len() != 0 {
		t.Errorf("probes must not log at info: %s", out.String())
	}
}

func TestRecoverTurnsPanicIntoJSON500(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&out, nil))
	handler := withRequestID(withRecover(logger, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))

	recorder := do(t, handler, http.MethodGet, "/", requestIDHeader, "req-9")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", recorder.Code)
	}
	var body errorBody
	decodeJSON(t, recorder, &body)
	if body.Error != "internal_error" {
		t.Errorf("body = %+v", body)
	}
	if strings.Contains(recorder.Body.String(), "boom") {
		t.Error("panic value leaked to client")
	}
	if !strings.Contains(out.String(), "boom") || !strings.Contains(out.String(), "req-9") {
		t.Errorf("panic not logged with request id: %s", out.String())
	}
}

func TestRecoverRepanicsOnAbortHandler(t *testing.T) {
	handler := withRecover(slog.New(slog.DiscardHandler), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	defer func() {
		recovered, _ := recover().(error)
		if !errors.Is(recovered, http.ErrAbortHandler) {
			t.Errorf("recovered = %v", recovered)
		}
	}()
	do(t, handler, http.MethodGet, "/")
}

func TestSPA(t *testing.T) {
	handler := NewHandler(Options{Assets: newTestAssets()})

	tests := []struct {
		name         string
		method       string
		target       string
		wantStatus   int
		wantBody     string
		wantCache    string
		wantNoBody   bool
		wantJSONCode string
		wantNoCache  bool
		wantAllow    string
	}{
		{name: "root serves index", method: http.MethodGet, target: "/", wantStatus: 200, wantBody: "<html>app</html>", wantCache: "no-cache"},
		{name: "client route falls back to index", method: http.MethodGet, target: "/repos/backend/docs/a.md", wantStatus: 200, wantBody: "<html>app</html>", wantCache: "no-cache"},
		{name: "fingerprinted asset is immutable", method: http.MethodGet, target: "/main-ABCD1234.js", wantStatus: 200, wantBody: "console.log(1)", wantCache: "public, max-age=31536000, immutable"},
		{name: "plain asset has no immutable cache", method: http.MethodGet, target: "/favicon.ico", wantStatus: 200, wantBody: "ico", wantNoCache: true},
		{name: "hyphenated name is not mistaken for a fingerprint", method: http.MethodGet, target: "/logo-vibefluence.svg", wantStatus: 200, wantBody: "<svg/>", wantNoCache: true},
		{name: "stale fingerprinted chunk is a real 404", method: http.MethodGet, target: "/main-OLDHASH1.js", wantStatus: 404},
		{name: "missing stylesheet is a real 404", method: http.MethodGet, target: "/styles.css", wantStatus: 404},
		{name: "dotfiles are hidden", method: http.MethodGet, target: "/.gitkeep", wantStatus: 404},
		{name: "post is rejected", method: http.MethodPost, target: "/anything", wantStatus: 405, wantAllow: "GET, HEAD"},
		{name: "options is rejected", method: http.MethodOptions, target: "/", wantStatus: 405, wantAllow: "GET, HEAD"},
		{name: "nested asset", method: http.MethodGet, target: "/nested/page.txt", wantStatus: 200, wantBody: "nested", wantNoCache: true},
		{name: "directory falls back to index", method: http.MethodGet, target: "/nested/", wantStatus: 200, wantBody: "<html>app</html>"},
		{name: "unknown api path is json 404", method: http.MethodGet, target: "/api/v1/nothing", wantStatus: 404, wantJSONCode: "not_found"},
		{name: "bare api path is json 404", method: http.MethodGet, target: "/api", wantStatus: 404, wantJSONCode: "not_found"},
		{name: "head index has no body", method: http.MethodHead, target: "/", wantStatus: 200, wantNoBody: true, wantCache: "no-cache"},
		{name: "path traversal is redirected to the cleaned path", method: http.MethodGet, target: "/../../etc/passwd", wantStatus: 307},
		{name: "cleaned traversal target falls back to index", method: http.MethodGet, target: "/etc/passwd", wantStatus: 200, wantBody: "<html>app</html>"},
		{name: "encoded traversal never leaves assets", method: http.MethodGet, target: "/%2e%2e/%2e%2e/etc/passwd", wantStatus: 200, wantBody: "<html>app</html>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := do(t, handler, tt.method, tt.target)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			if tt.wantJSONCode != "" {
				var body errorBody
				decodeJSON(t, recorder, &body)
				if body.Error != tt.wantJSONCode {
					t.Errorf("error code = %q", body.Error)
				}
				return
			}
			if tt.wantNoBody && recorder.Body.Len() != 0 {
				t.Errorf("HEAD returned body: %q", recorder.Body.String())
			}
			if tt.wantBody != "" && recorder.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", recorder.Body.String(), tt.wantBody)
			}
			if tt.wantAllow != "" && recorder.Header().Get("Allow") != tt.wantAllow {
				t.Errorf("allow = %q, want %q", recorder.Header().Get("Allow"), tt.wantAllow)
			}
			if tt.wantNoCache && strings.Contains(recorder.Header().Get("Cache-Control"), "immutable") {
				t.Errorf("unexpected immutable cache: %q", recorder.Header().Get("Cache-Control"))
			}
			if tt.wantCache != "" && recorder.Header().Get("Cache-Control") != tt.wantCache {
				t.Errorf("cache-control = %q, want %q", recorder.Header().Get("Cache-Control"), tt.wantCache)
			}
		})
	}
}

func TestSPAWithoutIndexReturnsJSON404(t *testing.T) {
	handler := NewHandler(Options{Assets: fstest.MapFS{"robots.txt": {Data: []byte("x")}}})

	recorder := do(t, handler, http.MethodGet, "/some/route")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d", recorder.Code)
	}
	var body errorBody
	decodeJSON(t, recorder, &body)
	if body.Error != "not_found" {
		t.Errorf("body = %+v", body)
	}
	if do(t, handler, http.MethodGet, "/robots.txt").Body.String() != "x" {
		t.Error("existing asset must still be served")
	}
}

func TestNoAssetsMeansNoCatchAll(t *testing.T) {
	recorder := do(t, NewHandler(Options{}), http.MethodGet, "/anything")
	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d", recorder.Code)
	}
}

func TestServeGracefulShutdownLetsRequestFinish(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = io.WriteString(w, "done")
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner := Runner{Handler: slow, Logger: slog.New(slog.DiscardHandler), ShutdownTimeout: 5 * time.Second}

	serveDone := make(chan error, 1)
	go func() { serveDone <- runner.Serve(ctx, listener) }()

	var wg sync.WaitGroup
	var body string
	var requestErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		response, err := http.Get("http://" + listener.Addr().String())
		if err != nil {
			requestErr = err
			return
		}
		defer func() { _ = response.Body.Close() }()
		raw, _ := io.ReadAll(response.Body)
		body = string(raw)
	}()

	<-started
	cancel()
	waitUntilRefused(t, listener.Addr().String())
	close(release)
	wg.Wait()

	if requestErr != nil {
		t.Fatalf("in-flight request failed: %v", requestErr)
	}
	if body != "done" {
		t.Errorf("body = %q", body)
	}
	select {
	case err := <-serveDone:
		if err != nil {
			t.Errorf("Serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after shutdown")
	}
}

func TestServeForcesCloseWhenShutdownTimesOut(t *testing.T) {
	started := make(chan struct{})
	block := make(chan struct{})
	defer close(block)
	stuck := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-block
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner := Runner{Handler: stuck, Logger: slog.New(slog.DiscardHandler), ShutdownTimeout: 100 * time.Millisecond}

	serveDone := make(chan error, 1)
	go func() { serveDone <- runner.Serve(ctx, listener) }()
	go func() {
		response, err := http.Get("http://" + listener.Addr().String())
		if err == nil {
			_ = response.Body.Close()
		}
	}()

	<-started
	cancel()
	select {
	case err := <-serveDone:
		if err == nil || !strings.Contains(err.Error(), "graceful shutdown") {
			t.Errorf("expected graceful shutdown error, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return")
	}
}

func TestServeReturnsListenerFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = listener.Close()

	runner := Runner{Handler: http.NotFoundHandler(), Logger: slog.New(slog.DiscardHandler), ShutdownTimeout: time.Second}
	if err := runner.Serve(context.Background(), listener); err == nil {
		t.Error("expected error from closed listener")
	}
}

func waitUntilRefused(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err != nil {
			return
		}
		_ = conn.Close()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("server kept accepting connections after shutdown began")
}

func TestReadyzDoesNotLeakErrorDetails(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&out, nil))
	handler := NewHandler(Options{Logger: logger, Checks: []ReadinessCheck{
		{Name: "gitlab", Check: func(context.Context) error { return errors.New("dial https://internal.example:8443 refused") }},
	}})

	recorder := do(t, handler, http.MethodGet, "/readyz")
	if strings.Contains(recorder.Body.String(), "internal.example") {
		t.Errorf("internal details leaked: %s", recorder.Body.String())
	}
	if !strings.Contains(out.String(), "internal.example") {
		t.Errorf("failure must be logged for operators: %s", out.String())
	}
}

func TestPanicIsAccessLogged(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&out, nil))
	handler := NewHandler(Options{Logger: logger, API: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})})

	recorder := do(t, handler, http.MethodGet, "/api/v1/boom")
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", recorder.Code)
	}
	var sawRequest bool
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line is not JSON: %s", line)
		}
		if record["msg"] == "request" && record["status"] == float64(500) {
			sawRequest = true
		}
	}
	if !sawRequest {
		t.Errorf("panicking request has no access log entry: %s", out.String())
	}
}

func TestAPIAndMCPHandlersAreMounted(t *testing.T) {
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "api:"+r.Method+r.URL.Path) })
	mcp := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "mcp:"+r.Method) })
	handler := NewHandler(Options{API: api, MCP: mcp, Assets: newTestAssets()})

	if got := do(t, handler, http.MethodPost, "/api/v1/x").Body.String(); got != "api:POST/api/v1/x" {
		t.Errorf("api body = %q", got)
	}
	if got := do(t, handler, http.MethodPost, "/mcp").Body.String(); got != "mcp:POST" {
		t.Errorf("mcp body = %q", got)
	}
}

func TestZeroValueRunnerUsesDefaults(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner := Runner{Handler: http.NotFoundHandler()}

	done := make(chan error, 1)
	go func() { done <- runner.Serve(ctx, listener) }()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return")
	}
}

func TestCrossSiteWritesAreRejectedWithoutABody(t *testing.T) {
	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := NewHandler(Options{API: api})

	cross := httptest.NewRequest(http.MethodPost, "/api/v1/repos/x/drafts/1/publish", nil)
	cross.Header.Set("Sec-Fetch-Site", "cross-site")
	cross.Header.Set("Origin", "http://evil.example")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, cross)
	if recorder.Code != http.StatusForbidden {
		t.Errorf("cross-site status = %d", recorder.Code)
	}

	same := httptest.NewRequest(http.MethodPost, "/api/v1/repos/x/drafts/1/publish", nil)
	same.Header.Set("Sec-Fetch-Site", "same-origin")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, same)
	if recorder.Code != http.StatusNoContent {
		t.Errorf("same-origin status = %d", recorder.Code)
	}

	plain := httptest.NewRequest(http.MethodPost, "/api/v1/repos/x/drafts/1/publish", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, plain)
	if recorder.Code != http.StatusNoContent {
		t.Errorf("non-browser status = %d", recorder.Code)
	}
}
