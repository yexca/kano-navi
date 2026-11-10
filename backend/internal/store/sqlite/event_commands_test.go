package sqlite

import (
	"context"
	"github.com/yexca/kano-navi/backend/internal/testutil"
	"testing"
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
