package httpapi

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

func worksStore(t *testing.T) (*sqlite.Store, string) {
	t.Helper()
	store, lineblog := lineblogStore(t)
	dir := filepath.Join(filepath.Dir(lineblog), "works")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return store, dir
}

// syntheticPNG is a small synthetic cover image.
func syntheticPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 5), G: uint8(y * 11), B: 0x40, A: 0xff})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestWorksMediaServesContainedCovers(t *testing.T) {
	store, dir := worksStore(t)
	cover := noisyJPEG(t, 1000, 1000)
	art := syntheticPNG(t, 40, 40)
	jpgName, pngName := sha256Of(cover)[:12]+".jpg", sha256Of(art)[:12]+".png"
	for name, content := range map[string][]byte{jpgName: cover, pngName: art} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewHandler(store)

	for name, want := range map[string]struct {
		contentType string
		content     []byte
	}{jpgName: {"image/jpeg", cover}, pngName: {"image/png", art}} {
		recorder := request(t, handler, http.MethodGet, "/media/works/"+name)
		headers := recorder.Header()
		if recorder.Code != http.StatusOK || !bytes.Equal(recorder.Body.Bytes(), want.content) ||
			headers.Get("Content-Type") != want.contentType || headers.Get("X-Content-Type-Options") != "nosniff" ||
			headers.Get("Content-Security-Policy") != "default-src 'none'; sandbox" ||
			headers.Get("Accept-Ranges") != "bytes" || headers.Get("ETag") == "" ||
			headers.Get("Cache-Control") != mediaCacheControl(true) {
			t.Fatalf("%s status=%d headers=%v", name, recorder.Code, headers)
		}
	}

	partial := httptest.NewRecorder()
	ranged := httptest.NewRequest(http.MethodGet, "/media/works/"+pngName, nil)
	ranged.Header.Set("Range", "bytes=1-3")
	handler.ServeHTTP(partial, ranged)
	if partial.Code != http.StatusPartialContent || partial.Body.String() != "PNG" {
		t.Fatalf("range status=%d body=%q", partial.Code, partial.Body)
	}
	etag := request(t, handler, http.MethodGet, "/media/works/"+jpgName).Header().Get("ETag")
	fresh := httptest.NewRecorder()
	revalidate := httptest.NewRequest(http.MethodGet, "/media/works/"+jpgName, nil)
	revalidate.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(fresh, revalidate)
	if fresh.Code != http.StatusNotModified {
		t.Fatalf("revalidation status=%d", fresh.Code)
	}
	if head := request(t, handler, http.MethodHead, "/media/works/"+pngName); head.Code != http.StatusOK || head.Header().Get("Content-Type") != "image/png" || head.Body.Len() != 0 {
		t.Fatalf("HEAD status=%d headers=%v", head.Code, head.Header())
	}
	if post := request(t, handler, http.MethodPost, "/media/works/"+pngName); post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d", post.Code)
	}
}

func TestWorksMediaServesSmallerCopiesAndImmutableOnlyUnderTheirOwnHash(t *testing.T) {
	store, dir := worksStore(t)
	cover := noisyJPEG(t, 1000, 1000)
	makeCopies(t, store, cover)
	named := sha256Of(cover)[:12] + ".jpg"
	for name, content := range map[string][]byte{named: cover, "0123456789ab.jpg": cover} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewHandler(store)

	copy := request(t, handler, http.MethodGet, "/media/works/"+named+"?w=320")
	if copy.Code != http.StatusOK || copy.Body.Len() >= len(cover) || decodedWidth(t, copy.Body.Bytes()) != 320 ||
		copy.Header().Get("Content-Type") != "image/jpeg" || copy.Header().Get("ETag") != `"`+sha256Of(cover)+`-320"` {
		t.Fatalf("copy status=%d length=%d headers=%v", copy.Code, copy.Body.Len(), copy.Header())
	}
	// Bytes that do not match their name revalidate, and so do their copies.
	for path, want := range map[string]bool{
		"/media/works/" + named:                true,
		"/media/works/" + named + "?w=320":     true,
		"/media/works/0123456789ab.jpg":        false,
		"/media/works/0123456789ab.jpg?w=320":  false,
		"/media/works/" + named + "?w=1920":    true,
		"/media/works/0123456789ab.jpg?w=1920": false,
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			recorder := request(t, handler, method, path)
			if recorder.Code != http.StatusOK || recorder.Header().Get("Cache-Control") != mediaCacheControl(want) {
				t.Fatalf("%s %s status=%d cache=%q", method, path, recorder.Code, recorder.Header().Get("Cache-Control"))
			}
		}
	}
	// A cover replaced under its old name is hashed anew and loses immutable.
	path := filepath.Join(dir, named)
	if err := os.WriteFile(path, noisyJPEG(t, 1200, 400), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "?w=320"} {
		recorder := request(t, handler, http.MethodGet, "/media/works/"+named+suffix)
		if recorder.Code != http.StatusOK || recorder.Header().Get("Cache-Control") != mediaCacheControl(false) {
			t.Fatalf("replaced %s cache=%q", suffix, recorder.Header().Get("Cache-Control"))
		}
	}
	for _, path := range []string{
		"/media/works/" + named + "?w=333",
		"/media/works/" + named + "?w=",
		"/media/works/" + named + "?w=320&w=640",
		"/media/works/cccccccccccc.jpg?w=320",
	} {
		if recorder := request(t, handler, http.MethodGet, path); recorder.Code != http.StatusNotFound {
			t.Fatalf("%s status=%d", path, recorder.Code)
		}
	}
}

