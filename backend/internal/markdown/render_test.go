package markdown

import (
	"regexp"
	"strings"
	"testing"
)

var link = LinkContext{
	Repo:    "platform",
	DocPath: "docs/guide/start.md",
	BlobURL: func(p string) string { return "https://gitlab.example.com/g/p/-/blob/master/" + p },
}

func render(t *testing.T, source string) Output {
	t.Helper()
	out, err := New().Render([]byte(source), link)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return out
}

func TestRendersBasicMarkdown(t *testing.T) {
	out := render(t, "# Title\n\nSome **bold** and `code`.\n\n- one\n- two\n")
	for _, want := range []string{"<h1", "Title</h1>", "<strong>bold</strong>", "<code>code</code>", "<li>one</li>"} {
		if !strings.Contains(out.HTML, want) {
			t.Errorf("html lacks %q: %s", want, out.HTML)
		}
	}
}

func TestHeadingsGetPrefixedIDsAndTableOfContents(t *testing.T) {
	out := render(t, "# Top\n\n## Second Section\n\n### Deep *emphasis* `code`\n\n## Второй раздел\n")
	if len(out.Headings) != 4 {
		t.Fatalf("headings = %+v", out.Headings)
	}
	want := []Heading{
		{1, "Top", "h-top"},
		{2, "Second Section", "h-second-section"},
		{3, "Deep emphasis code", "h-deep-emphasis-code"},
		{2, "Второй раздел", "h-второй-раздел"},
	}
	for i, heading := range want {
		if out.Headings[i] != heading {
			t.Errorf("heading %d = %+v, want %+v", i, out.Headings[i], heading)
		}
		if !strings.Contains(out.HTML, `id="`+heading.ID+`"`) {
			t.Errorf("html lacks id %q: %s", heading.ID, out.HTML)
		}
	}
}

func TestInPageAnchorsPointAtPrefixedIDs(t *testing.T) {
	out := render(t, "# My Section\n\n[jump](#My-Section)\n")
	if !strings.Contains(out.HTML, `href="#h-my-section"`) {
		t.Errorf("html = %s", out.HTML)
	}
}

func TestRelativeLinksAreRewritten(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{"sibling markdown", "[x](other.md)", `href="/r/platform/docs/guide/other.md"`},
		{"parent markdown", "[x](../intro.md)", `href="/r/platform/docs/intro.md"`},
		{"markdown with fragment", "[x](../intro.md#Setup)", `href="/r/platform/docs/intro.md#h-setup"`},
		{"repo absolute path", "[x](/docs/root.md)", `href="/r/platform/docs/root.md"`},
		{"dot slash", "[x](./sub/page.md)", `href="/r/platform/docs/guide/sub/page.md"`},
		{"percent encoded", "[x](my%20page.md)", `href="/r/platform/docs/guide/my%20page.md"`},
		{"query is dropped", "[x](other.md?raw=1)", `href="/r/platform/docs/guide/other.md"`},
		{"non markdown file goes to GitLab", "[x](../../src/main.go)", `href="https://gitlab.example.com/g/p/-/blob/master/src/main.go"`},
		{"escaping the repository", "[x](../../../etc/passwd.md)", "<p>x</p>"},
		{"deep escape", "[x](../../../../x.md)", "<p>x</p>"},
		{"protocol relative is neutralised", "[x](//evil.example/x)", "<p>x</p>"},
		{"external https", "[x](https://example.com/a)", `href="https://example.com/a"`},
		{"mailto", "[x](mailto:a@example.com)", `href="mailto:a@example.com"`},
		{"broken escape", "[x](bad%zz.md)", "<p>x</p>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := render(t, tt.source)
			if !strings.Contains(out.HTML, tt.want) {
				t.Errorf("html = %s, want %s", out.HTML, tt.want)
			}
		})
	}
}

