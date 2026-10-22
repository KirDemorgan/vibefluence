package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"vibefluence/internal/docs"
	"vibefluence/internal/drafts"
	"vibefluence/internal/gitlab"
	"vibefluence/internal/index"
	"vibefluence/internal/portal"
	"vibefluence/internal/publish"
)

type stub struct {
	err        error
	saved      portal.SaveDraftInput
	savedRepo  string
	published  portal.PublishInput
	publishRef [2]string
	draftRef   [2]string
	searchArgs []string
	searchHits []index.SearchResult
}

func (s *stub) Repos(context.Context) []portal.RepoSummary {
	return []portal.RepoSummary{{ID: "platform", Name: "Platform", Project: "demo/platform", Branch: "master", Paths: []string{"docs"}, DocCount: 3, Available: true, AutoMerge: true}}
}

func (s *stub) Tree(_ context.Context, repo string) (portal.TreeView, error) {
	if s.err != nil {
		return portal.TreeView{}, s.err
	}
	return portal.TreeView{Repo: repo, Roots: []*index.Node{{
		Name: "docs", Path: "docs", Kind: index.KindFolder,
		Children: []*index.Node{
			{Name: "README.md", Path: "docs/README.md", Kind: index.KindDoc, Title: "Welcome"},
			{Name: "specs", Path: "docs/specs", Kind: index.KindFolder, Children: []*index.Node{
				{Name: "auth.md", Path: "docs/specs/auth.md", Kind: index.KindDoc, Title: "Auth", Type: "spec"},
				{Name: "0001.md", Path: "docs/specs/0001.md", Kind: index.KindDoc, Title: "ADR 1", Type: "adr"},
			}},
		},
	}}}, nil
}

func (s *stub) Document(_ context.Context, repo, path string) (portal.DocumentView, error) {
	if s.err != nil {
		return portal.DocumentView{}, s.err
	}
	return portal.DocumentView{Repo: repo, Path: path, Title: "Auth", Type: "spec", Raw: "---\ntitle: Auth\n---\n# Auth\n", BlobID: "blob-1", WebURL: "https://gl/x"}, nil
}

func (s *stub) Search(_ context.Context, repo, query string, limit int) ([]index.SearchResult, error) {
	s.searchArgs = []string{repo, query, fmt.Sprint(limit)}
	if s.err != nil {
		return nil, s.err
	}
	return s.searchHits, nil
}

func (s *stub) summary() portal.DraftSummary {
	return portal.DraftSummary{ID: "id", Repo: "platform", Path: "docs/a.md", Title: "A", Status: docs.StatusDraft, IsNew: true, BaseBlobID: "b", MergeRequestURL: "https://gl/mr/1", UpdatedAt: time.Now()}
}

func (s *stub) ListDrafts(_ context.Context, repo string) ([]portal.DraftSummary, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []portal.DraftSummary{s.summary()}, nil
}

func (s *stub) AllDrafts(context.Context) ([]portal.DraftSummary, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []portal.DraftSummary{s.summary(), s.summary()}, nil
}

func (s *stub) GetDraft(_ context.Context, repo, id string) (portal.DraftView, error) {
	s.draftRef = [2]string{repo, id}
	if s.err != nil {
		return portal.DraftView{}, s.err
	}
	return portal.DraftView{DraftSummary: s.summary(), Content: "# A draft"}, nil
}

func (s *stub) SaveDraft(_ context.Context, repo string, input portal.SaveDraftInput) (portal.DraftView, error) {
	s.savedRepo, s.saved = repo, input
	if s.err != nil {
		return portal.DraftView{}, s.err
	}
	return portal.DraftView{DraftSummary: s.summary()}, nil
}

func (s *stub) PublishDraft(_ context.Context, repo, id string, input portal.PublishInput) (portal.PublishOutcome, error) {
	s.publishRef, s.published = [2]string{repo, id}, input
	if s.err != nil {
		return portal.PublishOutcome{}, s.err
	}
	return portal.PublishOutcome{Outcome: publish.OutcomePending, Reason: publish.ReasonAutoMergeDisabled, MergeRequestURL: "https://gl/mr/9", Created: true, Path: "docs/a.md"}, nil
}

