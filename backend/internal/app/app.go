package app

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"vibefluence/internal/api"
	"vibefluence/internal/config"
	"vibefluence/internal/drafts"
	"vibefluence/internal/gitlab"
	"vibefluence/internal/index"
	"vibefluence/internal/mcpserver"
	"vibefluence/internal/portal"
	"vibefluence/internal/publish"
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
	mcp     http.Handler
	checks  []server.ReadinessCheck

	writeRatePerMinute int
}

type settings struct {
	publishGrace    time.Duration
	publishInterval time.Duration
	version         string
}

type Option func(*settings)

func WithPublishTiming(grace, interval time.Duration) Option {
	return func(s *settings) {
		s.publishGrace, s.publishInterval = grace, interval
	}
}

func WithVersion(version string) Option {
	return func(s *settings) {
		s.version = version
	}
}

func New(cfg *config.Config, logger *slog.Logger, options ...Option) (*App, error) {
	var tuning settings
	for _, option := range options {
		option(&tuning)
	}
	if cfg.Server.DataDir == "" {
		return nil, fmt.Errorf("the data directory is not configured")
	}
	store, err := drafts.NewStore(cfg.Server.DataDir, nil)
	if err != nil {
		return nil, err
	}

	repos := make([]index.Repo, 0, len(cfg.Repositories))
	repoSettings := make(map[string]portal.RepoSettings, len(cfg.Repositories))
	for _, repo := range cfg.Repositories {
		repos = append(repos, index.Repo{
			ID: repo.ID, Name: repo.Name, Project: repo.Project, Branch: repo.Branch, Paths: repo.Paths,
		})
		repoSettings[repo.ID] = portal.RepoSettings{AutoMerge: repo.AutoMergeEnabled()}
	}

	var client *gitlab.Client
	var checks []server.ReadinessCheck
	if len(repos) > 0 {
		created, clientErr := gitlab.New(gitlab.Options{BaseURL: cfg.GitLab.BaseURL, Token: cfg.GitLab.Token})
		if clientErr != nil {
			return nil, fmt.Errorf("create GitLab client: %w", clientErr)
		}
		client = created
		checks = append(checks, server.ReadinessCheck{Name: "gitlab", Check: cachedCheck(botUserProbe(client.CurrentUser, cfg.GitLab.BotUsername, logger), time.Now)})
	}

	manager := index.NewManager(index.Options{Repos: repos, Source: sourceOrNil(client), Logger: logger})
	var publisher portal.Publisher
	if client != nil {
		publisher = publish.New(publish.Options{
			Client:         client,
			Invalidate:     manager.InvalidateProject,
			Logger:         logger,
			PipelineGrace:  tuning.publishGrace,
			StatusInterval: tuning.publishInterval,
		})
	}
	service := portal.New(portal.Options{
		Index:     manager,
		Files:     filesOrNil(client),
		GitLabURL: cfg.GitLab.BaseURL,
		Settings:  repoSettings,
		Logger:    logger,
		Drafts:    store,
		Publisher: publisher,
	})
	apiHandler := api.New(api.Options{
		Service:       service,
		Write:         service,
		Logger:        logger,
		Invalidate:    manager.InvalidateProject,
		WebhookSecret: cfg.GitLab.WebhookSecret,
	})
	var mcpHandler http.Handler
	if cfg.MCP.IsEnabled() {
		mcpHandler = mcpserver.Handler(mcpserver.New(mcpserver.Options{
			Service: service, Version: tuning.version, AllowPublish: cfg.MCP.AllowPublish,
		}))
	}
	return &App{
		Portal: service, Index: manager, Logger: logger,
		handler: apiHandler, mcp: mcpHandler, checks: checks,
		writeRatePerMinute: cfg.Server.WriteRatePerMinute(),
	}, nil
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
		MCP:    a.mcp,

		WriteRatePerMinute: a.writeRatePerMinute,
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

func botUserProbe(probe func(context.Context) (gitlab.User, error), botUsername string, logger *slog.Logger) func(context.Context) (gitlab.User, error) {
	var warned atomic.Bool
	return func(ctx context.Context) (gitlab.User, error) {
		user, err := probe(ctx)
		if err == nil && user.Username != botUsername && warned.CompareAndSwap(false, true) {
			logger.WarnContext(ctx, "the GitLab token belongs to a different user than gitlab.botUsername; CI rules keyed on the bot user will not match",
				slog.String("token_user", user.Username),
				slog.String("bot_username", botUsername),
			)
		}
		return user, err
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
