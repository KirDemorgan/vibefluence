package app

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"vibefluence/internal/config"
	"vibefluence/internal/testing/fakegitlab"
)

func (s *stack) send(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, s.web.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, _ := io.ReadAll(response.Body)
	var decoded map[string]any
	_ = json.Unmarshal(raw, &decoded)
	return response.StatusCode, decoded
}

func (s *stack) saveDraft(t *testing.T, path, content, baseBlob string) map[string]any {
	t.Helper()
	status, draft := s.send(t, http.MethodPut, "/api/v1/repos/docs/drafts", map[string]any{
		"path": path, "content": content, "baseBlobId": baseBlob, "author": "Alex",
	})
	if status != http.StatusOK {
		t.Fatalf("saving the draft: %d %v", status, draft)
	}
	return draft
}

func (s *stack) publish(t *testing.T, draftID string, body map[string]any) (int, map[string]any) {
	t.Helper()
	return s.send(t, http.MethodPost, "/api/v1/repos/docs/drafts/"+draftID+"/publish", body)
}

func TestEndToEndWritingANewDocument(t *testing.T) {
	s := newStack(t, nil)
	s.seed()

	content := "---\ntitle: Rollout plan\ntype: plan\nstatus: published\n---\n# Rollout plan\n\nSee [the guide](guide/setup.md).\n"
	draft := s.saveDraft(t, "docs/plans/rollout.md", content, "")
	if draft["isNew"] != true || draft["title"] != "Rollout plan" || draft["status"] != "published" {
		t.Fatalf("draft = %v", draft)
	}
	id := draft["id"].(string)

	status, listed := s.json(t, http.MethodGet, "/api/v1/repos/docs/drafts")
	if status != 200 || len(listed["drafts"].([]any)) != 1 {
		t.Fatalf("drafts = %d %v", status, listed)
	}
	if _, all := s.json(t, http.MethodGet, "/api/v1/drafts"); len(all["drafts"].([]any)) != 1 {
		t.Errorf("all drafts = %v", all)
	}
	if _, doc := s.json(t, http.MethodGet, "/api/v1/repos/docs/doc?path=docs/plans/rollout.md"); doc["error"] != "not_found" {
		t.Errorf("a draft must not be published yet: %v", doc)
	}

	status, preview := s.send(t, http.MethodPost, "/api/v1/repos/docs/preview", map[string]any{"path": "docs/plans/rollout.md", "content": content})
	if status != 200 || !strings.Contains(preview["html"].(string), `href="/r/docs/docs/plans/guide/setup.md"`) {
		t.Errorf("preview = %d %v", status, preview)
	}

	status, outcome := s.publish(t, id, nil)
	if status != 200 || outcome["outcome"] != "merged" || outcome["created"] != true || outcome["draftRemoved"] != true {
		t.Fatalf("publish = %d %v", status, outcome)
	}

	status, doc := s.json(t, http.MethodGet, "/api/v1/repos/docs/doc?path=docs/plans/rollout.md")
	if status != 200 || doc["title"] != "Rollout plan" || doc["status"] != "published" {
		t.Fatalf("the merged document must be visible immediately: %d %v", status, doc)
	}
	if raw, ok := s.fake.File(project, "master", "docs/plans/rollout.md"); !ok || !strings.Contains(raw, "status: published") {
		t.Errorf("file on master = %q", raw)
	}
	if strings.Join(s.fake.Branches(project), ",") != "master" {
		t.Errorf("branches = %v", s.fake.Branches(project))
	}
	if _, after := s.json(t, http.MethodGet, "/api/v1/repos/docs/drafts"); len(after["drafts"].([]any)) != 0 {
		t.Errorf("the published draft must be gone: %v", after)
	}
}

