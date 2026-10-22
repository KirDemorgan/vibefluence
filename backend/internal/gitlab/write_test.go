package gitlab

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"vibefluence/internal/testing/fakegitlab"
)

func text(value string) *string { return &value }

func TestCreateCommitAndMergeRequestFlow(t *testing.T) {
	f := newFixture(t)
	f.fake.PutFile(project, "master", "docs/a.md", "old")

	file, err := f.client.File(context.Background(), project, "master", "docs/a.md")
	if err != nil {
		t.Fatal(err)
	}
	commit, err := f.client.CreateCommit(context.Background(), project, CommitInput{
		Branch: "vibefluence/a-1", StartBranch: "master", Message: "Update docs/a.md",
		Actions: []CommitAction{
			{Action: "update", FilePath: "docs/a.md", Content: text("new"), LastCommitID: file.LastCommitID},
			{Action: "create", FilePath: "docs/b.md", Content: text("b")},
		},
	})
	if err != nil || len(commit.ID) != 40 || commit.ShortID == "" || !strings.Contains(commit.WebURL, commit.ID) {
		t.Fatalf("commit = %+v, err = %v", commit, err)
	}

	created, err := f.client.CreateMergeRequest(context.Background(), project, MergeRequestInput{
		SourceBranch: "vibefluence/a-1", TargetBranch: "master", Title: "Vibefluence: update a", RemoveSourceBranch: true,
	})
	if err != nil || created.IID != 1 || created.State != "opened" || created.DetailedMergeStatus != "checking" || created.SHA != commit.ID {
		t.Fatalf("created = %+v, err = %v", created, err)
	}

	early, err := f.client.MergeMergeRequest(context.Background(), project, created.IID, MergeOptions{SHA: created.SHA})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusMethodNotAllowed {
		t.Fatalf("merging before the check finished: %+v, err = %v", early, err)
	}

	ready, err := f.client.GetMergeRequest(context.Background(), project, created.IID)
	if err != nil || ready.DetailedMergeStatus != "mergeable" {
		t.Fatalf("poll after the failed attempt = %+v, err = %v", ready, err)
	}

	merged, err := f.client.MergeMergeRequest(context.Background(), project, created.IID, MergeOptions{
		SHA: ready.SHA, MergeCommitMessage: "Vibefluence: update a", ShouldRemoveSourceBranch: true,
	})
	if err != nil || merged.State != "merged" || merged.MergeCommitSHA == nil || *merged.MergeCommitSHA == "" {
		t.Fatalf("merged = %+v, err = %v", merged, err)
	}
	if got, _ := f.fake.File(project, "master", "docs/a.md"); got != "new" {
		t.Errorf("a.md = %q", got)
	}
	if got, _ := f.fake.File(project, "master", "docs/b.md"); got != "b" {
		t.Errorf("b.md = %q", got)
	}
	if strings.Join(f.fake.Branches(project), ",") != "master" {
		t.Errorf("branches = %v", f.fake.Branches(project))
	}
}

func TestCreateCommitValidatesInputBeforeSending(t *testing.T) {
	f := newFixture(t)
	for _, input := range []CommitInput{
		{Message: "m", Actions: []CommitAction{{Action: "create", FilePath: "a"}}},
		{Branch: "b", Actions: []CommitAction{{Action: "create", FilePath: "a"}}},
		{Branch: "b", Message: "m"},
	} {
		if _, err := f.client.CreateCommit(context.Background(), project, input); err == nil {
			t.Errorf("%+v should be rejected", input)
		}
	}
	if f.fake.RequestCount() != 0 {
		t.Errorf("invalid input must not reach GitLab: %d requests", f.fake.RequestCount())
	}
}

func TestStaleLastCommitIDIsReportedAsAPIError(t *testing.T) {
	f := newFixture(t)
	f.fake.PutFile(project, "master", "docs/a.md", "v1")
	stale, _ := f.client.File(context.Background(), project, "master", "docs/a.md")
	f.fake.PutFile(project, "master", "docs/a.md", "v2")

	_, err := f.client.CreateCommit(context.Background(), project, CommitInput{
		Branch: "topic", StartBranch: "master", Message: "m",
		Actions: []CommitAction{{Action: "update", FilePath: "docs/a.md", Content: text("mine"), LastCommitID: stale.LastCommitID}},
	})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest || !strings.Contains(apiErr.Message, "changed since you started editing") {
		t.Errorf("err = %v", err)
	}
}

