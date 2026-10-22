package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vibefluence/internal/gitlab"
	"vibefluence/internal/index"
	"vibefluence/internal/markdown"
	"vibefluence/internal/portal"
)

type stub struct {
	repos    []portal.RepoSummary
	tree     portal.TreeView
	doc      portal.DocumentView
	results  []index.SearchResult
	summary  portal.RepoSummary
	image    portal.Image
	notMod   bool
	err      error
	lastArgs []string
	lastLim  int
}

func (s *stub) Repos(context.Context) []portal.RepoSummary { return s.repos }
func (s *stub) Tree(_ context.Context, repo string) (portal.TreeView, error) {
	s.lastArgs = []string{repo}
	return s.tree, s.err
}
func (s *stub) Document(_ context.Context, repo, path string) (portal.DocumentView, error) {
	s.lastArgs = []string{repo, path}
	return s.doc, s.err
}
func (s *stub) Search(_ context.Context, repo, query string, limit int) ([]index.SearchResult, error) {
	s.lastArgs = []string{repo, query}
	s.lastLim = limit
	return s.results, s.err
}
func (s *stub) Refresh(_ context.Context, repo string) (portal.RepoSummary, error) {
	s.lastArgs = []string{repo}
	return s.summary, s.err
}
func (s *stub) Image(_ context.Context, repo, path, etag string) (portal.Image, bool, error) {
	s.lastArgs = []string{repo, path, etag}
	return s.image, s.notMod, s.err
}

func serve(t *testing.T, s *stub, mutate ...func(*Options)) http.Handler {
	t.Helper()
	opts := Options{Service: s}
	for _, m := range mutate {
		m(&opts)
	}
	return New(opts)
}

func call(handler http.Handler, method, target, body string, headers ...string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Set(headers[i], headers[i+1])
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decode(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
		t.Fatalf("body is not JSON: %v: %s", err, recorder.Body.String())
	}
	return out
}

