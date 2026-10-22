package fakegitlab

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

const (
	testToken   = "test-token"
	testProject = "group/docs"
)

type harness struct {
	t      *testing.T
	fake   *Server
	server *httptest.Server
}

func newHarness(t *testing.T, opts ProjectOptions) *harness {
	t.Helper()
	fake := New(testToken)
	fake.AddProject(testProject, "master", opts)
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return &harness{t: t, fake: fake, server: server}
}

type response struct {
	status  int
	header  http.Header
	body    []byte
	decoded map[string]any
}

func (h *harness) call(method, path string, payload any) response {
	h.t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			h.t.Fatal(err)
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, h.server.URL+path, body)
	if err != nil {
		h.t.Fatal(err)
	}
	request.Header.Set("PRIVATE-TOKEN", testToken)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return h.do(request)
}

func (h *harness) do(request *http.Request) response {
	h.t.Helper()
	httpResponse, err := http.DefaultClient.Do(request)
	if err != nil {
		h.t.Fatal(err)
	}
	defer func() { _ = httpResponse.Body.Close() }()
	raw, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		h.t.Fatal(err)
	}
	result := response{status: httpResponse.StatusCode, header: httpResponse.Header, body: raw}
	_ = json.Unmarshal(raw, &result.decoded)
	return result
}

func (h *harness) get(path string) response { return h.call(http.MethodGet, path, nil) }
func (h *harness) post(path string, payload any) response {
	return h.call(http.MethodPost, path, payload)
}
func (h *harness) put(path string, payload any) response {
	return h.call(http.MethodPut, path, payload)
}

func projectPath(suffix string) string {
	return "/api/v4/projects/" + url.PathEscape(testProject) + suffix
}

func filePath(name, query string) string {
	return projectPath("/repository/files/"+url.PathEscape(name)) + query
}

func (h *harness) expectStatus(r response, want int) {
	h.t.Helper()
	if r.status != want {
		h.t.Fatalf("status = %d, want %d, body: %s", r.status, want, r.body)
	}
}

func (h *harness) seedDocs() {
	h.fake.PutFile(testProject, "master", "README.md", "# root")
	h.fake.PutFile(testProject, "master", "docs/index.md", "# index")
	h.fake.PutFile(testProject, "master", "docs/guide/start.md", "# start")
	h.fake.PutFile(testProject, "master", "docs/guide/deep/more.md", "# more")
	h.fake.PutFile(testProject, "master", "src/main.go", "package main")
}

func paths(entries any) []string {
	var result []string
	for _, entry := range entries.([]any) {
		result = append(result, entry.(map[string]any)["path"].(string))
	}
	return result
}

func decodeList(t *testing.T, r response) []any {
	t.Helper()
	var list []any
	if err := json.Unmarshal(r.body, &list); err != nil {
		t.Fatalf("body is not a JSON array: %v: %s", err, r.body)
	}
	return list
}

func TestAuthentication(t *testing.T) {
	h := newHarness(t, ProjectOptions{})

	missing, _ := http.NewRequest(http.MethodGet, h.server.URL+"/api/v4/user", nil)
	if r := h.do(missing); r.status != http.StatusUnauthorized {
		t.Errorf("missing token: status = %d", r.status)
	}

	wrong, _ := http.NewRequest(http.MethodGet, h.server.URL+"/api/v4/user", nil)
	wrong.Header.Set("PRIVATE-TOKEN", "nope")
	if r := h.do(wrong); r.status != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d", r.status)
	}

	bearer, _ := http.NewRequest(http.MethodGet, h.server.URL+"/api/v4/user", nil)
	bearer.Header.Set("Authorization", "Bearer "+testToken)
	if r := h.do(bearer); r.status != http.StatusOK {
		t.Errorf("bearer token: status = %d", r.status)
	}
}

func TestEmptyServerTokenRejectsEverything(t *testing.T) {
	server := httptest.NewServer(New(""))
	defer server.Close()

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v4/user", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d", response.StatusCode)
	}
}

func TestUserReportsBot(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.fake.SetBotUsername("docs-bot")

	r := h.get("/api/v4/user")
	h.expectStatus(r, http.StatusOK)
	if r.decoded["username"] != "docs-bot" {
		t.Errorf("body = %s", r.body)
	}
}

func TestProjectLookup(t *testing.T) {
	h := newHarness(t, ProjectOptions{})

	byPath := h.get(projectPath(""))
	h.expectStatus(byPath, http.StatusOK)
	if byPath.decoded["default_branch"] != "master" || byPath.decoded["path_with_namespace"] != testProject {
		t.Errorf("body = %s", byPath.body)
	}

	byID := h.get("/api/v4/projects/1")
	h.expectStatus(byID, http.StatusOK)

	h.expectStatus(h.get("/api/v4/projects/99"), http.StatusNotFound)
	h.expectStatus(h.get("/api/v4/projects/"+url.PathEscape("other/project")), http.StatusNotFound)
}

func TestTreeNonRecursive(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.seedDocs()

	r := h.get(projectPath("/repository/tree?path=docs"))
	h.expectStatus(r, http.StatusOK)
	got := strings.Join(paths(decodeAny(t, r)), ",")
	if got != "docs/guide,docs/index.md" {
		t.Errorf("paths = %s", got)
	}

	entries := decodeList(t, r)
	first := entries[0].(map[string]any)
	if first["type"] != "tree" || first["mode"] != "040000" || first["name"] != "guide" {
		t.Errorf("tree entry = %v", first)
	}
	second := entries[1].(map[string]any)
	if second["type"] != "blob" || second["id"] == "" {
		t.Errorf("blob entry = %v", second)
	}
}