func connect(t *testing.T, service Service, allowPublish bool) *mcp.ClientSession {
	t.Helper()
	server := New(Options{Service: service, Version: "test", AllowPublish: allowPublish})
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func call(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return result
}

func decode[T any](t *testing.T, result *mcp.CallToolResult) T {
	t.Helper()
	if result.IsError {
		t.Fatalf("tool returned an error: %v", result.Content)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return out
}

func errorText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if !result.IsError {
		t.Fatalf("expected a tool error, got %+v", result.StructuredContent)
	}
	var text strings.Builder
	for _, content := range result.Content {
		if part, ok := content.(*mcp.TextContent); ok {
			text.WriteString(part.Text)
		}
	}
	return text.String()
}

func toolNames(t *testing.T, session *mcp.ClientSession) []string {
	t.Helper()
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func TestToolsWithoutPublishing(t *testing.T) {
	names := toolNames(t, connect(t, &stub{}, false))
	want := "list_documents,list_drafts,list_repositories,read_document,read_draft,save_draft,search_documents"
	if strings.Join(names, ",") != want {
		t.Errorf("tools = %v", names)
	}
}

func TestPublishingIsAnExplicitOptIn(t *testing.T) {
	names := toolNames(t, connect(t, &stub{}, true))
	if !strings.Contains(strings.Join(names, ","), "publish_document") {
		t.Errorf("tools = %v", names)
	}
}

func TestToolsDeclareTheirSafety(t *testing.T) {
	session := connect(t, &stub{}, true)
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if tool.Annotations == nil || tool.Description == "" || tool.InputSchema == nil {
			t.Errorf("%s lacks annotations, description or schema", tool.Name)
			continue
		}
		readOnly := strings.HasPrefix(tool.Name, "list_") || strings.HasPrefix(tool.Name, "read_") || strings.HasPrefix(tool.Name, "search_")
		if tool.Annotations.ReadOnlyHint != readOnly {
			t.Errorf("%s: readOnlyHint = %v", tool.Name, tool.Annotations.ReadOnlyHint)
		}
	}
}

func TestInstructionsMentionPublishingOnlyWhenAvailable(t *testing.T) {
	if got := instructions(false); !strings.Contains(got, "Publishing is disabled") || strings.Contains(got, "publish_document") {
		t.Errorf("instructions = %q", got)
	}
	if got := instructions(true); !strings.Contains(got, "publish_document") || strings.Contains(got, "disabled") {
		t.Errorf("instructions = %q", got)
	}
}

func TestListRepositories(t *testing.T) {
	out := decode[listRepositoriesOutput](t, call(t, connect(t, &stub{}, false), "list_repositories", nil))
	if len(out.Repositories) != 1 || out.Repositories[0].ID != "platform" || out.Repositories[0].DocCount != 3 || !out.Repositories[0].Available || !out.Repositories[0].AutoMerge {
		t.Errorf("out = %+v", out)
	}
}

func TestListDocumentsWalksTheTreeAndFilters(t *testing.T) {
	session := connect(t, &stub{}, false)
	paths := func(arguments map[string]any) string {
		out := decode[listDocumentsOutput](t, call(t, session, "list_documents", arguments))
		var got []string
		for _, doc := range out.Documents {
			got = append(got, doc.Path)
		}
		return strings.Join(got, ",")
	}
	if got := paths(map[string]any{"repo": "platform"}); got != "docs/README.md,docs/specs/auth.md,docs/specs/0001.md" {
		t.Errorf("all = %s", got)
	}
	if got := paths(map[string]any{"repo": "platform", "folder": "docs/specs/"}); got != "docs/specs/auth.md,docs/specs/0001.md" {
		t.Errorf("folder = %s", got)
	}
	if got := paths(map[string]any{"repo": "platform", "folder": "docs/spec"}); got != "" {
		t.Errorf("a folder prefix must respect path boundaries: %s", got)
	}
	if got := paths(map[string]any{"repo": "platform", "type": "ADR"}); got != "docs/specs/0001.md" {
		t.Errorf("type = %s", got)
	}
	out := decode[listDocumentsOutput](t, call(t, session, "list_documents", map[string]any{"repo": "platform", "type": "nothing"}))
	if out.Documents == nil || len(out.Documents) != 0 {
		t.Errorf("an empty result must be an empty list, got %+v", out.Documents)
	}
}

func TestReadDocumentReturnsRawMarkdownAndTheBaseBlob(t *testing.T) {
	out := decode[readDocumentOutput](t, call(t, connect(t, &stub{}, false), "read_document", map[string]any{"repo": "platform", "path": "docs/specs/auth.md"}))
	if out.Content != "---\ntitle: Auth\n---\n# Auth\n" || out.BlobID != "blob-1" || out.Type != "spec" || out.Path != "docs/specs/auth.md" || out.WebURL == "" {
		t.Errorf("out = %+v", out)
	}
}

func TestSearchDocuments(t *testing.T) {
	service := &stub{searchHits: []index.SearchResult{{Repo: "platform", Path: "docs/a.md", Title: "A", Type: "adr", Snippet: "…x…"}}}
	out := decode[searchOutput](t, call(t, connect(t, service, false), "search_documents", map[string]any{"query": "gateway", "repo": "platform", "limit": 5}))
	if len(out.Results) != 1 || out.Results[0].Snippet != "…x…" || out.Results[0].Type != "adr" {
		t.Errorf("out = %+v", out)
	}
	if strings.Join(service.searchArgs, "|") != "platform|gateway|5" {
		t.Errorf("args = %v", service.searchArgs)
	}
	empty := decode[searchOutput](t, call(t, connect(t, &stub{}, false), "search_documents", map[string]any{"query": "x"}))
	if empty.Results == nil {
		t.Error("no results must be an empty list")
	}
}

func TestListDrafts(t *testing.T) {
	session := connect(t, &stub{}, false)
	all := decode[listDraftsOutput](t, call(t, session, "list_drafts", nil))
	if len(all.Drafts) != 2 {
		t.Errorf("all = %+v", all)
	}
	one := decode[listDraftsOutput](t, call(t, session, "list_drafts", map[string]any{"repo": "platform"}))
	if len(one.Drafts) != 1 || one.Drafts[0].Status != "draft" || !one.Drafts[0].IsNew || one.Drafts[0].MergeRequestURL == "" {
		t.Errorf("one = %+v", one)
	}
}

func TestReadDraftFindsTheDraftOfAPath(t *testing.T) {
	service := &stub{}
	out := decode[readDraftOutput](t, call(t, connect(t, service, false), "read_draft", map[string]any{"repo": "platform", "path": "docs/a.md"}))
	if out.Content != "# A draft" || out.Path != "docs/a.md" {
		t.Errorf("out = %+v", out)
	}
	if service.draftRef != [2]string{"platform", drafts.ID("platform", "docs/a.md")} {
		t.Errorf("draft ref = %v", service.draftRef)
	}
}

func TestSaveDraftDefaultsTheAuthor(t *testing.T) {
	service := &stub{}
	session := connect(t, service, false)
	out := decode[draftInfo](t, call(t, session, "save_draft", map[string]any{"repo": "platform", "path": "docs/a.md", "content": "# A", "baseBlobId": "b"}))
	if out.Path != "docs/a.md" || out.Status != "draft" {
		t.Errorf("out = %+v", out)
	}
	if service.savedRepo != "platform" || service.saved != (portal.SaveDraftInput{Path: "docs/a.md", Content: "# A", BaseBlobID: "b", Author: "MCP agent"}) {
		t.Errorf("saved = %+v in %s", service.saved, service.savedRepo)
	}

	decode[draftInfo](t, call(t, session, "save_draft", map[string]any{"repo": "platform", "path": "docs/a.md", "content": "x", "author": "Review bot"}))
	if service.saved.Author != "Review bot" {
		t.Errorf("author = %q", service.saved.Author)
	}
	decode[draftInfo](t, call(t, session, "save_draft", map[string]any{"repo": "platform", "path": "docs/a.md", "content": "x", "author": "   "}))
	if service.saved.Author != "MCP agent" {
		t.Errorf("blank author = %q", service.saved.Author)
	}
}

func TestPublishDocument(t *testing.T) {
	service := &stub{}
	out := decode[publishOutput](t, call(t, connect(t, service, true), "publish_document", map[string]any{"repo": "platform", "path": "docs/a.md"}))
	if out.Outcome != "pending" || out.Reason != "auto_merge_disabled" || out.MergeRequestURL != "https://gl/mr/9" || !out.Created || out.Path != "docs/a.md" {
		t.Errorf("out = %+v", out)
	}
	if service.publishRef != [2]string{"platform", drafts.ID("platform", "docs/a.md")} || service.published.Author != "MCP agent" {
		t.Errorf("ref = %v, input = %+v", service.publishRef, service.published)
	}
	decode[publishOutput](t, call(t, connect(t, service, true), "publish_document", map[string]any{"repo": "platform", "path": "docs/a.md", "author": "Bot"}))
	if service.published.Author != "Bot" {
		t.Errorf("author = %q", service.published.Author)
	}
}

func TestPublishingIsRefusedWhenNotEnabled(t *testing.T) {
	session := connect(t, &stub{}, false)
	_, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "publish_document", Arguments: map[string]any{"repo": "platform", "path": "docs/a.md"}})
	if err == nil {
		t.Error("an unknown tool must be refused")
	}
}

