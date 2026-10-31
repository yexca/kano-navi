package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestPlatformTranslationsRankAndMoveTheRevision(t *testing.T) {
	ctx := context.Background()
	store, err := OpenInitialized(filepath.Join(t.TempDir(), "database", "kano.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.DB().Exec(`INSERT INTO videos (id, source, title, published_at, url, kind) VALUES ('v1', 'youtube', '奏 cover', '2026-10-01T00:00:00.000Z', 'https://video.example.test/v1', 'MUSIC')`); err != nil {
		t.Fatal(err)
	}
	revision := func() int64 {
		value, err := store.GetDashboardRevision(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	resolved := func() Translation {
		sets, err := store.resolveTranslations(ctx, TranslationOwnerVideo, []string{"v1"})
		if err != nil {
			t.Fatal(err)
		}
		return sets["v1"].Locales["zh-CN"]
	}
	write := func(platform, text string) {
		t.Helper()
		if err := store.UpsertPlatformTranslation(ctx, TranslationWrite{OwnerType: TranslationOwnerVideo, OwnerID: "v1", Field: "title", Locale: "zh-CN", Platform: platform, Text: text, SourceText: "奏 cover"}); err != nil {
			t.Fatal(err)
		}
	}
	start := revision()
	write("bilibili", "鹿乃 翻唱《奏》")
	if got := resolved(); got.Text != "鹿乃 翻唱《奏》" || got.Origin != TranslationOriginPlatform || *got.Platform != "bilibili" {
		t.Fatalf("bilibili=%+v", got)
	}
	if revision() != start+1 {
		t.Fatalf("a new platform row must move the revision")
	}
	write("bilibili", "鹿乃 翻唱《奏》")
	if revision() != start+1 {
		t.Fatalf("an unchanged platform row must not move the revision")
	}
	write("youtube", "奏（翻唱）")
	if got := resolved(); *got.Platform != "youtube" {
		t.Fatalf("a YouTube localization outranks a Bilibili title: %+v", got)
	}
	// Platform rows stay usable when the wording changes; their writer owns them.
	if _, err = store.DB().Exec(`UPDATE videos SET title = '奏 / cover' WHERE id = 'v1'`); err != nil {
		t.Fatal(err)
	}
	if got := resolved(); *got.Platform != "youtube" {
		t.Fatalf("platform row after a title change=%+v", got)
	}
	if err = store.DeletePlatformTranslation(ctx, TranslationOwnerVideo, "v1", "title", "zh-CN", "youtube"); err != nil {
		t.Fatal(err)
	}
	if got := resolved(); *got.Platform != "bilibili" {
		t.Fatalf("after delete=%+v", got)
	}
	if err = store.UpsertPlatformTranslation(ctx, TranslationWrite{OwnerType: TranslationOwnerVideo, OwnerID: "v1", Field: "text", Locale: "zh-CN", Platform: "youtube", Text: "x"}); !errors.Is(err, ErrTranslationInvalid) {
		t.Fatalf("a post field on a video must be rejected: %v", err)
	}
	if err = store.UpsertPlatformTranslation(ctx, TranslationWrite{OwnerType: TranslationOwnerVideo, OwnerID: "v1", Field: "title", Locale: "ja", Platform: "youtube", Text: "x"}); !errors.Is(err, ErrTranslationInvalid) {
		t.Fatalf("a Japanese target must be rejected: %v", err)
	}
}

func TestMachineTranslationForChangedWordingIsDropped(t *testing.T) {
	ctx := context.Background()
	store, err := OpenInitialized(filepath.Join(t.TempDir(), "database", "kano.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.DB().Exec(`INSERT INTO posts (id, source, type, text, published_at, url) VALUES ('p1', 'x', 'daily', 'こんばんは', '2026-10-01T00:00:00.000Z', 'https://x.example.invalid/p1')`); err != nil {
		t.Fatal(err)
	}
	result := MachineTranslation{OwnerType: TranslationOwnerPost, OwnerID: "p1", SourceHash: TranslationHash("こんにちは"), SourceLanguage: "ja", Texts: map[string]string{"en": "Hello", "zh-CN": "你好"}}
	saved, err := store.SaveMachineTranslation(ctx, result, time.Now())
	if err != nil || saved {
		t.Fatalf("saved=%v err=%v", saved, err)
	}
	var rows int
	if err = store.DB().QueryRow(`SELECT COUNT(*) FROM translations`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("rows=%d err=%v", rows, err)
	}
	result.SourceHash = TranslationHash("  こんばんは\n")
	result.Texts = map[string]string{"en": "Good evening", "zh-CN": "晚上好"}
	if saved, err = store.SaveMachineTranslation(ctx, result, time.Now()); err != nil || !saved {
		t.Fatalf("saved=%v err=%v", saved, err)
	}
	result.Texts["ja"] = "こんばんは"
	if _, err = store.SaveMachineTranslation(ctx, result, time.Now()); !errors.Is(err, ErrTranslationInvalid) {
		t.Fatalf("a Japanese machine row must be rejected: %v", err)
	}
}
