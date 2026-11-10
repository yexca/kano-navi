package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
	"time"

	bilisource "github.com/yexca/kano-navi/backend/internal/source/bilibili"
)

// simulcastEntry is a timed automatic entry for the planner.
func simulcastEntry(id, source, eventType string, platforms []string, inferred bool, offset time.Duration) mergeEvent {
	start := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	return mergeEvent{id: id, source: source, title: "杂谈", startsOn: "2026-11-07", eventType: eventType, precision: "exact",
		provenance: "automatic", cancellation: "none", platform: PrimaryPlatform(platforms), platforms: platforms, inferred: inferred,
		start: start.Add(offset), hasStart: true, startTimeSet: true}
}

// Rule E: the Bilibili side of a simulcast merges into the only compatible
// YouTube event within 30 minutes; Bilibili-only wording, other types and
// ambiguity keep entries apart.
func TestRuleEMergesTheBilibiliSideOfASimulcast(t *testing.T) {
	youtube := simulcastEntry("youtube-AAAAAAAAAAA", "youtube", "stream", []string{"youtube"}, false, 0)
	inferred := simulcastEntry("bilibili-schedule-a", "bilibili", "stream", []string{"bilibili"}, true, 10*time.Minute)
	desired, reasons, _ := planMerges([]mergeEvent{inferred, youtube})
	if desired[inferred.id] != youtube.id || reasons[inferred.id] != mergeReasonSimulcast {
		t.Fatalf("dynamic without platform: %v %v", desired, reasons)
	}
	// A dynamic naming both platforms merges too, also into a members-only
	// YouTube stream.
	both := simulcastEntry("bilibili-schedule-b", "bilibili", "stream", []string{"bilibili", "youtube"}, false, 0)
	both.platform = "bilibili"
	member := youtube
	member.eventType = "member"
	if desired, reasons, _ = planMerges([]mergeEvent{both, member}); desired[both.id] != member.id || reasons[both.id] != mergeReasonSimulcast {
		t.Fatalf("both platforms: %v %v", desired, reasons)
	}
	// 「B站限定」 states Bilibili only: two entries.
	stated := simulcastEntry("bilibili-schedule-c", "bilibili", "stream", []string{"bilibili"}, false, 0)
	if desired, _, _ = planMerges([]mergeEvent{stated, youtube}); len(desired) != 0 {
		t.Fatalf("bilibili-only merged: %v", desired)
	}
	// A YouTube premiere is a release, not the stream.
	premiere := youtube
	premiere.eventType = "release"
	if desired, _, _ = planMerges([]mergeEvent{inferred, premiere}); len(desired) != 0 {
		t.Fatalf("stream merged into a premiere: %v", desired)
	}
	// A Bilibili release never merges by this rule.
	release := simulcastEntry("bilibili-schedule-r", "bilibili", "release", []string{"bilibili"}, true, 0)
	if desired, _, _ = planMerges([]mergeEvent{release, premiere}); len(desired) != 0 {
		t.Fatalf("release merged: %v", desired)
	}
	// Two YouTube streams in the window: only one may be compatible.
	second := simulcastEntry("youtube-BBBBBBBBBBB", "youtube", "stream", []string{"youtube"}, false, 20*time.Minute)
	if desired, _, ambiguous := planMerges([]mergeEvent{inferred, youtube, second}); len(desired) != 0 || ambiguous != 1 {
		t.Fatalf("two youtube streams: %v %d", desired, ambiguous)
	}
	// Too far apart stays apart; a manual or locked entry never moves.
	late := simulcastEntry("bilibili-schedule-d", "bilibili", "stream", []string{"bilibili"}, true, 45*time.Minute)
	locked := inferred
	locked.locked = true
	if desired, _, _ = planMerges([]mergeEvent{late, youtube}); len(desired) != 0 {
		t.Fatalf("45 minutes merged: %v", desired)
	}
	if desired, _, _ = planMerges([]mergeEvent{locked, youtube}); len(desired) != 0 {
		t.Fatalf("locked merged: %v", desired)
	}
	// An X notice naming both platforms merges into the YouTube event.
	xBoth := simulcastEntry("x-schedule-a", "x", "stream", []string{"youtube", "bilibili"}, false, 0)
	if desired, _, _ = planMerges([]mergeEvent{xBoth, youtube}); desired[xBoth.id] != youtube.id {
		t.Fatalf("x simulcast: %v", desired)
	}
	// No chains: an X notice that Rule D merged into the dynamic follows the
	// dynamic onto the YouTube event.
	xBilibili := simulcastEntry("x-schedule-b", "x", "stream", []string{"bilibili"}, false, 5*time.Minute)
	desired, reasons, _ = planMerges([]mergeEvent{inferred, xBilibili, youtube})
	if desired[inferred.id] != youtube.id || desired[xBilibili.id] != youtube.id || reasons[xBilibili.id] != mergeReasonBilibili {
		t.Fatalf("chain: %v %v", desired, reasons)
	}
}