func decodeAny(t *testing.T, r response) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(r.body, &value); err != nil {
		t.Fatalf("not JSON: %v: %s", err, r.body)
	}
	return value
}

func TestTreeRootAndRecursive(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.seedDocs()

	root := h.get(projectPath("/repository/tree"))
	h.expectStatus(root, http.StatusOK)
	if got := strings.Join(paths(decodeAny(t, root)), ","); got != "README.md,docs,src" {
		t.Errorf("root paths = %s", got)
	}

	recursive := h.get(projectPath("/repository/tree?path=docs&recursive=true"))
	h.expectStatus(recursive, http.StatusOK)
	want := "docs/guide,docs/guide/deep,docs/guide/deep/more.md,docs/guide/start.md,docs/index.md"
	if got := strings.Join(paths(decodeAny(t, recursive)), ","); got != want {
		t.Errorf("recursive paths = %s, want %s", got, want)
	}
}

func TestTreeMissingPathOrRef(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.seedDocs()

	h.expectStatus(h.get(projectPath("/repository/tree?path=nope")), http.StatusNotFound)
	h.expectStatus(h.get(projectPath("/repository/tree?ref=unknown")), http.StatusNotFound)
}

func TestTreeKeysetPaginationFollowsLink(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	for _, name := range []string{"a.md", "b.md", "c.md", "d.md", "e.md"} {
		h.fake.PutFile(testProject, "master", "docs/"+name, name)
	}

	next := h.server.URL + projectPath("/repository/tree?path=docs&pagination=keyset&per_page=2")
	var collected []string
	pages := 0
	for next != "" {
		request, _ := http.NewRequest(http.MethodGet, next, nil)
		request.Header.Set("PRIVATE-TOKEN", testToken)
		page := h.do(request)
		h.expectStatus(page, http.StatusOK)
		collected = append(collected, paths(decodeAny(t, page))...)
		pages++
		next = parseNextLink(page.header.Get("Link"))
		if pages > 10 {
			t.Fatal("pagination does not terminate")
		}
	}
	if pages != 3 {
		t.Errorf("pages = %d", pages)
	}
	if got := strings.Join(collected, ","); got != "docs/a.md,docs/b.md,docs/c.md,docs/d.md,docs/e.md" {
		t.Errorf("collected = %s", got)
	}
}

func parseNextLink(header string) string {
	start := strings.Index(header, "<")
	end := strings.Index(header, ">")
	if start == -1 || end == -1 || !strings.Contains(header, `rel="next"`) {
		return ""
	}
	return header[start+1 : end]
}

func TestTreeOffsetPagination(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		h.fake.PutFile(testProject, "master", "docs/"+name, name)
	}

	first := h.get(projectPath("/repository/tree?path=docs&per_page=2"))
	if first.header.Get("X-Next-Page") != "2" || len(decodeList(t, first)) != 2 {
		t.Errorf("first page: next=%q body=%s", first.header.Get("X-Next-Page"), first.body)
	}
	second := h.get(projectPath("/repository/tree?path=docs&per_page=2&page=2"))
	if second.header.Get("X-Next-Page") != "" || len(decodeList(t, second)) != 1 {
		t.Errorf("second page: next=%q body=%s", second.header.Get("X-Next-Page"), second.body)
	}
	beyond := h.get(projectPath("/repository/tree?path=docs&per_page=2&page=9"))
	if len(decodeList(t, beyond)) != 0 {
		t.Errorf("page beyond the end must be empty: %s", beyond.body)
	}
}

func TestTreePerPageIsCapped(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	for i := 0; i < 150; i++ {
		h.fake.PutFile(testProject, "master", fmt.Sprintf("docs/f%03d.md", i), "x")
	}
	r := h.get(projectPath("/repository/tree?path=docs&per_page=500"))
	if got := len(decodeList(t, r)); got != maxPerPage {
		t.Errorf("entries = %d, want %d", got, maxPerPage)
	}
	d := h.get(projectPath("/repository/tree?path=docs&per_page=zero"))
	if got := len(decodeList(t, d)); got != defaultPerPage {
		t.Errorf("entries = %d, want %d", got, defaultPerPage)
	}
}

func TestFileMetadataAndRaw(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.fake.PutFile(testProject, "master", "docs/guide/start.md", "# Start\n")

	meta := h.get(filePath("docs/guide/start.md", "?ref=master"))
	h.expectStatus(meta, http.StatusOK)
	decoded, err := base64.StdEncoding.DecodeString(meta.decoded["content"].(string))
	if err != nil || string(decoded) != "# Start\n" {
		t.Fatalf("content = %q, err = %v", decoded, err)
	}
	if meta.decoded["file_name"] != "start.md" || meta.decoded["blob_id"] == "" || meta.decoded["last_commit_id"] == "" {
		t.Errorf("metadata = %v", meta.decoded)
	}

	rawResponse := h.get(projectPath("/repository/files/" + url.PathEscape("docs/guide/start.md") + "/raw"))
	h.expectStatus(rawResponse, http.StatusOK)
	if string(rawResponse.body) != "# Start\n" {
		t.Errorf("raw body = %q", rawResponse.body)
	}
	if rawResponse.header.Get("X-Gitlab-Blob-Id") != meta.decoded["blob_id"] {
		t.Errorf("blob id header = %q", rawResponse.header.Get("X-Gitlab-Blob-Id"))
	}
	if rawResponse.header.Get("X-Gitlab-Last-Commit-Id") != meta.decoded["last_commit_id"] {
		t.Errorf("last commit header = %q", rawResponse.header.Get("X-Gitlab-Last-Commit-Id"))
	}
	if rawResponse.header.Get("X-Gitlab-File-Path") != "docs/guide/start.md" || rawResponse.header.Get("X-Gitlab-Ref") != "master" {
		t.Errorf("headers = %v", rawResponse.header)
	}
}

