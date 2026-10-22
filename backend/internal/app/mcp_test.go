package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"vibefluence/internal/config"
)

func (s *stack) mcpSession(t *testing.T) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "agent", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: s.web.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func callTool[T any](t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) (T, *mcp.CallToolResult) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var out T
	if !result.IsError {
		raw, _ := json.Marshal(result.StructuredContent)
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
	}
	return out, result
}

type listed struct {
	Documents []struct {
		Path string `json:"path"`
	} `json:"documents"`
}

func TestEndToEndAgentsReadDocumentsOverMCP(t *testing.T) {
	s := newStack(t, nil)
	s.seed()
	session := s.mcpSession(t)

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "publish_document" {
			t.Error("publishing must be off unless configured")
		}
	}

	documents, _ := callTool[listed](t, session, "list_documents", map[string]any{"repo": "docs"})
	var paths []string
	for _, doc := range documents.Documents {
		paths = append(paths, doc.Path)
	}
	if strings.Join(paths, ",") != "docs/guide/setup.md,docs/README.md" {
		t.Errorf("paths = %v", paths)
	}
	for _, path := range paths {
		if strings.Contains(path, "hidden") {
			t.Errorf("a draft leaked through MCP: %s", path)
		}
	}

	read, _ := callTool[struct {
		Content string `json:"content"`
		BlobID  string `json:"blobId"`
	}](t, session, "read_document", map[string]any{"repo": "docs", "path": "docs/guide/setup.md"})
	if !strings.Contains(read.Content, "Install deployment tooling") || read.BlobID == "" {
		t.Errorf("read = %+v", read)
	}

	_, hidden := callTool[struct{}](t, session, "read_document", map[string]any{"repo": "docs", "path": "docs/guide/hidden.md"})
	if !hidden.IsError {
		t.Error("a document marked as a draft in the repository must not be readable")
	}

	found, _ := callTool[struct {
		Results []struct {
			Path string `json:"path"`
		} `json:"results"`
	}](t, session, "search_documents", map[string]any{"query": "deployment"})
	if len(found.Results) != 1 || found.Results[0].Path != "docs/guide/setup.md" {
		t.Errorf("search results = %+v", found)
	}

	resource, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "vibefluence://docs/docs/guide/setup.md"})
	if err != nil || !strings.Contains(resource.Contents[0].Text, "Install deployment tooling") {
		t.Errorf("resource = %+v, err = %v", resource, err)
	}
}

func TestEndToEndAgentsWriteDraftsButHumansPublish(t *testing.T) {
	s := newStack(t, nil)
	s.seed()
	session := s.mcpSession(t)

	saved, result := callTool[struct {
		Path   string `json:"path"`
		Status string `json:"status"`
		IsNew  bool   `json:"isNew"`
	}](t, session, "save_draft", map[string]any{
		"repo": "docs", "path": "docs/specs/agent-note.md",
		"content": "---\ntitle: Agent note\nstatus: draft\n---\n# Agent note\n",
	})
	if result.IsError || saved.Path != "docs/specs/agent-note.md" || saved.Status != "draft" || !saved.IsNew {
		t.Fatalf("saved = %+v, result = %+v", saved, result)
	}

	status, drafts := s.json(t, http.MethodGet, "/api/v1/repos/docs/drafts")
	items := drafts["drafts"].([]any)
	if status != 200 || len(items) != 1 || items[0].(map[string]any)["author"] != "MCP agent" {
		t.Errorf("the web interface must see the agent's draft: %v", drafts)
	}
	if _, ok := s.fake.File(project, "master", "docs/specs/agent-note.md"); ok {
		t.Error("saving a draft must not touch the repository")
	}

	listed, _ := callTool[struct {
		Drafts []struct {
			Path string `json:"path"`
		} `json:"drafts"`
	}](t, session, "list_drafts", map[string]any{})
	if len(listed.Drafts) != 1 {
		t.Errorf("listed = %+v", listed)
	}
	draft, _ := callTool[struct {
		Content string `json:"content"`
	}](t, session, "read_draft", map[string]any{"repo": "docs", "path": "docs/specs/agent-note.md"})
	if !strings.Contains(draft.Content, "Agent note") {
		t.Errorf("draft = %+v", draft)
	}

	if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "publish_document", Arguments: map[string]any{"repo": "docs", "path": "docs/specs/agent-note.md"}}); err == nil {
		t.Error("publish_document must not exist by default")
	}
}

func TestEndToEndPublishingOverMCPCanBeEnabled(t *testing.T) {
	s := newStack(t, func(cfg *config.Config) { cfg.MCP.AllowPublish = true })
	session := s.mcpSession(t)

	callTool[struct{}](t, session, "save_draft", map[string]any{
		"repo": "docs", "path": "docs/agent-doc.md", "content": "---\ntitle: Agent doc\nstatus: published\n---\n# Agent doc\n",
	})
	published, result := callTool[struct {
		Outcome string `json:"outcome"`
		Created bool   `json:"created"`
	}](t, session, "publish_document", map[string]any{"repo": "docs", "path": "docs/agent-doc.md"})
	if result.IsError || published.Outcome != "merged" || !published.Created {
		t.Fatalf("published = %+v, result = %+v", published, result)
	}
	if raw, ok := s.fake.File(project, "master", "docs/agent-doc.md"); !ok || !strings.Contains(raw, "Agent doc") {
		t.Errorf("file = %q", raw)
	}
}

func TestEndToEndMCPCanBeSwitchedOff(t *testing.T) {
	disabled := false
	s := newStack(t, func(cfg *config.Config) { cfg.MCP.Enabled = &disabled })
	status, _, _ := s.do(t, http.MethodPost, "/mcp")
	if status == http.StatusOK {
		t.Errorf("status = %d", status)
	}
	if status, _, body := s.do(t, http.MethodGet, "/mcp"); status != http.StatusOK || !strings.Contains(string(body), "app") {
		t.Errorf("a disabled MCP falls through to the single page application: %d", status)
	}
}