func TestWritesAreNotRetried(t *testing.T) {
	f := newFixture(t)
	f.fake.PutFile(project, "master", "docs/a.md", "x")
	f.fake.FailNext(1, http.StatusBadGateway)

	_, err := f.client.CreateCommit(context.Background(), project, CommitInput{
		Branch: "topic", StartBranch: "master", Message: "m",
		Actions: []CommitAction{{Action: "create", FilePath: "docs/n.md", Content: text("n")}},
	})
	if err == nil {
		t.Fatal("expected the failure to surface")
	}
	if f.fake.RequestCount() != 1 || len(f.sleeps) != 0 {
		t.Errorf("a non-idempotent write must not be retried: requests = %d, sleeps = %v", f.fake.RequestCount(), f.sleeps)
	}
}

func TestMergeFailuresAreTyped(t *testing.T) {
	f := newFixture(t)
	f.fake.SetProjectOptions(project, fakegitlab.ProjectOptions{ForbidMerge: true})
	f.fake.PutFile(project, "master", "docs/a.md", "x")
	if _, err := f.client.CreateCommit(context.Background(), project, CommitInput{
		Branch: "topic", StartBranch: "master", Message: "m",
		Actions: []CommitAction{{Action: "update", FilePath: "docs/a.md", Content: text("y")}},
	}); err != nil {
		t.Fatal(err)
	}
	created, err := f.client.CreateMergeRequest(context.Background(), project, MergeRequestInput{SourceBranch: "topic", TargetBranch: "master", Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.client.MergeMergeRequest(context.Background(), project, created.IID, MergeOptions{})
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("a forbidden merge must read as unauthorized: %v", err)
	}
	if _, err := f.client.GetMergeRequest(context.Background(), project, 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown merge request err = %v", err)
	}
	if _, err := f.client.CreateMergeRequest(context.Background(), project, MergeRequestInput{SourceBranch: "ghost", TargetBranch: "master", Title: "t"}); err == nil {
		t.Error("a merge request from an unknown branch must fail")
	}
}

func TestPipelineQueries(t *testing.T) {
	f := newFixture(t)
	f.fake.SetProjectOptions(project, fakegitlab.ProjectOptions{PipelineBranches: func(string) bool { return true }})
	f.fake.PutFile(project, "master", "docs/a.md", "x")
	if _, err := f.client.CreateCommit(context.Background(), project, CommitInput{
		Branch: "topic", StartBranch: "master", Message: "m",
		Actions: []CommitAction{{Action: "update", FilePath: "docs/a.md", Content: text("y")}},
	}); err != nil {
		t.Fatal(err)
	}
	created, err := f.client.CreateMergeRequest(context.Background(), project, MergeRequestInput{SourceBranch: "topic", TargetBranch: "master", Title: "t"})
	if err != nil {
		t.Fatal(err)
	}

	forMR, err := f.client.MergeRequestPipelines(context.Background(), project, created.IID)
	if err != nil || len(forMR) != 2 || forMR[0].Status != "pending" {
		t.Errorf("merge request pipelines = %+v, err = %v", forMR, err)
	}
	forBranch, err := f.client.BranchPipelines(context.Background(), project, "topic")
	if err != nil || len(forBranch) != 2 {
		t.Errorf("branch pipelines = %+v, err = %v", forBranch, err)
	}
	other, err := f.client.BranchPipelines(context.Background(), project, "elsewhere")
	if err != nil || len(other) != 0 {
		t.Errorf("unrelated branch pipelines = %+v, err = %v", other, err)
	}
}

func TestDeleteBranch(t *testing.T) {
	f := newFixture(t)
	f.fake.PutFile(project, "master", "docs/a.md", "x")
	if _, err := f.client.CreateCommit(context.Background(), project, CommitInput{
		Branch: "topic", StartBranch: "master", Message: "m",
		Actions: []CommitAction{{Action: "create", FilePath: "docs/n.md", Content: text("n")}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.client.DeleteBranch(context.Background(), project, "topic"); err != nil {
		t.Fatal(err)
	}
	if err := f.client.DeleteBranch(context.Background(), project, "topic"); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting twice: %v", err)
	}
	if err := f.client.DeleteBranch(context.Background(), project, "master"); !errors.Is(err, ErrForbidden) {
		t.Errorf("the default branch must be protected: %v", err)
	}
}