func TestMissingArgumentsAreRejectedBeforeTheHandler(t *testing.T) {
	service := &stub{}
	session := connect(t, service, false)
	for _, name := range []string{"list_documents", "read_document", "search_documents", "save_draft", "read_draft"} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
		if err == nil && !result.IsError {
			t.Errorf("%s accepted empty arguments: %+v", name, result.StructuredContent)
		}
	}
	if service.savedRepo != "" || len(service.searchArgs) != 0 {
		t.Errorf("the handlers must not run for invalid input: saved %q, search %v", service.savedRepo, service.searchArgs)
	}
}

func TestErrorsAreReportedAsToolErrorsWithoutLeakingDetails(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"not found", fmt.Errorf("%w: document", portal.ErrNotFound), "not found"},
		{"validation", &portal.ValidationError{Message: "path must end with .md"}, "path must end with .md"},
		{"conflict", &portal.ConflictError{Message: "it changed"}, "conflict: it changed"},
		{"busy", fmt.Errorf("%w: x", portal.ErrBusy), "being published"},
		{"gitlab", &gitlab.APIError{Status: 500, Message: "internal-host exploded", Path: "/api/v4/secret"}, "GitLab is unavailable"},
		{"unauthorized", gitlab.ErrUnauthorized, "GitLab is unavailable"},
		{"timeout", context.DeadlineExceeded, "timed out"},
		{"unknown", errors.New("password is hunter2"), "internal error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session := connect(t, &stub{err: tt.err}, true)
			for tool, arguments := range map[string]map[string]any{
				"list_documents":   {"repo": "platform"},
				"read_document":    {"repo": "platform", "path": "docs/a.md"},
				"search_documents": {"query": "x"},
				"list_drafts":      {},
				"read_draft":       {"repo": "platform", "path": "docs/a.md"},
				"save_draft":       {"repo": "platform", "path": "docs/a.md", "content": "x"},
				"publish_document": {"repo": "platform", "path": "docs/a.md"},
			} {
				text := errorText(t, call(t, session, tool, arguments))
				if !strings.Contains(text, tt.want) {
					t.Errorf("%s: %q does not mention %q", tool, text, tt.want)
				}
				if strings.Contains(text, "hunter2") || strings.Contains(text, "internal-host") || strings.Contains(text, "/api/v4") {
					t.Errorf("%s leaked details: %q", tool, text)
				}
			}
		})
	}
}

