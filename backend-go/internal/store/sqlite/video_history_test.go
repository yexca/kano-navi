package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	ytsource "github.com/yexca/kano-navi/backend-go/internal/source/youtube"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

const historyChannel = "UCAAAAAAAAAAAAAAAAAAAAAA"

// snapshotUnderRoot places a fixture at <root>/database/kano.sqlite, so the
// store's data root holds the media cache.
func snapshotUnderRoot(t *testing.T) (root, path string) {
	t.Helper()
	root = t.TempDir()
	path = filepath.Join(root, "database", "kano.sqlite")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(testutil.NewSnapshot(t), path); err != nil {
		t.Fatal(err)
	}
	return root, path
}

func checkedVideo(id, title, thumbnail string) ytsource.Video {
	return ytsource.Video{ID: id, Title: title, PublishedAt: "2026-01-02T09:00:00Z", URL: "https://www.youtube.com/watch?v=" + id, ThumbnailURL: thumbnail, Kind: "MUSIC"}
}

func TestYoutubeStateQueuesRefreshRequestsAndDueFollowUps(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now()
	at := func(d time.Duration) string { return workflowTime(now.Add(d)) }
	for _, v := range []struct {
		id, published, scheduled, checked, requested string
		upcoming                                     int
	}{
		{id: "requested", published: "2025-06-01T00:00:00Z", requested: at(-time.Hour)},
		{id: "stalerequest", published: "2025-06-01T00:00:00Z", requested: at(-25 * time.Hour)},
		{id: "dueupload", published: at(-2 * time.Hour), checked: at(-90 * time.Minute)},
		{id: "doneupload", published: at(-2 * time.Hour), checked: at(-30 * time.Minute)},
		{id: "premiere", published: at(-40 * time.Hour), scheduled: at(-30 * time.Hour), checked: at(-28 * time.Hour)},
		{id: "waitingroom", published: at(-2 * time.Hour), scheduled: at(time.Hour), upcoming: 1},
		{id: "ancient", published: at(-20 * 24 * time.Hour)},
	} {
		testutil.Exec(t, p, `INSERT INTO videos(id,source,title,published_at,scheduled_at,url,is_upcoming,live_checked_at,refresh_requested_at) VALUES(?,'youtube',?,?,NULLIF(?,''),?,?,NULLIF(?,''),NULLIF(?,''))`,
			v.id, v.id, v.published, v.scheduled, "https://www.youtube.com/watch?v="+v.id, v.upcoming, v.checked, v.requested)
	}
	// The premiere ended an hour after it started, so its day-one follow-up
	// counts from the end and is due; the check before it does not cover it.
	testutil.Exec(t, p, `INSERT INTO events(id,source,source_item_id,source_key,title,starts_on,starts_at,ends_at) VALUES('youtube-premiere','youtube','premiere','reservation','premiere',?,?,?)`, at(-30 * time.Hour)[:10], at(-30*time.Hour), at(-29*time.Hour))
	state, err := s.YoutubeRecentState(context.Background(), historyChannel)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(state.RefreshIDs, ",") != "requested" {
		t.Fatalf("refresh=%v", state.RefreshIDs)
	}
	if strings.Join(state.FollowUpIDs, ",") != "dueupload,premiere" {
		t.Fatalf("follow-ups=%v", state.FollowUpIDs)
	}
	if v, ok := state.Videos["requested"]; !ok || v.PublishedAt != "2025-06-01T00:00:00Z" {
		t.Fatalf("requested video state=%#v", v)
	}
}

func TestDetailCheckKeepsTheReplacedTitleAndThumbnail(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	write := func(v ytsource.Video) {
		t.Helper()
		if err := s.WriteYoutubeRecent(ctx, historyChannel, ytsource.Result{Videos: []ytsource.Video{v}, NextCursor: v.ID, NextCursorTime: v.PublishedAt}); err != nil {
			t.Fatal(err)
		}
	}
	first := checkedVideo("AAAAAAAAAAA", "Song cover premiere", "https://i.ytimg.com/vi/AAAAAAAAAAA/maxresdefault_live.jpg")
	write(first)
	testutil.Exec(t, p, `UPDATE videos SET refresh_requested_at='2026-01-03T00:00:00Z'`)
	second := checkedVideo("AAAAAAAAAAA", "Song cover", "https://i.ytimg.com/vi/AAAAAAAAAAA/maxresdefault.jpg")
	second.DurationSeconds = 289
	write(second)
	write(second)
	// A feed entry never replaces checked details, so it records nothing.
	feed := second
	feed.FeedOnly, feed.Title = true, "feed title"
	write(feed)
	db, _ := sql.Open("sqlite", p)
	defer db.Close()
	var revisions int
	var origin, title, thumbnail string
	if err = db.QueryRow(`SELECT COUNT(*),MAX(origin),MAX(title),MAX(thumbnail_url) FROM video_revisions WHERE video_id='AAAAAAAAAAA'`).Scan(&revisions, &origin, &title, &thumbnail); err != nil {
		t.Fatal(err)
	}
	if revisions != 1 || origin != revisionSync || title != first.Title || thumbnail != first.ThumbnailURL {
		t.Fatalf("revisions=%d origin=%s title=%s thumbnail=%s", revisions, origin, title, thumbnail)
	}
	var duration int
	var requested sql.NullString
	if err = db.QueryRow(`SELECT duration_seconds,refresh_requested_at FROM videos WHERE id='AAAAAAAAAAA'`).Scan(&duration, &requested); err != nil {
		t.Fatal(err)
	}
	if duration != 289 || requested.Valid {
		t.Fatalf("duration=%d requested=%v", duration, requested)
	}
}

