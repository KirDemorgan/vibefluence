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

The platform is a modular monolith with a single deployable.
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
