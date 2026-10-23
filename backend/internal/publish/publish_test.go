package publish

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
	*gitlab.Client
	mu             sync.Mutex
	commits        []gitlab.CommitInput
	mergeRequests  []gitlab.MergeRequestInput
	deleted        []string
	afterMR        func()
	failMR         error
	failCommit     error
	failPipelines  error
	failBranchPipe error
	mergeOverride  func(real func() (gitlab.MergeRequest, error)) (gitlab.MergeRequest, error)
	getOverride    func(iid int) (gitlab.MergeRequest, bool)
	gate           chan struct{}
	entered        chan struct{}
}

func (s *spy) CreateCommit(ctx context.Context, name string, input gitlab.CommitInput) (gitlab.Commit, error) {
	s.mu.Lock()
	s.commits = append(s.commits, input)
	s.mu.Unlock()
	if s.entered != nil {
		s.entered <- struct{}{}
	}
	if s.gate != nil {
		<-s.gate
	}
	if s.failCommit != nil {
		return gitlab.Commit{}, s.failCommit
	}
	return s.Client.CreateCommit(ctx, name, input)
}

func (s *spy) CreateMergeRequest(ctx context.Context, name string, input gitlab.MergeRequestInput) (gitlab.MergeRequest, error) {
	s.mu.Lock()
	s.mergeRequests = append(s.mergeRequests, input)
	s.mu.Unlock()
	if s.failMR != nil {
		return gitlab.MergeRequest{}, s.failMR
	}
	mr, err := s.Client.CreateMergeRequest(ctx, name, input)
	if err == nil && s.afterMR != nil {
		s.afterMR()
	}
	return mr, err
}

func (s *spy) GetMergeRequest(ctx context.Context, name string, iid int) (gitlab.MergeRequest, error) {
	if s.getOverride != nil {
		if mr, ok := s.getOverride(iid); ok {
			return mr, nil
		}
	}
	return s.Client.GetMergeRequest(ctx, name, iid)
}

func (s *spy) MergeMergeRequest(ctx context.Context, name string, iid int, options gitlab.MergeOptions) (gitlab.MergeRequest, error) {
	real := func() (gitlab.MergeRequest, error) { return s.Client.MergeMergeRequest(ctx, name, iid, options) }
	if s.mergeOverride != nil {
		return s.mergeOverride(real)
	}
	return real()
}

func (s *spy) MergeRequestPipelines(ctx context.Context, name string, iid int) ([]gitlab.Pipeline, error) {
	if s.failPipelines != nil {
		return nil, s.failPipelines
	}
	return s.Client.MergeRequestPipelines(ctx, name, iid)
}

func (s *spy) BranchPipelines(ctx context.Context, name, ref string) ([]gitlab.Pipeline, error) {
	if s.failBranchPipe != nil {
		return nil, s.failBranchPipe
	}
	return s.Client.BranchPipelines(ctx, name, ref)
}

func (s *spy) DeleteBranch(ctx context.Context, name, branch string) error {
	s.mu.Lock()
	s.deleted = append(s.deleted, branch)
	s.mu.Unlock()
	return s.Client.DeleteBranch(ctx, name, branch)
}

type env struct {
	mu          sync.Mutex
	fake        *fakegitlab.Server
	spy         *spy
	publisher   *Publisher
	sleeps      []time.Duration
	invalidated []string
}

func newEnv(t *testing.T, options fakegitlab.ProjectOptions) *env {
	t.Helper()
	fake := fakegitlab.New(token)
	fake.AddProject(project, "master", options)
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	client, err := gitlab.New(gitlab.Options{BaseURL: server.URL, Token: token, MaxRetries: -1})
	if err != nil {
		t.Fatal(err)
	}
	e := &env{fake: fake, spy: &spy{Client: client}}
	e.publisher = New(Options{
		Client: e.spy,
		Invalidate: func(p, b string) int {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.invalidated = append(e.invalidated, p+"@"+b)
			return 1
		},
		Sleep: func(_ context.Context, d time.Duration) error {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.sleeps = append(e.sleeps, d)
			return nil
		},
		NewSuffix: func() string { return "abc123" },
	})
	return e
}

