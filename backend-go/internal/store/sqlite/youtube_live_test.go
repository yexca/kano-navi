package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	ytsource "github.com/yexca/kano-navi/backend-go/internal/source/youtube"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

const liveChannel = "UCAAAAAAAAAAAAAAAAAAAAAA"

func liveVideo(id, scheduled string, upcoming bool) ytsource.Video {
	return ytsource.Video{ID: id, Title: "synthetic " + id, PublishedAt: "2026-01-01T00:00:00Z", ScheduledAt: scheduled, URL: "https://www.youtube.com/watch?v=" + id, ThumbnailURL: "https://i.ytimg.com/vi/" + id + "/hqdefault.jpg", Kind: "UPCOMING LIVE", IsUpcoming: upcoming}
}

func openLiveStore(t *testing.T) (*Store, string) {
	t.Helper()
	p := testutil.NewSnapshot(t)
	s, err := OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, p
}

func liveText(t *testing.T, s *Store, statement string, args ...any) string {
	t.Helper()
	var value sql.NullString
	if err := s.db.QueryRow(statement, args...).Scan(&value); err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
	if !value.Valid {
		return "<null>"
	}
	return value.String
}

func TestLiveCandidatesAreBoundedActiveVideos(t *testing.T) {
	s, p := openLiveStore(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 3, 12, 0, 0, 0, time.UTC)
	videos := []ytsource.Video{
		liveVideo("AAAAAAAAAAA", "2026-01-03T12:10:00Z", true),  // starts soon
		liveVideo("BBBBBBBBBBB", "2026-01-03T13:00:00Z", true),  // not near yet
		liveVideo("CCCCCCCCCCC", "2026-01-03T10:00:00Z", false), // started, no end
		liveVideo("DDDDDDDDDDD", "2026-01-02T10:00:00Z", false), // beyond the lookback
		liveVideo("EEEEEEEEEEE", "2026-01-03T09:00:00Z", false), // event ended
		liveVideo("FFFFFFFFFFF", "2026-01-03T08:00:00Z", false), // page reported the end
		liveVideo("GGGGGGGGGGG", "2026-01-03T11:00:00Z", false), // cancelled by the operator
		liveVideo("HHHHHHHHHHH", "2026-01-03T11:30:00Z", false), // waiting past its start
		{ID: "IIIIIIIIIII", Title: "ordinary upload", PublishedAt: "2026-01-03T11:00:00Z", URL: "https://www.youtube.com/watch?v=IIIIIIIIIII", Kind: "VIDEO"},
	}
	videos[4].EndedAt = "2026-01-03T11:00:00Z"
	videos[5].LiveStatus = ytsource.LiveStatusEnded
	if err := s.WriteYoutubeRecent(ctx, liveChannel, ytsource.Result{Videos: videos, NextCursor: "AAAAAAAAAAA", NextCursorTime: "2026-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, p, `UPDATE events SET cancellation_status='manual_confirmed' WHERE source_item_id='GGGGGGGGGGG'`)
	// Never-checked videos go first, then the least recently checked.
	testutil.Exec(t, p, `UPDATE videos SET live_checked_at=NULL WHERE id='HHHHHHHHHHH'`)
	testutil.Exec(t, p, `UPDATE videos SET live_checked_at='2026-01-03T11:00:00.000Z' WHERE id='CCCCCCCCCCC'`)
	got, err := s.YoutubeLiveCandidates(ctx, now, ytsource.MaxLiveProbes)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, v := range got {
		ids = append(ids, v.ID)
	}
	if strings.Join(ids, ",") != "HHHHHHHHHHH,CCCCCCCCCCC,AAAAAAAAAAA" {
		t.Fatalf("candidates=%v", ids)
	}
	if limited, _ := s.YoutubeLiveCandidates(ctx, now, 1); len(limited) != 1 || limited[0].ID != "HHHHHHHHHHH" {
		t.Fatalf("limited=%v", limited)
	}
	if got[1].PublishedAt != "2026-01-01T00:00:00Z" || got[1].ThumbnailURL == "" {
		t.Fatalf("candidate lost stored fields: %#v", got[1])
	}
}

func TestLiveProbeWritesStateKeepsManualDataAndBumpsOnlyOnChange(t *testing.T) {
	s, p := openLiveStore(t)
	ctx := context.Background()
	stored := []ytsource.Video{liveVideo("AAAAAAAAAAA", "2026-01-03T12:00:00Z", true), liveVideo("BBBBBBBBBBB", "2026-01-03T12:00:00Z", true), liveVideo("CCCCCCCCCCC", "2026-01-03T12:00:00Z", true)}
	if err := s.WriteYoutubeRecent(ctx, liveChannel, ytsource.Result{Videos: stored, NextCursor: "AAAAAAAAAAA", NextCursorTime: "2026-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, p, `UPDATE events SET title='manual title',manual_locked=1,provenance='manual' WHERE source_item_id='BBBBBBBBBBB'`)
	testutil.Exec(t, p, `UPDATE events SET deleted_at='2026-01-02T00:00:00.000Z' WHERE source_item_id='CCCCCCCCCCC'`)
	revision, _ := s.GetDashboardRevision(ctx)

	live := func(id string) ytsource.Video {
		v := liveVideo(id, "2026-01-03T12:04:30Z", false)
		v.Kind, v.LiveNow, v.LiveStatus = "LIVE ARCHIVE", true, ytsource.LiveStatusLive
		return v
	}
	unknown := liveVideo("ZZZZZZZZZZZ", "2026-01-03T12:00:00Z", false)
	probe := ytsource.ProbeResult{Videos: []ytsource.Video{live("AAAAAAAAAAA"), live("BBBBBBBBBBB"), live("CCCCCCCCCCC"), unknown}}
	changed, err := s.WriteYoutubeLive(ctx, probe)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	after, _ := s.GetDashboardRevision(ctx)
	if after != revision+1 {
		t.Fatalf("revision %d -> %d", revision, after)
	}
	// The actual start, the reservation state and the status text that says live.
	if got := liveText(t, s, `SELECT starts_at||'|'||is_upcoming||'|'||status FROM events WHERE id='youtube-AAAAAAAAAAA'`); got != "2026-01-03T12:04:30Z|0|live" {
		t.Fatalf("event=%s", got)
	}
	if got := liveText(t, s, `SELECT scheduled_at||'|'||is_upcoming||'|'||live_status FROM videos WHERE id='AAAAAAAAAAA'`); got != "2026-01-03T12:04:30Z|0|live" {
		t.Fatalf("video=%s", got)
	}
	if got := liveText(t, s, `SELECT title||'|'||status FROM events WHERE source_item_id='BBBBBBBBBBB'`); got != "manual title|scheduled" {
		t.Fatalf("locked event changed: %s", got)
	}
	if got := liveText(t, s, `SELECT deleted_at||'|'||status FROM events WHERE source_item_id='CCCCCCCCCCC'`); got != "2026-01-02T00:00:00.000Z|scheduled" {
		t.Fatalf("tombstone changed: %s", got)
	}
	if got := liveText(t, s, `SELECT COUNT(*) FROM videos WHERE id='ZZZZZZZZZZZ'`); got != "0" {
		t.Fatal("a probe added an unknown video")
	}

	// The same answer again changes nothing public and keeps the revision.
	changed, err = s.WriteYoutubeLive(ctx, probe)
	if err != nil || changed {
		t.Fatalf("repeat changed=%v err=%v", changed, err)
	}
	if again, _ := s.GetDashboardRevision(ctx); again != after {
		t.Fatalf("unchanged probe bumped revision %d -> %d", after, again)
	}

	// The end arrives: ends_at, the ended status, and the board revision.
	ended := live("AAAAAAAAAAA")
	ended.LiveNow, ended.LiveStatus, ended.EndedAt = false, ytsource.LiveStatusEnded, "2026-01-03T14:00:00Z"
	changed, err = s.WriteYoutubeLive(ctx, ytsource.ProbeResult{Videos: []ytsource.Video{ended}})
	if err != nil || !changed {
		t.Fatalf("end changed=%v err=%v", changed, err)
	}
	if got := liveText(t, s, `SELECT ends_at||'|'||status FROM events WHERE id='youtube-AAAAAAAAAAA'`); got != "2026-01-03T14:00:00Z|ended" {
		t.Fatalf("ended event=%s", got)
	}
	// The probe never moves the regular feed cursor or detail queue.
	if got := liveText(t, s, `SELECT cursor_id FROM sync_state WHERE source='youtube'`); got != "AAAAAAAAAAA" {
		t.Fatalf("cursor=%s", got)
	}
}

func TestRegularSyncNoLongerCompletesALiveStream(t *testing.T) {
	s, _ := openLiveStore(t)
	ctx := context.Background()
	v := liveVideo("AAAAAAAAAAA", "2026-01-03T12:04:30Z", false)
	v.LiveNow, v.LiveStatus = true, ytsource.LiveStatusLive
	member := liveVideo("BBBBBBBBBBB", "2026-01-03T12:00:00Z", false)
	member.Title, member.LiveNow, member.LiveStatus = "メンバー限定 synthetic", true, ytsource.LiveStatusLive
	if err := s.WriteYoutubeRecent(ctx, liveChannel, ytsource.Result{Videos: []ytsource.Video{v, member}, NextCursor: "AAAAAAAAAAA", NextCursorTime: "2026-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if got := liveText(t, s, `SELECT group_concat(event_type||':'||status,',') FROM (SELECT event_type,status FROM events ORDER BY id)`); got != "stream:live,member:live" {
		t.Fatalf("statuses=%s", got)
	}
}

func TestLiveSettingsDefaultAndValidate(t *testing.T) {
	s, _ := openLiveStore(t)
	ctx := context.Background()
	settings, err := s.LiveProbeSettings(ctx)
	if err != nil || !settings.Enabled || settings.IntervalMinutes != LiveProbeDefaultMinutes || settings.Freshness() != time.Hour {
		t.Fatalf("defaults=%#v err=%v", settings, err)
	}
	s.writable = true
	for _, minutes := range []int{LiveProbeMinMinutes - 1, LiveProbeMaxMinutes + 1} {
		if err = s.SaveLiveProbeSettings(ctx, LiveProbeSettings{Enabled: true, IntervalMinutes: minutes}); err == nil {
			t.Fatalf("accepted %d minutes", minutes)
		}
	}
	if err = s.SaveLiveProbeSettings(ctx, LiveProbeSettings{Enabled: false, IntervalMinutes: 10}); err != nil {
		t.Fatal(err)
	}
	if settings, _ = s.LiveProbeSettings(ctx); settings.Enabled || settings.IntervalMinutes != 10 {
		t.Fatalf("saved=%#v", settings)
	}
}

func TestDashboardTrustsOnlyFreshLiveState(t *testing.T) {
	s, p := openLiveStore(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 3, 18, 0, 0, 0, time.UTC)
	// A long stream that started five hours ago, beyond the start-time grace.
	long := liveVideo("AAAAAAAAAAA", "2026-01-03T13:00:00Z", false)
	long.LiveNow, long.LiveStatus = true, ytsource.LiveStatusLive
	stale := liveVideo("BBBBBBBBBBB", "2026-01-03T17:30:00Z", false)
	stale.LiveNow, stale.LiveStatus = true, ytsource.LiveStatusLive
	over := liveVideo("CCCCCCCCCCC", "2026-01-03T17:00:00Z", false)
	over.LiveStatus = ytsource.LiveStatusEnded
	if err := s.WriteYoutubeRecent(ctx, liveChannel, ytsource.Result{Videos: []ytsource.Video{long, stale, over}, NextCursor: "AAAAAAAAAAA", NextCursorTime: "2026-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, p, `UPDATE videos SET live_checked_at='2026-01-03T17:40:00.000Z' WHERE id='AAAAAAAAAAA'`)
	// Older than two default intervals: the board falls back to its guess.
	testutil.Exec(t, p, `UPDATE videos SET live_checked_at='2026-01-03T16:40:00.000Z' WHERE id IN ('BBBBBBBBBBB','CCCCCCCCCCC')`)
	dashboard, err := s.GetDashboard(ctx, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	status := func(id string) string {
		for _, v := range dashboard.Videos {
			if v.ID == id {
				if v.LiveStatus == nil {
					return "<nil>"
				}
				return *v.LiveStatus
			}
		}
		return "<absent>"
	}
	if status("AAAAAAAAAAA") != "live" || status("BBBBBBBBBBB") != "<nil>" || status("CCCCCCCCCCC") != "ended" {
		t.Fatalf("live=%s stale=%s over=%s", status("AAAAAAAAAAA"), status("BBBBBBBBBBB"), status("CCCCCCCCCCC"))
	}
	var event *DashboardEvent
	for i := range dashboard.Events {
		if dashboard.Events[i].ID == "youtube-AAAAAAAAAAA" {
			event = &dashboard.Events[i]
		}
	}
	if event == nil || event.LiveStatus == nil || *event.LiveStatus != "live" {
		t.Fatalf("linked event=%#v", event)
	}
}
