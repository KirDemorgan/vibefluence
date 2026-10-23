package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vibefluence/internal/docs"
	"vibefluence/internal/markdown"
	"vibefluence/internal/portal"
	"vibefluence/internal/publish"
)

type writeStub struct {
	all       []portal.DraftSummary
	list      []portal.DraftSummary
	view      portal.DraftView
	outcome   portal.PublishOutcome
	preview   portal.PreviewView
	err       error
	saved     portal.SaveDraftInput
	published portal.PublishInput
	args      []string
	previewIn []string
}

func (s *writeStub) AllDrafts(context.Context) ([]portal.DraftSummary, error) { return s.all, s.err }
func (s *writeStub) ListDrafts(_ context.Context, repo string) ([]portal.DraftSummary, error) {
	s.args = []string{repo}
	return s.list, s.err
}
func (s *writeStub) GetDraft(_ context.Context, repo, id string) (portal.DraftView, error) {
	s.args = []string{repo, id}
	return s.view, s.err
}
func (s *writeStub) SaveDraft(_ context.Context, repo string, input portal.SaveDraftInput) (portal.DraftView, error) {
	s.args, s.saved = []string{repo}, input
	return s.view, s.err
}
func (s *writeStub) DeleteDraft(_ context.Context, repo, id string) error {
	s.args = []string{repo, id}
	return s.err
}
func (s *writeStub) PublishDraft(_ context.Context, repo, id string, input portal.PublishInput) (portal.PublishOutcome, error) {
	s.args, s.published = []string{repo, id}, input
	return s.outcome, s.err
}
func (s *writeStub) Preview(_ context.Context, repo, path, content string) (portal.PreviewView, error) {
	s.previewIn = []string{repo, path, content}
	return s.preview, s.err
}

func writeHandler(t *testing.T, w *writeStub) http.Handler {
	t.Helper()
	return New(Options{Service: &stub{}, Write: w})
}

func jsonCall(handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	return call(handler, method, target, body, "Content-Type", "application/json")
}

var summary = portal.DraftSummary{
	ID: "0123456789abcdef", Repo: "platform", Path: "docs/a.md", Title: "A", Status: docs.StatusDraft, IsNew: true,
	Author: "Alex", UpdatedAt: time.Date(2026, 10, 22, 12, 30, 0, 0, time.UTC), BaseBlobID: "blob", MergeRequestURL: "https://gl/mr/1",
}

func TestWriteRoutesAreAbsentWithoutAWriteService(t *testing.T) {
	handler := New(Options{Service: &stub{}})
	for _, target := range []string{"/api/v1/drafts", "/api/v1/repos/a/drafts"} {
		if recorder := call(handler, http.MethodGet, target, ""); recorder.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d", target, recorder.Code)
		}
	}
}

func TestListingDrafts(t *testing.T) {
	w := &writeStub{all: []portal.DraftSummary{summary}, list: []portal.DraftSummary{summary}}
	handler := writeHandler(t, w)

	for _, target := range []string{"/api/v1/drafts", "/api/v1/repos/platform/drafts"} {
		recorder := call(handler, http.MethodGet, target, "")
		drafts := decode(t, recorder)["drafts"].([]any)
		item := drafts[0].(map[string]any)
		if recorder.Code != http.StatusOK || item["id"] != summary.ID || item["status"] != "draft" || item["isNew"] != true ||
			item["updatedAt"] != "2026-10-22T12:30:00Z" || item["mergeRequestUrl"] != "https://gl/mr/1" || item["baseBlobId"] != "blob" {
			t.Errorf("%s: %v", target, item)
		}
		if _, hasContent := item["content"]; hasContent {
			t.Errorf("%s: a listing must not carry the content", target)
		}
	}
	if w.args[0] != "platform" {
		t.Errorf("args = %v", w.args)
	}
}

func TestEmptyDraftListsAreArrays(t *testing.T) {
	recorder := call(writeHandler(t, &writeStub{}), http.MethodGet, "/api/v1/drafts", "")
	if !strings.Contains(recorder.Body.String(), `"drafts":[]`) {
		t.Errorf("body = %s", recorder.Body.String())
	}
}

func TestGettingADraftIncludesTheContent(t *testing.T) {
	w := &writeStub{view: portal.DraftView{DraftSummary: summary, Content: "# A"}}
	recorder := call(writeHandler(t, w), http.MethodGet, "/api/v1/repos/platform/drafts/0123456789abcdef", "")
	body := decode(t, recorder)
	if recorder.Code != http.StatusOK || body["content"] != "# A" || w.args[1] != "0123456789abcdef" {
		t.Errorf("status %d body %v args %v", recorder.Code, body, w.args)
	}
}

