package sqlite

import (
	"context"
	"database/sql"
	"testing"

	ytsource "github.com/yexca/kano-navi/backend-go/internal/source/youtube"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

func TestYoutubeWritesPreserveLocksTombstonesAndCursorOnRollback(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	channel := "UCAAAAAAAAAAAAAAAAAAAAAA"
	result := ytsource.Result{Videos: []ytsource.Video{{ID: "AAAAAAAAAAA", Title: "reservation", PublishedAt: "2026-01-01T00:00:00Z", ScheduledAt: "2026-01-02T17:00:00Z", URL: "https://www.youtube.com/watch?v=AAAAAAAAAAA", ThumbnailURL: "https://i.ytimg.com/vi/AAAAAAAAAAA/hqdefault.jpg", Kind: "UPCOMING LIVE", IsUpcoming: true}}, NextCursor: "AAAAAAAAAAA", NextCursorTime: "2026-01-01T00:00:00Z", Queue: []string{"AAAAAAAAAAA"}}
	if err = s.WriteYoutubeRecent(ctx, channel, result); err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, p, `UPDATE events SET title='manual',manual_locked=1,provenance='manual',deleted_at='2026-01-01T01:00:00Z',cancellation_status='manual_confirmed',cancellation_reason='operator' WHERE id='youtube-AAAAAAAAAAA'`)
	result.Videos[0].Title = "changed automatically"
	result.Videos[0].ScheduledAt = "2026-01-04T00:00:00Z"
	if err = s.WriteYoutubeRecent(ctx, channel, result); err != nil {
		t.Fatal(err)
	}
	db, _ := sql.Open("sqlite", p)
	defer db.Close()
	var title, date, deleted, cancel string
	if err = db.QueryRow(`SELECT title,starts_on,deleted_at,cancellation_status FROM events`).Scan(&title, &date, &deleted, &cancel); err != nil {
		t.Fatal(err)
	}
	if title != "manual" || date != "2026-01-03" || deleted == "" || cancel != "manual_confirmed" {
		t.Fatalf("manual fields changed %s %s %s %s", title, date, deleted, cancel)
	}
	var links, evidence int
	_ = db.QueryRow(`SELECT COUNT(*) FROM media_links`).Scan(&links)
	_ = db.QueryRow(`SELECT COUNT(*) FROM event_sources`).Scan(&evidence)
	if links != 1 || evidence != 1 {
		t.Fatalf("duplicate identities links=%d evidence=%d", links, evidence)
	}
	feed := result
	feed.Videos = append([]ytsource.Video{}, result.Videos...)
	feed.Videos[0].FeedOnly = true
	feed.Videos[0].Title = "RSS cannot replace checked details"
	feed.Videos[0].IsUpcoming = false
	if err = s.WriteYoutubeRecent(ctx, channel, feed); err != nil {
		t.Fatal(err)
	}
	var retainedTitle string
	_ = db.QueryRow(`SELECT title FROM videos`).Scan(&retainedTitle)
	if retainedTitle != "changed automatically" {
		t.Fatal("RSS replaced existing checked snapshot")
	}
	revision, _ := s.GetDashboardRevision(ctx)
	testutil.Exec(t, p, `CREATE TRIGGER reject_sync BEFORE UPDATE ON sync_state BEGIN SELECT RAISE(ABORT,'fixture'); END`)
	result.Videos[0].Title = "must roll back"
	result.NextCursor = "BBBBBBBBBBB"
	result.NextCursorTime = "2026-01-05T00:00:00Z"
	if err = s.WriteYoutubeRecent(ctx, channel, result); err == nil {
		t.Fatal("expected transaction rollback")
	}
	var video, cursor string
	_ = db.QueryRow(`SELECT title FROM videos`).Scan(&video)
	_ = db.QueryRow(`SELECT cursor_id FROM sync_state WHERE source='youtube'`).Scan(&cursor)
	if video == "must roll back" || cursor != "AAAAAAAAAAA" {
		t.Fatalf("partial write title=%s cursor=%s", video, cursor)
	}
	after, _ := s.GetDashboardRevision(ctx)
	if after != revision {
		t.Fatal("rolled back revision changed")
	}
}

