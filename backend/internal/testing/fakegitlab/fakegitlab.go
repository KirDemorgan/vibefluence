package fakegitlab

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
)

const defaultBotUsername = "vibefluence-bot"

type ProjectOptions struct {
	PipelinesMustSucceed bool
	ApprovalRequired     bool
	ForbidMerge          bool
	RequireMergeSHA      bool
	PipelineBranches     func(branch string) bool
}

type MergeRequestInfo struct {
	IID                 int
	Title               string
	State               string
	SourceBranch        string
	TargetBranch        string
	DetailedMergeStatus string
	MergeCommitSHA      string
}

type PipelineInfo struct {
	ID     int
	Ref    string
	Status string
}

type Server struct {
	mu          sync.Mutex
	token       string
	publicReads atomic.Bool
	botName     string
	nextID      int
	projects    map[string]*project
	failNext    int
	failStatus  int
	requests    int
	mux         *http.ServeMux
}

func New(token string) *Server {
	s := &Server{
		token:    token,
		botName:  defaultBotUsername,
		nextID:   1,
		projects: make(map[string]*project),
		mux:      http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.requests++
	if !s.authorized(r) {
		writeMessage(w, http.StatusUnauthorized, "401 Unauthorized")
		return
	}
	if s.failNext > 0 {
		s.failNext--
		if s.failStatus == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "1")
		}
		writeMessage(w, s.failStatus, http.StatusText(s.failStatus))
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) authorized(r *http.Request) bool {
	if s.publicReads.Load() && (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.URL.Path != "/api/v4/user" {
		return true
	}
	if r.Header.Get("PRIVATE-TOKEN") == s.token {
		return s.token != ""
	}
	return s.token != "" && r.Header.Get("Authorization") == "Bearer "+s.token
}

func (s *Server) SetPublicReads(enabled bool) {
	s.publicReads.Store(enabled)
}

func (s *Server) SetBotUsername(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.botName = name
}

func (s *Server) FailNext(count, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failNext = count
	s.failStatus = status
}

func (s *Server) RequestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

func (s *Server) AddProject(path, defaultBranch string, opts ProjectOptions) {
	s.mu.Lock()
	defer s.mu.Unlock()

	project := &project{
		id:            s.nextID,
		path:          path,
		defaultBranch: defaultBranch,
		branches:      make(map[string]*branch),
		opts:          opts,
	}
	s.nextID++
	project.branches[defaultBranch] = &branch{
		head:  project.newCommitID("initial commit"),
		files: make(map[string]file),
		base:  make(map[string]string),
	}
	s.projects[path] = project
}

func (s *Server) SetProjectOptions(path string, opts ProjectOptions) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mustProject(path).opts = opts
}

func (s *Server) PutFile(projectPath, branchName, filePath, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	project := s.mustProject(projectPath)
	target := project.mustBranch(branchName)
	commitID := project.newCommitID("put " + filePath)
	target.files[filePath] = newFile([]byte(content), commitID)
	target.head = commitID
}

func (s *Server) DeleteFile(projectPath, branchName, filePath string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	project := s.mustProject(projectPath)
	target := project.mustBranch(branchName)
	delete(target.files, filePath)
	target.head = project.newCommitID("delete " + filePath)
}

func (s *Server) File(projectPath, branchName, filePath string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	project := s.mustProject(projectPath)
	target, ok := project.branches[branchName]
	if !ok {
		return "", false
	}
	stored, ok := target.files[filePath]
	return string(stored.content), ok
}

func (s *Server) Branches(projectPath string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	project := s.mustProject(projectPath)
	names := make([]string, 0, len(project.branches))
	for name := range project.branches {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s *Server) MergeRequests(projectPath string) []MergeRequestInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	project := s.mustProject(projectPath)
	infos := make([]MergeRequestInfo, 0, len(project.mergeRequests))
	for _, mr := range project.mergeRequests {
		infos = append(infos, MergeRequestInfo{
			IID:                 mr.iid,
			Title:               mr.title,
			State:               mr.state,
			SourceBranch:        mr.sourceBranch,
			TargetBranch:        mr.targetBranch,
			DetailedMergeStatus: project.detailedMergeStatus(mr),
			MergeCommitSHA:      mr.mergeCommitSHA,
		})
	}
	return infos
}

