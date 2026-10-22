package publish

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"vibefluence/internal/docs"
	"vibefluence/internal/drafts"
	"vibefluence/internal/gitlab"
)

const (
	BranchPrefix = "vibefluence/"

	defaultPipelineGrace  = 2 * time.Second
	defaultStatusPolls    = 6
	defaultStatusInterval = 500 * time.Millisecond
	staleEditMessage      = "changed since you started editing"
)

type Outcome string

const (
	OutcomeMerged  Outcome = "merged"
	OutcomePending Outcome = "pending"
)

type Reason string

const (
	ReasonNone              Reason = ""
	ReasonAutoMergeDisabled Reason = "auto_merge_disabled"
	ReasonPipelineStarted   Reason = "pipeline_started"
	ReasonPipelineUnknown   Reason = "pipeline_unknown"
	ReasonNotMergeable      Reason = "not_mergeable"
	ReasonForbidden         Reason = "forbidden"
	ReasonRefused           Reason = "refused"
)

var (
	ErrInvalid   = errors.New("invalid publish request")
	ErrNoChanges = errors.New("nothing to publish")
	ErrBusy      = errors.New("a publish for this document is already running")
	ErrConflict  = errors.New("the document changed in the repository")
)

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string        { return e.Message }
func (e *ValidationError) Is(target error) bool { return target == ErrInvalid }

type ConflictError struct {
	Exists        bool
	CurrentBlobID string
}

func (e *ConflictError) Error() string {
	if e.Exists {
		return "the document was changed or created in the repository after this draft was started"
	}
	return "the document was removed from the repository after this draft was started"
}

func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

type Writer interface {
	File(ctx context.Context, project, ref, filePath string) (gitlab.File, error)
	CreateCommit(ctx context.Context, project string, input gitlab.CommitInput) (gitlab.Commit, error)
	CreateMergeRequest(ctx context.Context, project string, input gitlab.MergeRequestInput) (gitlab.MergeRequest, error)
	GetMergeRequest(ctx context.Context, project string, iid int) (gitlab.MergeRequest, error)
	MergeMergeRequest(ctx context.Context, project string, iid int, options gitlab.MergeOptions) (gitlab.MergeRequest, error)
	MergeRequestPipelines(ctx context.Context, project string, iid int) ([]gitlab.Pipeline, error)
	BranchPipelines(ctx context.Context, project, ref string) ([]gitlab.Pipeline, error)
	DeleteBranch(ctx context.Context, project, name string) error
}

type Options struct {
	Client         Writer
	Invalidate     func(project, branch string) int
	Logger         *slog.Logger
	Sleep          func(ctx context.Context, d time.Duration) error
	NewSuffix      func() string
	PipelineGrace  time.Duration
	StatusPolls    int
	StatusInterval time.Duration
}

type Publisher struct {
	client         Writer
	invalidate     func(project, branch string) int
	logger         *slog.Logger
	sleep          func(ctx context.Context, d time.Duration) error
	newSuffix      func() string
	pipelineGrace  time.Duration
	statusPolls    int
	statusInterval time.Duration

	mu     sync.Mutex
	active map[string]struct{}
}

func New(opts Options) *Publisher {
	publisher := &Publisher{
		client:         opts.Client,
		invalidate:     opts.Invalidate,
		logger:         opts.Logger,
		sleep:          opts.Sleep,
		newSuffix:      opts.NewSuffix,
		pipelineGrace:  opts.PipelineGrace,
		statusPolls:    opts.StatusPolls,
		statusInterval: opts.StatusInterval,
		active:         make(map[string]struct{}),
	}
	if publisher.logger == nil {
		publisher.logger = slog.New(slog.DiscardHandler)
	}
	if publisher.sleep == nil {
		publisher.sleep = sleepContext
	}
	if publisher.newSuffix == nil {
		publisher.newSuffix = randomSuffix
	}
	if publisher.invalidate == nil {
		publisher.invalidate = func(string, string) int { return 0 }
	}
	if publisher.pipelineGrace <= 0 {
		publisher.pipelineGrace = defaultPipelineGrace
	}
	if publisher.statusPolls <= 0 {
		publisher.statusPolls = defaultStatusPolls
	}
	if publisher.statusInterval <= 0 {
		publisher.statusInterval = defaultStatusInterval
	}
	return publisher
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func randomSuffix() string {
	raw := make([]byte, 3)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw)
}

type Request struct {
	Project    string
	Branch     string
	Roots      []string
	Path       string
	Content    string
	BaseBlobID string
	Author     string
	AutoMerge  bool
	Force      bool
}

type Result struct {
	Outcome         Outcome
	Reason          Reason
	Detail          string
	MergeRequestURL string
	MergeRequestIID int
	SourceBranch    string
	Created         bool
	MergeCommitSHA  string
	Content         string
}

