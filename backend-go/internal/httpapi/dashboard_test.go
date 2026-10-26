package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

func dashboardHandler(t *testing.T, setup func(string)) http.Handler {
	t.Helper()
	path := testutil.NewSnapshot(t)
	if setup != nil {
		setup(path)
	}
	store, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("open dashboard fixture: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, 10, 11, 3, 0, 0, 0, time.FixedZone("JST", 9*60*60))
	return NewHandlerWithClock(store, func() time.Time { return now })
}

func dashboardPayload(t *testing.T, handler http.Handler, path string) (int, http.Header, map[string]any, string) {
	t.Helper()
	recorder := request(t, handler, http.MethodGet, path)
	var payload map[string]any
	if recorder.Code == http.StatusOK {
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode dashboard: %v; body=%s", err, recorder.Body)
		}
	}
	return recorder.Code, recorder.Header(), payload, recorder.Body.String()
}

func TestDashboardEmptySnapshotAndDaysBounds(t *testing.T) {
	handler := dashboardHandler(t, nil)
	for _, tc := range []struct {
		path string
		days float64
	}{
		{"/api/dashboard", 3}, {"/api/dashboard?days=0", 1}, {"/api/dashboard?days=999", 30}, {"/api/dashboard?days=999999999999999999999999", 30}, {"/api/dashboard?days=-999999999999999999999999", 1}, {"/api/dashboard?days=bad", 3},
	} {
		status, headers, payload, body := dashboardPayload(t, handler, tc.path)
		if status != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", tc.path, status, body)
		}
		if headers.Get("Vary") != "Accept-Encoding" || headers.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s headers=%v", tc.path, headers)
		}
		meta := payload["meta"].(map[string]any)
		if meta["postWindowDays"] != tc.days {
			t.Fatalf("%s postWindowDays=%v want %v", tc.path, meta["postWindowDays"], tc.days)
		}
		if len(payload["posts"].([]any)) != 0 || len(payload["events"].([]any)) != 0 || len(payload["videos"].([]any)) != 0 {
			t.Fatalf("%s expected empty collections", tc.path)
		}
	}
}

