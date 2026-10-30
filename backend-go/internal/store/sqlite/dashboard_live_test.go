package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

func TestDashboardVideoPoolKeepsEachKindVisible(t *testing.T) {
	path := testutil.NewSnapshot(t)
	base := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("work-%02d", i)
		when := base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		testutil.Exec(t, path, `INSERT INTO videos (id,source,title,published_at,scheduled_at,url,kind,is_short) VALUES (?,'youtube',?,?,?,?,?,?)`,
			id, id, when, when, "https://www.youtube.com/watch?v="+id, "MUSIC", 0)
	}
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("stream-%02d", i)
		when := base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		testutil.Exec(t, path, `INSERT INTO videos (id,source,title,published_at,scheduled_at,url,kind,is_short) VALUES (?,'youtube',?,?,?,?,?,?)`,
			id, id, when, when, "https://www.youtube.com/watch?v="+id, "LIVE ARCHIVE", 0)
	}
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("short-%02d", i)
		when := base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		testutil.Exec(t, path, `INSERT INTO videos (id,source,title,published_at,scheduled_at,url,kind,is_short) VALUES (?,'youtube',?,?,?,?,?,?)`,
			id, id, when, when, "https://www.youtube.com/watch?v="+id, "VIDEO", 1)
	}
	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	dashboard, err := store.GetDashboard(context.Background(), 3, time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{"works": 0, "archives": 0, "shorts": 0}
	for _, video := range dashboard.Videos {
		if video.IsShort != nil && *video.IsShort {
			counts["shorts"]++
		} else if video.Kind != nil && *video.Kind == "LIVE ARCHIVE" {
			counts["archives"]++
		} else {
			counts["works"]++
		}
	}
	if len(dashboard.Videos) != 30 || counts["works"] != dashboardWorksLimit || counts["archives"] != dashboardArchivesLimit || counts["shorts"] != dashboardShortsLimit {
		t.Fatalf("video pool=%d counts=%v", len(dashboard.Videos), counts)
	}
}

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
