package schedule

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"
)

// A merged duplicate stays hidden when its post is extracted again, including
// after the post stops listing it and lists it once more.
func TestReextractionKeepsMergedEventOutOfPublicReads(t *testing.T) {
	ctx := context.Background()
	f := messageFixture(t)
	f.scan()
	eventID := f.text(`SELECT id FROM events`)
	// The reservation of the same stream starts a few seconds early.
	f.exec(`INSERT INTO events (id, source, source_item_id, source_key, title, starts_on, starts_at, time_precision,
		event_type, url, provenance, manual_locked, created_at, updated_at)
		VALUES ('youtube-AAAAAAAAAAA', 'youtube', 'AAAAAAAAAAA', 'reservation', '【歌枠】synthetic', ?, '2099-05-03T11:59:16.000Z',
		'exact', 'stream', 'https://www.youtube.com/watch?v=AAAAAAAAAAA', 'automatic', 0, ?, ?)`, songDate, testTimestamp, testTimestamp)
	now := time.Date(2099, 5, 1, 0, 0, 0, 0, time.UTC)
	if counts, err := f.store.ReconcileEventMerges(ctx, now); err != nil || counts.Merged != 1 {
		t.Fatalf("reconcile=%+v %v", counts, err)
	}
	visible := func() []string {
		t.Helper()
		page, err := f.store.GetCalendarPage(ctx, songDate, songDate, 1, 10)
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, item := range page.Items {
			ids = append(ids, item.ID)
		}
		if page.Total != int64(len(ids)) || page.DayCounts[songDate] != int64(len(ids)) {
			t.Fatalf("calendar total=%d days=%v items=%v", page.Total, page.DayCounts, ids)
		}
		return ids
	}
	want := []string{"youtube-AAAAAAAAAAA"}
	if got := visible(); !slices.Equal(got, want) {
		t.Fatalf("visible=%v", got)
	}
	reply := func(title, date, clock, status string) {
		f.provider.respond = func(recordedRequest) (int, string) {
			return http.StatusOK, chatReply(scheduleReply(title, date, clock, status))
		}
		f.requestReprocess("post-1")
		f.scan()
	}
	merged := `SELECT status || '|' || (deleted_at IS NULL) || '|' || COALESCE(merged_into, '') FROM events WHERE id=?`

	reply(songTitle, songDate, songClock, "changed by model")
	if got := f.text(merged, eventID); got != "changed by model|1|youtube-AAAAAAAAAAA" {
		t.Fatalf("re-extracted event=%q", got)
	}
	if got := visible(); !slices.Equal(got, want) {
		t.Fatalf("re-extraction resurrected the duplicate: %v", got)
	}

	// The post stops listing the stream, then lists it again.
	reply("別枠", "2099-05-06", "20:00", "scheduled")
	if got := f.text(merged, eventID); got != "changed by model|0|youtube-AAAAAAAAAAA" {
		t.Fatalf("retired event=%q", got)
	}
	reply(songTitle, songDate, songClock, "scheduled")
	if got := f.text(merged, eventID); got != "scheduled|1|youtube-AAAAAAAAAAA" {
		t.Fatalf("listed again=%q", got)
	}
	if got := visible(); !slices.Equal(got, want) {
		t.Fatalf("relisting resurrected the duplicate: %v", got)
	}
	if counts, err := f.store.ReconcileEventMerges(ctx, now); err != nil || counts.Changed() {
		t.Fatalf("second reconcile=%+v %v", counts, err)
	}
}
