package docs

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseFrontMatter(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		path         string
		wantTitle    string
		wantStatus   Status
		wantType     string
		wantBody     string
		wantWarnings []string
	}{
		{
			name:       "full front matter",
			content:    "---\ntitle: Rollout\nstatus: published\ntype: plan\n---\n# Heading\n\ntext\n",
			path:       "docs/a.md",
			wantTitle:  "Rollout",
			wantStatus: StatusPublished,
			wantType:   "plan",
			wantBody:   "# Heading\n\ntext\n",
		},
		{
			name:       "draft status is case insensitive",
			content:    "---\nstatus: DRAFT\n---\nbody",
			path:       "docs/a.md",
			wantTitle:  "a",
			wantStatus: StatusDraft,
			wantBody:   "body",
		},
		{
			name:       "no front matter means published and title from heading",
			content:    "# Ticket 7\n\ntext",
			path:       "docs/tickets/7.md",
			wantTitle:  "Ticket 7",
			wantStatus: StatusPublished,
			wantBody:   "# Ticket 7\n\ntext",
		},
		{
			name:       "front matter without status is published",
			content:    "---\ntitle: T\n---\nx",
			path:       "docs/a.md",
			wantTitle:  "T",
			wantStatus: StatusPublished,
			wantBody:   "x",
		},
		{
			name:       "empty status is published",
			content:    "---\nstatus:\n---\nx",
			path:       "docs/a.md",
			wantTitle:  "a",
			wantStatus: StatusPublished,
			wantBody:   "x",
		},
		{
			name:         "unknown status is hidden as a draft",
			content:      "---\nstatus: wip\n---\nx",
			path:         "docs/a.md",
			wantTitle:    "a",
			wantStatus:   StatusDraft,
			wantBody:     "x",
			wantWarnings: []string{`unknown status "wip", treated as draft`},
		},
		{
			name:         "invalid yaml is shown with a warning",
			content:      "---\ntitle: [unclosed\n---\nbody",
			path:         "docs/a.md",
			wantTitle:    "a",
			wantStatus:   StatusPublished,
			wantBody:     "body",
			wantWarnings: []string{"front matter is not valid YAML"},
		},
		{
			name:         "unclosed front matter keeps the whole text",
			content:      "---\ntitle: x\nno end",
			path:         "docs/a.md",
			wantTitle:    "a",
			wantStatus:   StatusPublished,
			wantBody:     "---\ntitle: x\nno end",
			wantWarnings: []string{"front matter is not closed"},
		},
		{
			name:       "windows line endings and dots as terminator",
			content:    "---\r\ntitle: Win\r\nstatus: draft\r\n...\r\nbody\r\n",
			path:       "docs/a.md",
			wantTitle:  "Win",
			wantStatus: StatusDraft,
			wantBody:   "body\r\n",
		},
		{
			name:       "byte order mark is ignored",
			content:    "\xef\xbb\xbf---\ntitle: Bom\n---\nx",
			path:       "docs/a.md",
			wantTitle:  "Bom",
			wantStatus: StatusPublished,
			wantBody:   "x",
		},
		{
			name:       "horizontal rule that is not on the first line is not front matter",
			content:    "intro\n---\nstatus: draft\n---\n",
			path:       "docs/a.md",
			wantTitle:  "a",
			wantStatus: StatusPublished,
			wantBody:   "intro\n---\nstatus: draft\n---\n",
		},
		{
			name:       "numeric title is converted to text",
			content:    "---\ntitle: 2026\n---\nx",
			path:       "docs/a.md",
			wantTitle:  "2026",
			wantStatus: StatusPublished,
			wantBody:   "x",
		},
		{
			name:       "empty file falls back to the file name",
			content:    "",
			path:       "docs/guide/setup-notes.md",
			wantTitle:  "setup-notes",
			wantStatus: StatusPublished,
			wantBody:   "",
		},
		{
			name:         "only an opening marker line",
			content:      "---",
			path:         "docs/a.md",
			wantTitle:    "a",
			wantStatus:   StatusPublished,
			wantBody:     "---",
			wantWarnings: []string{"front matter is not closed"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := Parse([]byte(tt.content), tt.path)
			if doc.Title != tt.wantTitle {
				t.Errorf("title = %q, want %q", doc.Title, tt.wantTitle)
			}
			if doc.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", doc.Status, tt.wantStatus)
			}
			if doc.Type != tt.wantType {
				t.Errorf("type = %q, want %q", doc.Type, tt.wantType)
			}
			if doc.Body != tt.wantBody {
				t.Errorf("body = %q, want %q", doc.Body, tt.wantBody)
			}
			if fmt.Sprint(doc.Warnings) != fmt.Sprint(tt.wantWarnings) {
				t.Errorf("warnings = %v, want %v", doc.Warnings, tt.wantWarnings)
			}
		})
	}
}

