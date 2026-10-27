package contentadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/httpx"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// syntheticPNG is a signature plus an IHDR header describing a 2x3 image.
// The variant byte only changes the content hash.
func syntheticPNG(variant byte) []byte {
	return append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR\x00\x00\x00\x02\x00\x00\x00\x03\x08\x06\x00\x00\x00\x00\x00\x00\x00"), variant)
}

func reply(status int, header map[string]string, body []byte) *http.Response {
	h := http.Header{}
	for key, value := range header {
		h.Set(key, value)
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body))}
}

// recorder is a fake transport: it never touches the network and records
// every destination the module tried to reach.
type recorder struct {
	mu      sync.Mutex
	visited []string
	routes  map[string]func(*http.Request) *http.Response
}

func (f *recorder) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.visited = append(f.visited, r.URL.String())
	if route := f.routes[r.URL.String()]; route != nil {
		return route(r), nil
	}
	return reply(http.StatusNotFound, nil, nil), nil
}

func (f *recorder) reached(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, visited := range f.visited {
		if strings.HasPrefix(visited, prefix) {
			return true
		}
	}
	return false
}

type fixture struct {
	t        *testing.T
	store    *sqlite.Store
	module   *Module
	dataRoot string
	network  *recorder
}

func newFixture(t *testing.T, writable bool) *fixture {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.OpenInitialized(filepath.Join(base, "database", "kano.sqlite"), writable, true)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	network := &recorder{routes: map[string]func(*http.Request) *http.Response{}}
	module := New(store, Options{HTTPClient: &http.Client{Transport: network}})
	return &fixture{t: t, store: store, module: module, dataRoot: store.DataRoot(), network: network}
}

func (f *fixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.store.DB().Exec(query, args...); err != nil {
		f.t.Fatalf("exec: %v", err)
	}
}

func (f *fixture) text(query string, args ...any) string {
	f.t.Helper()
	var value *string
	if err := f.store.DB().QueryRow(query, args...).Scan(&value); err != nil {
		f.t.Fatalf("query: %v", err)
	}
	if value == nil {
		return "<null>"
	}
	return *value
}

func (f *fixture) revision() string {
	return f.text(`SELECT COALESCE((SELECT value FROM app_settings WHERE key='dashboard_revision'), '0')`)
}

type result struct {
	status int
	header http.Header
	raw    []byte
	json   map[string]any
}

func (f *fixture) request(method, path, contentType string, body []byte) result {
	f.t.Helper()
	request := httptest.NewRequest(method, "/api/admin"+path, bytes.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	httpx.NewAdminMux(f.module.AdminRoutes()).ServeHTTP(response, request)
	out := result{status: response.Code, header: response.Header(), raw: response.Body.Bytes()}
	if strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
		if err := json.Unmarshal(out.raw, &out.json); err != nil {
			f.t.Fatalf("%s %s returned invalid JSON: %v", method, path, err)
		}
	}
	if strings.Contains(string(out.raw), f.dataRoot) || strings.Contains(string(out.raw), filepath.ToSlash(f.dataRoot)) {
		f.t.Fatalf("%s %s exposed an absolute path", method, path)
	}
	return out
}

func (f *fixture) post(path string, body any) result {
	f.t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		f.t.Fatal(err)
	}
	return f.request(http.MethodPost, path, "application/json", encoded)
}

func (f *fixture) put(path string, body any) result {
	f.t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		f.t.Fatal(err)
	}
	return f.request(http.MethodPut, path, "application/json", encoded)
}

func (r result) expect(t *testing.T, status int, code string) {
	t.Helper()
	if r.status != status {
		t.Fatalf("status = %d, want %d (%s)", r.status, status, r.raw)
	}
	if code != "" && r.json["error"] != code {
		t.Fatalf("error = %v, want %s", r.json["error"], code)
	}
}

func (r result) expectInvalid(t *testing.T, message string) {
	t.Helper()
	r.expect(t, http.StatusBadRequest, "invalid_input")
	if r.json["message"] != message {
		t.Fatalf("message = %v, want %q", r.json["message"], message)
	}
}

func object(t *testing.T, value any) map[string]any {
	t.Helper()
	out, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected an object, got %#v", value)
	}
	return out
}

func list(t *testing.T, value any) []any {
	t.Helper()
	out, ok := value.([]any)
	if !ok {
		t.Fatalf("expected an array, got %#v", value)
	}
	return out
}

func (f *fixture) addCandidate(slot, source, sourceURL string) string {
	f.t.Helper()
	response := f.post("/profile-media", map[string]any{"slot": slot, "source": source, "sourceUrl": sourceURL})
	response.expect(f.t, http.StatusCreated, "")
	return object(f.t, response.json["item"])["id"].(string)
}

func (f *fixture) serveImage(address, contentType string, body []byte) {
	f.network.routes[address] = func(*http.Request) *http.Response {
		return reply(http.StatusOK, map[string]string{"Content-Type": contentType}, body)
	}
}

func TestUnknownRoutesAnswerJSON(t *testing.T) {
	f := newFixture(t, true)
	for _, path := range []string{"/videos", "/posts/llm", "/profile-media/a/b/c", "/schedule-images/a", "/"} {
		f.request(http.MethodGet, path, "", nil).expect(t, http.StatusNotFound, "not_found")
	}
	f.request(http.MethodDelete, "/profile-media", "", nil).expect(t, http.StatusMethodNotAllowed, "method_not_allowed")
	f.request(http.MethodGet, "/profile-media/discover", "", nil).expect(t, http.StatusMethodNotAllowed, "method_not_allowed")
}

func TestReadOnlyStoreRejectsEveryMutation(t *testing.T) {
	f := newFixture(t, false)
	listing := f.request(http.MethodGet, "/profile-media", "", nil)
	listing.expect(t, http.StatusOK, "")
	if !strings.Contains(string(listing.raw), `"items":[]`) || !strings.Contains(string(listing.raw), `"avatar":null`) || !strings.Contains(string(listing.raw), `"banner":null`) {
		t.Fatalf("empty list shape changed: %s", listing.raw)
	}
	assets := f.request(http.MethodGet, "/schedule-images", "", nil)
	if assets.status != http.StatusOK || !strings.Contains(string(assets.raw), `"items":[]`) {
		t.Fatalf("empty schedule list shape changed: %s", assets.raw)
	}
	for _, path := range []string{
		"/profile-media", "/profile-media/discover", "/profile-media/upload?slot=avatar",
		"/profile-media/abc/download", "/profile-media/abc/select",
		"/schedule-images/abc/review", "/posts/abc/reprocess", "/posts/1900000000000000100/refresh",
	} {
		f.request(http.MethodPost, path, "application/json", []byte(`{}`)).expect(t, http.StatusServiceUnavailable, "read_only")
	}
	f.put("/schedule-images/abc/period", map[string]any{}).expect(t, http.StatusServiceUnavailable, "read_only")
	f.put("/posts/abc/quote/hidden", map[string]any{"hidden": true}).expect(t, http.StatusServiceUnavailable, "read_only")
	if len(f.network.visited) != 0 {
		t.Fatalf("read-only requests reached the network: %v", f.network.visited)
	}
}

