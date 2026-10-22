package portal

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vibefluence/internal/gitlab"
	"vibefluence/internal/index"
	"vibefluence/internal/testing/fakegitlab"
)

const (
	token   = "t"
	project = "group/docs"
)

type countingFiles struct {
	inner *gitlab.Client
	calls atomic.Int32
}

func (c *countingFiles) File(ctx context.Context, name, ref, filePath string) (gitlab.File, error) {
	c.calls.Add(1)
	return c.inner.File(ctx, name, ref, filePath)
}

type env struct {
	fake   *fakegitlab.Server
	files  *countingFiles
	portal *Portal
	server *httptest.Server
}

func newEnv(t *testing.T, repos ...index.Repo) *env {
	t.Helper()
	if len(repos) == 0 {
		repos = []index.Repo{{ID: "docs", Name: "Docs", Project: project, Branch: "master", Paths: []string{"docs"}}}
	}
	fake := fakegitlab.New(token)
	fake.AddProject(project, "master", fakegitlab.ProjectOptions{})
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	client, err := gitlab.New(gitlab.Options{BaseURL: server.URL, Token: token, MaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	files := &countingFiles{inner: client}
	manager := index.NewManager(index.Options{Repos: repos, Source: client, TTL: time.Minute})
	portal := New(Options{
		Index:     manager,
		Files:     files,
		GitLabURL: "https://gitlab.example.com/",
		Settings:  map[string]RepoSettings{"docs": {AutoMerge: false}},
	})
	return &env{fake: fake, files: files, portal: portal, server: server}
}

func (e *env) put(path, content string) { e.fake.PutFile(project, "master", path, content) }

func TestReposSummary(t *testing.T) {
	broken := index.Repo{ID: "broken", Name: "Broken", Project: "ghost/none", Branch: "master", Paths: []string{"docs"}}
	good := index.Repo{ID: "docs", Name: "Docs", Project: project, Paths: []string{"docs"}}
	e := newEnv(t, good, broken)
	e.put("docs/a.md", "# A")
	e.put("docs/b.md", "---\nstatus: draft\n---\nx")

	repos := e.portal.Repos(context.Background())
	if len(repos) != 2 {
		t.Fatalf("repos = %+v", repos)
	}
	docs, bad := repos[0], repos[1]
	if !docs.Available || docs.DocCount != 1 || docs.Hidden != 1 || docs.Branch != "master" || docs.AutoMerge {
		t.Errorf("docs = %+v", docs)
	}
	if docs.WebURL != "https://gitlab.example.com/group/docs" {
		t.Errorf("web url = %q", docs.WebURL)
	}
	if bad.Available || bad.DocCount != 0 || !bad.AutoMerge || bad.ID != "broken" {
		t.Errorf("broken = %+v", bad)
	}
}

func TestTree(t *testing.T) {
	e := newEnv(t)
	e.put("docs/a.md", "# A")
	e.put("docs/guide/b.md", "# B")

	tree, err := e.portal.Tree(context.Background(), "docs")
	if err != nil {
		t.Fatal(err)
	}
	if tree.Repo != "docs" || tree.Branch != "master" || len(tree.Roots) != 1 || len(tree.Roots[0].Children) != 2 {
		t.Errorf("tree = %+v", tree)
	}
	if _, err := e.portal.Tree(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestDocumentIsRenderedWithRewrittenLinks(t *testing.T) {
	e := newEnv(t)
	e.put("docs/guide/start.md", "---\ntitle: Start\ntype: guide\n---\n# Start\n\n## Install\n\nSee [next](next.md) and [code](../../src/main.go).\n\n![pic](pic.png)\n")

	doc, err := e.portal.Document(context.Background(), "docs", "docs/guide/start.md")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Start" || doc.Type != "guide" || doc.Status != "published" || doc.Stale {
		t.Errorf("doc = %+v", doc)
	}
	for _, want := range []string{
		`href="/r/docs/docs/guide/next.md"`,
		`href="https://gitlab.example.com/group/docs/-/blob/master/src/main.go"`,
		`src="/api/v1/repos/docs/raw?path=docs%2Fguide%2Fpic.png"`,
		`id="h-install"`,
	} {
		if !strings.Contains(doc.HTML, want) {
			t.Errorf("html lacks %q: %s", want, doc.HTML)
		}
	}
	if len(doc.Headings) != 2 || doc.Headings[1].Text != "Install" {
		t.Errorf("headings = %+v", doc.Headings)
	}
	if !strings.HasPrefix(doc.Raw, "---\ntitle: Start") || doc.BlobID == "" {
		t.Errorf("raw = %q blob = %q", doc.Raw, doc.BlobID)
	}
	if doc.WebURL != "https://gitlab.example.com/group/docs/-/blob/master/docs/guide/start.md" {
		t.Errorf("web url = %q", doc.WebURL)
	}
}

func TestDocumentNotFoundCases(t *testing.T) {
	e := newEnv(t)
	e.put("docs/draft.md", "---\nstatus: draft\n---\nsecret")
	e.put("src/outside.md", "# outside")

	for _, path := range []string{"docs/draft.md", "docs/none.md", "src/outside.md", "../etc/passwd", ""} {
		if _, err := e.portal.Document(context.Background(), "docs", path); !errors.Is(err, ErrNotFound) {
			t.Errorf("%q: err = %v", path, err)
		}
	}
	if _, err := e.portal.Document(context.Background(), "nope", "docs/a.md"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown repo err = %v", err)
	}
}

func TestSearchValidation(t *testing.T) {
	e := newEnv(t)
	e.put("docs/a.md", "# Needle\n")

	results, err := e.portal.Search(context.Background(), "", "needle", 5)
	if err != nil || len(results) != 1 {
		t.Fatalf("results = %+v, err = %v", results, err)
	}
	for _, query := range []string{"", "   ", strings.Repeat("x", maxQueryLength+1)} {
		if _, err := e.portal.Search(context.Background(), "", query, 5); !errors.Is(err, ErrInvalid) {
			t.Errorf("query %q: err = %v", query, err)
		}
	}
	if _, err := e.portal.Search(context.Background(), "nope", "needle", 5); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown repo err = %v", err)
	}
	for _, limit := range []int{0, -3, 1000} {
		if got, err := e.portal.Search(context.Background(), "", "needle", limit); err != nil || len(got) != 1 {
			t.Errorf("limit %d: %v %v", limit, got, err)
		}
	}
}

func TestValidationErrorMessage(t *testing.T) {
	err := invalid("bad %s", "input")
	if err.Error() != "bad input" || !errors.Is(err, ErrInvalid) || errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestRefreshPicksUpNewFiles(t *testing.T) {
	e := newEnv(t)
	e.put("docs/a.md", "# A")
	if _, err := e.portal.Tree(context.Background(), "docs"); err != nil {
		t.Fatal(err)
	}
	e.put("docs/b.md", "# B")

	summary, err := e.portal.Refresh(context.Background(), "docs")
	if err != nil || summary.DocCount != 2 {
		t.Errorf("summary = %+v, err = %v", summary, err)
	}
	if _, err := e.portal.Refresh(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown repo err = %v", err)
	}
}

func TestImageIsServedCachedAndRevalidated(t *testing.T) {
	e := newEnv(t)
	e.put("docs/a.md", "# A")
	e.put("docs/img/pic.png", "\x89PNG-bytes")

	image, notModified, err := e.portal.Image(context.Background(), "docs", "docs/img/pic.png", "")
	if err != nil || notModified {
		t.Fatalf("err = %v, notModified = %v", err, notModified)
	}
	if string(image.Content) != "\x89PNG-bytes" || image.ContentType != "image/png" || !strings.HasPrefix(image.ETag, `"`) {
		t.Errorf("image = %+v", image)
	}

	_, notModified, err = e.portal.Image(context.Background(), "docs", "docs/img/pic.png", image.ETag)
	if err != nil || !notModified {
		t.Errorf("matching ETag must report not modified: %v %v", notModified, err)
	}
	if e.files.calls.Load() != 1 {
		t.Errorf("GitLab file calls = %d, the second request must come from the cache", e.files.calls.Load())
	}
	_, notModified, _ = e.portal.Image(context.Background(), "docs", "docs/img/pic.png", `"other"`)
	if notModified {
		t.Error("a different ETag must not match")
	}
}

func TestImageFromOutsideDocsRootsIsAllowed(t *testing.T) {
	e := newEnv(t)
	e.put("docs/a.md", "# A")
	e.put("assets/logo.webp", "webp")
	image, _, err := e.portal.Image(context.Background(), "docs", "assets/logo.webp", "")
	if err != nil || image.ContentType != "image/webp" {
		t.Errorf("image = %+v, err = %v", image, err)
	}
}

func TestImageErrors(t *testing.T) {
	e := newEnv(t)
	e.put("docs/a.md", "# A")
	e.put("docs/vector.svg", "<svg/>")
	e.put("docs/big.png", strings.Repeat("x", maxImageBytes+1))

	invalidPaths := []string{"", "docs/vector.svg", "docs/a.md", "/docs/a.png", "docs\\a.png", "../a.png", "docs/../a.png", "docs//a.png", "docs/.hidden/a.png", "docs/a\x00.png", strings.Repeat("a", maxImagePathBytes) + ".png"}
	for _, path := range invalidPaths {
		if _, _, err := e.portal.Image(context.Background(), "docs", path, ""); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: err = %v", path, err)
		}
	}
	if _, _, err := e.portal.Image(context.Background(), "docs", "docs/missing.png", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing err = %v", err)
	}
	if _, _, err := e.portal.Image(context.Background(), "docs", "docs/big.png", ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("oversized err = %v", err)
	}
	if _, _, err := e.portal.Image(context.Background(), "nope", "docs/a.png", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown repo err = %v", err)
	}
}

func TestImageGitLabFailureIsReported(t *testing.T) {
	e := newEnv(t)
	e.put("docs/a.md", "# A")
	if _, err := e.portal.Tree(context.Background(), "docs"); err != nil {
		t.Fatal(err)
	}
	e.fake.FailNext(1, http.StatusInternalServerError)
	_, _, err := e.portal.Image(context.Background(), "docs", "docs/pic.png", "")
	var apiErr *gitlab.APIError
	if !errors.As(err, &apiErr) {
		t.Errorf("err = %v", err)
	}
}

type fixedClock struct{ now time.Time }

func (c *fixedClock) Now() time.Time { return c.now }

func TestImageCacheExpiryAndEviction(t *testing.T) {
	clock := &fixedClock{now: time.Unix(1000, 0)}
	cache := NewImageCache(2, 100, time.Minute, clock.Now)

	cache.Put("a", Image{Content: []byte("aaaa")})
	cache.Put("b", Image{Content: []byte("bbbb")})
	cache.Put("c", Image{Content: []byte("cccc")})
	if _, ok := cache.Get("a"); ok {
		t.Error("the oldest entry must be evicted when the entry limit is exceeded")
	}
	if cache.Len() != 2 {
		t.Errorf("len = %d", cache.Len())
	}

	clock.now = clock.now.Add(2 * time.Minute)
	if _, ok := cache.Get("b"); ok {
		t.Error("expired entries must not be served")
	}
	if cache.Len() != 1 {
		t.Errorf("len after expiry = %d", cache.Len())
	}
}

func TestImageCacheByteLimit(t *testing.T) {
	clock := &fixedClock{now: time.Unix(1000, 0)}
	cache := NewImageCache(10, 10, time.Hour, clock.Now)

	cache.Put("big", Image{Content: make([]byte, 11)})
	if cache.Len() != 0 {
		t.Error("an entry larger than the whole budget must not be stored")
	}
	cache.Put("a", Image{Content: make([]byte, 6)})
	cache.Put("b", Image{Content: make([]byte, 6)})
	if _, ok := cache.Get("a"); ok {
		t.Error("exceeding the byte budget must evict the oldest entry")
	}
	if _, ok := cache.Get("b"); !ok {
		t.Error("the newest entry must stay")
	}

	cache.Put("b", Image{Content: make([]byte, 2)})
	if cache.bytes != 2 || cache.Len() != 1 {
		t.Errorf("replacing must not double count: bytes = %d, len = %d", cache.bytes, cache.Len())
	}
	cache.remove("unknown")
}

func TestStaleFlagIsPropagated(t *testing.T) {
	e := newEnv(t)
	e.put("docs/a.md", "# A")
	if _, err := e.portal.Tree(context.Background(), "docs"); err != nil {
		t.Fatal(err)
	}
	e.portal.index.Invalidate("docs")
	e.fake.FailNext(1, http.StatusInternalServerError)

	doc, err := e.portal.Document(context.Background(), "docs", "docs/a.md")
	if err != nil || !doc.Stale {
		t.Errorf("doc stale = %v, err = %v", doc.Stale, err)
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	portal := New(Options{})
	if portal.renderer == nil || portal.images == nil || portal.logger == nil || portal.settings == nil {
		t.Errorf("defaults missing: %+v", portal)
	}
	if got := portal.blobURL(index.Repo{Project: "a/b"}, "main", "x.md"); got != "" {
		t.Errorf("blob url without a GitLab URL = %q", got)
	}
	if got := portal.projectURL(index.Repo{Project: "a/b"}); got != "" {
		t.Errorf("project url without a GitLab URL = %q", got)
	}
}
