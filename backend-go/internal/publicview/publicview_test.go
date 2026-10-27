package publicview

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

// marker tags every value that belongs to the operator boundary. No public
// projection may carry it, whatever field it sits in.
const marker = "OPERATOR_ONLY"

// operatorKeys are field names of operator-only state in any spelling the
// store or the console uses.
var operatorKeys = []string{
	"raw_json", "rawJson", "source_item_id", "sourceItemId", "source_key", "sourceKey", "job_id", "jobId",
	"triggered_by", "triggeredBy", "counts_json", "last_error", "lastError", "error", "cache_path", "cachePath",
	"sha256", "etag", "provider_id", "providerId", "model_id", "modelId", "source_hash", "sourceHash",
	"confidence", "extraction_id", "extractionId", "deleted_at", "deletedAt", "merge_reason", "mergeReason",
	"failure_count", "failureCount",
}

func encode(t *testing.T, value any) (string, any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	return string(body), decoded
}

// operatorKeyPaths lists every object key in the decoded JSON that names
// operator-only state.
func operatorKeyPaths(value any, path string) []string {
	found := []string{}
	switch x := value.(type) {
	case map[string]any:
		for key, child := range x {
			for _, forbidden := range operatorKeys {
				if key == forbidden {
					found = append(found, path+"."+key)
				}
			}
			found = append(found, operatorKeyPaths(child, path+"."+key)...)
		}
	case []any:
		for _, child := range x {
			found = append(found, operatorKeyPaths(child, path+"[]")...)
		}
	}
	return found
}

