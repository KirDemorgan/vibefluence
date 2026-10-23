package portal

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vibefluence/internal/docs"
	"vibefluence/internal/drafts"
	"vibefluence/internal/gitlab"
	"vibefluence/internal/index"
	"vibefluence/internal/publish"
	"vibefluence/internal/testing/fakegitlab"
)

type fakePublisher struct {
	requests []publish.Request
	result   publish.Result
	err      error
}

func (f *fakePublisher) Publish(_ context.Context, request publish.Request) (publish.Result, error) {
	f.requests = append(f.requests, request)
	return f.result, f.err
}

type writeEnv struct {
	*env
	store     *drafts.Store
	publisher *fakePublisher
}

func newWriteEnv(t *testing.T, autoMerge bool) *writeEnv {
	t.Helper()
	fake := fakegitlab.New(token)
	fake.AddProject(project, "master", fakegitlab.ProjectOptions{})
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	client, err := gitlab.New(gitlab.Options{BaseURL: server.URL, Token: token, MaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	store, err := drafts.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	publisher := &fakePublisher{result: publish.Result{Outcome: publish.OutcomeMerged, MergeRequestURL: "https://gl/mr/1", MergeRequestIID: 1, Created: true}}
	repos := []index.Repo{
		{ID: "docs", Name: "Docs", Project: project, Branch: "master", Paths: []string{"docs", "specs"}},
		{ID: "other", Name: "Other", Project: "group/other", Branch: "main", Paths: []string{"docs"}},
	}
	manager := index.NewManager(index.Options{Repos: repos, Source: client, TTL: time.Minute})
	portal := New(Options{
		Index: manager, Files: client, GitLabURL: "https://gitlab.example.com",
		Settings: map[string]RepoSettings{"docs": {AutoMerge: autoMerge}},
		Drafts:   store, Publisher: publisher,
	})
	return &writeEnv{env: &env{fake: fake, files: &countingFiles{inner: client}, portal: portal, server: server}, store: store, publisher: publisher}
}

func save(t *testing.T, e *writeEnv, path, content string) DraftView {
	t.Helper()
	view, err := e.portal.SaveDraft(context.Background(), "docs", SaveDraftInput{Path: path, Content: content, Author: "Alex"})
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func TestDraftRoundTrip(t *testing.T) {
	e := newWriteEnv(t, true)
	saved := save(t, e, "docs/plans/rollout.md", "---\ntitle: Rollout\nstatus: draft\n---\n# Rollout\n")
	if saved.ID == "" || saved.Title != "Rollout" || saved.Status != docs.StatusDraft || !saved.IsNew || saved.Author != "Alex" {
		t.Fatalf("saved = %+v", saved)
	}

	loaded, err := e.portal.GetDraft(context.Background(), "docs", saved.ID)
	if err != nil || loaded.Content != saved.Content || loaded.Path != "docs/plans/rollout.md" {
		t.Fatalf("loaded = %+v, err = %v", loaded, err)
	}
	listed, err := e.portal.ListDrafts(context.Background(), "docs")
	if err != nil || len(listed) != 1 || listed[0].ID != saved.ID {
		t.Fatalf("listed = %+v, err = %v", listed, err)
	}

	if err := e.portal.DeleteDraft(context.Background(), "docs", saved.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.portal.GetDraft(context.Background(), "docs", saved.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete: %v", err)
	}
}

func TestSavingADraftOfAnExistingDocumentKeepsItsBase(t *testing.T) {
	e := newWriteEnv(t, true)
	view, err := e.portal.SaveDraft(context.Background(), "docs", SaveDraftInput{Path: "docs/a.md", Content: "# A", BaseBlobID: "blob1"})
	if err != nil || view.IsNew || view.BaseBlobID != "blob1" {
		t.Errorf("view = %+v, err = %v", view, err)
	}
}

func TestDraftPathsMustBeInsideTheDocumentationFolders(t *testing.T) {
	e := newWriteEnv(t, true)
	for _, path := range []string{"src/a.md", "docs/a.txt", "docs/../a.md", "", "/docs/a.md", "docs/.hidden/a.md"} {
		_, err := e.portal.SaveDraft(context.Background(), "docs", SaveDraftInput{Path: path, Content: "x"})
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: err = %v", path, err)
		}
	}
	if _, err := e.portal.SaveDraft(context.Background(), "docs", SaveDraftInput{Path: "specs/a.md", Content: "x"}); err != nil {
		t.Errorf("the second root must be accepted: %v", err)
	}
}

func TestDraftContentIsValidated(t *testing.T) {
	e := newWriteEnv(t, true)
	for _, content := range []string{"a\x00b", "bad \xff", strings.Repeat("x", drafts.MaxContentBytes+1)} {
		if _, err := e.portal.SaveDraft(context.Background(), "docs", SaveDraftInput{Path: "docs/a.md", Content: content}); !errors.Is(err, ErrInvalid) {
			t.Errorf("err = %v", err)
		}
	}
}

func TestDraftOperationsOnUnknownThings(t *testing.T) {
	e := newWriteEnv(t, true)
	ctx := context.Background()
	if _, err := e.portal.SaveDraft(ctx, "nope", SaveDraftInput{Path: "docs/a.md"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("SaveDraft: %v", err)
	}
	if _, err := e.portal.ListDrafts(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ListDrafts: %v", err)
	}
	if _, err := e.portal.GetDraft(ctx, "nope", "0123456789abcdef"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDraft repo: %v", err)
	}
	if _, err := e.portal.GetDraft(ctx, "docs", "0123456789abcdef"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDraft draft: %v", err)
	}
	if _, err := e.portal.GetDraft(ctx, "docs", "../../etc"); !errors.Is(err, ErrInvalid) {
		t.Errorf("GetDraft traversal: %v", err)
	}
	if err := e.portal.DeleteDraft(ctx, "nope", "0123456789abcdef"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteDraft repo: %v", err)
	}
	if err := e.portal.DeleteDraft(ctx, "docs", "0123456789abcdef"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteDraft draft: %v", err)
	}
	if _, err := e.portal.PublishDraft(ctx, "nope", "0123456789abcdef", PublishInput{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("PublishDraft repo: %v", err)
	}
	if _, err := e.portal.PublishDraft(ctx, "docs", "0123456789abcdef", PublishInput{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("PublishDraft draft: %v", err)
	}
}

func TestAllDraftsSpansRepositoriesNewestFirst(t *testing.T) {
	e := newWriteEnv(t, true)
	save(t, e, "docs/first.md", "# First")
	time.Sleep(5 * time.Millisecond)
	if _, err := e.portal.SaveDraft(context.Background(), "other", SaveDraftInput{Path: "docs/second.md", Content: "# Second"}); err != nil {
		t.Fatal(err)
	}
	all, err := e.portal.AllDrafts(context.Background())
	if err != nil || len(all) != 2 || all[0].Path != "docs/second.md" || all[0].Repo != "other" || all[1].Repo != "docs" {
		t.Errorf("all = %+v, err = %v", all, err)
	}
}

func TestWithoutADraftStoreListingIsEmptyAndWritingIsRefused(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if list, err := e.portal.ListDrafts(ctx, "docs"); err != nil || len(list) != 0 {
		t.Errorf("list = %v, err = %v", list, err)
	}
	if all, err := e.portal.AllDrafts(ctx); err != nil || len(all) != 0 {
		t.Errorf("all = %v, err = %v", all, err)
	}
	if _, err := e.portal.SaveDraft(ctx, "docs", SaveDraftInput{Path: "docs/a.md"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("SaveDraft: %v", err)
	}
	if _, err := e.portal.GetDraft(ctx, "docs", "0123456789abcdef"); !errors.Is(err, ErrInvalid) {
		t.Errorf("GetDraft: %v", err)
	}
	if err := e.portal.DeleteDraft(ctx, "docs", "0123456789abcdef"); !errors.Is(err, ErrInvalid) {
		t.Errorf("DeleteDraft: %v", err)
	}
	if _, err := e.portal.PublishDraft(ctx, "docs", "0123456789abcdef", PublishInput{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("PublishDraft: %v", err)
	}
}

func TestPublishingAMergedDraftRemovesIt(t *testing.T) {
	e := newWriteEnv(t, true)
	e.fake.PutFile(project, "master", "docs/README.md", "# Readme")
	draft, err := e.portal.SaveDraft(context.Background(), "docs", SaveDraftInput{Path: "docs/a.md", Content: "# A", BaseBlobID: "base", Author: "Alex"})
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := e.portal.PublishDraft(context.Background(), "docs", draft.ID, PublishInput{})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Outcome != publish.OutcomeMerged || !outcome.DraftRemoved || outcome.MergeRequestURL != "https://gl/mr/1" || !outcome.Created || outcome.Path != "docs/a.md" {
		t.Errorf("outcome = %+v", outcome)
	}
	if left, _ := e.portal.ListDrafts(context.Background(), "docs"); len(left) != 0 {
		t.Errorf("drafts left = %+v", left)
	}

	request := e.publisher.requests[0]
	if request.Project != project || request.Branch != "master" || request.Path != "docs/a.md" || request.BaseBlobID != "base" ||
		request.Author != "Alex" || !request.AutoMerge || request.Force || strings.Join(request.Roots, ",") != "docs,specs" {
		t.Errorf("request = %+v", request)
	}
}

func TestPublishPassesTheOverridesAndTheRepositorySetting(t *testing.T) {
	e := newWriteEnv(t, false)
	draft := save(t, e, "docs/a.md", "# A")
	if _, err := e.portal.PublishDraft(context.Background(), "docs", draft.ID, PublishInput{Author: "Sam", Force: true}); err != nil {
		t.Fatal(err)
	}
	request := e.publisher.requests[0]
	if request.Author != "Sam" || !request.Force || request.AutoMerge {
		t.Errorf("request = %+v", request)
	}
}

func TestAPendingPublishKeepsTheDraftAndRemembersTheMergeRequest(t *testing.T) {
	e := newWriteEnv(t, true)
	e.publisher.result = publish.Result{
		Outcome: publish.OutcomePending, Reason: publish.ReasonNotMergeable, Detail: "ci_must_pass",
		MergeRequestURL: "https://gl/mr/9", MergeRequestIID: 9,
	}
	draft := save(t, e, "docs/a.md", "# A")

	outcome, err := e.portal.PublishDraft(context.Background(), "docs", draft.ID, PublishInput{})
	if err != nil || outcome.Outcome != publish.OutcomePending || outcome.DraftRemoved || outcome.Reason != publish.ReasonNotMergeable || outcome.Detail != "ci_must_pass" {
		t.Fatalf("outcome = %+v, err = %v", outcome, err)
	}
	kept, err := e.portal.GetDraft(context.Background(), "docs", draft.ID)
	if err != nil || kept.MergeRequestURL != "https://gl/mr/9" || kept.Content != draft.Content {
		t.Errorf("kept = %+v, err = %v", kept, err)
	}

	edited, err := e.portal.SaveDraft(context.Background(), "docs", SaveDraftInput{Path: "docs/a.md", Content: "# A edited"})
	if err != nil || edited.MergeRequestURL != "https://gl/mr/9" {
		t.Errorf("editing must keep the merge request link: %+v, err = %v", edited, err)
	}
}

func TestPublishErrorsAreTranslated(t *testing.T) {
	e := newWriteEnv(t, true)
	draft := save(t, e, "docs/a.md", "# A")
	ctx := context.Background()

	e.publisher.err = &publish.ConflictError{Exists: true, CurrentBlobID: "cur"}
	_, err := e.portal.PublishDraft(ctx, "docs", draft.ID, PublishInput{})
	var conflict *ConflictError
	if !errors.Is(err, ErrConflict) || !errors.As(err, &conflict) || !conflict.Exists || conflict.CurrentBlobID != "cur" || conflict.Message == "" {
		t.Errorf("conflict: %v", err)
	}

	e.publisher.err = &publish.ValidationError{Message: "bad path"}
	if _, err := e.portal.PublishDraft(ctx, "docs", draft.ID, PublishInput{}); !errors.Is(err, ErrInvalid) || err.Error() != "bad path" {
		t.Errorf("validation: %v", err)
	}

	e.publisher.err = publish.ErrNoChanges
	if _, err := e.portal.PublishDraft(ctx, "docs", draft.ID, PublishInput{}); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "nothing to publish") {
		t.Errorf("no changes: %v", err)
	}

	e.publisher.err = publish.ErrBusy
	if _, err := e.portal.PublishDraft(ctx, "docs", draft.ID, PublishInput{}); !errors.Is(err, ErrBusy) {
		t.Errorf("busy: %v", err)
	}

	boom := errors.New("boom")
	e.publisher.err = boom
	if _, err := e.portal.PublishDraft(ctx, "docs", draft.ID, PublishInput{}); !errors.Is(err, boom) {
		t.Errorf("other: %v", err)
	}
	if kept, _ := e.portal.ListDrafts(ctx, "docs"); len(kept) != 1 {
		t.Error("a failed publish must not delete the draft")
	}
}

func TestPublishingWithoutAPublisherIsRefused(t *testing.T) {
	e := newWriteEnv(t, true)
	draft := save(t, e, "docs/a.md", "# A")
	e.portal.publisher = nil
	if _, err := e.portal.PublishDraft(context.Background(), "docs", draft.ID, PublishInput{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v", err)
	}
}

func TestPreviewRendersWithoutStoring(t *testing.T) {
	e := newWriteEnv(t, true)
	preview, err := e.portal.Preview(context.Background(), "docs", "docs/guide/new.md",
		"---\ntitle: Fresh\ntype: guide\nstatus: draft\n---\n# Hello\n\n[other](other.md) ![pic](p.png)\n")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Title != "Fresh" || preview.Type != "guide" || preview.Status != docs.StatusDraft || len(preview.Headings) != 1 {
		t.Errorf("preview = %+v", preview)
	}
	for _, want := range []string{`href="/r/docs/docs/guide/other.md"`, `src="/api/v1/repos/docs/raw?path=docs%2Fguide%2Fp.png"`} {
		if !strings.Contains(preview.HTML, want) {
			t.Errorf("html lacks %q: %s", want, preview.HTML)
		}
	}
	if drafts, _ := e.portal.ListDrafts(context.Background(), "docs"); len(drafts) != 0 {
		t.Error("a preview must not store anything")
	}
}

func TestPreviewWithoutAPathUsesTheFirstRoot(t *testing.T) {
	e := newWriteEnv(t, true)
	preview, err := e.portal.Preview(context.Background(), "docs", "", "[a](a.md)")
	if err != nil || !strings.Contains(preview.HTML, `href="/r/docs/docs/a.md"`) {
		t.Errorf("preview = %+v, err = %v", preview, err)
	}
	long := strings.Repeat("x", maxImagePathBytes+1)
	if _, err := e.portal.Preview(context.Background(), "docs", long, "x"); err != nil {
		t.Errorf("an absurd path is replaced, not rejected: %v", err)
	}
}

func TestPreviewReportsProblemsInTheDocument(t *testing.T) {
	e := newWriteEnv(t, true)
	preview, err := e.portal.Preview(context.Background(), "docs", "docs/a.md", "---\ntitle: [oops\n---\nbody")
	if err != nil || len(preview.Warnings) == 0 {
		t.Errorf("preview = %+v, err = %v", preview, err)
	}
}

func TestPreviewLimits(t *testing.T) {
	e := newWriteEnv(t, true)
	if _, err := e.portal.Preview(context.Background(), "docs", "docs/a.md", strings.Repeat("x", drafts.MaxContentBytes+1)); !errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v", err)
	}
	if _, err := e.portal.Preview(context.Background(), "nope", "docs/a.md", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown repo err = %v", err)
	}
}

func TestPreviewSanitisesHostileMarkdown(t *testing.T) {
	e := newWriteEnv(t, true)
	preview, err := e.portal.Preview(context.Background(), "docs", "docs/a.md", "<script>alert(1)</script>[x](javascript:alert(1))")
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(preview.HTML)
	if strings.Contains(lower, "<script") || strings.Contains(lower, `href="javascript`) {
		t.Errorf("html = %s", preview.HTML)
	}
}

func TestFirstRootFallback(t *testing.T) {
	if got := firstRoot(index.Repo{}); got != "docs" {
		t.Errorf("got %q", got)
	}
	if got := firstRoot(index.Repo{Paths: []string{"specs"}}); got != "specs" {
		t.Errorf("got %q", got)
	}
}

func TestAReadOnlyPortalRefusesEveryWrite(t *testing.T) {
	e := newWriteEnv(t, true)
	readOnly := New(Options{Index: e.portal.index, Files: e.portal.files, Drafts: e.store, Publisher: e.publisher, ReadOnly: true})
	ctx := context.Background()

	if _, err := readOnly.SaveDraft(ctx, "docs", SaveDraftInput{Path: "docs/a.md", Content: "# A"}); !errors.Is(err, ErrReadOnly) {
		t.Errorf("save err = %v", err)
	}
	if err := readOnly.DeleteDraft(ctx, "docs", "0123456789abcdef"); !errors.Is(err, ErrReadOnly) {
		t.Errorf("delete err = %v", err)
	}
	if _, err := readOnly.PublishDraft(ctx, "docs", "0123456789abcdef", PublishInput{}); !errors.Is(err, ErrReadOnly) {
		t.Errorf("publish err = %v", err)
	}
	if len(e.publisher.requests) != 0 {
		t.Error("nothing may reach the publisher")
	}
	for _, summary := range readOnly.Repos(ctx) {
		if !summary.ReadOnly {
			t.Errorf("%s must be reported as read-only", summary.ID)
		}
	}
}