func TestDashboardPublicProjectionAndBoundedSemantics(t *testing.T) {
	handler := dashboardHandler(t, func(path string) {
		testutil.Exec(t, path, `
		INSERT INTO profiles (id,display_name,romanized_name,bio,x_url,youtube_url,updated_at) VALUES ('p','鹿乃まほろ','Kano Mahoro','bio','https://x.com/kano_2525','https://youtube.com/@kano','2026-10-11T00:00:00.000Z');
		INSERT INTO posts (id,source,account_handle,type,label,text,published_at,url,likes,reposts,replies,media_url,media_alt) VALUES ('post-1','x',NULL,'daily','label','hello','2026-10-10T12:00:00.000Z','https://x.com/kano_2525/status/1',1,2,3,'https://example.com/post.png','alt');
		INSERT INTO videos (id,source,title,published_at,scheduled_at,url,thumbnail_url,kind,is_upcoming,duration_seconds) VALUES ('video-1','youtube','Video','2026-10-09T00:00:00.000Z',NULL,'https://youtu.be/video-1','https://example.com/thumb.png','video',0,289);
		INSERT INTO events (id,source,title,detail,starts_on,starts_at,ends_at,timezone,time_precision,status,event_type,url,provenance,manual_locked,cancellation_status,cancellation_evidence,created_at,updated_at) VALUES
		 ('automatic','x','Automatic',NULL,'2026-10-11','2026-10-11T10:00:00+09:00',NULL,'Asia/Tokyo','exact','scheduled','event','https://x.com/e','automatic',0,'none',NULL,'',''),
		 ('locked','x','Locked',NULL,'2026-10-11','2026-10-11T09:00:00+09:00',NULL,'Asia/Tokyo','exact','scheduled','event','https://x.com/l','manual',1,'none',NULL,'',''),
		 ('tombstone','x','Deleted',NULL,'2026-10-11','2026-10-11T11:00:00+09:00',NULL,'Asia/Tokyo','exact','scheduled','event','https://x.com/t','manual',1,'none',NULL,'','');
		UPDATE events SET deleted_at='2026-10-11T00:00:00.000Z' WHERE id='tombstone';
		INSERT INTO events (id,source,title,starts_on,starts_at,timezone,time_precision,status,event_type,provenance,manual_locked,cancellation_status,cancellation_evidence,created_at,updated_at) VALUES ('suspected','x','Suspected','2026-10-11','2026-10-11T12:00:00+09:00','Asia/Tokyo','exact','cancelled','event','automatic',0,'llm_suspected','evidence','','');
		INSERT INTO events (id,source,title,starts_on,starts_at,timezone,time_precision,status,event_type,provenance,manual_locked,cancellation_status,created_at,updated_at) VALUES ('offset-z','x','Offset Z','2026-10-11','2026-10-10T23:00:00Z','Asia/Tokyo','exact','scheduled','event','automatic',0,'none','','');
		INSERT INTO events (id,source,title,starts_on,starts_at,timezone,time_precision,status,event_type,provenance,manual_locked,cancellation_status,created_at,updated_at) VALUES ('date-only','x','Date only','2026-10-13',NULL,'Asia/Tokyo','date','scheduled','event','automatic',0,'none','','');
		INSERT INTO media_assets (id,source,source_url,status) VALUES ('pending-media','x','https://example.com/post.png','pending');
		INSERT INTO media_assets (id,source,source_url,status) VALUES ('pending-thumb','youtube','https://example.com/thumb.png','pending');
		INSERT INTO profile_media (id,slot,source,source_url,status,is_active) VALUES ('avatar-candidate','avatar','x','https://example.com/avatar.png','pending',1);
		INSERT INTO assets (id,kind,url,source_url,alt,week_start,period_start,period_end,period_basis,source_account,updated_at) VALUES ('schedule-1','schedule','https://example.com/schedule.png','https://example.com/post/1','schedule','2026-10-05','2026-10-05','2026-10-11','explicit','kano_2525','2026-10-10T00:00:00.000Z');
		INSERT INTO schedule_asset_reviews (asset_id,llm_status,manual_status,updated_at) VALUES ('schedule-1','pending','schedule','');
		INSERT INTO assets (id,kind,url,source_url,alt,week_start,period_start,period_end,period_basis,source_account,updated_at) VALUES ('schedule-2','schedule','https://example.com/other.png','https://x.com/kano_2525/status/1','other','2026-10-05','2026-10-05','2026-10-11','explicit','kano_2525','2026-10-10T00:00:00.000Z');
		INSERT INTO schedule_asset_reviews (asset_id,llm_status,manual_status,updated_at) VALUES ('schedule-2','schedule','unreviewed','');
		INSERT INTO app_settings (key,value,updated_at) VALUES ('x_accounts','["kano_2525"]','');`)
		// The manually inserted post/video media rows intentionally remain pending.
	})
	status, headers, payload, body := dashboardPayload(t, handler, "/api/dashboard?days=7")
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if headers.Get("Vary") != "Accept-Encoding" || headers.Get("Cache-Control") != "no-store" {
		t.Fatalf("headers=%v", headers)
	}
	posts := payload["posts"].([]any)
	if len(posts) != 1 {
		t.Fatalf("posts=%v", posts)
	}
	post := posts[0].(map[string]any)
	if post["accountHandle"] != "kano_2525" || post["accountUrl"] != "https://x.com/kano_2525" {
		t.Fatalf("post projection=%v", post)
	}
	if post["mediaUrl"] != nil || post["mediaStatus"] != "pending" {
		t.Fatalf("unready post media leaked=%v", post)
	}
	videos := payload["videos"].([]any)
	if len(videos) != 1 {
		t.Fatalf("videos=%v", videos)
	}
	video := videos[0].(map[string]any)
	if video["thumbnailUrl"] != nil || video["thumbnailStatus"] != "pending" {
		t.Fatalf("unready thumbnail leaked=%v", video)
	}
	if video["durationSeconds"] != float64(289) {
		t.Fatalf("video length=%v", video["durationSeconds"])
	}
	profileMedia := payload["profileMedia"].(map[string]any)["avatar"].(map[string]any)
	if profileMedia["publicUrl"] != nil || profileMedia["status"] != "pending" {
		t.Fatalf("unready profile media leaked=%v", profileMedia)
	}
	if len(payload["assets"].([]any)) != 1 || len(payload["scheduleImages"].([]any)) != 0 {
		t.Fatalf("schedule asset projection=%v images=%v", payload["assets"], payload["scheduleImages"])
	}
	events := payload["events"].([]any)
	if len(events) != 5 {
		t.Fatalf("events=%v", events)
	}
	if events[0].(map[string]any)["id"] != "offset-z" {
		t.Fatalf("mixed offset sort=%v", events)
	}
	seenDateOnly, seenLocked, seenSuspected := false, false, false
	for _, item := range events {
		e := item.(map[string]any)
		if e["id"] == "date-only" {
			seenDateOnly = true
			if e["startsAt"] != nil {
				t.Fatalf("date-only startsAt=%v", e["startsAt"])
			}
		}
		if e["id"] == "locked" {
			seenLocked = true
			if e["manualLocked"] != true {
				t.Fatalf("locked projection=%v", e)
			}
		}
		if e["id"] == "suspected" {
			seenSuspected = true
			if e["cancellationStatus"] != "llm_suspected" {
				t.Fatalf("suspected projection=%v", e)
			}
		}
		if e["id"] == "tombstone" {
			t.Fatal("tombstone leaked")
		}
	}
	if !seenDateOnly || !seenLocked || !seenSuspected {
		t.Fatalf("events missing expected rows: %v", events)
	}
	encoded := body
	for _, forbidden := range []string{"raw_json", "jobId", "triggeredBy", "sourceItemId", "provider", "snapshot.sqlite", "C:\\"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("dashboard leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestDashboardMethodAndDatabaseErrorsKeepVary(t *testing.T) {
	handler := dashboardHandler(t, nil)
	method := request(t, handler, http.MethodPost, "/api/dashboard")
	if method.Code != http.StatusMethodNotAllowed || method.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("method status=%d headers=%v", method.Code, method.Header())
	}
	path := testutil.NewSnapshot(t)
	store, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	failed := request(t, NewHandler(store), http.MethodGet, "/api/dashboard?days=2")
	if failed.Code != http.StatusServiceUnavailable || failed.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("db status=%d headers=%v", failed.Code, failed.Header())
	}
	if failed.Body.String() != `{"error":"database_unavailable"}` {
		t.Fatalf("unstable database error body: %s", failed.Body)
	}
	if strings.Contains(failed.Body.String(), "query failed") || strings.Contains(failed.Body.String(), "snapshot") {
		t.Fatalf("error leaked: %s", failed.Body)
	}
}

