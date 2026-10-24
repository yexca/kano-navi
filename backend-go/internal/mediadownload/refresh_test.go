package mediadownload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

func TestThumbnailFallbacksStepDownYouTubeSizes(t *testing.T) {
	const vi = "https://i.ytimg.com/vi/AAAAAAAAAAA/"
	for _, tc := range []struct {
		raw  string
		want []string
	}{
		{vi + "maxresdefault.jpg", []string{vi + "sddefault.jpg", vi + "hqdefault.jpg"}},
		{vi + "sddefault.jpg?sqp=synthetic", []string{vi + "hqdefault.jpg"}},
		{"https://i4.ytimg.com/vi/AAAAAAAAAAA/hqdefault.jpg", []string{}},
		{vi + "maxresdefault_live.jpg", []string{vi + "sddefault_live.jpg", vi + "hqdefault_live.jpg", vi + "hqdefault.jpg"}},
		{"https://i.ytimg.com/vi_webp/AAAAAAAAAAA/maxresdefault.webp", []string{"https://i.ytimg.com/vi_webp/AAAAAAAAAAA/sddefault.webp", "https://i.ytimg.com/vi_webp/AAAAAAAAAAA/hqdefault.webp"}},
		{vi + "custom.jpg", nil},
		{"https://pbs.twimg.com/vi/AAAAAAAAAAA/maxresdefault.jpg", nil},
	} {
		if got := ThumbnailFallbacks(tc.raw); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s fallbacks = %#v, want %#v", tc.raw, got, tc.want)
		}
	}
}

func TestThumbnailFallsBackAndRevalidatesUntilALargerSizeAppears(t *testing.T) {
	p := testutil.NewSnapshot(t)
	store, err := sqlite.OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	const vi = "https://i.ytimg.com/vi/AAAAAAAAAAA/"
	u := vi + "maxresdefault.jpg"
	h := sha256.Sum256([]byte(u))
	id := hex.EncodeToString(h[:])
	testutil.Exec(t, p, `INSERT INTO videos (id,source,title,published_at,url,is_upcoming) VALUES ('AAAAAAAAAAA','youtube','reservation',?,'https://www.youtube.com/watch?v=AAAAAAAAAAA',1)`, time.Now().UTC().Format(time.RFC3339))
	testutil.Exec(t, p, `INSERT INTO media_assets(id,source,source_url,status) VALUES(?,'youtube',?,'pending')`, id, u)
	testutil.Exec(t, p, `INSERT INTO media_links(media_id,owner_type,owner_id,role,position) VALUES(?,'video','AAAAAAAAAAA','thumbnail',0)`, id)

	// files maps each URL YouTube serves to its ETag and bytes; others are 404.
	type file struct{ etag, body string }
	files := map[string]file{vi + "hqdefault.jpg": {`"small"`, "\xff\xd8\xffsmall"}}
	requested := []string{}
	client := NewClient(&http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		requested = append(requested, r.URL.String())
		f, ok := files[r.URL.String()]
		switch {
		case !ok:
			return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(""))}, nil
		case r.Header.Get("If-None-Match") == f.etag:
			return &http.Response{StatusCode: 304, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}, "Etag": {f.etag}}, Body: io.NopCloser(strings.NewReader(f.body))}, nil
	})})
	expire := func() {
		testutil.Exec(t, p, `UPDATE media_assets SET last_checked_at=? WHERE id=?`, time.Now().Add(-time.Hour).UTC().Format("2006-01-02T15:04:05.000Z"), id)
		requested = nil
	}
	content := func() string {
		got, err := store.ReadMedia(ctx, id, "")
		if err != nil {
			t.Fatal(err)
		}
		return string(got.Content)
	}

	// maxres and sd were never generated, so hq is cached under the maxres URL.
	summary, err := client.DownloadPending(ctx, store, 5)
	if err != nil || summary != (Summary{Attempted: 1, Ready: 1}) || content() != "\xff\xd8\xffsmall" {
		t.Fatalf("first summary=%#v err=%v", summary, err)
	}
	if want := []string{u, vi + "sddefault.jpg", vi + "hqdefault.jpg"}; !reflect.DeepEqual(requested, want) {
		t.Fatalf("requested %v, want %v", requested, want)
	}

	// An unchanged file answers 304 and leaves the board revision alone.
	expire()
	revision, _ := store.GetDashboardRevision(ctx)
	summary, err = client.DownloadPending(ctx, store, 5)
	if err != nil || summary != (Summary{Attempted: 1, Unchanged: 1}) {
		t.Fatalf("revalidation summary=%#v err=%v", summary, err)
	}
	if after, _ := store.GetDashboardRevision(ctx); after != revision {
		t.Fatal("unchanged thumbnail bumped the revision")
	}
	if pending, _ := store.PendingMedia(ctx, 5); len(pending) != 0 {
		t.Fatalf("checked thumbnail requested again at once: %#v", pending)
	}

	// Once the larger size exists it replaces the cached file.
	expire()
	files[u] = file{`"large"`, "\xff\xd8\xfflarge"}
	summary, err = client.DownloadPending(ctx, store, 5)
	if err != nil || summary != (Summary{Attempted: 1, Ready: 1}) || content() != "\xff\xd8\xfflarge" {
		t.Fatalf("upgrade summary=%#v err=%v", summary, err)
	}
	if after, _ := store.GetDashboardRevision(ctx); after == revision {
		t.Fatal("a new thumbnail did not advance the revision")
	}
}