func TestListRepos(t *testing.T) {
	s := &stub{repos: []portal.RepoSummary{
		{ID: "a", Name: "A", Project: "g/a", Branch: "main", Paths: []string{"docs"}, AutoMerge: true, DocCount: 3, Hidden: 1, Available: true, WebURL: "https://gl/g/a"},
		{ID: "b", Name: "B", Project: "g/b"},
	}}
	recorder := call(serve(t, s), http.MethodGet, "/api/v1/repos", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	repos := decode(t, recorder)["repos"].([]any)
	first := repos[0].(map[string]any)
	if first["id"] != "a" || first["docCount"] != float64(3) || first["hiddenCount"] != float64(1) || first["autoMerge"] != true || first["available"] != true || first["webUrl"] != "https://gl/g/a" {
		t.Errorf("first = %v", first)
	}
	if second := repos[1].(map[string]any); second["available"] != false {
		t.Errorf("second = %v", second)
	}
}

func TestListReposEmptyIsAnArray(t *testing.T) {
	recorder := call(serve(t, &stub{}), http.MethodGet, "/api/v1/repos", "")
	if !strings.Contains(recorder.Body.String(), `"repos":[]`) {
		t.Errorf("body = %s", recorder.Body.String())
	}
}

func TestTree(t *testing.T) {
	s := &stub{tree: portal.TreeView{Repo: "a", Branch: "main", Stale: true, Roots: []*index.Node{{
		Name: "docs", Path: "docs", Kind: index.KindFolder,
		Children: []*index.Node{{Name: "x.md", Path: "docs/x.md", Kind: index.KindDoc, Title: "X", Type: "spec"}},
	}}}}
	recorder := call(serve(t, s), http.MethodGet, "/api/v1/repos/a/tree", "")
	body := decode(t, recorder)
	if recorder.Code != http.StatusOK || body["branch"] != "main" || body["stale"] != true || s.lastArgs[0] != "a" {
		t.Fatalf("body = %v", body)
	}
	root := body["roots"].([]any)[0].(map[string]any)
	child := root["children"].([]any)[0].(map[string]any)
	if root["kind"] != "folder" || child["title"] != "X" || child["type"] != "spec" || child["path"] != "docs/x.md" {
		t.Errorf("root = %v", root)
	}
}

func TestDocument(t *testing.T) {
	s := &stub{doc: portal.DocumentView{
		Repo: "a", Path: "docs/x.md", Title: "X", Status: "published", HTML: "<p>x</p>", Raw: "x", BlobID: "b1", WebURL: "https://gl/x",
		Headings: []markdown.Heading{{Level: 2, Text: "Sub", ID: "h-sub"}},
	}}
	recorder := call(serve(t, s), http.MethodGet, "/api/v1/repos/a/doc?path=docs/x.md", "")
	body := decode(t, recorder)
	if recorder.Code != http.StatusOK || s.lastArgs[0] != "a" || s.lastArgs[1] != "docs/x.md" {
		t.Fatalf("status %d args %v", recorder.Code, s.lastArgs)
	}
	if body["html"] != "<p>x</p>" || body["blobId"] != "b1" || body["status"] != "published" || body["raw"] != "x" {
		t.Errorf("body = %v", body)
	}
	if warnings, ok := body["warnings"].([]any); !ok || len(warnings) != 0 {
		t.Errorf("warnings must be an empty array, got %v", body["warnings"])
	}
	if headings := body["headings"].([]any); len(headings) != 1 || headings[0].(map[string]any)["id"] != "h-sub" {
		t.Errorf("headings = %v", body["headings"])
	}
}

func TestDocumentRequiresPath(t *testing.T) {
	for _, target := range []string{"/api/v1/repos/a/doc", "/api/v1/repos/a/doc?path=", "/api/v1/repos/a/raw", "/api/v1/repos/a/doc?path=" + strings.Repeat("x", maxPathLength+1)} {
		recorder := call(serve(t, &stub{}), http.MethodGet, target, "")
		if recorder.Code != http.StatusBadRequest || decode(t, recorder)["error"] != "invalid_request" {
			t.Errorf("%s: status %d body %s", target, recorder.Code, recorder.Body.String())
		}
	}
}

func TestRawImage(t *testing.T) {
	s := &stub{image: portal.Image{Content: []byte("PNGDATA"), ContentType: "image/png", ETag: `"abc"`}}
	handler := serve(t, s)

	recorder := call(handler, http.MethodGet, "/api/v1/repos/a/raw?path=docs/p.png", "", "If-None-Match", `"zzz"`)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "PNGDATA" {
		t.Fatalf("status %d body %q", recorder.Code, recorder.Body.String())
	}
	header := recorder.Header()
	if header.Get("Content-Type") != "image/png" || header.Get("ETag") != `"abc"` || header.Get("Content-Length") != "7" {
		t.Errorf("headers = %v", header)
	}
	if !strings.Contains(header.Get("Cache-Control"), "max-age=300") || !strings.Contains(header.Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("headers = %v", header)
	}
	if s.lastArgs[2] != `"zzz"` {
		t.Errorf("If-None-Match was not forwarded: %v", s.lastArgs)
	}

	head := call(handler, http.MethodHead, "/api/v1/repos/a/raw?path=docs/p.png", "")
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Errorf("HEAD status %d body %q", head.Code, head.Body.String())
	}

	s.notMod = true
	notModified := call(handler, http.MethodGet, "/api/v1/repos/a/raw?path=docs/p.png", "")
	if notModified.Code != http.StatusNotModified || notModified.Body.Len() != 0 {
		t.Errorf("status %d body %q", notModified.Code, notModified.Body.String())
	}
}

func TestRefresh(t *testing.T) {
	s := &stub{summary: portal.RepoSummary{ID: "a", DocCount: 7, Available: true}}
	recorder := call(serve(t, s), http.MethodPost, "/api/v1/repos/a/refresh", "")
	if recorder.Code != http.StatusOK || decode(t, recorder)["docCount"] != float64(7) {
		t.Errorf("status %d body %s", recorder.Code, recorder.Body.String())
	}
	if get := call(serve(t, s), http.MethodGet, "/api/v1/repos/a/refresh", ""); get.Code != http.StatusNotFound && get.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET refresh status = %d", get.Code)
	}
}

