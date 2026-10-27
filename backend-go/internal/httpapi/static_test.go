package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticSiteServesBuildWithFallbackAndContainment(t *testing.T) {
	root := t.TempDir()
	write := func(name, content string) {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", "<!doctype html><title>synthetic</title>")
	write("assets/index-AbCd1234.js", "console.log(1)")
	write("assets/unlisted-AbCd1234.js", "console.log(2)")
	write("assets/kano-avatar.jpg", "jpg")
	write("robots.txt", "User-agent: *\nDisallow: /admin\n")
	write(".vite/manifest.json", `{"index.html":{"file":"assets/index-AbCd1234.js","css":[]}}`)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	site := NewStaticSite(root, "")
	if site == nil {
		t.Fatal("static site unavailable")
	}
	if NewStaticSite(t.TempDir(), "") != nil || NewStaticSite("", "") != nil {
		t.Fatal("a directory without index.html must not become a site")
	}
	get := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		site.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}
	const revalidate, immutable = "public, max-age=0, must-revalidate", "public, max-age=31536000, immutable"
	const page, missing = 200, 404
	for path, want := range map[string]struct {
		status      int
		contentType string
		cache       string
		body        string
	}{
		"/":                            {page, "text/html; charset=utf-8", revalidate, "synthetic"},
		"/index.html":                  {page, "text/html; charset=utf-8", revalidate, "synthetic"},
		"/admin":                       {page, "text/html; charset=utf-8", revalidate, "synthetic"},
		"/history/":                    {page, "text/html; charset=utf-8", revalidate, "<title>Journey · "},
		"/about//":                     {page, "text/html; charset=utf-8", revalidate, "<title>About · "},
		"/robots.txt":                  {page, "text/plain; charset=utf-8", revalidate, "Disallow: /admin"},
		"/assets/index-AbCd1234.js":    {page, "text/javascript; charset=utf-8", immutable, "console.log(1)"},
		"/assets/unlisted-AbCd1234.js": {page, "text/javascript; charset=utf-8", revalidate, "console.log(2)"},
		"/assets/kano-avatar.jpg":      {page, "image/jpeg", revalidate, "jpg"},
		// Paths outside the frontend's pages still load the application, which
		// shows its not-found view, but answer 404.
		"/history/2024":         {missing, "text/html; charset=utf-8", revalidate, "synthetic"},
		"/admin/runs":           {missing, "text/html; charset=utf-8", revalidate, "synthetic"},
		"/About":                {missing, "text/html; charset=utf-8", revalidate, "synthetic"},
		"/.vite/manifest.json":  {missing, "text/html; charset=utf-8", revalidate, "synthetic"},
		"/../outside.txt":       {missing, "text/html; charset=utf-8", revalidate, "synthetic"},
		"/assets/":              {missing, "text/html; charset=utf-8", revalidate, "synthetic"},
		"/assets/missing-1.png": {missing, "text/html; charset=utf-8", revalidate, "synthetic"},
	} {
		w := get("GET", path)
		if w.Code != want.status || w.Header().Get("Content-Type") != want.contentType || w.Header().Get("Cache-Control") != want.cache || !strings.Contains(w.Body.String(), want.body) {
			t.Fatalf("%s => %d %q %q %q", path, w.Code, w.Header().Get("Content-Type"), w.Header().Get("Cache-Control"), w.Body.String())
		}
	}
	if w := get("HEAD", "/missing"); w.Code != 404 || w.Body.Len() != 0 || w.Header().Get("Content-Length") == "" {
		t.Fatalf("head missing=%d %d %q", w.Code, w.Body.Len(), w.Header().Get("Content-Length"))
	}
	if w := get("GET", "/assets/index-AbCd1234.js"); w.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("vary=%q", w.Header().Get("Vary"))
	}
	if w := get("POST", "/"); w.Code != 404 {
		t.Fatalf("post=%d", w.Code)
	}
	if w := get("HEAD", "/"); w.Code != 200 || w.Body.Len() != 0 {
		t.Fatalf("head=%d %d", w.Code, w.Body.Len())
	}
}

