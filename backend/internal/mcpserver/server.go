package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"vibefluence/internal/drafts"
	"vibefluence/internal/gitlab"
	"vibefluence/internal/index"
	"vibefluence/internal/portal"
)

const (
	serverName       = "vibefluence"
	resourceTemplate = "vibefluence://{repo}/{+path}"
	resourceScheme   = "vibefluence"
	defaultAuthor    = "MCP agent"
)

type Service interface {
	Repos(ctx context.Context) []portal.RepoSummary
	Tree(ctx context.Context, repoID string) (portal.TreeView, error)
	Document(ctx context.Context, repoID, filePath string) (portal.DocumentView, error)
	Search(ctx context.Context, repoID, query string, limit int) ([]index.SearchResult, error)
	ListDrafts(ctx context.Context, repoID string) ([]portal.DraftSummary, error)
	AllDrafts(ctx context.Context) ([]portal.DraftSummary, error)
	GetDraft(ctx context.Context, repoID, id string) (portal.DraftView, error)
	SaveDraft(ctx context.Context, repoID string, input portal.SaveDraftInput) (portal.DraftView, error)
	PublishDraft(ctx context.Context, repoID, id string, input portal.PublishInput) (portal.PublishOutcome, error)
}

type Options struct {
	Service      Service
	Version      string
	AllowPublish bool
}

func New(opts Options) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: opts.Version}, &mcp.ServerOptions{
		Instructions: instructions(opts.AllowPublish),
	})
	tools := &tools{service: opts.Service}

	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_repositories", Title: "List repositories", Annotations: readOnly,
		Description: "List the repositories whose documentation is available, with their ids, projects, branches and document counts.",
	}, tools.listRepositories)
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_documents", Title: "List documents", Annotations: readOnly,
		Description: "List the published documents of a repository. Optionally filter by folder prefix or by the document type from the front matter (spec, adr, ticket, plan, guide).",
	}, tools.listDocuments)
	mcp.AddTool(server, &mcp.Tool{
		Name: "read_document", Title: "Read a document", Annotations: readOnly,
		Description: "Read a published document as raw Markdown including its front matter. The returned blobId is the base version to pass to save_draft when editing it.",
	}, tools.readDocument)
	mcp.AddTool(server, &mcp.Tool{
		Name: "search_documents", Title: "Search documents", Annotations: readOnly,
		Description: "Search the titles, paths and text of published documents. Every word of the query must match. Drafts are never searched.",
	}, tools.searchDocuments)
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_drafts", Title: "List drafts", Annotations: readOnly,
		Description: "List the drafts saved on the server, optionally for one repository. Drafts are working copies that are not in GitLab.",
	}, tools.listDrafts)
	mcp.AddTool(server, &mcp.Tool{
		Name: "read_draft", Title: "Read a draft", Annotations: readOnly,
		Description: "Read the saved draft of a document path as raw Markdown.",
	}, tools.readDraft)
	mcp.AddTool(server, &mcp.Tool{
		Name: "save_draft", Title: "Save a draft", Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: boolPointer(false)},
		Description: "Create or replace the draft of a document path. The draft stays on the server and does not reach GitLab. Put status: draft or status: published in the front matter. Pass baseBlobId from read_document when editing an existing document, and leave it empty for a new one.",
	}, tools.saveDraft)
	if opts.AllowPublish {
		mcp.AddTool(server, &mcp.Tool{
			Name: "publish_document", Title: "Publish a draft", Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPointer(true)},
			Description: "Publish the saved draft of a document path: a branch and a merge request are created and, when the repository allows it, merged into the default branch. Save the draft first.",
		}, tools.publishDocument)
	}

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: resourceTemplate, Name: "document", Title: "Published document", MIMEType: "text/markdown",
		Description: "A published document as Markdown, addressed as vibefluence://<repository id>/<path>.",
	}, tools.readResource)
	return server
}

func Handler(server *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})
}

