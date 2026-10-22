package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestLoadWithoutFileUsesDefaults(t *testing.T) {
	cfg, err := Load("", env(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Addr != ":8080" || cfg.Server.DataDir != "./data" {
		t.Errorf("unexpected server defaults: %+v", cfg.Server)
	}
	if cfg.Server.ShutdownTimeout != 15*time.Second {
		t.Errorf("shutdown timeout = %v", cfg.Server.ShutdownTimeout)
	}
	if cfg.Log.Level != "info" || cfg.Log.Format != "json" {
		t.Errorf("unexpected log defaults: %+v", cfg.Log)
	}
	if cfg.GitLab.BotUsername != "vibefluence-bot" {
		t.Errorf("bot username = %q", cfg.GitLab.BotUsername)
	}
	if len(cfg.Repositories) != 0 {
		t.Errorf("repositories = %v", cfg.Repositories)
	}
}

func TestLoadFullConfig(t *testing.T) {
	file := writeConfig(t, `
server:
  addr: ":9090"
  dataDir: /var/lib/vibefluence
  shutdownTimeout: 30s
log:
  level: debug
  format: text
gitlab:
  baseUrl: https://gitlab.example.com/
  botUsername: docs-bot
repositories:
  - id: backend
    name: Backend
    project: group/backend
    branch: master
    paths: [docs, ./specs/]
    autoMerge: false
  - id: web
    project: group/web
`)
	cfg, err := Load(file, env(map[string]string{"VIBEFLUENCE_GITLAB_TOKEN": "secret", "VIBEFLUENCE_GITLAB_WEBHOOK_SECRET": "hook"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Addr != ":9090" || cfg.Server.ShutdownTimeout != 30*time.Second {
		t.Errorf("server = %+v", cfg.Server)
	}
	if cfg.GitLab.BaseURL != "https://gitlab.example.com" {
		t.Errorf("base url = %q", cfg.GitLab.BaseURL)
	}
	if cfg.GitLab.Token != "secret" {
		t.Errorf("token not taken from env")
	}
	if cfg.GitLab.WebhookSecret != "hook" {
		t.Errorf("webhook secret = %q", cfg.GitLab.WebhookSecret)
	}
	backend := cfg.Repositories[0]
	if got := strings.Join(backend.Paths, ","); got != "docs,specs" {
		t.Errorf("paths = %q", got)
	}
	if backend.AutoMergeEnabled() {
		t.Error("autoMerge should be disabled")
	}
	web := cfg.Repositories[1]
	if web.Name != "web" {
		t.Errorf("name should default to id, got %q", web.Name)
	}
	if got := strings.Join(web.Paths, ","); got != "docs" {
		t.Errorf("default paths = %q", got)
	}
	if !web.AutoMergeEnabled() {
		t.Error("autoMerge should default to enabled")
	}
}

func TestEnvOverridesFile(t *testing.T) {
	file := writeConfig(t, "server:\n  addr: \":9090\"\nlog:\n  level: warn\n")
	cfg, err := Load(file, env(map[string]string{
		"VIBEFLUENCE_ADDR":      "127.0.0.1:7000",
		"VIBEFLUENCE_LOG_LEVEL": " debug ",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Addr != "127.0.0.1:7000" {
		t.Errorf("addr = %q", cfg.Server.Addr)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("level = %q", cfg.Log.Level)
	}
}

func TestEmptyFileIsValid(t *testing.T) {
	if _, err := Load(writeConfig(t, ""), env(nil)); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	file := writeConfig(t, "server:\n  listenAt: \":1\"\n")
	if _, err := Load(file, env(nil)); err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.yaml"), env(nil)); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestValidation(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		env     map[string]string
		wantErr string
	}{
		{
			name:    "bad log level",
			yaml:    "log:\n  level: loud\n",
			wantErr: "log.level",
		},
		{
			name:    "bad log format",
			yaml:    "log:\n  format: xml\n",
			wantErr: "log.format",
		},
		{
			name:    "negative shutdown timeout",
			yaml:    "server:\n  shutdownTimeout: -1s\n",
			wantErr: "shutdownTimeout",
		},
		{
			name:    "repositories need token",
			yaml:    "gitlab:\n  baseUrl: https://g.example.com\nrepositories:\n  - id: a\n    project: g/a\n",
			wantErr: "VIBEFLUENCE_GITLAB_TOKEN",
		},
		{
			name:    "repositories need base url",
			yaml:    "repositories:\n  - id: a\n    project: g/a\n",
			env:     map[string]string{"VIBEFLUENCE_GITLAB_TOKEN": "t"},
			wantErr: "gitlab.baseUrl",
		},
		{
			name:    "base url must be http",
			yaml:    "gitlab:\n  baseUrl: ftp://g.example.com\nrepositories:\n  - id: a\n    project: g/a\n",
			env:     map[string]string{"VIBEFLUENCE_GITLAB_TOKEN": "t"},
			wantErr: "gitlab.baseUrl",
		},
		{
			name:    "invalid repository id",
			yaml:    "gitlab:\n  baseUrl: https://g.example.com\nrepositories:\n  - id: Bad_ID\n    project: g/a\n",
			env:     map[string]string{"VIBEFLUENCE_GITLAB_TOKEN": "t"},
			wantErr: "repositories[0].id",
		},
		{
			name:    "duplicate repository id",
			yaml:    "gitlab:\n  baseUrl: https://g.example.com\nrepositories:\n  - id: a\n    project: g/a\n  - id: a\n    project: g/b\n",
			env:     map[string]string{"VIBEFLUENCE_GITLAB_TOKEN": "t"},
			wantErr: "duplicated",
		},
		{
			name:    "missing project",
			yaml:    "gitlab:\n  baseUrl: https://g.example.com\nrepositories:\n  - id: a\n",
			env:     map[string]string{"VIBEFLUENCE_GITLAB_TOKEN": "t"},
			wantErr: "project",
		},
		{
			name:    "absolute docs path",
			yaml:    "gitlab:\n  baseUrl: https://g.example.com\nrepositories:\n  - id: a\n    project: g/a\n    paths: [/etc]\n",
			env:     map[string]string{"VIBEFLUENCE_GITLAB_TOKEN": "t"},
			wantErr: "relative",
		},
		{
			name:    "docs path escaping repository",
			yaml:    "gitlab:\n  baseUrl: https://g.example.com\nrepositories:\n  - id: a\n    project: g/a\n    paths: [../secrets]\n",
			env:     map[string]string{"VIBEFLUENCE_GITLAB_TOKEN": "t"},
			wantErr: "leave the repository",
		},
		{
			name:    "duplicate docs path",
			yaml:    "gitlab:\n  baseUrl: https://g.example.com\nrepositories:\n  - id: a\n    project: g/a\n    paths: [docs, ./docs/]\n",
			env:     map[string]string{"VIBEFLUENCE_GITLAB_TOKEN": "t"},
			wantErr: "listed twice",
		},
		{
			name:    "docs path equal to repository root",
			yaml:    "gitlab:\n  baseUrl: https://g.example.com\nrepositories:\n  - id: a\n    project: g/a\n    paths: [.]\n",
			env:     map[string]string{"VIBEFLUENCE_GITLAB_TOKEN": "t"},
			wantErr: "repository root",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tt.yaml), env(tt.env))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidationReportsAllProblems(t *testing.T) {
	file := writeConfig(t, "log:\n  level: loud\n  format: xml\n")
	_, err := Load(file, env(nil))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"log.level", "log.format"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestMCPSettings(t *testing.T) {
	cfg, err := Load("", env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.MCP.IsEnabled() || cfg.MCP.AllowPublish {
		t.Errorf("defaults: enabled = %v, publish = %v", cfg.MCP.IsEnabled(), cfg.MCP.AllowPublish)
	}

	file := writeConfig(t, "mcp:\n  enabled: false\n  allowPublish: true\n")
	cfg, err = Load(file, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MCP.IsEnabled() || !cfg.MCP.AllowPublish {
		t.Errorf("explicit: enabled = %v, publish = %v", cfg.MCP.IsEnabled(), cfg.MCP.AllowPublish)
	}
}

func TestWriteRateLimitDefaultsAndOverrides(t *testing.T) {
	var cfg Config
	if got := cfg.Server.WriteRatePerMinute(); got != 120 {
		t.Errorf("default = %d", got)
	}
	disabled := -1
	cfg.Server.WriteRateLimit = &disabled
	if got := cfg.Server.WriteRatePerMinute(); got != -1 {
		t.Errorf("override = %d", got)
	}
}
