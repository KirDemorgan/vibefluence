package index

import (
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"vibefluence/internal/docs"
)

type Repo struct {
	ID      string
	Name    string
	Project string
	Branch  string
	Paths   []string
}

type Doc struct {
	Repo     string
	Path     string
	Root     string
	BlobID   string
	Title    string
	Status   docs.Status
	Type     string
	Warnings []string
	Body     string
	Raw      string

	lowerTitle string
	lowerPath  string
	lowerBody  string
}

type Snapshot struct {
	Repo     Repo
	Branch   string
	LoadedAt time.Time
	Docs     []*Doc
	Hidden   int

	byPath  map[string]*Doc
	blobIDs map[string]struct{}
}

func newSnapshot(repo Repo, branch string, loadedAt time.Time, all []*Doc, hidden int) *Snapshot {
	sort.Slice(all, func(i, j int) bool { return all[i].Path < all[j].Path })
	snapshot := &Snapshot{Repo: repo, Branch: branch, LoadedAt: loadedAt, Docs: all, Hidden: hidden, byPath: make(map[string]*Doc, len(all))}
	for _, doc := range all {
		doc.lowerTitle = strings.ToLower(doc.Title)
		doc.lowerPath = strings.ToLower(doc.Path)
		doc.lowerBody = strings.ToLower(doc.Body)
		snapshot.byPath[doc.Path] = doc
	}
	return snapshot
}

func (s *Snapshot) Doc(filePath string) (*Doc, bool) {
	doc, ok := s.byPath[filePath]
	return doc, ok
}

type NodeKind string

const (
	KindFolder NodeKind = "folder"
	KindDoc    NodeKind = "doc"
)

type Node struct {
	Name     string
	Path     string
	Kind     NodeKind
	Title    string
	Type     string
	Children []*Node
}

func (s *Snapshot) Tree() []*Node {
	roots := make([]*Node, 0, len(s.Repo.Paths))
	for _, root := range s.Repo.Paths {
		node := &Node{Name: root, Path: root, Kind: KindFolder}
		for _, doc := range s.Docs {
			if doc.Root != root {
				continue
			}
			insert(node, doc, strings.TrimPrefix(doc.Path, root+"/"))
		}
		sortNode(node)
		roots = append(roots, node)
	}
	return roots
}

func insert(parent *Node, doc *Doc, relative string) {
	segments := strings.Split(relative, "/")
	current := parent
	for i, segment := range segments {
		isLeaf := i == len(segments)-1
		if isLeaf {
			current.Children = append(current.Children, &Node{
				Name: segment, Path: doc.Path, Kind: KindDoc, Title: doc.Title, Type: doc.Type,
			})
			return
		}
		current = childFolder(current, segment)
	}
}

func childFolder(parent *Node, name string) *Node {
	for _, child := range parent.Children {
		if child.Kind == KindFolder && child.Name == name {
			return child
		}
	}
	folder := &Node{Name: name, Path: path.Join(parent.Path, name), Kind: KindFolder}
	parent.Children = append(parent.Children, folder)
	return folder
}

func sortNode(node *Node) {
	sort.SliceStable(node.Children, func(i, j int) bool {
		a, b := node.Children[i], node.Children[j]
		if a.Kind != b.Kind {
			return a.Kind == KindFolder
		}
		if rankA, rankB := leadRank(a), leadRank(b); rankA != rankB {
			return rankA < rankB
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	for _, child := range node.Children {
		sortNode(child)
	}
}

func leadRank(node *Node) int {
	if node.Kind != KindDoc {
		return 1
	}
	switch strings.ToLower(strings.TrimSuffix(node.Name, path.Ext(node.Name))) {
	case "readme", "index":
		return 0
	}
	return 1
}

func snippetAround(text string, offset, termLength int) string {
	const radius = 70
	start := max(0, offset-radius)
	end := min(len(text), offset+termLength+radius)
	for start > 0 && !utf8.RuneStart(text[start]) {
		start--
	}
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end++
	}
	snippet := cleanMarkdown(text[start:end])
	if start > 0 {
		snippet = "…" + snippet
	}
	if end < len(text) {
		snippet += "…"
	}
	return snippet
}

var (
	markdownLink  = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	tableDivider  = regexp.MustCompile(`\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?`)
	markdownNoise = strings.NewReplacer("`", "", "*", "", "|", " ", "#", "", ">", "", "~~", "")
)

func cleanMarkdown(fragment string) string {
	fragment = markdownLink.ReplaceAllString(fragment, "$1")
	fragment = tableDivider.ReplaceAllString(fragment, " ")
	fragment = markdownNoise.Replace(fragment)
	return strings.Join(strings.Fields(fragment), " ")
}
