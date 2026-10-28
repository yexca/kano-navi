package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

func archiveHandler(t *testing.T, setup string) http.Handler {
	t.Helper()
	path := testutil.NewSnapshot(t)
	testutil.Exec(t, path, setup)
	store, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return NewHandlerWithClock(store, func() time.Time { return time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC) })
}

func archiveIDs(t *testing.T, payload map[string]any) []string {
	t.Helper()
	ids := []string{}
	for _, item := range payload["items"].([]any) {
		ids = append(ids, item.(map[string]any)["id"].(string))
	}
	return ids
}

func TestPostArchiveReadsAJapanMonthNewestFirst(t *testing.T) {
	handler := archiveHandler(t, `
		INSERT INTO posts (id,source,account_handle,type,text,published_at,url,raw_json) VALUES
		 ('post-before','x','kano_2525','daily','September','2026-09-30T14:59:00Z','https://x.com/kano_2525/status/1','{"private":"synthetic"}'),
		 ('post-first','x','kano_2525','daily','First','2026-09-30T15:00:00Z','https://x.com/kano_2525/status/2','{"private":"synthetic"}'),
		 ('post-second','x','_Kanotic','daily','Second','2026-10-12T03:00:00+09:00','https://x.com/_Kanotic/status/3',NULL),
		 ('post-third','x','Kano_2525','daily','Third','2026-10-31T14:59:59Z','https://x.com/kano_2525/status/4',NULL),
		 ('post-after','x','_Kanotic','daily','November','2026-10-31T15:00:00Z','https://x.com/_Kanotic/status/5',NULL);
		INSERT INTO app_settings (key,value,updated_at) VALUES ('dashboard_revision','4','');`)

	recorder := request(t, handler, http.MethodGet, "/api/posts?from=2026-10-01&to=2026-10-31&pageSize=2")
	if recorder.Code != http.StatusOK || recorder.Header().Get("Cache-Control") != "no-cache" || !strings.HasPrefix(recorder.Header().Get("ETag"), `W/"`) {
		t.Fatalf("status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body)
	}
	if strings.Contains(recorder.Body.String(), "private") || strings.Contains(recorder.Body.String(), "raw") {
		t.Fatalf("archive leaked raw post fields: %s", recorder.Body)
	}
	payload := decodeObject(t, recorder)
	if got := strings.Join(archiveIDs(t, payload), ","); got != "post-third,post-second" {
		t.Fatalf("first page=%s", got)
	}
	if payload["total"] != float64(3) || payload["totalPages"] != float64(2) || payload["hasNext"] != true || payload["revision"] != float64(4) {
		t.Fatalf("paging=%#v", payload)
	}
	adjacent := payload["adjacent"].(map[string]any)
	if adjacent["previous"] != "2026-09-30" || adjacent["next"] != "2026-11-01" {
		t.Fatalf("adjacent=%#v", adjacent)
	}
	counts := payload["accountCounts"].(map[string]any)
	if counts["kano_2525"] != float64(2) || counts["_kanotic"] != float64(1) {
		t.Fatalf("account counts=%#v", counts)
	}
	first := payload["items"].([]any)[0].(map[string]any)
	if first["accountUrl"] != "https://x.com/Kano_2525" || first["text"] != "Third" {
		t.Fatalf("post projection=%#v", first)
	}

	second := decodeObject(t, request(t, handler, http.MethodGet, "/api/posts?from=2026-10-01&to=2026-10-31&pageSize=2&page=2"))
	if got := strings.Join(archiveIDs(t, second), ","); got != "post-first" || second["hasNext"] != false {
		t.Fatalf("second page=%s %#v", got, second)
	}

	// The account filter ignores case; counts and adjacent dates follow it.
	filtered := decodeObject(t, request(t, handler, http.MethodGet, "/api/posts?from=2026-10-01&to=2026-10-31&account=KANO_2525"))
	if got := strings.Join(archiveIDs(t, filtered), ","); got != "post-third,post-first" || filtered["total"] != float64(2) {
		t.Fatalf("filtered=%s %#v", got, filtered)
	}
	if adjacent := filtered["adjacent"].(map[string]any); adjacent["previous"] != "2026-09-30" || adjacent["next"] != nil {
		t.Fatalf("filtered adjacent=%#v", adjacent)
	}
	if filtered["accountCounts"].(map[string]any)["_kanotic"] != float64(1) {
		t.Fatalf("filtered counts=%#v", filtered["accountCounts"])
	}

	for path, code := range map[string]string{
		"/api/posts?from=2026-10-31&to=2026-10-01":                       "invalid_range",
		"/api/posts?from=2026-10-01":                                     "invalid_range",
		"/api/posts?from=2026-09-01&to=2026-11-30":                       "invalid_range",
		"/api/posts?from=2026-10-01&to=2026-10-31&account=kano%25":       "invalid_account",
		"/api/posts?from=2026-10-01&to=2026-10-31&account=a'%20OR%201=1": "invalid_account",
	} {
		bad := request(t, handler, http.MethodGet, path)
		if bad.Code != http.StatusBadRequest || bad.Body.String() != `{"error":"`+code+`"}` {
			t.Fatalf("%s: status=%d body=%s", path, bad.Code, bad.Body)
		}
	}
	if method := request(t, handler, http.MethodPost, "/api/posts"); method.Code != http.StatusMethodNotAllowed || method.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("method status=%d headers=%v", method.Code, method.Header())
	}
}

