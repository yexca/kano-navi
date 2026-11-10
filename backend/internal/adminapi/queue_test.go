package adminapi

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/schedule"
	ytsource "github.com/yexca/kano-navi/backend/internal/source/youtube"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

// queueConsole is a development console wired with the queue previews.
func queueConsole(t *testing.T, previews bool) *console {
	t.Helper()
	store, err := sqlite.OpenInitialized(filepath.Join(t.TempDir(), "database", "kano.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	options := Options{
		Store:    store,
		Auth:     newAuth(t, "development", ""),
		XHandles: []string{"synthetic_one"},
		Now:      func() time.Time { return time.Date(2026, 3, 2, 3, 0, 0, 0, time.UTC) },
	}
	if previews {
		extractor := schedule.New(store, schedule.Options{SecretsKey: "synthetic-secrets-key", Getenv: func(string) string { return "" }})
		translator := schedule.NewTranslator(store, schedule.Options{SecretsKey: "synthetic-secrets-key", Getenv: func(string) string { return "" }})
		options.Queues = QueuePreviews{Schedule: extractor.PreviewQueue, Translation: translator.PreviewQueue}
		options.Youtube = ytsource.NewAdapter(nil, ytsource.Options{ChannelID: "UCsynthetic"})
	}
	return &console{handler: New(options), store: store}
}

func TestQueuePreviewsNeedTheirSteps(t *testing.T) {
	c := queueConsole(t, false)
	for _, path := range []string{"/schedule", "/youtube", "/translate"} {
		if code, payload := c.call(t, http.MethodGet, "/api/admin/queue"+path, ""); code != http.StatusServiceUnavailable || payload["error"] != "unavailable" {
			t.Errorf("%s = %d %v", path, code, payload)
		}
	}
	if code, _ := c.call(t, http.MethodGet, "/api/admin/queue/x-refresh", ""); code != http.StatusOK {
		t.Errorf("x-refresh = %d", code)
	}
}

func TestScheduleQueuePreviewWritesNothing(t *testing.T) {
	c := queueConsole(t, true)
	c.exec(t, `INSERT INTO posts(id,source,account_handle,type,label,text,published_at,url) VALUES
	 ('p1','x','synthetic_one','post','','明日 21時から配信','2026-03-01T00:00:00.000Z','https://post.example.test/1')`)
	code, payload := c.call(t, http.MethodGet, "/api/admin/queue/schedule", "")
	if code != http.StatusOK {
		t.Fatalf("schedule = %d %v", code, payload)
	}
	queue := payload["queue"].(map[string]any)
	run := queue["run"].([]any)
	if len(run) != 1 {
		t.Fatalf("run = %v", run)
	}
	// Without a queue target the run would record a skip.
	entry := run[0].(map[string]any)
	if entry["postId"] != "p1" || entry["outcome"] != "skip" || entry["reason"] != "no_route_target" || entry["route"] != "message" || entry["source"] != "x" {
		t.Fatalf("entry = %v", entry)
	}
	if queue["limit"] != float64(20) || queue["candidates"] != float64(1) {
		t.Fatalf("queue = %v", queue)
	}
	if got := c.text(t, `SELECT COUNT(*) FROM post_llm_states`); got != "0" {
		t.Fatalf("the preview wrote %s states", got)
	}

	code, payload = c.call(t, http.MethodGet, "/api/admin/queue/translate", "")
	translate, _ := payload["queue"].(map[string]any)
	if code != http.StatusOK || translate["disabled"] != true || translate["reason"] != "no_route_target" {
		t.Fatalf("translate = %d %v", code, payload)
	}
	if _, ok := translate["candidates"].([]any); !ok {
		t.Fatalf("translate candidates = %v", translate["candidates"])
	}
}

func TestXRefreshQueueFollowsTheStep(t *testing.T) {
	c := queueConsole(t, false)
	for i := range 22 {
		c.exec(t, `INSERT INTO posts(id,source,account_handle,type,label,text,published_at,url,refresh_requested_at) VALUES (?,'x','Synthetic_One','post','','text','2026-03-01T00:00:00.000Z','https://post.example.test/a',?)`,
			fmt.Sprintf("1%02d", i), fmt.Sprintf("2026-03-01T01:00:%02d.000Z", 59-i))
	}
	c.exec(t, `INSERT INTO posts(id,source,account_handle,type,label,text,published_at,url,refresh_requested_at) VALUES
	 ('200','x','synthetic_related','post','','text','2026-03-01T00:00:00.000Z','https://post.example.test/b','2026-03-01T00:00:00.000Z'),
	 ('300','x','synthetic_one','post','','text','2026-03-01T00:00:00.000Z','https://post.example.test/c',NULL)`)
	code, payload := c.call(t, http.MethodGet, "/api/admin/queue/x-refresh", "")
	if code != http.StatusOK {
		t.Fatalf("x-refresh = %d %v", code, payload)
	}
	queue := payload["queue"].(map[string]any)
	entries := queue["entries"].([]any)
	if queue["total"] != float64(23) || len(entries) != 23 || queue["perAccount"] != float64(20) {
		t.Fatalf("queue = %v", queue)
	}
	statuses := map[string]int{}
	for _, item := range entries {
		statuses[item.(map[string]any)["status"].(string)]++
	}
	// Oldest request first; the related account is not read by the X step.
	first := entries[0].(map[string]any)
	second := entries[1].(map[string]any)
	if first["postId"] != "200" || first["status"] != "notRead" || second["postId"] != "121" || second["status"] != "next" {
		t.Fatalf("order = %v, %v", first, second)
	}
	// One run reads as many refreshes as its detail budget (12 by default)
	// gives the account, at most perAccount.
	if statuses["next"] != 12 || statuses["later"] != 10 || statuses["notRead"] != 1 {
		t.Fatalf("statuses = %v", statuses)
	}
	if last := entries[22].(map[string]any); last["postId"] != "100" || last["status"] != "later" {
		t.Fatalf("last = %v", last)
	}
	settings := sqlite.DefaultSyncSettings()
	settings.XMaxStatusRequests = 50
	if err := c.store.SaveSyncSettings(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	_, payload = c.call(t, http.MethodGet, "/api/admin/queue/x-refresh", "")
	statuses = map[string]int{}
	for _, item := range payload["queue"].(map[string]any)["entries"].([]any) {
		statuses[item.(map[string]any)["status"].(string)]++
	}
	if statuses["next"] != 20 || statuses["later"] != 2 {
		t.Fatalf("statuses with a budget of 50 = %v", statuses)
	}
}

func TestYoutubeQueueFollowsTheStepOrder(t *testing.T) {
	c := queueConsole(t, true)
	now := time.Now().UTC()
	iso := func(at time.Time) string { return at.Format("2006-01-02T15:04:05.000Z") }
	c.exec(t, `INSERT INTO videos(id,source,title,published_at,scheduled_at,url,is_upcoming,refresh_requested_at) VALUES
	 ('vid-refresh','youtube','Refresh',?,NULL,'https://www.youtube.com/watch?v=vid-refresh',0,?),
	 ('vid-queued','youtube','Queued',?,NULL,'https://www.youtube.com/watch?v=vid-queued',0,NULL),
	 ('vid-active','youtube','Active',?,?,'https://www.youtube.com/watch?v=vid-active',1,NULL)`,
		iso(now.AddDate(0, -2, 0)), iso(now.Add(-time.Hour)), iso(now.AddDate(0, -3, 0)), iso(now.AddDate(0, 0, -1)), iso(now.AddDate(0, 0, 2)))
	c.exec(t, `INSERT INTO sync_state(source,account_id,metadata_json) VALUES ('youtube-details','UCsynthetic','{"queue":["vid-queued","vid-refresh","vid-gone"]}')`)
	code, payload := c.call(t, http.MethodGet, "/api/admin/queue/youtube", "")
	if code != http.StatusOK {
		t.Fatalf("youtube = %d %v", code, payload)
	}
	queue := payload["queue"].(map[string]any)
	got := []string{}
	for _, item := range queue["entries"].([]any) {
		entry := item.(map[string]any)
		got = append(got, entry["id"].(string)+":"+entry["group"].(string)+":"+entry["title"].(string))
	}
	want := []string{"vid-refresh:refresh:Refresh", "vid-queued:queue:Queued", "vid-gone:queue:", "vid-active:active:Active"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	if queue["api"] != false || queue["apiVideos"] != float64(0) || queue["watchPages"] != float64(12) {
		t.Fatalf("budget = %v", queue)
	}
}
