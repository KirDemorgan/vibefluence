package portal

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"vibefluence/internal/docs"
	"vibefluence/internal/drafts"
	"vibefluence/internal/index"
	"vibefluence/internal/markdown"
	"vibefluence/internal/publish"
)

var (
	ErrBusy     = errors.New("busy")
	ErrConflict = errors.New("conflict")
)

type ConflictError struct {
	Exists        bool
	CurrentBlobID string
	Message       string
}

func (e *ConflictError) Error() string        { return e.Message }
func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

type DraftStore interface {
	Save(draft drafts.Draft) (drafts.Draft, error)
	Get(repo, id string) (drafts.Draft, error)
	List(repo string) ([]drafts.Draft, error)
	Delete(repo, id string) error
	SetMergeRequest(repo, id, url string, iid int) error
}

type Publisher interface {
	Publish(ctx context.Context, request publish.Request) (publish.Result, error)
}

type DraftSummary struct {
	ID              string
	Repo            string
	Path            string
	Title           string
	Status          docs.Status
	IsNew           bool
	Author          string
	UpdatedAt       time.Time
	BaseBlobID      string
	MergeRequestURL string
}

type DraftView struct {
	DraftSummary
	Content string
}

func summarize(draft drafts.Draft) DraftSummary {
	return DraftSummary{
		ID: draft.ID, Repo: draft.Repo, Path: draft.Path, Title: draft.Title,
		Status: docs.Parse([]byte(draft.Content), draft.Path).Status, IsNew: draft.IsNew(),
		Author: draft.Author, UpdatedAt: draft.UpdatedAt, BaseBlobID: draft.BaseBlobID,
		MergeRequestURL: draft.MergeRequestURL,
	}
}

func (p *Portal) requireWritable() error {
	if p.readOnly {
		return fmt.Errorf("%w: this server has no GitLab token", ErrReadOnly)
	}
	return nil
}

func (p *Portal) requireDrafts() error {
	if p.drafts == nil {
		return invalid("drafts are not available on this server")
	}
	return nil
}

func (p *Portal) draftError(err error) error {
	switch {
	case errors.Is(err, drafts.ErrNotFound):
		return fmt.Errorf("%w: draft", ErrNotFound)
	case errors.Is(err, drafts.ErrInvalid), errors.Is(err, drafts.ErrLimit):
		return &ValidationError{Message: err.Error()}
	}
	return err
}

func (p *Portal) ListDrafts(_ context.Context, repoID string) ([]DraftSummary, error) {
	if _, ok := p.index.Repo(repoID); !ok {
		return nil, fmt.Errorf("%w: repository %s", ErrNotFound, repoID)
	}
	if p.drafts == nil {
		return []DraftSummary{}, nil
	}
	stored, err := p.drafts.List(repoID)
	if err != nil {
		return nil, p.draftError(err)
	}
	summaries := make([]DraftSummary, 0, len(stored))
	for _, draft := range stored {
		summaries = append(summaries, summarize(draft))
	}
	return summaries, nil
}

func (p *Portal) AllDrafts(ctx context.Context) ([]DraftSummary, error) {
	all := []DraftSummary{}
	for _, repo := range p.index.Repos() {
		items, err := p.ListDrafts(ctx, repo.ID)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].UpdatedAt.After(all[j].UpdatedAt) })
	return all, nil
}

func (p *Portal) GetDraft(_ context.Context, repoID, id string) (DraftView, error) {
	if _, ok := p.index.Repo(repoID); !ok {
		return DraftView{}, fmt.Errorf("%w: repository %s", ErrNotFound, repoID)
	}
	if err := p.requireDrafts(); err != nil {
		return DraftView{}, err
	}
	draft, err := p.drafts.Get(repoID, id)
	if err != nil {
		return DraftView{}, p.draftError(err)
	}
	return DraftView{DraftSummary: summarize(draft), Content: draft.Content}, nil
}

type SaveDraftInput struct {
	Path       string
	Content    string
	BaseBlobID string
	Author     string
}

func (p *Portal) SaveDraft(_ context.Context, repoID string, input SaveDraftInput) (DraftView, error) {
	if err := p.requireWritable(); err != nil {
		return DraftView{}, err
	}
	repo, ok := p.index.Repo(repoID)
	if !ok {
		return DraftView{}, fmt.Errorf("%w: repository %s", ErrNotFound, repoID)
	}
	if err := p.requireDrafts(); err != nil {
		return DraftView{}, err
	}
	if err := docs.ValidateDocPath(input.Path, repo.Paths); err != nil {
		return DraftView{}, &ValidationError{Message: err.Error()}
	}
	saved, err := p.drafts.Save(drafts.Draft{
		Repo: repoID, Path: input.Path, Content: input.Content, BaseBlobID: input.BaseBlobID, Author: input.Author,
	})
	if err != nil {
		return DraftView{}, p.draftError(err)
	}
	return DraftView{DraftSummary: summarize(saved), Content: saved.Content}, nil
}

func (p *Portal) DeleteDraft(_ context.Context, repoID, id string) error {
	if err := p.requireWritable(); err != nil {
		return err
	}
	if _, ok := p.index.Repo(repoID); !ok {
		return fmt.Errorf("%w: repository %s", ErrNotFound, repoID)
	}
	if err := p.requireDrafts(); err != nil {
		return err
	}
	return p.draftError(p.drafts.Delete(repoID, id))
}

