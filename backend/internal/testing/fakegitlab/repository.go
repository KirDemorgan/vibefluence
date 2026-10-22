package fakegitlab

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultPerPage = 20
	maxPerPage     = 100
)

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/v4/user", s.handleUser)
	s.mux.HandleFunc("GET /api/v4/projects/{id}", s.handleProject)
	s.mux.HandleFunc("GET /api/v4/projects/{id}/repository/tree", s.handleTree)
	s.mux.HandleFunc("GET /api/v4/projects/{id}/repository/files/{path}/raw", s.handleFileRaw)
	s.mux.HandleFunc("GET /api/v4/projects/{id}/repository/files/{path}", s.handleFile)
	s.mux.HandleFunc("GET /api/v4/projects/{id}/repository/blobs/{sha}/raw", s.handleBlobRaw)
	s.mux.HandleFunc("POST /api/v4/projects/{id}/repository/commits", s.handleCreateCommit)
	s.mux.HandleFunc("POST /api/v4/projects/{id}/merge_requests", s.handleCreateMergeRequest)
	s.mux.HandleFunc("GET /api/v4/projects/{id}/merge_requests/{iid}", s.handleGetMergeRequest)
	s.mux.HandleFunc("PUT /api/v4/projects/{id}/merge_requests/{iid}/merge", s.handleMerge)
	s.mux.HandleFunc("GET /api/v4/projects/{id}/merge_requests/{iid}/pipelines", s.handleMergeRequestPipelines)
	s.mux.HandleFunc("GET /api/v4/projects/{id}/pipelines", s.handlePipelines)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeMessage(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"message": message})
}

func (s *Server) projectFor(w http.ResponseWriter, r *http.Request) (*project, bool) {
	id := r.PathValue("id")
	if numeric, err := strconv.Atoi(id); err == nil {
		for _, candidate := range s.projects {
			if candidate.id == numeric {
				return candidate, true
			}
		}
	} else if found, ok := s.projects[id]; ok {
		return found, true
	}
	writeMessage(w, http.StatusNotFound, "404 Project Not Found")
	return nil, false
}

func (s *Server) handleUser(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"id": 1, "username": s.botName, "bot": true})
}

func (s *Server) handleProject(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectFor(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":                  project.id,
		"path_with_namespace": project.path,
		"default_branch":      project.defaultBranch,
	})
}

type treeEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Path string `json:"path"`
	Mode string `json:"mode"`
}

