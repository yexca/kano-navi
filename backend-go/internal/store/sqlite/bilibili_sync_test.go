package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bilisource "github.com/yexca/kano-navi/backend-go/internal/source/bilibili"
	ytsource "github.com/yexca/kano-navi/backend-go/internal/source/youtube"
)

const biliAccount = "316381099"

func biliVideo(title, cover string) bilisource.Video {
	return bilisource.Video{
		BVID: "BV1AAAAAAAAA", AID: 1, OwnerMid: 316381099, Title: title, Kind: "MUSIC",
		URL: "https://www.bilibili.com/video/BV1AAAAAAAAA", ThumbnailURL: cover,
		PublishedAt: "2026-01-01T09:05:00Z", DurationSeconds: 289, Description: "synthetic description",
	}
}

func TestBilibiliReadsStoreUploadsHistoryAndBackoffState(t *testing.T) {
	root, path := snapshotUnderRoot(t)
	s, err := OpenWorkflowCore(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	at := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	session := bilisource.Session{Buvid3: "0A1B2C3D-0A1B-0A1B-0A1B-0A1B2C3D4E5F12345infoc", Buvid4: "synthetic-buvid4=="}
	state := bilisource.State{Session: session, SessionAt: at, LastAttemptAt: at, LastSuccessAt: at}
	first := biliVideo("鹿乃 翻唱《奏》", "https://i0.hdslb.com/bfs/archive/first.jpg")
	if _, err = s.WriteBilibiliRecent(ctx, biliAccount, bilisource.Result{Videos: []bilisource.Video{first}, State: state}); err != nil {
		t.Fatal(err)
	}
	// A captcha stores only the state; the known upload stays. A login cookie
	// that reached the state is never written.
	blocked := state
	blocked.LastAttemptAt, blocked.Failures, blocked.LastError, blocked.BlockedUntil = at.Add(4*time.Hour), 1, "bilibili_risk_control", at.Add(10*time.Hour)
	blocked.Session.Login = "synthetic0login%2C1767225600%2Cabcde"
	if _, err = s.WriteBilibiliRecent(ctx, biliAccount, bilisource.Result{Videos: []bilisource.Video{}, State: blocked}); err != nil {
		t.Fatal(err)
	}
	read, err := s.BilibiliState(ctx, biliAccount)
	if err != nil || read.Session != session || !read.BlockedUntil.Equal(blocked.BlockedUntil) || read.Failures != 1 || read.LastError != "bilibili_risk_control" || !read.LastSuccessAt.Equal(at) {
		t.Fatalf("state=%#v err=%v", read, err)
	}
	var stored string
	if err = s.DB().QueryRow(`SELECT metadata_json FROM sync_state WHERE source='bilibili'`).Scan(&stored); err != nil || strings.Contains(stored, "synthetic0login") {
		t.Fatalf("stored state=%s err=%v", stored, err)
	}
	// A later read reports a new title and cover; an operator correction stays.
	exec := func(statement string, args ...any) {
		t.Helper()
		if _, e := s.DB().Exec(statement, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`UPDATE videos SET manual_title='operator title' WHERE id='BV1AAAAAAAAA'`)
	second := biliVideo("鹿乃 翻唱《奏》（完整版）", "https://i1.hdslb.com/bfs/archive/second.jpg")
	changed, err := s.WriteBilibiliRecent(ctx, biliAccount, bilisource.Result{Videos: []bilisource.Video{second}, State: state})
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	var source, title, manual, thumbnail, raw string
	var duration int
	if err = s.DB().QueryRow(`SELECT source,title,manual_title,thumbnail_url,duration_seconds,raw_json FROM videos WHERE id='BV1AAAAAAAAA'`).Scan(&source, &title, &manual, &thumbnail, &duration, &raw); err != nil {
		t.Fatal(err)
	}
	if source != "bilibili" || title != second.Title || manual != "operator title" || thumbnail != second.ThumbnailURL || duration != 289 || !strings.Contains(raw, "synthetic description") {
		t.Fatalf("row=%s %s %s %s %d %s", source, title, manual, thumbnail, duration, raw)
	}
	var revisionTitle, revisionThumbnail string
	if err = s.DB().QueryRow(`SELECT title,thumbnail_url FROM video_revisions WHERE video_id='BV1AAAAAAAAA'`).Scan(&revisionTitle, &revisionThumbnail); err != nil || revisionTitle != first.Title || revisionThumbnail != first.ThumbnailURL {
		t.Fatalf("revision=%s %s err=%v", revisionTitle, revisionThumbnail, err)
	}

	// The cover is cached in its own namespace and served like other media.
	pending, err := s.PendingMedia(ctx, 10)
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending=%v err=%v", pending, err)
	}
	var cover PendingMedia
	for _, item := range pending {
		if item.SourceURL == second.ThumbnailURL {
			cover = item
		}
	}
	if cover.Source != "bilibili" {
		t.Fatalf("cover=%#v", cover)
	}
	content := []byte("\xff\xd8\xff synthetic cover")
	if err = s.SaveMediaDownload(ctx, cover, content, "image/jpeg", "", "", "ready", ""); err != nil {
		t.Fatal(err)
	}
	hash := sha256Hex(string(content))
	if _, err = os.Stat(filepath.Join(root, "bilibili", "sha256", hash[:2], hash+".jpg")); err != nil {
		t.Fatal(err)
	}
	if file, readErr := s.ReadMedia(ctx, cover.ID, hash); readErr != nil || string(file.Content) != string(content) {
		t.Fatalf("file=%v err=%v", file, readErr)
	}
}

func TestPublicReadersLeaveBilibiliUploadsInTheConsole(t *testing.T) {
	_, path := snapshotUnderRoot(t)
	s, err := OpenWorkflowCore(path, true)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = s.WriteYoutubeRecent(ctx, historyChannel, ytsource.Result{Videos: []ytsource.Video{checkedVideo("AAAAAAAAAAA", "奏 cover", "")}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.WriteBilibiliRecent(ctx, biliAccount, bilisource.Result{Videos: []bilisource.Video{biliVideo("鹿乃 翻唱《奏》", "")}}); err != nil {
		t.Fatal(err)
	}
	// Featuring a Bilibili upload does not bring it to the board either.
	if _, err = s.DB().Exec(`INSERT INTO app_settings(key,value,updated_at) VALUES('featured_video_id','BV1AAAAAAAAA','2026-01-02T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	board, err := s.GetDashboard(ctx, 3, now)
	if err != nil || len(board.Videos) != 1 || board.Videos[0].ID != "AAAAAAAAAAA" {
		t.Fatalf("board videos=%#v err=%v", board.Videos, err)
	}
	archive, err := s.GetVideoArchive(ctx, "2026-01-01", "2026-01-31", 1, 20, now)
	if err != nil || archive.Total != 1 || archive.Items[0].ID != "AAAAAAAAAAA" {
		t.Fatalf("archive=%#v err=%v", archive, err)
	}
	s.Close()
	admin, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	all, err := admin.ListAdminVideos(ctx, AdminVideoFilter{})
	if err != nil || all.Total != 2 {
		t.Fatalf("all=%#v err=%v", all, err)
	}
	only, err := admin.ListAdminVideos(ctx, AdminVideoFilter{Source: "bilibili"})
	if err != nil || only.Total != 1 || only.Items[0].Source != "bilibili" {
		t.Fatalf("bilibili=%#v err=%v", only, err)
	}
	var state sql.NullString
	if err = admin.DB().QueryRow(`SELECT metadata_json FROM sync_state WHERE source='bilibili'`).Scan(&state); err != nil || !state.Valid {
		t.Fatalf("state=%v err=%v", state, err)
	}
}
