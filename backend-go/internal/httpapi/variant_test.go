package httpapi

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "image/png"
)

// noisyJPEG is a synthetic photo-like image large enough to shrink.
func noisyJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 7), G: uint8(y * 13), B: uint8((x ^ y) * 3), A: 0xff})
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func decodedWidth(t *testing.T, content []byte) int {
	t.Helper()
	config, _, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("variant does not decode: %v", err)
	}
	return config.Width
}

func TestLineblogMediaServesSmallerCopies(t *testing.T) {
	store, dir := lineblogStore(t)
	original := noisyJPEG(t, 1000, 500)
	small := noisyJPEG(t, 200, 100)
	video := append([]byte("\x00\x00\x00\x18ftypmp42"), []byte(strings.Repeat("synthetic", 20))...)
	for name, content := range map[string][]byte{"0123456789ab.jpg": original, "abcdefabcdef.jpg": small, "ba9876543210.mp4": video} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewHandler(store)

	copy := request(t, handler, http.MethodGet, "/media/lineblog/0123456789ab.jpg?w=320")
	if copy.Code != http.StatusOK || copy.Body.Len() >= len(original) || decodedWidth(t, copy.Body.Bytes()) != 320 {
		t.Fatalf("copy status=%d length=%d", copy.Code, copy.Body.Len())
	}
	headers := copy.Header()
	if headers.Get("Content-Type") != "image/jpeg" || headers.Get("X-Content-Type-Options") != "nosniff" ||
		headers.Get("Content-Security-Policy") != "default-src 'none'; sandbox" || headers.Get("ETag") == "" ||
		headers.Get("Cache-Control") != mediaCacheControl(false) {
		t.Fatalf("copy headers=%v", headers)
	}
	cached, _ := filepath.Glob(filepath.Join(store.DataRoot(), "cache", "variants", "*", "*-320.jpg"))
	if len(cached) != 1 {
		t.Fatalf("copy not cached: %v", cached)
	}
	again := request(t, handler, http.MethodGet, "/media/lineblog/0123456789ab.jpg?w=320")
	if !bytes.Equal(again.Body.Bytes(), copy.Body.Bytes()) || again.Header().Get("ETag") != headers.Get("ETag") {
		t.Fatal("cached copy differs")
	}
	fresh := httptest.NewRecorder()
	revalidate := httptest.NewRequest(http.MethodGet, "/media/lineblog/0123456789ab.jpg?w=320", nil)
	revalidate.Header.Set("If-None-Match", headers.Get("ETag"))
	handler.ServeHTTP(fresh, revalidate)
	if fresh.Code != http.StatusNotModified {
		t.Fatalf("revalidation status=%d", fresh.Code)
	}
	if head := request(t, handler, http.MethodHead, "/media/lineblog/0123456789ab.jpg?w=320"); head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD status=%d length=%d", head.Code, head.Body.Len())
	}

	// No width, an image no wider than the width and a video get the original.
	for path, want := range map[string][]byte{
		"/media/lineblog/0123456789ab.jpg":       original,
		"/media/lineblog/abcdefabcdef.jpg?w=320": small,
		"/media/lineblog/ba9876543210.mp4?w=320": video,
	} {
		recorder := request(t, handler, http.MethodGet, path)
		if recorder.Code != http.StatusOK || !bytes.Equal(recorder.Body.Bytes(), want) {
			t.Fatalf("%s status=%d length=%d", path, recorder.Code, recorder.Body.Len())
		}
	}
	for _, path := range []string{
		"/media/lineblog/0123456789ab.jpg?w=333",
		"/media/lineblog/0123456789ab.jpg?w=",
		"/media/lineblog/0123456789ab.jpg?w=320&w=640",
		"/media/lineblog/ba9876543210.mp4?w=abc",
		"/media/lineblog/cccccccccccc.jpg?w=320",
		"/media/lineblog/../0123456789ab.jpg?w=320",
	} {
		if recorder := request(t, handler, http.MethodGet, path); recorder.Code != http.StatusNotFound {
			t.Fatalf("%s status=%d", path, recorder.Code)
		}
	}
}

func TestLineblogMediaRereadsAReplacedFile(t *testing.T) {
	store, dir := lineblogStore(t)
	path := filepath.Join(dir, "0123456789ab.jpg")
	if err := os.WriteFile(path, noisyJPEG(t, 1000, 500), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(store)
	first := request(t, handler, http.MethodGet, "/media/lineblog/0123456789ab.jpg?w=640")
	replaced := noisyJPEG(t, 1400, 300)
	if err := os.WriteFile(path, replaced, 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	second := request(t, handler, http.MethodGet, "/media/lineblog/0123456789ab.jpg?w=640")
	if first.Code != http.StatusOK || second.Code != http.StatusOK || bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
		t.Fatalf("replaced file served its old copy: %d %d", first.Code, second.Code)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(second.Body.Bytes()))
	if err != nil || config.Width != 640 || config.Height != 137 {
		t.Fatalf("copy of the replaced file %dx%d %v", config.Width, config.Height, err)
	}
}

func TestStaticSiteServesSmallerHistoryCopies(t *testing.T) {
	store, _ := lineblogStore(t)
	root := t.TempDir()
	original := noisyJPEG(t, 1000, 500)
	for name, content := range map[string][]byte{
		"index.html":                   []byte("<!doctype html><title>synthetic</title>"),
		"assets/history/synthetic.jpg": original,
		"assets/kano-banner.jpg":       original,
	} {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewStandaloneHandler(store, nil, Standalone{Static: NewStaticSite(root, "")})

	copy := request(t, handler, http.MethodGet, "/assets/history/synthetic.jpg?v=synthetic&w=640")
	if copy.Code != http.StatusOK || decodedWidth(t, copy.Body.Bytes()) != 640 || copy.Body.Len() >= len(original) {
		t.Fatalf("history copy status=%d length=%d", copy.Code, copy.Body.Len())
	}
	if copy.Header().Get("Cache-Control") != "public, max-age=0, must-revalidate" || copy.Header().Get("Content-Type") != "image/jpeg" ||
		copy.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("history copy headers=%v", copy.Header())
	}
	// The tracked original is never changed.
	if onDisk, _ := os.ReadFile(filepath.Join(root, "assets", "history", "synthetic.jpg")); !bytes.Equal(onDisk, original) {
		t.Fatal("history original changed")
	}
	// Other static files and requests without w are served whole.
	for _, path := range []string{"/assets/history/synthetic.jpg?v=synthetic", "/assets/kano-banner.jpg?w=640"} {
		recorder := request(t, handler, http.MethodGet, path)
		if recorder.Code != http.StatusOK || !bytes.Equal(recorder.Body.Bytes(), original) {
			t.Fatalf("%s status=%d length=%d", path, recorder.Code, recorder.Body.Len())
		}
	}
	if recorder := request(t, handler, http.MethodGet, "/assets/history/synthetic.jpg?w=100"); recorder.Code != http.StatusNotFound {
		t.Fatalf("unlisted width status=%d", recorder.Code)
	}
	// A site without the store's cache serves the original.
	plain := httptest.NewRecorder()
	NewStaticSite(root, "").ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/assets/history/synthetic.jpg?w=640", nil))
	if plain.Code != http.StatusOK || !bytes.Equal(plain.Body.Bytes(), original) {
		t.Fatalf("site without cache status=%d", plain.Code)
	}
}