func (e *env) request(path, content string) Request {
	return Request{Project: project, Branch: "master", Roots: []string{"docs"}, Path: path, Content: content, Author: "Alex", AutoMerge: true}
}

func (e *env) blob(t *testing.T, path string) string {
	t.Helper()
	file, err := e.spy.File(context.Background(), project, "master", path)
	if err != nil {
		t.Fatal(err)
	}
	return file.BlobID
}

func TestPublishCreatesADocumentAndMergesIt(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	e.fake.PutFile(project, "master", "docs/existing.md", "# Existing")

	result, err := e.publisher.Publish(context.Background(), e.request("docs/plans/rollout.md", "---\ntitle: Rollout plan\nstatus: draft\n---\n# Rollout\n"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeMerged || result.Reason != ReasonNone || !result.Created || result.MergeCommitSHA == "" {
		t.Fatalf("result = %+v", result)
	}
	if result.SourceBranch != "vibefluence/rollout-plan-abc123" || result.MergeRequestIID != 1 || !strings.Contains(result.MergeRequestURL, "/merge_requests/1") {
		t.Errorf("result = %+v", result)
	}

	content, ok := e.fake.File(project, "master", "docs/plans/rollout.md")
	if !ok || !strings.Contains(content, "status: published") || strings.Contains(content, "status: draft") {
		t.Errorf("the published file must carry the published status: %q", content)
	}
	if result.Content != content {
		t.Errorf("the result must report the published content")
	}
	if strings.Join(e.fake.Branches(project), ",") != "master" {
		t.Errorf("the source branch must be removed: %v", e.fake.Branches(project))
	}
	if len(e.invalidated) != 1 || e.invalidated[0] != project+"@master" {
		t.Errorf("invalidated = %v", e.invalidated)
	}
	if infos := e.fake.MergeRequests(project); len(infos) != 1 || infos[0].State != "merged" {
		t.Errorf("merge requests = %+v", infos)
	}
}

func TestPublishRecordsTheAuthorAndUsesPrefixedBranchesAndTitles(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	if _, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", "---\ntitle: Alpha\n---\nbody")); err != nil {
		t.Fatal(err)
	}
	commit := e.spy.commits[0]
	if commit.Branch != "vibefluence/alpha-abc123" || commit.StartBranch != "master" {
		t.Errorf("commit = %+v", commit)
	}
	if commit.Message != "Add docs/a.md\n\nEdited-by: Alex" {
		t.Errorf("message = %q", commit.Message)
	}
	if action := commit.Actions[0]; action.Action != "create" || action.LastCommitID != "" || action.FilePath != "docs/a.md" {
		t.Errorf("action = %+v", action)
	}
	mr := e.spy.mergeRequests[0]
	if mr.Title != "Vibefluence: add Alpha" || !mr.RemoveSourceBranch || mr.TargetBranch != "master" || mr.Description != "Published from Vibefluence by Alex." {
		t.Errorf("merge request = %+v", mr)
	}
}

func TestPublishWithoutAnAuthor(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	request := e.request("docs/a.md", "# A")
	request.Author = " \n "
	if _, err := e.publisher.Publish(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.spy.commits[0].Message, "Edited-by") || e.spy.mergeRequests[0].Description != "Published from Vibefluence." {
		t.Errorf("commit %q, description %q", e.spy.commits[0].Message, e.spy.mergeRequests[0].Description)
	}
}

func TestAHostileAuthorCannotInjectCommitTrailers(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	request := e.request("docs/a.md", "# A")
	request.Author = "Eve\n\nSigned-off-by: Admin <admin@example.com>"
	if _, err := e.publisher.Publish(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	message := e.spy.commits[0].Message
	if strings.Count(message, "\n") != 2 || strings.Contains(message, "\nSigned-off-by") {
		t.Errorf("message = %q", message)
	}
}

func TestBranchNameFallsBackToTheFileNameForUntitledDocuments(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	result, err := e.publisher.Publish(context.Background(), e.request("docs/Release Notes.md", "---\ntitle: \"!!!\"\n---\nx"))
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceBranch != "vibefluence/release-notes-abc123" {
		t.Errorf("branch = %q", result.SourceBranch)
	}
}

func TestPublishUpdatesAnExistingDocument(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	e.fake.PutFile(project, "master", "docs/a.md", "# Old\n")
	request := e.request("docs/a.md", "# New\n")
	request.BaseBlobID = e.blob(t, "docs/a.md")

	result, err := e.publisher.Publish(context.Background(), request)
	if err != nil || result.Outcome != OutcomeMerged || result.Created {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if got, _ := e.fake.File(project, "master", "docs/a.md"); !strings.Contains(got, "# New") {
		t.Errorf("content = %q", got)
	}
	action := e.spy.commits[0].Actions[0]
	if action.Action != "update" || action.LastCommitID == "" || !strings.HasPrefix(e.spy.commits[0].Message, "Update docs/a.md") {
		t.Errorf("commit = %+v", e.spy.commits[0])
	}
	if e.spy.mergeRequests[0].Title != "Vibefluence: update New" {
		t.Errorf("title = %q", e.spy.mergeRequests[0].Title)
	}
}

func TestPublishDetectsAChangedDocument(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	e.fake.PutFile(project, "master", "docs/a.md", "# v1\n")
	request := e.request("docs/a.md", "# mine\n")
	request.BaseBlobID = e.blob(t, "docs/a.md")
	e.fake.PutFile(project, "master", "docs/a.md", "# v2 by someone else\n")

	_, err := e.publisher.Publish(context.Background(), request)
	var conflict *ConflictError
	if !errors.Is(err, ErrConflict) || !errors.As(err, &conflict) || !conflict.Exists || conflict.CurrentBlobID != e.blob(t, "docs/a.md") {
		t.Fatalf("err = %v", err)
	}
	if len(e.spy.commits) != 0 {
		t.Error("nothing may be committed when the document changed")
	}
	if !strings.Contains(conflict.Error(), "changed or created") {
		t.Errorf("message = %q", conflict.Error())
	}
}

func TestForceOverwritesAChangedDocument(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	e.fake.PutFile(project, "master", "docs/a.md", "# v1\n")
	request := e.request("docs/a.md", "# mine\n")
	request.BaseBlobID = e.blob(t, "docs/a.md")
	e.fake.PutFile(project, "master", "docs/a.md", "# v2 by someone else\n")
	request.Force = true

	result, err := e.publisher.Publish(context.Background(), request)
	if err != nil || result.Outcome != OutcomeMerged {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if got, _ := e.fake.File(project, "master", "docs/a.md"); !strings.Contains(got, "# mine") {
		t.Errorf("content = %q", got)
	}
}

func TestANewDocumentCannotOverwriteAnExistingPath(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	e.fake.PutFile(project, "master", "docs/a.md", "# taken\n")

	_, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", "# mine\n"))
	var conflict *ConflictError
	if !errors.As(err, &conflict) || !conflict.Exists {
		t.Fatalf("err = %v", err)
	}
}

func TestAPublishedDraftOfARemovedDocumentConflictsUnlessForced(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	request := e.request("docs/gone.md", "# Mine\n")
	request.BaseBlobID = "0123456789012345678901234567890123456789"

	_, err := e.publisher.Publish(context.Background(), request)
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Exists || !strings.Contains(conflict.Error(), "removed") {
		t.Fatalf("err = %v", err)
	}

	request.Force = true
	result, err := e.publisher.Publish(context.Background(), request)
	if err != nil || !result.Created {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestPublishingIdenticalContentIsRejected(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	e.fake.PutFile(project, "master", "docs/a.md", "---\nstatus: published\n---\n# Same\n")
	request := e.request("docs/a.md", "---\nstatus: draft\n---\n# Same\n")
	request.BaseBlobID = e.blob(t, "docs/a.md")

	if _, err := e.publisher.Publish(context.Background(), request); !errors.Is(err, ErrNoChanges) {
		t.Errorf("err = %v", err)
	}
	if len(e.spy.commits) != 0 {
		t.Error("an unchanged document must not create a branch")
	}
}

func TestPublishValidatesTheRequest(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	tests := map[string]func(*Request){
		"path outside the roots": func(r *Request) { r.Path = "src/a.md" },
		"not markdown":           func(r *Request) { r.Path = "docs/a.txt" },
		"path traversal":         func(r *Request) { r.Path = "docs/../a.md" },
		"hidden segment":         func(r *Request) { r.Path = "docs/.git/a.md" },
		"broken front matter":    func(r *Request) { r.Content = "---\n0\n---\nbody" },
		"binary content":         func(r *Request) { r.Content = "a\x00b" },
		"oversized content":      func(r *Request) { r.Content = strings.Repeat("x", 1<<20+1) },
		"no project":             func(r *Request) { r.Project = "" },
		"no branch":              func(r *Request) { r.Branch = "" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			request := e.request("docs/a.md", "# A")
			mutate(&request)
			_, err := e.publisher.Publish(context.Background(), request)
			var validation *ValidationError
			if !errors.Is(err, ErrInvalid) || !errors.As(err, &validation) || validation.Message == "" {
				t.Errorf("err = %v", err)
			}
		})
	}
	if len(e.spy.commits) != 0 || e.fake.RequestCount() != 0 {
		t.Errorf("invalid requests must not reach GitLab: %d requests", e.fake.RequestCount())
	}
}

func TestWithoutAutoMergeTheMergeRequestStaysOpen(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	request := e.request("docs/a.md", "# A")
	request.AutoMerge = false

	result, err := e.publisher.Publish(context.Background(), request)
	if err != nil || result.Outcome != OutcomePending || result.Reason != ReasonAutoMergeDisabled {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if _, ok := e.fake.File(project, "master", "docs/a.md"); ok {
		t.Error("the document must not reach the default branch")
	}
	infos := e.fake.MergeRequests(project)
	if len(infos) != 1 || infos[0].State != "opened" || result.MergeRequestIID != 1 {
		t.Errorf("merge requests = %+v", infos)
	}
	if len(e.invalidated) != 0 {
		t.Errorf("nothing changed upstream, invalidated = %v", e.invalidated)
	}
}

func TestAPipelineStopsTheAutomaticMerge(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{PipelineBranches: func(string) bool { return true }})
	result, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", "# A"))
	if err != nil || result.Outcome != OutcomePending || result.Reason != ReasonPipelineStarted {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if _, ok := e.fake.File(project, "master", "docs/a.md"); ok {
		t.Error("nothing may be merged when a pipeline exists")
	}
	if e.fake.MergeRequests(project)[0].State != "opened" {
		t.Error("the merge request must stay open")
	}
}

func TestBranchPipelinesAlsoStopTheMerge(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{PipelineBranches: func(b string) bool { return strings.HasPrefix(b, "vibefluence/") }})
	e.spy.mergeOverride = func(func() (gitlab.MergeRequest, error)) (gitlab.MergeRequest, error) {
		t.Error("merge must not be attempted")
		return gitlab.MergeRequest{}, nil
	}
	result, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", "# A"))
	if err != nil || result.Reason != ReasonPipelineStarted {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestPipelineCheckFailuresAreCautious(t *testing.T) {
	for name, configure := range map[string]func(*spy){
		"merge request pipelines": func(s *spy) { s.failPipelines = errors.New("boom") },
		"branch pipelines":        func(s *spy) { s.failBranchPipe = errors.New("boom") },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, fakegitlab.ProjectOptions{})
			configure(e.spy)
			result, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", "# A"))
			if err != nil || result.Outcome != OutcomePending || result.Reason != ReasonPipelineUnknown {
				t.Fatalf("result = %+v, err = %v", result, err)
			}
			if e.fake.MergeRequests(project)[0].State != "opened" {
				t.Error("an unknown pipeline state must not lead to a merge")
			}
		})
	}
}

func TestMergeBlockersAreReportedWithGitLabsReason(t *testing.T) {
	tests := map[string]struct {
		options fakegitlab.ProjectOptions
		status  string
	}{
		"approval required":   {fakegitlab.ProjectOptions{ApprovalRequired: true}, "not_approved"},
		"pipelines must pass": {fakegitlab.ProjectOptions{PipelinesMustSucceed: true}, "ci_must_pass"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, tt.options)
			result, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", "# A"))
			if err != nil || result.Outcome != OutcomePending || result.Reason != ReasonNotMergeable || result.Detail != tt.status {
				t.Fatalf("result = %+v, err = %v", result, err)
			}
			if _, ok := e.fake.File(project, "master", "docs/a.md"); ok {
				t.Error("a blocked merge request must not change the default branch")
			}
			if result.MergeRequestURL == "" {
				t.Error("the merge request link is needed to finish by hand")
			}
		})
	}
}

func TestAConflictDevelopingAfterTheMergeRequestIsReported(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	e.fake.PutFile(project, "master", "docs/a.md", "# v1\n")
	request := e.request("docs/a.md", "# mine\n")
	request.BaseBlobID = e.blob(t, "docs/a.md")
	e.spy.afterMR = func() { e.fake.PutFile(project, "master", "docs/a.md", "# someone else\n") }

	result, err := e.publisher.Publish(context.Background(), request)
	if err != nil || result.Reason != ReasonNotMergeable || result.Detail != "conflict" {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestAForbiddenMergeLeavesTheMergeRequestOpen(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{ForbidMerge: true})
	result, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", "# A"))
	if err != nil || result.Outcome != OutcomePending || result.Reason != ReasonForbidden {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if e.fake.MergeRequests(project)[0].State != "opened" {
		t.Error("the merge request must stay open")
	}
}

func TestAMergeThatFailedButSucceededAnywayIsReportedAsMerged(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	e.spy.mergeOverride = func(real func() (gitlab.MergeRequest, error)) (gitlab.MergeRequest, error) {
		if _, err := real(); err != nil {
			return gitlab.MergeRequest{}, err
		}
		return gitlab.MergeRequest{}, &gitlab.APIError{Status: http.StatusGatewayTimeout}
	}
	result, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", "# A"))
	if err != nil || result.Outcome != OutcomeMerged {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if len(e.invalidated) != 1 {
		t.Errorf("invalidated = %v", e.invalidated)
	}
}

func TestAMergeThatFailedAndDidNotHappenIsPending(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	e.spy.mergeOverride = func(func() (gitlab.MergeRequest, error)) (gitlab.MergeRequest, error) {
		return gitlab.MergeRequest{}, &gitlab.APIError{Status: http.StatusUnprocessableEntity, Message: "Branch cannot be merged"}
	}
	result, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", "# A"))
	if err != nil || result.Outcome != OutcomePending || result.Reason != ReasonRefused || result.Detail == "" {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if len(e.invalidated) != 0 {
		t.Errorf("invalidated = %v", e.invalidated)
	}
}

func TestTheMergeUsesTheHeadShaOfTheBranch(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{RequireMergeSHA: true})
	result, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", "# A"))
	if err != nil || result.Outcome != OutcomeMerged {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestTheStatusIsPolledWhileGitLabIsStillChecking(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	result, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", "# A"))
	if err != nil || result.Outcome != OutcomeMerged {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if len(e.sleeps) < 2 || e.sleeps[0] != defaultPipelineGrace || e.sleeps[1] != defaultStatusInterval {
		t.Errorf("sleeps = %v", e.sleeps)
	}
}

func TestAnEndlessCheckingStateGivesUpWithoutMerging(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	e.spy.getOverride = func(iid int) (gitlab.MergeRequest, bool) {
		return gitlab.MergeRequest{IID: iid, State: "opened", DetailedMergeStatus: "checking"}, true
	}
	e.spy.mergeOverride = func(func() (gitlab.MergeRequest, error)) (gitlab.MergeRequest, error) {
		t.Error("must not merge while checking")
		return gitlab.MergeRequest{}, nil
	}
	result, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", "# A"))
	if err != nil || result.Reason != ReasonNotMergeable || result.Detail != "checking" {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if got := len(e.sleeps); got != 1+defaultStatusPolls-1 {
		t.Errorf("sleeps = %d", got)
	}
}

func TestAMergeRequestMergedByAnotherPartyIsAccepted(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	e.spy.getOverride = func(iid int) (gitlab.MergeRequest, bool) {
		sha := "abc"
		return gitlab.MergeRequest{IID: iid, State: "merged", MergeCommitSHA: &sha, WebURL: "https://gl/mr/1"}, true
	}
	e.spy.mergeOverride = func(func() (gitlab.MergeRequest, error)) (gitlab.MergeRequest, error) {
		return gitlab.MergeRequest{}, &gitlab.APIError{Status: http.StatusMethodNotAllowed}
	}
	result, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", "# A"))
	if err != nil || result.Outcome != OutcomeMerged || result.MergeCommitSHA != "abc" || result.MergeRequestURL != "https://gl/mr/1" {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestACommitFailureIsReturnedAndNothingIsLeftBehind(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	e.spy.failCommit = &gitlab.APIError{Status: http.StatusInternalServerError, Message: "boom"}
	_, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", "# A"))
	var apiErr *gitlab.APIError
	if !errors.As(err, &apiErr) || len(e.spy.mergeRequests) != 0 {
		t.Errorf("err = %v, merge requests = %d", err, len(e.spy.mergeRequests))
	}
}

func TestARaceOnTheCommitIsReportedAsAConflict(t *testing.T) {
	for _, message := range []string{"You are attempting to update a file that has changed since you started editing it: docs/a.md", "A file with this name already exists: docs/a.md"} {
		e := newEnv(t, fakegitlab.ProjectOptions{})
		e.spy.failCommit = &gitlab.APIError{Status: http.StatusBadRequest, Message: message}
		_, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", "# A"))
		if !errors.Is(err, ErrConflict) {
			t.Errorf("%q: err = %v", message, err)
		}
	}
}

func TestAnOrphanBranchIsRemovedWhenTheMergeRequestCannotBeCreated(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	e.spy.failMR = &gitlab.APIError{Status: http.StatusInternalServerError}
	_, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", "# A"))
	if err == nil || !strings.Contains(err.Error(), "merge request") {
		t.Fatalf("err = %v", err)
	}
	if len(e.spy.deleted) != 1 || e.spy.deleted[0] != "vibefluence/a-abc123" {
		t.Errorf("deleted = %v", e.spy.deleted)
	}
	if strings.Join(e.fake.Branches(project), ",") != "master" {
		t.Errorf("branches = %v", e.fake.Branches(project))
	}
}

func TestFailingToRemoveAnOrphanBranchIsOnlyLogged(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	e.spy.failMR = &gitlab.APIError{Status: http.StatusInternalServerError}
	e.fake.FailNext(0, 0)
	e.publisher = New(Options{Client: &brokenDelete{spy: e.spy}, NewSuffix: func() string { return "abc123" }})

	_, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", "# A"))
	if err == nil || !strings.Contains(err.Error(), "merge request") {
		t.Errorf("the original failure must be returned, got %v", err)
	}
}

type brokenDelete struct{ *spy }

func (b *brokenDelete) DeleteBranch(context.Context, string, string) error {
	return errors.New("cannot delete")
}

func TestOnlyOnePublishPerDocumentRunsAtATime(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	e.spy.gate = make(chan struct{})
	e.spy.entered = make(chan struct{}, 4)

	first := make(chan error, 1)
	go func() {
		_, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", "# A"))
		first <- err
	}()
	<-e.spy.entered

	if _, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", "# A again")); !errors.Is(err, ErrBusy) {
		t.Errorf("a concurrent publish of the same document: %v", err)
	}

	other := make(chan error, 1)
	go func() {
		_, err := e.publisher.Publish(context.Background(), e.request("docs/other.md", "# Other"))
		other <- err
	}()
	<-e.spy.entered

	close(e.spy.gate)
	if err := <-first; err != nil {
		t.Errorf("the first publish: %v", err)
	}
	if err := <-other; errors.Is(err, ErrBusy) {
		t.Errorf("other documents must not be blocked: %v", err)
	}
	if _, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", "# A again")); errors.Is(err, ErrBusy) {
		t.Error("the lock must be released afterwards")
	}
}

func TestAClientThatDisconnectsDoesNotAbortAPublishUnderWay(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	e.publisher = New(Options{
		Client:    e.spy,
		NewSuffix: func() string { return "abc123" },
		Sleep: func(ctx context.Context, d time.Duration) error {
			cancel()
			return ctx.Err()
		},
	})
	result, err := e.publisher.Publish(ctx, e.request("docs/a.md", "# A"))
	if err != nil || result.Outcome != OutcomeMerged {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if _, ok := e.fake.File(project, "master", "docs/a.md"); !ok {
		t.Error("the publish must finish after the client left")
	}
}

func TestACancelledContextAbortsBeforeAnythingIsWritten(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := e.publisher.Publish(ctx, e.request("docs/a.md", "# A"))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
	if _, ok := e.fake.File(project, "master", "docs/a.md"); ok {
		t.Error("nothing may be written after cancellation")
	}
}

func TestReadingTheCurrentDocumentCanFail(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	e.fake.FailNext(1, http.StatusInternalServerError)
	_, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", "# A"))
	if err == nil || !strings.Contains(err.Error(), "read the current document") {
		t.Errorf("err = %v", err)
	}
}

func TestTheDefaultsAreApplied(t *testing.T) {
	publisher := New(Options{})
	if publisher.pipelineGrace != defaultPipelineGrace || publisher.statusPolls != defaultStatusPolls || publisher.statusInterval != defaultStatusInterval {
		t.Errorf("defaults = %+v", publisher)
	}
	if publisher.invalidate("a", "b") != 0 || publisher.logger == nil {
		t.Error("the optional collaborators need safe defaults")
	}
	if suffix := publisher.newSuffix(); len(suffix) != 6 || suffix == publisher.newSuffix() {
		t.Errorf("suffix = %q", suffix)
	}
	if err := publisher.sleep(context.Background(), time.Millisecond); err != nil {
		t.Errorf("sleep: %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := publisher.sleep(cancelled, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled sleep: %v", err)
	}
}

func TestBranchNamesAreSafeForHostileTitles(t *testing.T) {
	publisher := New(Options{NewSuffix: func() string { return "s" }})
	for _, title := range []string{"../../etc", "a b/c", "--force", "refs/heads/master", "ёж", "\x00"} {
		name := publisher.branchName(title, "docs/x.md")
		if !strings.HasPrefix(name, BranchPrefix) || strings.Contains(strings.TrimPrefix(name, BranchPrefix), "/") || strings.Contains(name, "..") || strings.HasSuffix(name, ".lock") {
			t.Errorf("branchName(%q) = %q", title, name)
		}
	}
}

func TestPublishedContentAlwaysHasTheStatus(t *testing.T) {
	for _, content := range []string{"# plain", "---\ntitle: T\n---\nx", "---\nstatus: draft\n---\nx", "---\r\nstatus: draft\r\n---\r\nx"} {
		e := newEnv(t, fakegitlab.ProjectOptions{})
		if _, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", content)); err != nil {
			t.Fatalf("%q: %v", content, err)
		}
		got, _ := e.fake.File(project, "master", "docs/a.md")
		if parsed := docs.Parse([]byte(got), "docs/a.md"); parsed.Status != docs.StatusPublished || len(parsed.Warnings) != 0 {
			t.Errorf("%q published as %q (%v)", content, got, parsed.Warnings)
		}
	}
}

func TestContentThatIsAlreadyPublishedIsLeftUntouched(t *testing.T) {
	for _, content := range []string{"# Plain, no front matter\n", "---\ntitle: T\nstatus: published\n---\nbody\n", "---\r\ntitle: T\r\n---\r\nbody\r\n"} {
		e := newEnv(t, fakegitlab.ProjectOptions{})
		if _, err := e.publisher.Publish(context.Background(), e.request("docs/a.md", content)); err != nil {
			t.Fatalf("%q: %v", content, err)
		}
		if got, _ := e.fake.File(project, "master", "docs/a.md"); got != content {
			t.Errorf("content changed:\n got %q\nwant %q", got, content)
		}
	}
}

func TestAnOpenMergeRequestBlocksAnotherPublish(t *testing.T) {
	e := newEnv(t, fakegitlab.ProjectOptions{})
	first := e.request("docs/a.md", "# A")
	first.AutoMerge = false
	pending, err := e.publisher.Publish(context.Background(), first)
	if err != nil || pending.Outcome != OutcomePending {
		t.Fatalf("pending = %+v, err = %v", pending, err)
	}

	again := e.request("docs/a.md", "# A edited")
	again.PreviousMergeRequestIID = pending.MergeRequestIID
	_, err = e.publisher.Publish(context.Background(), again)
	var open *OpenMergeRequestError
	if !errors.As(err, &open) || open.URL != pending.MergeRequestURL {
		t.Fatalf("err = %v", err)
	}

	again.PreviousMergeRequestIID = 9999
	if _, err := e.publisher.Publish(context.Background(), again); err != nil {
		t.Errorf("an unknown previous merge request must not block: %v", err)
	}
}
