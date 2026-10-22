package index

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	"vibefluence/internal/docs"
	"vibefluence/internal/gitlab"
)

const (
	defaultTTL        = 60 * time.Second
	failureBackoff    = 10 * time.Second
	fetchConcurrency  = 8
	maxDocuments      = 5000
	maxDocumentBytes  = 2 << 20
	projectInfoMaxAge = 10 * time.Minute
	refreshTimeout    = 2 * time.Minute
)

var (
	ErrUnknownRepo = errors.New("index: unknown repository")
	ErrNotFound    = errors.New("index: document not found")
)

type GitLab interface {
	Project(ctx context.Context, project string) (gitlab.Project, error)
	Tree(ctx context.Context, project, ref, dir string) ([]gitlab.TreeEntry, error)
	Blob(ctx context.Context, project, blobID string) ([]byte, error)
	Branch(ctx context.Context, project, name string) (gitlab.Branch, error)
}

type Options struct {
	Repos  []Repo
	Source GitLab
	TTL    time.Duration
	Logger *slog.Logger
	Now    func() time.Time

	MaxDocuments     int
	MaxDocumentBytes int
}

type Manager struct {
	source GitLab
	repos  []Repo
	byID   map[string]Repo
	ttl    time.Duration
	logger *slog.Logger
	now    func() time.Time
	flight singleflight.Group

	maxDocuments     int
	maxDocumentBytes int

	mu     sync.Mutex
	states map[string]*repoState
	blobs  map[string][]byte
}

type repoState struct {
	snapshot    *Snapshot
	stale       bool
	failedAt    time.Time
	branch      string
	branchAt    time.Time
	lastFailure error
}

type View struct {
	*Snapshot
	Stale bool
}

func NewManager(opts Options) *Manager {
	manager := &Manager{
		source: opts.Source,
		repos:  append([]Repo(nil), opts.Repos...),
		byID:   make(map[string]Repo, len(opts.Repos)),
		ttl:    opts.TTL,
		logger: opts.Logger,
		now:    opts.Now,

		maxDocuments:     opts.MaxDocuments,
		maxDocumentBytes: opts.MaxDocumentBytes,
		states:           make(map[string]*repoState, len(opts.Repos)),
		blobs:            make(map[string][]byte),
	}
	if manager.ttl <= 0 {
		manager.ttl = defaultTTL
	}
	if manager.logger == nil {
		manager.logger = slog.New(slog.DiscardHandler)
	}
	if manager.now == nil {
		manager.now = time.Now
	}
	if manager.maxDocuments <= 0 {
		manager.maxDocuments = maxDocuments
	}
	if manager.maxDocumentBytes <= 0 {
		manager.maxDocumentBytes = maxDocumentBytes
	}
	for _, repo := range manager.repos {
		manager.byID[repo.ID] = repo
		manager.states[repo.ID] = &repoState{}
	}
	return manager
}

func (m *Manager) Repos() []Repo {
	return append([]Repo(nil), m.repos...)
}

func (m *Manager) Repo(id string) (Repo, bool) {
	repo, ok := m.byID[id]
	return repo, ok
}

func (m *Manager) Get(ctx context.Context, repoID string) (View, error) {
	if _, ok := m.byID[repoID]; !ok {
		return View{}, ErrUnknownRepo
	}
	if view, ok := m.fresh(repoID); ok {
		return view, nil
	}

	return m.afterRefresh(repoID, m.refreshShared(ctx, repoID))
}

func (m *Manager) refreshShared(ctx context.Context, repoID string) error {
	result := m.flight.DoChan(repoID, func() (any, error) {
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshTimeout)
		defer cancel()
		return nil, m.refresh(refreshCtx, repoID)
	})
	select {
	case outcome := <-result:
		return outcome.Err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Manager) Refresh(ctx context.Context, repoID string) (View, error) {
	if _, ok := m.byID[repoID]; !ok {
		return View{}, ErrUnknownRepo
	}
	m.mu.Lock()
	m.states[repoID].failedAt = time.Time{}
	m.mu.Unlock()

	return m.afterRefresh(repoID, m.refreshShared(ctx, repoID))
}

