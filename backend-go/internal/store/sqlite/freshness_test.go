package sqlite

import (
	"context"
	"testing"

	"github.com/yexca/kano-navi/backend-go/internal/source/history"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

func TestSourceFreshnessUsesRecentReadsAndOriginalWindowEnd(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := OpenReadOnly(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	freshness := func(x, youtube string) {
		t.Helper()
		got, err := s.sourceFetchedAt(ctx)
		if err != nil || len(got) != 2 || stringValue(got["x"]) != x || stringValue(got["youtube"]) != youtube {
			t.Fatalf("freshness=%v err=%v, want x=%q youtube=%q", got, err, x, youtube)
		}
	}
	freshness("", "")
	// Backfills, operator-selected ranges, malformed dates and bookkeeping are
	// successful work, but they say nothing about the newest source content.
	testutil.Exec(t, p, `INSERT INTO sync_state(source,account_id,last_success_at,metadata_json) VALUES
	 ('x-backfill','synthetic','2026-03-10T00:00:00Z','{}'),
	 ('youtube-window','range','2026-03-10T00:00:00Z','{"window":{"mode":"range","endTime":"2026-03-10T00:00:00Z"}}'),
	 ('x-window','malformed','invalid','{"window":{"mode":"recent","endTime":"2026-03-10T00:00:00Z"}}'),
	 ('x-window','metadata','2026-03-10T00:00:00Z','{invalid'),
	 ('x-timeline-window','before','2026-03-10T00:00:00Z','{"window":{"mode":"before"}}'),
	 ('x-timeline-window','legacy-count','2026-03-10T00:00:00Z','{"window":{"mode":"count","count":30}}'),
	 ('x-budget','synthetic','2026-03-10T00:00:00Z','{}'),
	 ('bilibili','synthetic','2026-03-10T00:00:00Z','{}')`)
	freshness("", "")
	testutil.Exec(t, p, `INSERT INTO sync_state(source,account_id,last_success_at,metadata_json) VALUES
	 ('x','first','2026-03-08T12:00:00+09:00','{"raw":"private-metadata"}'),
	 ('x','second','2026-03-08T02:00:00Z','{}'),
	 ('youtube-window','recent','2026-03-10T00:00:00Z','{"window":{"mode":"recent","endTime":"2026-03-09T00:00:00Z"}}')`)
	freshness("2026-03-08T03:00:00.000Z", "2026-03-09T00:00:00.000Z")
	testutil.Exec(t, p, `INSERT INTO sync_state(source,account_id,last_success_at,metadata_json) VALUES
	 ('x-timeline-window','recent','2026-03-10T00:00:00Z','{"window":{"mode":"recent","endTime":"2026-03-09T08:00:00Z"},"headFetchedAt":"2026-03-09T08:05:00Z"}')`)
	freshness("2026-03-09T08:00:00.000Z", "2026-03-09T00:00:00.000Z")
	testutil.Exec(t, p, `INSERT INTO sync_state(source,account_id,last_success_at,metadata_json) VALUES
	 ('x-timeline-window','count','2026-03-10T01:00:00Z','{"window":{"mode":"count","count":30},"headFetchedAt":"2026-03-10T01:00:00Z"}'),
	 ('youtube','incremental','2026-03-10T02:00:00Z','{}')`)
	freshness("2026-03-10T01:00:00.000Z", "2026-03-10T02:00:00.000Z")
}

func TestTimelineCheckpointFreshnessDoesNotAdvanceOnContinuationOrCleanup(t *testing.T) {
	s, query := openHistoryStore(t)
	ctx := context.Background()
	page := history.Page{Source: "x", Account: "synthetic", Checkpoint: history.TimelineCheckpoint,
		CheckpointKey: "synthetic:count", Window: history.Window{Mode: "count", Count: 30},
		HeadFetchedAt: "2026-03-10T01:00:00.000Z", Next: "older"}
	for range 2 {
		if _, err := s.WriteHistoryPage(ctx, page); err != nil {
			t.Fatal(err)
		}
		got, err := s.sourceFetchedAt(ctx)
		if err != nil || stringValue(got["x"]) != page.HeadFetchedAt {
			t.Fatalf("continuation freshness=%v err=%v", got, err)
		}
	}
	if _, err := s.db.Exec(`UPDATE sync_state SET last_success_at='2026-03-11T01:00:00.000Z' WHERE source='x-timeline-window'`); err != nil {
		t.Fatal(err)
	}
	page.CheckpointOnly, page.Next = true, ""
	if _, err := s.WriteHistoryPage(ctx, page); err != nil {
		t.Fatal(err)
	}
	if got := query(`SELECT COALESCE(cursor_id,'cleared')||'/'||last_success_at FROM sync_state WHERE source='x-timeline-window'`); got != "cleared/2026-03-11T01:00:00.000Z" {
		t.Fatalf("cursor cleanup recorded a new success=%s", got)
	}
	// A legacy checkpoint has no known head timestamp. Clearing it preserves
	// this uncertainty, including when no successful checkpoint row existed.
	if _, err := s.db.Exec(`DELETE FROM sync_state`); err != nil {
		t.Fatal(err)
	}
	page.HeadFetchedAt = ""
	if _, err := s.WriteHistoryPage(ctx, page); err != nil {
		t.Fatal(err)
	}
	if got := query(`SELECT COALESCE(last_success_at,'unknown') FROM sync_state WHERE source='x-timeline-window'`); got != "unknown" {
		t.Fatalf("empty checkpoint cleanup recorded success=%s", got)
	}
	got, err := s.sourceFetchedAt(ctx)
	if err != nil || got["x"] != nil {
		t.Fatalf("legacy cleanup freshness=%v err=%v", got, err)
	}
}

func TestSourceFreshnessWithoutCheckpointTableRemainsUnknown(t *testing.T) {
	p := testutil.NewSnapshot(t)
	testutil.Exec(t, p, `DROP TABLE sync_state`)
	s, err := OpenReadOnly(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.sourceFetchedAt(context.Background())
	if err != nil || len(got) != 2 || got["x"] != nil || got["youtube"] != nil {
		t.Fatalf("freshness=%v err=%v", got, err)
	}
}