// Rule E decisions are rule-owned: recomputed on every run and released
// when the match no longer holds.
func TestRuleEIsRecomputedAndReleased(t *testing.T) {
	f := newMergeFixture(t)
	f.add("youtube-A", "youtube", "雑談", "2026-10-25", "20:00", "stream", `platform='youtube',platforms='["youtube"]'`)
	f.add("bilibili-schedule-a", "bilibili", "杂谈", "2026-10-25", "20:00", "stream", `platform='bilibili',platforms='["bilibili"]',platforms_inferred=1`)
	f.add("bilibili-schedule-b", "bilibili", "B站限定", "2026-10-25", "20:00", "stream", `platform='bilibili',platforms='["bilibili"]'`)
	f.reconcile()
	f.expect(map[string]string{
		"bilibili-schedule-a": "youtube-A|rule|" + mergeReasonSimulcast,
		"bilibili-schedule-b": unmerged,
	})
	// The dynamic moves two hours later: the merge is released.
	f.exec(`UPDATE events SET starts_at='2026-10-25T13:00:00.000Z' WHERE id='bilibili-schedule-a'`)
	f.reconcile()
	f.expect(map[string]string{"bilibili-schedule-a": unmerged})
}

// simulcastNow is the store tests' clock: midday in Japan, so every instant
// they place around it falls on the same Japan date.
var simulcastNow = time.Date(2026, 11, 7, 12, 0, 0, 0, time.FixedZone("JST", 9*3600)).UTC()

// simulcastStore is a store with a learned live room.
func simulcastStore(t *testing.T) (*Store, func(id, source, kind, start, end, url, platforms string, inferred int)) {
	t.Helper()
	s := biliStore(t)
	add := func(id, source, kind, start, end, url, platforms string, inferred int) {
		t.Helper()
		var endsAt any
		if end != "" {
			endsAt = end
		}
		var list any
		if platforms != "" {
			list = platforms
		}
		day := parseInstant(start).In(time.FixedZone("JST", 9*3600)).Format("2006-01-02")
		if _, err := s.DB().Exec(`INSERT INTO events (id, source, title, starts_on, starts_at, ends_at, event_type, url, platform, platforms, platforms_inferred, provenance, created_at, updated_at)
 VALUES (?, ?, '杂谈', ?, ?, ?, ?, ?, json_extract(COALESCE(?,'[]'),'$[0]'), ?, ?, 'automatic', ?, ?)`,
			id, source, day, start, endsAt, kind, url, list, list, inferred, start, start); err != nil {
			t.Fatal(err)
		}
	}
	return s, add
}

func findEvent(events []DashboardEvent, id string) *DashboardEvent {
	for i := range events {
		if events[i].ID == id {
			return &events[i]
		}
	}
	return nil
}

