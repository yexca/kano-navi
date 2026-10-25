package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

func TestAdminPostsPreviewOnlyLocalImages(t *testing.T) {
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
	content := []byte("synthetic post image")
	hash := sha256Hex(string(content))
	cache := "x/sha256/" + hash + ".jpg"
	if err := os.MkdirAll(filepath.Join(root, "x", "sha256"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(cache)), content, 0o644); err != nil {
		t.Fatal(err)
	}
	image := func(name string) string { return "https://media.example.test/" + name + ".jpg" }
	for _, post := range []struct{ id, account, media, published string }{
		{"linked", "kano_2525", image("first"), "2026-10-13T00:00:00.000Z"},
		{"pending", "kano_2525", image("pending"), "2026-10-12T00:00:00.000Z"},
		{"text", "_Kanotic", "", "2026-10-11T00:00:00.000Z"},
	} {
		var media any
		if post.media != "" {
			media = post.media
		}
		testutil.Exec(t, path, `INSERT INTO posts (id,source,account_handle,type,text,published_at,url,media_url,raw_json) VALUES (?,'x',?,'post','text',?,?,?,'{}')`, post.id, post.account, post.published, "https://x.example.invalid/"+post.id, media)
	}
	for position, name := range []string{"first", "second"} {
		status, cachePath := "ready", any(cache)
		if name == "second" {
			status, cachePath = "pending", nil
		}
		testutil.Exec(t, path, `INSERT INTO media_assets(id,source,source_url,status,sha256,cache_path,created_at,updated_at) VALUES(?,'x',?,?,?,?,'2026-10-10T00:00:00Z','2026-10-10T00:00:00Z')`, sha256Hex(image(name)), image(name), status, hash, cachePath)
		testutil.Exec(t, path, `INSERT INTO media_links(media_id,owner_type,owner_id,role,position,created_at,updated_at) VALUES(?,'post','linked','post-image',?,'2026-10-10T00:00:00Z','2026-10-10T00:00:00Z')`, sha256Hex(image(name)), position)
	}
	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	page, err := store.ListAdminPosts(context.Background(), AdminPostFilter{})
	if err != nil {
		t.Fatal(err)
	}
	// Each image is "status url"; only a ready cached file has a URL.
	want := map[string]struct {
		status string
		images []string
	}{
		"linked":  {"ready", []string{"ready /media/" + sha256Hex(image("first")) + "?v=" + hash, "pending "}},
		"pending": {"pending", []string{"pending "}},
		"text":    {"none", []string{}},
	}
	if len(page.Items) != len(want) {
		t.Fatalf("items = %d, want %d", len(page.Items), len(want))
	}
	for _, post := range page.Items {
		w := want[post.ID]
		images := []string{}
		for _, media := range post.Media {
			images = append(images, media.Status+" "+stringValue(media.URL))
		}
		if post.MediaStatus != w.status || strings.Join(images, "|") != strings.Join(w.images, "|") {
			t.Fatalf("%s media = %s %q, want %s %q", post.ID, post.MediaStatus, images, w.status, w.images)
		}
	}

	// The account filter ignores case and a leading @.
	page, err = store.ListAdminPosts(context.Background(), AdminPostFilter{Account: "@KANO_2525"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || page.Items[0].ID != "linked" || page.Items[1].ID != "pending" {
		t.Fatalf("account filter = %d %+v", page.Total, page.Items)
	}
}
