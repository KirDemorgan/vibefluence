package fakegitlab

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const (
	stateOpened = "opened"
	stateMerged = "merged"
)

type mergeRequest struct {
	iid                int
	title              string
	state              string
	sourceBranch       string
	targetBranch       string
	removeSourceBranch bool
	squash             bool
	approved           bool
	mergeCommitSHA     string
	mergedAt           time.Time
	pendingChecks      int
}

type pipelineJSON struct {
	ID     int    `json:"id"`
	Ref    string `json:"ref"`
	SHA    string `json:"sha"`
	Status string `json:"status"`
}

type mergeRequestJSON struct {
	ID                       int           `json:"id"`
	IID                      int           `json:"iid"`
	ProjectID                int           `json:"project_id"`
	Title                    string        `json:"title"`
	State                    string        `json:"state"`
	SourceBranch             string        `json:"source_branch"`
	TargetBranch             string        `json:"target_branch"`
	SHA                      string        `json:"sha"`
	MergeCommitSHA           *string       `json:"merge_commit_sha"`
	DetailedMergeStatus      string        `json:"detailed_merge_status"`
	HasConflicts             bool          `json:"has_conflicts"`
	ShouldRemoveSourceBranch bool          `json:"should_remove_source_branch"`
	Squash                   bool          `json:"squash"`
	WebURL                   string        `json:"web_url"`
	HeadPipeline             *pipelineJSON `json:"head_pipeline"`
}

type change struct {
	path    string
	deleted bool
	file    file
}

func (p *project) changesOf(source *branch) []change {
	var changes []change
	for path, stored := range source.files {
		if source.base[path] != stored.blobID {
			changes = append(changes, change{path: path, file: stored})
		}
	}
	for path := range source.base {
		if _, still := source.files[path]; !still {
			changes = append(changes, change{path: path, deleted: true})
		}
	}
	return changes
}

func (p *project) conflicts(mr *mergeRequest) bool {
	source, target := p.branches[mr.sourceBranch], p.branches[mr.targetBranch]
	if source == nil || target == nil {
		return false
	}
	for _, c := range p.changesOf(source) {
		current, exists := target.files[c.path]
		baseID, hadBase := source.base[c.path]
		unchangedInTarget := (!exists && !hadBase) || (exists && hadBase && current.blobID == baseID)
		sameResult := (c.deleted && !exists) || (!c.deleted && exists && current.blobID == c.file.blobID)
		if !unchangedInTarget && !sameResult {
			return true
		}
	}
	return false
}

func (p *project) detailedMergeStatus(mr *mergeRequest) string {
	if mr.state != stateOpened {
		return "not_open"
	}
	if mr.pendingChecks > 0 {
		return "checking"
	}
	if p.conflicts(mr) {
		return "conflict"
	}
	if p.opts.PipelinesMustSucceed {
		if status := p.pipelineGate(mr); status != "" {
			return status
		}
	}
	if p.opts.ApprovalRequired && !mr.approved {
		return "not_approved"
	}
	return "mergeable"
}

func (p *project) pipelineGate(mr *mergeRequest) string {
	related := p.pipelinesFor(mr)
	if len(related) == 0 {
		return "ci_must_pass"
	}
	switch related[len(related)-1].status {
	case "success":
		return ""
	case "created", "pending", "running":
		return "ci_still_running"
	default:
		return "ci_must_pass"
	}
}

func (p *project) renderMergeRequest(r *http.Request, mr *mergeRequest) mergeRequestJSON {
	rendered := mergeRequestJSON{
		ID:                       p.id*1000 + mr.iid,
		IID:                      mr.iid,
		ProjectID:                p.id,
		Title:                    mr.title,
		State:                    mr.state,
		SourceBranch:             mr.sourceBranch,
		TargetBranch:             mr.targetBranch,
		DetailedMergeStatus:      p.detailedMergeStatus(mr),
		HasConflicts:             p.conflicts(mr),
		ShouldRemoveSourceBranch: mr.removeSourceBranch,
		Squash:                   mr.squash,
		WebURL:                   fmt.Sprintf("%s://%s/%s/-/merge_requests/%d", requestScheme(r), r.Host, p.path, mr.iid),
	}
	if source, ok := p.branches[mr.sourceBranch]; ok {
		rendered.SHA = source.head
	}
	if mr.mergeCommitSHA != "" {
		rendered.MergeCommitSHA = &mr.mergeCommitSHA
	}
	if related := p.pipelinesFor(mr); len(related) > 0 {
		latest := related[len(related)-1]
		rendered.HeadPipeline = &pipelineJSON{ID: latest.id, Ref: latest.ref, SHA: latest.sha, Status: latest.status}
	}
	return rendered
}

type mergeRequestCreate struct {
	SourceBranch       string `json:"source_branch"`
	TargetBranch       string `json:"target_branch"`
	Title              string `json:"title"`
	RemoveSourceBranch bool   `json:"remove_source_branch"`
	Squash             bool   `json:"squash"`
}

