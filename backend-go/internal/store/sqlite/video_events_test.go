package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// video inserts a synthetic YouTube video. scheduled is the broadcast start,
// empty for an upload; premiere is "NULL", "0" or "1".
func (f *mergeFixture) video(id, title, published, scheduled, liveStatus, premiere string, duration int) {
	f.t.Helper()
	f.exec(`INSERT INTO videos(id,source,title,published_at,scheduled_at,url,kind,is_upcoming,live_status,duration_seconds,is_premiere,raw_json)
		VALUES(?,'youtube',?,?,NULLIF(?,''),?,'VIDEO',?,NULLIF(?,''),?,`+premiere+`,'{}')`,
		id, title, published, scheduled, "https://www.youtube.com/watch?v="+id, liveStatus == "upcoming", liveStatus, duration)
}

// linked is the option list of an event an X post wrote for a YouTube link.
func linked(videoID string, options ...string) []string {
	return append([]string{"source_item_id='" + videoID + "'", "source_key='reservation'"}, options...)
}

// start is "date instant precision" of a stored event.
func (f *mergeFixture) start(id string) string {
	f.t.Helper()
	var day, precision string
	var at sql.NullString
	if err := f.s.DB().QueryRow(`SELECT starts_on,starts_at,time_precision FROM events WHERE id=?`, id).Scan(&day, &at, &precision); err != nil {
		f.t.Fatal(err)
	}
	return day + " " + at.String + " " + precision
}

func TestReconcileTimesLinkedShortAndMergesItsReleaseNotice(t *testing.T) {
	f := newMergeFixture(t)
	// An X post linked the Short, so its event carries only the post's date.
	f.video("SHORT000001", "高嶺の花子さん 歌ってみた", "2026-10-23T09:00:19.000Z", "", "", "NULL", 33)
	f.add("youtube-SHORT000001", "youtube", "高嶺の花子さん short cover ver.", "2026-10-23", "", "release", linked("SHORT000001")...)
	f.add("x-short", "x", "short動画投稿", "2026-10-23", "18:00", "release")
	counts := f.reconcile()
	if got := f.start("youtube-SHORT000001"); got != "2026-10-23 2026-10-23T09:00:19.000Z exact" {
		t.Fatalf("short start=%s", got)
	}
	f.expect(map[string]string{"x-short": "youtube-SHORT000001|rule|rule_b:youtube_start", "youtube-SHORT000001": unmerged})
	if counts.EventsTimed != 1 || counts.EventsCreated != 0 || counts.Merged != 1 {
		t.Fatalf("counts=%+v", counts)
	}
	if counts = f.reconcile(); counts.Changed() || counts.Kept != 1 {
		t.Fatalf("repeat=%+v", counts)
	}
}

func TestReconcileTimesVideoEventsFromTheirVideo(t *testing.T) {
	f := newMergeFixture(t)
	// An X post rewrote the reservation with its own announced time.
	f.video("STREAM00001", "歌枠リレー", "2026-10-20T09:00:00Z", "2026-10-20T10:59:16Z", "ended", "0", 1800)
	f.add("youtube-STREAM00001", "youtube", "歌枠リレー", "2026-10-20", "17:00", "stream", linked("STREAM00001", "ends_at='2026-10-20T09:30:00.000Z'")...)
	// Operator decisions and tombstones keep their fields.
	f.video("LOCKED00001", "MV", "2026-10-22T09:00:00Z", "", "", "NULL", 200)
	f.add("youtube-LOCKED00001", "youtube", "MV", "2026-10-22", "", "release", linked("LOCKED00001", "manual_locked=1", "provenance='manual'")...)
	f.video("DELETED0001", "MV", "2026-10-22T10:00:00Z", "", "", "NULL", 200)
	f.add("youtube-DELETED0001", "youtube", "MV", "2026-10-22", "", "release", linked("DELETED0001", "deleted_at='2026-10-22T12:00:00.000Z'")...)
	// Only the bounded window is settled.
	f.video("OLDVIDEO001", "MV", "2026-08-01T09:00:00Z", "", "", "NULL", 200)
	f.add("youtube-OLDVIDEO001", "youtube", "MV", "2026-08-01", "", "release", linked("OLDVIDEO001")...)
	if counts := f.reconcile(); counts.EventsTimed != 1 {
		t.Fatalf("counts=%+v", counts)
	}
	var ends sql.NullString
	if err := f.s.DB().QueryRow(`SELECT ends_at FROM events WHERE id='youtube-STREAM00001'`).Scan(&ends); err != nil || ends.Valid {
		t.Fatalf("end before the video's start kept: %v %v", ends, err)
	}
	for id, want := range map[string]string{
		"youtube-STREAM00001": "2026-10-20 2026-10-20T10:59:16Z exact",
		"youtube-LOCKED00001": "2026-10-22  unknown",
		"youtube-DELETED0001": "2026-10-22  unknown",
		"youtube-OLDVIDEO001": "2026-08-01  unknown",
	} {
		if got := f.start(id); got != want {
			t.Errorf("%s=%q, want %q", id, got, want)
		}
	}
}