func TestExternalLinksOpenSafely(t *testing.T) {
	out := render(t, "[x](https://example.com)")
	for _, want := range []string{`target="_blank"`, "noopener", "noreferrer"} {
		if !strings.Contains(out.HTML, want) {
			t.Errorf("html lacks %q: %s", want, out.HTML)
		}
	}
}

func TestNonMarkdownLinkWithoutBlobURLIsInert(t *testing.T) {
	out, err := New().Render([]byte("[x](file.go)"), LinkContext{Repo: "r", DocPath: "docs/a.md"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.HTML, "<a") || !strings.Contains(out.HTML, "x") {
		t.Errorf("an unresolvable link must degrade to plain text: %s", out.HTML)
	}
}

func TestImagesAreServedThroughTheRawEndpoint(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{"![a](diagram.png)", `src="/api/v1/repos/platform/raw?path=docs%2Fguide%2Fdiagram.png"`},
		{"![a](../img/pic one.png)", ""},
		{"![a](../img/pic%20one.png)", `src="/api/v1/repos/platform/raw?path=docs%2Fimg%2Fpic+one.png"`},
		{"![a](/assets/logo.webp)", `src="/api/v1/repos/platform/raw?path=assets%2Flogo.webp"`},
		{"![a](https://example.com/x.png)", `src="https://example.com/x.png"`},
		{"![a](vector.svg)", `src="https://gitlab.example.com/g/p/-/blob/master/docs/guide/vector.svg"`},
		{"![a](../../../outside.png)", `<img alt="a">`},
	}
	for _, tt := range tests {
		out := render(t, tt.source)
		if tt.want != "" && !strings.Contains(out.HTML, tt.want) {
			t.Errorf("%s: html = %s, want %s", tt.source, out.HTML, tt.want)
		}
	}
}

func TestHighlightsCodeWithClassesInsteadOfInlineStyles(t *testing.T) {
	out := render(t, "```go\npackage main\n\nfunc main() {}\n```\n")
	if !strings.Contains(out.HTML, `class="chroma"`) || !strings.Contains(out.HTML, `<span class="kd">func</span>`) {
		t.Errorf("html = %s", out.HTML)
	}
	if strings.Contains(out.HTML, "style=") {
		t.Errorf("inline styles must not survive sanitising: %s", out.HTML)
	}
}

func TestUnknownLanguageStillRenders(t *testing.T) {
	out := render(t, "```nonsense-lang\nx := 1\n```\n")
	if !strings.Contains(out.HTML, "x := 1") {
		t.Errorf("html = %s", out.HTML)
	}
}

func TestTablesTaskListsAndStrikethrough(t *testing.T) {
	out := render(t, "| a | b |\n|:--|--:|\n| 1 | 2 |\n\n- [x] done\n- [ ] todo\n\n~~gone~~\n")
	for _, want := range []string{"<table>", "<th", "<td", `type="checkbox"`, "disabled", "<del>gone</del>"} {
		if !strings.Contains(out.HTML, want) {
			t.Errorf("html lacks %q: %s", want, out.HTML)
		}
	}
}

func TestAutolinksAndDetails(t *testing.T) {
	out := render(t, "see https://example.com/page now\n\n<details><summary>More</summary>\n\nhidden **text**\n\n</details>\n")
	if !strings.Contains(out.HTML, `href="https://example.com/page"`) {
		t.Errorf("autolink missing: %s", out.HTML)
	}
	if !strings.Contains(out.HTML, "<details>") || !strings.Contains(out.HTML, "<summary>More</summary>") {
		t.Errorf("details missing: %s", out.HTML)
	}
}

var (
	eventHandler = regexp.MustCompile(`(?i)<[^>]*[\s"'/]on[a-z]+\s*=`)
	activeURL    = regexp.MustCompile(`(?i)<[^>]*[\s"'/](href|src|action|formaction|data|srcset|poster|background)\s*=\s*["']?\s*(javascript|vbscript|data):`)
	dangerous    = []string{"<script", "<iframe", "<style", "<svg", "<object", "<embed", "<form", "<link", "<meta", "<base", "data:text/html", "<math", "srcdoc"}
)

