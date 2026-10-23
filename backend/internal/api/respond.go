package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"vibefluence/internal/gitlab"
	"vibefluence/internal/portal"
	"vibefluence/internal/server"
)

type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

type conflictBody struct {
	Error         string `json:"error"`
	Message       string `json:"message"`
	Exists        bool   `json:"exists"`
	CurrentBlobID string `json:"currentBlobId,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: code, Message: message})
}

func (h *handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var validation *portal.ValidationError
	var conflict *portal.ConflictError
	var apiErr *gitlab.APIError
	switch {
	case errors.As(err, &conflict):
		writeJSON(w, http.StatusConflict, conflictBody{
			Error: "conflict", Message: conflict.Message, Exists: conflict.Exists, CurrentBlobID: conflict.CurrentBlobID,
		})
	case errors.Is(err, portal.ErrReadOnly):
		writeError(w, http.StatusForbidden, "read_only", "this server runs without a GitLab token and is read-only")
	case errors.Is(err, portal.ErrBusy):
		writeError(w, http.StatusConflict, "busy", "this document is being published right now, try again in a moment")
	case errors.As(err, &validation):
		writeError(w, http.StatusBadRequest, "invalid_request", validation.Message)
	case errors.Is(err, portal.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "the requested item does not exist")
	case errors.Is(err, context.Canceled):
		writeError(w, http.StatusServiceUnavailable, "cancelled", "the request was cancelled")
	case errors.Is(err, context.DeadlineExceeded):
		h.logUpstream(r, err)
		writeError(w, http.StatusGatewayTimeout, "upstream_timeout", "GitLab did not answer in time")
	case errors.Is(err, gitlab.ErrUnauthorized), errors.Is(err, gitlab.ErrForbidden):
		h.logUpstream(r, err)
		writeError(w, http.StatusBadGateway, "upstream_unauthorized", "the GitLab token was rejected or lacks permissions")
	case errors.As(err, &apiErr), errors.Is(err, gitlab.ErrNotFound):
		h.logUpstream(r, err)
		writeError(w, http.StatusBadGateway, "upstream_unavailable", "GitLab is unavailable or returned an error")
	default:
		h.logger.ErrorContext(r.Context(), "unhandled error",
			slog.Any("error", err), slog.String("request_id", server.RequestID(r.Context())))
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func (h *handler) logUpstream(r *http.Request, err error) {
	h.logger.WarnContext(r.Context(), "upstream error",
		slog.Any("error", err), slog.String("request_id", server.RequestID(r.Context())))
}
