package index

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vibefluence/internal/docs"
	"vibefluence/internal/gitlab"
	"vibefluence/internal/testing/fakegitlab"
)

const (
	token   = "t"
	project = "group/docs"
)

type spy struct {
	inner    *gitlab.Client
	projects atomic.Int32
	trees    atomic.Int32
	blobs    atomic.Int32
	gate     chan struct{}
}

func (s *spy) Project(ctx context.Context, name string) (gitlab.Project, error) {
	s.projects.Add(1)
	return s.inner.Project(ctx, name)
}

func (s *spy) Tree(ctx context.Context, name, ref, dir string) ([]gitlab.TreeEntry, error) {
	s.trees.Add(1)
	if s.gate != nil {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.inner.Tree(ctx, name, ref, dir)
}

func (s *spy) Branch(ctx context.Context, name, branch string) (gitlab.Branch, error) {
	return s.inner.Branch(ctx, name, branch)
}

func (s *spy) Blob(ctx context.Context, name, blobID string) ([]byte, error) {
	s.blobs.Add(1)
	return s.inner.Blob(ctx, name, blobID)
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type env struct {
	fake    *fakegitlab.Server
	spy     *spy
	clock   *clock
	manager *Manager
}

func baseRepo() Repo {
	return Repo{ID: "docs", Name: "Docs", Project: project, Branch: "master", Paths: []string{"docs"}}
}

func newEnv(t *testing.T, mutate func(*Options), repos ...Repo) *env {
	t.Helper()
	if len(repos) == 0 {
		repos = []Repo{baseRepo()}
	}
	fake := fakegitlab.New(token)
	fake.AddProject(project, "master", fakegitlab.ProjectOptions{})
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	client, err := gitlab.New(Options2(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	e := &env{fake: fake, spy: &spy{inner: client}, clock: &clock{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}}
	opts := Options{Repos: repos, Source: e.spy, Now: e.clock.Now, TTL: time.Minute}
	if mutate != nil {
		mutate(&opts)
	}
	e.manager = NewManager(opts)
	return e
}

func Options2(baseURL string) gitlab.Options {
	return gitlab.Options{BaseURL: baseURL, Token: token, MaxRetries: -1}
}

func (e *env) put(path, content string) { e.fake.PutFile(project, "master", path, content) }

func (e *env) seed() {
	e.put("docs/README.md", "---\ntitle: Welcome\ntype: guide\n---\n# Hello\n\nIntro text about deployment.\n")
	e.put("docs/guide/setup.md", "# Setup guide\n\nInstall the thing and configure deployment.\n")
	e.put("docs/guide/draft.md", "---\ntitle: Secret\nstatus: draft\n---\nnot ready\n")
	e.put("docs/guide/wip.md", "---\nstatus: wip\n---\nunknown status\n")
	e.put("docs/notes.txt", "not markdown")
	e.put("src/outside.md", "# Outside the docs root\n")
}

func mustGet(t *testing.T, m *Manager, repoID string) View {
	t.Helper()
	view, err := m.Get(context.Background(), repoID)
	if err != nil {
		t.Fatalf("Get(%s): %v", repoID, err)
	}
	return view
}

func TestSnapshotContainsOnlyVisibleMarkdownUnderRoots(t *testing.T) {
	e := newEnv(t, nil)
	e.seed()

	view := mustGet(t, e.manager, "docs")
	var got []string
	for _, doc := range view.Docs {
		got = append(got, doc.Path)
	}
	if strings.Join(got, ",") != "docs/README.md,docs/guide/setup.md" {
		t.Errorf("docs = %v", got)
	}
	if view.Hidden != 2 {
		t.Errorf("hidden = %d, want the draft and the unknown status", view.Hidden)
	}
	if view.Stale || view.Branch != "master" {
		t.Errorf("view = %+v", view)
	}

	readme, ok := view.Doc("docs/README.md")
	if !ok || readme.Title != "Welcome" || readme.Type != "guide" || readme.Root != "docs" || readme.Repo != "docs" {
		t.Errorf("readme = %+v", readme)
	}
	if !strings.HasPrefix(readme.Raw, "---\ntitle: Welcome") || strings.Contains(readme.Body, "title: Welcome") {
		t.Errorf("raw and body must differ by the front matter: raw=%q body=%q", readme.Raw, readme.Body)
	}
	if setup, _ := view.Doc("docs/guide/setup.md"); setup == nil || setup.Title != "Setup guide" {
		t.Errorf("setup = %+v", setup)
	}
	for _, hiddenPath := range []string{"docs/guide/draft.md", "docs/guide/wip.md", "docs/notes.txt", "src/outside.md"} {
		if _, ok := view.Doc(hiddenPath); ok {
			t.Errorf("%s must not be reachable", hiddenPath)
		}
	}
}

func TestBodyWithBrokenFrontMatterIsStillListed(t *testing.T) {
	e := newEnv(t, nil)
	e.put("docs/broken.md", "---\ntitle: [oops\n---\ntext\n")

	view := mustGet(t, e.manager, "docs")
	doc, ok := view.Doc("docs/broken.md")
	if !ok || len(doc.Warnings) != 1 {
		t.Fatalf("doc = %+v", doc)
	}
}

func TestMultipleRootsAndMissingRoot(t *testing.T) {
	repo := baseRepo()
	repo.Paths = []string{"docs", "specs", "absent"}
	e := newEnv(t, nil, repo)
	e.put("docs/a.md", "# A")
	e.put("specs/b.md", "# B")
	e.put("specs/nested/c.md", "# C")

	view := mustGet(t, e.manager, "docs")
	roots := map[string]string{}
	for _, doc := range view.Docs {
		roots[doc.Path] = doc.Root
	}
	if roots["docs/a.md"] != "docs" || roots["specs/b.md"] != "specs" || roots["specs/nested/c.md"] != "specs" || len(roots) != 3 {
		t.Errorf("roots = %v", roots)
	}
	tree := view.Tree()
	if len(tree) != 3 || tree[2].Name != "absent" || len(tree[2].Children) != 0 {
		t.Errorf("tree = %+v", tree)
	}
}

func TestOverlappingRootsDoNotDuplicateDocuments(t *testing.T) {
	repo := baseRepo()
	repo.Paths = []string{"docs", "docs/api"}
	e := newEnv(t, nil, repo)
	e.put("docs/api/x.md", "# X")

	view := mustGet(t, e.manager, "docs")
	if len(view.Docs) != 1 || view.Docs[0].Root != "docs" {
		t.Errorf("docs = %+v", view.Docs)
	}
}

func TestEmptyRepositoryProducesEmptySnapshot(t *testing.T) {
	e := newEnv(t, nil)
	view := mustGet(t, e.manager, "docs")
	if len(view.Docs) != 0 || view.Hidden != 0 {
		t.Errorf("view = %+v", view)
	}
	if len(view.Tree()) != 1 || len(view.Tree()[0].Children) != 0 {
		t.Errorf("tree = %+v", view.Tree())
	}
}

func TestUnknownRepository(t *testing.T) {
	e := newEnv(t, nil)
	if _, err := e.manager.Get(context.Background(), "nope"); !errors.Is(err, ErrUnknownRepo) {
		t.Errorf("Get err = %v", err)
	}
	if _, err := e.manager.Refresh(context.Background(), "nope"); !errors.Is(err, ErrUnknownRepo) {
		t.Errorf("Refresh err = %v", err)
	}
	if _, err := e.manager.Search(context.Background(), "nope", "x", 5); !errors.Is(err, ErrUnknownRepo) {
		t.Errorf("Search err = %v", err)
	}
	if _, ok := e.manager.Repo("nope"); ok {
		t.Error("Repo must report unknown ids")
	}
	if repo, ok := e.manager.Repo("docs"); !ok || repo.Project != project {
		t.Errorf("repo = %+v", repo)
	}
	if len(e.manager.Repos()) != 1 {
		t.Errorf("repos = %v", e.manager.Repos())
	}
	if e.manager.LastFailure("nope") != nil {
		t.Error("unknown repo has no failure")
	}
}

func TestSnapshotIsReusedWithinTTL(t *testing.T) {
	e := newEnv(t, nil)
	e.seed()

	mustGet(t, e.manager, "docs")
	trees := e.spy.trees.Load()
	e.clock.Advance(59 * time.Second)
	mustGet(t, e.manager, "docs")
	if e.spy.trees.Load() != trees {
		t.Error("a fresh snapshot must be served without calling GitLab")
	}

	e.clock.Advance(2 * time.Second)
	mustGet(t, e.manager, "docs")
	if e.spy.trees.Load() != trees+1 {
		t.Errorf("an expired snapshot must be refreshed once, trees = %d", e.spy.trees.Load())
	}
}

func TestUnchangedBlobsAreNotFetchedAgain(t *testing.T) {
	e := newEnv(t, nil)
	e.seed()
	mustGet(t, e.manager, "docs")
	first := e.spy.blobs.Load()
	if first != 4 {
		t.Fatalf("first refresh fetched %d blobs, want 4 markdown files", first)
	}

	e.clock.Advance(2 * time.Minute)
	mustGet(t, e.manager, "docs")
	if e.spy.blobs.Load() != first {
		t.Errorf("unchanged files were fetched again: %d", e.spy.blobs.Load())
	}

	e.put("docs/guide/setup.md", "# Setup guide v2\n")
	e.clock.Advance(2 * time.Minute)
	view := mustGet(t, e.manager, "docs")
	if e.spy.blobs.Load() != first+1 {
		t.Errorf("exactly the changed file must be fetched, blobs = %d", e.spy.blobs.Load())
	}
	if doc, _ := view.Doc("docs/guide/setup.md"); doc.Body != "# Setup guide v2\n" {
		t.Errorf("body = %q", doc.Body)
	}
}

func TestIdenticalContentIsFetchedOnce(t *testing.T) {
	e := newEnv(t, nil)
	e.put("docs/a.md", "# Same\n")
	e.put("docs/b.md", "# Same\n")
	view := mustGet(t, e.manager, "docs")
	if len(view.Docs) != 2 || e.spy.blobs.Load() != 1 {
		t.Errorf("docs = %d, blobs fetched = %d", len(view.Docs), e.spy.blobs.Load())
	}
}

func TestBlobCacheForgetsRemovedFiles(t *testing.T) {
	e := newEnv(t, nil)
	e.put("docs/a.md", "# A")
	e.put("docs/b.md", "# B")
	mustGet(t, e.manager, "docs")
	if len(e.manager.blobs) != 2 {
		t.Fatalf("cache = %d", len(e.manager.blobs))
	}
	e.fake.DeleteFile(project, "master", "docs/b.md")
	e.clock.Advance(2 * time.Minute)
	mustGet(t, e.manager, "docs")
	if len(e.manager.blobs) != 1 {
		t.Errorf("cache must drop blobs that left the snapshot, has %d", len(e.manager.blobs))
	}
}

func TestInvalidateForcesRefreshWithinTTL(t *testing.T) {
	e := newEnv(t, nil)
	e.put("docs/a.md", "# A")
	mustGet(t, e.manager, "docs")

	e.put("docs/b.md", "# B")
	if view := mustGet(t, e.manager, "docs"); len(view.Docs) != 1 {
		t.Fatal("snapshot should still be served from cache")
	}
	e.manager.Invalidate("docs")
	e.manager.Invalidate("unknown-is-ignored")
	if view := mustGet(t, e.manager, "docs"); len(view.Docs) != 2 {
		t.Errorf("docs after invalidate = %d", len(view.Docs))
	}
}

func TestServesStaleSnapshotWhenGitLabFails(t *testing.T) {
	e := newEnv(t, nil)
	e.put("docs/a.md", "# A")
	mustGet(t, e.manager, "docs")

	e.clock.Advance(2 * time.Minute)
	e.fake.FailNext(1, http.StatusInternalServerError)
	view := mustGet(t, e.manager, "docs")
	if !view.Stale || len(view.Docs) != 1 {
		t.Fatalf("view = stale %v, docs %d", view.Stale, len(view.Docs))
	}
	if e.manager.LastFailure("docs") == nil {
		t.Error("the failure must be recorded")
	}

	trees := e.spy.trees.Load()
	e.clock.Advance(5 * time.Second)
	if view := mustGet(t, e.manager, "docs"); !view.Stale {
		t.Error("still stale during backoff")
	}
	if e.spy.trees.Load() != trees {
		t.Error("GitLab must not be hammered during the failure backoff")
	}

	e.clock.Advance(10 * time.Second)
	view = mustGet(t, e.manager, "docs")
	if view.Stale || e.manager.LastFailure("docs") != nil {
		t.Errorf("recovered view = %+v", view)
	}
}

func TestRefreshBypassesFailureBackoff(t *testing.T) {
	e := newEnv(t, nil)
	e.put("docs/a.md", "# A")
	mustGet(t, e.manager, "docs")
	e.clock.Advance(2 * time.Minute)
	e.fake.FailNext(1, http.StatusInternalServerError)
	mustGet(t, e.manager, "docs")

	e.put("docs/b.md", "# B")
	view, err := e.manager.Refresh(context.Background(), "docs")
	if err != nil || view.Stale || len(view.Docs) != 2 {
		t.Errorf("view = %+v, err = %v", view, err)
	}
}

func TestFailureWithoutSnapshotIsAnError(t *testing.T) {
	e := newEnv(t, nil)
	e.fake.FailNext(1, http.StatusInternalServerError)
	if _, err := e.manager.Get(context.Background(), "docs"); err == nil {
		t.Fatal("expected an error before any snapshot exists")
	}
	if view := mustGet(t, e.manager, "docs"); view.Stale {
		t.Error("a later successful refresh must clear the failure")
	}
}

func TestBlobFailureFailsTheWholeRefresh(t *testing.T) {
	e := newEnv(t, nil)
	e.put("docs/a.md", "# A")
	e.fake.FailNext(0, 0)
	failing := &failingBlobs{GitLab: e.spy}
	e.manager = NewManager(Options{Repos: []Repo{baseRepo()}, Source: failing, Now: e.clock.Now})

	if _, err := e.manager.Get(context.Background(), "docs"); err == nil || !strings.Contains(err.Error(), "fetch blob") {
		t.Errorf("err = %v", err)
	}
}

type failingBlobs struct{ GitLab }

func (f *failingBlobs) Blob(context.Context, string, string) ([]byte, error) {
	return nil, errors.New("boom")
}

func TestConcurrentCallersShareOneRefresh(t *testing.T) {
	e := newEnv(t, nil)
	e.seed()
	e.spy.gate = make(chan struct{})

	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.manager.Get(context.Background(), "docs")
			errs <- err
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(e.spy.gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("Get: %v", err)
		}
	}
	if got := e.spy.trees.Load(); got != 1 {
		t.Errorf("tree requests = %d, concurrent callers must share one refresh", got)
	}
}

func TestCancelledWaiterGetsContextErrorWithoutSnapshot(t *testing.T) {
	e := newEnv(t, nil)
	e.spy.gate = make(chan struct{})
	defer close(e.spy.gate)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := e.manager.Get(ctx, "docs"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v", err)
	}
}

func TestCancelledWaiterGetsStaleSnapshotWhenAvailable(t *testing.T) {
	e := newEnv(t, nil)
	e.put("docs/a.md", "# A")
	mustGet(t, e.manager, "docs")
	e.clock.Advance(2 * time.Minute)
	e.spy.gate = make(chan struct{})
	defer close(e.spy.gate)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	view, err := e.manager.Get(ctx, "docs")
	if err != nil || !view.Stale || len(view.Docs) != 1 {
		t.Errorf("view = %+v, err = %v", view, err)
	}
}

func TestDefaultBranchIsResolvedAndCached(t *testing.T) {
	repo := baseRepo()
	repo.Branch = ""
	e := newEnv(t, nil, repo)
	e.put("docs/a.md", "# A")

	view := mustGet(t, e.manager, "docs")
	if view.Branch != "master" || e.spy.projects.Load() != 1 {
		t.Fatalf("branch = %q, project calls = %d", view.Branch, e.spy.projects.Load())
	}
	e.clock.Advance(2 * time.Minute)
	mustGet(t, e.manager, "docs")
	if e.spy.projects.Load() != 1 {
		t.Error("the default branch must be cached between refreshes")
	}
	e.clock.Advance(11 * time.Minute)
	mustGet(t, e.manager, "docs")
	if e.spy.projects.Load() != 2 {
		t.Errorf("project calls = %d, the cache must expire", e.spy.projects.Load())
	}
}

func TestDefaultBranchLookupFailure(t *testing.T) {
	repo := baseRepo()
	repo.Branch = ""
	repo.Project = "ghost/project"
	e := newEnv(t, nil, repo)
	if _, err := e.manager.Get(context.Background(), "docs"); err == nil || !strings.Contains(err.Error(), "default branch") {
		t.Errorf("err = %v", err)
	}
}

func TestDocumentCapAndOversizedDocuments(t *testing.T) {
	e := newEnv(t, func(o *Options) { o.MaxDocuments = 3; o.MaxDocumentBytes = 50 })
	for i := 0; i < 5; i++ {
		e.put(fmt.Sprintf("docs/d%d.md", i), "# D")
	}
	e.put("docs/d0.md", "# Big\n"+strings.Repeat("x", 100))

	view := mustGet(t, e.manager, "docs")
	if len(view.Docs) != 3 {
		t.Fatalf("docs = %d, want the cap of 3", len(view.Docs))
	}
	big, _ := view.Doc("docs/d0.md")
	if big == nil || big.Body != "" || len(big.Warnings) != 1 || !strings.Contains(big.Warnings[0], "too large") {
		t.Errorf("oversized doc = %+v", big)
	}
}

func TestTreeStructureAndOrdering(t *testing.T) {
	e := newEnv(t, nil)
	e.put("docs/zeta.md", "# Zeta")
	e.put("docs/Alpha.md", "# Alpha")
	e.put("docs/README.md", "# Readme")
	e.put("docs/guide/index.md", "# Guide home")
	e.put("docs/guide/b.md", "# B")
	e.put("docs/guide/deep/x.md", "# X")
	e.put("docs/Archive/old.md", "# Old")

	tree := mustGet(t, e.manager, "docs").Tree()
	if len(tree) != 1 || tree[0].Kind != KindFolder || tree[0].Path != "docs" {
		t.Fatalf("tree = %+v", tree)
	}
	var names []string
	for _, child := range tree[0].Children {
		names = append(names, child.Name)
	}
	if strings.Join(names, ",") != "Archive,guide,README.md,Alpha.md,zeta.md" {
		t.Errorf("top level = %v", names)
	}
	guide := tree[0].Children[1]
	var guideNames []string
	for _, child := range guide.Children {
		guideNames = append(guideNames, child.Name)
	}
	if strings.Join(guideNames, ",") != "deep,index.md,b.md" {
		t.Errorf("guide = %v", guideNames)
	}
	deep := guide.Children[0]
	if deep.Path != "docs/guide/deep" || deep.Children[0].Path != "docs/guide/deep/x.md" || deep.Children[0].Title != "X" {
		t.Errorf("deep = %+v", deep)
	}
}

func TestSearchRankingAndSemantics(t *testing.T) {
	e := newEnv(t, nil)
	e.put("docs/deploy.md", "---\ntitle: Deployment\n---\nHow to ship releases.\n")
	e.put("docs/ops.md", "# Operations\n\nDeployment happens nightly. Deployment is automatic.\n")
	e.put("docs/unrelated.md", "# Cooking\n\nPasta recipes.\n")
	e.put("docs/русский.md", "# Руководство\n\nКак развернуть сервис в продакшене.\n")
	view := mustGet(t, e.manager, "docs")

	results := view.Search("deployment", 10)
	if len(results) != 2 || results[0].Path != "docs/deploy.md" || results[1].Path != "docs/ops.md" {
		t.Fatalf("results = %+v", results)
	}
	if results[1].Snippet == "" || !strings.Contains(strings.ToLower(results[1].Snippet), "deployment") {
		t.Errorf("snippet = %q", results[1].Snippet)
	}
	if results[0].Score <= results[1].Score {
		t.Errorf("title matches must outrank body matches: %+v", results)
	}

	if got := view.Search("deployment nightly", 10); len(got) != 1 || got[0].Path != "docs/ops.md" {
		t.Errorf("AND search = %+v", got)
	}
	if got := view.Search("deployment pasta", 10); len(got) != 0 {
		t.Errorf("all terms must match: %+v", got)
	}
	if got := view.Search("DEPLOYMENT", 1); len(got) != 1 {
		t.Errorf("limit/case = %+v", got)
	}
	if got := view.Search("   ", 10); got != nil {
		t.Errorf("blank query = %+v", got)
	}
	if got := view.Search("развернуть", 10); len(got) != 1 || !strings.Contains(got[0].Snippet, "развернуть") {
		t.Errorf("unicode search = %+v", got)
	}
	if got := view.Search("ops", 10); len(got) != 1 || got[0].Path != "docs/ops.md" {
		t.Errorf("path match = %+v", got)
	}
}

func TestSnippetTrimsAndMarksEllipses(t *testing.T) {
	long := strings.Repeat("filler words ", 30) + "NEEDLE" + strings.Repeat(" more filler", 30)
	snippet := snippetAround(long, strings.Index(long, "NEEDLE"), len("NEEDLE"))
	if !strings.HasPrefix(snippet, "…") || !strings.HasSuffix(snippet, "…") || !strings.Contains(snippet, "NEEDLE") {
		t.Errorf("snippet = %q", snippet)
	}
	if len(snippet) > 200 {
		t.Errorf("snippet too long: %d", len(snippet))
	}
	if got := snippetAround("short text", 6, 4); got != "short text" {
		t.Errorf("short = %q", got)
	}
	multibyte := strings.Repeat("я", 100) + "x" + strings.Repeat("я", 100)
	if got := snippetAround(multibyte, strings.Index(multibyte, "x"), 1); !strings.Contains(got, "x") {
		t.Errorf("multibyte = %q", got)
	}
}

func TestSearchAcrossRepositories(t *testing.T) {
	second := Repo{ID: "other", Name: "Other", Project: "group/other", Branch: "master", Paths: []string{"docs"}}
	e := newEnv(t, nil, baseRepo(), second)
	e.fake.AddProject("group/other", "master", fakegitlab.ProjectOptions{})
	e.put("docs/a.md", "# Alpha release\n")
	e.fake.PutFile("group/other", "master", "docs/b.md", "# Beta release\nrelease notes release\n")

	results, err := e.manager.Search(context.Background(), "", "release", 10)
	if err != nil || len(results) != 2 {
		t.Fatalf("results = %+v, err = %v", results, err)
	}
	if results[0].Repo != "other" {
		t.Errorf("more relevant document first: %+v", results)
	}
	if limited, _ := e.manager.Search(context.Background(), "", "release", 1); len(limited) != 1 {
		t.Errorf("limit = %d", len(limited))
	}
	scoped, err := e.manager.Search(context.Background(), "docs", "release", 10)
	if err != nil || len(scoped) != 1 || scoped[0].Repo != "docs" {
		t.Errorf("scoped = %+v, err = %v", scoped, err)
	}
}

func TestSearchSkipsBrokenRepositoryButReportsScopedFailure(t *testing.T) {
	broken := Repo{ID: "broken", Project: "ghost/none", Branch: "master", Paths: []string{"docs"}}
	e := newEnv(t, nil, baseRepo(), broken)
	e.put("docs/a.md", "# Needle\n")

	results, err := e.manager.Search(context.Background(), "", "needle", 5)
	if err != nil || len(results) != 1 {
		t.Errorf("results = %+v, err = %v", results, err)
	}
	if _, err := e.manager.Search(context.Background(), "broken", "needle", 5); err == nil {
		t.Error("a scoped search must report the failure")
	}
}

func TestSnapshotIsImmutableAcrossRefreshes(t *testing.T) {
	e := newEnv(t, nil)
	e.put("docs/a.md", "# A")
	old := mustGet(t, e.manager, "docs").Snapshot

	e.put("docs/b.md", "# B")
	e.manager.Invalidate("docs")
	fresh := mustGet(t, e.manager, "docs").Snapshot
	if len(old.Docs) != 1 || len(fresh.Docs) != 2 {
		t.Errorf("old = %d, fresh = %d", len(old.Docs), len(fresh.Docs))
	}
	if _, ok := old.Doc("docs/b.md"); ok {
		t.Error("an earlier snapshot must not change")
	}
}

func TestDocStatusValuesAreOnlyPublishedInSnapshots(t *testing.T) {
	e := newEnv(t, nil)
	e.seed()
	for _, doc := range mustGet(t, e.manager, "docs").Docs {
		if doc.Status != docs.StatusPublished {
			t.Errorf("%s has status %s", doc.Path, doc.Status)
		}
	}
}

func TestMissingBranchIsAnErrorNotAnEmptyRepository(t *testing.T) {
	repo := baseRepo()
	repo.Branch = "release"
	e := newEnv(t, nil, repo)
	e.put("docs/a.md", "# A")

	_, err := e.manager.Get(context.Background(), "docs")
	if !errors.Is(err, gitlab.ErrNotFound) || !strings.Contains(err.Error(), "branch release") {
		t.Errorf("err = %v", err)
	}
}

func TestMissingProjectIsAnErrorNotAnEmptyRepository(t *testing.T) {
	repo := baseRepo()
	repo.Project = "ghost/none"
	e := newEnv(t, nil, repo)

	_, err := e.manager.Get(context.Background(), "docs")
	if !errors.Is(err, gitlab.ErrNotFound) || !strings.Contains(err.Error(), "project ghost/none") {
		t.Errorf("err = %v", err)
	}
}

func TestMissingDocsFolderInExistingRepositoryIsEmptyNotAnError(t *testing.T) {
	e := newEnv(t, nil)
	e.put("README.md", "# root only")
	view := mustGet(t, e.manager, "docs")
	if len(view.Docs) != 0 {
		t.Errorf("docs = %d", len(view.Docs))
	}
}

func TestHiddenDocumentsAreNotFetchedRepeatedly(t *testing.T) {
	e := newEnv(t, nil)
	e.put("docs/draft.md", "---\nstatus: draft\n---\nx")
	mustGet(t, e.manager, "docs")
	before := e.spy.blobs.Load()
	e.clock.Advance(2 * time.Minute)
	mustGet(t, e.manager, "docs")
	if e.spy.blobs.Load() != before {
		t.Errorf("a hidden draft was downloaded again: %d -> %d", before, e.spy.blobs.Load())
	}
}

func TestInvalidateProjectMatchesProjectAndBranch(t *testing.T) {
	second := Repo{ID: "other", Project: "group/other", Branch: "master", Paths: []string{"docs"}}
	e := newEnv(t, nil, baseRepo(), second)
	e.put("docs/a.md", "# A")
	mustGet(t, e.manager, "docs")

	if n := e.manager.InvalidateProject("group/unknown", "master"); n != 0 {
		t.Errorf("unknown project matched %d repositories", n)
	}
	if n := e.manager.InvalidateProject(project, "release"); n != 0 {
		t.Errorf("a push to another branch must not invalidate: %d", n)
	}
	if n := e.manager.InvalidateProject(project, "master"); n != 1 {
		t.Errorf("matched = %d", n)
	}
	e.put("docs/b.md", "# B")
	if view := mustGet(t, e.manager, "docs"); len(view.Docs) != 2 {
		t.Errorf("docs = %d, the invalidation must force a refresh", len(view.Docs))
	}
	if n := e.manager.InvalidateProject("group/other", "anything"); n != 1 {
		t.Errorf("a repository without a snapshot is invalidated for any branch: %d", n)
	}
}
