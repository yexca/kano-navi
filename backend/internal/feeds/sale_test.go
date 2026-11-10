package feeds

import (
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

func TestCalendarShowsASalePeriodAtItsTwoEnds(t *testing.T) {
	text := func(value string) *string { return &value }
	sale := text("sale")
	body := string(Calendar([]sqlite.DashboardEvent{
		// Opens at noon on 28 October and closes at 23:59 on 22 November.
		{ID: "period", Title: "受注", StartsOn: "2026-10-28", StartsAt: text("2026-10-28T03:00:00.000Z"), EndsAt: text("2026-11-22T14:59:00.000Z"), EndsOn: text("2026-11-22"), TimePrecision: "exact", EventType: sale},
		// Only the deadline is known.
		{ID: "deadline", Title: "締切", StartsOn: "2026-11-05", EndsOn: text("2026-11-05"), TimePrecision: "unknown", EventType: sale},
		// Only the opening is known.
		{ID: "opening", Title: "物販", StartsOn: "2026-11-07", StartsAt: text("2026-11-07T03:00:00.000Z"), TimePrecision: "exact", EventType: sale},
		// A stored opening time of unknown precision makes an all-day
		// opening, as on the board, even with a close on the same date.
		{ID: "same-day", Title: "通販", StartsOn: "2026-11-09", StartsAt: text("2026-11-09T03:00:00.000Z"), EndsOn: text("2026-11-09"), TimePrecision: "unknown", EventType: sale},
	}, time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC), Japanese))
	for _, want := range []string{
		"UID:period@kano-navi\r\nDTSTAMP:20261027T000000Z\r\nDTSTART:20261028T030000Z\r\n",
		"DTSTART:20261028T030000Z\r\nDTEND:20261028T033000Z\r\nSUMMARY:受付開始：受注\r\n",
		"UID:period-close@kano-navi\r\n",
		"DTSTART:20261122T142900Z\r\nDTEND:20261122T145900Z\r\nSUMMARY:受付締切：受注\r\n",
		"UID:deadline@kano-navi\r\n",
		"DTSTART;VALUE=DATE:20261105\r\nDTEND;VALUE=DATE:20261106\r\nX-MICROSOFT-CDO-ALLDAYEVENT:TRUE\r\nSUMMARY:受付締切：締切\r\n",
		"DTSTART:20261107T030000Z\r\nDTEND:20261107T033000Z\r\nSUMMARY:受付開始：物販\r\n",
		"DTSTART;VALUE=DATE:20261109\r\nDTEND;VALUE=DATE:20261110\r\nX-MICROSOFT-CDO-ALLDAYEVENT:TRUE\r\nSUMMARY:受付開始：通販\r\n",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("calendar lacks %q:\n%s", want, body)
		}
	}
	if count := strings.Count(body, "BEGIN:VEVENT"); count != 5 {
		t.Fatalf("entries = %d:\n%s", count, body)
	}
}