func (p *Publisher) Publish(ctx context.Context, request Request) (Result, error) {
	content, err := validate(request)
	if err != nil {
		return Result{}, err
	}
	release, err := p.acquire(request.Project + "\x00" + request.Path)
	if err != nil {
		return Result{}, err
	}
	defer release()

	parsed := docs.Parse([]byte(content), request.Path)
	existing, err := p.currentFile(ctx, request)
	if err != nil {
		return Result{}, err
	}
	if existing != nil && string(existing.Content) == content {
		return Result{}, ErrNoChanges
	}

	created := existing == nil
	source := p.branchName(parsed.Title, request.Path)
	if err := p.commit(ctx, request, content, parsed.Title, source, existing); err != nil {
		return Result{}, err
	}

	title := mergeTitle(created, parsed.Title)
	mr, err := p.client.CreateMergeRequest(ctx, request.Project, gitlab.MergeRequestInput{
		SourceBranch:       source,
		TargetBranch:       request.Branch,
		Title:              title,
		Description:        description(request.Author),
		RemoveSourceBranch: true,
	})
	if err != nil {
		p.discardBranch(ctx, request.Project, source)
		return Result{}, fmt.Errorf("create merge request: %w", err)
	}

	result := Result{
		Outcome: OutcomePending, MergeRequestURL: mr.WebURL, MergeRequestIID: mr.IID,
		SourceBranch: source, Created: created, Content: content,
	}
	if !request.AutoMerge {
		result.Reason = ReasonAutoMergeDisabled
		return result, nil
	}
	return p.merge(ctx, request, mr, title, result)
}

func validate(request Request) (string, error) {
	if request.Project == "" || request.Branch == "" {
		return "", &ValidationError{Message: "the repository is not configured completely"}
	}
	if err := docs.ValidateDocPath(request.Path, request.Roots); err != nil {
		return "", &ValidationError{Message: err.Error()}
	}
	if err := drafts.Validate(drafts.Draft{Repo: "validate", Path: request.Path, Content: request.Content}); err != nil {
		return "", &ValidationError{Message: err.Error()}
	}
	parsed := docs.Parse([]byte(request.Content), request.Path)
	if parsed.Status == docs.StatusPublished && len(parsed.Warnings) == 0 {
		return request.Content, nil
	}
	content, err := docs.EnsureStatus(request.Content, docs.StatusPublished)
	if err != nil {
		return "", &ValidationError{Message: err.Error()}
	}
	return content, nil
}

func (p *Publisher) acquire(key string) (func(), error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, busy := p.active[key]; busy {
		return nil, ErrBusy
	}
	p.active[key] = struct{}{}
	return func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		delete(p.active, key)
	}, nil
}