func TestYoutubeFeedFormatSurvivesWatchReadsAndLeavesOthersUnknown(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	channel := "UCAAAAAAAAAAAAAAAAAAAAAA"
	// BBBBBBBBBBB is stored before the feed names its format; CCCCCCCCCCC is
	// an older row the feed no longer lists, short enough to look like a Short.
	testutil.Exec(t, p, `INSERT INTO videos(id,source,title,published_at,url,kind) VALUES('BBBBBBBBBBB','youtube','stored','2026-01-01T00:00:00Z','https://www.youtube.com/watch?v=BBBBBBBBBBB','VIDEO');
		INSERT INTO videos(id,source,title,published_at,url,kind,duration_seconds) VALUES('CCCCCCCCCCC','youtube','older','2025-12-01T00:00:00Z','https://www.youtube.com/watch?v=CCCCCCCCCCC','VIDEO',30)`)
	short := ytsource.Video{ID: "AAAAAAAAAAA", Title: "new short", PublishedAt: "2026-01-02T00:00:00Z", URL: "https://www.youtube.com/watch?v=AAAAAAAAAAA", ThumbnailURL: "https://i.ytimg.com/vi/AAAAAAAAAAA/hqdefault.jpg", Kind: "VIDEO", FeedOnly: true}
	verdicts := map[string]bool{"AAAAAAAAAAA": true, "BBBBBBBBBBB": false, "ZZZZZZZZZZZ": true}
	result := ytsource.Result{Videos: []ytsource.Video{short}, Shorts: verdicts, NextCursor: "AAAAAAAAAAA", NextCursorTime: "2026-01-02T00:00:00Z"}
	formats := func() string {
		t.Helper()
		var out string
		if err := s.DB().QueryRow(`SELECT group_concat(id||'='||COALESCE(is_short,'unknown'),',') FROM (SELECT id,is_short FROM videos ORDER BY id)`).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	revision, _ := s.GetDashboardRevision(ctx)
	if err = s.WriteYoutubeRecent(ctx, channel, result); err != nil {
		t.Fatal(err)
	}
	// A verdict for a video the snapshot does not hold creates no row.
	if got := formats(); got != "AAAAAAAAAAA=1,BBBBBBBBBBB=0,CCCCCCCCCCC=unknown" {
		t.Fatalf("formats=%s", got)
	}
	if after, _ := s.GetDashboardRevision(ctx); after == revision {
		t.Fatal("a new format did not advance the revision")
	}
	// A watch-page read names no format and keeps the feed's.
	checked := short
	checked.FeedOnly, checked.Title, checked.DurationSeconds = false, "checked short", 31
	if err = s.WriteYoutubeRecent(ctx, channel, ytsource.Result{Videos: []ytsource.Video{checked}, NextCursor: "AAAAAAAAAAA", NextCursorTime: "2026-01-02T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if got := formats(); got != "AAAAAAAAAAA=1,BBBBBBBBBBB=0,CCCCCCCCCCC=unknown" {
		t.Fatalf("formats after watch read=%s", got)
	}
	// The same verdicts again change nothing the board shows.
	revision, _ = s.GetDashboardRevision(ctx)
	if err = s.WriteYoutubeRecent(ctx, channel, ytsource.Result{Shorts: verdicts, NextCursor: "AAAAAAAAAAA", NextCursorTime: "2026-01-02T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if after, _ := s.GetDashboardRevision(ctx); after != revision {
		t.Fatal("unchanged formats advanced the revision")
	}
}

func TestYoutubePremiereIsAReleaseThatKeepsItsReportedEnd(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	premiere := ytsource.Video{ID: "EEEEEEEEEEE", Title: "premiere", PublishedAt: "2026-01-02T03:00:00Z", ScheduledAt: "2026-01-02T09:00:09Z", EndedAt: "2026-01-02T09:06:55Z", URL: "https://www.youtube.com/watch?v=EEEEEEEEEEE", Kind: "MUSIC", Premiere: true}
	live := ytsource.Video{ID: "DDDDDDDDDDD", Title: "waiting room", PublishedAt: "2026-01-02T17:00:00Z", ScheduledAt: "2026-01-03T12:00:00Z", URL: "https://www.youtube.com/watch?v=DDDDDDDDDDD", Kind: "UPCOMING LIVE", IsUpcoming: true}
	write := func(videos ...ytsource.Video) {
		t.Helper()
		if err := s.WriteYoutubeRecent(ctx, "UCAAAAAAAAAAAAAAAAAAAAAA", ytsource.Result{Videos: videos, NextCursor: "DDDDDDDDDDD", NextCursorTime: "2026-01-02T17:00:00Z"}); err != nil {
			t.Fatal(err)
		}
	}
	write(premiere, live)
	// A later check that cannot read the end keeps the one already reported.
	premiere.EndedAt = ""
	write(premiere)
	db, _ := sql.Open("sqlite", p)
	defer db.Close()
	for id, want := range map[string][2]string{"youtube-EEEEEEEEEEE": {"release", "2026-01-02T09:06:55Z"}, "youtube-DDDDDDDDDDD": {"stream", ""}} {
		var eventType string
		var endsAt sql.NullString
		if err = db.QueryRow(`SELECT event_type,ends_at FROM events WHERE id=?`, id).Scan(&eventType, &endsAt); err != nil {
			t.Fatal(err)
		}
		if eventType != want[0] || endsAt.String != want[1] {
			t.Fatalf("%s type=%s ends=%v", id, eventType, endsAt)
		}
	}
}

func TestPendingMediaPrefersNewestOwnerOverSameSyncTie(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	// One sync registers every thumbnail with the same created_at, oldest first.
	ids := []string{"AAAAAAAAAAA", "BBBBBBBBBBB", "CCCCCCCCCCC", "DDDDDDDDDDD", "EEEEEEEEEEE"}
	result := ytsource.Result{NextCursor: ids[len(ids)-1], NextCursorTime: "2026-01-05T00:00:00Z"}
	thumbs := map[string]string{}
	for i, id := range ids {
		thumb := "https://i.ytimg.com/vi/" + id + "/hqdefault.jpg"
		media, _, err := mediaIdentity(thumb)
		if err != nil {
			t.Fatal(err)
		}
		thumbs[id] = media
		result.Videos = append(result.Videos, ytsource.Video{ID: id, Title: "video " + id, PublishedAt: "2026-01-0" + string(rune('1'+i)) + "T00:00:00Z", URL: "https://www.youtube.com/watch?v=" + id, ThumbnailURL: thumb, Kind: "VIDEO"})
	}
	if err = s.WriteYoutubeRecent(ctx, "UCAAAAAAAAAAAAAAAAAAAAAA", result); err != nil {
		t.Fatal(err)
	}
	pending, err := s.PendingMedia(ctx, 2)
	if err != nil || len(pending) != 2 || pending[0].ID != thumbs["EEEEEEEEEEE"] || pending[1].ID != thumbs["DDDDDDDDDDD"] {
		t.Fatalf("newest thumbnails not first pending=%#v err=%v", pending, err)
	}
	// A previously checked asset stays behind never-checked ones even when its
	// owner is the newest video.
	testutil.Exec(t, p, `UPDATE media_assets SET last_checked_at='2026-01-06T00:00:00Z' WHERE id=?`, thumbs["EEEEEEEEEEE"])
	pending, err = s.PendingMedia(ctx, len(ids))
	if err != nil || len(pending) != len(ids) {
		t.Fatalf("pending=%#v err=%v", pending, err)
	}
	for i, id := range []string{"DDDDDDDDDDD", "CCCCCCCCCCC", "BBBBBBBBBBB", "AAAAAAAAAAA", "EEEEEEEEEEE"} {
		if pending[i].ID != thumbs[id] {
			t.Fatalf("position %d is %s, want thumbnail of %s", i, pending[i].ID, id)
		}
	}
}
