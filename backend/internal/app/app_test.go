package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"vibefluence/internal/config"
	"vibefluence/internal/gitlab"
	"vibefluence/internal/testing/fakegitlab"
)

const (
	token   = "t"
	project = "group/docs"
)

type stack struct {
	fake *fakegitlab.Server
	web  *httptest.Server
}

func newStack(t *testing.T, mutate func(*config.Config)) *stack {
	t.Helper()
	fake := fakegitlab.New(token)
	fake.AddProject(project, "master", fakegitlab.ProjectOptions{})
	gitlabServer := httptest.NewServer(fake)
	t.Cleanup(gitlabServer.Close)

	autoMerge := true
	cfg := &config.Config{
		Server: config.Server{DataDir: t.TempDir()},
		GitLab: config.GitLab{BaseURL: gitlabServer.URL, Token: token, WebhookSecret: "hook"},
		Repositories: []config.Repository{
			{ID: "docs", Name: "Docs", Project: project, Branch: "master", Paths: []string{"docs"}, AutoMerge: &autoMerge},
		},
	}
	if mutate != nil {
		mutate(cfg)
	}
	application, err := New(cfg, slog.New(slog.DiscardHandler), WithPublishTiming(time.Millisecond, time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	assets := fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}}
	web := httptest.NewServer(application.Handler(assets))
	t.Cleanup(web.Close)
	return &stack{fake: fake, web: web}
}

func (s *stack) do(t *testing.T, method, path string, headers ...string) (int, http.Header, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, s.web.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Set(headers[i], headers[i+1])
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, response.Header, body
}

func (s *stack) json(t *testing.T, method, path string, headers ...string) (int, map[string]any) {
	t.Helper()
	status, _, body := s.do(t, method, path, headers...)
	var decoded map[string]any
	_ = json.Unmarshal(body, &decoded)
	return status, decoded
}

func (s *stack) seed() {
	s.fake.PutFile(project, "master", "docs/README.md", "---\ntitle: Welcome\n---\n# Welcome\n\nSee [guide](guide/setup.md) and ![pic](img/pic.png).\n")
	s.fake.PutFile(project, "master", "docs/guide/setup.md", "# Setup\n\nInstall deployment tooling.\n")
	s.fake.PutFile(project, "master", "docs/guide/hidden.md", "---\nstatus: draft\n---\nsecret deployment plan\n")
	s.fake.PutFile(project, "master", "docs/img/pic.png", "PNGDATA")
}

func TestEndToEndReading(t *testing.T) {
	s := newStack(t, nil)
	s.seed()

	status, repos := s.json(t, http.MethodGet, "/api/v1/repos")
	repo := repos["repos"].([]any)[0].(map[string]any)
	if status != 200 || repo["docCount"] != float64(2) || repo["hiddenCount"] != float64(1) || repo["available"] != true || repo["branch"] != "master" {
		t.Fatalf("repos = %v", repos)
	}

	status, tree := s.json(t, http.MethodGet, "/api/v1/repos/docs/tree")
	root := tree["roots"].([]any)[0].(map[string]any)
	if status != 200 || len(root["children"].([]any)) != 2 {
		t.Fatalf("tree = %v", tree)
	}

	status, doc := s.json(t, http.MethodGet, "/api/v1/repos/docs/doc?path=docs/README.md")
	html := doc["html"].(string)
	if status != 200 || doc["title"] != "Welcome" || !strings.Contains(html, `href="/r/docs/docs/guide/setup.md"`) || !strings.Contains(html, "/api/v1/repos/docs/raw?path=docs%2Fimg%2Fpic.png") {
		t.Fatalf("doc = %v", doc)
	}

	status, hidden := s.json(t, http.MethodGet, "/api/v1/repos/docs/doc?path=docs/guide/hidden.md")
	if status != 404 || hidden["error"] != "not_found" {
		t.Errorf("a draft must be invisible: %d %v", status, hidden)
	}

	status, search := s.json(t, http.MethodGet, "/api/v1/search?q=deployment")
	results := search["results"].([]any)
	if status != 200 || len(results) != 1 || results[0].(map[string]any)["path"] != "docs/guide/setup.md" {
		t.Errorf("search must not reveal drafts: %d %v", status, search)
	}
}

