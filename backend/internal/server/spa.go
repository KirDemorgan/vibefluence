package server

import (
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
)

const indexFile = "index.html"

var (
	fingerprinted = regexp.MustCompile(`-[A-Z0-9]{8}\.[A-Za-z0-9]+$`)
	assetSuffixes = map[string]struct{}{
		".js": {}, ".mjs": {}, ".css": {}, ".map": {}, ".json": {}, ".txt": {}, ".xml": {},
		".woff": {}, ".woff2": {}, ".ttf": {}, ".otf": {},
		".svg": {}, ".png": {}, ".jpg": {}, ".jpeg": {}, ".gif": {}, ".webp": {}, ".ico": {}, ".avif": {},
	}
)

func spaHandler(assets fs.FS) http.Handler {
	files := http.FileServerFS(assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "not_found", "no such API endpoint")
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" && !hidden(name) && isFile(assets, name) {
			if fingerprinted.MatchString(name) {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		if looksLikeAsset(name) {
			http.NotFound(w, r)
			return
		}
		if !isFile(assets, indexFile) {
			writeError(w, http.StatusNotFound, "not_found", "frontend is not bundled into this build")
			return
		}
		serveIndex(w, r, assets)
	})
}

func hidden(name string) bool {
	for _, segment := range strings.Split(name, "/") {
		if strings.HasPrefix(segment, ".") {
			return true
		}
	}
	return false
}

func looksLikeAsset(name string) bool {
	_, ok := assetSuffixes[strings.ToLower(path.Ext(name))]
	return ok || hidden(name)
}

func isFile(assets fs.FS, name string) bool {
	info, err := fs.Stat(assets, name)
	return err == nil && !info.IsDir()
}

func serveIndex(w http.ResponseWriter, r *http.Request, assets fs.FS) {
	content, err := fs.ReadFile(assets, indexFile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "cannot read index")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(content)
	}
}