func treeEntries(b *branch, dir string, recursive bool) []treeEntry {
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	seen := make(map[string]treeEntry)
	for filePath, stored := range b.files {
		if !strings.HasPrefix(filePath, prefix) {
			continue
		}
		parts := strings.Split(filePath[len(prefix):], "/")
		for i := 0; i < len(parts)-1; i++ {
			if !recursive && i > 0 {
				break
			}
			dirPath := prefix + strings.Join(parts[:i+1], "/")
			seen[dirPath] = treeEntry{ID: fakeTreeID(dirPath), Name: parts[i], Type: "tree", Path: dirPath, Mode: "040000"}
		}
		if recursive || len(parts) == 1 {
			seen[filePath] = treeEntry{ID: stored.blobID, Name: parts[len(parts)-1], Type: "blob", Path: filePath, Mode: "100644"}
		}
	}
	entries := make([]treeEntry, 0, len(seen))
	for _, entry := range seen {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries
}

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectFor(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	ref := query.Get("ref")
	if ref == "" {
		ref = project.defaultBranch
	}
	target, ok := project.branches[ref]
	if !ok {
		writeMessage(w, http.StatusNotFound, "404 Tree Not Found")
		return
	}
	dir := strings.Trim(query.Get("path"), "/")
	entries := treeEntries(target, dir, query.Get("recursive") == "true")
	if dir != "" && len(entries) == 0 {
		writeMessage(w, http.StatusNotFound, "404 Tree Not Found")
		return
	}

	perPage := defaultPerPage
	if requested, err := strconv.Atoi(query.Get("per_page")); err == nil && requested > 0 {
		perPage = min(requested, maxPerPage)
	}

	var page []treeEntry
	if query.Get("pagination") == "keyset" {
		page = keysetPage(w, r, entries, perPage)
	} else {
		page = offsetPage(w, query, entries, perPage)
	}
	writeJSON(w, http.StatusOK, page)
}

func keysetPage(w http.ResponseWriter, r *http.Request, entries []treeEntry, perPage int) []treeEntry {
	start := 0
	if token := r.URL.Query().Get("page_token"); token != "" {
		start = sort.Search(len(entries), func(i int) bool { return entries[i].Path > token })
	}
	end := min(start+perPage, len(entries))
	if end < len(entries) {
		next := *r.URL
		values := next.Query()
		values.Set("page_token", entries[end-1].Path)
		next.RawQuery = values.Encode()
		next.Scheme = requestScheme(r)
		next.Host = r.Host
		w.Header().Set("Link", fmt.Sprintf("<%s>; rel=\"next\"", next.String()))
	}
	return entries[start:end]
}

func offsetPage(w http.ResponseWriter, query url.Values, entries []treeEntry, perPage int) []treeEntry {
	page := 1
	if requested, err := strconv.Atoi(query.Get("page")); err == nil && requested > 0 {
		page = requested
	}
	start := min((page-1)*perPage, len(entries))
	end := min(start+perPage, len(entries))
	w.Header().Set("X-Page", strconv.Itoa(page))
	if end < len(entries) {
		w.Header().Set("X-Next-Page", strconv.Itoa(page+1))
	}
	return entries[start:end]
}

func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func (s *Server) lookupFile(w http.ResponseWriter, r *http.Request, refRequired bool) (*project, string, file, bool) {
	project, ok := s.projectFor(w, r)
	if !ok {
		return nil, "", file{}, false
	}
	ref := r.URL.Query().Get("ref")
	if ref == "" {
		if refRequired {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ref is missing"})
			return nil, "", file{}, false
		}
		ref = project.defaultBranch
	}
	target, ok := project.branches[ref]
	if !ok {
		writeMessage(w, http.StatusNotFound, "404 File Not Found")
		return nil, "", file{}, false
	}
	stored, ok := target.files[r.PathValue("path")]
	if !ok {
		writeMessage(w, http.StatusNotFound, "404 File Not Found")
		return nil, "", file{}, false
	}
	return project, ref, stored, true
}

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	project, ref, stored, ok := s.lookupFile(w, r, true)
	if !ok {
		return
	}
	filePath := r.PathValue("path")
	digest := sha256.Sum256(stored.content)
	setFileHeaders(w, filePath, ref, project.branches[ref].head, stored)
	writeJSON(w, http.StatusOK, map[string]any{
		"file_name":      filePath[strings.LastIndex(filePath, "/")+1:],
		"file_path":      filePath,
		"size":           len(stored.content),
		"encoding":       "base64",
		"content":        base64.StdEncoding.EncodeToString(stored.content),
		"content_sha256": hex.EncodeToString(digest[:]),
		"ref":            ref,
		"blob_id":        stored.blobID,
		"commit_id":      project.branches[ref].head,
		"last_commit_id": stored.lastCommitID,
	})
}

func (s *Server) handleFileRaw(w http.ResponseWriter, r *http.Request) {
	project, ref, stored, ok := s.lookupFile(w, r, false)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	setFileHeaders(w, r.PathValue("path"), ref, project.branches[ref].head, stored)
	_, _ = w.Write(stored.content)
}

func setFileHeaders(w http.ResponseWriter, filePath, ref, commitID string, stored file) {
	digest := sha256.Sum256(stored.content)
	header := w.Header()
	header.Set("X-Gitlab-Blob-Id", stored.blobID)
	header.Set("X-Gitlab-Commit-Id", commitID)
	header.Set("X-Gitlab-Last-Commit-Id", stored.lastCommitID)
	header.Set("X-Gitlab-Content-Sha256", hex.EncodeToString(digest[:]))
	header.Set("X-Gitlab-File-Path", filePath)
	header.Set("X-Gitlab-File-Name", filePath[strings.LastIndex(filePath, "/")+1:])
	header.Set("X-Gitlab-Ref", ref)
	header.Set("X-Gitlab-Size", strconv.Itoa(len(stored.content)))
}

func (s *Server) handleBlobRaw(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectFor(w, r)
	if !ok {
		return
	}
	wanted := r.PathValue("sha")
	for _, candidate := range project.branches {
		for _, stored := range candidate.files {
			if stored.blobID == wanted {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				_, _ = w.Write(stored.content)
				return
			}
		}
	}
	writeMessage(w, http.StatusNotFound, "404 Blob Not Found")
}

type commitAction struct {
	Action       string  `json:"action"`
	FilePath     string  `json:"file_path"`
	PreviousPath string  `json:"previous_path"`
	Content      *string `json:"content"`
	Encoding     string  `json:"encoding"`
	LastCommitID string  `json:"last_commit_id"`
}

