package fakegitlab

import "strings"

const DemoProject = "demo/platform"

var demoFiles = map[string]string{
	"README.md":   "# Platform\n",
	"src/main.go": "package main\n",
	"docs/README.md": `---
title: Platform docs
status: published
---

# Platform docs

Welcome. This documentation lives next to the code.
`,
	"docs/architecture/overview.md": `---
title: Architecture overview
status: published
type: adr
---

# Architecture overview

The platform is a **modular monolith** with a single deployable. This page shows how the main pieces fit together.

## Components

| Component | Responsibility | Owner |
|:--|:--|:--|
| API gateway | Authentication, routing, rate limiting | Platform |
| Billing | Invoices and payment provider integration | Payments |
| Notifications | Email and push delivery | Growth |

## Request flow

1. The client sends a request to the gateway.
2. The gateway validates the token and forwards the call.
3. The service answers, and the gateway records the metrics.

See the [auth rework](../specs/auth-rework.md) for the planned token changes and the [first ADR](decisions/0001-use-postgres.md) for the storage choice.

## Example

` + "```go" + `
func (s *Server) Handle(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.Allow(r) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	s.next.ServeHTTP(w, r)
}
` + "```" + `

## Checklist

- [x] Gateway in production
- [x] Metrics dashboard
- [ ] Per-tenant rate limits

> Keep services small enough to be owned by one team.

### Operational notes

Everything is deployed with the same pipeline, so a rollback is a single click.
`,
	"docs/guide/getting-started.md": `---
title: Начало работы
type: guide
---

# Начало работы

Этот документ написан по-русски, чтобы проверить заголовки и поиск на кириллице.

## Установка

Склонируйте репозиторий и запустите ` + "`make dev`" + `.

## Настройка окружения

Переменные окружения описаны в [обзоре архитектуры](../architecture/overview.md#components).

### Токены

Токен бота хранится только в окружении сервера.
`,
	"docs/architecture/decisions/0001-use-postgres.md": `---
title: "ADR 0001: use PostgreSQL"
status: published
type: adr
---

# ADR 0001: use PostgreSQL

## Decision

PostgreSQL is the primary datastore.
`,
	"docs/specs/auth-rework.md": `---
title: Auth rework
status: published
type: spec
---

# Auth rework

Replace session cookies with short-lived tokens.
`,
	"docs/specs/draft-rollout.md": `---
title: Rollout plan
status: draft
type: plan
---

# Rollout plan

Work in progress, must not appear in the portal.
`,
	"docs/tickets/PLAT-101.md": "# PLAT-101\n\nNo front matter: treated as published.\n",
}

func SeedDemo(s *Server) {
	s.AddProject(DemoProject, "master", ProjectOptions{
		PipelineBranches: func(branch string) bool { return !strings.HasPrefix(branch, "vibefluence/") },
	})
	for path, content := range demoFiles {
		s.PutFile(DemoProject, "master", path, content)
	}
}
