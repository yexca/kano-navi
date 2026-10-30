package httpapi

import (
	"bytes"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// maxIndexBytes bounds the index.html read for share-tag rewriting.
const maxIndexBytes = 1 << 20

// pageTags are a page's own link-preview wording in the English base locale,
// matching the frontend's metaTitle and metaDescription messages.
type pageTags struct {
	title       string
	description string
}

// spaPages are the cleaned request paths the frontend renders as pages
// (src/main.tsx), with their share tags, or nil to keep index.html's (the
// board's). The operator console routes by URL fragment, so /admin owns no
// deeper paths.
var spaPages = map[string]*pageTags{
	"":      nil,
	"admin": nil,
	"about": {
		title:       "About · What’s Kano Up To?",
		description: "Project overview, AI development credits, technology stack, and license for What’s Kano Up To?",
	},
	"history": {
		title:       "Journey · What’s Kano Up To?",
		description: "Kano's public milestones from the first 2010 covers to joining Milpro SONA in 2026, with sources.",
	},
	"lineblog": {
		title:       "LINE BLOG archive · What’s Kano Up To?",
		description: "Kano's 2015–2019 LINE BLOG posts, recovered from web archives with their photos and videos.",
	},
}

var (
	metaTag     = regexp.MustCompile(`(?s)<meta\b[^>]*>`)
	metaKey     = regexp.MustCompile(`\b(?:name|property)="([^"]+)"`)
	metaContent = regexp.MustCompile(`\bcontent="[^"]*"`)
	titleTag    = regexp.MustCompile(`(?s)<title>.*?</title>`)
)

// apply gives index.html the page's title, description and root-relative
// og:url, which PUBLIC_ORIGIN then makes absolute like the other share tags.
func (p *pageTags) apply(page []byte, route string) []byte {
	values := map[string]string{
		"description":         p.description,
		"og:title":            p.title,
		"og:description":      p.description,
		"og:url":              "/" + route,
		"twitter:title":       p.title,
		"twitter:description": p.description,
	}
	page = metaTag.ReplaceAllFunc(page, func(tag []byte) []byte {
		key := metaKey.FindSubmatch(tag)
		if key == nil {
			return tag
		}
		value, ok := values[string(key[1])]
		if !ok {
			return tag
		}
		return metaContent.ReplaceAllLiteral(tag, []byte(`content="`+html.EscapeString(value)+`"`))
	})
	return titleTag.ReplaceAllLiteral(page, []byte("<title>"+html.EscapeString(p.title)+"</title>"))
}

var hashedBuildFile = regexp.MustCompile(`^assets/[^/]+-[A-Za-z0-9_-]{8,}\.(?:js|css)$`)

// staticSite serves the built single-page application. Only manifest-listed,
// content-hashed JS/CSS filenames are immutable; curated /assets/history and
// replaceable icons keep revalidation. Other paths receive index.html so a
// reload keeps working: 200 for the frontend's pages, 404 for anything else.
type staticSite struct {
	root       string
	buildFiles map[string]bool
	// publicOrigin, when set, makes index.html's root-relative share-tag
	// URLs (content="/...") absolute, as link previews require.
	publicOrigin string
}

// NewStaticSite returns nil when the directory has no index.html, so an API-only
// process answers unknown paths with its JSON 404 instead. publicOrigin is a
// normalized origin or empty.
func NewStaticSite(directory, publicOrigin string) http.Handler {
	root, err := filepath.Abs(strings.TrimSpace(directory))
	if err != nil || strings.TrimSpace(directory) == "" {
		return nil
	}
	if info, err := os.Stat(filepath.Join(root, "index.html")); err != nil || !info.Mode().IsRegular() {
		return nil
	}
	site := &staticSite{root: root, buildFiles: map[string]bool{}, publicOrigin: publicOrigin}
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
		s.serve(w, r, name, relative, http.StatusOK, "")
		return
	}
	// Other paths still get the application, which shows its not-found view,
	// but with a 404 status so crawlers and link checkers see a missing page.
	status := http.StatusOK
	if _, ok := spaPages[relative]; !ok {
		status = http.StatusNotFound
	}
	s.serve(w, r, filepath.Join(s.root, "index.html"), "index.html", status, relative)
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

// serve writes one file. route names the page index.html is served for, whose
// share tags it then carries.
func (s *staticSite) serve(w http.ResponseWriter, r *http.Request, name, relative string, status int, route string) {
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
	tags := spaPages[route]
	if relative == "index.html" && (s.publicOrigin != "" || status != http.StatusOK || tags != nil) {
		page, err := io.ReadAll(io.LimitReader(file, maxIndexBytes+1))
		if err != nil || len(page) > maxIndexBytes {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "api_not_found"}, "")
			return
		}
		if tags != nil {
			page = tags.apply(page, route)
		}
		if s.publicOrigin != "" {
			// Configuration validates the origin; escaping keeps the
			// attribute intact whatever a caller passes.
			page = bytes.ReplaceAll(page, []byte(`content="/`), []byte(`content="`+html.EscapeString(s.publicOrigin)+`/`))
		}
		if status != http.StatusOK {
			// A missing page has nothing to revalidate; answer it whole.
			w.Header().Set("Content-Length", strconv.Itoa(len(page)))
			w.WriteHeader(status)
			if r.Method != http.MethodHead {
				_, _ = w.Write(page)
			}
			return
		}
		http.ServeContent(w, r, filepath.Base(name), info.ModTime(), bytes.NewReader(page))
		return
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