func TestEndToEndPublishingADraftMarkedAsDraftStillPublishesIt(t *testing.T) {
	s := newStack(t, nil)
	draft := s.saveDraft(t, "docs/wip.md", "---\ntitle: WIP\nstatus: draft\n---\ntext\n", "")
	if draft["status"] != "draft" {
		t.Fatalf("draft = %v", draft)
	}
	status, outcome := s.publish(t, draft["id"].(string), nil)
	if status != 200 || outcome["outcome"] != "merged" {
		t.Fatalf("publish = %d %v", status, outcome)
	}
	if raw, _ := s.fake.File(project, "master", "docs/wip.md"); strings.Contains(raw, "status: draft") {
		t.Errorf("an explicit publish must publish: %q", raw)
	}
}

func TestEndToEndEditingAnExistingDocument(t *testing.T) {
	s := newStack(t, nil)
	s.seed()
	_, doc := s.json(t, http.MethodGet, "/api/v1/repos/docs/doc?path=docs/guide/setup.md")
	blob := doc["blobId"].(string)

	draft := s.saveDraft(t, "docs/guide/setup.md", "# Setup v2\n\nMuch better.\n", blob)
	if draft["isNew"] != false {
		t.Fatalf("draft = %v", draft)
	}
	status, outcome := s.publish(t, draft["id"].(string), nil)
	if status != 200 || outcome["outcome"] != "merged" || outcome["created"] != false {
		t.Fatalf("publish = %d %v", status, outcome)
	}
	_, updated := s.json(t, http.MethodGet, "/api/v1/repos/docs/doc?path=docs/guide/setup.md")
	if !strings.Contains(updated["html"].(string), "Much better") || updated["blobId"] == blob {
		t.Errorf("updated = %v", updated)
	}
}

func TestEndToEndConflictsCanBeResolvedByForcing(t *testing.T) {
	s := newStack(t, nil)
	s.seed()
	_, doc := s.json(t, http.MethodGet, "/api/v1/repos/docs/doc?path=docs/guide/setup.md")
	draft := s.saveDraft(t, "docs/guide/setup.md", "# Mine\n", doc["blobId"].(string))
	s.fake.PutFile(project, "master", "docs/guide/setup.md", "# Someone else changed it\n")

	status, conflict := s.publish(t, draft["id"].(string), nil)
	if status != http.StatusConflict || conflict["error"] != "conflict" || conflict["exists"] != true || conflict["currentBlobId"] == "" {
		t.Fatalf("conflict = %d %v", status, conflict)
	}
	if raw, _ := s.fake.File(project, "master", "docs/guide/setup.md"); !strings.Contains(raw, "Someone else") {
		t.Error("a conflict must not touch the repository")
	}

	status, forced := s.publish(t, draft["id"].(string), map[string]any{"force": true})
	if status != 200 || forced["outcome"] != "merged" {
		t.Fatalf("forced = %d %v", status, forced)
	}
	if raw, _ := s.fake.File(project, "master", "docs/guide/setup.md"); !strings.Contains(raw, "# Mine") {
		t.Errorf("file = %q", raw)
	}
}

func TestEndToEndWithoutAutoMergeTheMergeRequestWaits(t *testing.T) {
	s := newStack(t, func(cfg *config.Config) {
		manual := false
		cfg.Repositories[0].AutoMerge = &manual
	})
	draft := s.saveDraft(t, "docs/a.md", "# A\n", "")

	status, outcome := s.publish(t, draft["id"].(string), nil)
	if status != 200 || outcome["outcome"] != "pending" || outcome["reason"] != "auto_merge_disabled" || outcome["mergeRequestUrl"] == "" || outcome["draftRemoved"] != false {
		t.Fatalf("outcome = %d %v", status, outcome)
	}
	if _, ok := s.fake.File(project, "master", "docs/a.md"); ok {
		t.Error("nothing may be merged")
	}
	status, kept := s.json(t, http.MethodGet, "/api/v1/repos/docs/drafts/"+draft["id"].(string))
	if status != 200 || kept["mergeRequestUrl"] == "" || kept["content"] != "# A\n" {
		t.Errorf("the draft must keep the merge request link: %d %v", status, kept)
	}
}

