package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"vibefluence/internal/docs"
	"vibefluence/internal/index"
	"vibefluence/internal/markdown"
	"vibefluence/internal/portal"
)

const (
	maxPathLength  = 512
	maxWebhookBody = 1 << 20
)

type Service interface {
	Repos(ctx context.Context) []portal.RepoSummary
	Tree(ctx context.Context, repoID string) (portal.TreeView, error)
	Document(ctx context.Context, repoID, filePath string) (portal.DocumentView, error)
	Search(ctx context.Context, repoID, query string, limit int) ([]index.SearchResult, error)
	Refresh(ctx context.Context, repoID string) (portal.RepoSummary, error)
	Image(ctx context.Context, repoID, filePath, ifNoneMatch string) (portal.Image, bool, error)
}

type Options struct {
	Service       Service
	Logger        *slog.Logger
	Invalidate    func(project, branch string) int
	WebhookSecret string
}

type handler struct {
	service       Service
	logger        *slog.Logger
	invalidate    func(project, branch string) int
	webhookSecret string
}

func New(opts Options) http.Handler {
	h := &handler{service: opts.Service, logger: opts.Logger, invalidate: opts.Invalidate, webhookSecret: opts.WebhookSecret}
	if h.logger == nil {
		h.logger = slog.New(slog.DiscardHandler)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos", h.listRepos)
	mux.HandleFunc("GET /api/v1/repos/{repo}/tree", h.tree)
	mux.HandleFunc("GET /api/v1/repos/{repo}/doc", h.document)
	mux.HandleFunc("GET /api/v1/repos/{repo}/raw", h.raw)
	mux.HandleFunc("POST /api/v1/repos/{repo}/refresh", h.refresh)
	mux.HandleFunc("GET /api/v1/search", h.search)
	mux.HandleFunc("POST /api/v1/webhooks/gitlab", h.webhook)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such API endpoint")
	})
	return mux
}

type repoDTO struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Project   string   `json:"project"`
	Branch    string   `json:"branch"`
	Paths     []string `json:"paths"`
	AutoMerge bool     `json:"autoMerge"`
	DocCount  int      `json:"docCount"`
	Hidden    int      `json:"hiddenCount"`
	Stale     bool     `json:"stale"`
	Available bool     `json:"available"`
	WebURL    string   `json:"webUrl"`
}

func toRepoDTO(summary portal.RepoSummary) repoDTO {
	return repoDTO{
		ID: summary.ID, Name: summary.Name, Project: summary.Project, Branch: summary.Branch, Paths: summary.Paths,
		AutoMerge: summary.AutoMerge, DocCount: summary.DocCount, Hidden: summary.Hidden, Stale: summary.Stale,
		Available: summary.Available, WebURL: summary.WebURL,
	}
}

func (h *handler) listRepos(w http.ResponseWriter, r *http.Request) {
	summaries := h.service.Repos(r.Context())
	repos := make([]repoDTO, 0, len(summaries))
	for _, summary := range summaries {
		repos = append(repos, toRepoDTO(summary))
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": repos})
}

type nodeDTO struct {
	Name     string     `json:"name"`
	Path     string     `json:"path"`
	Kind     string     `json:"kind"`
	Title    string     `json:"title,omitempty"`
	Type     string     `json:"type,omitempty"`
	Children []*nodeDTO `json:"children,omitempty"`
}

func toNodeDTO(node *index.Node) *nodeDTO {
	dto := &nodeDTO{Name: node.Name, Path: node.Path, Kind: string(node.Kind), Title: node.Title, Type: node.Type}
	for _, child := range node.Children {
		dto.Children = append(dto.Children, toNodeDTO(child))
	}
	return dto
}

func (h *handler) tree(w http.ResponseWriter, r *http.Request) {
	view, err := h.service.Tree(r.Context(), r.PathValue("repo"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	roots := make([]*nodeDTO, 0, len(view.Roots))
	for _, root := range view.Roots {
		roots = append(roots, toNodeDTO(root))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"repo": view.Repo, "branch": view.Branch, "stale": view.Stale, "roots": roots,
	})
}

type headingDTO struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	ID    string `json:"id"`
}

type documentDTO struct {
	Repo     string       `json:"repo"`
	Path     string       `json:"path"`
	Title    string       `json:"title"`
	Status   docs.Status  `json:"status"`
	Type     string       `json:"type,omitempty"`
	Warnings []string     `json:"warnings"`
	HTML     string       `json:"html"`
	Headings []headingDTO `json:"headings"`
	Raw      string       `json:"raw"`
	BlobID   string       `json:"blobId"`
	WebURL   string       `json:"webUrl"`
	Stale    bool         `json:"stale"`
}

func toDocumentDTO(view portal.DocumentView) documentDTO {
	headings := make([]headingDTO, 0, len(view.Headings))
	for _, heading := range view.Headings {
		headings = append(headings, toHeadingDTO(heading))
	}
	warnings := view.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	return documentDTO{
		Repo: view.Repo, Path: view.Path, Title: view.Title, Status: view.Status, Type: view.Type, Warnings: warnings,
		HTML: view.HTML, Headings: headings, Raw: view.Raw, BlobID: view.BlobID, WebURL: view.WebURL, Stale: view.Stale,
	}
}

func toHeadingDTO(heading markdown.Heading) headingDTO {
	return headingDTO{Level: heading.Level, Text: heading.Text, ID: heading.ID}
}

func queryPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	filePath := r.URL.Query().Get("path")
	if filePath == "" || len(filePath) > maxPathLength {
		writeError(w, http.StatusBadRequest, "invalid_request", "the path parameter is required and must be shorter than 512 bytes")
		return "", false
	}
	return filePath, true
}