func TestVideoArchiveUsesTheScheduledOrPublishedTime(t *testing.T) {
	handler := archiveHandler(t, `
		INSERT INTO videos (id,source,title,published_at,scheduled_at,url,kind,is_upcoming,raw_json,duration_seconds,is_short) VALUES
		 ('video-old','youtube','September','2026-09-20T12:00:00Z',NULL,'https://www.youtube.com/watch?v=old','video',0,'{"private":"synthetic"}',NULL,NULL),
		 ('video-upload','youtube','Upload','2026-10-05T12:00:00Z',NULL,'https://www.youtube.com/watch?v=upload','video',0,NULL,NULL,1),
		 ('video-stream','youtube','Stream','2026-09-28T12:00:00Z','2026-10-10T12:00:00Z','https://www.youtube.com/watch?v=stream','liveArchive',0,NULL,7265,0),
		 ('video-reserved','youtube','Reserved','2026-10-18T12:00:00Z','2026-10-25T12:00:00Z','https://www.youtube.com/watch?v=reserved','upcoming',1,NULL,NULL,NULL),
		 ('video-undated','youtube','Undated',NULL,NULL,'https://www.youtube.com/watch?v=undated','video',0,NULL,NULL,NULL);`)

	recorder := request(t, handler, http.MethodGet, "/api/videos?from=2026-10-01&to=2026-10-31")
	if recorder.Code != http.StatusOK || recorder.Header().Get("Cache-Control") != "no-cache" || !strings.HasPrefix(recorder.Header().Get("ETag"), `W/"`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body)
	}
	if strings.Contains(recorder.Body.String(), "private") {
		t.Fatalf("archive leaked raw video fields: %s", recorder.Body)
	}
	payload := decodeObject(t, recorder)
	if got := strings.Join(archiveIDs(t, payload), ","); got != "video-reserved,video-stream,video-upload" || payload["total"] != float64(3) || payload["hasNext"] != false {
		t.Fatalf("videos=%s %#v", got, payload)
	}
	items := payload["items"].([]any)
	if items[0].(map[string]any)["isUpcoming"] != true || items[1].(map[string]any)["isUpcoming"] != false {
		t.Fatalf("upcoming flags=%#v", items)
	}
	// A known length is public; an unknown one stays null.
	if length, ok := items[2].(map[string]any)["durationSeconds"]; items[1].(map[string]any)["durationSeconds"] != float64(7265) || !ok || length != nil {
		t.Fatalf("lengths=%#v", items)
	}
	// The feed's format is public; a row it never named stays null.
	if format, ok := items[0].(map[string]any)["isShort"]; !ok || format != nil || items[1].(map[string]any)["isShort"] != false || items[2].(map[string]any)["isShort"] != true {
		t.Fatalf("formats=%#v", items)
	}
	if adjacent := payload["adjacent"].(map[string]any); adjacent["previous"] != "2026-09-20" || adjacent["next"] != nil {
		t.Fatalf("adjacent=%#v", adjacent)
	}
	if _, ok := payload["accountCounts"]; ok {
		t.Fatalf("video archive carries post counts: %#v", payload)
	}
	if bad := request(t, handler, http.MethodGet, "/api/videos?from=2026-02-30&to=2026-03-01"); bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid range status=%d", bad.Code)
	}
}

func TestVideoArchiveValidatorFollowsTimeDerivedFields(t *testing.T) {
	path := testutil.NewSnapshot(t)
	testutil.Exec(t, path, `INSERT INTO videos (id,source,title,published_at,scheduled_at,url,kind,is_upcoming) VALUES
		 ('video-reserved','youtube','Reserved','2026-10-18T12:00:00Z','2026-10-25T12:00:00Z','https://www.youtube.com/watch?v=reserved','upcoming',1)`)
	store, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, 10, 25, 11, 0, 0, 0, time.UTC)
	handler := NewHandlerWithClock(store, func() time.Time { return now })
	read := func(ifNoneMatch string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/videos?from=2026-10-01&to=2026-10-31", nil)
		if ifNoneMatch != "" {
			req.Header.Set("If-None-Match", ifNoneMatch)
		}
		handler.ServeHTTP(recorder, req)
		return recorder
	}
	before := read("")
	if revalidated := read(before.Header().Get("ETag")); revalidated.Code != http.StatusNotModified || revalidated.Body.Len() != 0 {
		t.Fatalf("unchanged archive: status=%d body=%s", revalidated.Code, revalidated.Body)
	}
	// The reservation's start passes without a stored write.
	now = now.Add(5 * time.Hour)
	after := read(before.Header().Get("ETag"))
	if after.Code != http.StatusOK || after.Header().Get("ETag") == before.Header().Get("ETag") ||
		!strings.Contains(before.Body.String(), `"isUpcoming":true`) || !strings.Contains(after.Body.String(), `"isUpcoming":false`) {
		t.Fatalf("expired reservation: status=%d etag=%s body=%s", after.Code, after.Header().Get("ETag"), after.Body)
	}
}
