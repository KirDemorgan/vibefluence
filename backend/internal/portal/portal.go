package portal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"vibefluence/internal/docs"
	"vibefluence/internal/gitlab"
	"vibefluence/internal/index"
	"vibefluence/internal/markdown"
)

var (
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid request")
)

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }
func (e *ValidationError) Is(target error) bool {
	return target == ErrInvalid
}

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

type FileReader interface {
	File(ctx context.Context, project, ref, filePath string) (gitlab.File, error)
}

type RepoSettings struct {
	AutoMerge bool
}

type Options struct {
	Index      *index.Manager
	Files      FileReader
	GitLabURL  string
	Settings   map[string]RepoSettings
	Logger     *slog.Logger
	Renderer   *markdown.Renderer
	ImageCache *ImageCache
}

type Portal struct {
	index    *index.Manager
	files    FileReader
	baseURL  string
	settings map[string]RepoSettings
	logger   *slog.Logger
	renderer *markdown.Renderer
	images   *ImageCache
}

func New(opts Options) *Portal {
	portal := &Portal{
		index:    opts.Index,
		files:    opts.Files,
		baseURL:  strings.TrimRight(opts.GitLabURL, "/"),
		settings: opts.Settings,
		logger:   opts.Logger,
		renderer: opts.Renderer,
		images:   opts.ImageCache,
	}
	if portal.logger == nil {
		portal.logger = slog.New(slog.DiscardHandler)
	}
	if portal.renderer == nil {
		portal.renderer = markdown.New()
	}
	if portal.images == nil {
		portal.images = NewImageCache(defaultImageEntries, defaultImageBytes, defaultImageTTL, time.Now)
	}
	if portal.settings == nil {
		portal.settings = map[string]RepoSettings{}
	}
	return portal
}

type RepoSummary struct {
	ID        string
	Name      string
	Project   string
	Branch    string
	Paths     []string
	AutoMerge bool
	DocCount  int
	Hidden    int
	Stale     bool
	Available bool
	WebURL    string
}

func (p *Portal) Repos(ctx context.Context) []RepoSummary {
	repos := p.index.Repos()
	summaries := make([]RepoSummary, len(repos))
	var group errgroup.Group
	group.SetLimit(4)
	for i, repo := range repos {
		group.Go(func() error {
			summaries[i] = p.summarize(ctx, repo)
			return nil
		})
	}
	_ = group.Wait()
	return summaries
}

func (p *Portal) summarize(ctx context.Context, repo index.Repo) RepoSummary {
	summary := RepoSummary{
		ID: repo.ID, Name: repo.Name, Project: repo.Project, Branch: repo.Branch, Paths: repo.Paths,
		AutoMerge: p.autoMerge(repo.ID), WebURL: p.projectURL(repo),
	}
	view, err := p.index.Get(ctx, repo.ID)
	if err != nil {
		p.logger.WarnContext(ctx, "repository unavailable", slog.String("repo", repo.ID), slog.Any("error", err))
		return summary
	}
	summary.Available = true
	summary.Branch = view.Branch
	summary.DocCount = len(view.Docs)
	summary.Hidden = view.Hidden
	summary.Stale = view.Stale
	return summary
}

func (p *Portal) autoMerge(repoID string) bool {
	settings, ok := p.settings[repoID]
	return !ok || settings.AutoMerge
}

func (p *Portal) projectURL(repo index.Repo) string {
	if p.baseURL == "" {
		return ""
	}
	return p.baseURL + "/" + escapeSegments(repo.Project)
}

func (p *Portal) blobURL(repo index.Repo, branch, filePath string) string {
	if p.baseURL == "" {
		return ""
	}
	return p.projectURL(repo) + "/-/blob/" + escapeSegments(branch) + "/" + escapeSegments(filePath)
}

func escapeSegments(value string) string {
	segments := strings.Split(value, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}

type TreeView struct {
	Repo   string
	Branch string
	Stale  bool
	Roots  []*index.Node
}

func (p *Portal) Tree(ctx context.Context, repoID string) (TreeView, error) {
	view, err := p.view(ctx, repoID)
	if err != nil {
		return TreeView{}, err
	}
	return TreeView{Repo: repoID, Branch: view.Branch, Stale: view.Stale, Roots: view.Tree()}, nil
}

type DocumentView struct {
	Repo     string
	Path     string
	Title    string
	Status   docs.Status
	Type     string
	Warnings []string
	HTML     string
	Headings []markdown.Heading
	Raw      string
	BlobID   string
	WebURL   string
	Stale    bool
}

func (p *Portal) Document(ctx context.Context, repoID, filePath string) (DocumentView, error) {
	view, err := p.view(ctx, repoID)
	if err != nil {
		return DocumentView{}, err
	}
	doc, ok := view.Doc(filePath)
	if !ok {
		return DocumentView{}, fmt.Errorf("%w: document %s", ErrNotFound, filePath)
	}
	rendered, err := p.renderer.Render([]byte(doc.Body), markdown.LinkContext{
		Repo:    repoID,
		DocPath: doc.Path,
		BlobURL: func(target string) string { return p.blobURL(view.Repo, view.Branch, target) },
	})
	if err != nil {
		return DocumentView{}, fmt.Errorf("render %s: %w", filePath, err)
	}
	return DocumentView{
		Repo: repoID, Path: doc.Path, Title: doc.Title, Status: doc.Status, Type: doc.Type, Warnings: doc.Warnings,
		HTML: rendered.HTML, Headings: rendered.Headings, Raw: doc.Raw, BlobID: doc.BlobID,
		WebURL: p.blobURL(view.Repo, view.Branch, doc.Path), Stale: view.Stale,
	}, nil
}

func (p *Portal) Search(ctx context.Context, repoID, query string, limit int) ([]index.SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, invalid("query is empty")
	}
	if len(query) > maxQueryLength {
		return nil, invalid("query is longer than %d characters", maxQueryLength)
	}
	if limit <= 0 || limit > maxSearchResults {
		limit = defaultSearchResults
	}
	results, err := p.index.Search(ctx, repoID, query, limit)
	if errors.Is(err, index.ErrUnknownRepo) {
		return nil, fmt.Errorf("%w: repository %s", ErrNotFound, repoID)
	}
	return results, err
}