func TestThumbnailRevalidationKeepsTheReplacedFileAndAnOperatorCanPinIt(t *testing.T) {
	_, path := snapshotUnderRoot(t)
	s, err := OpenWorkflowCore(path, true)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	thumbnail := "https://i.ytimg.com/vi/AAAAAAAAAAA/maxresdefault.jpg"
	if err = s.WriteYoutubeRecent(ctx, historyChannel, ytsource.Result{Videos: []ytsource.Video{checkedVideo("AAAAAAAAAAA", "Song cover", thumbnail)}}); err != nil {
		t.Fatal(err)
	}
	pending, err := s.PendingMedia(ctx, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending=%v err=%v", pending, err)
	}
	asset := pending[0]
	placeholder, replaced := []byte("\xff\xd8\xff premiere placeholder"), []byte("\xff\xd8\xff final thumbnail")
	for _, content := range [][]byte{placeholder, replaced} {
		if err = s.SaveMediaDownload(ctx, asset, content, "image/jpeg", "", "", "ready", ""); err != nil {
			t.Fatal(err)
		}
	}
	oldHash, newHash := sha256Hex(string(placeholder)), sha256Hex(string(replaced))
	for hash, want := range map[string][]byte{oldHash: placeholder, newHash: replaced, "": replaced} {
		file, readErr := s.ReadMedia(ctx, asset.ID, hash)
		if readErr != nil || string(file.Content) != string(want) {
			t.Fatalf("version %q: file=%v err=%v", hash, file, readErr)
		}
	}
	if _, err = s.ReadMedia(ctx, asset.ID, strings.Repeat("0", 64)); !errors.Is(err, ErrMediaVersionNotFound) {
		t.Fatalf("unknown version err=%v", err)
	}
	history, err := s.VideoHistory(ctx, "AAAAAAAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Revisions) != 1 || history.Revisions[0].Origin != revisionMedia || stringValue(history.Revisions[0].ThumbnailURL) != "/media/"+asset.ID+"?v="+oldHash {
		t.Fatalf("revisions=%#v", history.Revisions)
	}
	if len(history.Thumbnails) != 2 || !history.Thumbnails[0].Current || history.Thumbnails[0].SHA256 != newHash || history.Thumbnails[1].SHA256 != oldHash {
		t.Fatalf("thumbnails=%#v", history.Thumbnails)
	}
	s.Close()

	admin, err := OpenAdminWrites(path)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	revision, _ := admin.GetDashboardRevision(ctx)
	title, kind, media := "奏 cover", "music", asset.ID
	video, err := admin.SetVideoOverrides(ctx, "AAAAAAAAAAA", VideoOverrides{SetTitle: true, Title: &title, SetKind: true, Kind: &kind, SetThumbnail: true, ThumbnailMediaID: &media, ThumbnailSHA256: &oldHash}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if video.Title != title || video.SourceTitle != "Song cover" || stringValue(video.Kind) != "MUSIC" || stringValue(video.ThumbnailURL) != "/media/"+asset.ID+"?v="+oldHash {
		t.Fatalf("video=%#v", video)
	}
	if after, _ := admin.GetDashboardRevision(ctx); after == revision {
		t.Fatal("an override did not advance the dashboard revision")
	}
	empty, unknownKind, unknownHash := "  ", "SHORTS", strings.Repeat("1", 64)
	for name, in := range map[string]VideoOverrides{
		"empty title":    {SetTitle: true, Title: &empty},
		"unknown kind":   {SetKind: true, Kind: &unknownKind},
		"unknown file":   {SetThumbnail: true, ThumbnailMediaID: &media, ThumbnailSHA256: &unknownHash},
		"half reference": {SetThumbnail: true, ThumbnailMediaID: &media},
	} {
		if _, err = admin.SetVideoOverrides(ctx, "AAAAAAAAAAA", in, time.Now()); !errors.Is(err, ErrVideoInvalid) {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
	// Clearing restores the platform values; untouched fields stay.
	video, err = admin.SetVideoOverrides(ctx, "AAAAAAAAAAA", VideoOverrides{SetTitle: true, SetThumbnail: true}, time.Now())
	if err != nil || video.Title != "Song cover" || video.ManualKind == nil || stringValue(video.ThumbnailURL) != "/media/"+asset.ID+"?v="+newHash {
		t.Fatalf("video=%#v err=%v", video, err)
	}
	if _, err = admin.SetVideoOverrides(ctx, "missing", VideoOverrides{SetTitle: true, Title: &title}, time.Now()); !errors.Is(err, ErrVideoNotFound) {
		t.Fatalf("missing video err=%v", err)
	}

	// The board and the archive show the corrections; the platform values
	// stay underneath for synchronization.
	if _, err = admin.SetVideoOverrides(ctx, "AAAAAAAAAAA", VideoOverrides{SetTitle: true, Title: &title, SetThumbnail: true, ThumbnailMediaID: &media, ThumbnailSHA256: &oldHash}, time.Now()); err != nil {
		t.Fatal(err)
	}
	pinned := "/media/" + asset.ID + "?v=" + oldHash
	board, err := admin.GetDashboard(ctx, 3, time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC))
	if err != nil || len(board.Videos) != 1 || board.Videos[0].Title != title || stringValue(board.Videos[0].ThumbnailURL) != pinned || stringValue(board.Videos[0].Kind) != "MUSIC" {
		t.Fatalf("board=%#v err=%v", board, err)
	}
	archive, err := admin.GetVideoArchive(ctx, "2026-01-01", "2026-01-31", 1, 20, time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC))
	if err != nil || len(archive.Items) != 1 || archive.Items[0].Title != title || stringValue(archive.Items[0].ThumbnailURL) != pinned {
		t.Fatalf("archive=%#v err=%v", archive, err)
	}
}

func TestPendingMediaRevalidatesAThumbnailAfterADetailCheck(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now()
	at := func(d time.Duration) string { return workflowTime(now.Add(d)) }
	for _, c := range []struct{ id, fileChecked, pageChecked string }{
		{"rechecked", at(-5 * 24 * time.Hour), at(-time.Hour)},
		{"unchanged", at(-time.Hour), at(-5 * 24 * time.Hour)},
		{"neverread", at(-5 * 24 * time.Hour), ""},
	} {
		u := "https://i.ytimg.com/vi/" + c.id + "/hqdefault.jpg"
		testutil.Exec(t, p, `INSERT INTO videos(id,source,title,published_at,url,live_checked_at) VALUES(?,'youtube',?,?,?,NULLIF(?,''))`, c.id, c.id, at(-10*24*time.Hour), "https://www.youtube.com/watch?v="+c.id, c.pageChecked)
		testutil.Exec(t, p, `INSERT INTO media_assets(id,source,source_url,status,sha256,last_checked_at,created_at,updated_at) VALUES(?,'youtube',?,'ready',?,?,?,?)`, sha256Hex(u), u, strings.Repeat("a", 64), c.fileChecked, c.fileChecked, c.fileChecked)
		testutil.Exec(t, p, `INSERT INTO media_links(media_id,owner_type,owner_id,role,position) VALUES(?,'video',?,'thumbnail',0)`, sha256Hex(u), c.id)
	}
	pending, err := s.PendingMedia(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, item := range pending {
		got = append(got, item.SourceURL)
	}
	if !slices.Equal(got, []string{"https://i.ytimg.com/vi/rechecked/hqdefault.jpg"}) {
		t.Fatalf("pending=%v", got)
	}
}

func TestRefreshRequestsNeedAKnownYoutubeVideo(t *testing.T) {
	p := testutil.NewSnapshot(t)
	testutil.Exec(t, p, `INSERT INTO videos(id,source,title,url) VALUES('AAAAAAAAAAA','youtube','video','https://www.youtube.com/watch?v=AAAAAAAAAAA'),('BV1AAAAAAAAA','bilibili','video','https://www.bilibili.com/video/BV1AAAAAAAAA')`)
	s, err := OpenAdminWrites(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	video, err := s.RequestVideoRefresh(ctx, "AAAAAAAAAAA", time.Now())
	if err != nil || video.RefreshRequestedAt == nil {
		t.Fatalf("video=%#v err=%v", video, err)
	}
	if _, err = s.RequestVideoRefresh(ctx, "missing", time.Now()); !errors.Is(err, ErrVideoNotFound) {
		t.Fatalf("missing err=%v", err)
	}
	if _, err = s.RequestVideoRefresh(ctx, "BV1AAAAAAAAA", time.Now()); !errors.Is(err, ErrVideoInvalid) {
		t.Fatalf("bilibili err=%v", err)
	}
}
