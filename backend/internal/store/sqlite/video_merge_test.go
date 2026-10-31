package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	bilisource "github.com/yexca/kano-navi/backend/internal/source/bilibili"
)

// videoMergeNow is the day after the 「奏」 premiere the fixtures copy.
var videoMergeNow = time.Date(2026, 10, 25, 3, 0, 0, 0, time.UTC)

type videoMergeFixture struct {
	t *testing.T
	s *Store
}

func newVideoMergeFixture(t *testing.T) *videoMergeFixture {
	t.Helper()
	s, err := OpenStandalone(filepath.Join(t.TempDir(), "database", "kano.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return &videoMergeFixture{t: t, s: s}
}

func (f *videoMergeFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.s.DB().Exec(query, args...); err != nil {
		f.t.Fatalf("%s: %v", query, err)
	}
}

func optionalDuration(seconds int) any {
	if seconds == 0 {
		return nil
	}
	return seconds
}

// youtube inserts a synthetic YouTube video. An empty scheduled time makes an
// ordinary upload and a zero duration an unknown length.
func (f *videoMergeFixture) youtube(id, title, published, scheduled string, duration int) {
	f.t.Helper()
	var start any
	if scheduled != "" {
		start = scheduled
	}
	f.exec(`INSERT INTO videos(id,source,title,published_at,scheduled_at,url,kind,duration_seconds) VALUES(?,'youtube',?,?,?,?,'MUSIC',?)`,
		id, title, published, start, "https://www.youtube.com/watch?v="+id, optionalDuration(duration))
}

func (f *videoMergeFixture) bilibili(id, title, published string, duration int) {
	f.t.Helper()
	f.exec(`INSERT INTO videos(id,source,title,published_at,url,kind,duration_seconds) VALUES(?,'bilibili',?,?,?,'MUSIC',?)`,
		id, title, published, "https://www.bilibili.com/video/"+id, optionalDuration(duration))
}

func (f *videoMergeFixture) reconcile() VideoMergeCounts {
	f.t.Helper()
	counts, err := f.s.ReconcileVideoMerges(context.Background(), videoMergeNow)
	if err != nil {
		f.t.Fatal(err)
	}
	return counts
}

// expect checks "target|source|reason" per upload, empty fields for NULL.
func (f *videoMergeFixture) expect(want map[string]string) {
	f.t.Helper()
	for id, state := range want {
		var into, source, reason sql.NullString
		if err := f.s.DB().QueryRow(`SELECT merged_into,merge_source,merge_reason FROM videos WHERE id=?`, id).Scan(&into, &source, &reason); err != nil {
			f.t.Fatal(err)
		}
		if got := into.String + "|" + source.String + "|" + reason.String; got != state {
			f.t.Errorf("%s = %q, want %q", id, got, state)
		}
	}
}

// translation is the video's Bilibili platform translation, or "".
func (f *videoMergeFixture) translation(id string) string {
	f.t.Helper()
	var text string
	err := f.s.DB().QueryRow(`SELECT text FROM translations WHERE owner_type='video' AND owner_id=? AND field='title' AND locale='zh-CN' AND origin='platform' AND platform='bilibili'`, id).Scan(&text)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		f.t.Fatal(err)
	}
	return text
}

func (f *videoMergeFixture) revision() string {
	f.t.Helper()
	var value sql.NullString
	if err := f.s.DB().QueryRow(`SELECT value FROM app_settings WHERE key='dashboard_revision'`).Scan(&value); err != nil && !errors.Is(err, sql.ErrNoRows) {
		f.t.Fatal(err)
	}
	return value.String
}

const (
	kanadeYoutube  = "e6acfMolexU"
	kanadeBilibili = "BV1CmHw6eEGE"
	kanadeRule     = kanadeYoutube + "|rule|" + videoMergeReasonRule
)

// realPair copies a premiere and the Bilibili upload of the same cover.
func (f *videoMergeFixture) realPair() {
	f.youtube(kanadeYoutube, "奏（スキマスイッチ） - 鹿乃まほろ cover.", "2026-10-20T12:00:00Z", "2026-10-24T09:00:09Z", 289)
	f.bilibili(kanadeBilibili, "鹿乃 翻唱《奏》", "2026-10-24T09:05:00Z", 289)
}

func TestVideoMergeRuleAttachesTheSameWork(t *testing.T) {
	f := newVideoMergeFixture(t)
	f.realPair()
	// The same song as a different video: a vertical Short a month after the
	// upload, and a same-day upload whose length the Short does not have.
	f.youtube("Tkit48gvqoM", "高嶺の花子さん 歌ってみた【鹿乃まほろ/ミリプロ】", "2026-10-23T11:00:00Z", "", 58)
	f.bilibili("BV1kkhv6JEyk", "鹿乃 翻唱《 高嶺の花子さん》", "2026-09-21T10:00:00Z", 263)
	f.bilibili("BV1sameday000", "鹿乃 翻唱《高嶺の花子さん》", "2026-10-23T11:30:00Z", 263)
	before := f.revision()
	if got := f.reconcile(); got != (VideoMergeCounts{Considered: 3, Merged: 1}) {
		t.Fatalf("counts = %+v", got)
	}
	f.expect(map[string]string{kanadeBilibili: kanadeRule, "BV1kkhv6JEyk": "||", "BV1sameday000": "||"})
	if got := f.translation(kanadeYoutube); got != "鹿乃 翻唱《奏》" {
		t.Fatalf("translation = %q", got)
	}
	merged := f.revision()
	if merged == before {
		t.Fatal("a merge must move the dashboard revision")
	}
	// A second run recomputes the same decision and writes nothing.
	if got := f.reconcile(); got != (VideoMergeCounts{Considered: 3, Kept: 1}) {
		t.Fatalf("second counts = %+v", got)
	}
	if f.revision() != merged {
		t.Fatal("an unchanged run must keep the revision")
	}
	// Known lengths further apart rule the Short out even as a suggestion.
	items, err := f.s.VideoMergeSuggestions(context.Background(), "BV1sameday000")
	if err != nil || len(items) != 1 || items[0].Status != "none" || len(items[0].Candidates) != 0 {
		t.Fatalf("short suggestion = %+v err=%v", items, err)
	}
}

func TestVideoMergeLeavesUnclearUploadsToTheOperator(t *testing.T) {
	f := newVideoMergeFixture(t)
	// A video read before lengths were stored.
	f.youtube("AAAAAAAAAAA", "ロキ - 鹿乃まほろ cover", "2026-10-09T00:00:00Z", "2026-10-10T10:00:00Z", 0)
	f.bilibili("BV1unknown000", "鹿乃 翻唱《ロキ》", "2026-10-10T10:30:00Z", 240)
	// Two videos with the same song and length.
	f.youtube("BBBBBBBBBBB", "シャルル 歌ってみた", "2026-10-12T10:00:00Z", "", 200)
	f.youtube("CCCCCCCCCCC", "シャルル 歌ってみた【MV】", "2026-10-12T12:00:00Z", "", 201)
	f.bilibili("BV1twice00000", "翻唱《シャルル》", "2026-10-12T11:00:00Z", 200)
	// Two uploads claiming one video.
	f.youtube("DDDDDDDDDDD", "夜に駆ける - cover", "2026-10-14T10:00:00Z", "", 250)
	f.bilibili("BV1claimA0000", "《夜に駆ける》", "2026-10-14T10:10:00Z", 250)
	f.bilibili("BV1claimB0000", "《夜に駆ける》 重投", "2026-10-14T10:20:00Z", 251)
	if got := f.reconcile(); got != (VideoMergeCounts{Considered: 4, Ambiguous: 3}) {
		t.Fatalf("counts = %+v", got)
	}
	for _, id := range []string{"BV1unknown000", "BV1twice00000", "BV1claimA0000", "BV1claimB0000"} {
		f.expect(map[string]string{id: "||"})
	}
	items, err := f.s.VideoMergeSuggestions(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]VideoMergeSuggestion{}
	order := []string{}
	for _, item := range items {
		got[item.Bilibili.ID] = item
		order = append(order, item.Bilibili.ID)
	}
	if len(order) != 4 || order[0] != "BV1claimB0000" || order[3] != "BV1unknown000" {
		t.Fatalf("order = %v", order)
	}
	unknown := got["BV1unknown000"]
	if unknown.Status != "unverified" || len(unknown.Candidates) != 1 {
		t.Fatalf("unknown = %+v", unknown)
	}
	if c := unknown.Candidates[0]; c.YouTube.ID != "AAAAAAAAAAA" || c.Complete || !c.TitleMatch || c.DurationDelta != nil || c.GapMinutes != 30 {
		t.Fatalf("unknown candidate = %+v", c)
	}
	if twice := got["BV1twice00000"]; twice.Status != "ambiguous" || len(twice.Candidates) != 2 || !twice.Candidates[0].Complete {
		t.Fatalf("twice = %+v", twice)
	}
	if claim := got["BV1claimA0000"]; claim.Status != "ambiguous" || len(claim.Candidates) != 1 || claim.Candidates[0].YouTube.ID != "DDDDDDDDDDD" {
		t.Fatalf("claim = %+v", claim)
	}

	ctx := context.Background()
	if counts, err := f.s.AdminCounts(ctx, "2026-10-25"); err != nil || counts.VideoMerges != 4 {
		t.Fatalf("decisions = %d err=%v", counts.VideoMerges, err)
	}
	// Keeping one claimant apart leaves the video to the other.
	video, err := f.s.KeepVideoSeparate(ctx, "BV1claimA0000", videoMergeNow)
	if err != nil || video.MergedInto != nil || stringValue(video.MergeSource) != MergeSourceManual {
		t.Fatalf("separate = %+v err=%v", video, err)
	}
	f.expect(map[string]string{"BV1claimA0000": "|manual|kept_separate"})
	if items, err = f.s.VideoMergeSuggestions(ctx, ""); err != nil {
		t.Fatal(err)
	}
	statuses := map[string]string{}
	for _, item := range items {
		statuses[item.Bilibili.ID] = item.Status
	}
	if _, listed := statuses["BV1claimA0000"]; listed || statuses["BV1claimB0000"] != "rule" {
		t.Fatalf("statuses after separate = %v", statuses)
	}
	if counts, err := f.s.AdminCounts(ctx, "2026-10-25"); err != nil || counts.VideoMerges != 2 {
		t.Fatalf("decisions after separate = %d err=%v", counts.VideoMerges, err)
	}
	if _, err = f.s.KeepVideoSeparate(ctx, "AAAAAAAAAAA", videoMergeNow); !errors.Is(err, ErrVideoMergeInvalid) {
		t.Fatalf("youtube separate err=%v", err)
	}
	if _, err = f.s.KeepVideoSeparate(ctx, "BV1missing000", videoMergeNow); !errors.Is(err, ErrVideoNotFound) {
		t.Fatalf("missing separate err=%v", err)
	}
}

func TestVideoMergeReleasesAMergeWhoseBasisChanged(t *testing.T) {
	f := newVideoMergeFixture(t)
	f.realPair()
	f.reconcile()
	f.expect(map[string]string{kanadeBilibili: kanadeRule})
	// The follow-up check finds a different length.
	f.exec(`UPDATE videos SET duration_seconds=320 WHERE id=?`, kanadeYoutube)
	before := f.revision()
	if got := f.reconcile(); got != (VideoMergeCounts{Considered: 1, Released: 1}) {
		t.Fatalf("counts = %+v", got)
	}
	f.expect(map[string]string{kanadeBilibili: "||"})
	if got := f.translation(kanadeYoutube); got != "" || f.revision() == before {
		t.Fatalf("release left translation %q or kept revision", got)
	}
}

func TestVideoMergeOperatorDecisionsOutrankTheRule(t *testing.T) {
	f := newVideoMergeFixture(t)
	ctx := context.Background()
	f.realPair()
	f.reconcile()

	// A merged upload is undone, not kept apart.
	if _, err := f.s.KeepVideoSeparate(ctx, kanadeBilibili, videoMergeNow); !errors.Is(err, ErrVideoMergeConflict) {
		t.Fatalf("separate a merged upload err=%v", err)
	}
	video, err := f.s.UnmergeVideo(ctx, kanadeBilibili, videoMergeNow)
	if err != nil || video.MergedInto != nil || stringValue(video.MergeSource) != MergeSourceManual {
		t.Fatalf("unmerge = %+v err=%v", video, err)
	}
	f.expect(map[string]string{kanadeBilibili: "|manual|unmerged_from:" + kanadeYoutube})
	if got := f.translation(kanadeYoutube); got != "" {
		t.Fatalf("undone merge kept translation %q", got)
	}
	// The rule never redoes an undone merge.
	if got := f.reconcile(); got != (VideoMergeCounts{}) {
		t.Fatalf("counts after undo = %+v", got)
	}
	if _, err = f.s.UnmergeVideo(ctx, kanadeBilibili, videoMergeNow); !errors.Is(err, ErrVideoNotMerged) {
		t.Fatalf("second undo err=%v", err)
	}
	if items, err := f.s.VideoMergeSuggestions(ctx, ""); err != nil || len(items) != 0 {
		t.Fatalf("an upload kept separate is no suggestion: %+v err=%v", items, err)
	}

	// The operator merges it again by watch link.
	video, err = f.s.MergeVideo(ctx, kanadeBilibili, "https://www.youtube.com/watch?v="+kanadeYoutube, videoMergeNow)
	if err != nil || stringValue(video.MergedInto) != kanadeYoutube || stringValue(video.MergedIntoTitle) != "奏（スキマスイッチ） - 鹿乃まほろ cover." {
		t.Fatalf("merge = %+v err=%v", video, err)
	}
	f.expect(map[string]string{kanadeBilibili: kanadeYoutube + "|manual|manual"})
	target, err := f.s.AdminVideoByID(ctx, kanadeYoutube)
	if err != nil || len(target.Alternates) != 1 || target.Alternates[0].ID != kanadeBilibili || f.translation(kanadeYoutube) != "鹿乃 翻唱《奏》" {
		t.Fatalf("target = %+v err=%v", target, err)
	}
	// Even a basis that no longer holds keeps an operator merge.
	f.exec(`UPDATE videos SET duration_seconds=320 WHERE id=?`, kanadeYoutube)
	if got := f.reconcile(); got != (VideoMergeCounts{}) {
		t.Fatalf("counts after manual merge = %+v", got)
	}
	f.expect(map[string]string{kanadeBilibili: kanadeYoutube + "|manual|manual"})

	// Another operator merge into the same video is a conflict.
	f.bilibili("BV1other00000", "鹿乃 翻唱《奏》 live", "2026-08-01T00:00:00Z", 300)
	if _, err = f.s.MergeVideo(ctx, "BV1other00000", kanadeYoutube, videoMergeNow); !errors.Is(err, ErrVideoMergeConflict) {
		t.Fatalf("conflict err=%v", err)
	}
	// A rule merge gives way to an operator merge into its video.
	f.youtube("FFFFFFFFFFF", "春 - 鹿乃まほろ cover", "2026-10-01T10:00:00Z", "", 180)
	f.bilibili("BV1spring0000", "鹿乃 翻唱《春》", "2026-10-01T10:30:00Z", 180)
	f.reconcile()
	f.expect(map[string]string{"BV1spring0000": "FFFFFFFFFFF|rule|" + videoMergeReasonRule})
	if _, err = f.s.MergeVideo(ctx, "BV1other00000", "FFFFFFFFFFF", videoMergeNow); err != nil {
		t.Fatal(err)
	}
	f.reconcile()
	f.expect(map[string]string{"BV1spring0000": "||", "BV1other00000": "FFFFFFFFFFF|manual|manual"})
	if got := f.translation("FFFFFFFFFFF"); got != "鹿乃 翻唱《奏》 live" {
		t.Fatalf("displaced translation = %q", got)
	}

	for _, c := range []struct {
		id, into string
		want     error
	}{
		{kanadeYoutube, "FFFFFFFFFFF", ErrVideoMergeInvalid},
		{"BV1other00000", "BV1spring0000", ErrVideoMergeInvalid},
		{"BV1other00000", "missing", ErrVideoMergeInvalid},
		{"BV1other00000", "https://media.example.test/watch?v=FFFFFFFFFFF", ErrVideoMergeInvalid},
		{"BV1missing000", kanadeYoutube, ErrVideoNotFound},
	} {
		if _, err = f.s.MergeVideo(ctx, c.id, c.into, videoMergeNow); !errors.Is(err, c.want) {
			t.Errorf("merge %s into %s err=%v, want %v", c.id, c.into, err, c.want)
		}
	}
}

func TestVideoMergeTranslationFollowsTitlesAndReadersListAlternates(t *testing.T) {
	f := newVideoMergeFixture(t)
	ctx := context.Background()
	f.realPair()
	f.reconcile()

	// A list read with a new title rewrites the translation.
	_, err := f.s.WriteBilibiliVideo(ctx, biliAccount, bilisource.Video{
		BVID: kanadeBilibili, AID: 1, OwnerMid: 316381099, Title: "鹿乃 翻唱《奏》 4K", Kind: "MUSIC",
		URL: "https://www.bilibili.com/video/" + kanadeBilibili, PublishedAt: "2026-10-24T09:05:00Z", DurationSeconds: 289,
	}, bilisource.State{})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.translation(kanadeYoutube); got != "鹿乃 翻唱《奏》 4K" {
		t.Fatalf("translation after read = %q", got)
	}
	// So does an operator's correction of the upload's title.
	title := "翻唱《奏》"
	if _, err = f.s.SetVideoOverrides(ctx, kanadeBilibili, VideoOverrides{SetTitle: true, Title: &title}, videoMergeNow); err != nil {
		t.Fatal(err)
	}
	if got := f.translation(kanadeYoutube); got != title {
		t.Fatalf("translation after correction = %q", got)
	}

	archive, err := f.s.GetVideoArchive(ctx, "2026-10-01", "2026-10-31", 1, 20, videoMergeNow)
	if err != nil || archive.Total != 1 {
		t.Fatalf("archive = %+v err=%v", archive, err)
	}
	item := archive.Items[0]
	if item.ID != kanadeYoutube || len(item.Alternates) != 1 || item.Alternates[0] != (VideoAlternate{Source: "bilibili", URL: "https://www.bilibili.com/video/" + kanadeBilibili}) {
		t.Fatalf("archive item = %+v", item)
	}
	if zh := item.Translations.Locales["zh-CN"]; zh.Text != title || zh.Origin != TranslationOriginPlatform || stringValue(zh.Platform) != "bilibili" {
		t.Fatalf("zh-CN = %+v", zh)
	}
	board, err := f.s.GetDashboard(ctx, 3, videoMergeNow)
	if err != nil || len(board.Videos) != 1 || len(board.Videos[0].Alternates) != 1 {
		t.Fatalf("board videos = %+v err=%v", board.Videos, err)
	}
}

func TestVideoTitlesMatchSongNames(t *testing.T) {
	for _, c := range []struct {
		bilibili, youtube string
		want              bool
	}{
		{"鹿乃 翻唱《奏》", "奏（スキマスイッチ） - 鹿乃まほろ cover.", true},
		{"鹿乃 翻唱《 高嶺の花子さん》", "高嶺の花子さん 歌ってみた【鹿乃まほろ/ミリプロ】", true},
		{"鹿乃 翻唱《まほろば》", "まほろば - 鹿乃まほろ cover", true},
		{"【鹿乃】夜に駆ける 翻唱", "夜に駆ける - 鹿乃まほろ cover", true},
		{"鹿乃 翻唱《ロキ》", "【歌ってみた】ロキ / 鹿乃まほろ", true},
		{"鹿乃 翻唱《ＲＯＫＩ》", "roki (cover)", true},
		// A one-character name must be the whole song title.
		{"鹿乃 翻唱《奏》", "【歌枠】奏でる夜", false},
		{"鹿乃 翻唱《シャルル》", "ロキ - 鹿乃まほろ cover", false},
		{"鹿乃 翻唱《》", "奏 - cover", false},
	} {
		b := mergeVideo{source: "bilibili", title: c.bilibili}
		y := mergeVideo{source: "youtube", title: c.youtube}
		if got := videoTitlesMatch(b, y); got != c.want {
			t.Errorf("%q / %q = %v, want %v", c.bilibili, c.youtube, got, c.want)
		}
	}
}
