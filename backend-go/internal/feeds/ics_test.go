package feeds

import (
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

func TestCalendarFoldsMultibyteTextWithoutSplittingCharacters(t *testing.T) {
	title := strings.Repeat("鹿乃まほろの歌枠、雑談;", 6)
	body := string(Calendar([]sqlite.DashboardEvent{{ID: "synthetic", Title: title + "\r\nsecond\x00line", StartsOn: "2026-10-28", TimePrecision: "unknown"}}, time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC), English))
	for _, line := range strings.Split(body, "\r\n") {
		if len(line) > 75 || !utf8.ValidString(line) {
			t.Fatalf("bad line %q", line)
		}
	}
	unfolded := strings.ReplaceAll(body, "\r\n ", "")
	want := "SUMMARY:" + strings.NewReplacer(",", `\,`, ";", `\;`, "、", "、").Replace(title) + `\nsecondline` + "\r\n"
	if !strings.Contains(unfolded, want) {
		t.Fatalf("summary not round-tripped:\n%s", unfolded)
	}
}

func TestCalendarDefaultsTheLengthOfTimedEventsWithoutAnEnd(t *testing.T) {
	text := func(value string) *string { return &value }
	start := "2026-10-28T11:00:00Z"
	body := string(Calendar([]sqlite.DashboardEvent{
		{ID: "stream", StartsOn: "2026-10-28", StartsAt: &start, TimePrecision: "exact", EventType: text("stream")},
		{ID: "member", StartsOn: "2026-10-28", StartsAt: &start, TimePrecision: "approximate", EventType: text(" member ")},
		{ID: "release", StartsOn: "2026-10-28", StartsAt: &start, TimePrecision: "exact", EventType: text("release")},
		{ID: "untyped", StartsOn: "2026-10-28", StartsAt: &start, TimePrecision: "exact"},
		// An end that does not follow the start counts as missing.
		{ID: "backwards", StartsOn: "2026-10-28", StartsAt: &start, EndsAt: text("2026-10-28T10:00:00Z"), TimePrecision: "exact", EventType: text("stream")},
	}, time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC), English))
	for id, end := range map[string]string{"stream": "120000", "member": "120000", "release": "113000", "untyped": "113000", "backwards": "120000"} {
		want := "UID:" + id + "@kano-navi\r\nDTSTAMP:20261027T000000Z\r\nDTSTART:20261028T110000Z\r\nDTEND:20261028T" + end + "Z\r\n"
		if !strings.Contains(body, want) {
			t.Fatalf("%s lacks %q:\n%s", id, want, body)
		}
	}
}

func TestCalendarSequenceFollowsTheUpdateTime(t *testing.T) {
	render := func(updated string) string {
		return string(Calendar([]sqlite.DashboardEvent{{ID: "synthetic", StartsOn: "2026-10-28", TimePrecision: "unknown", UpdatedAt: updated}}, time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC), English))
	}
	sequenceOf := func(body string) int64 {
		for _, line := range strings.Split(body, "\r\n") {
			if value, ok := strings.CutPrefix(line, "SEQUENCE:"); ok {
				parsed, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					t.Fatal(err)
				}
				return parsed
			}
		}
		t.Fatalf("no SEQUENCE:\n%s", body)
		return 0
	}
	first, offset := render("2026-10-26T09:30:00.000Z"), render("2026-10-26T09:30:01.250+09:00")
	if !strings.Contains(first, "LAST-MODIFIED:20261026T093000Z\r\n") || !strings.Contains(offset, "LAST-MODIFIED:20261026T003001Z\r\n") {
		t.Fatalf("LAST-MODIFIED not in UTC:\n%s\n%s", first, offset)
	}
	if again := render("2026-10-26T09:30:00.000Z"); sequenceOf(again) != sequenceOf(first) {
		t.Fatal("SEQUENCE changed without an update")
	}
	if later := render("2026-10-26T09:30:01.000Z"); sequenceOf(later) <= sequenceOf(first) {
		t.Fatal("SEQUENCE did not increase with the update time")
	}
	for _, missing := range []string{"", "not a time"} {
		if body := render(missing); strings.Contains(body, "SEQUENCE") || strings.Contains(body, "LAST-MODIFIED") {
			t.Fatalf("%q produced change markers:\n%s", missing, body)
		}
	}
}