func TestDashboardPostsStayBoundedWhileCountsRemainAccurate(t *testing.T) {
	handler := dashboardHandler(t, func(path string) {
		for i := 0; i < 125; i++ {
			testutil.Exec(t, path, `INSERT INTO posts (id,source,type,text,published_at,url) VALUES (?,?,?,?,?,?)`,
				fmt.Sprintf("post-%03d", i), "x", "daily", "text", "2026-10-10T12:00:00.000Z", fmt.Sprintf("https://x.com/p/%03d", i))
		}
	})
	status, _, payload, body := dashboardPayload(t, handler, "/api/dashboard")
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if len(payload["posts"].([]any)) != 100 {
		t.Fatalf("post payload was not bounded: %d", len(payload["posts"].([]any)))
	}
	counts := payload["summary"].(map[string]any)["counts"].(map[string]any)
	if counts["posts"] != float64(125) {
		t.Fatalf("post count=%v", counts["posts"])
	}
}

func TestDashboardUsesYouTubeStreamIdentityForAssociationAndCancellation(t *testing.T) {
	handler := dashboardHandler(t, func(path string) {
		testutil.Exec(t, path, `
		INSERT INTO videos (id,source,title,published_at,scheduled_at,url,kind,is_upcoming) VALUES
		 ('identity-live','youtube','Identity live','2026-10-10T00:00:00Z','2026-10-11T04:00:00Z','https://www.youtube.com/watch?v=identity-live','stream',1),
		 ('identity-cancelled','youtube','Identity cancelled','2026-10-10T00:00:00Z','2026-10-11T05:00:00Z','https://youtu.be/identity-cancelled?si=share','stream',1),
		 ('identity-other','youtube','Other video','2026-10-10T00:00:00Z','2026-10-11T06:00:00Z','https://www.youtube.com/watch?v=identity-other','stream',1);
		INSERT INTO events (id,source,title,starts_on,starts_at,ends_at,timezone,time_precision,status,event_type,url,provenance,manual_locked,cancellation_status,created_at,updated_at) VALUES
		 ('equivalent-live','youtube','Equivalent live','2026-10-11','2026-10-11T04:00:00Z','2026-10-11T05:00:00Z','Asia/Tokyo','exact','scheduled','stream','https://youtu.be/identity-live?feature=share','automatic',0,'none','',''),
		 ('equivalent-cancelled','youtube','Equivalent cancelled','2026-10-11','2026-10-11T05:00:00Z','2026-10-11T06:00:00Z','Asia/Tokyo','exact','scheduled','stream','https://www.youtube.com/live/identity-cancelled','manual',1,'manual_confirmed','','');`)
	})
	status, _, payload, body := dashboardPayload(t, handler, "/api/dashboard")
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, body)
	}
	videos := payload["videos"].([]any)
	byID := map[string]map[string]any{}
	for _, raw := range videos {
		video := raw.(map[string]any)
		byID[video["id"].(string)] = video
	}
	if got := byID["identity-live"]["endsAt"]; got != "2026-10-11T05:00:00Z" {
		t.Fatalf("equivalent URL did not inherit endsAt: %v", got)
	}
	if got := byID["identity-live"]["isCancelled"]; got != false {
		t.Fatalf("unexpected cancellation for live video: %v", got)
	}
	if got := byID["identity-cancelled"]["isCancelled"]; got != true {
		t.Fatalf("equivalent URL cancellation was not applied: %v", got)
	}
	if got := byID["identity-other"]["endsAt"]; got != nil {
		t.Fatalf("unrelated video inherited an end time: %v", got)
	}
	for _, raw := range payload["events"].([]any) {
		if raw.(map[string]any)["id"] == "equivalent-live" {
			return
		}
	}
	t.Fatal("linked equivalent event was not retained in the public event set")
}

