package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	defaultAddr            = ":8080"
	defaultDataDir         = "./data"
	defaultShutdownTimeout = 15 * time.Second
	defaultWriteRateLimit  = 120
	defaultLogLevel        = "info"
	defaultLogFormat       = "json"
	defaultDocsPath        = "docs"
	defaultBotUsername     = "vibefluence-bot"
)

var repositoryIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

type Config struct {
	Server       Server       `yaml:"server"`
	Log          Log          `yaml:"log"`
	GitLab       GitLab       `yaml:"gitlab"`
	Repositories []Repository `yaml:"repositories"`
	MCP          MCP          `yaml:"mcp"`
}

type MCP struct {
	Enabled      *bool `yaml:"enabled"`
	AllowPublish bool  `yaml:"allowPublish"`
}

func (m MCP) IsEnabled() bool {
	return m.Enabled == nil || *m.Enabled
}

type Server struct {
	Addr            string        `yaml:"addr"`
	DataDir         string        `yaml:"dataDir"`
	ShutdownTimeout time.Duration `yaml:"shutdownTimeout"`
	WriteRateLimit  *int          `yaml:"writeRateLimitPerMinute"`
}

func (s Server) WriteRatePerMinute() int {
	if s.WriteRateLimit == nil {
		return defaultWriteRateLimit
	}
	return *s.WriteRateLimit
}

type Log struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type GitLab struct {
	BaseURL     string `yaml:"baseUrl"`
	BotUsername string `yaml:"botUsername"`
	Token       string `yaml:"-"`

	WebhookSecret string `yaml:"-"`
}

type Repository struct {
	ID        string   `yaml:"id"`
	Name      string   `yaml:"name"`
	Project   string   `yaml:"project"`
	Branch    string   `yaml:"branch"`
	Paths     []string `yaml:"paths"`
	AutoMerge *bool    `yaml:"autoMerge"`
}

func (r Repository) AutoMergeEnabled() bool {
	return r.AutoMerge == nil || *r.AutoMerge
}

func Load(filePath string, getenv func(string) string) (*Config, error) {
	cfg := &Config{}
	if filePath != "" {
		raw, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
		if err := decode(raw, cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", filePath, err)
		}
	}
	cfg.applyEnv(getenv)
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}

func decode(raw []byte, cfg *Config) error {
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func (c *Config) applyEnv(getenv func(string) string) {
	overrides := []struct {
		name   string
		target *string
	}{
		{"VIBEFLUENCE_ADDR", &c.Server.Addr},
		{"VIBEFLUENCE_DATA_DIR", &c.Server.DataDir},
		{"VIBEFLUENCE_LOG_LEVEL", &c.Log.Level},
		{"VIBEFLUENCE_LOG_FORMAT", &c.Log.Format},
		{"VIBEFLUENCE_GITLAB_BASE_URL", &c.GitLab.BaseURL},
		{"VIBEFLUENCE_GITLAB_TOKEN", &c.GitLab.Token},
		{"VIBEFLUENCE_GITLAB_WEBHOOK_SECRET", &c.GitLab.WebhookSecret},
	}
	for _, override := range overrides {
		if value := strings.TrimSpace(getenv(override.name)); value != "" {
			*override.target = value
		}
	}
}

func (c *Config) applyDefaults() {
	if c.Server.Addr == "" {
		c.Server.Addr = defaultAddr
	}
	if c.Server.DataDir == "" {
		c.Server.DataDir = defaultDataDir
	}
	if c.Server.ShutdownTimeout == 0 {
		c.Server.ShutdownTimeout = defaultShutdownTimeout
	}
	if c.Log.Level == "" {
		c.Log.Level = defaultLogLevel
	}
	if c.Log.Format == "" {
		c.Log.Format = defaultLogFormat
	}
	if c.GitLab.BotUsername == "" {
		c.GitLab.BotUsername = defaultBotUsername
	}
	c.GitLab.BaseURL = strings.TrimRight(c.GitLab.BaseURL, "/")
	for i := range c.Repositories {
		repo := &c.Repositories[i]
		if repo.Name == "" {
			repo.Name = repo.ID
		}
		if len(repo.Paths) == 0 {
			repo.Paths = []string{defaultDocsPath}
		}
		for j, p := range repo.Paths {
			repo.Paths[j] = path.Clean(strings.TrimSpace(p))
		}
	}
}

func (c *Config) Validate() error {
	var errs []error
	errs = append(errs, c.validateServer()...)
	errs = append(errs, c.validateLog()...)
	errs = append(errs, c.validateRepositories()...)
	return errors.Join(errs...)
}

func (c *Config) validateServer() []error {
	var errs []error
	if c.Server.ShutdownTimeout < 0 {
		errs = append(errs, errors.New("server.shutdownTimeout must not be negative"))
	}
	return errs
}

func (c *Config) validateLog() []error {
	var errs []error
	switch strings.ToLower(c.Log.Level) {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("log.level %q is not one of debug, info, warn, error", c.Log.Level))
	}
	switch strings.ToLower(c.Log.Format) {
	case "json", "text":
	default:
		errs = append(errs, fmt.Errorf("log.format %q is not one of json, text", c.Log.Format))
	}
	return errs
}

func (c *Config) validateRepositories() []error {
	if len(c.Repositories) == 0 {
		return nil
	}
	var errs []error
	errs = append(errs, c.validateGitLab()...)
	seen := make(map[string]struct{}, len(c.Repositories))
	for i, repo := range c.Repositories {
		label := fmt.Sprintf("repositories[%d]", i)
		if !repositoryIDPattern.MatchString(repo.ID) {
			errs = append(errs, fmt.Errorf("%s.id %q must match %s", label, repo.ID, repositoryIDPattern))
		} else if _, duplicate := seen[repo.ID]; duplicate {
			errs = append(errs, fmt.Errorf("%s.id %q is duplicated", label, repo.ID))
		}
		seen[repo.ID] = struct{}{}
		if strings.Trim(repo.Project, "/") == "" || strings.Contains(repo.Project, " ") {
			errs = append(errs, fmt.Errorf("%s.project must be a GitLab project path like group/name", label))
		}
		pathsSeen := make(map[string]struct{}, len(repo.Paths))
		for _, p := range repo.Paths {
			if err := validateDocsPath(p); err != nil {
				errs = append(errs, fmt.Errorf("%s.paths: %w", label, err))
				continue
			}
			if _, duplicate := pathsSeen[p]; duplicate {
				errs = append(errs, fmt.Errorf("%s.paths: %q is listed twice", label, p))
			}
			pathsSeen[p] = struct{}{}
		}
	}
	return errs
}

func (c *Config) validateGitLab() []error {
	var errs []error
	parsed, err := url.Parse(c.GitLab.BaseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		errs = append(errs, fmt.Errorf("gitlab.baseUrl %q must be an http(s) URL", c.GitLab.BaseURL))
	}
	if c.GitLab.Token == "" {
		errs = append(errs, errors.New("VIBEFLUENCE_GITLAB_TOKEN must be set when repositories are configured"))
	}
	return errs
}

func validateDocsPath(p string) error {
	if p == "" || p == "." {
		return errors.New("path must not be empty or the repository root")
	}
	if path.IsAbs(p) {
		return fmt.Errorf("path %q must be relative", p)
	}
	if p == ".." || strings.HasPrefix(p, "../") {
		return fmt.Errorf("path %q must not leave the repository", p)
	}
	return nil
}
