package docs

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxPathBytes = 240

func IsMarkdown(filePath string) bool {
	switch strings.ToLower(path.Ext(filePath)) {
	case ".md", ".markdown":
		return true
	}
	return false
}

func IsImage(filePath string) bool {
	_, ok := ImageContentTypes[strings.ToLower(path.Ext(filePath))]
	return ok
}

var ImageContentTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".avif": "image/avif",
}

func ValidateDocPath(filePath string, roots []string) error {
	if filePath == "" {
		return errors.New("path is empty")
	}
	if len(filePath) > maxPathBytes || !utf8.ValidString(filePath) {
		return errors.New("path is too long or not valid UTF-8")
	}
	if strings.ContainsRune(filePath, '\\') || strings.HasPrefix(filePath, "/") || strings.HasSuffix(filePath, "/") {
		return errors.New("path must be relative and use forward slashes")
	}
	for _, r := range filePath {
		if unicode.IsControl(r) {
			return errors.New("path contains control characters")
		}
	}
	for _, segment := range strings.Split(filePath, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return errors.New("path contains empty or relative segments")
		}
		if strings.HasPrefix(segment, ".") {
			return errors.New("path segments must not start with a dot")
		}
		if strings.TrimSpace(segment) != segment {
			return errors.New("path segments must not start or end with spaces")
		}
	}
	if !IsMarkdown(filePath) {
		return errors.New("path must end with .md or .markdown")
	}
	if !UnderAnyRoot(filePath, roots) {
		return fmt.Errorf("path must be inside one of the documentation folders: %s", strings.Join(roots, ", "))
	}
	return nil
}

func UnderAnyRoot(filePath string, roots []string) bool {
	for _, root := range roots {
		if strings.HasPrefix(filePath, root+"/") {
			return true
		}
	}
	return false
}

func Slug(title string) string {
	var out strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(title) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			out.WriteRune(r)
			lastDash = false
		case !lastDash:
			out.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(out.String(), "-")
	if len(slug) > 60 {
		slug = strings.Trim(truncateUTF8(slug, 60), "-")
	}
	if slug == "" {
		return "untitled"
	}
	return slug
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}