func TestProfileCandidateUpsert(t *testing.T) {
	f := newFixture(t, true)
	created := f.post("/profile-media", map[string]any{"slot": " Avatar ", "source": "x", "sourceUrl": "https://PBS.twimg.com/profile_images/1/a.png#fragment", "sourceRef": "manual"})
	created.expect(t, http.StatusCreated, "")
	item := object(t, created.json["item"])
	normalized := "https://pbs.twimg.com/profile_images/1/a.png"
	if item["id"] != profileCandidateID("avatar", normalized) || item["sourceUrl"] != normalized || item["slot"] != "avatar" {
		t.Fatalf("unexpected candidate: %#v", item)
	}
	if item["status"] != "pending" || item["isActive"] != false || item["sourceRef"] != "manual" || item["publicUrl"] != nil || item["cachePath"] != nil {
		t.Fatalf("unexpected candidate state: %#v", item)
	}
	if _, present := item["previewUrl"]; present {
		t.Fatalf("the changed item must not carry previewUrl")
	}
	listed := object(t, list(t, created.json["items"])[0])
	if preview, present := listed["previewUrl"]; !present || preview != nil {
		t.Fatalf("a pending list item needs previewUrl: null, got %#v", listed)
	}
	active := object(t, created.json["active"])
	if active["avatar"] != nil || active["banner"] != nil {
		t.Fatalf("nothing is active yet: %#v", active)
	}

	// Seeing the same URL again updates the row instead of duplicating it.
	again := f.post("/profile-media", map[string]any{"slot": "avatar", "source": "x", "sourceUrl": normalized})
	again.expect(t, http.StatusCreated, "")
	if len(list(t, again.json["items"])) != 1 || object(t, again.json["item"])["sourceRef"] != "manual" {
		t.Fatalf("upsert duplicated or cleared the candidate: %s", again.raw)
	}

	f.post("/profile-media", map[string]any{"source": "x"}).expectInvalid(t, "slot is required")
	f.post("/profile-media", map[string]any{"slot": "avatar", "source": "upload", "sourceUrl": normalized}).expectInvalid(t, "slot or source is invalid")
	f.post("/profile-media", map[string]any{"slot": "cover", "source": "x", "sourceUrl": normalized}).expectInvalid(t, "slot or source is invalid")
	f.post("/profile-media", map[string]any{"slot": "avatar", "source": "x"}).expectInvalid(t, "sourceUrl is required")
	f.post("/profile-media", map[string]any{"slot": "avatar", "source": "x", "sourceUrl": "ftp://media.example.test/a.png"}).expectInvalid(t, "url must use HTTP or HTTPS")
	f.post("/profile-media", map[string]any{"slot": "avatar", "source": "x", "sourceUrl": "not a url"}).expectInvalid(t, "url is invalid")
	f.post("/profile-media", map[string]any{"slot": "avatar", "source": "x", "sourceUrl": "https://pbs.twimg.com/" + strings.Repeat("a", 2000)}).expectInvalid(t, "url is too long")
	f.request(http.MethodPost, "/profile-media", "application/json", []byte(`{"slot":`)).expect(t, http.StatusBadRequest, "invalid_json")
}

