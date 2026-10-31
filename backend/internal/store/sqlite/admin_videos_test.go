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

func TestAdminVideoFiltersSearchAndMerge(t *testing.T) {
	path := testutil.NewSnapshot(t)
	testutil.Exec(t, path, `INSERT INTO videos (id,source,title,manual_title,published_at,url,kind) VALUES ('yt1','youtube','Platform song','Corrected song','2026-10-01T00:00:00.000Z','https://www.youtube.com/watch?v=yt1','video')`)
	testutil.Exec(t, path, `INSERT INTO videos (id,source,title,published_at,url,kind) VALUES ('yt2','youtube','Chat 100%','2026-10-02T00:00:00.000Z','https://www.youtube.com/watch?v=yt2','live')`)
	testutil.Exec(t, path, `INSERT INTO videos (id,source,title,published_at,url,kind,merged_into,merge_source) VALUES ('BV1copy','bilibili','Platform song repost','2026-10-01T06:00:00.000Z','https://www.bilibili.com/video/BV1copy','video','yt1','rule')`)
	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ids := func(f AdminVideoFilter) string {
		t.Helper()
		page, err := store.ListAdminVideos(context.Background(), f)
		if err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, video := range page.Items {
			got = append(got, video.ID)
		}
		return strings.Join(got, ",")
	}
	for _, c := range []struct {
		filter AdminVideoFilter
		want   string
	}{
		// The shown title, the platform title and the ID all match.
		{AdminVideoFilter{Search: "corrected"}, "yt1"},
		{AdminVideoFilter{Search: "platform song"}, "BV1copy,yt1"},
		{AdminVideoFilter{Search: "BV1"}, "BV1copy"},
		// LIKE wildcards in the search are literal.
		{AdminVideoFilter{Search: "100%"}, "yt2"},
		{AdminVideoFilter{Merge: "visible"}, "yt2,yt1"},
		{AdminVideoFilter{Merge: "merged"}, "BV1copy"},
		{AdminVideoFilter{Search: "song", Merge: "visible"}, "yt1"},
	} {
		if got := ids(c.filter); got != c.want {
			t.Errorf("%+v listed %q, want %q", c.filter, got, c.want)
		}
	}
}

func TestAdminEventFiltersUnconfirmedAndDeleted(t *testing.T) {
	path := testutil.NewSnapshot(t)
	insert := `INSERT INTO events (id,source,title,starts_on,provenance,manual_locked,deleted_at,created_at,updated_at) VALUES (?,?,?,'2026-10-10',?,?,?,'','')`
	testutil.Exec(t, path, insert, "auto", "x", "Automatic stream", "automatic", 0, nil)
	testutil.Exec(t, path, insert, "locked", "x", "Confirmed stream", "automatic", 1, nil)
	testutil.Exec(t, path, insert, "manual", "manual", "Manual release", "manual", 1, nil)
	testutil.Exec(t, path, insert, "gone", "x", "Deleted entry", "automatic", 1, "2026-10-09T00:00:00Z")
	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, c := range []struct {
		filter AdminEventFilter
		want   string
	}{
		// Confirming locks an automatic entry, so it leaves the unconfirmed list.
		{AdminEventFilter{Provenance: "unconfirmed", Ascending: true}, "auto"},
		{AdminEventFilter{Provenance: "automatic", Ascending: true}, "auto,locked"},
		{AdminEventFilter{Ascending: true}, "auto,locked,manual"},
		{AdminEventFilter{IncludeDeleted: true, Ascending: true}, "auto,gone,locked,manual"},
		{AdminEventFilter{OnlyDeleted: true}, "gone"},
	} {
		page, err := store.ListAdminEvents(context.Background(), c.filter)
		if err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, event := range page.Items {
			got = append(got, event.ID)
		}
		if strings.Join(got, ",") != c.want {
			t.Errorf("%+v listed %v, want %q", c.filter, got, c.want)
		}
	}
}
