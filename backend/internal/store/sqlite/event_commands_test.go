package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/yexca/kano-navi/backend/internal/testutil"
)

func TestManualEventCommandsAreAtomicAndLocked(t *testing.T) {
	path := testutil.NewSnapshot(t)
	store, err := OpenAdminWrites(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e, err := store.CreateManualEvent(context.Background(), EventInput{ID: "manual-1", Title: "Synthetic", StartsOn: "2026-10-12"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Provenance != "manual" || !e.ManualLocked || e.TimePrecision != "unknown" {
		t.Fatalf("event=%+v", e)
	}
	if _, err = store.UpdateManualEvent(context.Background(), "manual-1", EventInput{Title: "Edited", StartsOn: "2026-10-12", CancellationStatus: "manual_confirmed"}); err == nil {
		t.Fatal("missing cancellation reason accepted")
	}
	if _, err = store.DeleteManualEvent(context.Background(), "manual-1"); err != nil {
		t.Fatal(err)
	}
	if e, err = store.DeleteManualEvent(context.Background(), "missing"); err == nil || e.ID != "" {
		t.Fatal("missing delete accepted")
	}
}

// An edited automatic entry keeps the extraction that produced it, and a
// single event reads back exactly as the console list shows it.
func TestEditedEventReadsBackAsTheListShowsIt(t *testing.T) {
	path := testutil.NewSnapshot(t)
	store, err := OpenAdminWrites(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if _, err = store.DB().ExecContext(ctx, `INSERT INTO events(id,source,source_item_id,title,starts_on,provenance,manual_locked,confidence,extraction_id,created_at,updated_at)
 VALUES('x-synthetic','x','100','Synthetic stream','2099-01-02','automatic',0,0.5,42,'2026-01-01T00:00:00.000Z','2026-01-01T00:00:00.000Z')`); err != nil {
		t.Fatal(err)
	}
	edited, err := store.UpdateManualEvent(ctx, "x-synthetic", EventInput{Title: "Edited stream", StartsOn: "2099-01-02"})
	if err != nil {
		t.Fatal(err)
	}
	if edited.ExtractionID == nil || *edited.ExtractionID != 42 || !edited.ManualLocked || edited.Title != "Edited stream" {
		t.Fatalf("edited=%+v extraction=%v", edited, edited.ExtractionID)
	}
	listed, err := store.ListAdminEvents(ctx, AdminEventFilter{Search: "Edited stream"})
	if err != nil || len(listed.Items) != 1 {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
	if !reflect.DeepEqual(edited, listed.Items[0]) {
		t.Fatalf("single read differs from the list:\n%+v\n%+v", edited, listed.Items[0])
	}
	if _, err = store.GetAdminEvent(ctx, "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing err=%v", err)
	}
}

func TestReadOnlyEventCommandsFailWithoutMutation(t *testing.T) {
	path := testutil.NewSnapshot(t)
	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.CreateManualEvent(context.Background(), EventInput{Title: "x", StartsOn: "2026-10-12"}); err != ErrReadOnly {
		t.Fatalf("err=%v", err)
	}
}

// A sale's close time falls on its close date in Japan, whichever caller
// writes it.
func TestManualSaleCloseTimeFallsOnItsCloseDate(t *testing.T) {
	path := testutil.NewSnapshot(t)
	store, err := OpenAdminWrites(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	for _, endsAt := range []string{"2026-11-20T10:00:00.000Z", "2026-11-06T15:00:00.000Z", "not-an-instant"} {
		if _, err = store.CreateManualEvent(ctx, EventInput{Title: "Synthetic goods", StartsOn: "2026-11-05", EndsOn: "2026-11-06", EndsAt: endsAt, EventType: "sale"}); err != ErrEventInvalid {
			t.Fatalf("endsAt %s: err=%v", endsAt, err)
		}
	}
	// 23:59 in Japan on 6 November.
	if _, err = store.CreateManualEvent(ctx, EventInput{Title: "Synthetic goods", StartsOn: "2026-11-05", EndsOn: "2026-11-06", EndsAt: "2026-11-06T14:59:00.000Z", EventType: "sale"}); err != nil {
		t.Fatal(err)
	}
}