var hostilePayloads = []string{
	"<script>alert(1)</script>",
	"<SCRIPT SRC=//evil.example/x.js></SCRIPT>",
	"[x](javascript:alert(1))",
	"[x](JaVaScRiPt:alert(1))",
	"[x](&#106;avascript:alert(1))",
	"[x](java\tscript:alert(1))",
	"[x]( javascript:alert(1))",
	"![x](javascript:alert(1))",
	"[x](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)",
	"[x](vbscript:msgbox(1))",
	"<img src=x onerror=alert(1)>",
	"<a href=\"javascript:alert(1)\">x</a>",
	"<a href='  javascript:alert(1)'>x</a>",
	"<iframe src=\"https://evil.example\"></iframe>",
	"<iframe srcdoc=\"<script>alert(1)</script>\"></iframe>",
	"<svg onload=alert(1)></svg>",
	"<svg><script>alert(1)</script></svg>",
	"<math><mi xlink:href=\"javascript:alert(1)\">x</mi></math>",
	"<div style=\"background:url(javascript:alert(1))\">x</div>",
	"<div style=\"position:fixed;top:0;left:0;width:100%;height:100%\">overlay</div>",
	"<style>body{display:none}</style>",
	"<link rel=stylesheet href=//evil.example/x.css>",
	"<meta http-equiv=refresh content=\"0;url=//evil.example\">",
	"<base href=//evil.example/>",
	"<form action=//evil.example><input name=x></form>",
	"<object data=\"//evil.example/x.swf\"></object>",
	"<embed src=\"//evil.example/x.swf\">",
	"<details open ontoggle=alert(1)>x</details>",
	"<input type=text onfocus=alert(1) autofocus>",
	"<button onclick=alert(1)>x</button>",
	"<a href=\"#\" onclick=\"alert(1)\">x</a>",
	"<table background=javascript:alert(1)><tr><td>x</td></tr></table>",
	"<h1 id=\"x\" onmouseover=alert(1)>t</h1>",
	"<img src=\"x\" srcset=\"javascript:alert(1) 1x\">",
	"<video><source onerror=alert(1)></video>",
	"<a href=\"//evil.example\">x</a>",
	"```\n</code></pre><script>alert(1)</script>\n```",
	"`</code><script>alert(1)</script>`",
	"<!--<script>alert(1)</script>-->",
	"<![CDATA[<script>alert(1)</script>]]>",
	"<noscript><p title=\"</noscript><img src=x onerror=alert(1)>\">",
}

func TestHostileInputNeverProducesActiveContent(t *testing.T) {
	for _, payload := range hostilePayloads {
		out := render(t, payload)
		lower := strings.ToLower(out.HTML)
		for _, bad := range dangerous {
			if strings.Contains(lower, bad) {
				t.Errorf("payload %q leaked %q: %s", payload, bad, out.HTML)
			}
		}
		if eventHandler.MatchString(out.HTML) {
			t.Errorf("payload %q kept an event handler: %s", payload, out.HTML)
		}
		if activeURL.MatchString(out.HTML) {
			t.Errorf("payload %q kept an active URL: %s", payload, out.HTML)
		}
		if strings.Contains(lower, "style=") && !strings.Contains(lower, "text-align") {
			t.Errorf("payload %q kept an inline style: %s", payload, out.HTML)
		}
	}
}

func TestProtocolRelativeLinksNeverReachTheOutput(t *testing.T) {
	out := render(t, "[x](//evil.example/path)\n\n![y](//evil.example/i.png)")
	if strings.Contains(out.HTML, "evil.example") {
		t.Errorf("html = %s", out.HTML)
	}
}

func TestRejectsOversizedDocuments(t *testing.T) {
	_, err := New().Render([]byte(strings.Repeat("a", maxSourceBytes+1)), link)
	if err == nil {
		t.Error("expected error for oversized source")
	}
}

