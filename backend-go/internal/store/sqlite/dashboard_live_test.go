package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

func TestDashboardLiveShortlistGivesGraceOnlyToStreams(t *testing.T) {
	path := testutil.NewSnapshot(t)
	// Every activity started before the current Monday-to-Sunday week, so only
	// the live shortlist can return it.
	for _, row := range []struct {
		id, source, eventType string
		endsAt                *string
	}{
		{"stream-open", "x", "stream", nil},
		{"premiere-open", "youtube", "release", nil},
		{"premiere-ending", "youtube", "release", strPtr("2026-10-25T15:45:00.000Z")},
		{"premiere-ended", "youtube", "release", strPtr("2026-10-25T15:00:00.000Z")},
	} {
		testutil.Exec(t, path, `INSERT INTO events (id,source,title,starts_on,starts_at,ends_at,event_type,status,created_at,updated_at) VALUES (?,?,?,'2026-10-25','2026-10-25T14:30:00.000Z',?,?,'scheduled','2026-10-20T00:00:00.000Z','2026-10-20T00:00:00.000Z')`, row.id, row.source, row.id, row.endsAt, row.eventType)
	}
	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	dashboard, err := store.GetDashboard(context.Background(), 3, time.Date(2026, 10, 25, 15, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{"stream-open": true, "premiere-open": false, "premiere-ending": true, "premiere-ended": false} {
		if containsEvent(dashboard.Events, id) != want {
			t.Fatalf("event %q live shortlist inclusion = %v, want %v", id, !want, want)
		}
	}
}

func TestCalendarCarriesTheBoardsLiveStatus(t *testing.T) {
	path := testutil.NewSnapshot(t)
	now := time.Now().UTC()
	// The default probe interval trusts a live result for an hour; an end is final.
	for _, row := range []struct {
		id, status string
		age        time.Duration
	}{
		{"AAAAAAAAAAA", "live", 5 * time.Minute},
		{"BBBBBBBBBBB", "live", 3 * time.Hour},
		{"CCCCCCCCCCC", "ended", 48 * time.Hour},
	} {
		testutil.Exec(t, path, `INSERT INTO videos (id,source,title,url,live_status,live_checked_at) VALUES (?,'youtube',?,?,?,?)`, row.id, row.id, "https://www.youtube.com/watch?v="+row.id, row.status, now.Add(-row.age).Format(time.RFC3339Nano))
	}
	// Any public link form names the same video.
	for _, row := range []struct{ id, url string }{
		{"live", "https://www.youtube.com/live/AAAAAAAAAAA?si=share"},
		{"stale", "https://youtu.be/BBBBBBBBBBB"},
		{"ended", "https://www.youtube.com/watch?v=CCCCCCCCCCC"},
		{"announcement", "https://x.com/example/status/1"},
	} {
		testutil.Exec(t, path, `INSERT INTO events (id,source,title,starts_on,starts_at,event_type,status,url,created_at,updated_at) VALUES (?,'youtube',?,'2026-10-24','2026-10-24T11:00:00.000Z','stream','scheduled',?,'','')`, row.id, row.id, row.url)
	}
	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	page, err := store.GetCalendarPage(context.Background(), "2026-10-19", "2026-10-25", 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, event := range page.Items {
		got[event.ID] = "<nil>"
		if event.LiveStatus != nil {
			got[event.ID] = *event.LiveStatus
		}
	}
	for id, want := range map[string]string{"live": "live", "stale": "<nil>", "ended": "ended", "announcement": "<nil>"} {
		if got[id] != want {
			t.Fatalf("calendar live status=%v, want %q for %q", got, want, id)
		}
	}
}