func TestDashboardProjectionKeepsOperatorColumnsOut(t *testing.T) {
	path := testutil.NewSnapshot(t)
	// Every operator-only column carries the marker, including absolute
	// paths, raw provider errors and a credential-shaped message.
	testutil.Exec(t, path, `
	INSERT INTO profiles (id,display_name,x_url,updated_at,raw_json) VALUES ('p','Synthetic','https://x.example.invalid/synthetic','2026-01-02T00:00:00.000Z','{"token":"OPERATOR_ONLY_profile"}');
	INSERT INTO posts (id,source,account_handle,type,text,published_at,url,likes,reposts,replies,media_url,raw_json,quote_json) VALUES
	 ('post-1','x','synthetic','daily','hello','2026-01-02T00:00:00.000Z','https://x.example.invalid/status/1',1,2,3,'https://media.example.invalid/post.png','{"search_text":"OPERATOR_ONLY_post search text"}','{"id":"1900000000000000001","authorHandle":"quoted_synthetic","text":"quoted wording","url":"https://x.com/quoted_synthetic/status/1900000000000000001","rawJson":"OPERATOR_ONLY_quote","mediaURLs":["https://media.example.invalid/OPERATOR_ONLY.png"]}');
	INSERT INTO post_llm_states (post_id,status,last_error,failure_fingerprint,updated_at) VALUES ('post-1','failed','OPERATOR_ONLY_llm upstream said Bearer sk-OPERATOR_ONLY','OPERATOR_ONLY_fingerprint','');
	INSERT INTO translations (owner_type,owner_id,field,locale,origin,platform,text,source_hash,provider_id,model_id,created_at,updated_at) VALUES
	 ('post','post-1','text','en','manual','','hello in English','OPERATOR_ONLY_hash','OPERATOR_ONLY_provider','OPERATOR_ONLY_model','','');
	INSERT INTO translation_states (owner_type,owner_id,field,source_hash,source_language,status,last_error,updated_at) VALUES ('post','post-1','text','OPERATOR_ONLY_state','ja','failed','OPERATOR_ONLY_translate','');
	INSERT INTO videos (id,source,title,published_at,url,thumbnail_url,kind,is_upcoming,raw_json,manual_thumbnail_sha256,merge_reason) VALUES
	 ('video-1','youtube','Video','2026-01-01T00:00:00.000Z','https://video.example.invalid/watch?v=1','https://media.example.invalid/thumb.png','video',0,'{"etag":"OPERATOR_ONLY_video"}','OPERATOR_ONLY_sha','OPERATOR_ONLY_merge');
	INSERT INTO events (id,source,source_item_id,source_key,title,starts_on,starts_at,timezone,time_precision,status,event_type,provenance,manual_locked,confidence,extraction_id,cancellation_status,cancellation_source_item_id,created_at,updated_at,raw_json,merge_reason) VALUES
	 ('event-1','x','OPERATOR_ONLY_item','OPERATOR_ONLY_key','Event','2026-01-03','2026-01-03T20:00:00+09:00','Asia/Tokyo','exact','scheduled','event','automatic',0,0.42,7,'none','OPERATOR_ONLY_cancel_item','','','{"prompt":"OPERATOR_ONLY_event"}','OPERATOR_ONLY_event_merge');
	INSERT INTO event_sources (event_id,source,source_item_id,source_key,url,raw_json,created_at,updated_at) VALUES ('event-1','x','OPERATOR_ONLY_source','OPERATOR_ONLY_source_key','https://x.example.invalid/status/1','{"x":"OPERATOR_ONLY_event_source"}','','');
	INSERT INTO media_assets (id,source,source_url,cache_path,sha256,status,etag,last_error,raw_json) VALUES
	 ('media-post','x','https://media.example.invalid/post.png','/srv/OPERATOR_ONLY/data/x/post.png','OPERATOR_ONLY_media_sha','pending','OPERATOR_ONLY_etag','OPERATOR_ONLY_download C:\OPERATOR_ONLY\data','{"OPERATOR_ONLY":1}');
	INSERT INTO media_links (media_id,owner_type,owner_id,role,position) VALUES ('media-post','post','post-1','image',0),('media-post','post','post-1','quote-image',0);
	INSERT INTO profile_media (id,slot,source,source_url,active_cache_path,active_sha256,status,is_active,raw_json) VALUES
	 ('avatar-1','avatar','x','https://media.example.invalid/avatar.png','/srv/OPERATOR_ONLY/avatar.png','OPERATOR_ONLY_avatar_sha','pending',1,'{"OPERATOR_ONLY":2}');
	INSERT INTO assets (id,kind,url,source_url,alt,week_start,period_start,period_end,period_basis,source_account,updated_at,raw_json) VALUES
	 ('asset-1','schedule','https://media.example.invalid/schedule.png','https://x.example.invalid/status/1','schedule','2025-12-29','2025-12-29','2026-01-04','explicit','synthetic','2026-01-01T00:00:00.000Z','{"OPERATOR_ONLY":3}');
	INSERT INTO schedule_asset_reviews (asset_id,llm_status,manual_status,updated_at) VALUES ('asset-1','pending','schedule','');
	INSERT INTO focus (id,title,url,updated_at,raw_json) VALUES (1,'Focus','https://video.example.invalid/watch?v=1','2026-01-01T00:00:00.000Z','{"OPERATOR_ONLY":4}');
	INSERT INTO timeline (id,year,title,raw_json) VALUES (1,'2026','Debut','{"OPERATOR_ONLY":5}');
	INSERT INTO resources (id,title,url,raw_json) VALUES (1,'Link','https://example.invalid/','{"OPERATOR_ONLY":6}');
	INSERT INTO sync_state (source,account_id,cursor_id,cursor_time,last_success_at,metadata_json) VALUES ('x','OPERATOR_ONLY_account','OPERATOR_ONLY_cursor','2026-01-02T00:00:00.000Z','2026-01-02T00:00:00.000Z','{"OPERATOR_ONLY":7}');
	INSERT INTO sync_runs (source,started_at,finished_at,status,message,counts_json,triggered_by,job_id) VALUES
	 ('workflow','2026-01-02T00:00:00.000Z','2026-01-02T00:01:00.000Z','failed','OPERATOR_ONLY dial tcp 192.0.2.1: refused OPERATOR_ONLY_reason','{"x":{"OPERATOR_ONLY":8}}','OPERATOR_ONLY_trigger','OPERATOR_ONLY_job');`)
	store, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	dashboard, err := store.GetDashboard(ctx, 30, time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.GetLatestSync(ctx)
	if err != nil {
		t.Fatal(err)
	}

	projections := map[string]any{
		"dashboard": Dashboard(dashboard),
		"health":    Health(time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), run),
	}
	for _, e := range dashboard.Events {
		projections["event "+e.ID] = PublicEvent(e, false)
	}
	for _, p := range dashboard.Posts {
		projections["post "+p.ID] = PublicPost(p)
	}
	for _, v := range dashboard.Videos {
		projections["video "+v.ID] = PublicVideo(v)
	}
	for _, a := range dashboard.Assets {
		projections["asset "+a.ID] = PublicAsset(a)
	}
	for name, projection := range projections {
		body, decoded := encode(t, projection)
		if strings.Contains(body, marker) || strings.Contains(body, "/srv/") || strings.Contains(body, `C:\\`) {
			t.Errorf("%s leaks operator data: %s", name, body)
		}
		if keys := operatorKeyPaths(decoded, name); len(keys) > 0 {
			t.Errorf("%s has operator fields %v", name, keys)
		}
	}

	// The records the markers ride on were all projected.
	body, _ := encode(t, projections["dashboard"])
	for _, id := range []string{`"post-1"`, `"video-1"`, `"event-1"`, `"asset-1"`, `"avatar-1"`, `"media-post"`, `"hello in English"`, `"Debut"`, `"quoted wording"`} {
		if !strings.Contains(body, id) {
			t.Fatalf("dashboard lacks %s: %s", id, body)
		}
	}
	for _, name := range []string{"event event-1", "post post-1", "video video-1", "asset asset-1"} {
		if projections[name] == nil {
			t.Fatalf("%s was not projected on its own", name)
		}
	}
}