func TestParseRejectsOversizedFrontMatter(t *testing.T) {
	content := "---\n" + strings.Repeat("key: value\n", 3000) + "---\nbody"
	doc := Parse([]byte(content), "docs/a.md")
	if len(doc.Warnings) != 1 || doc.Warnings[0] != "front matter is too large" {
		t.Errorf("warnings = %v", doc.Warnings)
	}
	if doc.Body != content {
		t.Error("oversized front matter must leave the body untouched")
	}
}

func TestFirstHeading(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"simple", "# Hello\n", "Hello"},
		{"closing hashes are trimmed", "# Hello ##\n", "Hello"},
		{"first heading wins", "intro\n\n# One\n# Two\n", "One"},
		{"level two is not a title", "## Sub\n", ""},
		{"hash without a space is not a heading", "#tag\n", ""},
		{"fenced code is skipped", "```\n# not a title\n```\n# Real\n", "Real"},
		{"tilde fences are skipped", "~~~\n# no\n~~~\n# Yes\n", "Yes"},
		{"indented code is skipped", "    # code\n# Real\n", "Real"},
		{"up to three spaces of indent are allowed", "   # Indented\n", "Indented"},
		{"unterminated fence swallows the rest", "```\n# lost\n", ""},
		{"longer fence is not closed by a shorter one", "````\n```\n# inside\n````\n# Out\n", "Out"},
		{"carriage returns", "# Win\r\nbody\r\n", "Win"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FirstHeading(tt.body); got != tt.want {
				t.Errorf("FirstHeading = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTitleFromPathFallbacks(t *testing.T) {
	for _, in := range []string{"", ".", "/", ".md"} {
		got := titleFromPath(in)
		if got == "" {
			t.Errorf("titleFromPath(%q) is empty", in)
		}
	}
	if got := titleFromPath("docs/a.markdown"); got != "a" {
		t.Errorf("got %q", got)
	}
}

func TestWithStatus(t *testing.T) {
	tests := []struct {
		name    string
		content string
		status  Status
		want    string
	}{
		{
			name:    "replaces an existing status",
			content: "---\ntitle: T\nstatus: draft\n---\nbody\n",
			status:  StatusPublished,
			want:    "---\ntitle: T\nstatus: published\n---\nbody\n",
		},
		{
			name:    "adds a missing status to existing front matter",
			content: "---\ntitle: T\n---\nbody\n",
			status:  StatusDraft,
			want:    "---\ntitle: T\nstatus: draft\n---\nbody\n",
		},
		{
			name:    "creates front matter when absent",
			content: "# Title\n",
			status:  StatusDraft,
			want:    "---\nstatus: draft\n---\n\n# Title\n",
		},
		{
			name:    "preserves windows line endings",
			content: "---\r\nstatus: draft\r\n---\r\nx",
			status:  StatusPublished,
			want:    "---\r\nstatus: published\r\n---\r\nx",
		},
		{
			name:    "adds status with windows line endings",
			content: "---\r\ntitle: T\r\n---\r\nx",
			status:  StatusPublished,
			want:    "---\r\ntitle: T\r\nstatus: published\r\n---\r\nx",
		},
		{
			name:    "creates front matter with windows line endings",
			content: "text\r\nmore",
			status:  StatusDraft,
			want:    "---\r\nstatus: draft\r\n---\r\n\r\ntext\r\nmore",
		},
		{
			name:    "does not touch a status line in the body",
			content: "---\ntitle: T\n---\nstatus: draft\n",
			status:  StatusPublished,
			want:    "---\ntitle: T\nstatus: published\n---\nstatus: draft\n",
		},
		{
			name:    "unclosed front matter is wrapped as plain text",
			content: "---\ntitle: x\n",
			status:  StatusDraft,
			want:    "---\nstatus: draft\n---\n\n---\ntitle: x\n",
		},
		{
			name:    "byte order mark is dropped",
			content: "\xef\xbb\xbf---\nstatus: draft\n---\n",
			status:  StatusPublished,
			want:    "---\nstatus: published\n---\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WithStatus(tt.content, tt.status); got != tt.want {
				t.Errorf("WithStatus =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestWithStatusRoundTripsThroughParse(t *testing.T) {
	inputs := []string{"", "# T\n", "---\ntitle: x\n---\nbody", "---\nstatus: wip\n---\n", "---\r\nstatus: draft\r\n---\r\n"}
	for _, input := range inputs {
		for _, status := range []Status{StatusDraft, StatusPublished} {
			output := WithStatus(input, status)
			if got := Parse([]byte(output), "docs/a.md").Status; got != status {
				t.Errorf("WithStatus(%q, %s) parsed back as %s", input, status, got)
			}
		}
	}
}

func TestCompose(t *testing.T) {
	got := Compose(NewDocument{Title: `Say "hi"`, Status: StatusDraft, Type: "adr", Body: "\n\n# Body"})
	want := "---\ntitle: \"Say \\\"hi\\\"\"\nstatus: draft\ntype: \"adr\"\n---\n\n# Body\n"
	if got != want {
		t.Errorf("Compose =\n%q\nwant\n%q", got, want)
	}

	parsed := Parse([]byte(got), "docs/a.md")
	if parsed.Title != `Say "hi"` || parsed.Status != StatusDraft || parsed.Type != "adr" {
		t.Errorf("round trip lost data: %+v", parsed)
	}

	minimal := Compose(NewDocument{Title: "T", Status: StatusPublished, Body: "text\n"})
	if strings.Contains(minimal, "type:") || !strings.HasSuffix(minimal, "text\n") {
		t.Errorf("minimal = %q", minimal)
	}
}

func TestComposeEscapesHostileTitles(t *testing.T) {
	titles := []string{"line1\nstatus: draft", "tab\there", "back\\slash", "cr\rhere", `"quoted"`, "---", ": colon", "- dash", "# hash"}
	for _, title := range titles {
		parsed := Parse([]byte(Compose(NewDocument{Title: title, Status: StatusPublished, Body: "x"})), "docs/a.md")
		if parsed.Title != title {
			t.Errorf("title %q came back as %q", title, parsed.Title)
		}
		if parsed.Status != StatusPublished {
			t.Errorf("title %q changed the status to %s", title, parsed.Status)
		}
		if len(parsed.Warnings) != 0 {
			t.Errorf("title %q produced warnings %v", title, parsed.Warnings)
		}
	}
}

func TestValidateDocPath(t *testing.T) {
	roots := []string{"docs", "specs/internal"}
	valid := []string{"docs/a.md", "docs/guide/start.markdown", "specs/internal/x.MD", "docs/спека.md", "docs/with space.md"}
	for _, p := range valid {
		if err := ValidateDocPath(p, roots); err != nil {
			t.Errorf("%q should be valid: %v", p, err)
		}
	}
	invalid := map[string]string{
		"":                  "empty",
		"/docs/a.md":        "relative",
		"docs/a.md/":        "relative",
		"docs\\a.md":        "forward slashes",
		"docs/../secret.md": "relative segments",
		"docs//a.md":        "empty or relative",
		"docs/./a.md":       "empty or relative",
		"docs/.hidden/a.md": "dot",
		"docs/.a.md":        "dot",
		"docs/a.txt":        ".md",
		"docs/a":            ".md",
		"other/a.md":        "inside one of",
		"docsx/a.md":        "inside one of",
		"docs.md":           "inside one of",
		"docs/a\x00.md":     "control",
		"docs/a\n.md":       "control",
		"docs/ a.md":        "spaces",
		"docs/" + strings.Repeat("x", 300) + ".md": "too long",
		"docs/\xff.md": "UTF-8",
	}
	for p, wantFragment := range invalid {
		err := ValidateDocPath(p, roots)
		if err == nil {
			t.Errorf("%q should be invalid", p)
			continue
		}
		if !strings.Contains(err.Error(), wantFragment) {
			t.Errorf("%q: error %q does not mention %q", p, err, wantFragment)
		}
	}
}

func TestIsMarkdownAndIsImage(t *testing.T) {
	for _, p := range []string{"a.md", "A.MD", "x/y.markdown"} {
		if !IsMarkdown(p) {
			t.Errorf("%q should be markdown", p)
		}
	}
	for _, p := range []string{"a.txt", "a", "a.md.bak"} {
		if IsMarkdown(p) {
			t.Errorf("%q should not be markdown", p)
		}
	}
	for _, p := range []string{"a.png", "b.JPG", "c.jpeg", "d.gif", "e.webp", "f.avif"} {
		if !IsImage(p) {
			t.Errorf("%q should be an image", p)
		}
	}
	for _, p := range []string{"a.svg", "a.html", "a.md", "a"} {
		if IsImage(p) {
			t.Errorf("%q must not be served as an image", p)
		}
	}
}

func TestSlug(t *testing.T) {
	tests := map[string]string{
		"Rollout Plan":             "rollout-plan",
		"  Spaces   everywhere  ":  "spaces-everywhere",
		"Что-то на русском":        "что-то-на-русском",
		"!!!":                      "untitled",
		"":                         "untitled",
		"a/b\\c":                   "a-b-c",
		"Ünïcode & symbols *2026*": "ünïcode-symbols-2026",
	}
	for input, want := range tests {
		if got := Slug(input); got != want {
			t.Errorf("Slug(%q) = %q, want %q", input, got, want)
		}
	}
	long := Slug(strings.Repeat("слово ", 40))
	if len(long) > 60 || !utf8.ValidString(long) || strings.HasSuffix(long, "-") {
		t.Errorf("long slug = %q (%d bytes)", long, len(long))
	}
}

func FuzzParse(f *testing.F) {
	for _, seed := range []string{"", "---\n---\n", "---\ntitle: x\n---\n# h", "---", "\xef\xbb\xbf---\r\n...", "---\n[\n---\n", "# t"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, content string) {
		doc := Parse([]byte(content), "docs/f.md")
		if doc.Status != StatusPublished && doc.Status != StatusDraft {
			t.Fatalf("invalid status %q", doc.Status)
		}
		if doc.Title == "" {
			t.Fatal("title must never be empty")
		}
		if !strings.HasSuffix(content, doc.Body) && !strings.HasSuffix(strings.TrimPrefix(content, "\xef\xbb\xbf"), doc.Body) {
			t.Fatalf("body %q is not a suffix of the input", doc.Body)
		}
	})
}

func FuzzWithStatus(f *testing.F) {
	for _, seed := range []string{"", "---\nstatus: draft\n---\n", "text", "---\ntitle: x\n---\nbody", "---\r\n...\r\n"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, content string) {
		for _, status := range []Status{StatusDraft, StatusPublished} {
			updated, err := EnsureStatus(content, status)
			if err != nil {
				continue
			}
			got := Parse([]byte(updated), "docs/f.md")
			if got.Status != status || len(got.Warnings) > 0 {
				t.Fatalf("EnsureStatus returned content that parses as %s with warnings %v: %q", got.Status, got.Warnings, updated)
			}
		}
	})
}

func FuzzValidateDocPath(f *testing.F) {
	for _, seed := range []string{"docs/a.md", "../x.md", "docs/a/../b.md", "", "docs/\x00.md"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, filePath string) {
		if ValidateDocPath(filePath, []string{"docs"}) != nil {
			return
		}
		if !strings.HasPrefix(filePath, "docs/") || strings.Contains(filePath, "..") && strings.Contains(filePath, "/../") {
			t.Fatalf("accepted unsafe path %q", filePath)
		}
		for _, segment := range strings.Split(filePath, "/") {
			if segment == ".." || segment == "." || segment == "" {
				t.Fatalf("accepted path with segment %q: %q", segment, filePath)
			}
		}
	})
}

func FuzzSlug(f *testing.F) {
	for _, seed := range []string{"", "Hello World", "!!!", "ÿ\xff"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, title string) {
		slug := Slug(title)
		if slug == "" || len(slug) > 60 || !utf8.ValidString(slug) || strings.ContainsAny(slug, "/\\ ") {
			t.Fatalf("bad slug %q for %q", slug, title)
		}
	})
}

func TestEnsureStatus(t *testing.T) {
	updated, err := EnsureStatus("---\ntitle: T\nstatus: draft\n---\nbody", StatusPublished)
	if err != nil || updated != "---\ntitle: T\nstatus: published\n---\nbody" {
		t.Errorf("updated = %q, err = %v", updated, err)
	}

	created, err := EnsureStatus("# Plain\n", StatusDraft)
	if err != nil || Parse([]byte(created), "a.md").Status != StatusDraft {
		t.Errorf("created = %q, err = %v", created, err)
	}

	for _, broken := range []string{"---\n0\n---\nbody", "---\ntitle: [oops\n---\nbody"} {
		if _, err := EnsureStatus(broken, StatusPublished); err == nil {
			if parsed := Parse([]byte(broken), "a.md"); len(parsed.Warnings) > 0 {
				t.Errorf("broken front matter %q was accepted", broken)
			}
		}
	}
	if _, err := EnsureStatus("---\n0\n---\nbody", StatusDraft); !errors.Is(err, ErrBrokenFrontMatter) {
		t.Errorf("err = %v", err)
	}
}
