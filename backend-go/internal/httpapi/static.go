package httpapi

import (
	"bytes"
	"encoding/json"
	"html"
	"io"
	"io/fs"
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
// content-hashed JS/CSS filenames and /assets/history files asked for by
// their bytes' hash are immutable; other files keep revalidation. Text files
// are sent from their precompressed siblings when the request accepts one.
// Other paths receive index.html so a reload keeps working: 200 for the
// frontend's pages, 404 for anything else.
type staticSite struct {
	root       string
	buildFiles map[string]bool
	// publicOrigin, when set, makes index.html's root-relative share-tag
	// URLs (content="/...") absolute, as link previews require.
	publicOrigin string
	// variants makes the w copies of the fixed history images; without it
	// they are served whole.
	variants variantReader
}

// historyImage matches a fixed history image, the only static files with
// smaller copies.
var historyImage = regexp.MustCompile(`^assets/history/[^/]+\.(?:jpe?g|png|webp)$`)

// historyFile matches a fixed history file, which pages request with v set to
// the SHA-256 of its bytes.
var historyFile = regexp.MustCompile(`^assets/history/[^/]+$`)

// historyVersion returns the request's single v when it has the form of a
// SHA-256, or "".
func historyVersion(r *http.Request) string {
	values := r.URL.Query()["v"]
	if len(values) != 1 || !sha256Hex.MatchString(values[0]) {
		return ""
	}
	return values[0]
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// versioned reports whether hash is the v the request names. A missing or
// malformed v never matches.
func versioned(r *http.Request) func(hash string) bool {
	version := historyVersion(r)
	return func(hash string) bool { return version != "" && hash == version }
}

// precompressedCodings are the build's precompressed siblings of its text
// files, in order of preference.
var precompressedCodings = []struct{ coding, suffix string }{{"br", ".br"}, {"gzip", ".gz"}}

// precompressible reports whether the build may hold precompressed siblings
// of the file name (scripts/vite-precompress.ts).
func precompressible(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".html", ".js", ".css", ".json", ".svg":
		return true
	}
	return false
}

// precompressedSibling reports whether relative is a precompressed sibling,
// which is only served in place of its original.
func precompressedSibling(relative string) bool {
	for _, candidate := range precompressedCodings {
		if stem, ok := strings.CutSuffix(relative, candidate.suffix); ok && precompressible(stem) {
			return true
		}
	}
	return false
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
		if width, present, valid := requestedWidth(r); present && historyImage.MatchString(relative) {
			if !valid {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "media_not_found"}, "")
				return
			}
			if s.serveVariant(w, r, name, width) {
				return
			}
		}
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
// directory. Dotfiles (including the manifest) and precompressed siblings
// are never served by their own path.
func (s *staticSite) file(relative string) (string, bool) {
	if precompressedSibling(relative) {
		return "", false
	}
	return s.contained(relative)
}

func (s *staticSite) contained(relative string) (string, bool) {
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

// serveVariant answers a history image's w request from the variant cache,
// keyed by the SHA-256 of the file's bytes. It reports false when the
// original is to be served instead.
func (s *staticSite) serveVariant(w http.ResponseWriter, r *http.Request, name string, width int) bool {
	info, err := os.Stat(name)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	open := func() (fs.File, error) { return os.Open(name) }
	return serveImageVariant(w, r, s.variants, name, info, open, staticContentType(name), width, versioned(r))
}

// precompressed opens the most preferred precompressed sibling of relative
// that the request accepts and returns it with its length and coding. A
// sibling older than its original is stale and skipped.
func (s *staticSite) precompressed(r *http.Request, relative string, original fs.FileInfo) (*os.File, int64, string) {
	if !precompressible(relative) {
		return nil, 0, ""
	}
	accept := r.Header.Get("Accept-Encoding")
	for _, candidate := range precompressedCodings {
		if !acceptsCoding(accept, candidate.coding) {
			continue
		}
		name, ok := s.contained(relative + candidate.suffix)
		if !ok {
			continue
		}
		file, err := os.Open(name)
		if err != nil {
			continue
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.ModTime().Before(original.ModTime()) {
			_ = file.Close()
			continue
		}
		return file, info.Size(), candidate.coding
	}
	return nil, 0, ""
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
	immutable := s.buildFiles[relative] && hashedBuildFile.MatchString(relative)
	if historyFile.MatchString(relative) && historyVersion(r) != "" {
		// A history file is immutable under the v that is its bytes' hash.
		hash, err := fixedFileHash(name, info, file)
		if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "api_not_found"}, "")
			return
		}
		immutable = err == nil && versioned(r)(hash)
	}
	if precompressible(relative) {
		w.Header().Add("Vary", "Accept-Encoding")
	}
	w.Header().Set("Cache-Control", mediaCacheControl(immutable))
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
	if sibling, length, coding := s.precompressed(r, relative, info); sibling != nil {
		defer sibling.Close()
		w.Header().Set("Content-Encoding", coding)
		// The original's modification time keeps Last-Modified, and so the
		// conditional requests, the same for every encoding; ranges address
		// the encoded bytes.
		http.ServeContent(lengthWriter{ResponseWriter: w, length: length}, r, filepath.Base(name), info.ModTime(), sibling)
		return
	}
	http.ServeContent(w, r, filepath.Base(name), info.ModTime(), file)
}

// lengthWriter declares a precompressed sibling's length on a full response,
// which ServeContent leaves out once Content-Encoding is set.
type lengthWriter struct {
	http.ResponseWriter
	length int64
}

func (l lengthWriter) WriteHeader(status int) {
	if status == http.StatusOK && l.Header().Get("Content-Length") == "" {
		l.Header().Set("Content-Length", strconv.FormatInt(l.length, 10))
	}
	l.ResponseWriter.WriteHeader(status)
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