func TestPublicSyncRunReplacesTheStoredMessage(t *testing.T) {
	finished := "2026-01-02T00:01:00.000Z"
	for status, message := range map[string]string{
		"success": "同步完成",
		"partial": "部分数据源不可用或存在警告，保留已有快照",
		"failed":  "同步失败，保留已有快照",
		"running": "同步进行中",
	} {
		got := PublicSyncRun(&sqlite.SyncRun{ID: 3, Source: "workflow", StartedAt: "2026-01-02T00:00:00.000Z", FinishedAt: &finished, Status: status})
		if got.Message == nil || *got.Message != message || got.ID != 3 || got.Source != "workflow" || got.FinishedAt != &finished || got.Status != status {
			t.Fatalf("%s: %+v", status, got)
		}
	}
	if got := PublicSyncRun(&sqlite.SyncRun{Status: "unexpected"}); got.Message != nil {
		t.Fatalf("unknown status message=%q", *got.Message)
	}
	if PublicSyncRun(nil) != nil {
		t.Fatal("missing run was projected")
	}
}

func TestHealthAndRevision(t *testing.T) {
	health := Health(time.Date(2026, 1, 2, 12, 34, 56, 789000000, time.FixedZone("JST", 9*60*60)), nil)
	if !health.OK || health.Service != "kano-status-board" || health.Now != "2026-01-02T03:34:56.789Z" || health.LastSync != nil {
		t.Fatalf("health=%+v", health)
	}
	same, other := int64(5), int64(4)
	for _, c := range []struct {
		since *int64
		want  bool
	}{{nil, true}, {&same, false}, {&other, true}} {
		if got := Revision(5, c.since); got.Revision != 5 || got.Changed != c.want {
			t.Fatalf("since=%v got=%+v", c.since, got)
		}
	}
}

func TestPublicEventExtrasAreDashboardOnly(t *testing.T) {
	live := "live"
	event := sqlite.DashboardEvent{ID: "e", Title: "Event", ManualLocked: true, IsUpcoming: true, LiveStatus: &live}
	extras := []string{"manualLocked", "isUpcoming", "liveStatus"}
	full, compact := PublicEvent(event, true), PublicEvent(event, false)
	for _, key := range extras {
		if _, ok := compact[key]; ok {
			t.Fatalf("compact event has %s: %v", key, compact)
		}
	}
	if full["manualLocked"] != true || full["isUpcoming"] != true || full["liveStatus"] != "live" || len(full) != len(compact)+len(extras) {
		t.Fatalf("full event=%v", full)
	}
}

func TestDashboardSharesTheLatestPostAndKeepsTranslationText(t *testing.T) {
	if Dashboard(nil) != nil {
		t.Fatal("missing dashboard was projected")
	}
	platform := "youtube"
	listed := sqlite.Post{ID: "listed", Text: "listed", Translations: sqlite.TranslationSet{Locales: map[string]sqlite.Translation{"en": {Text: "listed en", Origin: "platform", Platform: &platform}}}}
	older := sqlite.Post{ID: "older", Text: "older"}
	projected := Dashboard(&sqlite.Dashboard{Posts: []sqlite.Post{listed}, Summary: sqlite.DashboardSummary{LatestPost: &listed}})
	posts := projected["posts"].([]any)
	latest := projected["summary"].(map[string]any)["latestPost"]
	if !reflect.DeepEqual(latest, posts[0]) {
		t.Fatalf("latest=%v posts=%v", latest, posts)
	}
	translation := posts[0].(map[string]any)["translations"].(map[string]any)["en"]
	if !reflect.DeepEqual(translation, map[string]any{"text": "listed en", "origin": "platform", "platform": "youtube"}) {
		t.Fatalf("translation=%v", translation)
	}
	// A latest post outside the window is projected on its own.
	projected = Dashboard(&sqlite.Dashboard{Posts: []sqlite.Post{listed}, Summary: sqlite.DashboardSummary{LatestPost: &older}})
	if got := projected["summary"].(map[string]any)["latestPost"]; !reflect.DeepEqual(got, PublicPost(older)) {
		t.Fatalf("latest outside the window=%v", got)
	}
}
