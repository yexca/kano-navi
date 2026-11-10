package sqlite

import (
	"context"
	"testing"
	"time"
)

// A Bilibili-only stream announced on both X and Bilibili: the duplicate
// rules compare X notices only with YouTube events (Rule B), so two timed
// entries of the same stream stay apart, while a date-only X entry merges
// into the timed Bilibili one (Rule A) as into any timed stream.
func TestBilibiliStreamsFollowTheExistingMergeRules(t *testing.T) {
	start := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	automatic := func(id, source string, timed bool) mergeEvent {
		e := mergeEvent{id: id, source: source, title: "杂谈", startsOn: "2026-11-07", eventType: "stream", precision: "exact", provenance: "automatic", cancellation: "none"}
		if timed {
			e.start, e.hasStart, e.startTimeSet = start, true, true
		} else {
			e.precision = "unknown"
		}
		return e
	}
	desired, _, ambiguous := planMerges([]mergeEvent{automatic("bilibili-schedule-a", "bilibili", true), automatic("x-schedule-a", "x", true)})
	if len(desired) != 0 || ambiguous != 0 {
		t.Fatalf("timed duplicates merged: %v %d", desired, ambiguous)
	}
	desired, reasons, _ := planMerges([]mergeEvent{automatic("bilibili-schedule-a", "bilibili", true), automatic("x-schedule-b", "x", false)})
	if desired["x-schedule-b"] != "bilibili-schedule-a" || reasons["x-schedule-b"] != mergeReasonTitle {
		t.Fatalf("date-only merge=%v %v", desired, reasons)
	}
	// A timed Bilibili notice is not a Rule B notice, so it never merges into
	// a YouTube stream at the same time.
	youtube := automatic("youtube-AAAAAAAAAAA", "youtube", true)
	if desired, _, _ = planMerges([]mergeEvent{automatic("bilibili-schedule-a", "bilibili", true), youtube}); len(desired) != 0 {
		t.Fatalf("bilibili merged into youtube: %v", desired)
	}
}

// Rule D: an X notice of a Bilibili stream or release merges into the
// Bilibili dynamic's entry starting within 30 minutes; never the reverse,
// and never into a YouTube event.
func TestXNoticesOfBilibiliStreamsMergeIntoTheDynamic(t *testing.T) {
	start := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	entry := func(id, source, eventType, platform string, offset time.Duration) mergeEvent {
		// As loadMergeEvents reads a row whose platforms array is its
		// single platform.
		return mergeEvent{id: id, source: source, title: "杂谈", startsOn: "2026-11-07", eventType: eventType, precision: "exact",
			provenance: "automatic", cancellation: "none", platform: platform, platforms: NormalizePlatforms([]string{platform}),
			start: start.Add(offset), hasStart: true, startTimeSet: true}
	}
	dynamic := entry("bilibili-schedule-a", "bilibili", "stream", "bilibili", 0)
	notice := entry("x-schedule-a", "x", "stream", "bilibili", 20*time.Minute)
	youtube := entry("youtube-AAAAAAAAAAA", "youtube", "stream", "youtube", 0)
	desired, reasons, _ := planMerges([]mergeEvent{dynamic, notice, youtube})
	if desired["x-schedule-a"] != "bilibili-schedule-a" || reasons["x-schedule-a"] != mergeReasonBilibili || desired["bilibili-schedule-a"] != "" {
		t.Fatalf("desired=%v reasons=%v", desired, reasons)
	}
	// Too far apart, or a release against a stream, stays apart.
	if desired, _, _ = planMerges([]mergeEvent{dynamic, entry("x-schedule-b", "x", "stream", "bilibili", 40*time.Minute)}); len(desired) != 0 {
		t.Fatalf("40 minutes merged: %v", desired)
	}
	if desired, _, _ = planMerges([]mergeEvent{dynamic, entry("x-schedule-c", "x", "release", "bilibili", 0)}); len(desired) != 0 {
		t.Fatalf("release merged into a stream: %v", desired)
	}
	// Two Bilibili entries at the same time leave the notice alone.
	if desired, _, ambiguous := planMerges([]mergeEvent{dynamic, entry("bilibili-schedule-b", "bilibili", "stream", "bilibili", 10*time.Minute), notice}); len(desired) != 0 || ambiguous != 1 {
		t.Fatalf("ambiguous=%v %d", desired, ambiguous)
	}
	// A date-only Bilibili entry never merges into a timed X notice.
	dateOnly := mergeEvent{id: "bilibili-schedule-d", source: "bilibili", title: "杂谈", startsOn: "2026-11-07", eventType: "stream", precision: "unknown", provenance: "automatic", cancellation: "none", platform: "bilibili"}
	if desired, _, _ = planMerges([]mergeEvent{dateOnly, notice}); len(desired) != 0 {
		t.Fatalf("bilibili merged into x: %v", desired)
	}
}