func (h *handler) document(w http.ResponseWriter, r *http.Request) {
	filePath, ok := queryPath(w, r)
	if !ok {
		return
	}
	view, err := h.service.Document(r.Context(), r.PathValue("repo"), filePath)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toDocumentDTO(view))
}

func (h *handler) raw(w http.ResponseWriter, r *http.Request) {
	filePath, ok := queryPath(w, r)
	if !ok {
		return
	}
	image, notModified, err := h.service.Image(r.Context(), r.PathValue("repo"), filePath, r.Header.Get("If-None-Match"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	header := w.Header()
	header.Set("ETag", image.ETag)
	header.Set("Cache-Control", "private, max-age=300")
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if notModified {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	header.Set("Content-Type", image.ContentType)
	header.Set("Content-Length", strconv.Itoa(len(image.Content)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(image.Content)
	}
}

func (h *handler) refresh(w http.ResponseWriter, r *http.Request) {
	summary, err := h.service.Refresh(r.Context(), r.PathValue("repo"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toRepoDTO(summary))
}

type searchResultDTO struct {
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Title   string `json:"title"`
	Type    string `json:"type,omitempty"`
	Snippet string `json:"snippet"`
}

func (h *handler) search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, _ := strconv.Atoi(query.Get("limit"))
	results, err := h.service.Search(r.Context(), query.Get("repo"), query.Get("q"), limit)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	items := make([]searchResultDTO, 0, len(results))
	for _, result := range results {
		items = append(items, searchResultDTO{Repo: result.Repo, Path: result.Path, Title: result.Title, Type: result.Type, Snippet: result.Snippet})
	}
	writeJSON(w, http.StatusOK, map[string]any{"query": strings.TrimSpace(query.Get("q")), "results": items})
}

type pushEvent struct {
	ObjectKind string `json:"object_kind"`
	Ref        string `json:"ref"`
	Project    struct {
		Path string `json:"path_with_namespace"`
	} `json:"project"`
}

func (h *handler) webhook(w http.ResponseWriter, r *http.Request) {
	if h.webhookSecret == "" || h.invalidate == nil {
		writeError(w, http.StatusNotFound, "not_found", "no such API endpoint")
		return
	}
	provided := r.Header.Get("X-Gitlab-Token")
	if subtle.ConstantTimeCompare([]byte(provided), []byte(h.webhookSecret)) != 1 {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid webhook token")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody+1))
	if err != nil || len(body) > maxWebhookBody {
		writeError(w, http.StatusBadRequest, "invalid_request", "the webhook body is unreadable or too large")
		return
	}
	var event pushEvent
	if err := json.Unmarshal(body, &event); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "the webhook body is not valid JSON")
		return
	}
	if event.ObjectKind == "push" && strings.HasPrefix(event.Ref, "refs/heads/") && event.Project.Path != "" {
		h.invalidate(event.Project.Path, strings.TrimPrefix(event.Ref, "refs/heads/"))
	}
	w.WriteHeader(http.StatusNoContent)
}
