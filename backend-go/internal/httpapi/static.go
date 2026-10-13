package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var hashedBuildFile = regexp.MustCompile(`^assets/[^/]+-[A-Za-z0-9_-]{8,}\.(?:js|css)$`)

// staticSite serves the built single-page application. Only manifest-listed,
// content-hashed JS/CSS filenames are immutable; curated /assets/history and
// replaceable icons keep revalidation. Unknown paths receive index.html so
// client-side routes survive a reload.
type staticSite struct {
	root       string
	buildFiles map[string]bool
}

// NewStaticSite returns nil when the directory has no index.html, so an API-only
// process answers unknown paths with its JSON 404 instead.
func NewStaticSite(directory string) http.Handler {
	root, err := filepath.Abs(strings.TrimSpace(directory))
	if err != nil || strings.TrimSpace(directory) == "" {
		return nil
	}
	if info, err := os.Stat(filepath.Join(root, "index.html")); err != nil || !info.Mode().IsRegular() {
		return nil
	}
	site := &staticSite{root: root, buildFiles: map[string]bool{}}
	if raw, err := os.ReadFile(filepath.Join(root, ".vite", "manifest.json")); err == nil {
		var manifest map[string]struct {
			File string   `json:"file"`
			CSS  []string `json:"css"`
		}
		// Older builds without a manifest remain safe to revalidate.
		if json.Unmarshal(raw, &manifest) == nil {
			for _, entry := range manifest {
				if entry.File != "" {
					site.buildFiles[entry.File] = true
				}
				for _, css := range entry.CSS {
					site.buildFiles[css] = true
				}
			}
		}
	}
	return site
}

func (s *staticSite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "api_not_found"}, "")
		return
	}
	relative := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name, ok := s.file(relative); ok {
		s.serve(w, r, name, relative)
		return
	}
	s.serve(w, r, filepath.Join(s.root, "index.html"), "index.html")
}

// file resolves a request path to a regular file contained by the build
// directory. Dotfiles (including the manifest) are never served.
func (s *staticSite) file(relative string) (string, bool) {
	if relative == "" {
		return "", false
	}
	for _, segment := range strings.Split(relative, "/") {
		if segment == "" || strings.HasPrefix(segment, ".") {
			return "", false
		}
	}
	name := filepath.Join(s.root, filepath.FromSlash(relative))
	resolved, err := filepath.EvalSymlinks(name)
	if err != nil {
		return "", false
	}
	root, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return "", false
	}
	inside, err := filepath.Rel(root, resolved)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) || filepath.IsAbs(inside) {
		return "", false
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return resolved, true
}

func (s *staticSite) serve(w http.ResponseWriter, r *http.Request, name, relative string) {
	file, err := os.Open(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "api_not_found"}, "")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "api_not_found"}, "")
		return
	}
	cache := "public, max-age=0, must-revalidate"
	if s.buildFiles[relative] && hashedBuildFile.MatchString(relative) {
		cache = "public, max-age=31536000, immutable"
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".html", ".js", ".css":
		w.Header().Add("Vary", "Accept-Encoding")
	}
	w.Header().Set("Cache-Control", cache)
	if contentType := staticContentType(name); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, filepath.Base(name), info.ModTime(), file)
}

// staticContentType pins the types a build emits so responses do not depend
// on the host's MIME registry (absent in the scratch image, wrong on Windows).
func staticContentType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json", ".map":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".avif":
		return "image/avif"
	case ".ico":
		return "image/x-icon"
	case ".woff2":
		return "font/woff2"
	case ".woff":
		return "font/woff"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".webmanifest":
		return "application/manifest+json"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	}
	return ""
}
