package sqlite

import (
	"context"
	"testing"

	"github.com/yexca/kano-navi/backend/internal/testutil"
)

// The App reads the calendar, so an operator's label has to arrive on the
// event of the video it was set on.
func TestCalendarCarriesTheOperatorTag(t *testing.T) {
	path := testutil.NewSnapshot(t)
	testutil.Exec(t, path, `INSERT INTO videos(id,source,title,scheduled_at,url,manual_tag) VALUES
		('AAAAAAAAAAA','youtube','Song','2026-11-01T12:00:00.000Z','https://www.youtube.com/watch?v=AAAAAAAAAAA','song'),
		('BBBBBBBBBBB','youtube','Plain','2026-11-01T13:00:00.000Z','https://www.youtube.com/watch?v=BBBBBBBBBBB',NULL)`)
	testutil.Exec(t, path, `INSERT INTO events(id,source,source_item_id,source_key,title,starts_on,starts_at,url) VALUES
		('youtube-AAAAAAAAAAA','youtube','AAAAAAAAAAA','reservation','Song','2026-11-01','2026-11-01T12:00:00.000Z','https://www.youtube.com/watch?v=AAAAAAAAAAA'),
		('youtube-BBBBBBBBBBB','youtube','BBBBBBBBBBB','reservation','Plain','2026-11-01','2026-11-01T13:00:00.000Z','https://www.youtube.com/watch?v=BBBBBBBBBBB')`)
	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	page, err := store.GetCalendarPage(context.Background(), "2026-11-01", "2026-11-02", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	tags := map[string]string{}
	for _, item := range page.Items {
		if item.Tag != nil {
			tags[item.ID] = *item.Tag
		}
	}
	if len(page.Items) != 2 || tags["youtube-AAAAAAAAAAA"] != "song" {
		t.Fatalf("items=%d tags=%v", len(page.Items), tags)
	}
	if _, ok := tags["youtube-BBBBBBBBBBB"]; ok {
		t.Fatalf("an untagged video must not invent a tag: %v", tags)
	}
}
