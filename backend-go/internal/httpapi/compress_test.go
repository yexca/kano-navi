package httpapi

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCompressOnlyPublicReadsAndBuildText(t *testing.T) {
	large := strings.Repeat("synthetic ", 400)
	routes := map[string]func(http.ResponseWriter, *http.Request){
		"/api/dashboard": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"value": large}, "no-store")
		},
		"/api/dashboard/revision": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]int{"revision": 1}, "no-store")
		},
		"/api/admin/config": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"value": large}, "")
		},
		"/api/calendar": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"value": large}, "")
		},
		"/mcp": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"value": large}, "")
		},
		"/media/synthetic": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(large))
		},
		"/assets/app.js": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			http.ServeContent(w, r, "app.js", time.Unix(1_700_000_000, 0), strings.NewReader(large))
		},
		"/assets/photo.jpg": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/jpeg")
			http.ServeContent(w, r, "photo.jpg", time.Unix(1_700_000_000, 0), strings.NewReader(large))
		},
		"/cookie": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("Set-Cookie", "synthetic=1")
			_, _ = w.Write([]byte(large))
		},
		"/missing": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(large))
		},
	}
	server := httptest.NewServer(Compress(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routes[r.URL.Path](w, r)
	})))
	defer server.Close()
	// DisableCompression keeps the transport from transparently decoding.
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	fetch := func(method, path string, headers ...string) (*http.Response, []byte) {
		t.Helper()
		request, _ := http.NewRequest(method, server.URL+path, nil)
		for i := 0; i+1 < len(headers); i += 2 {
			request.Header.Set(headers[i], headers[i+1])
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		return response, body
	}
	for path, compressed := range map[string]bool{
		"/api/dashboard": true, "/assets/app.js": true,
		"/api/dashboard/revision": false, "/api/admin/config": false, "/api/calendar": false, "/mcp": false,
		"/media/synthetic": false, "/assets/photo.jpg": false, "/cookie": false, "/missing": false,
	} {
		response, body := fetch("GET", path, "Accept-Encoding", "gzip, br")
		if got := response.Header.Get("Content-Encoding") == "gzip"; got != compressed {
			t.Fatalf("%s compressed=%v, want %v", path, got, compressed)
		}
		if compressed {
			reader, err := gzip.NewReader(bytes.NewReader(body))
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			body, _ = io.ReadAll(reader)
			if !strings.Contains(response.Header.Get("Vary"), "Accept-Encoding") {
				t.Fatalf("%s vary=%q", path, response.Header.Get("Vary"))
			}
		}
		if path != "/api/dashboard/revision" && !strings.Contains(string(body), "synthetic synthetic") {
			t.Fatalf("%s body was not preserved", path)
		}
	}
	if response, _ := fetch("GET", "/api/dashboard"); response.Header.Get("Content-Encoding") != "" {
		t.Fatal("a client that did not ask for gzip received it")
	}
	if response, _ := fetch("GET", "/api/dashboard", "Accept-Encoding", "gzip;q=0"); response.Header.Get("Content-Encoding") != "" {
		t.Fatal("gzip;q=0 must be honoured")
	}
	if response, body := fetch("GET", "/assets/app.js", "Accept-Encoding", "gzip", "Range", "bytes=0-9"); response.StatusCode != http.StatusPartialContent || response.Header.Get("Content-Encoding") != "" || string(body) != large[:10] {
		t.Fatalf("range=%d %q", response.StatusCode, response.Header.Get("Content-Encoding"))
	}
	if response, body := fetch("HEAD", "/assets/app.js", "Accept-Encoding", "gzip"); response.StatusCode != http.StatusOK || len(body) != 0 {
		t.Fatalf("head=%d %d", response.StatusCode, len(body))
	}
}
