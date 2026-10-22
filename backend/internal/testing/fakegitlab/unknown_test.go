package fakegitlab

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"testing"
)

func TestUnknownProjectIsNotFoundEverywhere(t *testing.T) {
	h := newHarness(t, ProjectOptions{})
	base := "/api/v4/projects/" + url.PathEscape("ghost/project")

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, base},
		{http.MethodGet, base + "/repository/tree"},
		{http.MethodGet, base + "/repository/files/a.md"},
		{http.MethodGet, base + "/repository/files/a.md/raw"},
		{http.MethodGet, base + "/repository/blobs/abc/raw"},
		{http.MethodGet, base + "/repository/branches/master"},
		{http.MethodPost, base + "/repository/commits"},
		{http.MethodPost, base + "/merge_requests"},
		{http.MethodGet, base + "/merge_requests/1"},
		{http.MethodPut, base + "/merge_requests/1/merge"},
		{http.MethodGet, base + "/merge_requests/1/pipelines"},
		{http.MethodGet, base + "/pipelines"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			h.expectStatus(h.call(tt.method, tt.path, nil), http.StatusNotFound)
		})
	}
}

func TestSeedDemoDoesNotRunPipelinesForVibefluenceBranches(t *testing.T) {
	fake := New(testToken)
	SeedDemo(fake)

	fake.mu.Lock()
	filter := fake.projects[DemoProject].opts.PipelineBranches
	fake.mu.Unlock()

	if filter("vibefluence/draft") {
		t.Error("vibefluence branches must not run pipelines")
	}
	if !filter("feature/x") || !filter("master") {
		t.Error("other branches must run pipelines")
	}
}

func TestHTTPSSchemeIsReflectedInLinks(t *testing.T) {
	request, _ := http.NewRequest(http.MethodGet, "https://example.test/", nil)
	if requestScheme(request) != "http" {
		t.Error("plain request must report http")
	}
	request.TLS = &tls.ConnectionState{}
	if requestScheme(request) != "https" {
		t.Error("TLS request must report https")
	}
}