func TestEndToEndImages(t *testing.T) {
	s := newStack(t, nil)
	s.seed()

	status, header, body := s.do(t, http.MethodGet, "/api/v1/repos/docs/raw?path=docs/img/pic.png")
	if status != 200 || string(body) != "PNGDATA" || header.Get("Content-Type") != "image/png" || header.Get("ETag") == "" {
		t.Fatalf("status %d header %v body %q", status, header, body)
	}
	status, _, _ = s.do(t, http.MethodGet, "/api/v1/repos/docs/raw?path=docs/img/pic.png", "If-None-Match", header.Get("ETag"))
	if status != http.StatusNotModified {
		t.Errorf("revalidation status = %d", status)
	}
	if status, _, _ := s.do(t, http.MethodGet, "/api/v1/repos/docs/raw?path=docs/README.md"); status != 400 {
		t.Errorf("non-image status = %d", status)
	}
	if status, _, _ := s.do(t, http.MethodGet, "/api/v1/repos/docs/raw?path=docs/img/none.png"); status != 404 {
		t.Errorf("missing image status = %d", status)
	}
}

func TestEndToEndWebhookInvalidatesCache(t *testing.T) {
	s := newStack(t, nil)
	s.seed()
	_, _ = s.json(t, http.MethodGet, "/api/v1/repos/docs/tree")

	s.fake.PutFile(project, "master", "docs/new.md", "# New")
	_, tree := s.json(t, http.MethodGet, "/api/v1/repos/docs/tree")
	if got := len(tree["roots"].([]any)[0].(map[string]any)["children"].([]any)); got != 2 {
		t.Fatalf("the snapshot should still be cached, children = %d", got)
	}

	event := `{"object_kind":"push","ref":"refs/heads/master","project":{"path_with_namespace":"group/docs"}}`
	request, _ := http.NewRequest(http.MethodPost, s.web.URL+"/api/v1/webhooks/gitlab", strings.NewReader(event))
	request.Header.Set("X-Gitlab-Token", "hook")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("webhook status = %d", response.StatusCode)
	}

	_, tree = s.json(t, http.MethodGet, "/api/v1/repos/docs/tree")
	if got := len(tree["roots"].([]any)[0].(map[string]any)["children"].([]any)); got != 3 {
		t.Errorf("children after the webhook = %d", got)
	}
}

func TestEndToEndManualRefresh(t *testing.T) {
	s := newStack(t, nil)
	s.seed()
	_, _ = s.json(t, http.MethodGet, "/api/v1/repos/docs/tree")
	s.fake.PutFile(project, "master", "docs/late.md", "# Late")

	status, repo := s.json(t, http.MethodPost, "/api/v1/repos/docs/refresh")
	if status != 200 || repo["docCount"] != float64(3) {
		t.Errorf("status %d repo %v", status, repo)
	}
}

func TestEndToEndUnavailableGitLab(t *testing.T) {
	s := newStack(t, nil)
	s.fake.FailNext(100, http.StatusInternalServerError)

	status, body := s.json(t, http.MethodGet, "/api/v1/repos/docs/tree")
	if status != http.StatusBadGateway || body["error"] != "upstream_unavailable" {
		t.Errorf("status %d body %v", status, body)
	}
	status, repos := s.json(t, http.MethodGet, "/api/v1/repos")
	repo := repos["repos"].([]any)[0].(map[string]any)
	if status != 200 || repo["available"] != false {
		t.Errorf("an unreachable repository must be listed as unavailable: %d %v", status, repos)
	}
}

func TestEndToEndReadiness(t *testing.T) {
	s := newStack(t, nil)
	if status, body := s.json(t, http.MethodGet, "/readyz"); status != 200 || body["status"] != "ok" {
		t.Errorf("ready: %d %v", status, body)
	}

	broken := newStack(t, nil)
	broken.fake.FailNext(1, http.StatusInternalServerError)
	status, body := broken.json(t, http.MethodGet, "/readyz")
	if status != http.StatusServiceUnavailable || body["status"] != "unavailable" {
		t.Errorf("not ready: %d %v", status, body)
	}
	if checks := body["checks"].(map[string]any); checks["gitlab"] != "failed" {
		t.Errorf("checks = %v", checks)
	}
}

func TestSPAFallbackStillWorksAlongsideTheAPI(t *testing.T) {
	s := newStack(t, nil)
	status, header, body := s.do(t, http.MethodGet, "/r/docs/docs/guide/setup.md")
	if status != 200 || !strings.Contains(string(body), "app") || !strings.HasPrefix(header.Get("Content-Type"), "text/html") {
		t.Errorf("status %d header %v", status, header)
	}
	if status, _, _ := s.do(t, http.MethodGet, "/api/v1/unknown"); status != 404 {
		t.Errorf("unknown API path status = %d", status)
	}
}