func TestSavingADraft(t *testing.T) {
	w := &writeStub{view: portal.DraftView{DraftSummary: summary, Content: "# A"}}
	handler := writeHandler(t, w)
	recorder := jsonCall(handler, http.MethodPut, "/api/v1/repos/platform/drafts",
		`{"path":"docs/a.md","content":"# A","baseBlobId":"blob","author":"Alex"}`)
	if recorder.Code != http.StatusOK || decode(t, recorder)["content"] != "# A" {
		t.Fatalf("status %d body %s", recorder.Code, recorder.Body.String())
	}
	if w.saved != (portal.SaveDraftInput{Path: "docs/a.md", Content: "# A", BaseBlobID: "blob", Author: "Alex"}) || w.args[0] != "platform" {
		t.Errorf("saved = %+v args = %v", w.saved, w.args)
	}
}

func TestDeletingADraft(t *testing.T) {
	w := &writeStub{}
	recorder := call(writeHandler(t, w), http.MethodDelete, "/api/v1/repos/platform/drafts/0123456789abcdef", "")
	if recorder.Code != http.StatusNoContent || recorder.Body.Len() != 0 || w.args[1] != "0123456789abcdef" {
		t.Errorf("status %d body %q", recorder.Code, recorder.Body.String())
	}
}

func TestPublishingADraft(t *testing.T) {
	w := &writeStub{outcome: portal.PublishOutcome{
		Outcome: publish.OutcomePending, Reason: publish.ReasonNotMergeable, Detail: "ci_must_pass",
		MergeRequestURL: "https://gl/mr/3", MergeRequestIID: 3, Created: true, Path: "docs/a.md",
	}}
	handler := writeHandler(t, w)
	recorder := jsonCall(handler, http.MethodPost, "/api/v1/repos/platform/drafts/0123456789abcdef/publish", `{"author":"Sam","force":true}`)
	body := decode(t, recorder)
	if recorder.Code != http.StatusOK || body["outcome"] != "pending" || body["reason"] != "not_mergeable" || body["detail"] != "ci_must_pass" ||
		body["mergeRequestUrl"] != "https://gl/mr/3" || body["mergeRequestIid"] != float64(3) || body["created"] != true || body["draftRemoved"] != false {
		t.Fatalf("status %d body %v", recorder.Code, body)
	}
	if w.published != (portal.PublishInput{Author: "Sam", Force: true}) {
		t.Errorf("published = %+v", w.published)
	}
}

func TestPublishingAcceptsAnEmptyBody(t *testing.T) {
	w := &writeStub{outcome: portal.PublishOutcome{Outcome: publish.OutcomeMerged, DraftRemoved: true, Path: "docs/a.md"}}
	recorder := call(writeHandler(t, w), http.MethodPost, "/api/v1/repos/platform/drafts/0123456789abcdef/publish", "")
	body := decode(t, recorder)
	if recorder.Code != http.StatusOK || body["outcome"] != "merged" || body["draftRemoved"] != true {
		t.Errorf("status %d body %v", recorder.Code, body)
	}
	if _, has := body["reason"]; has {
		t.Errorf("a merged outcome carries no reason: %v", body)
	}
	if w.published != (portal.PublishInput{}) {
		t.Errorf("published = %+v", w.published)
	}
}

func TestPreviewingADocument(t *testing.T) {
	w := &writeStub{preview: portal.PreviewView{
		Title: "T", Status: docs.StatusPublished, Type: "adr", HTML: "<p>x</p>",
		Headings: []markdown.Heading{{Level: 2, Text: "H", ID: "h-h"}},
	}}
	recorder := jsonCall(writeHandler(t, w), http.MethodPost, "/api/v1/repos/platform/preview", `{"path":"docs/a.md","content":"# T"}`)
	body := decode(t, recorder)
	if recorder.Code != http.StatusOK || body["html"] != "<p>x</p>" || body["type"] != "adr" || body["title"] != "T" {
		t.Fatalf("status %d body %v", recorder.Code, body)
	}
	if warnings, ok := body["warnings"].([]any); !ok || len(warnings) != 0 {
		t.Errorf("warnings = %v", body["warnings"])
	}
	if headings := body["headings"].([]any); len(headings) != 1 {
		t.Errorf("headings = %v", headings)
	}
	if strings.Join(w.previewIn, "|") != "platform|docs/a.md|# T" {
		t.Errorf("preview input = %v", w.previewIn)
	}
}

func TestWriteRequestsRequireJSON(t *testing.T) {
	handler := writeHandler(t, &writeStub{})
	targets := []struct{ method, target string }{
		{http.MethodPut, "/api/v1/repos/platform/drafts"},
		{http.MethodPost, "/api/v1/repos/platform/preview"},
		{http.MethodPost, "/api/v1/repos/platform/drafts/0123456789abcdef/publish"},
	}
	for _, tt := range targets {
		for _, contentType := range []string{"", "text/plain", "application/x-www-form-urlencoded", "multipart/form-data", "application/jsonp"} {
			headers := []string{}
			if contentType != "" {
				headers = []string{"Content-Type", contentType}
			}
			recorder := call(handler, tt.method, tt.target, `{"path":"docs/a.md"}`, headers...)
			if recorder.Code != http.StatusUnsupportedMediaType || decode(t, recorder)["error"] != "unsupported_media_type" {
				t.Errorf("%s %s with %q: status %d", tt.method, tt.target, contentType, recorder.Code)
			}
		}
	}
}