func TestEmptyDocument(t *testing.T) {
	out := render(t, "")
	if strings.TrimSpace(out.HTML) != "" || len(out.Headings) != 0 {
		t.Errorf("out = %+v", out)
	}
}

func TestHasScheme(t *testing.T) {
	yes := []string{"http://x", "https://x", "mailto:a@b", "javascript:alert(1)", "a+b-c.d:rest", "x1:y"}
	no := []string{"", "path/to:file", "./a:b", "1http://x", ":nothing", "a b:c", "docs/a.md", "#frag"}
	for _, s := range yes {
		if !hasScheme(s) {
			t.Errorf("hasScheme(%q) = false", s)
		}
	}
	for _, s := range no {
		if hasScheme(s) {
			t.Errorf("hasScheme(%q) = true", s)
		}
	}
}

func TestResolveRepoPath(t *testing.T) {
	tests := []struct {
		target, doc, want string
		ok                bool
	}{
		{"a.md", "docs/x/y.md", "docs/x/a.md", true},
		{"../a.md", "docs/x/y.md", "docs/a.md", true},
		{"../../a.md", "docs/x/y.md", "a.md", true},
		{"../../../a.md", "docs/x/y.md", "", false},
		{"/a/b.md", "docs/x/y.md", "a/b.md", true},
		{"/../a.md", "docs/x/y.md", "", false},
		{"", "docs/x/y.md", "", false},
		{"%2e%2e/%2e%2e/%2e%2e/a.md", "docs/x/y.md", "", false},
		{"%", "docs/x/y.md", "", false},
		{".", "docs/x/y.md", "docs/x", true},
	}
	for _, tt := range tests {
		got, ok := resolveRepoPath(tt.target, tt.doc)
		if ok != tt.ok || got != tt.want {
			t.Errorf("resolveRepoPath(%q, %q) = %q, %v; want %q, %v", tt.target, tt.doc, got, ok, tt.want, tt.ok)
		}
	}
}

func FuzzRender(f *testing.F) {
	for _, seed := range hostilePayloads {
		f.Add(seed)
	}
	f.Add("# h\n\n[l](../x.md)\n\n![i](a.png)\n")
	renderer := New()
	f.Fuzz(func(t *testing.T, source string) {
		out, err := renderer.Render([]byte(source), link)
		if err != nil {
			return
		}
		lower := strings.ToLower(out.HTML)
		if activeURL.MatchString(out.HTML) {
			t.Fatalf("input %q kept an active URL: %s", source, out.HTML)
		}
		for _, bad := range []string{"<script", "<iframe", "<object", "<embed", "<style"} {
			if strings.Contains(lower, bad) {
				t.Fatalf("input %q leaked %q: %s", source, bad, out.HTML)
			}
		}
		if eventHandler.MatchString(out.HTML) {
			t.Fatalf("input %q kept an event handler: %s", source, out.HTML)
		}
	})
}

func TestDuplicateHeadingsGetUniqueIDs(t *testing.T) {
	out := render(t, "## Setup\n\n## Setup\n\n## Setup\n\n## !!!\n")
	var ids []string
	for _, heading := range out.Headings {
		ids = append(ids, heading.ID)
	}
	if strings.Join(ids, ",") != "h-setup,h-setup-1,h-setup-2,h-section" {
		t.Errorf("ids = %v", ids)
	}
}

func TestSlugify(t *testing.T) {
	tests := map[string]string{
		"Hello World":      "hello-world",
		"  spaced   out  ": "spaced-out",
		"Что нового?":      "что-нового",
		"snake_case stays": "snake_case-stays",
		"a - b":            "a-b",
		"C++ & Go":         "c-go",
		"":                 "",
		"!!!":              "",
	}
	for input, want := range tests {
		if got := slugify(input); got != want {
			t.Errorf("slugify(%q) = %q, want %q", input, got, want)
		}
	}
}