func TestBlobIDsMatchGit(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.fake.PutFile(testProject, "master", "hello.txt", "hello\n")

	r := h.get(filePath("hello.txt", "?ref=master"))
	h.expectStatus(r, http.StatusOK)
	if r.decoded["blob_id"] != "ce013625030ba8dba906f756967f9e9ca394464a" {
		t.Errorf("blob id = %v (git hash-object of \"hello\\n\" is ce013625030ba8dba906f756967f9e9ca394464a)", r.decoded["blob_id"])
	}
}

func TestFileNotFound(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.expectStatus(h.get(filePath("absent.md", "?ref=master")), http.StatusNotFound)
	h.expectStatus(h.get(filePath("absent.md", "?ref=unknown")), http.StatusNotFound)
	h.expectStatus(h.get(projectPath("/repository/files/"+url.PathEscape("absent.md")+"/raw")), http.StatusNotFound)
}

func TestBlobRaw(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.fake.PutFile(testProject, "master", "a.md", "alpha")
	blobID := h.get(filePath("a.md", "?ref=master")).decoded["blob_id"].(string)

	r := h.get(projectPath("/repository/blobs/" + blobID + "/raw"))
	h.expectStatus(r, http.StatusOK)
	if string(r.body) != "alpha" {
		t.Errorf("body = %q", r.body)
	}
	h.expectStatus(h.get(projectPath("/repository/blobs/0000/raw")), http.StatusNotFound)
}

func commit(branch, message string, actions ...map[string]any) map[string]any {
	return map[string]any{"branch": branch, "commit_message": message, "actions": actions}
}

func action(kind, path string, extra ...any) map[string]any {
	result := map[string]any{"action": kind, "file_path": path}
	for i := 0; i+1 < len(extra); i += 2 {
		result[extra[i].(string)] = extra[i+1]
	}
	return result
}

func TestCommitCreateUpdateDelete(t *testing.T) {
	h := newHarness(t, ProjectOptions{})

	created := h.post(projectPath("/repository/commits"), commit("master", "add docs\n\nbody",
		action("create", "docs/a.md", "content", "one"),
		action("create", "docs/b.md", "content", "two"),
	))
	h.expectStatus(created, http.StatusCreated)
	if created.decoded["title"] != "add docs" || len(created.decoded["id"].(string)) != 40 {
		t.Errorf("commit = %s", created.body)
	}
	if got, _ := h.fake.File(testProject, "master", "docs/a.md"); got != "one" {
		t.Errorf("a.md = %q", got)
	}

	updated := h.post(projectPath("/repository/commits"), commit("master", "edit",
		action("update", "docs/a.md", "content", "uno"),
		action("delete", "docs/b.md"),
	))
	h.expectStatus(updated, http.StatusCreated)
	if got, _ := h.fake.File(testProject, "master", "docs/a.md"); got != "uno" {
		t.Errorf("a.md = %q", got)
	}
	if _, exists := h.fake.File(testProject, "master", "docs/b.md"); exists {
		t.Error("b.md should be deleted")
	}
}

func TestCommitBase64Content(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	encoded := base64.StdEncoding.EncodeToString([]byte("binary\x00data"))

	h.expectStatus(h.post(projectPath("/repository/commits"), commit("master", "img",
		action("create", "docs/img.bin", "content", encoded, "encoding", "base64"))), http.StatusCreated)
	if got, _ := h.fake.File(testProject, "master", "docs/img.bin"); got != "binary\x00data" {
		t.Errorf("content = %q", got)
	}

	bad := h.post(projectPath("/repository/commits"), commit("master", "img2",
		action("create", "docs/bad.bin", "content", "!!!not-base64", "encoding", "base64")))
	h.expectStatus(bad, http.StatusBadRequest)
}

func TestCommitMove(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.fake.PutFile(testProject, "master", "docs/old.md", "text")
	h.fake.PutFile(testProject, "master", "docs/taken.md", "x")

	h.expectStatus(h.post(projectPath("/repository/commits"), commit("master", "mv",
		action("move", "docs/new.md", "previous_path", "docs/old.md"))), http.StatusCreated)
	if got, _ := h.fake.File(testProject, "master", "docs/new.md"); got != "text" {
		t.Errorf("moved content = %q", got)
	}
	if _, exists := h.fake.File(testProject, "master", "docs/old.md"); exists {
		t.Error("old path should be gone")
	}

	h.expectStatus(h.post(projectPath("/repository/commits"), commit("master", "mv+edit",
		action("move", "docs/edited.md", "previous_path", "docs/new.md", "content", "changed"))), http.StatusCreated)
	if got, _ := h.fake.File(testProject, "master", "docs/edited.md"); got != "changed" {
		t.Errorf("edited content = %q", got)
	}

	h.expectStatus(h.post(projectPath("/repository/commits"), commit("master", "mv onto existing",
		action("move", "docs/taken.md", "previous_path", "docs/edited.md"))), http.StatusBadRequest)
	h.expectStatus(h.post(projectPath("/repository/commits"), commit("master", "mv missing",
		action("move", "docs/z.md", "previous_path", "docs/ghost.md"))), http.StatusBadRequest)
}