func TestDownloadStoresVerifiedImageAndPreviewsIt(t *testing.T) {
	f := newFixture(t, true)
	start := "https://pbs.twimg.com/profile_images/1/a.png"
	final := "https://abs.twimg.com/moved/a.png"
	image := syntheticPNG(1)
	f.network.routes[start] = func(r *http.Request) *http.Response {
		if r.Header.Get("User-Agent") != profileUserAgent {
			t.Errorf("unexpected user agent %q", r.Header.Get("User-Agent"))
		}
		return reply(http.StatusFound, map[string]string{"Location": "//abs.twimg.com/moved/a.png"}, nil)
	}
	f.serveImage(final, "image/png; charset=binary", image)
	id := f.addCandidate("avatar", "x", start)

	downloaded := f.request(http.MethodPost, "/profile-media/"+id+"/download", "", nil)
	downloaded.expect(t, http.StatusOK, "")
	item := object(t, downloaded.json["item"])
	hash := sha256Hex(image)
	wantPath := "x/sha256/" + hash[:2] + "/" + hash + ".png"
	if item["status"] != "ready" || item["cachePath"] != wantPath || item["sha256"] != hash || item["mimeType"] != "image/png" || item["extension"] != "png" {
		t.Fatalf("unexpected ready candidate: %#v", item)
	}
	if item["byteSize"] != float64(len(image)) || item["width"] != float64(2) || item["height"] != float64(3) || item["downloadStatus"] != "success" || item["lastError"] != nil {
		t.Fatalf("unexpected image metadata: %#v", item)
	}
	stored, err := os.ReadFile(filepath.Join(f.dataRoot, filepath.FromSlash(wantPath)))
	if err != nil || !bytes.Equal(stored, image) {
		t.Fatalf("cached bytes differ: %v", err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(f.dataRoot, "x", "sha256", hash[:2], ".part-*"))
	if len(leftovers) != 0 {
		t.Fatalf("temporary files remain: %v", leftovers)
	}
	previewURL := object(t, list(t, downloaded.json["items"])[0])["previewUrl"]
	if previewURL != "/api/admin/profile-media/"+id+"/preview" {
		t.Fatalf("unexpected preview URL %v", previewURL)
	}

	preview := f.request(http.MethodGet, "/profile-media/"+id+"/preview", "", nil)
	if preview.status != http.StatusOK || !bytes.Equal(preview.raw, image) {
		t.Fatalf("preview status %d", preview.status)
	}
	if preview.header.Get("Content-Type") != "image/png" || preview.header.Get("X-Content-Type-Options") != "nosniff" ||
		preview.header.Get("Content-Security-Policy") != "default-src 'none'; sandbox" || preview.header.Get("Cache-Control") != "no-store, no-transform" {
		t.Fatalf("unexpected preview headers: %v", preview.header)
	}

	// A later failure records its outcome but keeps the ready snapshot.
	f.network.routes[start] = func(*http.Request) *http.Response { return reply(http.StatusServiceUnavailable, nil, nil) }
	f.request(http.MethodPost, "/profile-media/"+id+"/download", "", nil).expect(t, http.StatusInternalServerError, "internal_error")
	if got := f.text(`SELECT status || '/' || download_status || '/' || last_error || '/' || sha256 FROM profile_media WHERE id=?`, id); got != "ready/failed/profile image returned HTTP 503/"+hash {
		t.Fatalf("ready snapshot was not retained: %s", got)
	}
}

func TestDownloadFailuresAreRecordedWithFixedMessages(t *testing.T) {
	f := newFixture(t, true)
	image := syntheticPNG(2)
	cases := []struct {
		name, address, want string
		route               func(*http.Request) *http.Response
	}{
		{"redirect leaves the allowlist", "https://pbs.twimg.com/case/redirect.png", "profile image host is not allowed", func(*http.Request) *http.Response {
			return reply(http.StatusMovedPermanently, map[string]string{"Location": "https://media.example.test/a.png"}, nil)
		}},
		{"redirect downgrades to plain HTTP", "https://pbs.twimg.com/case/downgrade.png", "profile image host is not allowed", func(*http.Request) *http.Response {
			return reply(http.StatusFound, map[string]string{"Location": "http://pbs.twimg.com/a.png"}, nil)
		}},
		{"redirect loop", "https://pbs.twimg.com/case/loop.png", "profile image redirect limit exceeded", func(*http.Request) *http.Response {
			return reply(http.StatusFound, map[string]string{"Location": "https://pbs.twimg.com/case/loop.png"}, nil)
		}},
		{"declared type disagrees", "https://pbs.twimg.com/case/mismatch.png", "profile response is not a supported image", func(*http.Request) *http.Response {
			return reply(http.StatusOK, map[string]string{"Content-Type": "image/jpeg"}, image)
		}},
		{"not an image", "https://i.ytimg.com/case/page.png", "profile response is not a supported image", func(*http.Request) *http.Response {
			return reply(http.StatusOK, map[string]string{"Content-Type": "text/html"}, []byte("<html></html>"))
		}},
		{"declared length over the cap", "https://pbs.twimg.com/case/large.png", "response exceeds the size limit", func(*http.Request) *http.Response {
			response := reply(http.StatusOK, map[string]string{"Content-Type": "image/png"}, image)
			response.ContentLength = maxProfileImageBytes + 1
			return response
		}},
		{"body over the cap", "https://pbs.twimg.com/case/stream.png", "response exceeds the size limit", func(*http.Request) *http.Response {
			response := reply(http.StatusOK, map[string]string{"Content-Type": "image/png"}, append(syntheticPNG(3), make([]byte, maxProfileImageBytes)...))
			response.ContentLength = -1
			return response
		}},
		{"missing upstream", "https://pbs.twimg.com/case/missing.png", "profile image returned HTTP 404", nil},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if test.route != nil {
				f.network.routes[test.address] = test.route
			}
			id := f.addCandidate("banner", "x", test.address)
			f.request(http.MethodPost, "/profile-media/"+id+"/download", "", nil).expect(t, http.StatusInternalServerError, "internal_error")
			if got := f.text(`SELECT status || '/' || download_status || '/' || last_error FROM profile_media WHERE id=?`, id); got != "failed/failed/"+test.want {
				t.Fatalf("recorded outcome = %s", got)
			}
			if f.text(`SELECT cache_path FROM profile_media WHERE id=?`, id) != "<null>" {
				t.Fatal("a failed download must not publish a cache path")
			}
		})
	}
	if f.network.reached("https://media.example.test") || f.network.reached("http://") {
		t.Fatalf("a rejected redirect was followed: %v", f.network.visited)
	}

	// A registered candidate on an unlisted host is refused before any request.
	before := len(f.network.visited)
	id := f.addCandidate("avatar", "x", "https://media.example.test/avatar.png")
	f.request(http.MethodPost, "/profile-media/"+id+"/download", "", nil).expect(t, http.StatusInternalServerError, "internal_error")
	if len(f.network.visited) != before || f.text(`SELECT last_error FROM profile_media WHERE id=?`, id) != "profile image host is not allowed" {
		t.Fatalf("unlisted host handling changed: %v", f.network.visited[before:])
	}
	f.request(http.MethodPost, "/profile-media/unknown/download", "", nil).expect(t, http.StatusInternalServerError, "internal_error")
}