func (s *Server) handleCreateMergeRequest(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectFor(w, r)
	if !ok {
		return
	}
	var request mergeRequestCreate
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeMessage(w, http.StatusBadRequest, "400 Bad Request: invalid JSON")
		return
	}
	if request.SourceBranch == "" || request.TargetBranch == "" || request.Title == "" {
		writeMessage(w, http.StatusBadRequest, "400 Bad request - source_branch, target_branch and title are required")
		return
	}
	if _, found := project.branches[request.SourceBranch]; !found {
		writeMessage(w, http.StatusBadRequest, "Source branch does not exist")
		return
	}
	if _, found := project.branches[request.TargetBranch]; !found {
		writeMessage(w, http.StatusBadRequest, "Target branch does not exist")
		return
	}
	if request.SourceBranch == request.TargetBranch {
		writeMessage(w, http.StatusUnprocessableEntity, "Source branch and target branch must be different")
		return
	}
	for _, existing := range project.mergeRequests {
		if existing.state == stateOpened && existing.sourceBranch == request.SourceBranch && existing.targetBranch == request.TargetBranch {
			writeMessage(w, http.StatusConflict, fmt.Sprintf("Another open merge request already exists for this source branch: !%d", existing.iid))
			return
		}
	}

	mr := &mergeRequest{
		iid:                len(project.mergeRequests) + 1,
		title:              request.Title,
		state:              stateOpened,
		sourceBranch:       request.SourceBranch,
		targetBranch:       request.TargetBranch,
		removeSourceBranch: request.RemoveSourceBranch,
		squash:             request.Squash,
		pendingChecks:      1,
	}
	project.mergeRequests = append(project.mergeRequests, mr)
	project.startPipeline(mr.sourceBranch, project.branches[mr.sourceBranch].head, mr.iid)
	writeJSON(w, http.StatusCreated, project.renderMergeRequest(r, mr))
}

func (s *Server) mergeRequestFor(w http.ResponseWriter, r *http.Request) (*project, *mergeRequest, bool) {
	project, ok := s.projectFor(w, r)
	if !ok {
		return nil, nil, false
	}
	iid, err := strconv.Atoi(r.PathValue("iid"))
	if err != nil || iid < 1 || iid > len(project.mergeRequests) {
		writeMessage(w, http.StatusNotFound, "404 Merge Request Not Found")
		return nil, nil, false
	}
	return project, project.mergeRequests[iid-1], true
}

func (s *Server) handleGetMergeRequest(w http.ResponseWriter, r *http.Request) {
	project, mr, ok := s.mergeRequestFor(w, r)
	if !ok {
		return
	}
	rendered := project.renderMergeRequest(r, mr)
	mr.consumeCheck()
	writeJSON(w, http.StatusOK, rendered)
}

func (mr *mergeRequest) consumeCheck() {
	if mr.pendingChecks > 0 {
		mr.pendingChecks--
	}
}

type mergeRequestAccept struct {
	SHA                      string `json:"sha"`
	MergeCommitMessage       string `json:"merge_commit_message"`
	ShouldRemoveSourceBranch *bool  `json:"should_remove_source_branch"`
}

func (s *Server) handleMerge(w http.ResponseWriter, r *http.Request) {
	project, mr, ok := s.mergeRequestFor(w, r)
	if !ok {
		return
	}
	var request mergeRequestAccept
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeMessage(w, http.StatusBadRequest, "400 Bad Request: invalid JSON")
			return
		}
	}
	if project.opts.ForbidMerge {
		writeMessage(w, http.StatusUnauthorized, "401 Unauthorized")
		return
	}
	if project.opts.RequireMergeSHA && request.SHA == "" {
		writeMessage(w, http.StatusBadRequest, "SHA must be provided when merging")
		return
	}
	mergeable := mr.state == stateOpened && project.detailedMergeStatus(mr) == "mergeable"
	mr.consumeCheck()
	if !mergeable {
		writeMessage(w, http.StatusMethodNotAllowed, "405 Method Not Allowed")
		return
	}
	source, target := project.branches[mr.sourceBranch], project.branches[mr.targetBranch]
	if request.SHA != "" && request.SHA != source.head {
		writeMessage(w, http.StatusConflict, "SHA does not match HEAD of source branch: "+source.head)
		return
	}

	message := request.MergeCommitMessage
	if message == "" {
		message = fmt.Sprintf("Merge branch '%s' into '%s'", mr.sourceBranch, mr.targetBranch)
	}
	commitID := project.newCommitID(message)
	for _, c := range project.changesOf(source) {
		if c.deleted {
			delete(target.files, c.path)
			continue
		}
		target.files[c.path] = newFile(c.file.content, commitID)
	}
	target.head = commitID
	project.startPipeline(mr.targetBranch, commitID, 0)

	mr.state = stateMerged
	mr.mergeCommitSHA = commitID
	mr.mergedAt = time.Now()
	removeSource := mr.removeSourceBranch
	if request.ShouldRemoveSourceBranch != nil {
		removeSource = *request.ShouldRemoveSourceBranch
	}
	if removeSource && mr.sourceBranch != project.defaultBranch {
		delete(project.branches, mr.sourceBranch)
	}
	writeJSON(w, http.StatusOK, project.renderMergeRequest(r, mr))
}

func renderPipelines(pipelines []*pipeline) []pipelineJSON {
	rendered := make([]pipelineJSON, 0, len(pipelines))
	for i := len(pipelines) - 1; i >= 0; i-- {
		p := pipelines[i]
		rendered = append(rendered, pipelineJSON{ID: p.id, Ref: p.ref, SHA: p.sha, Status: p.status})
	}
	return rendered
}

func (s *Server) handleMergeRequestPipelines(w http.ResponseWriter, r *http.Request) {
	project, mr, ok := s.mergeRequestFor(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, renderPipelines(project.pipelinesFor(mr)))
}

func (s *Server) handlePipelines(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectFor(w, r)
	if !ok {
		return
	}
	ref := r.URL.Query().Get("ref")
	var matching []*pipeline
	for _, candidate := range project.pipelines {
		if ref == "" || candidate.ref == ref {
			matching = append(matching, candidate)
		}
	}
	writeJSON(w, http.StatusOK, renderPipelines(matching))
}