func (s *Server) Pipelines(projectPath string) []PipelineInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	project := s.mustProject(projectPath)
	infos := make([]PipelineInfo, 0, len(project.pipelines))
	for _, pipeline := range project.pipelines {
		infos = append(infos, PipelineInfo{ID: pipeline.id, Ref: pipeline.ref, Status: pipeline.status})
	}
	return infos
}

func (s *Server) CompletePipelines(projectPath string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, pipeline := range s.mustProject(projectPath).pipelines {
		pipeline.status = "success"
	}
}

func (s *Server) FailPipelines(projectPath string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, pipeline := range s.mustProject(projectPath).pipelines {
		pipeline.status = "failed"
	}
}

func (s *Server) ApproveMergeRequest(projectPath string, iid int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	project := s.mustProject(projectPath)
	project.mustMergeRequest(iid).approved = true
}

func (s *Server) mustProject(path string) *project {
	project, ok := s.projects[path]
	if !ok {
		panic(fmt.Sprintf("fakegitlab: unknown project %q", path))
	}
	return project
}

type file struct {
	content      []byte
	blobID       string
	lastCommitID string
}

func newFile(content []byte, commitID string) file {
	return file{content: content, blobID: gitBlobID(content), lastCommitID: commitID}
}

func gitBlobID(content []byte) string {
	hash := sha1.New()
	_, _ = fmt.Fprintf(hash, "blob %d\x00", len(content))
	hash.Write(content)
	return hex.EncodeToString(hash.Sum(nil))
}

func fakeTreeID(path string) string {
	sum := sha1.Sum([]byte("tree " + path))
	return hex.EncodeToString(sum[:])
}

type branch struct {
	head  string
	files map[string]file
	base  map[string]string
}

func (b *branch) fork(head string) *branch {
	forked := &branch{
		head:  head,
		files: make(map[string]file, len(b.files)),
		base:  make(map[string]string, len(b.files)),
	}
	for path, stored := range b.files {
		forked.files[path] = stored
		forked.base[path] = stored.blobID
	}
	return forked
}

type pipeline struct {
	id     int
	ref    string
	sha    string
	status string
	mrIID  int
}

type project struct {
	id            int
	path          string
	defaultBranch string
	branches      map[string]*branch
	mergeRequests []*mergeRequest
	pipelines     []*pipeline
	opts          ProjectOptions
	seq           int
}

func (p *project) newCommitID(seed string) string {
	p.seq++
	sum := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%s", p.path, p.seq, seed)))
	return hex.EncodeToString(sum[:])
}

func (p *project) mustBranch(name string) *branch {
	found, ok := p.branches[name]
	if !ok {
		panic(fmt.Sprintf("fakegitlab: unknown branch %q in %q", name, p.path))
	}
	return found
}

func (p *project) mustMergeRequest(iid int) *mergeRequest {
	if iid < 1 || iid > len(p.mergeRequests) {
		panic(fmt.Sprintf("fakegitlab: unknown merge request !%d in %q", iid, p.path))
	}
	return p.mergeRequests[iid-1]
}

func (p *project) startPipeline(ref, sha string, mrIID int) {
	if p.opts.PipelineBranches == nil || !p.opts.PipelineBranches(ref) {
		return
	}
	p.pipelines = append(p.pipelines, &pipeline{
		id:     len(p.pipelines) + 1,
		ref:    ref,
		sha:    sha,
		status: "pending",
		mrIID:  mrIID,
	})
}

func (p *project) pipelinesFor(mr *mergeRequest) []*pipeline {
	var related []*pipeline
	for _, candidate := range p.pipelines {
		if candidate.mrIID == mr.iid || (candidate.mrIID == 0 && candidate.ref == mr.sourceBranch) {
			related = append(related, candidate)
		}
	}
	return related
}
