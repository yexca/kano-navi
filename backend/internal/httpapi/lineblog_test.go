package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend/internal/testutil"
)

func lineblogStore(t *testing.T) (*sqlite.Store, string) {
	t.Helper()
	store, err := sqlite.OpenReadOnly(testutil.NewSnapshot(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	dir := filepath.Join(store.DataRoot(), "lineblog")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return store, dir
}

func TestLineblogMediaServesContainedArchiveFiles(t *testing.T) {
	store, dir := lineblogStore(t)
	video := append([]byte("\x00\x00\x00\x18ftypmp42"), []byte(strings.Repeat("synthetic", 20))...)
	if err := os.WriteFile(filepath.Join(dir, "0123456789ab.mp4"), video, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "abcdefabcdef.png"), []byte("\x89PNG\r\n\x1a\nsynthetic"), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(store)

	recorder := request(t, handler, http.MethodGet, "/media/lineblog/0123456789ab.mp4")
	if recorder.Code != http.StatusOK || recorder.Body.Len() != len(video) {
		t.Fatalf("video status=%d length=%d", recorder.Code, recorder.Body.Len())
	}
	headers := recorder.Header()
	if headers.Get("Content-Type") != "video/mp4" || headers.Get("X-Content-Type-Options") != "nosniff" ||
		headers.Get("Content-Security-Policy") != "default-src 'none'; sandbox" ||
		headers.Get("Accept-Ranges") != "bytes" || headers.Get("ETag") == "" {
		t.Fatalf("video headers=%v", headers)
	}

	partial := httptest.NewRecorder()
	ranged := httptest.NewRequest(http.MethodGet, "/media/lineblog/0123456789ab.mp4", nil)
	ranged.Header.Set("Range", "bytes=4-11")
	handler.ServeHTTP(partial, ranged)
	if partial.Code != http.StatusPartialContent || partial.Body.String() != "ftypmp42" {
		t.Fatalf("range status=%d body=%q", partial.Code, partial.Body)
	}

	fresh := httptest.NewRecorder()
	revalidate := httptest.NewRequest(http.MethodGet, "/media/lineblog/0123456789ab.mp4", nil)
	revalidate.Header.Set("If-None-Match", headers.Get("ETag"))
	handler.ServeHTTP(fresh, revalidate)
	if fresh.Code != http.StatusNotModified {
		t.Fatalf("revalidation status=%d", fresh.Code)
	}

	image := request(t, handler, http.MethodHead, "/media/lineblog/abcdefabcdef.png")
	if image.Code != http.StatusOK || image.Header().Get("Content-Type") != "image/png" || image.Body.Len() != 0 {
		t.Fatalf("image HEAD status=%d headers=%v", image.Code, image.Header())
	}
	if post := request(t, handler, http.MethodPost, "/media/lineblog/abcdefabcdef.png"); post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d", post.Code)
	}
}

func TestLineblogMediaRejectsOtherPaths(t *testing.T) {
	store, dir := lineblogStore(t)
	outside := filepath.Join(filepath.Dir(dir), "fedcbafedcba.jpg")
	if err := os.WriteFile(outside, []byte("\xff\xd8\xffsynthetic"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(outside) })
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("synthetic"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "aaaaaaaaaaaa.jpg"), 0o755); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(dir, "bbbbbbbbbbbb.jpg")
	symlinked := os.Symlink(outside, linked) == nil
	handler := NewHandler(store)
	paths := []string{
		"/media/lineblog/",
		"/media/lineblog/notes.txt",
		"/media/lineblog/aaaaaaaaaaaa.jpg",
		"/media/lineblog/cccccccccccc.jpg",
		"/media/lineblog/ABCDEFABCDEF.jpg",
		"/media/lineblog/0123456789ab.gif",
		"/media/lineblog/../fedcbafedcba.jpg",
		"/media/lineblog/%2e%2e/fedcbafedcba.jpg",
		"/media/lineblog/x/fedcbafedcba.jpg",
	}
	if symlinked {
		paths = append(paths, "/media/lineblog/bbbbbbbbbbbb.jpg")
	}
	for _, path := range paths {
		recorder := request(t, handler, http.MethodGet, path)
		if recorder.Code != http.StatusNotFound || strings.Contains(recorder.Body.String(), "synthetic") {
			t.Fatalf("%s status=%d body=%s", path, recorder.Code, recorder.Body)
		}
	}
}