func TestOutboundAllowlists(t *testing.T) {
	// Host policy is checked on bare names: exact entries, real subdomains of
	// the CDN suffixes, and nothing that merely looks similar.
	for host, want := range map[string]bool{
		"pbs.twimg.com": true, "video.twimg.com": true, "i9.ytimg.com": true, "yt3.ggpht.com": true, "YT3.GGPHT.COM": true,
		"twimg.com": false, "ytimg.com": false, "eviltwimg.com": false, "ggpht.com": false, "evil.ggpht.com": false,
		"pbs.twimg.com.example.test": false, "x.com": false, "localhost": false, "": false,
	} {
		if got := hostAllowed(host, profileImageHosts, profileImageHostSuffixes); got != want {
			t.Errorf("image host %q allowed = %v, want %v", host, got, want)
		}
	}
	allowedImages := []string{
		"https://pbs.twimg.com/a.jpg", "https://video.twimg.com/a.jpg", "https://i9.ytimg.com/vi/a.jpg",
		"https://PBS.TWIMG.COM:443/a.jpg?format=jpg&name=large",
	}
	for _, address := range allowedImages {
		if _, ok := profileImageURL(address); !ok {
			t.Errorf("%s should be allowed", address)
		}
	}
	// Built from parts: a synthetic user name, never a real credential.
	withUserInfo := (&url.URL{Scheme: "https", User: url.User("synthetic"), Host: "pbs.twimg.com", Path: "/a.jpg"}).String()
	rejectedImages := []string{
		"http://pbs.twimg.com/a.jpg", "https://twimg.com/a.jpg", "https://pbs.twimg.com.example.test/a.jpg",
		"https://pbs.twimg.com:8443/a.jpg", withUserInfo, "https://127.0.0.1/a.png",
		"https://pbs.twimg.com/a.jpg?access_token=synthetic", "file:///etc/hosts", "//pbs.twimg.com/a.jpg", "",
	}
	for _, address := range rejectedImages {
		if _, ok := profileImageURL(address); ok {
			t.Errorf("%s should be rejected", address)
		}
	}
	for address, want := range map[string]bool{
		"https://x.com/synthetic": true, "https://www.youtube.com/channel/synthetic": true,
		"http://x.com/synthetic": false, "https://x.com.example.test/synthetic": false, "https://m.youtube.com/synthetic": false, "https://pbs.twimg.com/a.jpg": false,
	} {
		if _, ok := profilePageURL(address); ok != want {
			t.Errorf("page %s allowed = %v, want %v", address, ok, want)
		}
	}
	for input, want := range map[string]string{
		" HTTPS://Example.TEST:443/a b?q=1#frag ": "https://example.test/a%20b?q=1",
		"http://example.test":                     "http://example.test/",
		"http://example.test:8080/x":              "http://example.test:8080/x",
		"mailto:someone@example.test":             "",
		"/assets/a.png":                           "",
	} {
		if got, _ := normalizeSourceURL(input); got != want {
			t.Errorf("normalizeSourceURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestUploadSelectAndPublicRead(t *testing.T) {
	f := newFixture(t, true)
	first, second := syntheticPNG(10), syntheticPNG(11)
	uploaded := f.request(http.MethodPost, "/profile-media/upload?slot=avatar", "image/png", first)
	uploaded.expect(t, http.StatusCreated, "")
	item := object(t, uploaded.json["item"])
	firstID, firstHash := item["id"].(string), sha256Hex(first)
	if !strings.HasPrefix(firstID, "upload-") || item["source"] != "upload" || item["sourceRef"] != "admin-upload" || item["sourceUrl"] != nil {
		t.Fatalf("unexpected upload candidate: %#v", item)
	}
	if item["status"] != "ready" || item["cachePath"] != "avatar/sha256/"+firstHash[:2]+"/"+firstHash+".png" || item["isActive"] != false || item["publicUrl"] != nil {
		t.Fatalf("an upload is ready but not active: %#v", item)
	}

	before := f.revision()
	selected := f.request(http.MethodPost, "/profile-media/"+firstID+"/select", "", nil)
	selected.expect(t, http.StatusOK, "")
	item = object(t, selected.json["item"])
	if item["isActive"] != true || item["activeCachePath"] != "avatar/avatar.png" || item["activeSha256"] != firstHash ||
		item["activeMimeType"] != "image/png" || item["publicUrl"] != "/media/profile/avatar?v="+firstHash {
		t.Fatalf("unexpected selected candidate: %#v", item)
	}
	if object(t, object(t, selected.json["active"])["avatar"])["id"] != firstID || object(t, selected.json["active"])["banner"] != nil {
		t.Fatalf("unexpected active slots: %#v", selected.json["active"])
	}
	if f.revision() == before {
		t.Fatal("selecting profile media must bump the dashboard revision")
	}
	// The existing public reader must accept exactly what selection wrote.
	served, err := f.store.ReadProfileMedia(context.Background(), "avatar", firstHash)
	if err != nil || !bytes.Equal(served.Content, first) || served.MIMEType != "image/png" {
		t.Fatalf("public reader rejected the selected file: %v", err)
	}

	// Selecting another candidate atomically replaces the slot file.
	uploaded = f.request(http.MethodPost, "/profile-media/upload?slot=avatar", "application/octet-stream", second)
	uploaded.expect(t, http.StatusCreated, "")
	secondID := object(t, uploaded.json["item"])["id"].(string)
	f.request(http.MethodPost, "/profile-media/"+secondID+"/select", "", nil).expect(t, http.StatusOK, "")
	served, err = f.store.ReadProfileMedia(context.Background(), "avatar", sha256Hex(second))
	if err != nil || !bytes.Equal(served.Content, second) {
		t.Fatalf("public reader rejected the replacement: %v", err)
	}
	if f.text(`SELECT group_concat(id) FROM profile_media WHERE is_active=1`) != secondID {
		t.Fatal("exactly the last selection must be active")
	}
	entries, err := os.ReadDir(filepath.Join(f.dataRoot, "avatar"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "avatar.png" && entry.Name() != "sha256" {
			t.Fatalf("unexpected file in the selected namespace: %s", entry.Name())
		}
	}
}

func TestUploadValidation(t *testing.T) {
	f := newFixture(t, true)
	image := syntheticPNG(20)
	f.request(http.MethodPost, "/profile-media/upload?slot=cover", "image/png", image).expectInvalid(t, "slot is invalid")
	f.request(http.MethodPost, "/profile-media/upload?slot=avatar", "text/plain", image).expectInvalid(t, "image body is required")
	f.request(http.MethodPost, "/profile-media/upload?slot=avatar", "image/png", nil).expectInvalid(t, "image body is required")
	// Bytes must be a supported image and agree with the declared type.
	f.request(http.MethodPost, "/profile-media/upload?slot=avatar", "image/png", []byte("<svg><script>synthetic</script></svg>")).expect(t, http.StatusInternalServerError, "internal_error")
	f.request(http.MethodPost, "/profile-media/upload?slot=avatar", "image/jpeg", image).expect(t, http.StatusInternalServerError, "internal_error")
	large := httptest.NewRequest(http.MethodPost, "/api/admin/profile-media/upload?slot=banner", bytes.NewReader(make([]byte, maxProfileImageBytes+1)))
	large.Header.Set("Content-Type", "image/png")
	response := httptest.NewRecorder()
	httpx.NewAdminMux(f.module.AdminRoutes()).ServeHTTP(response, large)
	if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), "request_too_large") {
		t.Fatalf("oversized upload answered %d %s", response.Code, response.Body.String())
	}
	if f.text(`SELECT CAST(COUNT(*) AS TEXT) FROM profile_media`) != "0" {
		t.Fatal("rejected uploads must not leave candidates behind")
	}
	if _, err := os.Stat(filepath.Join(f.dataRoot, "avatar")); !os.IsNotExist(err) {
		t.Fatal("rejected uploads must not write files")
	}
}

func TestSelectAndPreviewContainment(t *testing.T) {
	f := newFixture(t, true)
	image := syntheticPNG(30)
	hash := sha256Hex(image)
	outside := filepath.Join(f.dataRoot, "outside.png")
	if err := os.WriteFile(outside, image, 0o600); err != nil {
		t.Fatal(err)
	}
	insert := func(id, status, cachePath string) {
		t.Helper()
		f.exec(`INSERT INTO profile_media(id,slot,source,cache_path,mime_type,extension,sha256,status,is_active,created_at,updated_at)
		        VALUES(?,?,?,?,?,?,?,?,0,?,?)`, id, "avatar", "upload", cachePath, "image/png", "png", hash, status, "2026-10-01T00:00:00.000Z", "2026-10-01T00:00:00.000Z")
	}
	absolute := filepath.ToSlash(outside)
	insert("traversal", "ready", "avatar/../outside.png")
	insert("parent", "ready", "../outside.png")
	insert("absolute", "ready", absolute)
	insert("database", "ready", "database/kano.sqlite")
	insert("missing", "ready", "avatar/sha256/"+hash[:2]+"/"+hash+".png")
	insert("pending", "pending", "avatar/sha256/"+hash[:2]+"/"+hash+".png")
	revision := f.revision()
	for _, id := range []string{"traversal", "parent", "absolute", "database", "missing"} {
		f.request(http.MethodGet, "/profile-media/"+id+"/preview", "", nil).expect(t, http.StatusNotFound, "profile_media_not_found")
		f.request(http.MethodPost, "/profile-media/"+id+"/select", "", nil).expect(t, http.StatusInternalServerError, "internal_error")
	}
	f.request(http.MethodGet, "/profile-media/pending/preview", "", nil).expect(t, http.StatusNotFound, "profile_media_not_ready")
	f.request(http.MethodGet, "/profile-media/unknown/preview", "", nil).expect(t, http.StatusNotFound, "profile_media_not_ready")
	f.request(http.MethodPost, "/profile-media/pending/select", "", nil).expect(t, http.StatusInternalServerError, "internal_error")
	f.request(http.MethodPost, "/profile-media/unknown/select", "", nil).expect(t, http.StatusInternalServerError, "internal_error")

	// A cached file that no longer matches its metadata is never selected or served as an image.
	directory := filepath.Join(f.dataRoot, "avatar", "sha256", hash[:2])
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, hash+".png"), []byte("<script>synthetic</script>"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.request(http.MethodGet, "/profile-media/missing/preview", "", nil).expect(t, http.StatusNotFound, "profile_media_not_found")
	f.request(http.MethodPost, "/profile-media/missing/select", "", nil).expect(t, http.StatusInternalServerError, "internal_error")
	if err := os.WriteFile(filepath.Join(directory, hash+".png"), syntheticPNG(31), 0o600); err != nil {
		t.Fatal(err)
	}
	f.request(http.MethodPost, "/profile-media/missing/select", "", nil).expect(t, http.StatusInternalServerError, "internal_error")

	// A link inside the namespace that points outside it is not followed.
	link := filepath.Join(directory, "link.png")
	if err := os.Symlink(outside, link); err == nil {
		insert("symlink", "ready", "avatar/sha256/"+hash[:2]+"/link.png")
		f.request(http.MethodGet, "/profile-media/symlink/preview", "", nil).expect(t, http.StatusNotFound, "profile_media_not_found")
		f.request(http.MethodPost, "/profile-media/symlink/select", "", nil).expect(t, http.StatusInternalServerError, "internal_error")
	} else {
		t.Logf("symlink case skipped: links are unavailable here")
	}

	if f.revision() != revision || f.text(`SELECT CAST(COUNT(*) AS TEXT) FROM profile_media WHERE is_active=1`) != "0" {
		t.Fatal("rejected selections must not change the snapshot")
	}
	if _, err := os.Stat(filepath.Join(f.dataRoot, "avatar", "avatar.png")); !os.IsNotExist(err) {
		t.Fatal("rejected selections must not publish a slot file")
	}
}

func TestMediaPathAndDimensionHelpers(t *testing.T) {
	for _, rejected := range []string{"", " ", "/etc/hosts", `C:\data\a.png`, "c:/data/a.png", "x/../a", "x//a", "x/./a", `..\a`, "x/a\x00b"} {
		if _, ok := cleanRelativePath(rejected); ok {
			t.Errorf("%q should be rejected", rejected)
		}
	}
	if normalized, ok := cleanRelativePath(`avatar\sha256\ab\file.png`); !ok || normalized != "avatar/sha256/ab/file.png" {
		t.Errorf("backslashes should normalize, got %q", normalized)
	}
	f := newFixture(t, true)
	if _, err := f.module.lexicalMediaPath("database/kano.sqlite"); err == nil {
		t.Error("only media namespaces may be resolved")
	}
	if err := f.module.writeRootFile("../escape.png", syntheticPNG(1), false); err == nil {
		t.Error("writes outside the data root must fail")
	}
	gif := append([]byte("GIF89a"), 0x05, 0x00, 0x07, 0x00)
	if width, height := imageDimensions(gif, "image/gif"); width == nil || *width != 5 || *height != 7 {
		t.Error("GIF dimensions were not probed")
	}
	jpeg := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x04, 0x00, 0x00, 0xff, 0xc0, 0x00, 0x08, 0x08, 0x00, 0x09, 0x00, 0x04, 0x01, 0x00, 0x00}
	if width, height := imageDimensions(jpeg, "image/jpeg"); width == nil || *width != 4 || *height != 9 {
		t.Error("JPEG dimensions were not probed")
	}
	if width, height := imageDimensions([]byte("RIFF0000WEBP"), "image/webp"); width != nil || height != nil {
		t.Error("unknown formats report no dimensions")
	}
	for source, want := range map[string]string{"x": "x", "youtube": "youtube", "upload": "avatar", "profile-x": "avatar", "": "x"} {
		if got := mediaNamespaceForSource(source); got != want {
			t.Errorf("namespace for %q = %q, want %q", source, got, want)
		}
	}
}

func TestDiscoverRegistersAllowlistedCandidates(t *testing.T) {
	f := newFixture(t, true)
	f.exec(`UPDATE profiles SET x_url=?, avatar_url=?`, "https://x.com/synthetic_handle", "https://pbs.twimg.com/profile_images/9/existing.jpg")
	page := `<html><head>
<meta property="og:image" content="https://pbs.twimg.com/profile_images/1/og.jpg?format=jpg&amp;name=large">
<meta name="twitter:image" content="https://media.example.test/unrelated.png">
<meta content="https://pbs.twimg.com/profile_images/1/reversed.jpg" property="og:image">
</head><script>{"profile_image_url_https":"https:\/\/pbs.twimg.com\/profile_images\/1\/keyed_400x400.jpg","profile_banner_url":"https:\/\/pbs.twimg.com\/profile_banners\/1\/banner"}</script></html>`
	f.network.routes["https://x.com/synthetic_handle"] = func(r *http.Request) *http.Response {
		if !strings.Contains(r.Header.Get("Accept"), "text/html") {
			t.Errorf("unexpected accept header %q", r.Header.Get("Accept"))
		}
		return reply(http.StatusFound, map[string]string{"Location": "https://www.x.com/synthetic_handle"}, nil)
	}
	f.network.routes["https://www.x.com/synthetic_handle"] = func(*http.Request) *http.Response {
		return reply(http.StatusOK, map[string]string{"Content-Type": "text/html"}, []byte(page))
	}

	discovered := f.post("/profile-media/discover", map[string]any{"slot": "avatar", "source": "x"})
	discovered.expect(t, http.StatusOK, "")
	if discovered.json["slot"] != "avatar" || discovered.json["source"] != "x" || discovered.json["sourceUrl"] != "https://www.x.com/synthetic_handle" {
		t.Fatalf("unexpected discovery summary: %s", discovered.raw)
	}
	found := map[string]bool{}
	for _, entry := range list(t, discovered.json["candidates"]) {
		candidate := object(t, entry)
		if candidate["status"] != "pending" || candidate["source"] != "x" || candidate["sourceRef"] != "https://www.x.com/synthetic_handle" {
			t.Fatalf("unexpected candidate: %#v", candidate)
		}
		found[candidate["sourceUrl"].(string)] = true
	}
	want := []string{
		"https://pbs.twimg.com/profile_images/1/keyed_400x400.jpg",
		"https://pbs.twimg.com/profile_images/1/og.jpg?format=jpg&name=large",
		"https://pbs.twimg.com/profile_images/1/reversed.jpg",
		"https://pbs.twimg.com/profile_images/9/existing.jpg",
	}
	if len(found) != len(want) {
		t.Fatalf("candidates = %v", found)
	}
	for _, address := range want {
		if !found[address] {
			t.Fatalf("missing candidate %s in %v", address, found)
		}
	}
	if len(list(t, discovered.json["items"])) != len(want) || discovered.json["active"] == nil {
		t.Fatalf("discovery must include the refreshed list: %s", discovered.raw)
	}
	// Discovery registers URLs only; it never downloads image bytes.
	if f.network.reached("https://pbs.twimg.com") || f.network.reached("https://media.example.test") {
		t.Fatalf("discovery fetched images: %v", f.network.visited)
	}

	f.post("/profile-media/discover", map[string]any{"slot": "avatar", "source": "upload"}).expectInvalid(t, "slot or source is invalid")
	f.post("/profile-media/discover", map[string]any{"slot": "avatar"}).expectInvalid(t, "source is required")
}

func TestDiscoverRejectsUnlistedPagesAndRedirects(t *testing.T) {
	f := newFixture(t, true)
	f.exec(`UPDATE profiles SET x_url=?, youtube_url=?`, "https://profile.example.test/synthetic", "https://www.youtube.com/channel/synthetic")
	f.post("/profile-media/discover", map[string]any{"slot": "banner", "source": "x"}).expect(t, http.StatusInternalServerError, "internal_error")
	if len(f.network.visited) != 0 {
		t.Fatalf("an unlisted profile page was requested: %v", f.network.visited)
	}
	f.network.routes["https://www.youtube.com/channel/synthetic"] = func(*http.Request) *http.Response {
		return reply(http.StatusSeeOther, map[string]string{"Location": "https://consent.example.test/continue"}, nil)
	}
	f.post("/profile-media/discover", map[string]any{"slot": "banner", "source": "youtube"}).expect(t, http.StatusInternalServerError, "internal_error")
	if f.network.reached("https://consent.example.test") {
		t.Fatal("a redirect to an unlisted host was followed")
	}
	f.network.routes["https://www.youtube.com/channel/synthetic"] = func(*http.Request) *http.Response {
		return reply(http.StatusForbidden, nil, []byte("blocked"))
	}
	f.post("/profile-media/discover", map[string]any{"slot": "banner", "source": "youtube"}).expect(t, http.StatusInternalServerError, "internal_error")
	if f.text(`SELECT CAST(COUNT(*) AS TEXT) FROM profile_media`) != "0" {
		t.Fatal("failed discovery must not register candidates")
	}
}

func seedScheduleAssets(f *fixture) (boardURL, thumbURL, thumbHash string) {
	boardURL, thumbURL = "https://pbs.twimg.com/media/board.jpg", "https://pbs.twimg.com/media/thumb.jpg"
	stamp := "2026-10-01T00:00:00.000Z"
	f.exec(`INSERT INTO posts(id,source,text,published_at,url,raw_json) VALUES('p1','x',?,?,?,?)`, "配信します", stamp, "https://x.com/synthetic/status/1", `{"search_text":"今週のスケジュールです"}`)
	f.exec(`INSERT INTO posts(id,source,text,published_at,url) VALUES('p2','x',?,?,?)`, "配信予定のお知らせ", stamp, "https://x.com/synthetic/status/2")
	f.exec(`INSERT INTO assets(id,kind,url,source_url,alt,week_start,updated_at,period_start,period_end,period_basis) VALUES('board','schedule',?,?,?,?,?,?,?,'inferred')`,
		boardURL, "https://x.com/synthetic/status/1", "weekly board", "2026-10-05", stamp, "2026-10-05", "2026-10-11")
	f.exec(`INSERT INTO assets(id,kind,url,source_url,week_start,updated_at) VALUES('thumb','schedule',?,?,?,?)`, thumbURL, "https://x.com/synthetic/status/2", "2026-09-28", stamp)
	f.exec(`INSERT INTO assets(id,kind,url,updated_at) VALUES('logo','brand','/assets/logo.png',?)`, stamp)
	f.exec(`INSERT INTO schedule_asset_reviews(asset_id,llm_status,llm_confidence,llm_reason,updated_at) VALUES('board','schedule',0.9,'weekly grid',?)`, stamp)
	f.exec(`INSERT INTO schedule_asset_reviews(asset_id,llm_status,llm_confidence,updated_at) VALUES('thumb','schedule',0.7,?)`, stamp)

	image := syntheticPNG(40)
	thumbHash = sha256Hex(image)
	relative := "x/sha256/" + thumbHash[:2] + "/" + thumbHash + ".png"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(f.dataRoot, filepath.FromSlash(relative))), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dataRoot, filepath.FromSlash(relative)), image, 0o600); err != nil {
		f.t.Fatal(err)
	}
	f.exec(`INSERT INTO media_assets(id,source,source_url,cache_path,mime_type,sha256,status,created_at,updated_at) VALUES(?,?,?,?,?,?,'ready',?,?)`,
		sha256Hex([]byte(thumbURL)), "x", thumbURL, relative, "image/png", thumbHash, stamp, stamp)
	return boardURL, thumbURL, thumbHash
}