func TestCommitValidation(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.fake.PutFile(testProject, "master", "docs/a.md", "a")

	tests := []struct {
		name    string
		payload any
	}{
		{"missing branch", map[string]any{"commit_message": "m", "actions": []any{action("create", "x", "content", "c")}}},
		{"missing message", map[string]any{"branch": "master", "actions": []any{action("create", "x", "content", "c")}}},
		{"no actions", map[string]any{"branch": "master", "commit_message": "m"}},
		{"create existing file", commit("master", "m", action("create", "docs/a.md", "content", "again"))},
		{"update missing file", commit("master", "m", action("update", "docs/none.md", "content", "x"))},
		{"delete missing file", commit("master", "m", action("delete", "docs/none.md"))},
		{"unknown action", commit("master", "m", action("explode", "docs/a.md"))},
		{"missing file path", commit("master", "m", map[string]any{"action": "create", "content": "x"})},
		{"unknown branch without start", commit("feature", "m", action("create", "x.md", "content", "c"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h.expectStatus(h.post(projectPath("/repository/commits"), tt.payload), http.StatusBadRequest)
		})
	}

	request, _ := http.NewRequest(http.MethodPost, h.server.URL+projectPath("/repository/commits"), strings.NewReader("{"))
	request.Header.Set("PRIVATE-TOKEN", testToken)
	h.expectStatus(h.do(request), http.StatusBadRequest)
}

func TestCommitFailureLeavesBranchUntouched(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.fake.PutFile(testProject, "master", "docs/a.md", "a")

	r := h.post(projectPath("/repository/commits"), commit("master", "atomic",
		action("create", "docs/new.md", "content", "n"),
		action("update", "docs/missing.md", "content", "x"),
	))
	h.expectStatus(r, http.StatusBadRequest)
	if _, exists := h.fake.File(testProject, "master", "docs/new.md"); exists {
		t.Error("partial commit leaked into the branch")
	}
}

func TestCommitDetectsStaleLastCommitID(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.fake.PutFile(testProject, "master", "docs/a.md", "v1")
	stale := h.get(filePath("docs/a.md", "?ref=master")).decoded["last_commit_id"].(string)
	h.fake.PutFile(testProject, "master", "docs/a.md", "v2")

	r := h.post(projectPath("/repository/commits"), commit("master", "edit",
		action("update", "docs/a.md", "content", "mine", "last_commit_id", stale)))
	h.expectStatus(r, http.StatusBadRequest)
	if !strings.Contains(r.decoded["message"].(string), "changed since you started editing") {
		t.Errorf("message = %v", r.decoded["message"])
	}

	fresh := h.get(filePath("docs/a.md", "?ref=master")).decoded["last_commit_id"].(string)
	h.expectStatus(h.post(projectPath("/repository/commits"), commit("master", "edit",
		action("update", "docs/a.md", "content", "mine", "last_commit_id", fresh))), http.StatusCreated)

	h.expectStatus(h.post(projectPath("/repository/commits"), commit("master", "rm",
		action("delete", "docs/a.md", "last_commit_id", stale))), http.StatusBadRequest)
}

func TestCommitCreatesBranchFromStartBranch(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.fake.PutFile(testProject, "master", "docs/a.md", "a")

	payload := commit("vibefluence/new", "add", action("create", "docs/b.md", "content", "b"))
	payload["start_branch"] = "master"
	h.expectStatus(h.post(projectPath("/repository/commits"), payload), http.StatusCreated)

	if got, _ := h.fake.File(testProject, "vibefluence/new", "docs/a.md"); got != "a" {
		t.Errorf("branch must inherit master files, a.md = %q", got)
	}
	if _, onMaster := h.fake.File(testProject, "master", "docs/b.md"); onMaster {
		t.Error("new branch commit leaked into master")
	}
	if got := strings.Join(h.fake.Branches(testProject), ","); got != "master,vibefluence/new" {
		t.Errorf("branches = %s", got)
	}

	payload["start_branch"] = "unknown"
	payload["branch"] = "other"
	h.expectStatus(h.post(projectPath("/repository/commits"), payload), http.StatusBadRequest)
}

func TestMergeRequestHappyPath(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.fake.PutFile(testProject, "master", "docs/a.md", "a")
	h.fake.PutFile(testProject, "master", "docs/gone.md", "bye")

	payload := commit("vibefluence/doc", "add b",
		action("create", "docs/b.md", "content", "b"),
		action("update", "docs/a.md", "content", "a2"),
		action("delete", "docs/gone.md"),
	)
	payload["start_branch"] = "master"
	h.expectStatus(h.post(projectPath("/repository/commits"), payload), http.StatusCreated)

	created := h.post(projectPath("/merge_requests"), map[string]any{
		"source_branch": "vibefluence/doc", "target_branch": "master", "title": "Add b", "remove_source_branch": true,
	})
	h.expectStatus(created, http.StatusCreated)
	if created.decoded["iid"] != float64(1) || created.decoded["state"] != "opened" || created.decoded["detailed_merge_status"] != "checking" {
		t.Fatalf("merge request = %s", created.body)
	}
	head := created.decoded["sha"].(string)
	h.settle(1)

	merged := h.put(projectPath("/merge_requests/1/merge"), map[string]any{
		"sha": head, "merge_commit_message": "Vibefluence: add b",
	})
	h.expectStatus(merged, http.StatusOK)
	if merged.decoded["state"] != "merged" || merged.decoded["merge_commit_sha"] == nil {
		t.Errorf("merged = %s", merged.body)
	}

	if got, _ := h.fake.File(testProject, "master", "docs/b.md"); got != "b" {
		t.Errorf("b.md on master = %q", got)
	}
	if got, _ := h.fake.File(testProject, "master", "docs/a.md"); got != "a2" {
		t.Errorf("a.md on master = %q", got)
	}
	if _, exists := h.fake.File(testProject, "master", "docs/gone.md"); exists {
		t.Error("deleted file must disappear from master")
	}
	if got := strings.Join(h.fake.Branches(testProject), ","); got != "master" {
		t.Errorf("source branch should be removed, branches = %s", got)
	}

	again := h.put(projectPath("/merge_requests/1/merge"), nil)
	h.expectStatus(again, http.StatusMethodNotAllowed)

	infos := h.fake.MergeRequests(testProject)
	if len(infos) != 1 || infos[0].State != "merged" || infos[0].DetailedMergeStatus != "not_open" || infos[0].MergeCommitSHA == "" {
		t.Errorf("infos = %+v", infos)
	}
}

func TestMergeKeepsSourceBranchByDefault(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.fake.PutFile(testProject, "master", "docs/a.md", "a")
	payload := commit("topic", "edit", action("update", "docs/a.md", "content", "a2"))
	payload["start_branch"] = "master"
	h.expectStatus(h.post(projectPath("/repository/commits"), payload), http.StatusCreated)
	h.expectStatus(h.post(projectPath("/merge_requests"), map[string]any{
		"source_branch": "topic", "target_branch": "master", "title": "t",
	}), http.StatusCreated)
	h.settle(1)

	h.expectStatus(h.put(projectPath("/merge_requests/1/merge"), nil), http.StatusOK)
	if got := strings.Join(h.fake.Branches(testProject), ","); got != "master,topic" {
		t.Errorf("branches = %s", got)
	}
}

func TestMergeCanRemoveSourceBranchViaRequest(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.fake.PutFile(testProject, "master", "docs/a.md", "a")
	payload := commit("topic", "edit", action("update", "docs/a.md", "content", "a2"))
	payload["start_branch"] = "master"
	h.expectStatus(h.post(projectPath("/repository/commits"), payload), http.StatusCreated)
	h.expectStatus(h.post(projectPath("/merge_requests"), map[string]any{
		"source_branch": "topic", "target_branch": "master", "title": "t",
	}), http.StatusCreated)
	h.settle(1)

	h.expectStatus(h.put(projectPath("/merge_requests/1/merge"), map[string]any{"should_remove_source_branch": true}), http.StatusOK)
	if got := strings.Join(h.fake.Branches(testProject), ","); got != "master" {
		t.Errorf("branches = %s", got)
	}
}

func (h *harness) settle(iid int) {
	h.t.Helper()
	for i := 0; i < 5; i++ {
		if h.get(projectPath("/merge_requests/" + strconv.Itoa(iid))).decoded["detailed_merge_status"] != "checking" {
			return
		}
	}
	h.t.Fatal("merge request never left the checking state")
}

func openMergeRequest(h *harness, branchName string) string {
	h.t.Helper()
	payload := commit(branchName, "edit", action("create", "docs/"+branchName+".md", "content", branchName))
	payload["start_branch"] = "master"
	h.expectStatus(h.post(projectPath("/repository/commits"), payload), http.StatusCreated)
	created := h.post(projectPath("/merge_requests"), map[string]any{
		"source_branch": branchName, "target_branch": "master", "title": branchName,
	})
	h.expectStatus(created, http.StatusCreated)
	h.settle(int(created.decoded["iid"].(float64)))
	return created.decoded["sha"].(string)
}

func TestMergeCreationValidation(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	openMergeRequest(h, "topic")

	tests := []struct {
		name    string
		payload any
		want    int
	}{
		{"missing fields", map[string]any{"source_branch": "topic"}, http.StatusBadRequest},
		{"unknown source", map[string]any{"source_branch": "x", "target_branch": "master", "title": "t"}, http.StatusBadRequest},
		{"unknown target", map[string]any{"source_branch": "topic", "target_branch": "x", "title": "t"}, http.StatusBadRequest},
		{"same branches", map[string]any{"source_branch": "master", "target_branch": "master", "title": "t"}, http.StatusUnprocessableEntity},
		{"duplicate open merge request", map[string]any{"source_branch": "topic", "target_branch": "master", "title": "t"}, http.StatusConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h.expectStatus(h.post(projectPath("/merge_requests"), tt.payload), tt.want)
		})
	}

	request, _ := http.NewRequest(http.MethodPost, h.server.URL+projectPath("/merge_requests"), strings.NewReader("{"))
	request.Header.Set("PRIVATE-TOKEN", testToken)
	h.expectStatus(h.do(request), http.StatusBadRequest)
}