func TestStaticSiteMakesShareTagsAbsoluteWithPublicOrigin(t *testing.T) {
	root := t.TempDir()
	page := `<meta property="og:image" content="/assets/kano-banner.jpg" /><meta property="og:url" content="/" /><a href="/about">about</a>`
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "other.html"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	site := NewStaticSite(root, "https://kano.example.invalid")
	for _, path := range []string{"/", "/admin"} {
		w := httptest.NewRecorder()
		site.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		body := w.Body.String()
		if w.Code != 200 || !strings.Contains(body, `content="https://kano.example.invalid/assets/kano-banner.jpg"`) ||
			!strings.Contains(body, `content="https://kano.example.invalid/"`) || !strings.Contains(body, `href="/about"`) {
			t.Fatalf("%s => %d %s", path, w.Code, body)
		}
	}
	// A missing page is rewritten too, and keeps its 404.
	missing := httptest.NewRecorder()
	site.ServeHTTP(missing, httptest.NewRequest("GET", "/no-such-page", nil))
	if missing.Code != 404 || !strings.Contains(missing.Body.String(), `content="https://kano.example.invalid/"`) {
		t.Fatalf("missing page => %d %s", missing.Code, missing.Body.String())
	}
	// Only index.html is rewritten; other files are served as stored.
	w := httptest.NewRecorder()
	site.ServeHTTP(w, httptest.NewRequest("GET", "/other.html", nil))
	if w.Body.String() != page {
		t.Fatalf("other file rewritten: %s", w.Body.String())
	}
	plain := httptest.NewRecorder()
	NewStaticSite(root, "").ServeHTTP(plain, httptest.NewRequest("GET", "/", nil))
	if plain.Body.String() != page {
		t.Fatalf("rewritten without an origin: %s", plain.Body.String())
	}
	// An origin that bypassed configuration still cannot leave the attribute.
	escaped := httptest.NewRecorder()
	NewStaticSite(root, `https://kano.example.invalid"><script>`).ServeHTTP(escaped, httptest.NewRequest("GET", "/", nil))
	if body := escaped.Body.String(); strings.Contains(body, "<script>") || !strings.Contains(body, `.invalid&#34;&gt;&lt;script&gt;/assets/`) {
		t.Fatalf("origin not escaped: %s", body)
	}
}

func TestStaticSiteGivesPagesTheirOwnShareTags(t *testing.T) {
	root := t.TempDir()
	// Tags laid out the way the formatter writes index.html.
	page := `<head>
    <meta
      name="description"
      content="Board description"
    />
    <meta property="og:title" content="Board" />
    <meta
      property="og:description"
      content="Board description"
    />
    <meta property="og:url" content="/" />
    <meta property="og:image" content="/assets/kano-banner.jpg" />
    <title>Board</title>
  </head>`
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	get := func(site http.Handler, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		site.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	for _, tc := range []struct {
		origin, path string
		want         []string
	}{
		{"", "/history", []string{
			"<title>Journey · What’s Kano Up To?</title>",
			`property="og:title" content="Journey · What’s Kano Up To?"`,
			`property="og:url" content="/history"`,
			`content="Kano&#39;s public milestones`,
			`property="og:image" content="/assets/kano-banner.jpg"`,
		}},
		{"https://kano.example.invalid", "/about/", []string{
			"<title>About · What’s Kano Up To?</title>",
			`property="og:url" content="https://kano.example.invalid/about"`,
			`content="Project overview, AI development credits`,
			`property="og:image" content="https://kano.example.invalid/assets/kano-banner.jpg"`,
		}},
	} {
		w := get(NewStaticSite(root, tc.origin), tc.path)
		body := w.Body.String()
		if w.Code != 200 || strings.Contains(body, "Board") {
			t.Fatalf("%s => %d %s", tc.path, w.Code, body)
		}
		for _, want := range tc.want {
			if !strings.Contains(body, want) {
				t.Fatalf("%s lacks %q:\n%s", tc.path, want, body)
			}
		}
	}
	// The board and missing pages keep index.html's own tags.
	for path, status := range map[string]int{"/": 200, "/admin": 200, "/history/2024": 404} {
		if w := get(NewStaticSite(root, ""), path); w.Code != status || w.Body.String() != page {
			t.Fatalf("%s => %d %s", path, w.Code, w.Body.String())
		}
	}
}