func TestWithoutRepositories(t *testing.T) {
	s := newStack(t, func(cfg *config.Config) { cfg.Repositories = nil })

	status, repos := s.json(t, http.MethodGet, "/api/v1/repos")
	if status != 200 || len(repos["repos"].([]any)) != 0 {
		t.Errorf("repos = %d %v", status, repos)
	}
	if status, _ := s.json(t, http.MethodGet, "/api/v1/repos/docs/tree"); status != 404 {
		t.Errorf("tree status = %d", status)
	}
	if status, body := s.json(t, http.MethodGet, "/readyz"); status != 200 || body["status"] != "ok" {
		t.Errorf("readyz: %d %v", status, body)
	}
}

func TestNewRejectsAnInvalidGitLabURL(t *testing.T) {
	_, err := New(&config.Config{
		Server:       config.Server{DataDir: t.TempDir()},
		GitLab:       config.GitLab{BaseURL: "not a url", Token: "t"},
		Repositories: []config.Repository{{ID: "a", Project: "g/a", Paths: []string{"docs"}}},
	}, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Error("expected error")
	}
}

func TestUnavailableSourceReportsMissingConfiguration(t *testing.T) {
	var source unavailableSource
	ctx := context.Background()
	checks := []error{
		func() error { _, err := source.Project(ctx, "p"); return err }(),
		func() error { _, err := source.Tree(ctx, "p", "r", "d"); return err }(),
		func() error { _, err := source.Blob(ctx, "p", "b"); return err }(),
		func() error { _, err := source.Branch(ctx, "p", "b"); return err }(),
		func() error { _, err := source.File(ctx, "p", "r", "f"); return err }(),
	}
	for i, err := range checks {
		if !errors.Is(err, errNoGitLab) {
			t.Errorf("method %d: err = %v", i, err)
		}
	}
}

func TestCachedCheck(t *testing.T) {
	now := time.Unix(1000, 0)
	calls := 0
	var result error
	check := cachedCheck(func(context.Context) (gitlab.User, error) {
		calls++
		return gitlab.User{}, result
	}, func() time.Time { return now })

	if err := check(context.Background()); err != nil || calls != 1 {
		t.Fatalf("first: err %v calls %d", err, calls)
	}
	now = now.Add(29 * time.Second)
	_ = check(context.Background())
	if calls != 1 {
		t.Errorf("a recent success must be reused: calls = %d", calls)
	}
	now = now.Add(2 * time.Second)
	result = errors.New("down")
	if err := check(context.Background()); err == nil || calls != 2 {
		t.Fatalf("after the TTL: err %v calls %d", err, calls)
	}
	now = now.Add(4 * time.Second)
	if err := check(context.Background()); err == nil || calls != 2 {
		t.Errorf("a recent failure must be reused briefly: err %v calls %d", err, calls)
	}
	now = now.Add(2 * time.Second)
	result = nil
	if err := check(context.Background()); err != nil || calls != 3 {
		t.Errorf("recovery: err %v calls %d", err, calls)
	}
}

func TestNewRequiresAWritableDataDirectory(t *testing.T) {
	if _, err := New(&config.Config{}, slog.New(slog.DiscardHandler)); err == nil {
		t.Error("an empty data directory must be rejected")
	}
	blocker := t.TempDir() + "/file"
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(&config.Config{Server: config.Server{DataDir: blocker}}, slog.New(slog.DiscardHandler)); err == nil {
		t.Error("a data directory that is a file must be rejected")
	}
}

func TestBotUserProbeWarnsOnceWhenTheTokenUserDiffers(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	user := gitlab.User{Username: "someone-else"}
	probe := botUserProbe(func(context.Context) (gitlab.User, error) { return user, nil }, "vibefluence-bot", logger)
	for range 3 {
		if got, err := probe(context.Background()); err != nil || got != user {
			t.Fatalf("probe = %v, %v", got, err)
		}
	}
	if count := strings.Count(logs.String(), "different user"); count != 1 {
		t.Errorf("warnings = %d, want 1\n%s", count, logs.String())
	}
	if !strings.Contains(logs.String(), "token_user=someone-else") {
		t.Errorf("log lacks the token user: %s", logs.String())
	}
}

func TestBotUserProbeStaysQuietForTheBotAndForErrors(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	matching := botUserProbe(func(context.Context) (gitlab.User, error) {
		return gitlab.User{Username: "vibefluence-bot"}, nil
	}, "vibefluence-bot", logger)
	if _, err := matching(context.Background()); err != nil {
		t.Fatal(err)
	}
	failing := botUserProbe(func(context.Context) (gitlab.User, error) {
		return gitlab.User{}, errors.New("down")
	}, "vibefluence-bot", logger)
	if _, err := failing(context.Background()); err == nil {
		t.Fatal("expected the probe error to pass through")
	}
	if logs.Len() != 0 {
		t.Errorf("unexpected logs: %s", logs.String())
	}
}