func TestGetMergeRequest(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	openMergeRequest(h, "topic")

	r := h.get(projectPath("/merge_requests/1"))
	h.expectStatus(r, http.StatusOK)
	if r.decoded["source_branch"] != "topic" || !strings.Contains(r.decoded["web_url"].(string), "/merge_requests/1") {
		t.Errorf("merge request = %s", r.body)
	}
	h.expectStatus(h.get(projectPath("/merge_requests/9")), http.StatusNotFound)
	h.expectStatus(h.get(projectPath("/merge_requests/abc")), http.StatusNotFound)
	h.expectStatus(h.put(projectPath("/merge_requests/9/merge"), nil), http.StatusNotFound)
}

func TestMergeShaMismatch(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	openMergeRequest(h, "topic")

	h.expectStatus(h.put(projectPath("/merge_requests/1/merge"), map[string]any{"sha": "deadbeef"}), http.StatusConflict)
}

func TestMergeRequiresShaWhenConfigured(t *testing.T) {
	h := newHarness(t, ProjectOptions{RequireMergeSHA: true})
	head := openMergeRequest(h, "topic")

	h.expectStatus(h.put(projectPath("/merge_requests/1/merge"), nil), http.StatusBadRequest)
	h.expectStatus(h.put(projectPath("/merge_requests/1/merge"), map[string]any{"sha": head}), http.StatusOK)
}

