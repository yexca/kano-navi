package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

func TestYoutubeEventsShowTheirVideosTitleTranslations(t *testing.T) {
	path := testutil.NewSnapshot(t)
	for _, row := range []struct {
		id, title string
		manual    *string
	}{
		{"AAAAAAAAAAA", "歌枠 / singing", nil},
		{"BBBBBBBBBBB", "雑談", nil},
		{"CCCCCCCCCCC", "旧タイトル", strPtr("新タイトル")},
	} {
		testutil.Exec(t, path, `INSERT INTO videos (id,source,title,manual_title,url) VALUES (?,'youtube',?,?,?)`, row.id, row.title, row.manual, "https://www.youtube.com/watch?v="+row.id)
		for locale, text := range map[string]string{"en": row.id + " en", "zh-CN": row.id + " zh"} {
			testutil.Exec(t, path, `INSERT INTO translations (owner_type,owner_id,field,locale,origin,text,source_hash,created_at,updated_at) VALUES ('video',?,'title',?,'manual',?,'','','')`, row.id, locale, text)
		}
	}
	for _, row := range []struct{ id, title, url string }{
		// Any public link form names the same video.
		{"same", "歌枠 / singing", "https://youtu.be/AAAAAAAAAAA"},
		// An operator's or an extraction's own wording keeps the source.
		{"edited", "雑談（告知）", "https://www.youtube.com/watch?v=BBBBBBBBBBB"},
		// The effective title follows the operator's video correction.
		{"corrected", "新タイトル", "https://www.youtube.com/watch?v=CCCCCCCCCCC"},
		{"announcement", "歌枠 / singing", "https://x.com/example/status/1"},
	} {
		testutil.Exec(t, path, `INSERT INTO events (id,source,title,starts_on,starts_at,event_type,status,url,created_at,updated_at) VALUES (?,'youtube',?,'2026-10-24','2026-10-24T11:00:00.000Z','stream','scheduled',?,'','')`, row.id, row.title, row.url)
	}
	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	want := map[string]string{"same": "AAAAAAAAAAA zh", "edited": "", "corrected": "CCCCCCCCCCC zh", "announcement": ""}
	check := func(route string, events []DashboardEvent) {
		t.Helper()
		seen := 0
		for _, event := range events {
			expected, ok := want[event.ID]
			if !ok {
				continue
			}
			seen++
			if got := event.Translations.Locales["zh-CN"].Text; got != expected {
				t.Fatalf("%s %s zh-CN=%q, want %q", route, event.ID, got, expected)
			}
			if expected != "" && event.Translations.Locales["en"].Origin != TranslationOriginManual {
				t.Fatalf("%s %s en=%+v", route, event.ID, event.Translations.Locales["en"])
			}
		}
		if seen != len(want) {
			t.Fatalf("%s returned %d of %d events", route, seen, len(want))
		}
	}
	page, err := store.GetCalendarPage(context.Background(), "2026-10-19", "2026-10-25", 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	check("calendar", page.Items)
	dashboard, err := store.GetDashboard(context.Background(), 3, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	check("dashboard", dashboard.Events)
	if next := dashboard.Summary.NextEvent; next == nil || next.Translations.Locales["zh-CN"].Text != want[next.ID] {
		t.Fatalf("next event lost its translations: %+v", next)
	}
}