func TestScheduleAssetListAndManualReview(t *testing.T) {
	f := newFixture(t, true)
	boardURL, thumbURL, thumbHash := seedScheduleAssets(f)

	listing := f.request(http.MethodGet, "/schedule-images", "", nil)
	listing.expect(t, http.StatusOK, "")
	assets := list(t, listing.json["items"])
	if len(assets) != 2 {
		t.Fatalf("only schedule images are listed: %s", listing.raw)
	}
	board, thumb := object(t, assets[0]), object(t, assets[1])
	if board["id"] != "board" || board["approved"] != true || board["sourceMatchesBoard"] != true || board["effectiveStatus"] != "schedule" ||
		board["llmConfidence"] != 0.9 || board["manualStatus"] != "unreviewed" || board["periodBasis"] != "inferred" || board["weekStart"] != "2026-10-05" {
		t.Fatalf("unexpected board review: %#v", board)
	}
	if board["mediaStatus"] != "pending" || board["mediaId"] != sha256Hex([]byte(boardURL)) || board["mediaSourceUrl"] != boardURL || board["previewUrl"] != nil {
		t.Fatalf("an undownloaded image is pending: %#v", board)
	}
	// The image check alone cannot approve a post that is not a board notice.
	if thumb["id"] != "thumb" || thumb["approved"] != false || thumb["sourceMatchesBoard"] != false || thumb["periodBasis"] != "legacy" {
		t.Fatalf("unexpected thumbnail review: %#v", thumb)
	}
	if thumb["mediaStatus"] != "ready" || thumb["previewUrl"] != "/media/"+sha256Hex([]byte(thumbURL))+"?v="+thumbHash {
		t.Fatalf("a cached image exposes only its opaque URL: %#v", thumb)
	}
	if _, leaked := board["sourceText"]; leaked {
		t.Fatal("source post text must stay server-side")
	}
	for _, key := range []string{"manualReason", "manualCheckedAt", "periodReason", "llmEvidence", "inferredPeriodStart"} {
		if _, present := thumb[key]; !present {
			t.Fatalf("%s must be present as null", key)
		}
	}
	if limited := f.request(http.MethodGet, "/schedule-images?limit=1", "", nil); len(list(t, limited.json["items"])) != 1 {
		t.Fatalf("limit was ignored: %s", limited.raw)
	}
	if fallback := f.request(http.MethodGet, "/schedule-images?limit=abc", "", nil); len(list(t, fallback.json["items"])) != 2 {
		t.Fatalf("an unparseable limit uses the default: %s", fallback.raw)
	}

	before := f.revision()
	reviewed := f.post("/schedule-images/thumb/review", map[string]any{"status": "schedule", "reason": "  confirmed board  "})
	reviewed.expect(t, http.StatusOK, "")
	asset := object(t, reviewed.json["image"])
	if asset["approved"] != true || asset["manualStatus"] != "schedule" || asset["manualReason"] != "confirmed board" || asset["manualCheckedAt"] == nil ||
		asset["llmStatus"] != "schedule" || asset["llmConfidence"] != 0.7 || asset["previewUrl"] == nil {
		t.Fatalf("unexpected manual approval: %#v", asset)
	}
	if f.revision() == before {
		t.Fatal("a manual review must bump the dashboard revision")
	}
	rejected := f.post("/schedule-images/board/review", map[string]any{"status": "not_schedule", "reason": "thumbnail"})
	if asset = object(t, rejected.json["image"]); asset["approved"] != false || asset["effectiveStatus"] != "not_schedule" {
		t.Fatalf("a manual rejection overrides the image check: %#v", asset)
	}
	cleared := f.post("/schedule-images/board/review", map[string]any{"status": "unreviewed", "reason": "ignored"})
	if asset = object(t, cleared.json["image"]); asset["approved"] != true || asset["manualReason"] != nil || asset["manualCheckedAt"] != nil {
		t.Fatalf("clearing the label restores the automatic decision: %#v", asset)
	}

	before = f.revision()
	f.post("/schedule-images/thumb/review", map[string]any{}).expectInvalid(t, "status is required")
	f.post("/schedule-images/thumb/review", map[string]any{"status": "Schedule", "reason": "x"}).expectInvalid(t, "status is invalid")
	f.post("/schedule-images/thumb/review", map[string]any{"status": "schedule"}).expectInvalid(t, "reason is required")
	f.post("/schedule-images/thumb/review", map[string]any{"status": "schedule", "reason": strings.Repeat("r", 501)}).expectInvalid(t, "reason is too long")
	f.post("/schedule-images/logo/review", map[string]any{"status": "schedule", "reason": "x"}).expect(t, http.StatusNotFound, "schedule_image_not_found")
	f.post("/schedule-images/unknown/review", map[string]any{"status": "unreviewed"}).expect(t, http.StatusNotFound, "schedule_image_not_found")
	if f.revision() != before || f.text(`SELECT CAST(COUNT(*) AS TEXT) FROM schedule_asset_reviews`) != "2" {
		t.Fatal("rejected reviews must not change the snapshot")
	}
}

