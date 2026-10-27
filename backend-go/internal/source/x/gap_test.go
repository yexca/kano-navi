package x

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// gapTimeline lists two own statuses a page, newest first, ending with the
// stored floor and an older status.
func gapTimeline(at func(int) string) *timelineServer {
	return &timelineServer{bodies: map[string]string{
		"":       timelineBody("page-2", at(1)+"@kano", at(2)+"@kano"),
		"page-2": timelineBody("page-3", at(3)+"@kano", at(4)+"@kano"),
		"page-3": timelineBody("page-4", at(5)+"@kano", at(6)+"@kano"),
		"page-4": timelineBody("page-5", at(100)+"@kano", at(101)+"@kano"),
	}}
}

// syncRuns repeats incremental runs as the X step does: every admitted ID is
// stored, the cursor moves to the newest one and the gap is kept.
func syncRuns(t *testing.T, a *Adapter, opts Options, runs int) (found []string, last Result) {
	t.Helper()
	for i := 0; i < runs; i++ {
		result, err := a.FetchRecent(context.Background(), "kano", opts)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range result.PendingIDs {
			if !opts.KnownIDs[id] {
				found = append(found, id)
				opts.KnownIDs[id] = true
				if idAfter(id, opts.CursorID) {
					opts.CursorID = id
				}
			}
		}
		opts.Gap, last = result.Gap, result
	}
	return found, last
}

func TestCappedRunsReadTheGapBackToTheStoredCursor(t *testing.T) {
	at := func(h int) string { return statusAt(timelineNow.Add(-time.Duration(h) * time.Hour)) }
	s := gapTimeline(at)
	// 40 statuses allow two pages: the first run stops at page 3.
	a := s.adapter(Options{BootstrapDays: 7, DiscoveryLimit: 40})
	opts := a.Options
	opts.CursorID, opts.KnownIDs = at(100), map[string]bool{at(100): true}
	found, last := syncRuns(t, a, opts, 3)
	if strings.Join(found, ",") != strings.Join([]string{at(1), at(2), at(3), at(4), at(5), at(6)}, ",") {
		t.Fatalf("found=%v", found)
	}
	// Run 1 reads the head; run 2 the head, then the gap from page 3 to the
	// floor on page 4; run 3 only the head.
	if strings.Join(s.cursors, ",") != ",page-2,,page-3,page-4," || last.Gap != nil || len(last.Notes) != 0 {
		t.Fatalf("cursors=%q gap=%v notes=%v", s.cursors, last.Gap, last.Notes)
	}
}

func TestCapCountsOnlyUnstoredStatusesAndRereadsAPartlyTakenPage(t *testing.T) {
	at := func(h int) string { return statusAt(timelineNow.Add(-time.Duration(h) * time.Hour)) }
	s := gapTimeline(at)
	a := s.adapter(Options{BootstrapDays: 7, DiscoveryLimit: 3})
	opts := a.Options
	opts.CursorID, opts.KnownIDs = at(100), map[string]bool{at(100): true}
	result, err := a.FetchRecent(context.Background(), "kano", opts)
	if err != nil {
		t.Fatal(err)
	}
	// One page of 20 is the budget of a cap of 3, so the head stops after
	// the first page and the gap starts at the second.
	if result.Gap == nil || result.Gap.Cursor != "page-2" || result.Gap.Floor != at(100) || len(result.PendingIDs) != 2 {
		t.Fatalf("gap=%v queue=%v", result.Gap, result.PendingIDs)
	}
	s.bodies[""] = timelineBody("page-2", at(1)+"@kano", at(2)+"@kano", at(3)+"@kano", at(4)+"@kano")
	s.cursors = nil
	opts.KnownIDs[at(1)] = true
	result, err = a.FetchRecent(context.Background(), "kano", Options{BootstrapDays: 7, DiscoveryLimit: 3, CursorID: at(1), KnownIDs: opts.KnownIDs})
	if err != nil {
		t.Fatal(err)
	}
	// A stored status does not count: 2, 3 and 4 are taken and the cap ends the
	// page there, so nothing is skipped.
	if strings.Join(result.PendingIDs, ",") != strings.Join([]string{at(2), at(3), at(4)}, ",") || result.Gap != nil {
		t.Fatalf("queue=%v gap=%v", result.PendingIDs, result.Gap)
	}
}