func (m *Manager) Invalidate(repoID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if state, ok := m.states[repoID]; ok {
		state.stale = true
		state.failedAt = time.Time{}
	}
}

func (m *Manager) fresh(repoID string) (View, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.states[repoID]
	if state.snapshot == nil {
		return View{}, false
	}
	now := m.now()
	if !state.stale && now.Sub(state.snapshot.LoadedAt) < m.ttl {
		return View{Snapshot: state.snapshot}, true
	}
	if !state.failedAt.IsZero() && now.Sub(state.failedAt) < failureBackoff {
		return View{Snapshot: state.snapshot, Stale: true}, true
	}
	return View{}, false
}

func (m *Manager) afterRefresh(repoID string, refreshErr error) (View, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.states[repoID]
	if state.snapshot == nil {
		if refreshErr == nil {
			refreshErr = errors.New("index: refresh produced no snapshot")
		}
		return View{}, refreshErr
	}
	return View{Snapshot: state.snapshot, Stale: refreshErr != nil}, nil
}

func (m *Manager) refresh(ctx context.Context, repoID string) error {
	repo := m.byID[repoID]
	snapshot, err := m.build(ctx, repo)

	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.states[repoID]
	if err != nil {
		state.failedAt = m.now()
		state.lastFailure = err
		m.logger.WarnContext(ctx, "repository refresh failed",
			slog.String("repo", repoID), slog.Any("error", err), slog.Bool("serving_stale", state.snapshot != nil))
		return err
	}
	state.snapshot = snapshot
	state.stale = false
	state.failedAt = time.Time{}
	state.lastFailure = nil
	m.collectBlobs()
	return nil
}

func (m *Manager) collectBlobs() {
	live := make(map[string]struct{})
	for _, state := range m.states {
		if state.snapshot == nil {
			continue
		}
		for blobID := range state.snapshot.blobIDs {
			live[blobID] = struct{}{}
		}
	}
	for blobID := range m.blobs {
		if _, keep := live[blobID]; !keep {
			delete(m.blobs, blobID)
		}
	}
}

func (m *Manager) resolveBranch(ctx context.Context, repo Repo) (string, error) {
	if repo.Branch != "" {
		return repo.Branch, nil
	}
	m.mu.Lock()
	state := m.states[repo.ID]
	if state.branch != "" && m.now().Sub(state.branchAt) < projectInfoMaxAge {
		branch := state.branch
		m.mu.Unlock()
		return branch, nil
	}
	m.mu.Unlock()

	project, err := m.source.Project(ctx, repo.Project)
	if err != nil {
		return "", fmt.Errorf("resolve default branch of %s: %w", repo.Project, err)
	}
	m.mu.Lock()
	state.branch, state.branchAt = project.DefaultBranch, m.now()
	m.mu.Unlock()
	return project.DefaultBranch, nil
}

type candidate struct {
	path   string
	root   string
	blobID string
}

func (m *Manager) build(ctx context.Context, repo Repo) (*Snapshot, error) {
	branch, err := m.resolveBranch(ctx, repo)
	if err != nil {
		return nil, err
	}
	candidates, err := m.listMarkdown(ctx, repo, branch)
	if err != nil {
		return nil, err
	}
	contents, err := m.fetchBlobs(ctx, repo.Project, candidates)
	if err != nil {
		return nil, err
	}

	var visible []*Doc
	hidden := 0
	blobIDs := make(map[string]struct{}, len(candidates))
	for _, item := range candidates {
		blobIDs[item.blobID] = struct{}{}
		doc := m.buildDoc(repo.ID, item, contents[item.blobID])
		if doc.Status == docs.StatusDraft {
			hidden++
			continue
		}
		visible = append(visible, doc)
	}
	snapshot := newSnapshot(repo, branch, m.now(), visible, hidden)
	snapshot.blobIDs = blobIDs
	return snapshot, nil
}

