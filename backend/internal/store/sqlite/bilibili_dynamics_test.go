package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	bilisource "github.com/yexca/kano-navi/backend/internal/source/bilibili"
)

func biliStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenInitialized(filepath.Join(t.TempDir(), "data", "database", "kano.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func biliDynamic(text string, images ...string) bilisource.Dynamic {
	return bilisource.Dynamic{IDStr: "900000000000000010", OwnerMid: 316381099, Text: text, PublishedAt: "2026-11-07T04:04:48Z",
		Images: images, Raw: map[string]any{"type": "DYNAMIC_TYPE_WORD"}}
}

func TestBilibiliDynamicsArePrefixedPostsThatKeepHides(t *testing.T) {
	s := biliStore(t)
	ctx := context.Background()
	// An X status with the same digits is another post.
	if _, err := s.DB().Exec(`INSERT INTO posts (id, source, account_handle, type, text, published_at, url) VALUES ('900000000000000010', 'x', 'synthetic_handle', 'daily', 'x post', '2026-11-07T00:00:00Z', 'https://x.com/synthetic_handle/status/900000000000000010')`); err != nil {
		t.Fatal(err)
	}
	revision := func() int64 {
		t.Helper()
		value, err := s.GetDashboardRevision(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	start := revision()
	state := bilisource.State{Dynamics: bilisource.DynamicsState{LastAttemptAt: time.Now(), LastSuccessAt: time.Now()}}
	changed, err := s.WriteBilibiliDynamics(ctx, biliAccount, bilisource.DynamicsResult{Dynamics: []bilisource.Dynamic{biliDynamic("今天20点开播", "https://i0.hdslb.com/bfs/new_dyn/a.jpg")}, State: state})
	if err != nil || !changed || revision() != start+1 {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	var source, account, url, text string
	if err = s.DB().QueryRow(`SELECT source, account_handle, url, text FROM posts WHERE id = 'bilibili-900000000000000010'`).Scan(&source, &account, &url, &text); err != nil ||
		source != "bilibili" || account != biliAccount || url != "https://www.bilibili.com/opus/900000000000000010" || text != "今天20点开播" {
		t.Fatalf("post=%s %s %s %s err=%v", source, account, url, text, err)
	}
	var images int
	if err = s.DB().QueryRow(`SELECT COUNT(*) FROM media_links l JOIN media_assets a ON a.id = l.media_id WHERE l.owner_id = 'bilibili-900000000000000010' AND l.role = 'post-image' AND a.source = 'bilibili'`).Scan(&images); err != nil || images != 1 {
		t.Fatalf("images=%d err=%v", images, err)
	}
	// The same read changes nothing readers see.
	if changed, _ = s.WriteBilibiliDynamics(ctx, biliAccount, bilisource.DynamicsResult{Dynamics: []bilisource.Dynamic{biliDynamic("今天20点开播", "https://i0.hdslb.com/bfs/new_dyn/a.jpg")}, State: state}); changed || revision() != start+1 {
		t.Fatalf("unchanged read moved the revision: %v", changed)
	}
	// An operator's hide survives an edited dynamic.
	if _, err = s.DB().Exec(`UPDATE posts SET hidden_at = '2026-11-07T06:00:00Z' WHERE id = 'bilibili-900000000000000010'`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.WriteBilibiliDynamics(ctx, biliAccount, bilisource.DynamicsResult{Dynamics: []bilisource.Dynamic{biliDynamic("今天21点开播")}, State: state}); err != nil {
		t.Fatal(err)
	}
	var hidden string
	if err = s.DB().QueryRow(`SELECT COALESCE(hidden_at, '') FROM posts WHERE id = 'bilibili-900000000000000010'`).Scan(&hidden); err != nil || hidden == "" {
		t.Fatalf("hide lost: %q %v", hidden, err)
	}
	var xText string
	if err = s.DB().QueryRow(`SELECT text FROM posts WHERE id = '900000000000000010'`).Scan(&xText); err != nil || xText != "x post" {
		t.Fatalf("x post=%q %v", xText, err)
	}
	// The listing's clock and the WBI keys survive an upload read's write.
	state.WBI = bilisource.WBIKeys{Img: "0123456789abcdef0123456789abcdef", Sub: "fedcba9876543210fedcba9876543210", At: "2026-11-07T00:00:00Z"}
	if _, err = s.WriteBilibiliDynamics(ctx, biliAccount, bilisource.DynamicsResult{State: state}); err != nil {
		t.Fatal(err)
	}
	stored, err := s.BilibiliState(ctx, biliAccount)
	if err != nil || stored.WBI != state.WBI || stored.Dynamics.LastSuccessAt.IsZero() {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
	stored.LastAttemptAt = time.Now()
	if _, err = s.WriteBilibiliRecent(ctx, biliAccount, bilisource.Result{State: stored}); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.BilibiliState(ctx, biliAccount); again.WBI != state.WBI || again.Dynamics.LastSuccessAt.IsZero() {
		t.Fatalf("again=%#v", again)
	}
	status, err := s.BilibiliStatus(ctx, biliAccount)
	if err != nil || status.Dynamics.Posts != 1 || status.Dynamics.LastSuccessAt == nil {
		t.Fatalf("status=%#v err=%v", status, err)
	}
}

func TestBilibiliLiveMatchesStreamsByPlatformAndTime(t *testing.T) {
	s := biliStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	day := now.In(time.FixedZone("JST", 9*3600)).Format("2006-01-02")
	room := bilisource.RoomURL("1000001")
	// Two Bilibili streams the same day, an X notice of one with platform
	// bilibili, a release, and a stream on another platform.
	for _, row := range []struct{ id, source, kind, start, url, platform string }{
		{"bilibili-schedule-a", "bilibili", "stream", at(-10 * time.Minute), "https://www.bilibili.com/opus/1", "bilibili"},
		{"bilibili-schedule-c", "bilibili", "stream", at(-6 * time.Hour), "https://www.bilibili.com/opus/3", "bilibili"},
		{"x-schedule-a", "x", "member", at(-10 * time.Minute), "https://x.com/synthetic_handle/status/1", "bilibili"},
		{"bilibili-schedule-b", "bilibili", "release", at(-10 * time.Minute), "https://www.bilibili.com/opus/2", "bilibili"},
		{"x-schedule-o", "x", "stream", at(-10 * time.Minute), "https://x.com/synthetic_handle/status/2", "other"},
	} {
		if _, err := s.DB().Exec(`INSERT INTO events (id, source, title, starts_on, starts_at, event_type, url, platform, platforms, provenance, created_at, updated_at) VALUES (?, ?, '杂谈', ?, ?, ?, ?, ?, ?, 'automatic', ?, ?)`,
			row.id, row.source, day, row.start, row.kind, row.url, row.platform, EncodePlatforms([]string{row.platform}), row.start, row.start); err != nil {
			t.Fatal(err)
		}
	}
	settings, err := s.LiveProbeSettings(ctx)
	if err != nil || !settings.BilibiliEnabled || settings.BilibiliIntervalMinutes != BilibiliLiveDefaultMinutes {
		t.Fatalf("settings=%#v err=%v", settings, err)
	}
	// Both entries of the current stream are followed; never checked, due
	// now. The stream on another platform gets a sparse check 5 minutes
	// after its start.
	plan, err := s.BilibiliLivePlan(ctx, biliAccount, now, settings)
	if err != nil || len(plan.Targets) != 4 || plan.Phase != LivePhaseStarting || plan.DueAt.After(now) {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	if sparse := plan.Targets[3]; sparse.EventID != "x-schedule-o" || sparse.Phase != LivePhaseSparse || !sparse.DueAt.Equal(now.Add(-5*time.Minute)) {
		t.Fatalf("sparse=%#v", sparse)
	}
	find := func(events []DashboardEvent, id string) *DashboardEvent {
		for i := range events {
			if events[i].ID == id {
				return &events[i]
			}
		}
		return nil
	}
	board := func(at time.Time) *Dashboard {
		t.Helper()
		dashboard, err := s.GetDashboard(ctx, 3, at)
		if err != nil {
			t.Fatal(err)
		}
		return dashboard
	}
	// Before any check the room is unknown, so no entry has a live link.
	if a := find(board(now).Events, "bilibili-schedule-a"); a == nil || a.LiveURL != nil || *a.URL != "https://www.bilibili.com/opus/1" {
		t.Fatalf("before=%#v", a)
	}
	// The room is learned from the check; the broadcast started 5 minutes
	// before its announcement and matches the nearest entry only.
	live := bilisource.LiveState{Room: bilisource.Room{RoomID: "1000001", Title: "杂谈", LiveStatus: 1, LiveTime: at(-15 * time.Minute)}, CheckedAt: now, LastAttemptAt: now}
	if changed, err := s.WriteBilibiliLive(ctx, biliAccount, live); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	dashboard := board(now)
	stream, notice, release, other, earlier := find(dashboard.Events, "bilibili-schedule-a"), find(dashboard.Events, "x-schedule-a"),
		find(dashboard.Events, "bilibili-schedule-b"), find(dashboard.Events, "x-schedule-o"), find(dashboard.Events, "bilibili-schedule-c")
	if stream == nil || stream.LiveStatus == nil || *stream.LiveStatus != "live" || stream.LiveURL == nil || *stream.LiveURL != room || *stream.URL != "https://www.bilibili.com/opus/1" {
		t.Fatalf("stream=%#v", stream)
	}
	if notice == nil || notice.LiveStatus != nil || notice.LiveURL == nil || release == nil || release.LiveURL != nil || other == nil || other.LiveURL != nil ||
		earlier == nil || earlier.LiveStatus != nil || dashboard.LiveNow != nil {
		t.Fatalf("notice=%#v release=%#v other=%#v earlier=%#v liveNow=%#v", notice, release, other, earlier, dashboard.LiveNow)
	}
	// A stale check falls back to the start-time grace.
	if stale := find(board(now.Add(61*time.Minute)).Events, "bilibili-schedule-a"); stale != nil && stale.LiveStatus != nil {
		t.Fatalf("stale=%#v", stale)
	}
	// A later check off air ends the broadcast; the last live check is its
	// approximate end, and the ended stream leaves the plan.
	off := live
	off.Room.LiveStatus, off.Room.LiveTime, off.CheckedAt = 0, "", now.Add(time.Hour)
	if changed, err := s.WriteBilibiliLive(ctx, biliAccount, off); err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if ended := find(board(now.Add(time.Hour)).Events, "bilibili-schedule-a"); ended == nil || ended.LiveStatus == nil || *ended.LiveStatus != "ended" || ended.EndsAt != nil {
		t.Fatalf("ended=%#v", ended)
	}
	stored, err := s.BilibiliLiveState(ctx, biliAccount)
	if err != nil || !stored.LastLiveAt.Equal(now) || !stored.EndedAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
	if plan, _ = s.BilibiliLivePlan(ctx, biliAccount, now.Add(time.Hour), settings); len(plan.Targets) != 2 {
		t.Fatalf("after=%#v", plan)
	}
	// A broadcast no entry matches is liveNow, not an event; a rotation is
	// not live.
	surprise := off
	surprise.Room.LiveStatus, surprise.Room.LiveTime, surprise.CheckedAt = 1, at(8*time.Hour), now.Add(8*time.Hour)
	if _, err := s.WriteBilibiliLive(ctx, biliAccount, surprise); err != nil {
		t.Fatal(err)
	}
	later := board(now.Add(8 * time.Hour))
	if later.LiveNow == nil || later.LiveNow.Platform != "bilibili" || later.LiveNow.URL != room || later.LiveNow.Title != "杂谈" || later.LiveNow.StartedAt != workflowTime(now.Add(8*time.Hour)) {
		t.Fatalf("liveNow=%#v", later.LiveNow)
	}
	for _, e := range later.Events {
		if e.LiveStatus != nil && *e.LiveStatus == "live" {
			t.Fatalf("an entry claimed the unannounced broadcast: %#v", e)
		}
	}
	rotation := surprise
	rotation.Room.LiveStatus, rotation.Room.LiveTime, rotation.CheckedAt = 2, "", now.Add(8*time.Hour+time.Minute)
	if _, err := s.WriteBilibiliLive(ctx, biliAccount, rotation); err != nil {
		t.Fatal(err)
	}
	if b := board(now.Add(8*time.Hour + time.Minute)); b.LiveNow != nil {
		t.Fatalf("rotation=%#v", b.LiveNow)
	}
}

func TestStandaloneAddsTheEventPlatformColumn(t *testing.T) {
	p := filepath.Join(t.TempDir(), "snapshot.sqlite")
	s, err := OpenStandalone(p)
	if err != nil {
		t.Fatal(err)
	}
	const stamp = "2026-03-01T00:00:00.000Z"
	for _, statement := range []string{
		`ALTER TABLE events DROP COLUMN platform`,
		`INSERT INTO events(id,source,title,starts_on,provenance,manual_locked,created_at,updated_at) VALUES('youtube-a','youtube','a','2026-03-02','automatic',0,'` + stamp + `','` + stamp + `')`,
		`INSERT INTO events(id,source,title,starts_on,provenance,manual_locked,created_at,updated_at) VALUES('x-a','x','a','2026-03-02','automatic',0,'` + stamp + `','` + stamp + `')`,
	} {
		if _, err = s.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	if s, err = OpenStandalone(p); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for id, want := range map[string]string{"youtube-a": "youtube", "x-a": ""} {
		var got string
		if err = s.db.QueryRow(`SELECT COALESCE(platform,'') FROM events WHERE id=?`, id).Scan(&got); err != nil || got != want {
			t.Fatalf("%s platform=%q err=%v, want %q", id, got, err, want)
		}
	}
}

func TestBilibiliDynamicsStoreTheirVideoDynamicsAsUploads(t *testing.T) {
	s := biliStore(t)
	ctx := context.Background()
	upload := bilisource.Video{BVID: "BV1SYNTHEABC", AID: 113000000000001, Title: "翻唱《合成》", URL: "https://www.bilibili.com/video/BV1SYNTHEABC",
		ThumbnailURL: "https://i0.hdslb.com/bfs/archive/synthetic.jpg", Kind: bilisource.KindUpload, PublishedAt: "2026-11-07T04:00:00Z", DurationSeconds: 186}
	state := bilisource.State{Dynamics: bilisource.DynamicsState{LastAttemptAt: time.Now(), LastSuccessAt: time.Now()}}
	if _, err := s.WriteBilibiliDynamics(ctx, biliAccount, bilisource.DynamicsResult{Uploads: []bilisource.Video{upload}, State: state}); err != nil {
		t.Fatal(err)
	}
	var source, title, kind string
	var duration int
	if err := s.DB().QueryRow(`SELECT source,title,kind,duration_seconds FROM videos WHERE id='BV1SYNTHEABC'`).Scan(&source, &title, &kind, &duration); err != nil ||
		source != "bilibili" || title != "翻唱《合成》" || kind != "upload" || duration != 186 {
		t.Fatalf("video=%s %s %s %d err=%v", source, title, kind, duration, err)
	}
	// The upload read's newest cursor stays its own.
	var cursor sql.NullString
	if err := s.DB().QueryRow(`SELECT cursor_id FROM sync_state WHERE source='bilibili' AND account_id=?`, biliAccount).Scan(&cursor); err != nil || cursor.Valid && cursor.String != "" {
		t.Fatalf("cursor=%v err=%v", cursor, err)
	}
}

func TestBilibiliListingWithoutADescriptionKeepsTheStoredOne(t *testing.T) {
	s := biliStore(t)
	ctx := context.Background()
	upload := bilisource.Video{BVID: "BV1SYNTHEABC", AID: 113000000000001, Title: "翻唱《合成》", URL: "https://www.bilibili.com/video/BV1SYNTHEABC",
		Kind: bilisource.KindUpload, PublishedAt: "2026-11-07T04:00:00Z", DurationSeconds: 186, Description: "synthetic description"}
	if _, err := s.WriteBilibiliRecent(ctx, biliAccount, bilisource.Result{Videos: []bilisource.Video{upload}}); err != nil {
		t.Fatal(err)
	}
	upload.Description, upload.Title = "", "翻唱《合成》 renamed"
	if _, err := s.WriteBilibiliRecent(ctx, biliAccount, bilisource.Result{Videos: []bilisource.Video{upload}}); err != nil {
		t.Fatal(err)
	}
	var title, description string
	if err := s.DB().QueryRow(`SELECT title,json_extract(raw_json,'$.description') FROM videos WHERE id='BV1SYNTHEABC'`).Scan(&title, &description); err != nil ||
		title != "翻唱《合成》 renamed" || description != "synthetic description" {
		t.Fatalf("title=%q description=%q err=%v", title, description, err)
	}
}

// A partial upload (the dynamics' or the official client's) dates an upload
// by its own clock and may omit the aid: it keeps a stored upload's
// publication time, aid and form, so alternating listings change nothing,
// while the upload list still corrects the time.
func TestPartialUploadsKeepTheStoredTimeAidAndForm(t *testing.T) {
	s := biliStore(t)
	ctx := context.Background()
	listed := bilisource.Video{BVID: "BV1SYNTHEABC", AID: 113000000000001, Title: "翻唱《合成》", URL: "https://www.bilibili.com/video/BV1SYNTHEABC",
		Kind: bilisource.KindUpload, PublishedAt: "2026-11-07T04:00:00Z", DurationSeconds: 186, Description: "synthetic description"}
	if _, err := s.WriteBilibiliRecent(ctx, biliAccount, bilisource.Result{Videos: []bilisource.Video{listed}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`UPDATE videos SET kind='stream',kind_source='rule' WHERE id='BV1SYNTHEABC'`); err != nil {
		t.Fatal(err)
	}
	row := func() string {
		t.Helper()
		var out string
		if err := s.DB().QueryRow(`SELECT published_at||'|'||json_extract(raw_json,'$.aid')||'|'||json_extract(raw_json,'$.description')||'|'||kind||'|'||kind_source FROM videos WHERE id='BV1SYNTHEABC'`).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	stored := row()
	if stored != "2026-11-07T04:00:00Z|113000000000001|synthetic description|stream|rule" {
		t.Fatalf("stored=%s", stored)
	}
	app := listed
	app.PublishedAt, app.AID, app.Description, app.Partial = "2026-11-07T04:00:07Z", 0, "", true
	dynamic := listed
	dynamic.PublishedAt, dynamic.Description, dynamic.Partial = "2026-11-07T04:00:30Z", "synthetic", true
	for _, write := range []func() (bool, error){
		func() (bool, error) {
			return s.WriteBilibiliRecent(ctx, biliAccount, bilisource.Result{Videos: []bilisource.Video{app}, Fallback: "app"})
		},
		func() (bool, error) {
			return s.WriteBilibiliDynamics(ctx, biliAccount, bilisource.DynamicsResult{Uploads: []bilisource.Video{dynamic}})
		},
	} {
		if changed, err := write(); err != nil || changed {
			t.Fatalf("partial write changed=%v err=%v", changed, err)
		}
	}
	if got := row(); got != "2026-11-07T04:00:00Z|113000000000001|synthetic|stream|rule" {
		t.Fatalf("after partial writes=%s", got)
	}
	// A new upload takes a partial listing's time until the list reads it.
	fresh := app
	fresh.BVID, fresh.URL = "BV1SYNTHEABD", "https://www.bilibili.com/video/BV1SYNTHEABD"
	if changed, err := s.WriteBilibiliRecent(ctx, biliAccount, bilisource.Result{Videos: []bilisource.Video{fresh}, Fallback: "app"}); err != nil || !changed {
		t.Fatalf("new partial changed=%v err=%v", changed, err)
	}
	listed.PublishedAt = "2026-11-07T04:01:00Z"
	if changed, err := s.WriteBilibiliRecent(ctx, biliAccount, bilisource.Result{Videos: []bilisource.Video{listed}}); err != nil || !changed {
		t.Fatalf("list correction changed=%v err=%v", changed, err)
	}
	if got := row(); got != "2026-11-07T04:01:00Z|113000000000001|synthetic description|stream|rule" {
		t.Fatalf("after the list=%s", got)
	}
	var published string
	if err := s.DB().QueryRow(`SELECT published_at FROM videos WHERE id='BV1SYNTHEABD'`).Scan(&published); err != nil || published != "2026-11-07T04:00:07Z" {
		t.Fatalf("new partial published=%q err=%v", published, err)
	}
}
