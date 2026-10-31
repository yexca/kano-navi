package sqlite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yexca/kano-navi/backend/internal/testutil"
)

func TestAdminVideosServeOnlyLocalThumbnails(t *testing.T) {
	// The store's data root is the parent of the database directory, so the
	// snapshot moves under <root>/database next to the cached media tree.
	root := t.TempDir()
	path := filepath.Join(root, "database", "kano.sqlite")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(testutil.NewSnapshot(t), path); err != nil {
		t.Fatal(err)
	}
	content := []byte("synthetic thumbnail")
	hash := sha256Hex(string(content))
	cache := "youtube/sha256/" + hash + ".jpg"
	if err := os.MkdirAll(filepath.Join(root, "youtube", "sha256"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(cache)), content, 0o644); err != nil {
		t.Fatal(err)
	}
	thumb := func(id string) string { return "https://i.ytimg.com/vi/" + id + "/hqdefault.jpg" }
	for i, id := range []string{"cached", "evicted", "pending", "none"} {
		var thumbnail any
		if id != "none" {
			thumbnail = thumb(id)
		}
		testutil.Exec(t, path, `INSERT INTO videos (id,source,title,published_at,url,thumbnail_url,kind) VALUES (?,'youtube',?,?,?,?,'video')`, id, id, fmt.Sprintf("2026-10-1%dT00:00:00.000Z", i), "https://www.youtube.com/watch?v="+id, thumbnail)
	}
	for _, id := range []string{"cached", "evicted"} {
		cachePath := cache
		if id == "evicted" {
			cachePath = "youtube/sha256/" + strings.Repeat("0", 64) + ".jpg"
		}
		testutil.Exec(t, path, `INSERT INTO media_assets(id,source,source_url,status,sha256,cache_path,created_at,updated_at) VALUES(?,'youtube',?,'ready',?,?,'2026-10-10T00:00:00Z','2026-10-10T00:00:00Z')`, sha256Hex(thumb(id)), thumb(id), hash, cachePath)
	}
	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	page, err := store.ListAdminVideos(context.Background(), AdminVideoFilter{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct{ url, status string }{
		"cached":  {"/media/" + sha256Hex(thumb("cached")) + "?v=" + hash, "ready"},
		"evicted": {"", "missing"},
		"pending": {"", "pending"},
		"none":    {"", "missing"},
	}
	if len(page.Items) != len(want) {
		t.Fatalf("items = %d, want %d", len(page.Items), len(want))
	}
	for _, video := range page.Items {
		w := want[video.ID]
		if got := stringValue(video.ThumbnailURL); got != w.url || video.ThumbnailStatus != w.status {
			t.Fatalf("%s thumbnail = %q/%s, want %q/%s", video.ID, got, video.ThumbnailStatus, w.url, w.status)
		}
	}
}

func TestAdminVideosReturnPinnedVideoOutsideThePage(t *testing.T) {
	path := testutil.NewSnapshot(t)
	testutil.Exec(t, path, `INSERT INTO videos (id,source,title,published_at,url,kind) VALUES ('pinned','youtube','Pinned','2026-01-01T00:00:00.000Z','https://www.youtube.com/watch?v=pinned','video')`)
	testutil.Exec(t, path, `INSERT INTO videos (id,source,title,scheduled_at,url,kind,is_upcoming) VALUES ('soon','youtube','Soon','2026-10-30T12:00:00.000Z','https://www.youtube.com/watch?v=soon','live',1)`)
	testutil.Exec(t, path, `INSERT OR REPLACE INTO app_settings(key,value,updated_at) VALUES('featured_video_id','pinned','2026-10-20T00:00:00Z')`)
	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	page, err := store.ListAdminVideos(context.Background(), AdminVideoFilter{Status: "upcoming"})
	if err != nil {
		t.Fatal(err)
	}
	for _, video := range page.Items {
		if video.ID == "pinned" {
			t.Fatal("the upcoming filter listed the published pinned video")
		}
	}
	if page.FeaturedVideo == nil || page.FeaturedVideo.ID != "pinned" || page.FeaturedVideo.Title != "Pinned" || page.FeaturedVideo.ThumbnailStatus != "missing" {
		t.Fatalf("featured video = %+v", page.FeaturedVideo)
	}
	testutil.Exec(t, path, `UPDATE app_settings SET value='' WHERE key='featured_video_id'`)
	page, err = store.ListAdminVideos(context.Background(), AdminVideoFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if page.FeaturedVideoID != nil || page.FeaturedVideo != nil {
		t.Fatalf("automatic selection returned %v/%+v", page.FeaturedVideoID, page.FeaturedVideo)
	}
}
