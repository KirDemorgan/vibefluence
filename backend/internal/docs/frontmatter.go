package docs

import (
	"bytes"
	"fmt"
	"path"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

type Status string

const (
	StatusPublished Status = "published"
	StatusDraft     Status = "draft"
)

const maxFrontMatterBytes = 16 << 10

type Meta struct {
	Title    string
	Status   Status
	Type     string
	Warnings []string
}

type Document struct {
	Meta
	Body string
}

type frontMatter struct {
	Title  string `yaml:"title"`
	Status string `yaml:"status"`
	Type   string `yaml:"type"`
}

var atxHeading = regexp.MustCompile(`^#[ \t]+(.+?)(?:[ \t]+#+)?[ \t]*$`)

func Parse(content []byte, filePath string) Document {
	block, body, state := splitFrontMatter(content)
	doc := Document{Meta: Meta{Status: StatusPublished}, Body: body}

	switch state {
	case frontMatterUnclosed:
		doc.Warnings = append(doc.Warnings, "front matter is not closed")
	case frontMatterTooLarge:
		doc.Warnings = append(doc.Warnings, "front matter is too large")
	case frontMatterPresent:
		doc.applyFrontMatter(block)
	}

	if doc.Title == "" {
		doc.Title = FirstHeading(doc.Body)
	}
	if doc.Title == "" {
		doc.Title = titleFromPath(filePath)
	}
	return doc
}

func (d *Document) applyFrontMatter(block string) {
	var parsed frontMatter
	if err := yaml.Unmarshal([]byte(block), &parsed); err != nil {
		d.Warnings = append(d.Warnings, "front matter is not valid YAML")
		return
	}
	d.Title = strings.TrimSpace(parsed.Title)
	d.Type = strings.TrimSpace(parsed.Type)

	switch strings.ToLower(strings.TrimSpace(parsed.Status)) {
	case "", string(StatusPublished):
		d.Status = StatusPublished
	case string(StatusDraft):
		d.Status = StatusDraft
	default:
		d.Status = StatusDraft
		d.Warnings = append(d.Warnings, fmt.Sprintf("unknown status %q, treated as draft", parsed.Status))
	}
}

type frontMatterState int

const (
	frontMatterAbsent frontMatterState = iota
	frontMatterPresent
	frontMatterUnclosed
	frontMatterTooLarge
)

func splitFrontMatter(content []byte) (block, body string, state frontMatterState) {
	text := string(bytes.TrimPrefix(content, []byte("\xef\xbb\xbf")))
	first, rest, found := cutLine(text)
	if !found || strings.TrimRight(first, " \t\r") != "---" {
		return "", text, frontMatterAbsent
	}

	var collected strings.Builder
	for rest != "" {
		var line string
		line, rest, _ = cutLine(rest)
		marker := strings.TrimRight(line, " \t\r")
		if marker == "---" || marker == "..." {
			return collected.String(), rest, frontMatterPresent
		}
		collected.WriteString(line)
		collected.WriteByte('\n')
		if collected.Len() > maxFrontMatterBytes {
			return "", text, frontMatterTooLarge
		}
	}
	return "", text, frontMatterUnclosed
}

func cutLine(text string) (line, rest string, found bool) {
	index := strings.IndexByte(text, '\n')
	if index < 0 {
		return text, "", text != ""
	}
	return strings.TrimSuffix(text[:index], "\r"), text[index+1:], true
}

func FirstHeading(body string) string {
	var open string
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)

		if open != "" {
			if indent <= 3 && closesFence(trimmed, open) {
				open = ""
			}
			continue
		}
		if indent > 3 {
			continue
		}
		if run := fenceRun(trimmed); run != "" {
			open = run
			continue
		}
		if match := atxHeading.FindStringSubmatch(trimmed); match != nil {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

func fenceRun(line string) string {
	for _, char := range []byte{'`', '~'} {
		length := 0
		for length < len(line) && line[length] == char {
			length++
		}
		if length >= 3 {
			return line[:length]
		}
	}
	return ""
}

func closesFence(line, open string) bool {
	run := fenceRun(line)
	return run != "" && run[0] == open[0] && len(run) >= len(open) && strings.Trim(line, string(open[0])) == ""
}

func titleFromPath(filePath string) string {
	base := path.Base(filePath)
	stem := strings.TrimSuffix(base, path.Ext(base))
	if stem == "" || stem == "." || stem == "/" {
		return "Untitled"
	}
	return stem
}
