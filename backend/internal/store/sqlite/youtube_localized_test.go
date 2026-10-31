package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ytsource "github.com/yexca/kano-navi/backend/internal/source/youtube"
)

func TestYoutubeLocalizedTitlesArePlatformTranslations(t *testing.T) {
	ctx := context.Background()
	s, err := OpenInitialized(filepath.Join(t.TempDir(), "database", "kano.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	revision := func() int64 {
		t.Helper()
		value, err := s.GetDashboardRevision(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	resolved := func(id string) map[string]Translation {
		t.Helper()
		sets, err := s.resolveTranslations(ctx, TranslationOwnerVideo, []string{id})
		if err != nil {
			t.Fatal(err)
		}
		return sets[id].Locales
	}
	candidates := func() string {
		t.Helper()
		items, err := s.ListTranslationCandidates(ctx, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), 20)
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, item := range items {
			ids = append(ids, item.OwnerID)
		}
		return strings.Join(ids, ",")
	}
	write := func(localized map[string]map[string]string, videos ...ytsource.Video) {
		t.Helper()
		if err := s.WriteYoutubeRecent(ctx, historyChannel, ytsource.Result{Videos: videos, Localized: localized}); err != nil {
			t.Fatal(err)
		}
	}

	// A new video and its localized titles land in one write; an ID that is
	// not a stored YouTube video is ignored.
	write(map[string]map[string]string{
		"AAAAAAAAAAA": {"en": "[Singing] Autumn night", "zh-CN": "【歌回】秋夜"},
		"BBBBBBBBBBB": {},
		"ZZZZZZZZZZZ": {"en": "not stored"},
	}, checkedVideo("AAAAAAAAAAA", "【歌枠】秋の夜", ""), checkedVideo("BBBBBBBBBBB", "【雑談】おはよう", ""))
	got := resolved("AAAAAAAAAAA")
	if en, zh := got["en"], got["zh-CN"]; en.Text != "[Singing] Autumn night" || en.Origin != TranslationOriginPlatform || en.Platform == nil || *en.Platform != "youtube" || zh.Text != "【歌回】秋夜" {
		t.Fatalf("translations=%+v", got)
	}
	var orphans int
	if err = s.DB().QueryRow(`SELECT COUNT(*) FROM translations WHERE owner_id='ZZZZZZZZZZZ'`).Scan(&orphans); err != nil || orphans != 0 {
		t.Fatalf("orphans=%d err=%v", orphans, err)
	}
	// Both locales are covered, so only the other video needs a model.
	if got := candidates(); got != "BBBBBBBBBBB" {
		t.Fatalf("candidates=%s", got)
	}

	// The same answer changes nothing readers see.
	start := revision()
	write(map[string]map[string]string{"AAAAAAAAAAA": {"en": "[Singing] Autumn night", "zh-CN": "【歌回】秋夜"}})
	if revision() != start {
		t.Fatal("an unchanged localization moved the revision")
	}
	// A manual row hides the platform one: changing it is not visible.
	if err = s.SetManualTranslation(ctx, TranslationOwnerVideo, "AAAAAAAAAAA", "en", "Operator title"); err != nil {
		t.Fatal(err)
	}
	start = revision()
	write(map[string]map[string]string{"AAAAAAAAAAA": {"en": "[Singing] An autumn night", "zh-CN": "【歌回】秋夜"}})
	if revision() != start {
		t.Fatal("a hidden platform change moved the revision")
	}
	var platformText string
	if err = s.DB().QueryRow(`SELECT text FROM translations WHERE owner_id='AAAAAAAAAAA' AND locale='en' AND origin='platform'`).Scan(&platformText); err != nil || platformText != "[Singing] An autumn night" {
		t.Fatalf("platform=%q err=%v", platformText, err)
	}

	// A localization the creator removed loses its row; the manual one stays.
	// Videos no call answered keep theirs.
	write(map[string]map[string]string{"AAAAAAAAAAA": {"en": "[Singing] An autumn night"}})
	if revision() != start+1 {
		t.Fatal("a removed localization must move the revision")
	}
	got = resolved("AAAAAAAAAAA")
	if _, kept := got["zh-CN"]; kept || got["en"].Origin != TranslationOriginManual || got["en"].Text != "Operator title" {
		t.Fatalf("translations=%+v", got)
	}
	write(map[string]map[string]string{"AAAAAAAAAAA": {}})
	if got := resolved("AAAAAAAAAAA"); len(got) != 1 || got["en"].Origin != TranslationOriginManual {
		t.Fatalf("translations=%+v", got)
	}
	write(nil)
	if got := resolved("AAAAAAAAAAA"); got["en"].Origin != TranslationOriginManual {
		t.Fatalf("translations=%+v", got)
	}

	// A live-status probe's answer is written the same way.
	start = revision()
	changed, err := s.WriteYoutubeLive(ctx, ytsource.ProbeResult{Localized: map[string]map[string]string{"BBBBBBBBBBB": {"zh-CN": "【杂谈】早上好"}}})
	if err != nil || !changed || revision() != start+1 || resolved("BBBBBBBBBBB")["zh-CN"].Text != "【杂谈】早上好" {
		t.Fatalf("changed=%v err=%v translations=%+v", changed, err, resolved("BBBBBBBBBBB"))
	}
}