// The room is checked densely for streams shown on Bilibili (announced, or
// through an entry merged into them) and sparsely for every other one.
func TestLivePlanChecksEveryScheduledStream(t *testing.T) {
	s, add := simulcastStore(t)
	ctx := context.Background()
	now := simulcastNow
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	add("youtube-AAAAAAAAAAA", "youtube", "stream", at(-time.Minute), "", "https://www.youtube.com/watch?v=AAAAAAAAAAA", `["youtube"]`, 0)
	add("youtube-BBBBBBBBBBB", "youtube", "stream", at(-time.Minute), "", "https://www.youtube.com/watch?v=BBBBBBBBBBB", `["youtube"]`, 0)
	add("bilibili-schedule-b", "bilibili", "stream", at(-time.Minute), "", "https://www.bilibili.com/opus/2", `["bilibili"]`, 1)
	add("youtube-CCCCCCCCCCC", "youtube", "stream", at(-3*time.Hour), at(-2*time.Hour), "https://www.youtube.com/watch?v=CCCCCCCCCCC", `["youtube"]`, 0)
	if _, err := s.DB().Exec(`UPDATE events SET merged_into='youtube-BBBBBBBBBBB',merge_source='rule',merge_reason=? WHERE id='bilibili-schedule-b'`, mergeReasonSimulcast); err != nil {
		t.Fatal(err)
	}
	settings, _ := s.LiveProbeSettings(ctx)
	checked := bilisource.LiveState{Room: bilisource.Room{RoomID: "1000001"}, CheckedAt: now.Add(-2 * time.Minute), LastAttemptAt: now.Add(-2 * time.Minute)}
	if _, err := s.WriteBilibiliLive(ctx, biliAccount, checked); err != nil {
		t.Fatal(err)
	}
	plan, err := s.BilibiliLivePlan(ctx, biliAccount, now, settings)
	if err != nil {
		t.Fatal(err)
	}
	phases := map[string]BilibiliLiveTarget{}
	for _, target := range plan.Targets {
		phases[target.EventID] = target
	}
	// The ended YouTube stream is left out.
	if len(plan.Targets) != 2 || phases["youtube-BBBBBBBBBBB"].Phase != LivePhaseStarting {
		t.Fatalf("plan=%#v", plan.Targets)
	}
	sparse := phases["youtube-AAAAAAAAAAA"]
	if sparse.Phase != LivePhaseSparse || !sparse.DueAt.Equal(now.Add(4*time.Minute)) {
		t.Fatalf("sparse=%#v", sparse)
	}
	// After the 5-minute check the next is at 20 minutes; after that none
	// while the room is off air, and each interval while it is live.
	checked.CheckedAt, checked.LastAttemptAt = now.Add(5*time.Minute), now.Add(5*time.Minute)
	_, _ = s.WriteBilibiliLive(ctx, biliAccount, checked)
	plan, _ = s.BilibiliLivePlan(ctx, biliAccount, now.Add(5*time.Minute), settings)
	if target := plan.Targets[0]; target.EventID != "youtube-AAAAAAAAAAA" && plan.Targets[1].EventID != "youtube-AAAAAAAAAAA" {
		t.Fatalf("plan=%#v", plan.Targets)
	}
	for _, target := range plan.Targets {
		if target.EventID == "youtube-AAAAAAAAAAA" && !target.DueAt.Equal(now.Add(19*time.Minute)) {
			t.Fatalf("second sparse=%#v", target)
		}
	}
	checked.CheckedAt, checked.LastAttemptAt = now.Add(20*time.Minute), now.Add(20*time.Minute)
	_, _ = s.WriteBilibiliLive(ctx, biliAccount, checked)
	plan, _ = s.BilibiliLivePlan(ctx, biliAccount, now.Add(20*time.Minute), settings)
	for _, target := range plan.Targets {
		if target.EventID == "youtube-AAAAAAAAAAA" {
			t.Fatalf("sparse after 20 minutes off air=%#v", target)
		}
	}
	checked.Room.LiveStatus, checked.Room.LiveTime = bilisource.RoomLive, at(-time.Minute)
	checked.CheckedAt, checked.LastAttemptAt = now.Add(21*time.Minute), now.Add(21*time.Minute)
	_, _ = s.WriteBilibiliLive(ctx, biliAccount, checked)
	plan, _ = s.BilibiliLivePlan(ctx, biliAccount, now.Add(21*time.Minute), settings)
	interval := time.Duration(settings.BilibiliIntervalMinutes) * time.Minute
	for _, target := range plan.Targets {
		if target.EventID == "youtube-AAAAAAAAAAA" && (target.Phase != LivePhaseSparse || !target.DueAt.Equal(now.Add(21*time.Minute+interval))) {
			t.Fatalf("sparse while live=%#v", target)
		}
	}
}