func TestWorksMediaRevalidationSeesASameSizeCorrection(t *testing.T) {
	store, dir := worksStore(t)
	art := syntheticPNG(t, 12, 12)
	name := sha256Of(art)[:12] + ".png"
	corrupt := bytes.Clone(art)
	corrupt[len(corrupt)/2] ^= 0xff
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(store)
	etag := request(t, handler, http.MethodGet, "/media/works/"+name).Header().Get("ETag")
	if err := os.WriteFile(path, art, 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	revalidate := httptest.NewRequest(http.MethodGet, "/media/works/"+name, nil)
	revalidate.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(recorder, revalidate)
	if recorder.Code != http.StatusOK || !bytes.Equal(recorder.Body.Bytes(), art) || recorder.Header().Get("ETag") == etag {
		t.Fatalf("status=%d etag=%q old=%q", recorder.Code, recorder.Header().Get("ETag"), etag)
	}
}

func TestWorksMediaRejectsOtherPaths(t *testing.T) {
	store, dir := worksStore(t)
	// A LINE BLOG file is not a cover, nor is a file beside the archive.
	lineblog := filepath.Join(filepath.Dir(dir), "lineblog", "dddddddddddd.jpg")
	if err := os.WriteFile(lineblog, []byte("\xff\xd8\xffsynthetic"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(dir), "fedcbafedcba.jpg")
	if err := os.WriteFile(outside, []byte("\xff\xd8\xffsynthetic"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(outside) })
	for name, content := range map[string]string{
		"notes.txt":        "synthetic",
		"0123456789ab.mp4": "\x00\x00\x00\x18ftypmp42synthetic",
		"0123456789ab.gif": "GIF89asynthetic",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "aaaaaaaaaaaa.jpg"), 0o755); err != nil {
		t.Fatal(err)
	}
	symlinked := os.Symlink(outside, filepath.Join(dir, "bbbbbbbbbbbb.jpg")) == nil
	handler := NewHandler(store)
	paths := []string{
		"/media/works/",
		"/media/works/notes.txt",
		"/media/works/0123456789ab.mp4",
		"/media/works/0123456789ab.mp4?w=320",
		"/media/works/0123456789ab.gif",
		"/media/works/aaaaaaaaaaaa.jpg",
		"/media/works/cccccccccccc.jpg",
		"/media/works/dddddddddddd.jpg",
		"/media/works/ABCDEFABCDEF.jpg",
		"/media/works/0123456789a.jpg",
		"/media/works/../fedcbafedcba.jpg",
		"/media/works/%2e%2e/fedcbafedcba.jpg",
		"/media/works/../lineblog/dddddddddddd.jpg",
		"/media/works/x/fedcbafedcba.jpg",
	}
	if symlinked {
		paths = append(paths, "/media/works/bbbbbbbbbbbb.jpg")
	}
	for _, path := range paths {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			recorder := request(t, handler, method, path)
			if recorder.Code != http.StatusNotFound || strings.Contains(recorder.Body.String(), "synthetic") {
				t.Fatalf("%s %s status=%d body=%s", method, path, recorder.Code, recorder.Body)
			}
			if method == http.MethodGet && !strings.Contains(recorder.Body.String(), "media_not_found") {
				t.Fatalf("%s body=%s", path, recorder.Body)
			}
		}
	}
}

func TestJourneyMediaServesItsArchiveWithCopies(t *testing.T) {
	store, works := worksStore(t)
	dir := filepath.Join(filepath.Dir(works), "journey")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	image := noisyJPEG(t, 1000, 500)
	makeCopies(t, store, image)
	named := sha256Of(image)[:12] + ".jpg"
	for name, content := range map[string][]byte{named: image, "0123456789ab.jpg": image} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A Works cover is not a Journey image.
	if err := os.WriteFile(filepath.Join(works, "fedcba987654.jpg"), image, 0o644); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(store)
	original := request(t, handler, http.MethodGet, "/media/journey/"+named)
	if original.Code != http.StatusOK || !bytes.Equal(original.Body.Bytes(), image) || original.Header().Get("Content-Type") != "image/jpeg" ||
		original.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("original status=%d headers=%v", original.Code, original.Header())
	}
	copy := request(t, handler, http.MethodGet, "/media/journey/"+named+"?w=640")
	if copy.Code != http.StatusOK || decodedWidth(t, copy.Body.Bytes()) != 640 || copy.Body.Len() >= len(image) {
		t.Fatalf("copy status=%d length=%d", copy.Code, copy.Body.Len())
	}
	for path, want := range map[string]bool{
		"/media/journey/" + named:               true,
		"/media/journey/" + named + "?w=640":    true,
		"/media/journey/0123456789ab.jpg":       false,
		"/media/journey/0123456789ab.jpg?w=640": false,
	} {
		recorder := request(t, handler, http.MethodGet, path)
		if recorder.Code != http.StatusOK || recorder.Header().Get("Cache-Control") != mediaCacheControl(want) {
			t.Fatalf("%s status=%d cache=%q", path, recorder.Code, recorder.Header().Get("Cache-Control"))
		}
	}
	for _, path := range []string{"/media/journey/fedcba987654.jpg", "/media/journey/synthetic.jpg", "/media/journey/" + named + "?w=100"} {
		if recorder := request(t, handler, http.MethodGet, path); recorder.Code != http.StatusNotFound {
			t.Fatalf("%s status=%d", path, recorder.Code)
		}
	}
	if post := request(t, handler, http.MethodPost, "/media/journey/"+named); post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d", post.Code)
	}
}
