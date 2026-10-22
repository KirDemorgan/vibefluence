package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vibefluence/internal/testing/fakegitlab"
)

const (
	token   = "secret-token"
	project = "group/docs"
)

type fixture struct {
	fake   *fakegitlab.Server
	server *httptest.Server
	client *Client
	sleeps []time.Duration
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{fake: fakegitlab.New(token)}
	f.fake.AddProject(project, "master", fakegitlab.ProjectOptions{})
	f.server = httptest.NewServer(f.fake)
	t.Cleanup(f.server.Close)
	f.client = f.newClient(t, f.server.URL, token)
	return f
}

func (f *fixture) newClient(t *testing.T, baseURL, apiToken string) *Client {
	t.Helper()
	client, err := New(Options{
		BaseURL: baseURL,
		Token:   apiToken,
		Sleep: func(_ context.Context, d time.Duration) error {
			f.sleeps = append(f.sleeps, d)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestNewValidatesOptions(t *testing.T) {
	tests := []struct {
		name string
		opts Options
	}{
		{"empty base url", Options{Token: "t"}},
		{"non http scheme", Options{BaseURL: "ftp://gitlab.example.com", Token: "t"}},
		{"missing host", Options{BaseURL: "https://", Token: "t"}},
		{"missing token", Options{BaseURL: "https://gitlab.example.com"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.opts); err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	client, err := New(Options{BaseURL: "https://gitlab.example.com/", Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if client.maxRetries != defaultMaxRetries || client.retryDelay != defaultRetryDelay || client.http == nil || client.sleep == nil {
		t.Errorf("defaults not applied: %+v", client)
	}
	noRetry, _ := New(Options{BaseURL: "https://gitlab.example.com", Token: "t", MaxRetries: -1})
	if noRetry.maxRetries != 0 {
		t.Errorf("negative MaxRetries must disable retries, got %d", noRetry.maxRetries)
	}
}

func TestCurrentUserAndProject(t *testing.T) {
	f := newFixture(t)
	user, err := f.client.CurrentUser(context.Background())
	if err != nil || user.Username != "vibefluence-bot" {
		t.Fatalf("user = %+v, err = %v", user, err)
	}
	found, err := f.client.Project(context.Background(), project)
	if err != nil || found.DefaultBranch != "master" || found.Path != project {
		t.Fatalf("project = %+v, err = %v", found, err)
	}
}

func TestProjectNotFound(t *testing.T) {
	f := newFixture(t)
	_, err := f.client.Project(context.Background(), "ghost/project")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestProjectWithoutDefaultBranchIsRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"id":1,"path_with_namespace":"a/b","default_branch":null}`)
	}))
	defer server.Close()
	client, _ := New(Options{BaseURL: server.URL, Token: "t"})
	if _, err := client.Project(context.Background(), "a/b"); err == nil || !strings.Contains(err.Error(), "default branch") {
		t.Errorf("err = %v", err)
	}
}

func TestErrorClassification(t *testing.T) {
	f := newFixture(t)

	unauthorized := f.newClient(t, f.server.URL, "wrong")
	_, err := unauthorized.CurrentUser(context.Background())
	if !errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrNotFound) {
		t.Errorf("401: err = %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized || apiErr.Message != "401 Unauthorized" {
		t.Errorf("api error = %+v", apiErr)
	}

	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer forbidden.Close()
	client, _ := New(Options{BaseURL: forbidden.URL, Token: "t"})
	_, err = client.CurrentUser(context.Background())
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("403: err = %v", err)
	}
	if !strings.Contains(err.Error(), "status 403") || strings.Contains(err.Error(), "status 403:") {
		t.Errorf("empty message formatting: %v", err)
	}
}

func TestErrorMessageVariants(t *testing.T) {
	tests := []struct {
		body string
		want string
	}{
		{`{"message":"plain"}`, "plain"},
		{`{"message":["a","b"]}`, `["a","b"]`},
		{`{"message":{"base":["x"]}}`, `{"base":["x"]}`},
		{`{"error":"ref is missing"}`, "ref is missing"},
		{`not json`, ""},
		{`{}`, ""},
	}
	for _, tt := range tests {
		if got := errorMessage([]byte(tt.body)); got != tt.want {
			t.Errorf("errorMessage(%s) = %q, want %q", tt.body, got, tt.want)
		}
	}
}

func seedDocs(f *fixture, count int) {
	for i := 0; i < count; i++ {
		f.fake.PutFile(project, "master", fmt.Sprintf("docs/section/doc-%03d.md", i), fmt.Sprintf("# Doc %d", i))
	}
	f.fake.PutFile(project, "master", "src/main.go", "package main")
}

func TestTreeFollowsKeysetPagination(t *testing.T) {
	f := newFixture(t)
	seedDocs(f, 250)

	entries, err := f.client.Tree(context.Background(), project, "master", "docs")
	if err != nil {
		t.Fatal(err)
	}
	var blobs int
	for _, entry := range entries {
		if entry.Type == "blob" {
			blobs++
			if !strings.HasPrefix(entry.Path, "docs/section/") {
				t.Errorf("unexpected path %q", entry.Path)
			}
		}
	}
	if blobs != 250 {
		t.Errorf("blobs = %d, want 250", blobs)
	}
	if len(entries) != 251 {
		t.Errorf("entries = %d, want 250 blobs and one directory", len(entries))
	}
}

func TestTreeUsesDefaultBranchWhenRefIsEmpty(t *testing.T) {
	f := newFixture(t)
	seedDocs(f, 2)
	entries, err := f.client.Tree(context.Background(), project, "", "docs")
	if err != nil || len(entries) != 3 {
		t.Errorf("entries = %d, err = %v", len(entries), err)
	}
}

func TestTreeMissingPath(t *testing.T) {
	f := newFixture(t)
	_, err := f.client.Tree(context.Background(), project, "master", "docs")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestTreeRejectsLinkToAnotherHost(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Link", `<https://evil.example/api/v4/projects/1/repository/tree?page_token=x>; rel="next"`)
		_, _ = fmt.Fprint(w, `[{"id":"1","name":"a.md","type":"blob","path":"docs/a.md","mode":"100644"}]`)
	}))
	defer server.Close()
	client, _ := New(Options{BaseURL: server.URL, Token: "t"})

	entries, err := client.Tree(context.Background(), project, "master", "docs")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || calls.Load() != 1 {
		t.Errorf("entries = %d, calls = %d: a foreign Link must not be followed", len(entries), calls.Load())
	}
}

func TestTreeRejectsLinkOutsideAPI(t *testing.T) {
	var calls atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Link", fmt.Sprintf(`<%s/admin/secrets>; rel="next"`, server.URL))
		_, _ = fmt.Fprint(w, `[]`)
	}))
	defer server.Close()
	client, _ := New(Options{BaseURL: server.URL, Token: "t"})

	if _, err := client.Tree(context.Background(), project, "master", "docs"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d: a Link outside /api/v4 must not be followed", calls.Load())
	}
}

func TestTreeStopsAfterTooManyPages(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", fmt.Sprintf(`<%s%s?page_token=x>; rel="next"`, server.URL, r.URL.EscapedPath()))
		_, _ = fmt.Fprint(w, `[]`)
	}))
	defer server.Close()
	client, _ := New(Options{BaseURL: server.URL, Token: "t"})

	_, err := client.Tree(context.Background(), project, "master", "docs")
	if err == nil || !strings.Contains(err.Error(), "pages") {
		t.Errorf("err = %v", err)
	}
}