// A Bilibili broadcast at the time of a YouTube broadcast is its simulcast:
// the YouTube card shows both platforms, and no separate live-now ticket.
func TestObservedSimulcastAttachesToTheYoutubeEvent(t *testing.T) {
	s, add := simulcastStore(t)
	ctx := context.Background()
	now := simulcastNow
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	_ = add
	watch := "https://www.youtube.com/watch?v=AAAAAAAAAAA"
	// The YouTube writer stores the reservation the probe saw live.
	reservation := youtubeResult("AAAAAAAAAAA", "杂谈", at(-20*time.Minute))
	reservation.Videos[0].IsUpcoming, reservation.Videos[0].LiveNow, reservation.Videos[0].LiveStatus = false, true, "live"
	if err := s.WriteYoutubeRecent(ctx, "UCsynthetic", reservation); err != nil {
		t.Fatal(err)
	}
	live := bilisource.LiveState{Room: bilisource.Room{RoomID: "1000001", Title: "杂谈", LiveStatus: bilisource.RoomLive, LiveTime: at(-15 * time.Minute)}, CheckedAt: now, LastAttemptAt: now}
	if _, err := s.WriteBilibiliLive(ctx, biliAccount, live); err != nil {
		t.Fatal(err)
	}
	dashboard, err := s.GetDashboard(ctx, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	event := findEvent(dashboard.Events, "youtube-AAAAAAAAAAA")
	room := bilisource.RoomURL("1000001")
	if dashboard.LiveNow != nil || event == nil || !slices.Equal(event.Platforms, []string{"youtube", "bilibili"}) ||
		event.LiveStatus == nil || *event.LiveStatus != "live" || event.LiveURL == nil || *event.LiveURL != room || *event.URL != watch {
		t.Fatalf("liveNow=%#v event=%#v", dashboard.LiveNow, event)
	}
	if len(event.Links) != 2 || *event.Links[0].URL != watch || event.Links[1].Platform != "bilibili" || *event.Links[1].URL != room ||
		event.Links[1].LiveStatus == nil || *event.Links[1].LiveStatus != "live" {
		t.Fatalf("links=%#v", event.Links)
	}
	// Nothing observed is written.
	var stored string
	if err = s.DB().QueryRow(`SELECT platforms FROM events WHERE id='youtube-AAAAAAAAAAA'`).Scan(&stored); err != nil || stored != `["youtube"]` {
		t.Fatalf("stored=%q err=%v", stored, err)
	}
	// The calendar reads the same.
	page, err := s.GetCalendarPage(ctx, event.StartsOn, event.StartsOn, 1, 100)
	if err != nil || len(page.Items) != 1 || len(page.Items[0].Links) != 2 {
		t.Fatalf("calendar=%#v err=%v", page, err)
	}
	// The admin list marks the observed platform.
	admin, err := s.ListAdminEvents(ctx, AdminEventFilter{Page: 1, PageSize: 10})
	if err != nil || len(admin.Items) != 1 || !slices.Equal(admin.Items[0].Platforms, []string{"youtube"}) ||
		!slices.Equal(admin.Items[0].ShownPlatforms, []string{"youtube", "bilibili"}) {
		t.Fatalf("admin=%#v err=%v", admin.Items, err)
	}
}

// A YouTube stream that ended before an unannounced Bilibili broadcast
// began is not its simulcast: the broadcast stays live-now.
func TestBroadcastAfterAnEndedYoutubeStreamIsLiveNow(t *testing.T) {
	s, add := simulcastStore(t)
	ctx := context.Background()
	now := simulcastNow
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	add("youtube-AAAAAAAAAAA", "youtube", "stream", at(-40*time.Minute), at(-20*time.Minute), "https://www.youtube.com/watch?v=AAAAAAAAAAA", `["youtube"]`, 0)
	live := bilisource.LiveState{Room: bilisource.Room{RoomID: "1000001", Title: "杂谈", LiveStatus: bilisource.RoomLive, LiveTime: at(-15 * time.Minute)}, CheckedAt: now, LastAttemptAt: now}
	if _, err := s.WriteBilibiliLive(ctx, biliAccount, live); err != nil {
		t.Fatal(err)
	}
	dashboard, err := s.GetDashboard(ctx, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	event := findEvent(dashboard.Events, "youtube-AAAAAAAAAAA")
	if dashboard.LiveNow == nil || event == nil || !slices.Equal(event.Platforms, []string{"youtube"}) || event.LiveURL != nil {
		t.Fatalf("liveNow=%#v event=%#v", dashboard.LiveNow, event)
	}
	// An entry announced on Bilibili wins the broadcast over the YouTube
	// event, whose platforms stay its own.
	add("bilibili-schedule-a", "bilibili", "stream", at(-30*time.Minute), "", "https://www.bilibili.com/opus/1", `["bilibili"]`, 0)
	dashboard, _ = s.GetDashboard(ctx, 3, now)
	if announced := findEvent(dashboard.Events, "bilibili-schedule-a"); dashboard.LiveNow != nil || announced == nil || *announced.LiveStatus != "live" {
		t.Fatalf("liveNow=%#v announced=%#v", dashboard.LiveNow, announced)
	}
}

// A stored entry whose url is the live room reads as a Bilibili entry with
// no identity link; the row is not rewritten.
func TestARoomLinkAsUrlReadsAsTheBilibiliPlatform(t *testing.T) {
	s, add := simulcastStore(t)
	ctx := context.Background()
	now := simulcastNow
	room := bilisource.RoomURL("1000001")
	add("manual-a", "manual", "stream", now.Add(time.Hour).Format(time.RFC3339), "", room, "", 0)
	add("x-schedule-o", "x", "stream", now.Add(2*time.Hour).Format(time.RFC3339), "", "https://live.bilibili.com/2000002", `["other"]`, 0)
	dashboard, err := s.GetDashboard(ctx, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	legacy, collab := findEvent(dashboard.Events, "manual-a"), findEvent(dashboard.Events, "x-schedule-o")
	if legacy == nil || legacy.URL != nil || legacy.Platform == nil || *legacy.Platform != "bilibili" ||
		!slices.Equal(legacy.Platforms, []string{"bilibili"}) || *legacy.Links[0].URL != room {
		t.Fatalf("legacy=%#v", legacy)
	}
	if collab == nil || collab.URL == nil || *collab.URL != "https://live.bilibili.com/2000002" || !slices.Equal(collab.Platforms, []string{"other"}) {
		t.Fatalf("collab=%#v", collab)
	}
	var stored string
	if err = s.DB().QueryRow(`SELECT url FROM events WHERE id='manual-a'`).Scan(&stored); err != nil || stored != room {
		t.Fatalf("stored=%q err=%v", stored, err)
	}
}

// Manual saves write the operator's platforms; a save without them keeps
// the stored ones, and a live-room link is refused unless other is chosen.
func TestManualEventPlatforms(t *testing.T) {
	s := biliStore(t)
	ctx := context.Background()
	input := EventInput{Title: "同時配信", StartsOn: "2026-11-07", EventType: "stream", Platforms: []string{"youtube", "bilibili"}, PlatformsSet: true}
	created, err := s.CreateManualEvent(ctx, input)
	if err != nil || !slices.Equal(created.Platforms, []string{"youtube", "bilibili"}) || created.PlatformsInferred {
		t.Fatalf("created=%#v err=%v", created, err)
	}
	var platform string
	_ = s.DB().QueryRow(`SELECT platform FROM events WHERE id=?`, created.ID).Scan(&platform)
	if platform != "youtube" {
		t.Fatalf("platform=%q", platform)
	}
	kept, err := s.UpdateManualEvent(ctx, created.ID, EventInput{Title: "同時配信", StartsOn: "2026-11-07", EventType: "stream"})
	if err != nil || !slices.Equal(kept.Platforms, []string{"youtube", "bilibili"}) {
		t.Fatalf("kept=%#v err=%v", kept, err)
	}
	room := EventInput{Title: "雑談", StartsOn: "2026-11-07", EventType: "stream", URL: "https://live.bilibili.com/1000001", Platforms: []string{"bilibili"}, PlatformsSet: true}
	if _, err = s.CreateManualEvent(ctx, room); err != ErrEventLiveRoomURL {
		t.Fatalf("room link err=%v", err)
	}
	room.Platforms = []string{"other"}
	if _, err = s.CreateManualEvent(ctx, room); err != nil {
		t.Fatalf("collaboration err=%v", err)
	}
}

// Startup adds the platform columns to an older snapshot without rewriting
// the array: a YouTube writer's row gets its single platform and reads as
// youtube, every other row as no platform.
func TestStandaloneAddsTheEventPlatformsColumns(t *testing.T) {
	p := filepath.Join(t.TempDir(), "snapshot.sqlite")
	s, err := OpenStandalone(p)
	if err != nil {
		t.Fatal(err)
	}
	const stamp = "2026-03-01T00:00:00.000Z"
	for _, statement := range []string{
		`ALTER TABLE events DROP COLUMN platform`,
		`ALTER TABLE events DROP COLUMN platforms`,
		`ALTER TABLE events DROP COLUMN platforms_inferred`,
		`INSERT INTO events(id,source,title,starts_on,provenance,manual_locked,created_at,updated_at) VALUES('youtube-a','youtube','a','2026-03-02','automatic',0,'` + stamp + `','` + stamp + `')`,
		`INSERT INTO events(id,source,title,starts_on,provenance,manual_locked,created_at,updated_at) VALUES('x-a','x','a','2026-03-02','automatic',0,'` + stamp + `','` + stamp + `')`,
	} {
		if _, err = s.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	if s, err = OpenStandalone(p); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for id, want := range map[string][]string{"youtube-a": {"youtube"}, "x-a": {}} {
		var platform, platforms sql.NullString
		var inferred int
		var updated string
		if err = s.db.QueryRow(`SELECT platform,platforms,platforms_inferred,updated_at FROM events WHERE id=?`, id).Scan(&platform, &platforms, &inferred, &updated); err != nil {
			t.Fatal(err)
		}
		if platforms.Valid || inferred != 0 || updated != stamp || !slices.Equal(storedPlatforms(platforms, platform), want) {
			t.Fatalf("%s platforms=%v inferred=%d updated=%s", id, platforms, inferred, updated)
		}
	}
}