func instructions(allowPublish bool) string {
	text := "Vibefluence serves the Markdown documentation of GitLab repositories. " +
		"Use list_repositories to find repository ids, list_documents or search_documents to find documents and read_document to read them. " +
		"Documents marked status: draft in their front matter are hidden. " +
		"Write with save_draft: drafts are stored on this server only."
	if allowPublish {
		return text + " Use publish_document to turn a saved draft into a merge request."
	}
	return text + " Publishing is disabled on this server, a person publishes drafts in the web interface."
}

func boolPointer(value bool) *bool { return &value }

type tools struct {
	service Service
}

type repositoryInfo struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Project   string   `json:"project"`
	Branch    string   `json:"branch"`
	Paths     []string `json:"paths"`
	DocCount  int      `json:"documentCount"`
	Available bool     `json:"available"`
	AutoMerge bool     `json:"autoMerge"`
}

type listRepositoriesOutput struct {
	Repositories []repositoryInfo `json:"repositories"`
}

type noInput struct{}

func (t *tools) listRepositories(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, listRepositoriesOutput, error) {
	summaries := t.service.Repos(ctx)
	out := listRepositoriesOutput{Repositories: make([]repositoryInfo, 0, len(summaries))}
	for _, summary := range summaries {
		out.Repositories = append(out.Repositories, repositoryInfo{
			ID: summary.ID, Name: summary.Name, Project: summary.Project, Branch: summary.Branch, Paths: summary.Paths,
			DocCount: summary.DocCount, Available: summary.Available, AutoMerge: summary.AutoMerge,
		})
	}
	return nil, out, nil
}

type listDocumentsInput struct {
	Repo   string `json:"repo" jsonschema:"the repository id from list_repositories"`
	Folder string `json:"folder,omitempty" jsonschema:"only documents whose path starts with this folder, for example docs/specs"`
	Type   string `json:"type,omitempty" jsonschema:"only documents of this front matter type, for example adr"`
}

type documentInfo struct {
	Path  string `json:"path"`
	Title string `json:"title"`
	Type  string `json:"type,omitempty"`
}

type listDocumentsOutput struct {
	Documents []documentInfo `json:"documents"`
}

func (t *tools) listDocuments(ctx context.Context, _ *mcp.CallToolRequest, input listDocumentsInput) (*mcp.CallToolResult, listDocumentsOutput, error) {
	view, err := t.service.Tree(ctx, input.Repo)
	if err != nil {
		return nil, listDocumentsOutput{}, friendly(err)
	}
	folder := strings.Trim(input.Folder, "/")
	out := listDocumentsOutput{Documents: []documentInfo{}}
	var walk func(nodes []*index.Node)
	walk = func(nodes []*index.Node) {
		for _, node := range nodes {
			if node.Kind == index.KindFolder {
				walk(node.Children)
				continue
			}
			if folder != "" && node.Path != folder && !strings.HasPrefix(node.Path, folder+"/") {
				continue
			}
			if input.Type != "" && !strings.EqualFold(node.Type, input.Type) {
				continue
			}
			out.Documents = append(out.Documents, documentInfo{Path: node.Path, Title: node.Title, Type: node.Type})
		}
	}
	walk(view.Roots)
	return nil, out, nil
}

type readDocumentInput struct {
	Repo string `json:"repo" jsonschema:"the repository id"`
	Path string `json:"path" jsonschema:"the document path, for example docs/specs/auth.md"`
}

type readDocumentOutput struct {
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Title   string `json:"title"`
	Type    string `json:"type,omitempty"`
	Content string `json:"content"`
	BlobID  string `json:"blobId"`
	WebURL  string `json:"webUrl,omitempty"`
}

