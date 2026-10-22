package docs

import (
	"errors"
	"regexp"
	"strings"
)

var statusLine = regexp.MustCompile(`^status[ \t]*:.*$`)

func WithStatus(content string, status Status) string {
	text := strings.TrimPrefix(content, "\xef\xbb\xbf")
	lines := strings.Split(text, "\n")
	newline := "\n"
	if strings.Contains(text, "\r\n") {
		newline = "\r\n"
	}

	closing := closingMarkerIndex(lines)
	if closing < 0 {
		return "---" + newline + "status: " + string(status) + newline + "---" + newline + newline + text
	}

	replacement := "status: " + string(status)
	if strings.HasSuffix(lines[0], "\r") {
		replacement += "\r"
	}
	for i := 1; i < closing; i++ {
		if statusLine.MatchString(strings.TrimRight(lines[i], "\r")) {
			lines[i] = replacement
			return strings.Join(lines, "\n")
		}
	}
	withStatus := make([]string, 0, len(lines)+1)
	withStatus = append(withStatus, lines[:closing]...)
	withStatus = append(withStatus, replacement)
	withStatus = append(withStatus, lines[closing:]...)
	return strings.Join(withStatus, "\n")
}

func closingMarkerIndex(lines []string) int {
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t\r") != "---" {
		return -1
	}
	for i := 1; i < len(lines); i++ {
		marker := strings.TrimRight(lines[i], " \t\r")
		if marker == "---" || marker == "..." {
			return i
		}
	}
	return -1
}

type NewDocument struct {
	Title  string
	Status Status
	Type   string
	Body   string
}

func Compose(doc NewDocument) string {
	var out strings.Builder
	out.WriteString("---\n")
	out.WriteString("title: " + yamlString(doc.Title) + "\n")
	out.WriteString("status: " + string(doc.Status) + "\n")
	if doc.Type != "" {
		out.WriteString("type: " + yamlString(doc.Type) + "\n")
	}
	out.WriteString("---\n\n")
	out.WriteString(strings.TrimLeft(doc.Body, "\r\n"))
	if !strings.HasSuffix(doc.Body, "\n") {
		out.WriteString("\n")
	}
	return out.String()
}

func yamlString(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	return `"` + replacer.Replace(value) + `"`
}

var ErrBrokenFrontMatter = errors.New("front matter is not valid, fix it before changing the status")

func EnsureStatus(content string, status Status) (string, error) {
	updated := WithStatus(content, status)
	parsed := Parse([]byte(updated), "document.md")
	if parsed.Status != status || len(parsed.Warnings) > 0 {
		return "", ErrBrokenFrontMatter
	}
	return updated, nil
}
