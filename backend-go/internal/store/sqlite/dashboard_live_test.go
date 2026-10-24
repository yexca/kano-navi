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