func TestScheduleAssetWithoutImageReportsNoImage(t *testing.T) {
	f := newFixture(t, true)
	stamp := "2026-10-01T00:00:00.000Z"
	f.exec(`INSERT INTO assets(id,kind,url,week_start,updated_at) VALUES('weekly-schedule','schedule','','2026-09-28',?)`, stamp)
	f.exec(`INSERT INTO schedule_asset_reviews(asset_id,llm_status,llm_reason,updated_at) VALUES('weekly-schedule','skipped','no_image',?)`, stamp)

	listing := f.request(http.MethodGet, "/schedule-images", "", nil)
	listing.expect(t, http.StatusOK, "")
	assets := list(t, listing.json["items"])
	if len(assets) != 1 {
		t.Fatalf("the legacy alias is listed: %s", listing.raw)
	}
	alias := object(t, assets[0])
	if alias["mediaStatus"] != "none" || alias["previewUrl"] != nil || alias["mediaId"] != nil || alias["mediaSourceUrl"] != nil ||
		alias["approved"] != false || alias["periodBasis"] != "legacy" || alias["effectiveStatus"] != "skipped" || alias["llmReason"] != "no_image" {
		t.Fatalf("a URL-less asset reports no image: %#v", alias)
	}

	// The operator closes it with the ordinary label; the scan verdict stays.
	rejected := f.post("/schedule-images/weekly-schedule/review", map[string]any{"status": "not_schedule", "reason": "legacy record"})
	rejected.expect(t, http.StatusOK, "")
	if asset := object(t, rejected.json["image"]); asset["manualStatus"] != "not_schedule" || asset["effectiveStatus"] != "not_schedule" ||
		asset["llmStatus"] != "skipped" || asset["llmReason"] != "no_image" || asset["mediaStatus"] != "none" {
		t.Fatalf("unexpected label on a URL-less asset: %#v", asset)
	}
	if f.text(`SELECT url||'|'||period_basis FROM assets WHERE id='weekly-schedule'`) != "|legacy" {
		t.Fatal("labelling must not rewrite the legacy row")
	}
}

