package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStaticHTMLRevalidatesRenderedContent(t *testing.T) {
	root := t.TempDir()
	page := `<!doctype html><head><title>board</title><meta property="og:url" content="/" /></head><img src="/assets/kano-avatar.jpg">`
	name := filepath.Join(root, "index.html")
	if err := os.WriteFile(name, []byte(page), 0o600); err != nil {
		t.Fatal(err)
	}
	built := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(name, built, built); err != nil {
		t.Fatal(err)
	}
	get := func(origin, cdn, route, method, tag string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, route, nil)
		// Include a timestamp from a deployment that used file modification
		// times; it must not hide changes in runtime configuration or tags.
		req.Header.Set("If-Modified-Since", built.UTC().Format(http.TimeFormat))
		if tag != "" {
			req.Header.Set("If-None-Match", tag)
		}
		w := httptest.NewRecorder()
		Compress(NewStaticSite(root, origin, cdn)).ServeHTTP(w, req)
		return w
	}
	configs := []struct{ origin, cdn, route string }{
		{"", "", "/"},
		{"", "https://cdn.example.invalid/one", "/"},
		{"", "https://cdn.example.invalid/two", "/"},
		{"https://site.example.invalid", "", "/"},
		{"", "", "/journey"},
	}
	for _, from := range configs {
		first := get(from.origin, from.cdn, from.route, http.MethodGet, "")
		if first.Code != http.StatusOK || first.Header().Get("ETag") == "" || first.Header().Get("Last-Modified") != "" {
			t.Fatalf("HTML needs a content validator: config=%+v status=%d headers=%v", from, first.Code, first.Header())
		}
		for _, to := range configs {
			got := get(to.origin, to.cdn, to.route, http.MethodGet, first.Header().Get("ETag"))
			want := http.StatusOK
			if from == to {
				want = http.StatusNotModified
			}
			if got.Code != want || (want == http.StatusNotModified && got.Body.Len() != 0) || (want == http.StatusOK && got.Body.String() == first.Body.String()) {
				t.Fatalf("from=%+v to=%+v => status=%d want=%d body=%q", from, to, got.Code, want, got.Body)
			}
		}
		head := get(from.origin, from.cdn, from.route, http.MethodHead, "")
		if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("ETag") != first.Header().Get("ETag") {
			t.Fatalf("HEAD config=%+v status=%d headers=%v", from, head.Code, head.Header())
		}
	}
}

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
	site := NewStaticSite(root, "", "")
	if site == nil {
		t.Fatal("static site unavailable")
	}
	if NewStaticSite(t.TempDir(), "", "") != nil || NewStaticSite("", "", "") != nil {
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
		"/journey/":                    {page, "text/html; charset=utf-8", revalidate, "<title>Journey · "},
		"/about//":                     {page, "text/html; charset=utf-8", revalidate, "<title>About · "},
		"/lineblog":                    {page, "text/html; charset=utf-8", revalidate, "<title>LINE BLOG archive · "},
		"/works/":                      {page, "text/html; charset=utf-8", revalidate, "<title>Works · "},
		"/robots.txt":                  {page, "text/plain; charset=utf-8", revalidate, "Disallow: /admin"},
		"/assets/index-AbCd1234.js":    {page, "text/javascript; charset=utf-8", immutable, "console.log(1)"},
		"/assets/unlisted-AbCd1234.js": {page, "text/javascript; charset=utf-8", revalidate, "console.log(2)"},
		"/assets/kano-avatar.jpg":      {page, "image/jpeg", revalidate, "jpg"},
		// Paths outside the frontend's pages still load the application, which
		// shows its not-found view, but answer 404.
		"/lineblog/archives/1":  {missing, "text/html; charset=utf-8", revalidate, "synthetic"},
		"/journey/2024":         {missing, "text/html; charset=utf-8", revalidate, "synthetic"},
		"/works/1":              {missing, "text/html; charset=utf-8", revalidate, "synthetic"},
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
	// The journey's earlier address moves to its own, keeping the query.
	for path, want := range map[string]string{
		"/history":              "/journey",
		"/history/":             "/journey",
		"/history?view=gallery": "/journey?view=gallery",
	} {
		if w := get("GET", path); w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != want {
			t.Fatalf("%s => %d %q", path, w.Code, w.Header().Get("Location"))
		}
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
	site := NewStaticSite(root, "https://kano.example.invalid", "")
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
	NewStaticSite(root, "", "").ServeHTTP(plain, httptest.NewRequest("GET", "/", nil))
	if plain.Body.String() != page {
		t.Fatalf("rewritten without an origin: %s", plain.Body.String())
	}
	// An origin that bypassed configuration still cannot leave the attribute.
	escaped := httptest.NewRecorder()
	NewStaticSite(root, `https://kano.example.invalid"><script>`, "").ServeHTTP(escaped, httptest.NewRequest("GET", "/", nil))
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
		{"", "/journey", []string{
			"<title>Journey · What’s Kano Up To?</title>",
			`property="og:title" content="Journey · What’s Kano Up To?"`,
			`property="og:url" content="/journey"`,
			`content="Kano&#39;s public milestones`,
			`property="og:image" content="/assets/kano-banner.jpg"`,
		}},
		{"https://kano.example.invalid", "/about/", []string{
			"<title>About · What’s Kano Up To?</title>",
			`property="og:url" content="https://kano.example.invalid/about"`,
			`content="Project overview, mobile apps and other sites`,
			`property="og:image" content="https://kano.example.invalid/assets/kano-banner.jpg"`,
		}},
		{"https://kano.example.invalid", "/works", []string{
			"<title>Works · What’s Kano Up To?</title>",
			`property="og:title" content="Works · What’s Kano Up To?"`,
			`property="og:url" content="https://kano.example.invalid/works"`,
			`content="Kano&#39;s discography: her own releases`,
		}},
	} {
		w := get(NewStaticSite(root, tc.origin, ""), tc.path)
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
	for path, status := range map[string]int{"/": 200, "/admin": 200, "/journey/2024": 404} {
		if w := get(NewStaticSite(root, "", ""), path); w.Code != status || w.Body.String() != page {
			t.Fatalf("%s => %d %s", path, w.Code, w.Body.String())
		}
	}
}

func TestStaticSitePutsFixedFilesOnTheCDN(t *testing.T) {
	root := t.TempDir()
	page := `<head>` +
		`<meta property="og:image" content="/assets/kano-banner.jpg">` +
		`<meta property="og:url" content="/">` +
		`<link rel="manifest" href="/manifest.webmanifest">` +
		`<link rel="icon" href="/assets/icons/favicon-32.png">` +
		`</head><body><script type="module" src="/assets/index-AbCd1234.js"></script><a href="/about">about</a></body>`
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	site := NewStaticSite(root, "https://kano.example.invalid", "https://cdn.example.invalid/kano")
	w := httptest.NewRecorder()
	site.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	if w.Code != 200 || w.Header().Get("Cache-Control") != "public, max-age=0, must-revalidate" {
		t.Fatalf("status=%d headers=%v", w.Code, w.Header())
	}
	for _, want := range []string{
		`<meta name="kano-cdn-base" content="https://cdn.example.invalid/kano">`,
		`content="https://cdn.example.invalid/kano/assets/kano-banner.jpg"`,
		`content="https://kano.example.invalid/"`,
		`href="/manifest.webmanifest"`,
		`href="https://cdn.example.invalid/kano/assets/icons/favicon-32.png"`,
		`src="https://cdn.example.invalid/kano/assets/index-AbCd1234.js"`,
		`href="/about"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("page lacks %q:\n%s", want, body)
		}
	}
	// Without a CDN base the page carries no base and keeps the origin paths.
	plain := httptest.NewRecorder()
	NewStaticSite(root, "", "").ServeHTTP(plain, httptest.NewRequest("GET", "/", nil))
	if plain.Body.String() != page {
		t.Fatalf("page rewritten without a CDN:\n%s", plain.Body.String())
	}
}
