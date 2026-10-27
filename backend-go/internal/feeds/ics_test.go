package feeds

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

func TestCalendarFoldsMultibyteTextWithoutSplittingCharacters(t *testing.T) {
	title := strings.Repeat("鹿乃まほろの歌枠、雑談;", 6)
	body := string(Calendar([]sqlite.DashboardEvent{{ID: "synthetic", Title: title + "\r\nsecond\x00line", StartsOn: "2026-10-28", TimePrecision: "unknown"}}, time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)))
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