func TestSearch(t *testing.T) {
	s := &stub{results: []index.SearchResult{{Repo: "a", Path: "docs/x.md", Title: "X", Type: "spec", Snippet: "…needle…"}}}
	recorder := call(serve(t, s), http.MethodGet, "/api/v1/search?q=%20needle%20&repo=a&limit=5", "")
	body := decode(t, recorder)
	if recorder.Code != http.StatusOK || body["query"] != "needle" || s.lastArgs[0] != "a" || s.lastArgs[1] != " needle " || s.lastLim != 5 {
		t.Fatalf("body %v args %v limit %d", body, s.lastArgs, s.lastLim)
	}
	item := body["results"].([]any)[0].(map[string]any)
	if item["snippet"] != "…needle…" || item["type"] != "spec" {
		t.Errorf("item = %v", item)
	}

	call(serve(t, s), http.MethodGet, "/api/v1/search?q=x&limit=abc", "")
	if s.lastLim != 0 {
		t.Errorf("an unparsable limit must become 0, got %d", s.lastLim)
	}
	empty := call(serve(t, &stub{}), http.MethodGet, "/api/v1/search?q=x", "")
	if !strings.Contains(empty.Body.String(), `"results":[]`) {
		t.Errorf("empty = %s", empty.Body.String())
	}
}

func TestErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"validation", &portal.ValidationError{Message: "path is bad"}, http.StatusBadRequest, "invalid_request"},
		{"not found", fmt.Errorf("%w: x", portal.ErrNotFound), http.StatusNotFound, "not_found"},
		{"cancelled", context.Canceled, http.StatusServiceUnavailable, "cancelled"},
		{"timeout", context.DeadlineExceeded, http.StatusGatewayTimeout, "upstream_timeout"},
		{"unauthorized", fmt.Errorf("x: %w", &gitlab.APIError{Status: 401}), http.StatusBadGateway, "upstream_unauthorized"},
		{"forbidden", &gitlab.APIError{Status: 403}, http.StatusBadGateway, "upstream_unauthorized"},
		{"gitlab 500", &gitlab.APIError{Status: 500, Message: "boom"}, http.StatusBadGateway, "upstream_unavailable"},
		{"gitlab not found", &gitlab.APIError{Status: 404}, http.StatusBadGateway, "upstream_unavailable"},
		{"unexpected", errors.New("kaboom"), http.StatusInternalServerError, "internal_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := serve(t, &stub{err: tt.err})
			for _, target := range []string{"/api/v1/repos/a/tree", "/api/v1/repos/a/doc?path=x.md", "/api/v1/search?q=x", "/api/v1/repos/a/raw?path=x.png"} {
				recorder := call(handler, http.MethodGet, target, "")
				body := decode(t, recorder)
				if recorder.Code != tt.wantStatus || body["error"] != tt.wantCode {
					t.Errorf("%s: status %d body %v", target, recorder.Code, body)
				}
			}
			if recorder := call(handler, http.MethodPost, "/api/v1/repos/a/refresh", ""); recorder.Code != tt.wantStatus {
				t.Errorf("refresh: status %d", recorder.Code)
			}
		})
	}
}

func TestErrorMessagesNeverLeakUpstreamDetails(t *testing.T) {
	secret := &gitlab.APIError{Method: "GET", Path: "/api/v4/projects/secret%2Fproject", Status: 500, Message: "internal-host.example exploded"}
	recorder := call(serve(t, &stub{err: secret}), http.MethodGet, "/api/v1/repos/a/tree", "")
	if strings.Contains(recorder.Body.String(), "internal-host") || strings.Contains(recorder.Body.String(), "secret") {
		t.Errorf("body leaks upstream details: %s", recorder.Body.String())
	}
	unexpected := call(serve(t, &stub{err: errors.New("db password is hunter2")}), http.MethodGet, "/api/v1/repos/a/tree", "")
	if strings.Contains(unexpected.Body.String(), "hunter2") {
		t.Errorf("body leaks internal details: %s", unexpected.Body.String())
	}
}