func TestBlobAndFile(t *testing.T) {
	f := newFixture(t)
	f.fake.PutFile(project, "master", "docs/a.md", "# Title\n")

	entries, err := f.client.Tree(context.Background(), project, "master", "docs")
	if err != nil || len(entries) != 1 {
		t.Fatalf("tree: %v %v", entries, err)
	}
	content, err := f.client.Blob(context.Background(), project, entries[0].ID)
	if err != nil || string(content) != "# Title\n" {
		t.Errorf("blob = %q, err = %v", content, err)
	}
	if _, err := f.client.Blob(context.Background(), project, "0000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing blob err = %v", err)
	}

	file, err := f.client.File(context.Background(), project, "master", "docs/a.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(file.Content) != "# Title\n" || file.BlobID != entries[0].ID || file.LastCommitID == "" || file.Path != "docs/a.md" || file.Ref != "master" {
		t.Errorf("file = %+v", file)
	}
	if _, err := f.client.File(context.Background(), project, "master", "docs/none.md"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing file err = %v", err)
	}
	if _, err := f.client.File(context.Background(), project, "", "docs/a.md"); err == nil {
		t.Error("empty ref must be rejected before any request")
	}
}

func TestFileRejectsUnknownEncoding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"encoding":"text","content":"x"}`)
	}))
	defer server.Close()
	client, _ := New(Options{BaseURL: server.URL, Token: "t"})
	if _, err := client.File(context.Background(), project, "master", "a.md"); err == nil || !strings.Contains(err.Error(), "encoding") {
		t.Errorf("err = %v", err)
	}
}

func TestFileRejectsBrokenBase64(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"encoding":"base64","content":"***"}`)
	}))
	defer server.Close()
	client, _ := New(Options{BaseURL: server.URL, Token: "t"})
	if _, err := client.File(context.Background(), project, "master", "a.md"); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Errorf("err = %v", err)
	}
}

