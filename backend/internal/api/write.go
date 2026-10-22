package api

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"time"

	"vibefluence/internal/docs"
	"vibefluence/internal/portal"
	"vibefluence/internal/publish"
)

const maxJSONBody = 2 << 20

type WriteService interface {
	AllDrafts(ctx context.Context) ([]portal.DraftSummary, error)
	ListDrafts(ctx context.Context, repoID string) ([]portal.DraftSummary, error)
	GetDraft(ctx context.Context, repoID, id string) (portal.DraftView, error)
	SaveDraft(ctx context.Context, repoID string, input portal.SaveDraftInput) (portal.DraftView, error)
	DeleteDraft(ctx context.Context, repoID, id string) error
	PublishDraft(ctx context.Context, repoID, id string, input portal.PublishInput) (portal.PublishOutcome, error)
	Preview(ctx context.Context, repoID, filePath, content string) (portal.PreviewView, error)
}

func (h *handler) registerWrite(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/drafts", h.allDrafts)
	mux.HandleFunc("GET /api/v1/repos/{repo}/drafts", h.listDrafts)
	mux.HandleFunc("GET /api/v1/repos/{repo}/drafts/{id}", h.getDraft)
	mux.HandleFunc("PUT /api/v1/repos/{repo}/drafts", h.saveDraft)
	mux.HandleFunc("DELETE /api/v1/repos/{repo}/drafts/{id}", h.deleteDraft)
	mux.HandleFunc("POST /api/v1/repos/{repo}/drafts/{id}/publish", h.publishDraft)
	mux.HandleFunc("POST /api/v1/repos/{repo}/preview", h.preview)
}

func readJSON(w http.ResponseWriter, r *http.Request, target any, allowEmpty bool) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if r.ContentLength == 0 && allowEmpty {
		return true
	}
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "send the request body as application/json")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "the request body is too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid_request", "the request body is not valid JSON")
		return false
	}
	return true
}

type draftDTO struct {
	ID              string      `json:"id"`
	Repo            string      `json:"repo"`
	Path            string      `json:"path"`
	Title           string      `json:"title"`
	Status          docs.Status `json:"status"`
	IsNew           bool        `json:"isNew"`
	Author          string      `json:"author"`
	UpdatedAt       string      `json:"updatedAt"`
	BaseBlobID      string      `json:"baseBlobId"`
	MergeRequestURL string      `json:"mergeRequestUrl,omitempty"`
	Content         *string     `json:"content,omitempty"`
}

func toDraftDTO(summary portal.DraftSummary) draftDTO {
	return draftDTO{
		ID: summary.ID, Repo: summary.Repo, Path: summary.Path, Title: summary.Title, Status: summary.Status,
		IsNew: summary.IsNew, Author: summary.Author, UpdatedAt: summary.UpdatedAt.UTC().Format(time.RFC3339),
		BaseBlobID: summary.BaseBlobID, MergeRequestURL: summary.MergeRequestURL,
	}
}

func toDraftViewDTO(view portal.DraftView) draftDTO {
	dto := toDraftDTO(view.DraftSummary)
	content := view.Content
	dto.Content = &content
	return dto
}

func draftList(summaries []portal.DraftSummary) []draftDTO {
	items := make([]draftDTO, 0, len(summaries))
	for _, summary := range summaries {
		items = append(items, toDraftDTO(summary))
	}
	return items
}

func (h *handler) allDrafts(w http.ResponseWriter, r *http.Request) {
	summaries, err := h.write.AllDrafts(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"drafts": draftList(summaries)})
}

func (h *handler) listDrafts(w http.ResponseWriter, r *http.Request) {
	summaries, err := h.write.ListDrafts(r.Context(), r.PathValue("repo"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"drafts": draftList(summaries)})
}

func (h *handler) getDraft(w http.ResponseWriter, r *http.Request) {
	view, err := h.write.GetDraft(r.Context(), r.PathValue("repo"), r.PathValue("id"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toDraftViewDTO(view))
}

type saveDraftBody struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	BaseBlobID string `json:"baseBlobId"`
	Author     string `json:"author"`
}

func (h *handler) saveDraft(w http.ResponseWriter, r *http.Request) {
	var body saveDraftBody
	if !readJSON(w, r, &body, false) {
		return
	}
	view, err := h.write.SaveDraft(r.Context(), r.PathValue("repo"), portal.SaveDraftInput{
		Path: body.Path, Content: body.Content, BaseBlobID: body.BaseBlobID, Author: body.Author,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toDraftViewDTO(view))
}

func (h *handler) deleteDraft(w http.ResponseWriter, r *http.Request) {
	if err := h.write.DeleteDraft(r.Context(), r.PathValue("repo"), r.PathValue("id")); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type publishBody struct {
	Author string `json:"author"`
	Force  bool   `json:"force"`
}

type publishDTO struct {
	Outcome         publish.Outcome `json:"outcome"`
	Reason          publish.Reason  `json:"reason,omitempty"`
	Detail          string          `json:"detail,omitempty"`
	MergeRequestURL string          `json:"mergeRequestUrl,omitempty"`
	MergeRequestIID int             `json:"mergeRequestIid,omitempty"`
	Created         bool            `json:"created"`
	Path            string          `json:"path"`
	DraftRemoved    bool            `json:"draftRemoved"`
}

func (h *handler) publishDraft(w http.ResponseWriter, r *http.Request) {
	var body publishBody
	if !readJSON(w, r, &body, true) {
		return
	}
	outcome, err := h.write.PublishDraft(r.Context(), r.PathValue("repo"), r.PathValue("id"), portal.PublishInput{
		Author: body.Author, Force: body.Force,
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, publishDTO{
		Outcome: outcome.Outcome, Reason: outcome.Reason, Detail: outcome.Detail, MergeRequestURL: outcome.MergeRequestURL,
		MergeRequestIID: outcome.MergeRequestIID, Created: outcome.Created, Path: outcome.Path, DraftRemoved: outcome.DraftRemoved,
	})
}

type previewBody struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type previewDTO struct {
	Title    string       `json:"title"`
	Status   docs.Status  `json:"status"`
	Type     string       `json:"type,omitempty"`
	Warnings []string     `json:"warnings"`
	HTML     string       `json:"html"`
	Headings []headingDTO `json:"headings"`
}

func (h *handler) preview(w http.ResponseWriter, r *http.Request) {
	var body previewBody
	if !readJSON(w, r, &body, false) {
		return
	}
	view, err := h.write.Preview(r.Context(), r.PathValue("repo"), body.Path, body.Content)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	headings := make([]headingDTO, 0, len(view.Headings))
	for _, heading := range view.Headings {
		headings = append(headings, toHeadingDTO(heading))
	}
	warnings := view.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	writeJSON(w, http.StatusOK, previewDTO{
		Title: view.Title, Status: view.Status, Type: view.Type, Warnings: warnings, HTML: view.HTML, Headings: headings,
	})
}
