package gitlab

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

type CommitAction struct {
	Action       string  `json:"action"`
	FilePath     string  `json:"file_path"`
	PreviousPath string  `json:"previous_path,omitempty"`
	Content      *string `json:"content,omitempty"`
	LastCommitID string  `json:"last_commit_id,omitempty"`
}

type CommitInput struct {
	Branch      string         `json:"branch"`
	StartBranch string         `json:"start_branch,omitempty"`
	Message     string         `json:"commit_message"`
	AuthorName  string         `json:"author_name,omitempty"`
	AuthorEmail string         `json:"author_email,omitempty"`
	Actions     []CommitAction `json:"actions"`
}

type Commit struct {
	ID      string `json:"id"`
	ShortID string `json:"short_id"`
	WebURL  string `json:"web_url"`
}

func (c *Client) CreateCommit(ctx context.Context, project string, input CommitInput) (Commit, error) {
	if input.Branch == "" || input.Message == "" || len(input.Actions) == 0 {
		return Commit{}, errors.New("gitlab: a commit needs a branch, a message and at least one action")
	}
	resp, err := c.send(ctx, http.MethodPost, projectEndpoint(project, "/repository/commits"), nil, input)
	if err != nil {
		return Commit{}, err
	}
	var commit Commit
	return commit, decodeBody(resp, &commit)
}

func (c *Client) DeleteBranch(ctx context.Context, project, name string) error {
	_, err := c.send(ctx, http.MethodDelete, projectEndpoint(project, "/repository/branches/"+url.PathEscape(name)), nil, nil)
	return err
}

type MergeRequestInput struct {
	SourceBranch       string `json:"source_branch"`
	TargetBranch       string `json:"target_branch"`
	Title              string `json:"title"`
	Description        string `json:"description,omitempty"`
	RemoveSourceBranch bool   `json:"remove_source_branch"`
}

type MergeRequest struct {
	IID                 int     `json:"iid"`
	State               string  `json:"state"`
	SourceBranch        string  `json:"source_branch"`
	TargetBranch        string  `json:"target_branch"`
	SHA                 string  `json:"sha"`
	DetailedMergeStatus string  `json:"detailed_merge_status"`
	HasConflicts        bool    `json:"has_conflicts"`
	WebURL              string  `json:"web_url"`
	MergeCommitSHA      *string `json:"merge_commit_sha"`
}

func (c *Client) CreateMergeRequest(ctx context.Context, project string, input MergeRequestInput) (MergeRequest, error) {
	resp, err := c.send(ctx, http.MethodPost, projectEndpoint(project, "/merge_requests"), nil, input)
	if err != nil {
		return MergeRequest{}, err
	}
	var created MergeRequest
	return created, decodeBody(resp, &created)
}

func (c *Client) GetMergeRequest(ctx context.Context, project string, iid int) (MergeRequest, error) {
	var found MergeRequest
	err := c.getJSON(ctx, projectEndpoint(project, "/merge_requests/"+strconv.Itoa(iid)), nil, &found)
	return found, err
}

type MergeOptions struct {
	SHA                      string `json:"sha,omitempty"`
	MergeCommitMessage       string `json:"merge_commit_message,omitempty"`
	ShouldRemoveSourceBranch bool   `json:"should_remove_source_branch"`
}

func (c *Client) MergeMergeRequest(ctx context.Context, project string, iid int, options MergeOptions) (MergeRequest, error) {
	resp, err := c.send(ctx, http.MethodPut, projectEndpoint(project, "/merge_requests/"+strconv.Itoa(iid)+"/merge"), nil, options)
	if err != nil {
		return MergeRequest{}, err
	}
	var merged MergeRequest
	return merged, decodeBody(resp, &merged)
}

type Pipeline struct {
	ID     int    `json:"id"`
	Ref    string `json:"ref"`
	SHA    string `json:"sha"`
	Status string `json:"status"`
}

func (c *Client) MergeRequestPipelines(ctx context.Context, project string, iid int) ([]Pipeline, error) {
	var pipelines []Pipeline
	err := c.getJSON(ctx, projectEndpoint(project, "/merge_requests/"+strconv.Itoa(iid)+"/pipelines"), nil, &pipelines)
	return pipelines, err
}

func (c *Client) BranchPipelines(ctx context.Context, project, ref string) ([]Pipeline, error) {
	var pipelines []Pipeline
	err := c.getJSON(ctx, projectEndpoint(project, "/pipelines"), url.Values{"ref": {ref}}, &pipelines)
	return pipelines, err
}