func TestMergeForbiddenForBot(t *testing.T) {
	h := newHarness(t, ProjectOptions{ForbidMerge: true})
	openMergeRequest(h, "topic")

	h.expectStatus(h.put(projectPath("/merge_requests/1/merge"), nil), http.StatusUnauthorized)
	if _, merged := h.fake.File(testProject, "master", "docs/topic.md"); merged {
		t.Error("forbidden merge must not change master")
	}
}

func TestMergeBlockedByApproval(t *testing.T) {
	h := newHarness(t, ProjectOptions{ApprovalRequired: true})
	openMergeRequest(h, "topic")

	r := h.get(projectPath("/merge_requests/1"))
	if r.decoded["detailed_merge_status"] != "not_approved" {
		t.Errorf("status = %v", r.decoded["detailed_merge_status"])
	}
	h.expectStatus(h.put(projectPath("/merge_requests/1/merge"), nil), http.StatusMethodNotAllowed)

	h.fake.ApproveMergeRequest(testProject, 1)
	h.expectStatus(h.put(projectPath("/merge_requests/1/merge"), nil), http.StatusOK)
}

func TestMergeBlockedByPipelinesMustSucceedWithoutPipelines(t *testing.T) {
	h := newHarness(t, ProjectOptions{PipelinesMustSucceed: true})
	openMergeRequest(h, "topic")

	r := h.get(projectPath("/merge_requests/1"))
	if r.decoded["detailed_merge_status"] != "ci_must_pass" {
		t.Errorf("status = %v", r.decoded["detailed_merge_status"])
	}
	h.expectStatus(h.put(projectPath("/merge_requests/1/merge"), nil), http.StatusMethodNotAllowed)
}

func TestPipelinesAreCreatedOnlyForConfiguredBranches(t *testing.T) {
	h := newHarness(t, ProjectOptions{
		PipelinesMustSucceed: true,
		PipelineBranches:     func(branch string) bool { return !strings.HasPrefix(branch, "vibefluence/") },
	})
	openMergeRequest(h, "vibefluence/quiet")
	if got := h.fake.Pipelines(testProject); len(got) != 0 {
		t.Errorf("excluded branch created pipelines: %+v", got)
	}

	openMergeRequest(h, "noisy")
	pipelines := h.fake.Pipelines(testProject)
	if len(pipelines) != 2 {
		t.Fatalf("expected a branch pipeline and a merge request pipeline, got %+v", pipelines)
	}

	byRef := h.get(projectPath("/pipelines?ref=noisy"))
	if got := len(decodeList(t, byRef)); got != 2 {
		t.Errorf("pipelines by ref = %d", got)
	}
	all := h.get(projectPath("/pipelines"))
	if got := len(decodeList(t, all)); got != 2 {
		t.Errorf("all pipelines = %d", got)
	}
	forMR := h.get(projectPath("/merge_requests/2/pipelines"))
	if got := len(decodeList(t, forMR)); got != 2 {
		t.Errorf("merge request pipelines = %d", got)
	}
	quietMR := h.get(projectPath("/merge_requests/1/pipelines"))
	if got := len(decodeList(t, quietMR)); got != 0 {
		t.Errorf("excluded merge request pipelines = %d", got)
	}

	running := h.get(projectPath("/merge_requests/2"))
	if running.decoded["detailed_merge_status"] != "ci_still_running" || running.decoded["head_pipeline"] == nil {
		t.Errorf("running pipeline must block: %s", running.body)
	}

	h.fake.CompletePipelines(testProject)
	done := h.get(projectPath("/merge_requests/2"))
	if done.decoded["detailed_merge_status"] != "mergeable" {
		t.Errorf("status after completion = %v", done.decoded["detailed_merge_status"])
	}
}

func TestMergeCreatesPipelineOnTargetBranch(t *testing.T) {
	h := newHarness(t, ProjectOptions{PipelineBranches: func(branch string) bool { return branch == "master" }})
	openMergeRequest(h, "topic")
	h.expectStatus(h.put(projectPath("/merge_requests/1/merge"), nil), http.StatusOK)

	pipelines := h.fake.Pipelines(testProject)
	if len(pipelines) != 1 || pipelines[0].Ref != "master" || pipelines[0].Status != "pending" {
		t.Errorf("pipelines = %+v", pipelines)
	}
}

func TestMergeConflictIsDetected(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.fake.PutFile(testProject, "master", "docs/a.md", "base")

	payload := commit("topic", "edit", action("update", "docs/a.md", "content", "from topic"))
	payload["start_branch"] = "master"
	h.expectStatus(h.post(projectPath("/repository/commits"), payload), http.StatusCreated)
	h.fake.PutFile(testProject, "master", "docs/a.md", "from master")
	h.expectStatus(h.post(projectPath("/merge_requests"), map[string]any{
		"source_branch": "topic", "target_branch": "master", "title": "t",
	}), http.StatusCreated)
	h.settle(1)

	r := h.get(projectPath("/merge_requests/1"))
	if r.decoded["detailed_merge_status"] != "conflict" || r.decoded["has_conflicts"] != true {
		t.Errorf("merge request = %s", r.body)
	}
	h.expectStatus(h.put(projectPath("/merge_requests/1/merge"), nil), http.StatusMethodNotAllowed)
}