func TestReconcileMergesUnnamedXNoticeIntoTheStream(t *testing.T) {
	f := newMergeFixture(t)
	f.add("x-notice", "x", "このあと20時からはじまります！", "2026-10-20", "20:00", "event")
	f.add("youtube-relay", "youtube", "【歌枠】歌で繋がる秋の夜！歌います！", "2026-10-20", "19:59:16", "stream")
	f.add("x-weekly", "x", "カスタム・歌枠リレー・コラボ配信", "2026-10-20", "", "stream")
	f.reconcile()
	f.expect(map[string]string{
		"x-notice": "youtube-relay|rule|rule_b:youtube_start",
		"x-weekly": "youtube-relay|rule|rule_a:title",
	})
}

func TestReconcileRuleBNoticeTypes(t *testing.T) {
	type target struct{ clock, kind string }
	for _, tt := range []struct {
		name, kind, precision, clock string
		targets                      []target
		merged                       bool
		ambiguous                    int
	}{
		{name: "release into release", kind: "release", precision: "exact", clock: "18:00", targets: []target{{"18:00:19", "release"}}, merged: true},
		{name: "approximate release", kind: "release", precision: "approximate", clock: "18:00", targets: []target{{"18:15", "release"}}, merged: true},
		{name: "release into stream", kind: "release", precision: "exact", clock: "18:00", targets: []target{{"18:00", "stream"}}},
		{name: "stream into release", kind: "stream", precision: "exact", clock: "18:00", targets: []target{{"18:00", "release"}}},
		{name: "two releases", kind: "release", precision: "exact", clock: "18:00", targets: []target{{"18:00", "release"}, {"18:20", "release"}}, ambiguous: 1},
		{name: "release thirty-one minutes", kind: "release", precision: "exact", clock: "18:31", targets: []target{{"18:00", "release"}}},
		{name: "unnamed into stream", kind: "event", precision: "exact", clock: "20:00", targets: []target{{"19:59:16", "stream"}}, merged: true},
		{name: "unnamed into members-only", kind: "event", precision: "exact", clock: "20:00", targets: []target{{"20:05", "member"}}, merged: true},
		{name: "unnamed approximate", kind: "event", precision: "approximate", clock: "20:00", targets: []target{{"20:00", "stream"}}},
		{name: "unnamed into release", kind: "event", precision: "exact", clock: "20:00", targets: []target{{"20:00", "release"}}},
		{name: "unnamed two streams", kind: "event", precision: "exact", clock: "20:00", targets: []target{{"19:50", "stream"}, {"20:10", "stream"}}, ambiguous: 1},
		{name: "unnamed thirty-one minutes", kind: "event", precision: "exact", clock: "20:31", targets: []target{{"20:00", "stream"}}},
		{name: "appearance", kind: "appearance", precision: "exact", clock: "20:00", targets: []target{{"20:00", "stream"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newMergeFixture(t)
			f.add("x1", "x", "告知", "2026-10-23", tt.clock, tt.kind, "time_precision='"+tt.precision+"'")
			for i, target := range tt.targets {
				f.add(fmt.Sprintf("y%d", i+1), "youtube", "動画", "2026-10-23", target.clock, target.kind)
			}
			counts := f.reconcile()
			state := f.state("x1")
			merged := strings.HasSuffix(state, "|rule|rule_b:youtube_start")
			if merged != tt.merged || !tt.merged && state != unmerged || counts.Ambiguous != tt.ambiguous {
				t.Fatalf("state=%s counts=%+v", state, counts)
			}
		})
	}
}

func TestRuleANeverMergesYoutubeIntoX(t *testing.T) {
	f := newMergeFixture(t)
	// A YouTube event an X post wrote without a stored video stays date-only.
	f.add("youtube-NOVIDEO0001", "youtube", "歌枠", "2026-10-25", "", "stream", linked("NOVIDEO0001")...)
	f.add("x1", "x", "歌枠", "2026-10-25", "20:00", "stream")
	f.add("youtube-NOVIDEO0002", "youtube", "雑談", "2026-10-26", "", "stream", linked("NOVIDEO0002")...)
	f.add("x2", "x", "ゲーム", "2026-10-26", "20:00", "stream")
	counts := f.reconcile()
	f.expect(map[string]string{"youtube-NOVIDEO0001": unmerged, "x1": unmerged, "youtube-NOVIDEO0002": unmerged, "x2": unmerged})
	if counts.Ambiguous != 0 || counts.Merged != 0 {
		t.Fatalf("counts=%+v", counts)
	}
	// Another YouTube event is still a target.
	f.add("youtube-y3", "youtube", "【歌枠】秋の夜", "2026-10-25", "21:00", "stream")
	f.reconcile()
	f.expect(map[string]string{"youtube-NOVIDEO0001": "youtube-y3|rule|rule_a:title"})
}

func TestReconcileCreatesEventsForEndedBroadcasts(t *testing.T) {
	f := newMergeFixture(t)
	ctx := context.Background()
	// An unreadable watch page left these broadcasts without a verdict.
	f.video("COLLAB00001", "【VALORANT】てとちゃん&レイちゃんとコンペする【メンバー限定】", "2026-10-09T13:46:37Z", "2026-10-09T10:01:35Z", "ended", "NULL", 12690)
	f.video("SHORTLIVE01", "告知", "2026-10-09T09:00:00Z", "2026-10-09T09:00:00Z", "ended", "NULL", 240)
	// A known premiere verdict creates a release.
	f.video("PREMIERE001", "MV", "2026-10-10T09:00:00Z", "2026-10-10T09:00:00Z", "ended", "1", 240)
	// Upcoming, uploads, tombstones and existing evidence create nothing.
	f.video("UPCOMING001", "歌枠", "2026-10-01T09:00:00Z", "2026-10-30T11:00:00Z", "upcoming", "0", 0)
	f.video("UPLOAD00001", "MV", "2026-10-11T09:00:00Z", "", "", "NULL", 3600)
	f.video("DELETED0001", "雑談", "2026-10-12T10:00:00Z", "2026-10-12T10:00:00Z", "ended", "0", 7200)
	f.add("youtube-DELETED0001", "youtube", "雑談", "2026-10-12", "19:00", "stream", linked("DELETED0001", "deleted_at='2026-10-12T12:00:00.000Z'", "manual_locked=1")...)
	f.video("EVIDENCE001", "雑談", "2026-10-13T10:00:00Z", "2026-10-13T10:00:00Z", "ended", "0", 7200)
	f.exec(`INSERT INTO events(id,source,title,starts_on,time_precision,event_type,provenance,manual_locked,created_at,updated_at) VALUES('legacy-id','youtube','雑談','2026-10-13','unknown','stream','automatic',0,'2026-10-01T00:00:00.000Z','2026-10-01T00:00:00.000Z')`)
	f.exec(`INSERT INTO event_sources(event_id,source,source_item_id,source_key,created_at,updated_at) VALUES('legacy-id','youtube','EVIDENCE001','reservation','2026-10-01T00:00:00.000Z','2026-10-01T00:00:00.000Z')`)
	// The weekly plan's two entries for that day.
	f.add("x-collab", "x", "コラボ配信", "2026-10-09", "", "stream")
	f.add("x-chat", "x", "夜に雑談配信をちょびっと", "2026-10-09", "", "stream")
	before, _ := f.s.GetDashboardRevision(ctx)
	counts := f.reconcile()
	if counts.EventsCreated != 2 {
		t.Fatalf("counts=%+v", counts)
	}
	var created string
	if err := f.s.DB().QueryRow(`SELECT group_concat(id||' '||event_type||' '||starts_on||' '||starts_at||' '||status||' '||detail,'; ') FROM (SELECT * FROM events WHERE source='youtube' AND deleted_at IS NULL AND id<>'legacy-id' ORDER BY id)`).Scan(&created); err != nil {
		t.Fatal(err)
	}
	if created != "youtube-COLLAB00001 member 2026-10-09 2026-10-09T10:01:35Z ended YouTube Live; youtube-PREMIERE001 release 2026-10-10 2026-10-10T09:00:00Z ended YouTube Premiere" {
		t.Fatalf("created=%s", created)
	}
	var evidence int
	if err := f.s.DB().QueryRow(`SELECT COUNT(*) FROM event_sources WHERE source='youtube' AND source_key='reservation' AND source_item_id IN ('COLLAB00001','PREMIERE001')`).Scan(&evidence); err != nil || evidence != 2 {
		t.Fatalf("evidence=%d %v", evidence, err)
	}
	if after, _ := f.s.GetDashboardRevision(ctx); after != before+1 {
		t.Fatalf("revision %d -> %d", before, after)
	}
	// Two weekly entries for one stream without a shared title stay for the
	// operator, whose merge the reconciler keeps. The remaining entry is then
	// the day's only date-only one.
	f.expect(map[string]string{"x-collab": unmerged, "x-chat": unmerged})
	if counts.Ambiguous != 2 {
		t.Fatalf("counts=%+v", counts)
	}
	if _, err := f.s.MergeEvent(ctx, "x-collab", "youtube-COLLAB00001"); err != nil {
		t.Fatal(err)
	}
	if counts = f.reconcile(); counts.Merged != 1 || counts.EventsCreated != 0 {
		t.Fatalf("repeat=%+v", counts)
	}
	f.expect(map[string]string{
		"x-collab": "youtube-COLLAB00001|manual|manual",
		"x-chat":   "youtube-COLLAB00001|rule|rule_a:only_stream",
	})
}

func TestManualMergeIsDurable(t *testing.T) {
	f := newMergeFixture(t)
	ctx := context.Background()
	f.add("y1", "youtube", "【雑談】ただいま", "2026-10-08", "22:02:28", "stream")
	f.add("y2", "youtube", "【VALORANT】コンペ", "2026-10-09", "19:01:35", "stream")
	f.add("x-chat", "x", "夜に雑談配信をちょびっと", "2026-10-09", "", "stream")
	f.add("x-notice", "x", "コンペ", "2026-10-09", "19:00", "stream")
	f.reconcile()
	f.expect(map[string]string{"x-notice": "y2|rule|rule_b:youtube_start"})
	before, _ := f.s.GetDashboardRevision(ctx)
	// Merging across dates is the operator's call.
	event, err := f.s.MergeEvent(ctx, "x-chat", " y1 ")
	if err != nil || event.MergedInto == nil || *event.MergedInto != "y1" || *event.MergedIntoTitle != "【雑談】ただいま" {
		t.Fatalf("merge=%+v %v", event, err)
	}
	if after, _ := f.s.GetDashboardRevision(ctx); after != before+1 {
		t.Fatalf("revision %d -> %d", before, after)
	}
	if _, err = f.s.MergeEvent(ctx, "x-chat", "y1"); err != nil {
		t.Fatal(err)
	}
	if again, _ := f.s.GetDashboardRevision(ctx); again != before+1 {
		t.Fatal("a repeated merge bumped the revision")
	}
	if counts := f.reconcile(); counts.Changed() || f.state("x-chat") != "y1|manual|manual" {
		t.Fatalf("reconcile=%+v %s", counts, f.state("x-chat"))
	}
	// An event holding an operator's merge cannot be merged away; one holding
	// only rule merges can, and they are released.
	if _, err = f.s.MergeEvent(ctx, "y1", "y2"); !errors.Is(err, ErrEventMergeConflict) {
		t.Fatalf("conflict=%v", err)
	}
	if _, err = f.s.MergeEvent(ctx, "y2", "y1"); err != nil {
		t.Fatal(err)
	}
	f.expect(map[string]string{"x-notice": unmerged, "y2": "y1|manual|manual"})
	if f.reconcile(); f.state("x-notice") != unmerged {
		t.Fatalf("rule merged into a merged event: %s", f.state("x-notice"))
	}
	// Invalid targets: itself, a merged event, a deleted one, a missing one.
	f.add("x-gone", "x", "削除済み", "2026-10-10", "", "stream", "deleted_at='2026-10-10T00:00:00.000Z'")
	for _, into := range []string{"y1", "x-chat", "y2", "x-gone", "missing", ""} {
		if _, err = f.s.MergeEvent(ctx, "y1", into); !errors.Is(err, ErrEventMergeInvalid) {
			t.Errorf("into %q: %v", into, err)
		}
	}
	if _, err = f.s.MergeEvent(ctx, "missing", "y1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing=%v", err)
	}
	if _, err = f.s.MergeEvent(ctx, "x-gone", "y1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted=%v", err)
	}
	// Undoing it keeps the event separate.
	if _, err = f.s.UnmergeEvent(ctx, "x-chat"); err != nil || f.state("x-chat") != "|manual|unmerged_from:y1" {
		t.Fatalf("unmerge=%v %s", err, f.state("x-chat"))
	}
}
