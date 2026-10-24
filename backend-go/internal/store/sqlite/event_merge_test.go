package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ytsource "github.com/yexca/kano-navi/backend-go/internal/source/youtube"
)

// mergeNow is Saturday 2026-10-24 12:00 in Japan.
var mergeNow = time.Date(2026, 10, 24, 3, 0, 0, 0, time.UTC)

type mergeFixture struct {
	t *testing.T
	s *Store
}

func newMergeFixture(t *testing.T) *mergeFixture {
	t.Helper()
	s, err := OpenStandalone(filepath.Join(t.TempDir(), "database", "kano.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &mergeFixture{t: t, s: s}
}

// add inserts a synthetic event. clock is a Japan time ("20:00" or
// "19:59:16"); an empty clock makes a date-only entry. options holds extra
// column assignments applied after the insert.
func (f *mergeFixture) add(id, source, title, day, clock, kind string, options ...string) {
	f.t.Helper()
	var startsAt any
	precision := "unknown"
	if clock != "" {
		if strings.Count(clock, ":") == 1 {
			clock += ":00"
		}
		start, err := time.Parse(time.RFC3339, day+"T"+clock+"+09:00")
		if err != nil {
			f.t.Fatal(err)
		}
		startsAt, precision = ISOTime(start), "exact"
	}
	url := "https://x.com/example/status/" + id
	if source == "youtube" {
		url = "https://www.youtube.com/watch?v=" + strings.Repeat(strings.ToUpper(id[len(id)-1:]), 11)
	}
	f.exec(`INSERT INTO events(id,source,source_item_id,source_key,title,starts_on,starts_at,time_precision,event_type,url,provenance,manual_locked,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,'automatic',0,'2026-10-01T00:00:00.000Z','2026-10-01T00:00:00.000Z')`,
		id, source, id, "key", title, day, startsAt, precision, kind, url)
	for _, option := range options {
		f.exec(`UPDATE events SET `+option+` WHERE id=?`, id)
	}
}

func (f *mergeFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.s.DB().Exec(query, args...); err != nil {
		f.t.Fatalf("%s: %v", query, err)
	}
}

func (f *mergeFixture) reconcile() MergeCounts {
	f.t.Helper()
	counts, err := f.s.ReconcileEventMerges(context.Background(), mergeNow)
	if err != nil {
		f.t.Fatal(err)
	}
	return counts
}

// state is "target|source|reason", empty fields for NULL.
func (f *mergeFixture) state(id string) string {
	f.t.Helper()
	var into, source, reason sql.NullString
	if err := f.s.DB().QueryRow(`SELECT merged_into,merge_source,merge_reason FROM events WHERE id=?`, id).Scan(&into, &source, &reason); err != nil {
		f.t.Fatal(err)
	}
	return into.String + "|" + source.String + "|" + reason.String
}

func (f *mergeFixture) expect(want map[string]string) {
	f.t.Helper()
	for id, state := range want {
		if got := f.state(id); got != state {
			f.t.Errorf("%s = %q, want %q", id, got, state)
		}
	}
}

const unmerged = "||"

func TestReconcileRuleAMergesDateOnlyEntries(t *testing.T) {
	f := newMergeFixture(t)
	// A shared token chooses among several timed streams.
	f.add("a1", "x", "歌枠リレー", "2026-10-25", "", "stream")
	f.add("y1", "youtube", "【歌枠】秋を奏でる歌枠リレー", "2026-10-25", "20:00", "stream")
	f.add("x1", "x", "雑談", "2026-10-25", "12:00", "stream")
	f.add("a2", "x", "カスタム配信", "2026-10-26", "", "stream")
	f.add("x2", "x", "【カスタム】APEX", "2026-10-26", "21:00", "stream")
	f.add("x3", "x", "朝活", "2026-10-26", "08:00", "stream")
	// The only timed stream of the day needs no shared title.
	f.add("a3", "x", "コラボ配信", "2026-10-27", "", "stream")
	f.add("y3", "youtube", "お歌の時間", "2026-10-27", "20:00", "stream")
	counts := f.reconcile()
	f.expect(map[string]string{
		"a1": "y1|rule|rule_a:title", "x1": unmerged, "y1": unmerged,
		"a2": "x2|rule|rule_a:title", "x2": unmerged, "x3": unmerged,
		"a3": "y3|rule|rule_a:only_stream", "y3": unmerged,
	})
	if counts.Merged != 3 || counts.Ambiguous != 0 || counts.From != "2026-09-24" || counts.To != "2026-12-23" {
		t.Fatalf("counts=%+v", counts)
	}
}

func TestReconcileRuleBMergesXNoticeIntoYoutube(t *testing.T) {
	f := newMergeFixture(t)
	f.add("x4", "x", "今夜は雑談", "2026-10-28", "20:00", "stream")
	f.add("y4", "youtube", "【雑談】おしゃべり", "2026-10-28", "19:59:16", "stream")
	// Rule B runs first, so the weekly entry sees one timed stream.
	f.add("a4", "x", "雑談", "2026-10-28", "", "stream")
	f.add("x5", "x", "歌", "2026-10-29", "21:00", "stream")
	f.add("y5", "youtube", "歌枠", "2026-10-29", "20:00", "stream")
	f.add("x6", "x", "メン限雑談", "2026-10-30", "22:00", "stream")
	f.add("y6", "youtube", "【メンバー限定】雑談", "2026-10-30", "22:10", "member")
	f.add("x7", "x", "メン限", "2026-10-31", "23:00", "member")
	f.add("y7", "youtube", "雑談", "2026-10-31", "23:05", "stream")
	f.reconcile()
	f.expect(map[string]string{
		"x4": "y4|rule|rule_b:youtube_start", "a4": "y4|rule|rule_a:title", "y4": unmerged,
		"x5": unmerged, // an hour apart
		"x6": "y6|rule|rule_b:youtube_start",
		"x7": unmerged, // a members-only notice never merges into a public stream
	})
}

func TestReconcileExclusions(t *testing.T) {
	f := newMergeFixture(t)
	// A manual or locked event is never merged away, but can be a target.
	f.add("x8", "x", "雑談", "2026-11-01", "20:00", "stream", "provenance='manual',manual_locked=1")
	f.add("y8", "youtube", "雑談", "2026-11-01", "20:00", "stream")
	f.add("x9", "x", "雑談", "2026-11-02", "20:10", "stream")
	f.add("y9", "youtube", "雑談", "2026-11-02", "20:00", "stream", "provenance='manual',manual_locked=1")
	// A cancellation overlay on either side blocks the merge.
	f.add("x10", "x", "雑談", "2026-11-03", "20:00", "stream", "cancellation_status='llm_suspected'")
	f.add("y10", "youtube", "雑談", "2026-11-03", "20:00", "stream")
	f.add("x11", "x", "雑談", "2026-11-04", "20:00", "stream")
	f.add("y11", "youtube", "雑談", "2026-11-04", "20:00", "stream", "cancellation_status='manual_confirmed',cancellation_reason='synthetic'")
	// Ambiguity: two reservations near the notice.
	f.add("x12", "x", "雑談", "2026-11-05", "20:10", "stream")
	f.add("y12a", "youtube", "雑談", "2026-11-05", "20:00", "stream")
	f.add("y12b", "youtube", "ゲーム", "2026-11-05", "20:20", "stream")
	// Ambiguity: two title matches, no title match among two streams, and
	// two weekly entries for one stream.
	f.add("a13", "x", "歌枠", "2026-11-06", "", "stream")
	f.add("x13a", "x", "歌枠 前半", "2026-11-06", "12:00", "stream")
	f.add("x13b", "x", "歌枠 後半", "2026-11-06", "23:00", "stream")
	f.add("a14", "x", "コラボ", "2026-11-07", "", "stream")
	f.add("x14a", "x", "雑談", "2026-11-07", "12:00", "stream")
	f.add("x14b", "x", "ゲーム", "2026-11-07", "20:00", "stream")
	f.add("a15a", "x", "コラボ", "2026-11-08", "", "stream")
	f.add("a15b", "x", "企画", "2026-11-08", "", "stream")
	f.add("x15", "x", "雑談", "2026-11-08", "20:00", "stream")
	// Only streams merge, and only inside the bounded window.
	f.add("a16", "x", "イベント", "2026-11-09", "", "event")
	f.add("x16", "x", "イベント告知", "2026-11-09", "20:00", "stream")
	f.add("a17", "x", "歌枠", "2027-03-01", "", "stream")
	f.add("x17", "x", "歌枠", "2027-03-01", "20:00", "stream")
	counts := f.reconcile()
	f.expect(map[string]string{
		"x8": unmerged, "x9": "y9|rule|rule_b:youtube_start",
		"x10": unmerged, "x11": unmerged,
		"x12": unmerged, "a13": unmerged, "a14": unmerged, "a15a": unmerged, "a15b": unmerged,
		"a16": unmerged, "a17": unmerged,
	})
	if counts.Merged != 1 || counts.Ambiguous != 5 {
		t.Fatalf("counts=%+v", counts)
	}
	// A later overlay freezes an existing merge instead of releasing it.
	f.exec(`UPDATE events SET cancellation_status='llm_suspected' WHERE id='y9'`)
	if counts = f.reconcile(); counts.Changed() || f.state("x9") != "y9|rule|rule_b:youtube_start" {
		t.Fatalf("overlay changed the merge: %+v %s", counts, f.state("x9"))
	}
}

func TestReconcileIsIdempotentAndReleasesLostTargets(t *testing.T) {
	f := newMergeFixture(t)
	ctx := context.Background()
	f.add("x1", "x", "雑談", "2026-10-25", "20:00", "stream")
	f.add("y1", "youtube", "雑談", "2026-10-25", "20:00", "stream")
	before, _ := f.s.GetDashboardRevision(ctx)
	if counts := f.reconcile(); counts.Merged != 1 {
		t.Fatalf("first=%+v", counts)
	}
	first, _ := f.s.GetDashboardRevision(ctx)
	counts := f.reconcile()
	second, _ := f.s.GetDashboardRevision(ctx)
	if counts.Changed() || counts.Kept != 1 || first != before+1 || second != first {
		t.Fatalf("second=%+v revisions %d %d %d", counts, before, first, second)
	}
	// A deleted target releases its duplicate, which is the remaining record.
	if _, err := f.s.DeleteManualEvent(ctx, "y1"); err != nil {
		t.Fatal(err)
	}
	if counts = f.reconcile(); counts.Released != 1 || f.state("x1") != unmerged {
		t.Fatalf("release=%+v %s", counts, f.state("x1"))
	}
	// Editing a merged event makes it an operator's event: it is shown again
	// at once and never merged away afterwards.
	f.add("x2", "x", "歌枠", "2026-10-26", "21:00", "stream")
	f.add("y2", "youtube", "歌枠", "2026-10-26", "21:00", "stream")
	f.reconcile()
	if _, err := f.s.UpdateManualEvent(ctx, "x2", EventInput{Title: "歌枠", StartsOn: "2026-10-26", StartsAt: "2026-10-26T12:00:00.000Z", EventType: "stream", CancellationStatus: "none"}); err != nil {
		t.Fatal(err)
	}
	if f.state("x2") != unmerged {
		t.Fatalf("edit kept the merge: %s", f.state("x2"))
	}
	if f.reconcile(); f.state("x2") != unmerged {
		t.Fatalf("locked event merged again: %s", f.state("x2"))
	}
}

func TestManualUnmergeStaysUndone(t *testing.T) {
	f := newMergeFixture(t)
	ctx := context.Background()
	f.add("a1", "x", "歌枠", "2026-10-25", "", "stream")
	f.add("y1", "youtube", "歌枠", "2026-10-25", "20:00", "stream")
	f.reconcile()
	event, err := f.s.UnmergeEvent(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if event.MergedInto != nil || event.MergeSource == nil || *event.MergeSource != MergeSourceManual {
		t.Fatalf("event=%+v", event)
	}
	if counts := f.reconcile(); counts.Changed() || f.state("a1") != "|manual|unmerged_from:y1" {
		t.Fatalf("reconcile undid the operator: %+v %s", counts, f.state("a1"))
	}
	if _, err = f.s.UnmergeEvent(ctx, "a1"); !errors.Is(err, ErrEventNotMerged) {
		t.Fatalf("second unmerge=%v", err)
	}
	if _, err = f.s.UnmergeEvent(ctx, "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing=%v", err)
	}
	page, err := f.s.GetCalendarPage(ctx, "2026-10-25", "2026-10-25", 1, 10)
	if err != nil || page.Total != 2 {
		t.Fatalf("calendar=%+v %v", page, err)
	}
}

func TestMergedEventsLeavePublicReads(t *testing.T) {
	f := newMergeFixture(t)
	ctx := context.Background()
	f.add("a1", "x", "歌枠リレー", "2026-10-25", "", "stream")
	f.add("y1", "youtube", "秋を奏でる歌枠リレー", "2026-10-25", "20:00", "stream")
	f.add("x1", "x", "雑談", "2026-10-25", "12:00", "stream")
	f.add("x2", "x", "雑談", "2026-10-24", "20:00", "stream")
	f.add("y2", "youtube", "雑談", "2026-10-24", "20:05", "stream")
	f.reconcile()

	page, err := f.s.GetCalendarPage(ctx, "2026-10-19", "2026-10-25", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || page.TotalPages != 2 || page.DayCounts["2026-10-25"] != 2 || page.DayCounts["2026-10-24"] != 1 {
		t.Fatalf("calendar total=%d pages=%d days=%v", page.Total, page.TotalPages, page.DayCounts)
	}
	second, err := f.s.GetCalendarPage(ctx, "2026-10-19", "2026-10-25", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, item := range append(page.Items, second.Items...) {
		seen[item.ID] = true
	}
	if len(seen) != 3 || seen["a1"] || seen["x2"] {
		t.Fatalf("calendar items=%v", seen)
	}

	dashboard, err := f.s.GetDashboard(ctx, 7, mergeNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range dashboard.Events {
		if event.ID == "a1" || event.ID == "x2" {
			t.Fatalf("dashboard listed merged %s", event.ID)
		}
	}
	if dashboard.Summary.Counts.Events != 3 || dashboard.Summary.Counts.UpcomingEvents != 3 {
		t.Fatalf("dashboard counts=%+v", dashboard.Summary.Counts)
	}
	if next := dashboard.Summary.NextEvent; next == nil || next.ID != "y2" {
		t.Fatalf("next=%+v", next)
	}

	counts, err := f.s.AdminCounts(ctx, "2026-10-24")
	if err != nil || counts.Events != 3 || counts.UpcomingEvents != 3 {
		t.Fatalf("admin counts=%+v %v", counts, err)
	}
	visible, err := f.s.ListAdminEvents(ctx, AdminEventFilter{Merge: "visible"})
	if err != nil || visible.Total != 3 {
		t.Fatalf("visible=%+v %v", visible, err)
	}
	merged, err := f.s.ListAdminEvents(ctx, AdminEventFilter{Merge: "merged", Ascending: true})
	if err != nil || merged.Total != 2 {
		t.Fatalf("merged=%+v %v", merged, err)
	}
	if item := merged.Items[0]; item.ID != "x2" || *item.MergedInto != "y2" || *item.MergedIntoTitle != "雑談" || *item.MergeReason != "rule_b:youtube_start" || item.MergedAt == nil {
		t.Fatalf("merged item=%+v", item)
	}
	if all, _ := f.s.ListAdminEvents(ctx, AdminEventFilter{}); all.Total != 5 {
		t.Fatalf("admin default hides merged events: %d", all.Total)
	}
}

func TestSourceWritesKeepMergeColumns(t *testing.T) {
	f := newMergeFixture(t)
	ctx := context.Background()
	f.add("a1", "x", "歌枠", "2026-10-25", "", "stream")
	f.add("y1", "youtube", "歌枠", "2026-10-25", "20:00", "stream")
	f.exec(`UPDATE events SET merged_into='y1',merge_source='manual',merge_reason='synthetic',merged_at='2026-10-24T00:00:00.000Z' WHERE id='a1'`)
	f.exec(`UPDATE events SET merge_source='manual',merge_reason='synthetic' WHERE id='y1'`)
	// The YouTube writer refreshes the reservation without touching merges.
	f.exec(`UPDATE events SET source_item_id='AAAAAAAAAAA',source_key='reservation' WHERE id='y1'`)
	if err := f.s.WriteYoutubeRecent(ctx, "UCAAAAAAAAAAAAAAAAAAAAAA", youtubeResult("AAAAAAAAAAA", "歌枠 refreshed", "2026-10-25T11:00:00Z")); err != nil {
		t.Fatal(err)
	}
	var title, state string
	if err := f.s.DB().QueryRow(`SELECT title,COALESCE(merged_into,'')||'|'||merge_source FROM events WHERE id='y1'`).Scan(&title, &state); err != nil {
		t.Fatal(err)
	}
	if title != "歌枠 refreshed" || state != "|manual" {
		t.Fatalf("youtube write=%s %s", title, state)
	}
	// Another owner's decision is left alone by the reconciler.
	if counts := f.reconcile(); counts.Changed() || f.state("a1") != "y1|manual|synthetic" {
		t.Fatalf("reconcile=%+v %s", counts, f.state("a1"))
	}
}

func youtubeResult(id, title, scheduledAt string) ytsource.Result {
	return ytsource.Result{
		Videos:     []ytsource.Video{{ID: id, Title: title, PublishedAt: "2026-10-20T00:00:00Z", ScheduledAt: scheduledAt, URL: "https://www.youtube.com/watch?v=" + id, Kind: "UPCOMING LIVE", IsUpcoming: true}},
		NextCursor: id, NextCursorTime: "2026-10-20T00:00:00Z",
	}
}