func (p *Portal) Refresh(ctx context.Context, repoID string) (RepoSummary, error) {
	repo, ok := p.index.Repo(repoID)
	if !ok {
		return RepoSummary{}, fmt.Errorf("%w: repository %s", ErrNotFound, repoID)
	}
	if _, err := p.index.Refresh(ctx, repoID); err != nil {
		return RepoSummary{}, err
	}
	return p.summarize(ctx, repo), nil
}

func (p *Portal) view(ctx context.Context, repoID string) (index.View, error) {
	view, err := p.index.Get(ctx, repoID)
	if errors.Is(err, index.ErrUnknownRepo) {
		return index.View{}, fmt.Errorf("%w: repository %s", ErrNotFound, repoID)
	}
	return view, err
}

const (
	maxQueryLength       = 200
	maxSearchResults     = 50
	defaultSearchResults = 20
	maxImageBytes        = 8 << 20
	maxImagePathBytes    = 512
)

type Image struct {
	Content     []byte
	ContentType string
	ETag        string
}

func (p *Portal) Image(ctx context.Context, repoID, filePath, ifNoneMatch string) (Image, bool, error) {
	repo, ok := p.index.Repo(repoID)
	if !ok {
		return Image{}, false, fmt.Errorf("%w: repository %s", ErrNotFound, repoID)
	}
	if err := validateImagePath(filePath); err != nil {
		return Image{}, false, err
	}
	view, err := p.view(ctx, repoID)
	if err != nil {
		return Image{}, false, err
	}

	key := repoID + "\x00" + filePath
	if cached, ok := p.images.Get(key); ok {
		return cached, cached.ETag == ifNoneMatch && ifNoneMatch != "", nil
	}

	file, err := p.files.File(ctx, repo.Project, view.Branch, filePath)
	if errors.Is(err, gitlab.ErrNotFound) {
		return Image{}, false, fmt.Errorf("%w: image %s", ErrNotFound, filePath)
	}
	if err != nil {
		return Image{}, false, err
	}
	if len(file.Content) > maxImageBytes {
		return Image{}, false, invalid("image is larger than %d bytes", maxImageBytes)
	}
	image := Image{
		Content:     file.Content,
		ContentType: docs.ImageContentTypes[strings.ToLower(path.Ext(filePath))],
		ETag:        `"` + file.BlobID + `"`,
	}
	p.images.Put(key, image)
	return image, image.ETag == ifNoneMatch && ifNoneMatch != "", nil
}

func validateImagePath(filePath string) error {
	if filePath == "" || len(filePath) > maxImagePathBytes {
		return invalid("image path is empty or too long")
	}
	if !docs.IsImage(filePath) {
		return invalid("only png, jpg, gif, webp and avif images are served")
	}
	if strings.HasPrefix(filePath, "/") || strings.ContainsRune(filePath, '\\') || strings.ContainsRune(filePath, 0) {
		return invalid("image path must be relative")
	}
	for _, segment := range strings.Split(filePath, "/") {
		if segment == "" || segment == "." || segment == ".." || strings.HasPrefix(segment, ".") {
			return invalid("image path contains forbidden segments")
		}
	}
	return nil
}

type ImageCache struct {
	mu         sync.Mutex
	entries    map[string]*imageEntry
	order      []string
	maxEntries int
	maxBytes   int
	bytes      int
	ttl        time.Duration
	now        func() time.Time
}

type imageEntry struct {
	image   Image
	expires time.Time
}

const (
	defaultImageEntries = 200
	defaultImageBytes   = 64 << 20
	defaultImageTTL     = 5 * time.Minute
)

func NewImageCache(maxEntries, maxBytes int, ttl time.Duration, now func() time.Time) *ImageCache {
	return &ImageCache{entries: make(map[string]*imageEntry), maxEntries: maxEntries, maxBytes: maxBytes, ttl: ttl, now: now}
}

func (c *ImageCache) Get(key string) (Image, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return Image{}, false
	}
	if !c.now().Before(entry.expires) {
		c.remove(key)
		return Image{}, false
	}
	return entry.image, true
}

func (c *ImageCache) Put(key string, image Image) {
	if len(image.Content) > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; exists {
		c.remove(key)
	}
	c.entries[key] = &imageEntry{image: image, expires: c.now().Add(c.ttl)}
	c.order = append(c.order, key)
	c.bytes += len(image.Content)
	for (len(c.entries) > c.maxEntries || c.bytes > c.maxBytes) && len(c.order) > 0 {
		c.remove(c.order[0])
	}
}

func (c *ImageCache) remove(key string) {
	entry, ok := c.entries[key]
	if !ok {
		return
	}
	c.bytes -= len(entry.image.Content)
	delete(c.entries, key)
	for i, candidate := range c.order {
		if candidate == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

func (c *ImageCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}