func TestRetriesRateLimitHonouringRetryAfter(t *testing.T) {
	f := newFixture(t)
	f.fake.FailNext(2, http.StatusTooManyRequests)

	if _, err := f.client.CurrentUser(context.Background()); err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(f.sleeps) != 2 || f.sleeps[0] != time.Second || f.sleeps[1] != time.Second {
		t.Errorf("sleeps = %v, want Retry-After of 1s twice", f.sleeps)
	}
	if got := f.fake.RequestCount(); got != 3 {
		t.Errorf("requests = %d", got)
	}
}

func TestRetriesUseExponentialBackoffOnServerErrors(t *testing.T) {
	f := newFixture(t)
	f.fake.FailNext(3, http.StatusBadGateway)

	if _, err := f.client.CurrentUser(context.Background()); err != nil {
		t.Fatalf("err = %v", err)
	}
	want := []time.Duration{defaultRetryDelay, defaultRetryDelay * 2, defaultRetryDelay * 4}
	if fmt.Sprint(f.sleeps) != fmt.Sprint(want) {
		t.Errorf("sleeps = %v, want %v", f.sleeps, want)
	}
}

func TestRetryAfterIsCapped(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = fmt.Fprint(w, `{"id":1,"username":"bot"}`)
	}))
	defer server.Close()
	f := &fixture{}
	client := f.newClient(t, server.URL, "t")

	if _, err := client.CurrentUser(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.sleeps) != 1 || f.sleeps[0] != maxRetryAfter {
		t.Errorf("sleeps = %v", f.sleeps)
	}
}

func TestRetriesGiveUpAndReturnTheLastAPIError(t *testing.T) {
	f := newFixture(t)
	f.fake.FailNext(10, http.StatusServiceUnavailable)

	_, err := f.client.CurrentUser(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("err = %v", err)
	}
	if got := f.fake.RequestCount(); got != 1+defaultMaxRetries {
		t.Errorf("requests = %d, want %d", got, 1+defaultMaxRetries)
	}
}

func TestClientErrorsAreNotRetried(t *testing.T) {
	f := newFixture(t)
	if _, err := f.client.Project(context.Background(), "ghost/project"); err == nil {
		t.Fatal("expected error")
	}
	if got := f.fake.RequestCount(); got != 1 {
		t.Errorf("requests = %d, 404 must not be retried", got)
	}
}

func TestNetworkErrorsAreRetriedThenReported(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close()
	f := &fixture{}
	client := f.newClient(t, url, "t")

	_, err := client.CurrentUser(context.Background())
	if err == nil || !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("err = %v", err)
	}
	if len(f.sleeps) != defaultMaxRetries {
		t.Errorf("sleeps = %d, want %d", len(f.sleeps), defaultMaxRetries)
	}
}

func TestCancelledContextStopsImmediately(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := f.client.CurrentUser(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}

func TestSleepFailureAbortsRetries(t *testing.T) {
	f := newFixture(t)
	f.fake.FailNext(5, http.StatusBadGateway)
	boom := errors.New("sleep interrupted")
	client, _ := New(Options{BaseURL: f.server.URL, Token: token, Sleep: func(context.Context, time.Duration) error { return boom }})

	if _, err := client.CurrentUser(context.Background()); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
}

func TestSleepContextHonoursCancellation(t *testing.T) {
	if err := sleepContext(context.Background(), time.Millisecond); err != nil {
		t.Errorf("err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}

func TestOversizedResponseIsRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		chunk := make([]byte, 1<<20)
		for i := 0; i < 33; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	client, _ := New(Options{BaseURL: server.URL, Token: "t", MaxRetries: -1})

	if _, err := client.Blob(context.Background(), project, "abc"); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("err = %v", err)
	}
}

func TestInvalidJSONIsReported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `<html>`)
	}))
	defer server.Close()
	client, _ := New(Options{BaseURL: server.URL, Token: "t"})

	if _, err := client.CurrentUser(context.Background()); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Errorf("err = %v", err)
	}
}

func TestBaseURLWithPathPrefix(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_, _ = fmt.Fprint(w, `{"id":1,"path_with_namespace":"a/b","default_branch":"main"}`)
	}))
	defer server.Close()
	client, _ := New(Options{BaseURL: server.URL + "/gitlab/", Token: "t"})

	if _, err := client.Project(context.Background(), "a/b"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/gitlab/api/v4/projects/a%2Fb" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestRequestsCarryTokenAndUserAgent(t *testing.T) {
	var header http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Clone()
		_, _ = fmt.Fprint(w, `{"id":1,"username":"bot"}`)
	}))
	defer server.Close()
	client, _ := New(Options{BaseURL: server.URL, Token: "abc"})

	if _, err := client.CurrentUser(context.Background()); err != nil {
		t.Fatal(err)
	}
	if header.Get("PRIVATE-TOKEN") != "abc" || header.Get("User-Agent") != userAgent || header.Get("Accept") != "application/json" {
		t.Errorf("headers = %v", header)
	}
}