type PublishInput struct {
	Author string
	Force  bool
}

type PublishOutcome struct {
	Outcome         publish.Outcome
	Reason          publish.Reason
	Detail          string
	MergeRequestURL string
	MergeRequestIID int
	Created         bool
	Path            string
	DraftRemoved    bool
}

func (p *Portal) PublishDraft(ctx context.Context, repoID, id string, input PublishInput) (PublishOutcome, error) {
	if err := p.requireWritable(); err != nil {
		return PublishOutcome{}, err
	}
	repo, ok := p.index.Repo(repoID)
	if !ok {
		return PublishOutcome{}, fmt.Errorf("%w: repository %s", ErrNotFound, repoID)
	}
	if err := p.requireDrafts(); err != nil {
		return PublishOutcome{}, err
	}
	if p.publisher == nil {
		return PublishOutcome{}, invalid("publishing is not available on this server")
	}
	draft, err := p.drafts.Get(repoID, id)
	if err != nil {
		return PublishOutcome{}, p.draftError(err)
	}
	view, err := p.view(ctx, repoID)
	if err != nil {
		return PublishOutcome{}, err
	}
	author := input.Author
	if author == "" {
		author = draft.Author
	}

	result, err := p.publisher.Publish(ctx, publish.Request{
		Project: repo.Project, Branch: view.Branch, Roots: repo.Paths, Path: draft.Path, Content: draft.Content,
		BaseBlobID: draft.BaseBlobID, Author: author, AutoMerge: p.autoMerge(repoID), Force: input.Force, PreviousMergeRequestIID: draft.MergeRequestIID,
	})
	if err != nil {
		return PublishOutcome{}, p.publishError(err)
	}

	outcome := PublishOutcome{
		Outcome: result.Outcome, Reason: result.Reason, Detail: result.Detail, MergeRequestURL: result.MergeRequestURL,
		MergeRequestIID: result.MergeRequestIID, Created: result.Created, Path: draft.Path,
	}
	if result.Outcome == publish.OutcomeMerged {
		outcome.DraftRemoved = p.removeDraft(ctx, repoID, id)
		return outcome, nil
	}
	p.rememberMergeRequest(ctx, draft, result)
	return outcome, nil
}

func (p *Portal) removeDraft(ctx context.Context, repoID, id string) bool {
	if err := p.drafts.Delete(repoID, id); err != nil && !errors.Is(err, drafts.ErrNotFound) {
		p.logger.WarnContext(ctx, "could not remove a published draft", "repo", repoID, "error", err)
		return false
	}
	return true
}

func (p *Portal) rememberMergeRequest(ctx context.Context, draft drafts.Draft, result publish.Result) {
	if err := p.drafts.SetMergeRequest(draft.Repo, draft.ID, result.MergeRequestURL, result.MergeRequestIID); err != nil {
		p.logger.WarnContext(ctx, "could not remember the merge request of a draft", "repo", draft.Repo, "error", err)
	}
}

func (p *Portal) publishError(err error) error {
	var conflict *publish.ConflictError
	var validation *publish.ValidationError
	var open *publish.OpenMergeRequestError
	switch {
	case errors.As(err, &open):
		return invalid("%s. Merge or close it before publishing again", open.Error())
	case errors.As(err, &conflict):
		return &ConflictError{Exists: conflict.Exists, CurrentBlobID: conflict.CurrentBlobID, Message: conflict.Error()}
	case errors.As(err, &validation):
		return &ValidationError{Message: validation.Message}
	case errors.Is(err, publish.ErrNoChanges):
		return invalid("nothing to publish: the document is identical to the version in the repository")
	case errors.Is(err, publish.ErrBusy):
		return fmt.Errorf("%w: a publish for this document is already running", ErrBusy)
	}
	return err
}

type PreviewView struct {
	Title    string
	Status   docs.Status
	Type     string
	Warnings []string
	HTML     string
	Headings []markdown.Heading
}

func (p *Portal) Preview(ctx context.Context, repoID, filePath, content string) (PreviewView, error) {
	view, err := p.view(ctx, repoID)
	if err != nil {
		return PreviewView{}, err
	}
	if len(content) > drafts.MaxContentBytes {
		return PreviewView{}, invalid("the document is larger than %d bytes", drafts.MaxContentBytes)
	}
	if filePath == "" || len(filePath) > maxImagePathBytes {
		filePath = firstRoot(view.Repo) + "/preview.md"
	}
	parsed := docs.Parse([]byte(content), filePath)
	rendered, err := p.renderer.Render([]byte(parsed.Body), markdown.LinkContext{
		Repo:    repoID,
		DocPath: filePath,
		BlobURL: func(target string) string { return p.blobURL(view.Repo, view.Branch, target) },
	})
	if err != nil {
		return PreviewView{}, &ValidationError{Message: err.Error()}
	}
	return PreviewView{
		Title: parsed.Title, Status: parsed.Status, Type: parsed.Type, Warnings: parsed.Warnings,
		HTML: rendered.HTML, Headings: rendered.Headings,
	}, nil
}

func firstRoot(repo index.Repo) string {
	if len(repo.Paths) == 0 {
		return "docs"
	}
	return repo.Paths[0]
}