func TestMergeConflictRules(t *testing.T) {
	tests := []struct {
		name         string
		sourceAction map[string]any
		mutateTarget func(h *harness)
		wantConflict bool
	}{
		{
			name:         "both add different content",
			sourceAction: action("create", "docs/new.md", "content", "source"),
			mutateTarget: func(h *harness) { h.fake.PutFile(testProject, "master", "docs/new.md", "target") },
			wantConflict: true,
		},
		{
			name:         "both add identical content",
			sourceAction: action("create", "docs/new.md", "content", "same"),
			mutateTarget: func(h *harness) { h.fake.PutFile(testProject, "master", "docs/new.md", "same") },
			wantConflict: false,
		},
		{
			name:         "source deletes what target edited",
			sourceAction: action("delete", "docs/a.md"),
			mutateTarget: func(h *harness) { h.fake.PutFile(testProject, "master", "docs/a.md", "edited") },
			wantConflict: true,
		},
		{
			name:         "both delete",
			sourceAction: action("delete", "docs/a.md"),
			mutateTarget: func(h *harness) { h.fake.DeleteFile(testProject, "master", "docs/a.md") },
			wantConflict: false,
		},
		{
			name:         "target touches unrelated file",
			sourceAction: action("update", "docs/a.md", "content", "src"),
			mutateTarget: func(h *harness) { h.fake.PutFile(testProject, "master", "docs/other.md", "x") },
			wantConflict: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, ProjectOptions{})
			h.fake.PutFile(testProject, "master", "docs/a.md", "base")
			payload := commit("topic", "edit", tt.sourceAction)
			payload["start_branch"] = "master"
			h.expectStatus(h.post(projectPath("/repository/commits"), payload), http.StatusCreated)
			tt.mutateTarget(h)
			h.expectStatus(h.post(projectPath("/merge_requests"), map[string]any{
				"source_branch": "topic", "target_branch": "master", "title": "t",
			}), http.StatusCreated)

			got := h.get(projectPath("/merge_requests/1")).decoded["has_conflicts"]
			if got != tt.wantConflict {
				t.Errorf("has_conflicts = %v, want %v", got, tt.wantConflict)
			}
		})
	}
}

func TestMergeRequestWithRemovedSourceBranchHasNoSha(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	openMergeRequest(h, "topic")
	h.expectStatus(h.put(projectPath("/merge_requests/1/merge"), map[string]any{"should_remove_source_branch": true}), http.StatusOK)

	r := h.get(projectPath("/merge_requests/1"))
	if r.decoded["sha"] != "" || r.decoded["state"] != "merged" {
		t.Errorf("merge request = %s", r.body)
	}
}

func TestFailNextInjectsErrors(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.fake.FailNext(2, http.StatusTooManyRequests)

	first := h.get("/api/v4/user")
	h.expectStatus(first, http.StatusTooManyRequests)
	if first.header.Get("Retry-After") != "1" {
		t.Errorf("Retry-After = %q", first.header.Get("Retry-After"))
	}
	h.expectStatus(h.get("/api/v4/user"), http.StatusTooManyRequests)
	h.expectStatus(h.get("/api/v4/user"), http.StatusOK)

	h.fake.FailNext(1, http.StatusBadGateway)
	bad := h.get("/api/v4/user")
	h.expectStatus(bad, http.StatusBadGateway)
	if bad.header.Get("Retry-After") != "" {
		t.Error("Retry-After must only be set for 429")
	}
	if got := h.fake.RequestCount(); got != 4 {
		t.Errorf("request count = %d", got)
	}
}

func TestSetProjectOptionsTakesEffect(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	openMergeRequest(h, "topic")
	if got := h.get(projectPath("/merge_requests/1")).decoded["detailed_merge_status"]; got != "mergeable" {
		t.Fatalf("status = %v", got)
	}
	h.fake.SetProjectOptions(testProject, ProjectOptions{ApprovalRequired: true})
	if got := h.get(projectPath("/merge_requests/1")).decoded["detailed_merge_status"]; got != "not_approved" {
		t.Errorf("status = %v", got)
	}
}

func TestTestHelpersPanicOnUnknownEntities(t *testing.T) {
	fake := New(testToken)
	fake.AddProject(testProject, "master", ProjectOptions{})

	mustPanic := func(name string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s did not panic", name)
			}
		}()
		fn()
	}
	mustPanic("unknown project", func() { fake.PutFile("none/none", "master", "a", "b") })
	mustPanic("unknown branch", func() { fake.PutFile(testProject, "ghost", "a", "b") })
	mustPanic("unknown merge request", func() { fake.ApproveMergeRequest(testProject, 3) })
	mustPanic("unknown merge request zero", func() { fake.ApproveMergeRequest(testProject, 0) })

	if _, ok := fake.File(testProject, "ghost", "a"); ok {
		t.Error("File on unknown branch must report not found")
	}
}