func TestBilibiliReleasesPairWithTheirUpload(t *testing.T) {
	s := biliStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	opus := "https://www.bilibili.com/opus/1"
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.DB().Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO events (id, source, source_item_id, source_key, title, starts_on, starts_at, event_type, url, platform, provenance, created_at, updated_at) VALUES
		('bilibili-schedule-r', 'bilibili', 'bilibili-1', 'k1', '限定歌曲', '2026-11-07', '2026-11-07T09:00:00.000Z', 'release', ?1, 'bilibili', 'automatic', ?2, ?2),
		('bilibili-schedule-l', 'bilibili', 'bilibili-1', 'k2', '锁定', '2026-11-07', '2026-11-07T09:00:00.000Z', 'release', ?1, 'bilibili', 'automatic', ?2, ?2)`, opus, workflowTime(now))
	exec(`UPDATE events SET manual_locked = 1 WHERE id = 'bilibili-schedule-l'`)
	exec(`INSERT INTO videos (id, source, title, published_at, url, kind) VALUES ('BV1AAAAAAAAA', 'bilibili', '限定歌曲', '2026-11-07T09:12:00Z', 'https://www.bilibili.com/video/BV1AAAAAAAAA', 'upload')`)
	counts, err := s.ReconcileEventMerges(ctx, now)
	if err != nil || counts.ReleasesPaired != 1 {
		t.Fatalf("counts=%#v err=%v", counts, err)
	}
	var url, locked string
	var sources int
	if err = s.DB().QueryRow(`SELECT url FROM events WHERE id = 'bilibili-schedule-r'`).Scan(&url); err != nil || url != "https://www.bilibili.com/video/BV1AAAAAAAAA" {
		t.Fatalf("url=%q err=%v", url, err)
	}
	if err = s.DB().QueryRow(`SELECT url FROM events WHERE id = 'bilibili-schedule-l'`).Scan(&locked); err != nil || locked != opus {
		t.Fatalf("locked url=%q err=%v", locked, err)
	}
	if err = s.DB().QueryRow(`SELECT COUNT(*) FROM event_sources WHERE event_id = 'bilibili-schedule-r' AND source = 'bilibili-video' AND source_item_id = 'BV1AAAAAAAAA'`).Scan(&sources); err != nil || sources != 1 {
		t.Fatalf("sources=%d err=%v", sources, err)
	}
	// A second run changes nothing.
	if again, err := s.ReconcileEventMerges(ctx, now); err != nil || again.ReleasesPaired != 0 {
		t.Fatalf("again=%#v err=%v", again, err)
	}
	// A second upload in the window makes the pairing ambiguous for a new
	// release, which keeps its link.
	exec(`INSERT INTO videos (id, source, title, published_at, url, kind) VALUES ('BV1BBBBBBBBB', 'bilibili', 'other', '2026-11-07T08:50:00Z', 'https://www.bilibili.com/video/BV1BBBBBBBBB', 'upload')`)
	exec(`INSERT INTO events (id, source, source_item_id, source_key, title, starts_on, starts_at, event_type, url, platform, provenance, created_at, updated_at) VALUES
		('bilibili-schedule-n', 'bilibili', 'bilibili-2', 'k3', '新歌', '2026-11-07', '2026-11-07T09:00:00.000Z', 'release', ?1, 'bilibili', 'automatic', ?2, ?2)`, opus, workflowTime(now))
	if _, err = s.ReconcileEventMerges(ctx, now); err != nil {
		t.Fatal(err)
	}
	if err = s.DB().QueryRow(`SELECT url FROM events WHERE id = 'bilibili-schedule-n'`).Scan(&url); err != nil || url != opus {
		t.Fatalf("ambiguous url=%q err=%v", url, err)
	}
}

// A locked release keeps its pairing: a new automatic release of the same
// upload is not paired, and the locked entry's source row is not rewritten.
func TestALockedReleaseKeepsItsUpload(t *testing.T) {
	s := biliStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	opus := "https://www.bilibili.com/opus/1"
	video := "https://www.bilibili.com/video/BV1AAAAAAAAA"
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.DB().Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO events (id, source, source_item_id, source_key, title, starts_on, starts_at, event_type, url, platform, provenance, created_at, updated_at) VALUES
		('bilibili-schedule-l', 'bilibili', 'bilibili-1', 'k1', '限定歌曲', '2026-11-07', '2026-11-07T09:00:00.000Z', 'release', ?1, 'bilibili', 'automatic', ?2, ?2)`, opus, workflowTime(now))
	exec(`INSERT INTO videos (id, source, title, published_at, url, kind) VALUES ('BV1AAAAAAAAA', 'bilibili', '限定歌曲', '2026-11-07T09:12:00Z', ?, 'upload')`, video)
	if counts, err := s.ReconcileEventMerges(ctx, now); err != nil || counts.ReleasesPaired != 1 {
		t.Fatalf("counts=%#v err=%v", counts, err)
	}
	exec(`UPDATE events SET manual_locked = 1 WHERE id = 'bilibili-schedule-l'`)
	exec(`INSERT INTO events (id, source, source_item_id, source_key, title, starts_on, starts_at, event_type, url, platform, provenance, created_at, updated_at) VALUES
		('bilibili-schedule-n', 'bilibili', 'bilibili-2', 'k2', '限定歌曲', '2026-11-07', '2026-11-07T09:10:00.000Z', 'release', ?1, 'bilibili', 'automatic', ?2, ?2)`, opus, workflowTime(now))
	before := workflowTime(now)
	if counts, err := s.ReconcileEventMerges(ctx, now.Add(time.Hour)); err != nil || counts.ReleasesPaired != 0 {
		t.Fatalf("counts=%#v err=%v", counts, err)
	}
	var owner, updated string
	if err := s.DB().QueryRow(`SELECT event_id, updated_at FROM event_sources WHERE source = 'bilibili-video' AND source_item_id = 'BV1AAAAAAAAA' AND deleted_at IS NULL`).Scan(&owner, &updated); err != nil ||
		owner != "bilibili-schedule-l" || updated != before {
		t.Fatalf("owner=%q updated=%q err=%v", owner, updated, err)
	}
	var locked, fresh string
	if err := s.DB().QueryRow(`SELECT (SELECT url FROM events WHERE id = 'bilibili-schedule-l'), (SELECT url FROM events WHERE id = 'bilibili-schedule-n')`).Scan(&locked, &fresh); err != nil ||
		locked != video || fresh != opus {
		t.Fatalf("locked=%q new=%q err=%v", locked, fresh, err)
	}
}
