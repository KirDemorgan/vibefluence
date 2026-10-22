package markdown

import (
	"bytes"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"

	"vibefluence/internal/docs"
)

const (
	headingIDPrefix = "h-"
	maxSourceBytes  = 4 << 20
)

type Heading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	ID    string `json:"id"`
}

type Output struct {
	HTML     string
	Headings []Heading
}

type LinkContext struct {
	Repo    string
	DocPath string
	BlobURL func(filePath string) string
}

type Renderer struct {
	engine goldmark.Markdown
	policy *bluemonday.Policy
}

func New() *Renderer {
	engine := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			highlighting.NewHighlighting(
				highlighting.WithFormatOptions(chromahtml.WithClasses(true)),
			),
		),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(html.WithUnsafe()),
	)
	return &Renderer{engine: engine, policy: newPolicy()}
}

var (
	classPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}( [A-Za-z0-9_-]{1,40}){0,8}$`)
	idPattern    = regexp.MustCompile(`^[\p{L}\p{N}_-]{1,120}$`)
	checkbox     = regexp.MustCompile(`^checkbox$`)
	alignment    = regexp.MustCompile(`^(left|center|right)$`)
)

func newPolicy() *bluemonday.Policy {
	policy := bluemonday.UGCPolicy()
	policy.AllowAttrs("class").Matching(classPattern).OnElements("pre", "code", "span", "div", "ul", "li", "table")
	policy.AllowAttrs("id").Matching(idPattern).OnElements("h1", "h2", "h3", "h4", "h5", "h6")
	policy.AllowAttrs("type").Matching(checkbox).OnElements("input")
	policy.AllowAttrs("disabled", "checked").OnElements("input")
	policy.AllowAttrs("align").Matching(alignment).OnElements("th", "td")
	policy.AllowAttrs("style").Matching(regexp.MustCompile(`^text-align:\s*(left|center|right);?$`)).OnElements("th", "td")
	policy.AllowElements("details", "summary", "kbd", "mark")
	policy.AllowURLSchemes("http", "https", "mailto")
	policy.AllowRelativeURLs(true)
	policy.RequireNoReferrerOnLinks(true)
	policy.AddTargetBlankToFullyQualifiedLinks(true)
	return policy
}

func (r *Renderer) Render(source []byte, link LinkContext) (Output, error) {
	if len(source) > maxSourceBytes {
		return Output{}, fmt.Errorf("document is larger than %d bytes", maxSourceBytes)
	}

	reader := text.NewReader(source)
	context := parser.NewContext(parser.WithIDs(newHeadingIDs()))
	root := r.engine.Parser().Parse(reader, parser.WithContext(context))

	var headings []Heading
	err := ast.Walk(root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch typed := node.(type) {
		case *ast.Heading:
			headings = append(headings, prefixHeading(typed, source))
		case *ast.Link:
			typed.Destination = []byte(rewriteLink(string(typed.Destination), link, false))
		case *ast.Image:
			typed.Destination = []byte(rewriteLink(string(typed.Destination), link, true))
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return Output{}, fmt.Errorf("walk markdown: %w", err)
	}

	var rendered bytes.Buffer
	if err := r.engine.Renderer().Render(&rendered, source, root); err != nil {
		return Output{}, fmt.Errorf("render markdown: %w", err)
	}
	return Output{HTML: r.policy.Sanitize(rendered.String()), Headings: headings}, nil
}

func prefixHeading(heading *ast.Heading, source []byte) Heading {
	id := ""
	if value, ok := heading.AttributeString("id"); ok {
		if raw, ok := value.([]byte); ok {
			id = headingIDPrefix + string(raw)
			heading.SetAttributeString("id", []byte(id))
		}
	}
	return Heading{Level: heading.Level, Text: plainText(heading, source), ID: id}
}

func plainText(node ast.Node, source []byte) string {
	var out strings.Builder
	_ = ast.Walk(node, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch typed := child.(type) {
		case *ast.Text:
			out.Write(typed.Segment.Value(source))
		case *ast.String:
			out.Write(typed.Value)
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(out.String())
}

func rewriteLink(destination string, link LinkContext, isImage bool) string {
	destination = strings.TrimSpace(destination)
	switch {
	case destination == "":
		return ""
	case strings.HasPrefix(destination, "//"):
		return "#"
	case strings.HasPrefix(destination, "#"):
		return "#" + headingIDPrefix + strings.ToLower(strings.TrimPrefix(destination, "#"))
	}
	if hasScheme(destination) {
		return destination
	}

	target, fragment := splitFragment(destination)
	if index := strings.IndexByte(target, '?'); index >= 0 {
		target = target[:index]
	}
	resolved, ok := resolveRepoPath(target, link.DocPath)
	if !ok {
		return "#"
	}

	switch {
	case isImage && docs.IsImage(resolved):
		return "/api/v1/repos/" + url.PathEscape(link.Repo) + "/raw?path=" + url.QueryEscape(resolved)
	case !isImage && docs.IsMarkdown(resolved):
		return "/r/" + url.PathEscape(link.Repo) + "/" + escapeSegments(resolved) + fragment
	case link.BlobURL != nil:
		return link.BlobURL(resolved)
	default:
		return "#"
	}
}

func hasScheme(destination string) bool {
	colon := strings.IndexByte(destination, ':')
	if colon <= 0 {
		return false
	}
	for i := 0; i < colon; i++ {
		c := destination[i]
		letter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !letter && (i == 0 || ((c < '0' || c > '9') && c != '+' && c != '-' && c != '.')) {
			return false
		}
	}
	return true
}

func splitFragment(destination string) (target, fragment string) {
	index := strings.IndexByte(destination, '#')
	if index < 0 {
		return destination, ""
	}
	return destination[:index], "#" + headingIDPrefix + strings.ToLower(destination[index+1:])
}

func resolveRepoPath(target, docPath string) (string, bool) {
	decoded, err := url.PathUnescape(target)
	if err != nil || decoded == "" {
		return "", false
	}
	var joined string
	if strings.HasPrefix(decoded, "/") {
		joined = strings.TrimPrefix(decoded, "/")
	} else {
		joined = path.Join(path.Dir(docPath), decoded)
	}
	cleaned := path.Clean(joined)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", false
	}
	return cleaned, true
}

func escapeSegments(filePath string) string {
	segments := strings.Split(filePath, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}

type headingIDs struct {
	used map[string]struct{}
}

func newHeadingIDs() *headingIDs {
	return &headingIDs{used: make(map[string]struct{})}
}

func (h *headingIDs) Generate(value []byte, _ ast.NodeKind) []byte {
	base := slugify(string(value))
	if base == "" {
		base = "section"
	}
	candidate := base
	for suffix := 1; ; suffix++ {
		if _, taken := h.used[candidate]; !taken {
			break
		}
		candidate = base + "-" + strconv.Itoa(suffix)
	}
	h.used[candidate] = struct{}{}
	return []byte(candidate)
}

func (h *headingIDs) Put(value []byte) {
	h.used[string(value)] = struct{}{}
}

func slugify(value string) string {
	var out strings.Builder
	pendingDash := false
	for _, r := range strings.ToLower(value) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			if pendingDash && out.Len() > 0 {
				out.WriteByte('-')
			}
			pendingDash = false
			out.WriteRune(r)
		case r == '-' || unicode.IsSpace(r):
			pendingDash = true
		}
	}
	return out.String()
}
