package app

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"vibefluence/internal/api"
	"vibefluence/internal/config"
	"vibefluence/internal/gitlab"
	"vibefluence/internal/index"
	"vibefluence/internal/portal"
	"vibefluence/internal/server"
)

const (
	readinessSuccessTTL = 30 * time.Second
	readinessFailureTTL = 5 * time.Second
)

type App struct {
	Portal *portal.Portal
	Index  *index.Manager
	Logger *slog.Logger

	handler http.Handler
	checks  []server.ReadinessCheck
}

func New(cfg *config.Config, logger *slog.Logger) (*App, error) {
	repos := make([]index.Repo, 0, len(cfg.Repositories))
	settings := make(map[string]portal.RepoSettings, len(cfg.Repositories))
	for _, repo := range cfg.Repositories {
		repos = append(repos, index.Repo{
			ID: repo.ID, Name: repo.Name, Project: repo.Project, Branch: repo.Branch, Paths: repo.Paths,
		})
		settings[repo.ID] = portal.RepoSettings{AutoMerge: repo.AutoMergeEnabled()}
	}

	var client *gitlab.Client
	var checks []server.ReadinessCheck
	if len(repos) > 0 {
		created, err := gitlab.New(gitlab.Options{BaseURL: cfg.GitLab.BaseURL, Token: cfg.GitLab.Token})
		if err != nil {
			return nil, fmt.Errorf("create GitLab client: %w", err)
		}
		client = created
		checks = append(checks, server.ReadinessCheck{Name: "gitlab", Check: cachedCheck(client.CurrentUser, time.Now)})
	}

	manager := index.NewManager(index.Options{Repos: repos, Source: sourceOrNil(client), Logger: logger})
	service := portal.New(portal.Options{
		Index:     manager,
		Files:     filesOrNil(client),
		GitLabURL: cfg.GitLab.BaseURL,
		Settings:  settings,
		Logger:    logger,
	})
	apiHandler := api.New(api.Options{
		Service:       service,
		Logger:        logger,
		Invalidate:    manager.InvalidateProject,
		WebhookSecret: cfg.GitLab.WebhookSecret,
	})
	return &App{Portal: service, Index: manager, Logger: logger, handler: apiHandler, checks: checks}, nil
}

func sourceOrNil(client *gitlab.Client) index.GitLab {
	if client == nil {
		return unavailableSource{}
	}
	return client
}

func filesOrNil(client *gitlab.Client) portal.FileReader {
	if client == nil {
		return unavailableSource{}
	}
	return client
}

func (a *App) Handler(assets fs.FS) http.Handler {
	return server.NewHandler(server.Options{
		Logger: a.Logger,
		Assets: assets,
		Checks: a.checks,
		API:    a.handler,
	})
}

func cachedCheck(probe func(context.Context) (gitlab.User, error), now func() time.Time) func(context.Context) error {
	var (
		mu        sync.Mutex
		checkedAt time.Time
		lastErr   error
		checked   bool
	)
	return func(ctx context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		ttl := readinessSuccessTTL
		if lastErr != nil {
			ttl = readinessFailureTTL
		}
		if checked && now().Sub(checkedAt) < ttl {
			return lastErr
		}
		_, lastErr = probe(ctx)
		checkedAt, checked = now(), true
		return lastErr
	}
}

type unavailableSource struct{}

var errNoGitLab = fmt.Errorf("no GitLab repositories are configured")

func (unavailableSource) Project(context.Context, string) (gitlab.Project, error) {
	return gitlab.Project{}, errNoGitLab
}

func (unavailableSource) Tree(context.Context, string, string, string) ([]gitlab.TreeEntry, error) {
	return nil, errNoGitLab
}

func (unavailableSource) Blob(context.Context, string, string) ([]byte, error) {
	return nil, errNoGitLab
}

func (unavailableSource) Branch(context.Context, string, string) (gitlab.Branch, error) {
	return gitlab.Branch{}, errNoGitLab
}

func (unavailableSource) File(context.Context, string, string, string) (gitlab.File, error) {
	return gitlab.File{}, errNoGitLab
}
