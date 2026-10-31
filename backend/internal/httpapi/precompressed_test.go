package httpapi

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestStaticSiteServesPrecompressedSiblings(t *testing.T) {
	root := t.TempDir()
	script := []byte(strings.Repeat("console.log(\"synthetic\");\n", 100))
	var gz bytes.Buffer
	writer := gzip.NewWriter(&gz)
	_, _ = writer.Write(script)
	_ = writer.Close()
	// The server never decodes a sibling, so synthetic bytes stand in for
	// Brotli.
	br := []byte("synthetic-brotli-bytes")
	page := []byte(`<!doctype html><meta property="og:url" content="/" /><title>synthetic</title>`)
	built := time.Unix(1_700_000_000, 0)
	files := map[string][]byte{
		"index.html":                  page,
		"index.html.br":               []byte("synthetic-brotli-page"),
		"assets/index-AbCd1234.js":    script,
		"assets/index-AbCd1234.js.br": br,
		"assets/index-AbCd1234.js.gz": gz.Bytes(),
		"assets/stale-AbCd1234.js":    script,
		"assets/stale-AbCd1234.js.br": br,
		"assets/data.json":            []byte(`{"synthetic":true}`),
		"assets/data.json.gz":         []byte("synthetic-gzip-json"),
		"assets/photo.jpg":            []byte("synthetic-jpeg"),
		"assets/photo.jpg.br":         []byte("synthetic-brotli-jpeg"),
		".vite/manifest.json":         []byte(`{"index.html":{"file":"assets/index-AbCd1234.js","css":[]}}`),
	}
	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(full, built, built); err != nil {
			t.Fatal(err)
		}
	}
	// A sibling older than its original is stale.
	stale := filepath.Join(root, "assets", "stale-AbCd1234.js.br")
	if err := os.Chtimes(stale, built.Add(-time.Hour), built.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	// The listener's on-the-fly gzip wraps the site, as in production.
	site := Compress(NewStaticSite(root, ""))
	get := func(method, path string, headers ...string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, nil)
		for i := 0; i+1 < len(headers); i += 2 {
			request.Header.Set(headers[i], headers[i+1])
		}
		recorder := httptest.NewRecorder()
		site.ServeHTTP(recorder, request)
		return recorder
	}
	const immutable = "public, max-age=31536000, immutable"
	for _, tc := range []struct {
		path, accept, encoding string
		body                   []byte
	}{
		{"/assets/index-AbCd1234.js", "gzip, deflate, br, zstd", "br", br},
		{"/assets/index-AbCd1234.js", "*", "br", br},
		{"/assets/index-AbCd1234.js", "br;q=0, gzip", "gzip", gz.Bytes()},
		{"/assets/index-AbCd1234.js", "gzip", "gzip", gz.Bytes()},
		{"/assets/index-AbCd1234.js", "", "", script},
		{"/assets/index-AbCd1234.js", "br;q=0, gzip;q=0", "", script},
		{"/assets/data.json", "gzip, br", "gzip", []byte("synthetic-gzip-json")},
		{"/", "gzip, br", "br", []byte("synthetic-brotli-page")},
	} {
		recorder := get(http.MethodGet, tc.path, "Accept-Encoding", tc.accept)
		headers := recorder.Header()
		if recorder.Code != http.StatusOK || headers.Get("Content-Encoding") != tc.encoding || !bytes.Equal(recorder.Body.Bytes(), tc.body) ||
			headers.Get("Vary") != "Accept-Encoding" || headers.Get("Content-Length") != strconv.Itoa(len(tc.body)) {
			t.Fatalf("%s %q => %d encoding=%q vary=%q length=%q body=%q", tc.path, tc.accept, recorder.Code,
				headers.Get("Content-Encoding"), headers.Get("Vary"), headers.Get("Content-Length"), recorder.Body)
		}
		if tc.path == "/assets/index-AbCd1234.js" && (headers.Get("Content-Type") != "text/javascript; charset=utf-8" || headers.Get("Cache-Control") != immutable) {
			t.Fatalf("%q headers=%v", tc.accept, headers)
		}
	}

	// HEAD, conditional and range requests use the sibling's bytes.
	head := get(http.MethodHead, "/assets/index-AbCd1234.js", "Accept-Encoding", "br")
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Encoding") != "br" || head.Header().Get("Content-Length") != strconv.Itoa(len(br)) {
		t.Fatalf("head=%d headers=%v", head.Code, head.Header())
	}
	lastModified := head.Header().Get("Last-Modified")
	if identity := get(http.MethodHead, "/assets/index-AbCd1234.js"); identity.Header().Get("Last-Modified") != lastModified || lastModified == "" {
		t.Fatalf("Last-Modified differs across encodings: %q %q", lastModified, identity.Header().Get("Last-Modified"))
	}
	fresh := get(http.MethodGet, "/assets/index-AbCd1234.js", "Accept-Encoding", "br", "If-Modified-Since", lastModified)
	if fresh.Code != http.StatusNotModified || fresh.Body.Len() != 0 || fresh.Header().Get("Content-Encoding") != "" {
		t.Fatalf("conditional=%d headers=%v", fresh.Code, fresh.Header())
	}
	partial := get(http.MethodGet, "/assets/index-AbCd1234.js", "Accept-Encoding", "br", "Range", "bytes=0-8")
	if partial.Code != http.StatusPartialContent || partial.Header().Get("Content-Encoding") != "br" || partial.Body.String() != string(br[:9]) {
		t.Fatalf("range=%d headers=%v body=%q", partial.Code, partial.Header(), partial.Body)
	}

	// A stale sibling and an image's sibling are ignored; the text original
	// falls back to on-the-fly gzip.
	if recorder := get(http.MethodGet, "/assets/stale-AbCd1234.js", "Accept-Encoding", "br"); recorder.Header().Get("Content-Encoding") != "" || !bytes.Equal(recorder.Body.Bytes(), script) {
		t.Fatalf("stale sibling served: %v", recorder.Header())
	}
	if recorder := get(http.MethodGet, "/assets/stale-AbCd1234.js", "Accept-Encoding", "gzip, br"); recorder.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("stale fallback=%v", recorder.Header())
	}
	if recorder := get(http.MethodGet, "/assets/photo.jpg", "Accept-Encoding", "br"); recorder.Header().Get("Content-Encoding") != "" || recorder.Body.String() != "synthetic-jpeg" {
		t.Fatalf("image sibling served: %v", recorder.Header())
	}
	// Siblings are not addressable on their own.
	for _, path := range []string{"/assets/index-AbCd1234.js.br", "/assets/index-AbCd1234.js.gz", "/index.html.br"} {
		if recorder := get(http.MethodGet, path); recorder.Code != http.StatusNotFound || bytes.Contains(recorder.Body.Bytes(), br) {
			t.Fatalf("%s => %d", path, recorder.Code)
		}
	}
	// Pages whose share tags are rewritten, and missing pages, never use the
	// stored sibling.
	for _, path := range []string{"/history", "/no-such-page"} {
		recorder := get(http.MethodGet, path, "Accept-Encoding", "br")
		if recorder.Header().Get("Content-Encoding") != "" || !strings.Contains(recorder.Body.String(), "og:url") {
			t.Fatalf("%s => %v %q", path, recorder.Header(), recorder.Body)
		}
	}
	origin := Compress(NewStaticSite(root, "https://kano.example.invalid"))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept-Encoding", "br")
	origin.ServeHTTP(recorder, request)
	if recorder.Header().Get("Content-Encoding") != "" || !strings.Contains(recorder.Body.String(), `content="https://kano.example.invalid/"`) {
		t.Fatalf("rewritten index => %v %q", recorder.Header(), recorder.Body)
	}
}

func TestAcceptsCoding(t *testing.T) {
	for _, tc := range []struct {
		header, coding string
		want           bool
	}{
		{"gzip, deflate, br", "br", true},
		{"gzip, deflate", "br", false},
		{"BR", "br", true},
		{"br;q=0", "br", false},
		{"br;q=0.5", "br", true},
		{"*", "br", true},
		{"*;q=0", "gzip", false},
		{"*;q=0, gzip", "gzip", true},
		{"gzip;q=0, *", "gzip", false},
		{"", "gzip", false},
	} {
		if got := acceptsCoding(tc.header, tc.coding); got != tc.want {
			t.Fatalf("acceptsCoding(%q, %q)=%v", tc.header, tc.coding, got)
		}
	}
}