func (t *tools) readDocument(ctx context.Context, _ *mcp.CallToolRequest, input readDocumentInput) (*mcp.CallToolResult, readDocumentOutput, error) {
	doc, err := t.service.Document(ctx, input.Repo, input.Path)
	if err != nil {
		return nil, readDocumentOutput{}, friendly(err)
	}
	return nil, readDocumentOutput{
		Repo: doc.Repo, Path: doc.Path, Title: doc.Title, Type: doc.Type, Content: doc.Raw, BlobID: doc.BlobID, WebURL: doc.WebURL,
	}, nil
}

type searchInput struct {
	Query string `json:"query" jsonschema:"words to look for, all of them must match"`
	Repo  string `json:"repo,omitempty" jsonschema:"limit the search to one repository id"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum number of results, up to 50"`
}

type searchHit struct {
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Title   string `json:"title"`
	Type    string `json:"type,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

type searchOutput struct {
	Results []searchHit `json:"results"`
}

func (t *tools) searchDocuments(ctx context.Context, _ *mcp.CallToolRequest, input searchInput) (*mcp.CallToolResult, searchOutput, error) {
	results, err := t.service.Search(ctx, input.Repo, input.Query, input.Limit)
	if err != nil {
		return nil, searchOutput{}, friendly(err)
	}
	out := searchOutput{Results: make([]searchHit, 0, len(results))}
	for _, result := range results {
		out.Results = append(out.Results, searchHit{
			Repo: result.Repo, Path: result.Path, Title: result.Title, Type: result.Type, Snippet: result.Snippet,
		})
	}
	return nil, out, nil
}

type listDraftsInput struct {
	Repo string `json:"repo,omitempty" jsonschema:"limit the list to one repository id"`
}

type draftInfo struct {
	Repo            string `json:"repo"`
	Path            string `json:"path"`
	Title           string `json:"title"`
	Status          string `json:"status"`
	IsNew           bool   `json:"isNew"`
	BaseBlobID      string `json:"baseBlobId,omitempty"`
	MergeRequestURL string `json:"mergeRequestUrl,omitempty"`
}

type listDraftsOutput struct {
	Drafts []draftInfo `json:"drafts"`
}

func toDraftInfo(summary portal.DraftSummary) draftInfo {
	return draftInfo{
		Repo: summary.Repo, Path: summary.Path, Title: summary.Title, Status: string(summary.Status), IsNew: summary.IsNew,
		BaseBlobID: summary.BaseBlobID, MergeRequestURL: summary.MergeRequestURL,
	}
}

func (t *tools) listDrafts(ctx context.Context, _ *mcp.CallToolRequest, input listDraftsInput) (*mcp.CallToolResult, listDraftsOutput, error) {
	var summaries []portal.DraftSummary
	var err error
	if input.Repo != "" {
		summaries, err = t.service.ListDrafts(ctx, input.Repo)
	} else {
		summaries, err = t.service.AllDrafts(ctx)
	}
	if err != nil {
		return nil, listDraftsOutput{}, friendly(err)
	}
	out := listDraftsOutput{Drafts: make([]draftInfo, 0, len(summaries))}
	for _, summary := range summaries {
		out.Drafts = append(out.Drafts, toDraftInfo(summary))
	}
	return nil, out, nil
}

type readDraftOutput struct {
	draftInfo
	Content string `json:"content"`
}

func (t *tools) readDraft(ctx context.Context, _ *mcp.CallToolRequest, input readDocumentInput) (*mcp.CallToolResult, readDraftOutput, error) {
	view, err := t.service.GetDraft(ctx, input.Repo, drafts.ID(input.Repo, input.Path))
	if err != nil {
		return nil, readDraftOutput{}, friendly(err)
	}
	return nil, readDraftOutput{draftInfo: toDraftInfo(view.DraftSummary), Content: view.Content}, nil
}

type saveDraftInput struct {
	Repo       string `json:"repo" jsonschema:"the repository id"`
	Path       string `json:"path" jsonschema:"the document path inside a documentation folder, ending in .md"`
	Content    string `json:"content" jsonschema:"the whole document as Markdown, with a front matter block for title, type and status"`
	BaseBlobID string `json:"baseBlobId,omitempty" jsonschema:"the blobId returned by read_document when editing an existing document, empty for a new one"`
	Author     string `json:"author,omitempty" jsonschema:"a name recorded with the draft, defaults to MCP agent"`
}

func (t *tools) saveDraft(ctx context.Context, _ *mcp.CallToolRequest, input saveDraftInput) (*mcp.CallToolResult, draftInfo, error) {
	author := input.Author
	if strings.TrimSpace(author) == "" {
		author = defaultAuthor
	}
	view, err := t.service.SaveDraft(ctx, input.Repo, portal.SaveDraftInput{
		Path: input.Path, Content: input.Content, BaseBlobID: input.BaseBlobID, Author: author,
	})
	if err != nil {
		return nil, draftInfo{}, friendly(err)
	}
	return nil, toDraftInfo(view.DraftSummary), nil
}

type publishInput struct {
	Repo   string `json:"repo" jsonschema:"the repository id"`
	Path   string `json:"path" jsonschema:"the path of a saved draft"`
	Author string `json:"author,omitempty" jsonschema:"a name recorded in the commit, defaults to MCP agent"`
}

type publishOutput struct {
	Outcome         string `json:"outcome"`
	Reason          string `json:"reason,omitempty"`
	Detail          string `json:"detail,omitempty"`
	MergeRequestURL string `json:"mergeRequestUrl,omitempty"`
	Created         bool   `json:"created"`
	Path            string `json:"path"`
}

func (t *tools) publishDocument(ctx context.Context, _ *mcp.CallToolRequest, input publishInput) (*mcp.CallToolResult, publishOutput, error) {
	author := input.Author
	if strings.TrimSpace(author) == "" {
		author = defaultAuthor
	}
	outcome, err := t.service.PublishDraft(ctx, input.Repo, drafts.ID(input.Repo, input.Path), portal.PublishInput{Author: author})
	if err != nil {
		return nil, publishOutput{}, friendly(err)
	}
	return nil, publishOutput{
		Outcome: string(outcome.Outcome), Reason: string(outcome.Reason), Detail: outcome.Detail,
		MergeRequestURL: outcome.MergeRequestURL, Created: outcome.Created, Path: outcome.Path,
	}, nil
}

func (t *tools) readResource(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	uri := request.Params.URI
	repo, filePath, ok := parseResourceURI(uri)
	if !ok {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	doc, err := t.service.Document(ctx, repo, filePath)
	if err != nil {
		if errors.Is(err, portal.ErrNotFound) {
			return nil, mcp.ResourceNotFoundError(uri)
		}
		return nil, friendly(err)
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "text/markdown", Text: doc.Raw}}}, nil
}

func parseResourceURI(raw string) (repo, filePath string, ok bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != resourceScheme || parsed.Host == "" {
		return "", "", false
	}
	filePath = strings.TrimPrefix(parsed.Path, "/")
	if filePath == "" {
		return "", "", false
	}
	return parsed.Host, filePath, true
}

func friendly(err error) error {
	var validation *portal.ValidationError
	var conflict *portal.ConflictError
	var apiErr *gitlab.APIError
	switch {
	case errors.As(err, &validation):
		return errors.New(validation.Message)
	case errors.As(err, &conflict):
		return fmt.Errorf("conflict: %s", conflict.Message)
	case errors.Is(err, portal.ErrNotFound):
		return errors.New("not found: check the repository id and the path")
	case errors.Is(err, portal.ErrBusy):
		return errors.New("this document is being published right now, try again in a moment")
	case errors.As(err, &apiErr), errors.Is(err, gitlab.ErrNotFound), errors.Is(err, gitlab.ErrUnauthorized), errors.Is(err, gitlab.ErrForbidden):
		return errors.New("GitLab is unavailable or rejected the request")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return errors.New("the request was cancelled or timed out")
	}
	return errors.New("internal error")
}