func TestListDraftsForOneRepositoryFailure(t *testing.T) {
	session := connect(t, &stub{err: portal.ErrNotFound}, false)
	if text := errorText(t, call(t, session, "list_drafts", map[string]any{"repo": "nope"})); !strings.Contains(text, "not found") {
		t.Errorf("text = %q", text)
	}
}

func TestResourceTemplateReadsADocument(t *testing.T) {
	session := connect(t, &stub{}, false)
	templates, err := session.ListResourceTemplates(context.Background(), nil)
	if err != nil || len(templates.ResourceTemplates) != 1 || templates.ResourceTemplates[0].URITemplate != resourceTemplate {
		t.Fatalf("templates = %+v, err = %v", templates, err)
	}
	result, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "vibefluence://platform/docs/specs/auth.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Contents) != 1 || result.Contents[0].Text != "---\ntitle: Auth\n---\n# Auth\n" || result.Contents[0].MIMEType != "text/markdown" {
		t.Errorf("result = %+v", result.Contents)
	}
}

func TestResourceErrors(t *testing.T) {
	session := connect(t, &stub{err: portal.ErrNotFound}, false)
	for _, uri := range []string{"vibefluence://platform/docs/none.md", "vibefluence://platform", "vibefluence:///docs/a.md", "other://platform/docs/a.md"} {
		if _, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri}); err == nil {
			t.Errorf("%s should fail", uri)
		}
	}
	failing := connect(t, &stub{err: errors.New("boom")}, false)
	if _, err := failing.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "vibefluence://platform/docs/a.md"}); err == nil {
		t.Error("an internal failure must be reported")
	}
}

func TestParseResourceURI(t *testing.T) {
	repo, path, ok := parseResourceURI("vibefluence://platform/docs/a%20b.md")
	if !ok || repo != "platform" || path != "docs/a b.md" {
		t.Errorf("got %q %q %v", repo, path, ok)
	}
	for _, bad := range []string{"", "http://platform/docs/a.md", "vibefluence://", "vibefluence://platform/", "::bad::"} {
		if _, _, ok := parseResourceURI(bad); ok {
			t.Errorf("%q must not parse", bad)
		}
	}
}

func TestOverStreamableHTTP(t *testing.T) {
	server := New(Options{Service: &stub{}, Version: "test"})
	web := httptest.NewServer(Handler(server))
	defer web.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "http-client", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: web.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()

	out := decode[listRepositoriesOutput](t, call(t, session, "list_repositories", nil))
	if len(out.Repositories) != 1 {
		t.Errorf("out = %+v", out)
	}
}

func TestToolSchemasDescribeTheirInputs(t *testing.T) {
	session := connect(t, &stub{}, false)
	listed, _ := session.ListTools(context.Background(), nil)
	for _, tool := range listed.Tools {
		raw, _ := json.Marshal(tool.InputSchema)
		if tool.Name == "save_draft" && (!strings.Contains(string(raw), "baseBlobId") || !strings.Contains(string(raw), "description")) {
			t.Errorf("save_draft schema = %s", raw)
		}
	}
}