func TestDashboardTimelineAndResourcesAreNotTruncated(t *testing.T) {
	handler := dashboardHandler(t, func(path string) {
		for i := 0; i < 125; i++ {
			testutil.Exec(t, path, `INSERT INTO timeline (id,year,title,sort_order) VALUES (?,?,?,?)`, i+1, "2026", fmt.Sprintf("Timeline %03d", i), i)
			testutil.Exec(t, path, `INSERT INTO resources (id,title,url,sort_order) VALUES (?,?,?,?)`, i+1, fmt.Sprintf("Resource %03d", i), fmt.Sprintf("https://example.invalid/%03d", i), i)
		}
	})
	status, _, payload, body := dashboardPayload(t, handler, "/api/dashboard")
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if got := len(payload["timeline"].([]any)); got != 125 {
		t.Fatalf("timeline length=%d, want 125", got)
	}
	if got := len(payload["resources"].([]any)); got != 125 {
		t.Fatalf("resources length=%d, want 125", got)
	}
}

func TestDashboardSourceFreshnessIsSanitizedAndIndependentOfHistoryWork(t *testing.T) {
	handler := dashboardHandler(t, func(path string) {
		testutil.Exec(t, path, `INSERT INTO sync_state(source,account_id,last_success_at,metadata_json) VALUES
		 ('x-timeline-window','private-checkpoint','2026-10-11T00:00:00Z','{"window":{"mode":"recent","endTime":"2026-10-10T20:00:00Z"},"headFetchedAt":"2026-10-10T20:05:00Z","raw":"synthetic-private-metadata"}'),
		 ('youtube-window','private-range','2026-10-11T00:00:00Z','{"window":{"mode":"range","endTime":"2026-10-11T00:00:00Z"}}')`)
	})
	status, _, payload, body := dashboardPayload(t, handler, "/api/dashboard")
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, body)
	}
	freshness := payload["meta"].(map[string]any)["sourceFetchedAt"].(map[string]any)
	if len(freshness) != 2 || freshness["x"] != "2026-10-10T20:00:00.000Z" || freshness["youtube"] != nil {
		t.Fatalf("source freshness=%v", freshness)
	}
	for _, private := range []string{"private-checkpoint", "private-range", "synthetic-private-metadata", "metadata_json", "last_success_at"} {
		if strings.Contains(body, private) {
			t.Fatalf("public freshness leaked %s", private)
		}
	}
}
