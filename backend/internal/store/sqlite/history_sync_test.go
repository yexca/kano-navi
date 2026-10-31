package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/yexca/kano-navi/backend/internal/source/history"
	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
	ytsource "github.com/yexca/kano-navi/backend/internal/source/youtube"
)

func openHistoryStore(t *testing.T) (*Store, func(string, ...any) string) {
	t.Helper()
	s, err := OpenStandalone(filepath.Join(t.TempDir(), "database", "kano.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, func(statement string, args ...any) string {
		t.Helper()
		var value sql.NullString
		if err := s.db.QueryRow(statement, args...).Scan(&value); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
		return value.String
	}
}

func TestHistoryPageCommitsRecordsWithCheckpointAndKeepsIncrementalCursor(t *testing.T) {
	s, query := openHistoryStore(t)
	ctx := context.Background()
	if _, err := s.db.Exec(`INSERT INTO sync_state(source,account_id,cursor_id,cursor_time,last_success_at,metadata_json) VALUES('x','synthetic_handle','1900000000000000009','2026-03-09T00:00:00.000Z','2026-03-09T00:00:00.000Z','{"pendingStatusIds":["1900000000000000010"]}')`); err != nil {
		t.Fatal(err)
	}
	window := history.Window{Method: history.MethodAPI, Mode: history.ModeBefore, Days: 2, StartTime: "2026-03-03T00:00:00.000Z", EndTime: "2026-03-05T00:00:00.000Z"}
	post := xsource.Post{ID: "1900000000000000001", AccountHandle: "synthetic_handle", Type: "notice", Label: "SCHEDULE / 日程", Text: "weekly schedule", PublishedAt: "2026-03-04T03:00:00Z", URL: "https://x.com/synthetic_handle/status/1900000000000000001", MediaURLs: []string{"https://media.example.test/board.png"}, Raw: map[string]any{"id": "1900000000000000001"}}
	page := history.Page{
		Source: history.SourceX, Account: "synthetic_handle", CheckpointKey: "synthetic_handle:checkpoint", Window: window, Next: "page-2", Discovered: 1,
		Posts:              []xsource.Post{post},
		ScheduleCandidates: []xsource.ScheduleCandidate{{PostID: post.ID, SourceURL: post.URL, MediaURL: post.MediaURLs[0], AccountHandle: "synthetic_handle"}},
		Backfill:           &history.Backfill{Pending: true},
	}
	revision := query(`SELECT value FROM app_settings WHERE key='dashboard_revision'`)
	written, err := s.WriteHistoryPage(ctx, page)
	if err != nil || written.New != 1 || written.Stored != 0 || written.ScheduleAssets != 1 || len(written.NewIDs) != 1 || written.NewIDs[0] != post.ID {
		t.Fatalf("written=%+v err=%v", written, err)
	}
	for statement, want := range map[string]string{
		`SELECT text||'/'||media_url FROM posts WHERE id='1900000000000000001'`:                                                                          "weekly schedule/https://media.example.test/board.png",
		`SELECT COUNT(*) FROM media_links WHERE owner_type='post' AND owner_id='1900000000000000001'`:                                                    "1",
		`SELECT llm_status||'/'||manual_status FROM schedule_asset_reviews`:                                                                              "pending/unreviewed",
		`SELECT cursor_id FROM sync_state WHERE source='x-window' AND account_id='synthetic_handle:checkpoint'`:                                          "page-2",
		`SELECT json_extract(metadata_json,'$.window.endTime')||'/'||json_extract(metadata_json,'$.discovered') FROM sync_state WHERE source='x-window'`: "2026-03-05T00:00:00.000Z/1",
		// An open interval keeps its end as the cursor and remembers the window.
		`SELECT cursor_time||'/'||json_extract(metadata_json,'$.pendingWindow.startTime') FROM sync_state WHERE source='x-backfill' AND account_id='synthetic_handle'`: "2026-03-05T00:00:00.000Z/2026-03-03T00:00:00.000Z",
		// The incremental cursor and its pending queue belong to the recent sync.
		`SELECT cursor_id||'/'||cursor_time||'/'||metadata_json FROM sync_state WHERE source='x' AND account_id='synthetic_handle'`: `1900000000000000009/2026-03-09T00:00:00.000Z/{"pendingStatusIds":["1900000000000000010"]}`,
	} {
		if got := query(statement); got != want {
			t.Fatalf("%s = %q, want %q", statement, got, want)
		}
	}
	if query(`SELECT value FROM app_settings WHERE key='dashboard_revision'`) == revision {
		t.Fatal("stored records must tell browsers to reload")
	}
	// The last page closes the interval: the cursor moves to its start.
	page.Next, page.Posts, page.ScheduleCandidates, page.Backfill = "", nil, nil, &history.Backfill{}
	if _, err = s.WriteHistoryPage(ctx, page); err != nil {
		t.Fatal(err)
	}
	if got := query(`SELECT cursor_time||'/'||COALESCE(json_extract(metadata_json,'$.pendingWindow'),'closed') FROM sync_state WHERE source='x-backfill'`); got != "2026-03-03T00:00:00.000Z/closed" {
		t.Fatalf("closed backfill=%s", got)
	}
	if got := query(`SELECT COALESCE(cursor_id,'done') FROM sync_state WHERE source='x-window'`); got != "done" {
		t.Fatalf("checkpoint=%s", got)
	}
	// A post that another account already owns keeps its owner and content.
	post.Text = "claimed by another account"
	page.Account, page.Posts, page.Backfill = "other_handle", []xsource.Post{post}, nil
	if written, err = s.WriteHistoryPage(ctx, page); err != nil || written.New != 0 || written.Stored != 1 {
		t.Fatalf("written=%+v err=%v", written, err)
	}
	if got := query(`SELECT account_handle||'/'||text FROM posts WHERE id='1900000000000000001'`); got != "synthetic_handle/weekly schedule" {
		t.Fatalf("post owner=%s", got)
	}
}

func TestTimelinePageKeepsStoredPostsAndItsOwnCheckpoint(t *testing.T) {
	s, query := openHistoryStore(t)
	ctx := context.Background()
	if _, err := s.db.Exec(`INSERT INTO sync_state(source,account_id,cursor_id,cursor_time,last_success_at,metadata_json) VALUES('x','synthetic_handle','1900000000000000009','2026-03-09T00:00:00.000Z','2026-03-09T00:00:00.000Z','{"pendingStatusIds":["1900000000000000010"]}')`); err != nil {
		t.Fatal(err)
	}
	stored := xsource.Post{ID: "1900000000000000001", AccountHandle: "synthetic_handle", Type: "daily", Label: "DAILY / 近况", Text: "from the detail path", PublishedAt: "2026-03-04T03:00:00Z", URL: "https://x.com/synthetic_handle/status/1900000000000000001", MediaURLs: []string{"https://media.example.test/detail.png"}, Raw: map[string]any{"tweetID": "1900000000000000001"}}
	page := history.Page{Source: history.SourceX, Account: "synthetic_handle", CheckpointKey: "synthetic_handle:checkpoint", Posts: []xsource.Post{stored}}
	if _, err := s.WriteHistoryPage(ctx, page); err != nil {
		t.Fatal(err)
	}
	window := history.Window{Method: history.MethodTimeline, Mode: history.ModeAll}
	again := stored
	again.Text, again.MediaURLs = "from the timeline", []string{"https://media.example.test/timeline.png"}
	fresh := xsource.Post{ID: "1900000000000000002", AccountHandle: "synthetic_handle", Type: "notice", Label: "SCHEDULE / 日程", Text: "weekly schedule", PublishedAt: "2026-03-05T03:00:00Z", URL: "https://x.com/synthetic_handle/status/1900000000000000002", MediaURLs: []string{"https://media.example.test/board.png"}, Raw: map[string]any{"id": "1900000000000000002"}}
	timeline := history.Page{
		Source: history.SourceX, Account: "synthetic_handle", CheckpointKey: "synthetic_handle:checkpoint", Checkpoint: history.TimelineCheckpoint,
		Window: window, Next: "timeline-cursor", Discovered: 2, KeepStored: true, Posts: []xsource.Post{again, fresh},
		ScheduleCandidates: []xsource.ScheduleCandidate{
			{PostID: again.ID, SourceURL: again.URL, MediaURL: again.MediaURLs[0], AccountHandle: "synthetic_handle"},
			{PostID: fresh.ID, SourceURL: fresh.URL, MediaURL: fresh.MediaURLs[0], AccountHandle: "synthetic_handle"},
		},
	}
	written, err := s.WriteHistoryPage(ctx, timeline)
	if err != nil || written.New != 1 || written.Stored != 1 || written.ScheduleAssets != 1 || len(written.NewIDs) != 1 || written.NewIDs[0] != fresh.ID {
		t.Fatalf("written=%+v err=%v", written, err)
	}
	for statement, want := range map[string]string{
		// The stored post keeps its text, media and missing candidate.
		`SELECT text||'/'||media_url FROM posts WHERE id='1900000000000000001'`:                                   "from the detail path/https://media.example.test/detail.png",
		`SELECT COUNT(*) FROM assets WHERE id='schedule-x-1900000000000000001'`:                                   "0",
		`SELECT text FROM posts WHERE id='1900000000000000002'`:                                                   "weekly schedule",
		`SELECT COUNT(*) FROM media_links WHERE owner_id='1900000000000000002'`:                                   "1",
		`SELECT COUNT(*) FROM assets WHERE id='schedule-x-1900000000000000002'`:                                   "1",
		`SELECT cursor_id FROM sync_state WHERE source='x-timeline-window'`:                                       "timeline-cursor",
		`SELECT COUNT(*) FROM sync_state WHERE source='x-window' AND cursor_id='timeline-cursor'`:                 "0",
		`SELECT cursor_id||'/'||metadata_json FROM sync_state WHERE source='x' AND account_id='synthetic_handle'`: `1900000000000000009/{"pendingStatusIds":["1900000000000000010"]}`,
	} {
		if got := query(statement); got != want {
			t.Fatalf("%s = %q, want %q", statement, got, want)
		}
	}
	// Only X timeline windows name their own checkpoint source.
	for _, invalid := range []history.Page{
		{Source: history.SourceX, Account: "synthetic_handle", CheckpointKey: "k", Checkpoint: "x"},
		{Source: history.SourceYoutube, Account: "UCAAAAAAAAAAAAAAAAAAAAAA", CheckpointKey: "k", Checkpoint: history.TimelineCheckpoint},
	} {
		if _, err := s.WriteHistoryPage(ctx, invalid); err == nil {
			t.Fatalf("checkpoint %q for %s was accepted", invalid.Checkpoint, invalid.Source)
		}
	}
}

func TestHistoryVideosKeepStoredRowsOrRefreshThemWithoutReservationState(t *testing.T) {
	s, query := openHistoryStore(t)
	ctx := context.Background()
	if _, err := s.db.Exec(`INSERT INTO videos(id,source,title,published_at,scheduled_at,url,thumbnail_url,kind,is_upcoming,raw_json) VALUES('AAAAAAAAAAA','youtube','old title','2026-03-04T10:00:00Z','2026-03-20T10:00:00Z','https://www.youtube.com/watch?v=AAAAAAAAAAA',NULL,'UPCOMING LIVE',1,'{}')`); err != nil {
		t.Fatal(err)
	}
	window := history.Window{Method: history.MethodAPI, Mode: history.ModeAll}
	stored := ytsource.Video{ID: "AAAAAAAAAAA", Title: "new title", PublishedAt: "2026-03-04T10:00:00.000Z", URL: "https://www.youtube.com/watch?v=AAAAAAAAAAA", ThumbnailURL: "https://media.example.test/a.jpg", Kind: "VIDEO"}
	older := ytsource.Video{ID: "BBBBBBBBBBB", Title: "older", PublishedAt: "2026-02-20T10:00:00.000Z", URL: "https://www.youtube.com/watch?v=BBBBBBBBBBB", Kind: "VIDEO"}
	page := history.Page{
		Source: history.SourceYoutube, Account: "UCAAAAAAAAAAAAAAAAAAAAAA", CheckpointKey: "channel:checkpoint", Window: window, Discovered: 2,
		Videos: []ytsource.Video{stored, older}, KeepStored: true,
	}
	written, err := s.WriteHistoryPage(ctx, page)
	if err != nil || written.New != 1 || written.Stored != 1 || len(written.NewIDs) != 1 || written.NewIDs[0] != older.ID {
		t.Fatalf("written=%+v err=%v", written, err)
	}
	if got := query(`SELECT title FROM videos WHERE id='AAAAAAAAAAA'`); got != "old title" {
		t.Fatalf("a kept video changed: %s", got)
	}
	// A refresh rewrites the listing's fields; reservation state stays.
	page.KeepStored, page.Videos = false, []ytsource.Video{stored}
	if written, err = s.WriteHistoryPage(ctx, page); err != nil || written.New != 0 || written.Stored != 1 {
		t.Fatalf("refresh=%+v err=%v", written, err)
	}
	for statement, want := range map[string]string{
		`SELECT title||'/'||kind||'/'||is_upcoming||'/'||scheduled_at||'/'||thumbnail_url FROM videos WHERE id='AAAAAAAAAAA'`: "new title/UPCOMING LIVE/1/2026-03-20T10:00:00Z/https://media.example.test/a.jpg",
		`SELECT COUNT(*) FROM videos`:                                                    "2",
		`SELECT COUNT(*) FROM video_revisions WHERE video_id='AAAAAAAAAAA'`:              "1",
		`SELECT COUNT(*) FROM media_links WHERE owner_type='video' AND role='thumbnail'`: "1",
		`SELECT COUNT(*) FROM sync_state WHERE source='youtube'`:                         "0",
	} {
		if got := query(statement); got != want {
			t.Fatalf("%s = %q, want %q", statement, got, want)
		}
	}
	state, err := s.SyncState(ctx, "youtube-window", "channel:checkpoint")
	if err != nil || !state.Found || state.CursorID != "" || state.Metadata["discovered"] != float64(2) {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	// A listing read in one request writes no checkpoint, and so cannot carry
	// a backfill.
	feed := history.Page{Source: history.SourceYoutube, Account: "UCAAAAAAAAAAAAAAAAAAAAAA", Window: history.Window{Method: history.MethodRSS, Mode: history.ModeCount, Count: 5}, KeepStored: true, Videos: []ytsource.Video{older}}
	if written, err = s.WriteHistoryPage(ctx, feed); err != nil || written.Stored != 1 {
		t.Fatalf("feed=%+v err=%v", written, err)
	}
	if got := query(`SELECT COUNT(*) FROM sync_state WHERE source='youtube-window'`); got != "1" {
		t.Fatalf("checkpoints=%s", got)
	}
	feed.Backfill = &history.Backfill{}
	if _, err = s.WriteHistoryPage(ctx, feed); err == nil {
		t.Fatal("a backfill without a checkpoint was accepted")
	}
	if err = s.WriteSyncMetadata(ctx, "x-budget", "window-range", map[string]any{"lastAccount": "synthetic_handle"}); err != nil {
		t.Fatal(err)
	}
	if got := query(`SELECT COALESCE(last_success_at,'none')||'/'||json_extract(metadata_json,'$.lastAccount') FROM sync_state WHERE source='x-budget'`); got != "none/synthetic_handle" {
		t.Fatalf("rotation=%s", got)
	}
}

func TestStandaloneSeedsNoWorkflowsAndRestoresStaticWording(t *testing.T) {
	s, query := openHistoryStore(t)
	ctx := context.Background()
	// No workflows are seeded (ADR-0003): the table stays, empty.
	if got := query(`SELECT COUNT(*) FROM workflows`); got != "0" {
		t.Fatalf("workflows=%s", got)
	}
	if _, err := s.db.Exec(`UPDATE resources SET title='edited' WHERE id=9`); err != nil {
		t.Fatal(err)
	}
	if err := s.SeedStatic(ctx, false); err != nil {
		t.Fatal(err)
	}
	if got := query(`SELECT title FROM resources WHERE id=9`); got != "edited" {
		t.Fatalf("a plain seed replaced an operator edit: %s", got)
	}
	if err := s.SeedStatic(ctx, true); err != nil {
		t.Fatal(err)
	}
	if got := query(`SELECT title FROM resources WHERE id=9`); got != "official site" {
		t.Fatalf("overwrite kept %s", got)
	}
	counts, err := s.SnapshotCounts(ctx)
	if err != nil || counts["profiles"] != 1 || counts["posts"] != 0 || counts["events"] != 0 || counts["videos"] != 0 || counts["resources"] == 0 {
		t.Fatalf("counts=%v err=%v", counts, err)
	}
}

func TestStandaloneRebuildsLegacyEventsKeepingEveryRow(t *testing.T) {
	p, _ := olderSnapshot(t)
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	// The first events layout: one source per row and no calendar date.
	for _, statement := range []string{
		`DROP TABLE event_sources`, `DROP TABLE events`,
		`CREATE TABLE events (id TEXT PRIMARY KEY, source TEXT NOT NULL, title TEXT NOT NULL, detail TEXT, starts_at TEXT NOT NULL, ends_at TEXT, status TEXT, event_type TEXT DEFAULT 'event', url TEXT, is_upcoming INTEGER DEFAULT 0, raw_json TEXT)`,
		`CREATE INDEX events_starts_at_idx ON events (starts_at)`,
		`INSERT INTO events(id,source,title,detail,starts_at,status,event_type,url,is_upcoming,raw_json) VALUES('curated-1','x','curated event','from a post','2026-03-01T20:00:00.000Z','已预约','event','https://x.com/synthetic_handle/status/1',1,'{"kept":true}')`,
		`INSERT INTO events(id,source,title,starts_at,url) VALUES('youtube-AAAAAAAAAAA','youtube','stream','2026-03-02T10:00:00+09:00','https://www.youtube.com/watch?v=AAAAAAAAAAA')`,
	} {
		if _, err = db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	db.Close()
	s, err := OpenStandalone(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	query := func(statement string) string {
		t.Helper()
		var value sql.NullString
		if err := s.db.QueryRow(statement).Scan(&value); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
		return value.String
	}
	for statement, want := range map[string]string{
		// 20:00 UTC is already the next calendar day in Japan.
		`SELECT title||'/'||starts_on||'/'||starts_at||'/'||provenance||'/'||manual_locked||'/'||is_upcoming||'/'||raw_json FROM events WHERE id='curated-1'`: `curated event/2026-03-02/2026-03-01T20:00:00.000Z/manual/1/1/{"kept":true}`,
		`SELECT starts_on||'/'||provenance||'/'||manual_locked||'/'||timezone||'/'||cancellation_status FROM events WHERE id='youtube-AAAAAAAAAAA'`:           "2026-03-02/automatic/0/Asia/Tokyo/none",
		`SELECT source_item_id||'/'||source_key FROM events WHERE id='curated-1'`:                                                                             "curated-1/curated-1",
		`SELECT group_concat(event_id||':'||source||':'||source_item_id,',') FROM (SELECT * FROM event_sources ORDER BY event_id)`:                            "curated-1:x:curated-1,youtube-AAAAAAAAAAA:youtube:youtube-AAAAAAAAAAA",
		`SELECT COUNT(*) FROM sqlite_master WHERE name IN('events_legacy')`:                                                                                   "0",
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name IN('events_starts_at_idx','events_source_key_idx','event_sources_identity_idx')`:      "3",
		`SELECT text FROM posts WHERE id='synthetic-post'`:                                                                                                    "retained",
	} {
		if got := query(statement); got != want {
			t.Fatalf("%s = %q, want %q", statement, got, want)
		}
	}
	// The rebuilt snapshot is current: a second start changes nothing.
	s.Close()
	if s, err = OpenStandalone(p); err != nil {
		t.Fatal(err)
	}
	if got := query(`SELECT COUNT(*) FROM events`) + "/" + query(`SELECT COUNT(*) FROM event_sources`); got != "2/2" {
		t.Fatalf("second start rows=%s", got)
	}
}

func TestTheSameThumbnailInAnotherFormIsNotARevision(t *testing.T) {
	s, query := openHistoryStore(t)
	ctx := context.Background()
	const channel = "UCAAAAAAAAAAAAAAAAAAAAAA"
	watched := "https://i.ytimg.com/vi/AAAAAAAAAAA/maxresdefault.jpg?v=abc"
	write := func(thumbnail string) {
		t.Helper()
		if err := s.WriteYoutubeRecent(ctx, channel, ytsource.Result{Videos: []ytsource.Video{checkedVideo("AAAAAAAAAAA", "title", thumbnail)}}); err != nil {
			t.Fatal(err)
		}
	}
	write(watched)
	// A history page names the Data API's form of the same image; the stored
	// thumbnail and its media link stay.
	page := history.Page{
		Source: history.SourceYoutube, Account: channel, CheckpointKey: "channel:checkpoint",
		Window: history.Window{Method: history.MethodAPI, Mode: history.ModeAll},
		Videos: []ytsource.Video{{ID: "AAAAAAAAAAA", Title: "title", PublishedAt: "2026-01-02T09:00:00Z", URL: "https://www.youtube.com/watch?v=AAAAAAAAAAA", ThumbnailURL: "https://i.ytimg.com/vi/AAAAAAAAAAA/hqdefault.jpg", Kind: "MUSIC"}},
	}
	if _, err := s.WriteHistoryPage(ctx, page); err != nil {
		t.Fatal(err)
	}
	linked := `SELECT a.source_url FROM media_links ml JOIN media_assets a ON a.id=ml.media_id WHERE ml.owner_type='video' AND ml.owner_id='AAAAAAAAAAA' AND ml.role='thumbnail'`
	if got := query(`SELECT thumbnail_url FROM videos WHERE id='AAAAAAAAAAA'`); got != watched || query(linked) != watched {
		t.Fatalf("thumbnail=%s linked=%s", got, query(linked))
	}
	// A row an earlier history page left in the API's form is not replaced
	// by the watch page's form of that image either.
	if _, err := s.db.Exec(`UPDATE videos SET thumbnail_url='https://i.ytimg.com/vi/AAAAAAAAAAA/hqdefault.jpg' WHERE id='AAAAAAAAAAA'`); err != nil {
		t.Fatal(err)
	}
	write(watched)
	if got := query(`SELECT COUNT(*) FROM video_revisions WHERE video_id='AAAAAAAAAAA'`); got != "0" {
		t.Fatalf("revisions=%s", got)
	}
	// A new version of the image is a replacement.
	write("https://i.ytimg.com/vi/AAAAAAAAAAA/maxresdefault.jpg?v=def")
	if got := query(`SELECT COUNT(*)||'/'||MAX(thumbnail_url) FROM video_revisions WHERE video_id='AAAAAAAAAAA'`); got != "1/"+watched {
		t.Fatalf("revisions=%s", got)
	}
}