func TestGapsMergeRestartStaleCursorsAndReportATimelineEnd(t *testing.T) {
	at := func(h int) string { return statusAt(timelineNow.Add(-time.Duration(h) * time.Hour)) }
	t.Run("merge", func(t *testing.T) {
		got := withGap(&Gap{Cursor: "old", Floor: at(100)}, Gap{Cursor: "new", Floor: at(10)})
		if got.Cursor != "new" || got.Floor != at(100) {
			t.Fatalf("gap=%v", got)
		}
	})
	t.Run("stale cursor", func(t *testing.T) {
		s := gapTimeline(at)
		a := s.adapter(Options{BootstrapDays: 7, DiscoveryLimit: 50})
		known := map[string]bool{at(1): true, at(2): true, at(100): true}
		result, err := a.FetchRecent(context.Background(), "kano", Options{BootstrapDays: 7, DiscoveryLimit: 50, CursorID: at(1), KnownIDs: known, Gap: &Gap{Cursor: "expired", Floor: at(100)}})
		if err != nil {
			t.Fatal(err)
		}
		// The expired continuation restarts at the newest page within the
		// gap's budget of three pages and goes on from page 3 next time.
		if strings.Join(s.cursors, ",") != ",expired,,page-2" || result.Gap == nil || result.Gap.Cursor != "page-3" || result.Gap.Floor != at(100) {
			t.Fatalf("cursors=%q gap=%v", s.cursors, result.Gap)
		}
		if !slices.Contains(result.PendingIDs, at(4)) {
			t.Fatalf("queue=%v", result.PendingIDs)
		}
	})
	t.Run("timeline end", func(t *testing.T) {
		s := &timelineServer{pages: map[string]*http.Response{
			"":       response(200, timelineBody("page-2", at(1)+"@kano")),
			"page-2": response(200, timelineBody("", at(2)+"@kano")),
		}}
		a := s.adapter(Options{BootstrapDays: 7, DiscoveryLimit: 50})
		result, err := a.FetchRecent(context.Background(), "kano", Options{BootstrapDays: 7, DiscoveryLimit: 50, CursorID: at(1), KnownIDs: map[string]bool{at(1): true}, Gap: &Gap{Cursor: "page-2", Floor: at(100)}})
		if err != nil {
			t.Fatal(err)
		}
		if result.Gap != nil || len(result.Notes) != 1 || !strings.HasPrefix(result.Notes[0], string(ErrTimelineGap)) || !slices.Contains(result.PendingIDs, at(2)) {
			t.Fatalf("gap=%v notes=%v queue=%v", result.Gap, result.Notes, result.PendingIDs)
		}
	})
	t.Run("bootstrap", func(t *testing.T) {
		s := gapTimeline(at)
		a := s.adapter(Options{BootstrapDays: 7, DiscoveryLimit: 2})
		result, err := a.FetchRecent(context.Background(), "kano", a.Options)
		if err != nil {
			t.Fatal(err)
		}
		// Without a cursor the cap bounds the first run on purpose.
		if result.Gap != nil || len(result.PendingIDs) != 2 {
			t.Fatalf("gap=%v queue=%v", result.Gap, result.PendingIDs)
		}
	})
	t.Run("profile fallback", func(t *testing.T) {
		s := &timelineServer{pages: map[string]*http.Response{"": response(503, "unavailable")}}
		a := s.adapter(Options{BootstrapDays: 7, DiscoveryLimit: 50})
		result, err := a.FetchRecent(context.Background(), "kano", Options{BootstrapDays: 7, DiscoveryLimit: 50, CursorID: at(100), KnownIDs: map[string]bool{at(100): true}})
		if err != nil {
			t.Fatal(err)
		}
		// The profile's newest status is not stored, so the timeline reads
		// the rest from its newest page later.
		if result.Discovery != DiscoveryProfile || result.Gap == nil || result.Gap.Cursor != "" || result.Gap.Floor != at(100) {
			t.Fatalf("discovery=%s gap=%v", result.Discovery, result.Gap)
		}
	})
}
