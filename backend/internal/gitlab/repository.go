package gitlab

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	treePageSize = 100
	maxTreePages = 200
)

type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
}

type Project struct {
	ID            int    `json:"id"`
	Path          string `json:"path_with_namespace"`
	DefaultBranch string `json:"default_branch"`
}

type TreeEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Path string `json:"path"`
	Mode string `json:"mode"`
}

type Branch struct {
	Name     string `json:"name"`
	CommitID string `json:"-"`
}

type File struct {
	Path         string
	Ref          string
	Content      []byte
	BlobID       string
	LastCommitID string
}

func (c *Client) CurrentUser(ctx context.Context) (User, error) {
	var user User
	err := c.getJSON(ctx, "/api/v4/user", nil, &user)
	return user, err
}

func (c *Client) Project(ctx context.Context, project string) (Project, error) {
	var found Project
	if err := c.getJSON(ctx, projectEndpoint(project, ""), nil, &found); err != nil {
		return Project{}, err
	}
	if found.DefaultBranch == "" {
		return Project{}, fmt.Errorf("gitlab: project %q has no default branch", project)
	}
	return found, nil
}

func (c *Client) Tree(ctx context.Context, project, ref, dir string) ([]TreeEntry, error) {
	query := url.Values{}
	query.Set("recursive", "true")
	query.Set("pagination", "keyset")
	query.Set("per_page", fmt.Sprint(treePageSize))
	if ref != "" {
		query.Set("ref", ref)
	}
	if dir != "" {
		query.Set("path", dir)
	}

	var entries []TreeEntry
	path := projectEndpoint(project, "/repository/tree")
	for page := 0; page < maxTreePages; page++ {
		resp, err := c.send(ctx, http.MethodGet, path, query, nil)
		if err != nil {
			return nil, err
		}
		var batch []TreeEntry
		if err := decodeBody(resp, &batch); err != nil {
			return nil, err
		}
		entries = append(entries, batch...)

		next, ok := c.nextPage(resp.header.Get("Link"))
		if !ok {
			return entries, nil
		}
		path, query = next.path, next.query
	}
	return nil, fmt.Errorf("gitlab: tree of %s exceeds %d pages", project, maxTreePages)
}

type pageLink struct {
	path  string
	query url.Values
}

func (c *Client) nextPage(header string) (pageLink, bool) {
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(part, ";")
		if len(fields) < 2 || !strings.Contains(part, `rel="next"`) {
			continue
		}
		raw := strings.Trim(strings.TrimSpace(fields[0]), "<>")
		target, err := url.Parse(raw)
		if err != nil || !strings.EqualFold(target.Host, c.base.Host) {
			return pageLink{}, false
		}
		escaped := target.EscapedPath()
		prefix := strings.TrimRight(c.base.EscapedPath(), "/")
		if !strings.HasPrefix(escaped, prefix+"/api/v4/") {
			return pageLink{}, false
		}
		return pageLink{path: strings.TrimPrefix(escaped, prefix), query: target.Query()}, true
	}
	return pageLink{}, false
}

func (c *Client) Blob(ctx context.Context, project, blobID string) ([]byte, error) {
	resp, err := c.send(ctx, http.MethodGet, projectEndpoint(project, "/repository/blobs/"+url.PathEscape(blobID)+"/raw"), nil, nil)
	if err != nil {
		return nil, err
	}
	return resp.body, nil
}

func (c *Client) File(ctx context.Context, project, ref, filePath string) (File, error) {
	if ref == "" {
		return File{}, errors.New("gitlab: ref is required to read a file")
	}
	query := url.Values{"ref": {ref}}
	var decoded struct {
		FilePath     string `json:"file_path"`
		Ref          string `json:"ref"`
		Encoding     string `json:"encoding"`
		Content      string `json:"content"`
		BlobID       string `json:"blob_id"`
		LastCommitID string `json:"last_commit_id"`
	}
	endpoint := projectEndpoint(project, "/repository/files/"+url.PathEscape(filePath))
	if err := c.getJSON(ctx, endpoint, query, &decoded); err != nil {
		return File{}, err
	}
	if decoded.Encoding != "base64" {
		return File{}, fmt.Errorf("gitlab: unsupported file encoding %q", decoded.Encoding)
	}
	content, err := base64.StdEncoding.DecodeString(decoded.Content)
	if err != nil {
		return File{}, fmt.Errorf("gitlab: decode file content: %w", err)
	}
	return File{
		Path:         decoded.FilePath,
		Ref:          decoded.Ref,
		Content:      content,
		BlobID:       decoded.BlobID,
		LastCommitID: decoded.LastCommitID,
	}, nil
}

func (c *Client) Branch(ctx context.Context, project, name string) (Branch, error) {
	var decoded struct {
		Name   string `json:"name"`
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err := c.getJSON(ctx, projectEndpoint(project, "/repository/branches/"+url.PathEscape(name)), nil, &decoded); err != nil {
		return Branch{}, err
	}
	return Branch{Name: decoded.Name, CommitID: decoded.Commit.ID}, nil
}