func TestUnknownAPIPath(t *testing.T) {
	recorder := call(serve(t, &stub{}), http.MethodGet, "/api/v1/nothing", "")
	if recorder.Code != http.StatusNotFound || decode(t, recorder)["error"] != "not_found" {
		t.Errorf("status %d body %s", recorder.Code, recorder.Body.String())
	}
}

func TestWebhook(t *testing.T) {
	var gotProject, gotBranch string
	calls := 0
	handler := serve(t, &stub{}, func(o *Options) {
		o.WebhookSecret = "s3cret"
		o.Invalidate = func(project, branch string) int {
			gotProject, gotBranch = project, branch
			calls++
			return 1
		}
	})
	push := `{"object_kind":"push","ref":"refs/heads/master","project":{"path_with_namespace":"group/docs"}}`

	if recorder := call(handler, http.MethodPost, "/api/v1/webhooks/gitlab", push, "X-Gitlab-Token", "s3cret"); recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d", recorder.Code)
	}
	if gotProject != "group/docs" || gotBranch != "master" || calls != 1 {
		t.Errorf("invalidate(%q, %q) x%d", gotProject, gotBranch, calls)
	}

	ignored := []string{
		`{"object_kind":"tag_push","ref":"refs/tags/v1","project":{"path_with_namespace":"group/docs"}}`,
		`{"object_kind":"push","ref":"refs/tags/v1","project":{"path_with_namespace":"group/docs"}}`,
		`{"object_kind":"push","ref":"refs/heads/master","project":{}}`,
		`{"object_kind":"merge_request"}`,
	}
	for _, body := range ignored {
		if recorder := call(handler, http.MethodPost, "/api/v1/webhooks/gitlab", body, "X-Gitlab-Token", "s3cret"); recorder.Code != http.StatusNoContent {
			t.Errorf("%s: status %d", body, recorder.Code)
		}
	}
	if calls != 1 {
		t.Errorf("irrelevant events must not invalidate: calls = %d", calls)
	}
}

func TestWebhookRejections(t *testing.T) {
	configured := serve(t, &stub{}, func(o *Options) {
		o.WebhookSecret = "s3cret"
		o.Invalidate = func(string, string) int { return 0 }
	})
	for _, token := range []string{"", "wrong", "s3cret ", "S3CRET"} {
		recorder := call(configured, http.MethodPost, "/api/v1/webhooks/gitlab", `{}`, "X-Gitlab-Token", token)
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("token %q: status = %d", token, recorder.Code)
		}
	}
	if recorder := call(configured, http.MethodPost, "/api/v1/webhooks/gitlab", `not json`, "X-Gitlab-Token", "s3cret"); recorder.Code != http.StatusBadRequest {
		t.Errorf("invalid json: status = %d", recorder.Code)
	}
	huge := `{"x":"` + strings.Repeat("a", maxWebhookBody) + `"}`
	if recorder := call(configured, http.MethodPost, "/api/v1/webhooks/gitlab", huge, "X-Gitlab-Token", "s3cret"); recorder.Code != http.StatusBadRequest {
		t.Errorf("oversized body: status = %d", recorder.Code)
	}

	disabled := serve(t, &stub{})
	if recorder := call(disabled, http.MethodPost, "/api/v1/webhooks/gitlab", `{}`, "X-Gitlab-Token", "anything"); recorder.Code != http.StatusNotFound {
		t.Errorf("disabled webhook: status = %d", recorder.Code)
	}
	noSecret := serve(t, &stub{}, func(o *Options) { o.Invalidate = func(string, string) int { return 0 } })
	if recorder := call(noSecret, http.MethodPost, "/api/v1/webhooks/gitlab", `{}`, "X-Gitlab-Token", ""); recorder.Code != http.StatusNotFound {
		t.Errorf("an empty secret must disable the webhook, not accept empty tokens: status = %d", recorder.Code)
	}
}