func (m *Manager) listMarkdown(ctx context.Context, repo Repo, branch string) ([]candidate, error) {
	seen := make(map[string]struct{})
	var found []candidate
	missingRoots := 0
	for _, root := range repo.Paths {
		entries, err := m.source.Tree(ctx, repo.Project, branch, root)
		if errors.Is(err, gitlab.ErrNotFound) {
			missingRoots++
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("list %s/%s: %w", repo.Project, root, err)
		}
		for _, entry := range entries {
			if entry.Type != "blob" || !docs.IsMarkdown(entry.Path) {
				continue
			}
			if _, duplicate := seen[entry.Path]; duplicate {
				continue
			}
			seen[entry.Path] = struct{}{}
			found = append(found, candidate{path: entry.Path, root: root, blobID: entry.ID})
		}
	}
	if len(repo.Paths) > 0 && missingRoots == len(repo.Paths) {
		if err := m.verifyRepository(ctx, repo, branch); err != nil {
			return nil, err
		}
	}
	if len(found) > m.maxDocuments {
		m.logger.WarnContext(ctx, "too many documents, truncating",
			slog.String("repo", repo.ID), slog.Int("found", len(found)), slog.Int("limit", m.maxDocuments))
		sort.Slice(found, func(i, j int) bool { return found[i].path < found[j].path })
		found = found[:m.maxDocuments]
	}
	return found, nil
}

func (m *Manager) verifyRepository(ctx context.Context, repo Repo, branch string) error {
	if _, err := m.source.Project(ctx, repo.Project); err != nil {
		return fmt.Errorf("project %s: %w", repo.Project, err)
	}
	if _, err := m.source.Branch(ctx, repo.Project, branch); err != nil {
		return fmt.Errorf("branch %s of %s: %w", branch, repo.Project, err)
	}
	return nil
}

func (m *Manager) fetchBlobs(ctx context.Context, project string, items []candidate) (map[string][]byte, error) {
	result := make(map[string][]byte, len(items))
	var missing []string

	m.mu.Lock()
	for _, item := range items {
		if cached, ok := m.blobs[item.blobID]; ok {
			result[item.blobID] = cached
			continue
		}
		if _, queued := result[item.blobID]; !queued {
			result[item.blobID] = nil
			missing = append(missing, item.blobID)
		}
	}
	m.mu.Unlock()

	var fetched sync.Map
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(fetchConcurrency)
	for _, blobID := range missing {
		group.Go(func() error {
			content, err := m.source.Blob(groupCtx, project, blobID)
			if err != nil {
				return fmt.Errorf("fetch blob %s: %w", blobID, err)
			}
			fetched.Store(blobID, content)
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, blobID := range missing {
		value, _ := fetched.Load(blobID)
		content, _ := value.([]byte)
		m.blobs[blobID] = content
		result[blobID] = content
	}
	return result, nil
}

func (m *Manager) buildDoc(repoID string, item candidate, content []byte) *Doc {
	if len(content) > m.maxDocumentBytes {
		return &Doc{
			Repo: repoID, Path: item.path, Root: item.root, BlobID: item.blobID,
			Title: docs.Parse(nil, item.path).Title, Status: docs.StatusPublished,
			Warnings: []string{"document is too large to display"},
		}
	}
	parsed := docs.Parse(content, item.path)
	return &Doc{
		Repo: repoID, Path: item.path, Root: item.root, BlobID: item.blobID,
		Title: parsed.Title, Status: parsed.Status, Type: parsed.Type, Warnings: parsed.Warnings,
		Body: parsed.Body, Raw: string(content),
	}
}

func (m *Manager) Search(ctx context.Context, repoID, query string, limit int) ([]SearchResult, error) {
	ids := []string{repoID}
	if repoID == "" {
		ids = ids[:0]
		for _, repo := range m.repos {
			ids = append(ids, repo.ID)
		}
	}
	var results []SearchResult
	for _, id := range ids {
		view, err := m.Get(ctx, id)
		if err != nil {
			if repoID == "" {
				m.logger.WarnContext(ctx, "search skipped repository", slog.String("repo", id), slog.Any("error", err))
				continue
			}
			return nil, err
		}
		results = append(results, view.Search(query, 0)...)
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].Repo != results[j].Repo {
			return results[i].Repo < results[j].Repo
		}
		return results[i].Path < results[j].Path
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (m *Manager) LastFailure(repoID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if state, ok := m.states[repoID]; ok {
		return state.lastFailure
	}
	return nil
}