type commitRequest struct {
	Branch        string         `json:"branch"`
	StartBranch   string         `json:"start_branch"`
	CommitMessage string         `json:"commit_message"`
	Actions       []commitAction `json:"actions"`
}

var errBadAction = errors.New("bad action")

type commitError string

func (e commitError) Error() string { return string(e) }

func (s *Server) handleCreateCommit(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectFor(w, r)
	if !ok {
		return
	}
	var request commitRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeMessage(w, http.StatusBadRequest, "400 Bad Request: invalid JSON")
		return
	}
	if request.Branch == "" || request.CommitMessage == "" {
		writeMessage(w, http.StatusBadRequest, "400 Bad request - branch and commit_message are required")
		return
	}
	if len(request.Actions) == 0 {
		writeMessage(w, http.StatusBadRequest, "400 Bad request - actions is empty")
		return
	}

	target, exists := project.branches[request.Branch]
	if !exists {
		start, found := project.branches[request.StartBranch]
		if request.StartBranch == "" || !found {
			writeMessage(w, http.StatusBadRequest, "You can only create or edit files when you are on a branch: start_branch is missing or unknown")
			return
		}
		target = start.fork("")
	}

	working := make(map[string]file, len(target.files))
	for path, stored := range target.files {
		working[path] = stored
	}
	commitID := project.newCommitID(request.CommitMessage)
	if err := applyActions(working, request.Actions, commitID); err != nil {
		writeMessage(w, http.StatusBadRequest, err.Error())
		return
	}

	target.files = working
	target.head = commitID
	if !exists {
		project.branches[request.Branch] = target
	}
	project.startPipeline(request.Branch, commitID, 0)

	title, _, _ := strings.Cut(request.CommitMessage, "\n")
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         commitID,
		"short_id":   commitID[:8],
		"title":      title,
		"message":    request.CommitMessage,
		"parent_ids": []string{},
		"web_url":    fmt.Sprintf("%s://%s/%s/-/commit/%s", requestScheme(r), r.Host, project.path, commitID),
	})
}

func applyActions(files map[string]file, actions []commitAction, commitID string) error {
	for _, action := range actions {
		if action.FilePath == "" {
			return fmt.Errorf("%w: file_path is required", errBadAction)
		}
		content, err := decodeContent(action)
		if err != nil {
			return err
		}
		if (action.Action == "create" || action.Action == "update") && action.Content == nil {
			return commitError("content is missing for " + action.Action + ": " + action.FilePath)
		}
		switch action.Action {
		case "create":
			if _, exists := files[action.FilePath]; exists {
				return commitError("A file with this name already exists: " + action.FilePath)
			}
			files[action.FilePath] = newFile(content, commitID)
		case "update":
			if err := requireUnchanged(files, action.FilePath, action.LastCommitID); err != nil {
				return err
			}
			files[action.FilePath] = newFile(content, commitID)
		case "delete":
			if err := requireUnchanged(files, action.FilePath, action.LastCommitID); err != nil {
				return err
			}
			delete(files, action.FilePath)
		case "move":
			if err := applyMove(files, action, content, commitID); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: unknown action %q", errBadAction, action.Action)
		}
	}
	return nil
}

func decodeContent(action commitAction) ([]byte, error) {
	if action.Content == nil {
		return nil, nil
	}
	if action.Encoding == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(*action.Content)
		if err != nil {
			return nil, fmt.Errorf("%w: content is not valid base64", errBadAction)
		}
		return decoded, nil
	}
	return []byte(*action.Content), nil
}

func requireUnchanged(files map[string]file, path, lastCommitID string) error {
	stored, exists := files[path]
	if !exists {
		return commitError("A file with this name doesn't exist: " + path)
	}
	if lastCommitID != "" && lastCommitID != stored.lastCommitID {
		return commitError("You are attempting to update a file that has changed since you started editing it: " + path)
	}
	return nil
}

func applyMove(files map[string]file, action commitAction, content []byte, commitID string) error {
	if err := requireUnchanged(files, action.PreviousPath, action.LastCommitID); err != nil {
		return err
	}
	if _, exists := files[action.FilePath]; exists {
		return commitError("A file with this name already exists: " + action.FilePath)
	}
	moved := files[action.PreviousPath].content
	if action.Content != nil {
		moved = content
	}
	delete(files, action.PreviousPath)
	files[action.FilePath] = newFile(moved, commitID)
	return nil
}
