GO ?= go
NPM ?= npm
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
FRONTEND_DIST := frontend/dist/vibefluence-frontend/browser
EMBED_DIR := backend/internal/web/dist

.DEFAULT_GOAL := help

.PHONY: help
help:
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: dev
dev: ## Run fake GitLab, backend and frontend dev server together
	$(MAKE) -j3 dev-fakegitlab dev-backend dev-frontend

.PHONY: dev-fakegitlab
dev-fakegitlab: ## Run the fake GitLab with demo data on :8929
	cd backend && $(GO) run ./cmd/fakegitlab

.PHONY: dev-backend
dev-backend: ## Run the backend against the fake GitLab on :8080
	cd backend && VIBEFLUENCE_GITLAB_TOKEN=dev-token $(GO) run ./cmd/vibefluence -config config.dev.yaml

.PHONY: dev-frontend
dev-frontend: ## Run the Angular dev server on :4200 with an API proxy
	cd frontend && $(NPM) start

frontend/node_modules: frontend/package-lock.json
	cd frontend && $(NPM) ci
	touch frontend/node_modules

.PHONY: install
install: frontend/node_modules ## Install frontend dependencies

.PHONY: build-frontend
build-frontend: frontend/node_modules ## Build the frontend and copy it into the backend embed directory
	cd frontend && $(NPM) run build
	find $(EMBED_DIR) -mindepth 1 ! -name .gitkeep -delete
	cp -R $(FRONTEND_DIST)/. $(EMBED_DIR)/

.PHONY: build
build: build-frontend ## Build the single production binary into backend/bin
	cd backend && CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/vibefluence ./cmd/vibefluence

.PHONY: test
test: test-backend test-frontend ## Run all tests

.PHONY: test-backend
test-backend: ## Run backend tests with the race detector
	cd backend && $(GO) test -race -count=1 -cover ./...

.PHONY: test-frontend
test-frontend: ## Run frontend tests with coverage
	cd frontend && $(NPM) run test:ci

.PHONY: lint
lint: lint-backend lint-frontend ## Run all linters

.PHONY: lint-backend
lint-backend: ## gofmt check, go vet and golangci-lint
	@test -z "$$(cd backend && gofmt -l .)" || (cd backend && gofmt -l . && echo "gofmt: files above need formatting" && exit 1)
	cd backend && $(GO) vet ./...
	cd backend && golangci-lint run ./...

.PHONY: lint-frontend
lint-frontend: ## ESLint and Prettier check
	cd frontend && $(NPM) run lint && $(NPM) run format:check

.PHONY: vuln
vuln: ## Scan dependencies for known vulnerabilities
	cd backend && govulncheck ./...
	cd frontend && $(NPM) audit --audit-level=high

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf backend/bin backend/coverage.out frontend/dist frontend/coverage
	find $(EMBED_DIR) -mindepth 1 ! -name .gitkeep -delete