func TestJSONWithCharsetIsAccepted(t *testing.T) {
	w := &writeStub{view: portal.DraftView{DraftSummary: summary}}
	recorder := call(writeHandler(t, w), http.MethodPut, "/api/v1/repos/platform/drafts", `{"path":"docs/a.md","content":"x"}`,
		"Content-Type", "application/json; charset=utf-8")
	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d", recorder.Code)
	}
}

func TestMalformedAndOversizedBodies(t *testing.T) {
	handler := writeHandler(t, &writeStub{})
	if recorder := jsonCall(handler, http.MethodPut, "/api/v1/repos/platform/drafts", `{not json`); recorder.Code != http.StatusBadRequest {
		t.Errorf("malformed: status %d", recorder.Code)
	}
	if recorder := jsonCall(handler, http.MethodPut, "/api/v1/repos/platform/drafts", ""); recorder.Code != http.StatusBadRequest {
		t.Errorf("empty body where one is required: status %d", recorder.Code)
	}
	huge := `{"path":"docs/a.md","content":"` + strings.Repeat("x", maxJSONBody) + `"}`
	recorder := jsonCall(handler, http.MethodPut, "/api/v1/repos/platform/drafts", huge)
	if recorder.Code != http.StatusRequestEntityTooLarge || decode(t, recorder)["error"] != "too_large" {
		t.Errorf("oversized: status %d", recorder.Code)
	}
}

func TestConflictsAndBusyDocumentsAreReportedWith409(t *testing.T) {
	w := &writeStub{err: &portal.ConflictError{Exists: true, CurrentBlobID: "cur", Message: "it changed"}}
	handler := writeHandler(t, w)
	recorder := jsonCall(handler, http.MethodPost, "/api/v1/repos/platform/drafts/0123456789abcdef/publish", `{}`)
	body := decode(t, recorder)
	if recorder.Code != http.StatusConflict || body["error"] != "conflict" || body["exists"] != true || body["currentBlobId"] != "cur" || body["message"] != "it changed" {
		t.Errorf("conflict: status %d body %v", recorder.Code, body)
	}

	w.err = fmt.Errorf("%w: running", portal.ErrBusy)
	recorder = jsonCall(handler, http.MethodPost, "/api/v1/repos/platform/drafts/0123456789abcdef/publish", `{}`)
	if recorder.Code != http.StatusConflict || decode(t, recorder)["error"] != "busy" {
		t.Errorf("busy: status %d body %s", recorder.Code, recorder.Body.String())
	}

	w.err = fmt.Errorf("%w: no token", portal.ErrReadOnly)
	recorder = jsonCall(handler, http.MethodPost, "/api/v1/repos/platform/drafts/0123456789abcdef/publish", `{}`)
	if recorder.Code != http.StatusForbidden || decode(t, recorder)["error"] != "read_only" {
		t.Errorf("read-only: status %d body %s", recorder.Code, recorder.Body.String())
	}
}

func TestWriteErrorMapping(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{&portal.ValidationError{Message: "bad"}, http.StatusBadRequest, "invalid_request"},
		{fmt.Errorf("%w: draft", portal.ErrNotFound), http.StatusNotFound, "not_found"},
		{errors.New("kaboom"), http.StatusInternalServerError, "internal_error"},
	}
	for _, tt := range cases {
		handler := writeHandler(t, &writeStub{err: tt.err})
		requests := []*httptest.ResponseRecorder{
			call(handler, http.MethodGet, "/api/v1/drafts", ""),
			call(handler, http.MethodGet, "/api/v1/repos/a/drafts", ""),
			call(handler, http.MethodGet, "/api/v1/repos/a/drafts/0123456789abcdef", ""),
			call(handler, http.MethodDelete, "/api/v1/repos/a/drafts/0123456789abcdef", ""),
			jsonCall(handler, http.MethodPut, "/api/v1/repos/a/drafts", `{"path":"docs/a.md"}`),
			jsonCall(handler, http.MethodPost, "/api/v1/repos/a/drafts/0123456789abcdef/publish", `{}`),
			jsonCall(handler, http.MethodPost, "/api/v1/repos/a/preview", `{"content":"x"}`),
		}
		for i, recorder := range requests {
			if recorder.Code != tt.status || decode(t, recorder)["error"] != tt.code {
				t.Errorf("%v request %d: status %d body %s", tt.err, i, recorder.Code, recorder.Body.String())
			}
		}
	}
}