func TestSeedDemo(t *testing.T) {
	fake := New(testToken)
	SeedDemo(fake)
	server := httptest.NewServer(fake)
	defer server.Close()

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v4/projects/"+url.PathEscape(DemoProject)+"/repository/tree?path=docs&recursive=true", nil)
	request.Header.Set("PRIVATE-TOKEN", testToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var entries []map[string]any
	if err := json.NewDecoder(response.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) < 5 {
		t.Errorf("demo tree is too small: %d entries", len(entries))
	}
	if content, ok := fake.File(DemoProject, "master", "docs/specs/draft-rollout.md"); !ok || !strings.Contains(content, "status: draft") {
		t.Error("demo data must include a draft document")
	}
}

func TestMergeRequestStartsInCheckingState(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.fake.PutFile(testProject, "master", "docs/a.md", "a")
	payload := commit("topic", "edit", action("update", "docs/a.md", "content", "a2"))
	payload["start_branch"] = "master"
	h.expectStatus(h.post(projectPath("/repository/commits"), payload), http.StatusCreated)

	created := h.post(projectPath("/merge_requests"), map[string]any{
		"source_branch": "topic", "target_branch": "master", "title": "t",
	})
	if created.decoded["detailed_merge_status"] != "checking" {
		t.Fatalf("new merge request status = %v", created.decoded["detailed_merge_status"])
	}

	early := h.put(projectPath("/merge_requests/1/merge"), nil)
	h.expectStatus(early, http.StatusMethodNotAllowed)
	if got := h.fake.MergeRequests(testProject)[0].DetailedMergeStatus; got != "mergeable" {
		t.Errorf("status after the first read = %s", got)
	}
	h.expectStatus(h.put(projectPath("/merge_requests/1/merge"), nil), http.StatusOK)
}

func TestGetConsumesTheCheckingState(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	openMergeRequestUnsettled(h, "topic")

	first := h.get(projectPath("/merge_requests/1"))
	second := h.get(projectPath("/merge_requests/1"))
	if first.decoded["detailed_merge_status"] != "checking" || second.decoded["detailed_merge_status"] != "mergeable" {
		t.Errorf("statuses = %v then %v", first.decoded["detailed_merge_status"], second.decoded["detailed_merge_status"])
	}
}

func openMergeRequestUnsettled(h *harness, branchName string) {
	h.t.Helper()
	payload := commit(branchName, "edit", action("create", "docs/"+branchName+".md", "content", branchName))
	payload["start_branch"] = "master"
	h.expectStatus(h.post(projectPath("/repository/commits"), payload), http.StatusCreated)
	h.expectStatus(h.post(projectPath("/merge_requests"), map[string]any{
		"source_branch": branchName, "target_branch": "master", "title": branchName,
	}), http.StatusCreated)
}

func TestFailedPipelineBlocksMergeWithCIMustPass(t *testing.T) {
	h := newHarness(t, ProjectOptions{
		PipelinesMustSucceed: true,
		PipelineBranches:     func(string) bool { return true },
	})
	openMergeRequest(h, "topic")
	h.fake.FailPipelines(testProject)

	if got := h.get(projectPath("/merge_requests/1")).decoded["detailed_merge_status"]; got != "ci_must_pass" {
		t.Errorf("status = %v", got)
	}
}

func TestPipelinesAreListedNewestFirstAndStartPending(t *testing.T) {
	h := newHarness(t, ProjectOptions{PipelineBranches: func(string) bool { return true }})
	h.fake.PutFile(testProject, "master", "docs/a.md", "a")
	for _, name := range []string{"first", "second"} {
		payload := commit(name, "edit", action("create", "docs/"+name+".md", "content", name))
		payload["start_branch"] = "master"
		h.expectStatus(h.post(projectPath("/repository/commits"), payload), http.StatusCreated)
	}

	list := decodeList(t, h.get(projectPath("/pipelines")))
	if len(list) != 2 {
		t.Fatalf("pipelines = %d", len(list))
	}
	newest, oldest := list[0].(map[string]any), list[1].(map[string]any)
	if newest["ref"] != "second" || oldest["ref"] != "first" {
		t.Errorf("order = %v, %v", newest["ref"], oldest["ref"])
	}
	if newest["status"] != "pending" {
		t.Errorf("initial status = %v", newest["status"])
	}
}

func TestFileMetadataRequiresRef(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.fake.PutFile(testProject, "master", "docs/a.md", "a")

	r := h.get(filePath("docs/a.md", ""))
	h.expectStatus(r, http.StatusBadRequest)
	if r.decoded["error"] != "ref is missing" {
		t.Errorf("body = %s", r.body)
	}
	h.expectStatus(h.get(projectPath("/repository/files/"+url.PathEscape("docs/a.md")+"/raw")), http.StatusOK)
}

func TestHeadReturnsFileHeadersWithoutBody(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.fake.PutFile(testProject, "master", "docs/a.md", "hello\n")

	r := h.call(http.MethodHead, filePath("docs/a.md", "?ref=master"), nil)
	h.expectStatus(r, http.StatusOK)
	if len(r.body) != 0 {
		t.Errorf("HEAD returned a body: %q", r.body)
	}
	want := map[string]string{
		"X-Gitlab-Blob-Id":        "ce013625030ba8dba906f756967f9e9ca394464a",
		"X-Gitlab-Size":           "6",
		"X-Gitlab-File-Name":      "a.md",
		"X-Gitlab-Ref":            "master",
		"X-Gitlab-Content-Sha256": "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03",
	}
	for name, value := range want {
		if got := r.header.Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
	if r.header.Get("X-Gitlab-Commit-Id") == "" || r.header.Get("X-Gitlab-Last-Commit-Id") == "" {
		t.Error("commit id headers are missing")
	}
}

func TestCommitContentSemantics(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	h.fake.PutFile(testProject, "master", "docs/a.md", "keep me")

	h.expectStatus(h.post(projectPath("/repository/commits"), commit("master", "m",
		map[string]any{"action": "update", "file_path": "docs/a.md"})), http.StatusBadRequest)
	h.expectStatus(h.post(projectPath("/repository/commits"), commit("master", "m",
		map[string]any{"action": "create", "file_path": "docs/new.md"})), http.StatusBadRequest)

	h.expectStatus(h.post(projectPath("/repository/commits"), commit("master", "empty file",
		action("update", "docs/a.md", "content", ""))), http.StatusCreated)
	if got, _ := h.fake.File(testProject, "master", "docs/a.md"); got != "" {
		t.Errorf("empty content must replace the file, got %q", got)
	}

	h.expectStatus(h.post(projectPath("/repository/commits"), commit("master", "move without content",
		action("move", "docs/b.md", "previous_path", "docs/a.md"))), http.StatusCreated)
}