func TestScheduleAssetPeriod(t *testing.T) {
	f := newFixture(t, true)
	seedScheduleAssets(f)
	before := f.revision()
	updated := f.put("/schedule-images/thumb/period", map[string]any{"start": "2026-10-07", "end": "2026-10-20", "reason": " corrected range "})
	updated.expect(t, http.StatusOK, "")
	asset := object(t, updated.json["image"])
	if asset["periodStart"] != "2026-10-07" || asset["periodEnd"] != "2026-10-20" || asset["periodBasis"] != "manual" ||
		asset["periodReason"] != "corrected range" || asset["weekStart"] != "2026-10-05" || asset["inferredPeriodStart"] != nil {
		t.Fatalf("unexpected manual period: %#v", asset)
	}
	if f.revision() == before {
		t.Fatal("a period change must bump the dashboard revision")
	}

	before = f.revision()
	invalid := []map[string]any{
		{"start": "2026-10-07", "end": "2026-10-06", "reason": "r"},
		{"start": "2026-02-30", "end": "2026-03-01", "reason": "r"},
		{"start": "2026-1-7", "end": "2026-01-08", "reason": "r"},
		{"start": "2025-01-01", "end": "2026-01-03", "reason": "r"},
		{"start": 20261007, "end": "2026-10-08", "reason": "r"},
		{"end": "2026-10-08", "reason": "r"},
	}
	for _, body := range invalid {
		f.put("/schedule-images/thumb/period", body).expectInvalid(t, "period is invalid")
	}
	f.put("/schedule-images/thumb/period", map[string]any{"start": "2026-10-07", "end": "2026-10-08"}).expectInvalid(t, "reason is required")
	f.put("/schedule-images/logo/period", map[string]any{"start": "2026-10-07", "end": "2026-10-08", "reason": "r"}).expect(t, http.StatusNotFound, "schedule_image_not_found")
	if f.revision() != before || f.text(`SELECT manual_period_end FROM assets WHERE id='thumb'`) != "2026-10-20" {
		t.Fatal("rejected periods must not change the snapshot")
	}
	// Exactly 366 days is the widest accepted span.
	f.put("/schedule-images/thumb/period", map[string]any{"start": "2025-01-01", "end": "2026-01-02", "reason": "r"}).expect(t, http.StatusOK, "")
}

