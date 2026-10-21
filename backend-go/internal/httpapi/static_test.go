package httpapi

import (
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
	write(".vite/manifest.json", `{"index.html":{"file":"assets/index-AbCd1234.js","css":[]}}`)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	site := NewStaticSite(root)
	if site == nil {
		t.Fatal("static site unavailable")
	}
	if NewStaticSite(t.TempDir()) != nil || NewStaticSite("") != nil {
		t.Fatal("a directory without index.html must not become a site")
	}
	get := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		site.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}
	const revalidate, immutable = "public, max-age=0, must-revalidate", "public, max-age=31536000, immutable"
	for path, want := range map[string][3]string{
		"/":                            {"text/html; charset=utf-8", revalidate, "synthetic"},
		"/admin":                       {"text/html; charset=utf-8", revalidate, "synthetic"},
		"/history/2024":                {"text/html; charset=utf-8", revalidate, "synthetic"},
		"/assets/index-AbCd1234.js":    {"text/javascript; charset=utf-8", immutable, "console.log(1)"},
		"/assets/unlisted-AbCd1234.js": {"text/javascript; charset=utf-8", revalidate, "console.log(2)"},
		"/assets/kano-avatar.jpg":      {"image/jpeg", revalidate, "jpg"},
		"/.vite/manifest.json":         {"text/html; charset=utf-8", revalidate, "synthetic"},
		"/../outside.txt":              {"text/html; charset=utf-8", revalidate, "synthetic"},
		"/assets/":                     {"text/html; charset=utf-8", revalidate, "synthetic"},
	} {
		w := get("GET", path)
		if w.Code != 200 || w.Header().Get("Content-Type") != want[0] || w.Header().Get("Cache-Control") != want[1] || !strings.Contains(w.Body.String(), want[2]) {
			t.Fatalf("%s => %d %q %q %q", path, w.Code, w.Header().Get("Content-Type"), w.Header().Get("Cache-Control"), w.Body.String())
		}
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