func TestEndToEndAPipelineOnTheRepositoryBlocksTheMerge(t *testing.T) {
	s := newStack(t, nil)
	s.fake.SetProjectOptions(project, fakegitlab.ProjectOptions{PipelineBranches: func(string) bool { return true }})
	draft := s.saveDraft(t, "docs/a.md", "# A\n", "")

	status, outcome := s.publish(t, draft["id"].(string), nil)
	if status != 200 || outcome["outcome"] != "pending" || outcome["reason"] != "pipeline_started" {
		t.Fatalf("outcome = %d %v", status, outcome)
	}
	if _, ok := s.fake.File(project, "master", "docs/a.md"); ok {
		t.Error("a repository that starts pipelines for our branches must not be merged into automatically")
	}
}

func TestEndToEndValidationAndSafety(t *testing.T) {
	s := newStack(t, nil)

	for _, path := range []string{"src/a.md", "docs/a.txt", "docs/../a.md", ""} {
		status, body := s.send(t, http.MethodPut, "/api/v1/repos/docs/drafts", map[string]any{"path": path, "content": "x"})
		if status != http.StatusBadRequest || body["error"] != "invalid_request" {
			t.Errorf("%q: %d %v", path, status, body)
		}
	}
	if status, _ := s.send(t, http.MethodPut, "/api/v1/repos/nope/drafts", map[string]any{"path": "docs/a.md", "content": "x"}); status != http.StatusNotFound {
		t.Errorf("unknown repository: %d", status)
	}
	if status, _ := s.json(t, http.MethodGet, "/api/v1/repos/docs/drafts/..%2F..%2Fetc"); status != http.StatusBadRequest && status != http.StatusNotFound {
		t.Errorf("a traversal attempt: %d", status)
	}

	request, _ := http.NewRequest(http.MethodPut, s.web.URL+"/api/v1/repos/docs/drafts", strings.NewReader(`{"path":"docs/a.md","content":"x"}`))
	request.Header.Set("Content-Type", "text/plain")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("a cross-site form post must be refused, got %d", response.StatusCode)
	}
}

func TestEndToEndPublishingIdenticalContentIsRejected(t *testing.T) {
	s := newStack(t, nil)
	s.seed()
	_, doc := s.json(t, http.MethodGet, "/api/v1/repos/docs/doc?path=docs/guide/setup.md")
	draft := s.saveDraft(t, "docs/guide/setup.md", doc["raw"].(string), doc["blobId"].(string))

	status, body := s.publish(t, draft["id"].(string), nil)
	if status != http.StatusBadRequest || !strings.Contains(body["message"].(string), "nothing to publish") {
		t.Errorf("status %d body %v", status, body)
	}
}

func TestEndToEndDraftsSurviveARestart(t *testing.T) {
	fake := fakegitlab.New(token)
	fake.AddProject(project, "master", fakegitlab.ProjectOptions{})
	gitlabServer := httptest.NewServer(fake)
	defer gitlabServer.Close()
	dataDir := t.TempDir()
	cfg := &config.Config{
		Server:       config.Server{DataDir: dataDir},
		GitLab:       config.GitLab{BaseURL: gitlabServer.URL, Token: token},
		Repositories: []config.Repository{{ID: "docs", Name: "Docs", Project: project, Branch: "master", Paths: []string{"docs"}}},
	}

	boot := func() *stack {
		application, err := New(cfg, slog.New(slog.DiscardHandler), WithPublishTiming(time.Millisecond, time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		web := httptest.NewServer(application.Handler(fstest.MapFS{"index.html": {Data: []byte("x")}}))
		t.Cleanup(web.Close)
		return &stack{fake: fake, web: web}
	}

	first := boot()
	first.saveDraft(t, "docs/persisted.md", "# Persisted\n", "")
	second := boot()
	_, listed := second.json(t, http.MethodGet, "/api/v1/repos/docs/drafts")
	drafts := listed["drafts"].([]any)
	if len(drafts) != 1 || drafts[0].(map[string]any)["path"] != "docs/persisted.md" {
		t.Errorf("drafts after a restart = %v", listed)
	}
}