func TestPostReprocessRequest(t *testing.T) {
	f := newFixture(t, true)
	f.exec(`INSERT INTO posts(id,source,text,published_at,url) VALUES('p/1','x','synthetic','2026-10-01T00:00:00.000Z','https://x.com/synthetic/status/1')`)
	f.exec(`INSERT INTO post_llm_states(post_id,status,last_error,last_extraction_id,last_processed_at,updated_at,failure_count,failure_fingerprint,next_attempt_at)
		VALUES('p/1','failed','synthetic failure',7,'2026-10-02T00:00:00.000Z','2026-10-02T00:00:00.000Z',4,'synthetic-input',NULL)`)
	module := f.module
	module.now = func() time.Time { return time.Date(2026, 10, 13, 1, 2, 3, 4_000_000, time.UTC) }

	// The identifier travels as one encoded path segment.
	queued := f.post("/posts/p%2F1/reprocess", map[string]any{"route": "schedule_board"})
	queued.expect(t, http.StatusOK, "")
	state := object(t, queued.json["state"])
	if state["postId"] != "p/1" || state["status"] != "queued" || state["route"] != "schedule_board" || state["reprocessRequested"] != true ||
		state["lastError"] != nil || state["lastExtractionId"] != float64(7) || state["lastProcessedAt"] != "2026-10-02T00:00:00.000Z" ||
		state["lastAttemptAt"] != nil || state["updatedAt"] != "2026-10-13T01:02:03.004Z" {
		t.Fatalf("unexpected reprocess state: %#v", state)
	}
	// The request starts the failure backoff over, even after retries stopped.
	if got := f.text(`SELECT failure_count || '|' || COALESCE(failure_fingerprint, '-') || '|' || COALESCE(next_attempt_at, '-') FROM post_llm_states`); got != "0|-|-" {
		t.Fatalf("failure backoff kept: %s", got)
	}
	// An unknown route never replaces the stored one.
	kept := f.post("/posts/p%2F1/reprocess", map[string]any{"route": "anything"})
	if object(t, kept.json["state"])["route"] != "schedule_board" {
		t.Fatalf("stored route was replaced: %s", kept.raw)
	}
	f.request(http.MethodPost, "/posts/p%2F1/reprocess", "", nil).expect(t, http.StatusOK, "")
	f.post("/posts/unknown/reprocess", map[string]any{}).expect(t, http.StatusNotFound, "post_not_found")
	if f.text(`SELECT CAST(COUNT(*) AS TEXT) FROM post_llm_states`) != "1" {
		t.Fatal("an unknown post must not create state")
	}
}