func (p *Publisher) currentFile(ctx context.Context, request Request) (*gitlab.File, error) {
	file, err := p.client.File(ctx, request.Project, request.Branch, request.Path)
	if errors.Is(err, gitlab.ErrNotFound) {
		if request.BaseBlobID != "" && !request.Force {
			return nil, &ConflictError{Exists: false}
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the current document: %w", err)
	}
	if !request.Force && request.BaseBlobID != file.BlobID {
		return nil, &ConflictError{Exists: true, CurrentBlobID: file.BlobID}
	}
	return &file, nil
}

func (p *Publisher) branchName(title, filePath string) string {
	base := docs.Slug(title)
	if base == "untitled" {
		stem := strings.TrimSuffix(path.Base(filePath), path.Ext(filePath))
		base = docs.Slug(stem)
	}
	return BranchPrefix + base + "-" + p.newSuffix()
}

func (p *Publisher) commit(ctx context.Context, request Request, content, title, branch string, existing *gitlab.File) error {
	action := gitlab.CommitAction{Action: "create", FilePath: request.Path, Content: &content}
	verb := "Add"
	if existing != nil {
		action.Action = "update"
		action.LastCommitID = existing.LastCommitID
		verb = "Update"
	}
	message := fmt.Sprintf("%s %s", verb, request.Path)
	if author := drafts.CleanAuthor(request.Author); author != "" {
		message += "\n\nEdited-by: " + author
	}
	_, err := p.client.CreateCommit(ctx, request.Project, gitlab.CommitInput{
		Branch: branch, StartBranch: request.Branch, Message: message, Actions: []gitlab.CommitAction{action},
	})
	if err == nil {
		return nil
	}
	var apiErr *gitlab.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusBadRequest {
		lower := strings.ToLower(apiErr.Message)
		if strings.Contains(lower, staleEditMessage) || strings.Contains(lower, "already exists") {
			return &ConflictError{Exists: true}
		}
	}
	return fmt.Errorf("commit the document (%s): %w", title, err)
}

func (p *Publisher) discardBranch(ctx context.Context, project, branch string) {
	if err := p.client.DeleteBranch(context.WithoutCancel(ctx), project, branch); err != nil {
		p.logger.WarnContext(ctx, "could not remove an orphan branch",
			slog.String("project", project), slog.String("branch", branch), slog.Any("error", err))
	}
}

func mergeTitle(created bool, title string) string {
	if created {
		return "Vibefluence: add " + title
	}
	return "Vibefluence: update " + title
}

func description(author string) string {
	if cleaned := drafts.CleanAuthor(author); cleaned != "" {
		return "Published from Vibefluence by " + cleaned + "."
	}
	return "Published from Vibefluence."
}

func (p *Publisher) merge(ctx context.Context, request Request, mr gitlab.MergeRequest, title string, result Result) (Result, error) {
	if err := p.sleep(ctx, p.pipelineGrace); err != nil {
		return Result{}, err
	}
	if reason, detail := p.pipelineGate(ctx, request, mr); reason != ReasonNone {
		result.Reason, result.Detail = reason, detail
		return result, nil
	}

	current, reason, detail, err := p.waitUntilMergeable(ctx, request.Project, mr)
	if err != nil {
		return Result{}, err
	}
	if reason != ReasonNone {
		result.Reason, result.Detail = reason, detail
		return result, nil
	}

	merged, err := p.client.MergeMergeRequest(ctx, request.Project, mr.IID, gitlab.MergeOptions{
		SHA: current.SHA, MergeCommitMessage: title, ShouldRemoveSourceBranch: true,
	})
	if err != nil {
		return p.afterMergeError(ctx, request, mr, result, err)
	}
	return p.finish(request, result, merged), nil
}

func (p *Publisher) pipelineGate(ctx context.Context, request Request, mr gitlab.MergeRequest) (Reason, string) {
	forMR, err := p.client.MergeRequestPipelines(ctx, request.Project, mr.IID)
	if err != nil {
		p.logger.WarnContext(ctx, "cannot list merge request pipelines", slog.Any("error", err))
		return ReasonPipelineUnknown, "could not check whether CI pipelines were started"
	}
	forBranch, err := p.client.BranchPipelines(ctx, request.Project, mr.SourceBranch)
	if err != nil {
		p.logger.WarnContext(ctx, "cannot list branch pipelines", slog.Any("error", err))
		return ReasonPipelineUnknown, "could not check whether CI pipelines were started"
	}
	if len(forMR)+len(forBranch) > 0 {
		return ReasonPipelineStarted, "a CI pipeline was started for this change, so it was not merged automatically"
	}
	return ReasonNone, ""
}

func transient(status string) bool {
	switch status {
	case "checking", "unchecked", "preparing", "approvals_syncing", "":
		return true
	}
	return false
}

func (p *Publisher) waitUntilMergeable(ctx context.Context, project string, mr gitlab.MergeRequest) (gitlab.MergeRequest, Reason, string, error) {
	current := mr
	for poll := 0; poll < p.statusPolls; poll++ {
		if poll > 0 {
			if err := p.sleep(ctx, p.statusInterval); err != nil {
				return current, ReasonNone, "", err
			}
		}
		latest, err := p.client.GetMergeRequest(ctx, project, mr.IID)
		if err != nil {
			return current, ReasonNone, "", fmt.Errorf("check the merge request: %w", err)
		}
		current = latest
		switch {
		case latest.State == "merged":
			return current, ReasonNone, "", nil
		case latest.DetailedMergeStatus == "mergeable":
			return current, ReasonNone, "", nil
		case !transient(latest.DetailedMergeStatus):
			return current, ReasonNotMergeable, latest.DetailedMergeStatus, nil
		}
	}
	return current, ReasonNotMergeable, current.DetailedMergeStatus, nil
}

func (p *Publisher) afterMergeError(ctx context.Context, request Request, mr gitlab.MergeRequest, result Result, mergeErr error) (Result, error) {
	latest, err := p.client.GetMergeRequest(context.WithoutCancel(ctx), request.Project, mr.IID)
	if err == nil && latest.State == "merged" {
		return p.finish(request, result, latest), nil
	}
	p.logger.WarnContext(ctx, "merge was not completed", slog.Int("iid", mr.IID), slog.Any("error", mergeErr))
	if errors.Is(mergeErr, gitlab.ErrUnauthorized) || errors.Is(mergeErr, gitlab.ErrForbidden) {
		result.Reason = ReasonForbidden
		result.Detail = "the GitLab token is not allowed to merge here"
		return result, nil
	}
	result.Reason = ReasonRefused
	result.Detail = "GitLab refused or failed to merge the request"
	return result, nil
}

func (p *Publisher) finish(request Request, result Result, merged gitlab.MergeRequest) Result {
	result.Outcome = OutcomeMerged
	result.Reason = ReasonNone
	if merged.MergeCommitSHA != nil {
		result.MergeCommitSHA = *merged.MergeCommitSHA
	}
	if merged.WebURL != "" {
		result.MergeRequestURL = merged.WebURL
	}
	p.invalidate(request.Project, request.Branch)
	return result
}
