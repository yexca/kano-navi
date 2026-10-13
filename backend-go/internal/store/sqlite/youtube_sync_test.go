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
	revision, _ := s.GetDashboardRevision(ctx)
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
	revision, _ = s.GetDashboardRevision(ctx)
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
