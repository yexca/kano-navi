package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

func TestCalendarRangePaginationAndPublicProjection(t *testing.T) {
	path := testutil.NewSnapshot(t)
	testutil.Exec(t, path, `
		INSERT INTO events (id,source,title,detail,starts_on,starts_at,timezone,time_precision,status,event_type,url,provenance,manual_locked,cancellation_status,cancellation_evidence,created_at,updated_at) VALUES
		 ('calendar-a','x','A','public','2026-10-10','2026-10-10T23:00:00Z','Asia/Tokyo','exact','scheduled','event','https://example.invalid/a','automatic',1,'none',NULL,'',''),
		 ('calendar-prev','x','Previous',NULL,'2026-10-09',NULL,'Asia/Tokyo','date','scheduled','event',NULL,'automatic',0,'none',NULL,'',''),
		 ('calendar-b','x','B',NULL,'2026-10-11','2026-10-11T01:00:00+09:00','Asia/Tokyo','exact','cancelled','event','https://example.invalid/b','automatic',0,'llm_suspected','private evidence','',''),
		 ('calendar-c','x','C',NULL,'2026-10-11',NULL,'Asia/Tokyo','date','scheduled','event',NULL,'manual',0,'none',NULL,'',''),
		 ('calendar-next','x','Next',NULL,'2026-10-13',NULL,'Asia/Tokyo','date','scheduled','event',NULL,'automatic',0,'none',NULL,'',''),
		 ('calendar-d','x','D',NULL,'2026-10-12','2026-10-12T01:00:00+09:00','Asia/Tokyo','exact','scheduled','event',NULL,'automatic',0,'none',NULL,'','');
		UPDATE events SET deleted_at='2026-10-11T00:00:00Z' WHERE id='calendar-d';
		INSERT INTO assets (id,kind,url,source_url,alt,week_start,period_start,period_end,period_basis,source_account,updated_at) VALUES ('schedule-image','schedule','/assets/synthetic.png','https://example.invalid/post','Synthetic','2026-10-10','2026-10-10','2026-10-11','explicit','kano','2026-10-11T00:00:00Z');
		INSERT INTO schedule_asset_reviews (asset_id,llm_status,manual_status,updated_at) VALUES ('schedule-image','pending','schedule','');
		INSERT INTO app_settings (key,value,updated_at) VALUES ('dashboard_revision','9','');`)
	store, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	handler := NewHandler(store)
	recorder := request(t, handler, http.MethodGet, "/api/calendar?from=2026-10-10&to=2026-10-11&page=1&pageSize=1")
	if recorder.Code != http.StatusOK || recorder.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("calendar status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body)
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["total"] != float64(3) || payload["totalPages"] != float64(3) || payload["hasNext"] != true || payload["revision"] != float64(9) {
		t.Fatalf("calendar metadata=%#v", payload)
	}
	items := payload["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != "calendar-b" {
		t.Fatalf("calendar ordering/page=%#v", items)
	}
	if items[0].(map[string]any)["manualLocked"] != false || strings.Contains(recorder.Body.String(), "sourceItemId") {
		t.Fatalf("calendar leaked private fields: %s", recorder.Body)
	}
	if value, ok := items[0].(map[string]any)["liveStatus"]; !ok || value != nil {
		t.Fatalf("calendar live status projection=%#v", items[0])
	}
	counts := payload["dayCounts"].(map[string]any)
	if counts["2026-10-10"] != float64(1) || counts["2026-10-11"] != float64(2) {
		t.Fatalf("dayCounts=%#v", counts)
	}
	adjacent := payload["adjacent"].(map[string]any)
	if adjacent["previous"] != "2026-10-09" || adjacent["next"] != "2026-10-13" {
		t.Fatalf("adjacent=%#v", payload["adjacent"])
	}
	images := payload["scheduleImages"].([]any)
	if len(images) != 1 || images[0].(map[string]any)["id"] != "schedule-image" || images[0].(map[string]any)["url"] != nil {
		t.Fatalf("schedule image projection=%#v", images)
	}
	second := request(t, handler, http.MethodGet, "/api/calendar?from=2026-10-10&to=2026-10-11&page=2&pageSize=1")
	var secondPayload map[string]any
	if err := json.Unmarshal(second.Body.Bytes(), &secondPayload); err != nil {
		t.Fatal(err)
	}
	secondItem := secondPayload["items"].([]any)[0].(map[string]any)
	if secondItem["id"] != "calendar-a" || secondItem["manualLocked"] != true {
		t.Fatalf("manual lock projection=%#v", secondItem)
	}
	empty := request(t, handler, http.MethodGet, "/api/calendar?from=2026-10-14&to=2026-10-20&page=99&pageSize=0")
	var emptyPayload map[string]any
	if err := json.Unmarshal(empty.Body.Bytes(), &emptyPayload); err != nil {
		t.Fatal(err)
	}
	if emptyPayload["total"] != float64(0) || len(emptyPayload["items"].([]any)) != 0 || emptyPayload["totalPages"] != float64(1) || emptyPayload["page"] != float64(1) {
		t.Fatalf("empty calendar=%#v", emptyPayload)
	}

	for _, path := range []string{
		"/api/calendar?from=2026-10-11&to=2026-10-03",
		"/api/calendar?from=2026-02-30&to=2026-03-01",
		"/api/calendar?from=2026-09-28&to=2026-11-09",
	} {
		bad := request(t, handler, http.MethodGet, path)
		if bad.Code != http.StatusBadRequest || bad.Body.String() != `{"error":"invalid_calendar_range"}` {
			t.Fatalf("invalid calendar %s: status=%d body=%s", path, bad.Code, bad.Body)
		}
	}
}

func TestCalendarAcceptsAMonthGrid(t *testing.T) {
	path := testutil.NewSnapshot(t)
	store, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	handler := NewHandler(store)
	// A six-week Monday-first grid is the widest public window.
	month := request(t, handler, http.MethodGet, "/api/calendar?from=2026-09-28&to=2026-11-08")
	if month.Code != http.StatusOK {
		t.Fatalf("month grid status=%d body=%s", month.Code, month.Body)
	}
}

func TestMediaAndProfileMediaContainmentAndHeaders(t *testing.T) {
	path := testutil.NewSnapshot(t)
	root := filepath.Dir(filepath.Dir(path))
	body := append([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, []byte("synthetic")...)
	hashBytes := sha256.Sum256(body)
	hash := hex.EncodeToString(hashBytes[:])
	id := strings.Repeat("a", 64)
	relative := filepath.ToSlash(filepath.Join("x", "sha256", hash[:2], hash+".png"))
	mediaPath := filepath.Join(root, filepath.FromSlash(relative))
	profileRelative := "avatar/avatar.png"
	profilePath := filepath.Join(root, filepath.FromSlash(profileRelative))
	if err := os.MkdirAll(filepath.Dir(mediaPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mediaPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(mediaPath); _ = os.Remove(profilePath) })
	testutil.Exec(t, path, `
		INSERT INTO media_assets (id,source,source_url,cache_path,mime_type,byte_size,sha256,status,fetched_at) VALUES (?,?,?,?,?,?,?,?,?)`, id, "x", "https://cdn.example.invalid/media.png", relative, "image/png", len(body), hash, "ready", "2026-10-11T00:00:00Z")
	testutil.Exec(t, path, `
		INSERT INTO media_assets (id,source,source_url,status) VALUES (?,?,?,?)`, strings.Repeat("b", 64), "x", "https://cdn.example.invalid/pending.png", "pending")
	testutil.Exec(t, path, `
		INSERT INTO profile_media (id,slot,source,source_url,active_cache_path,active_sha256,active_mime_type,status,is_active) VALUES (?,?,?,?,?,?,?,?,?)`, "profile", "avatar", "upload", "https://example.invalid/profile.png", profileRelative, hash, "image/png", "ready", 1)
	store, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	handler := NewHandler(store)
	response := request(t, handler, http.MethodGet, "/media/"+id+"?v="+hash)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" || response.Header().Get("ETag") != `"`+hash+`"` || response.Body.String() != string(body) {
		t.Fatalf("media response status=%d headers=%v body=%x", response.Code, response.Header(), response.Body.Bytes())
	}
	// A width the original cannot shrink to serves the original; only the
	// fixed variant widths are accepted.
	variant := request(t, handler, http.MethodGet, "/media/"+id+"?v="+hash+"&w=320")
	if variant.Code != http.StatusOK || variant.Body.String() != string(body) || variant.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("variant fallback status=%d headers=%v", variant.Code, variant.Header())
	}
	for _, width := range []string{"321", "", "abc", "320&w=640"} {
		if rejected := request(t, handler, http.MethodGet, "/media/"+id+"?v="+hash+"&w="+width); rejected.Code != http.StatusNotFound {
			t.Fatalf("width %q accepted: %d", width, rejected.Code)
		}
	}
	for _, tc := range []struct {
		name, value string
		status      int
	}{
		{"exact", `"` + hash + `"`, http.StatusNotModified},
		{"weak", `W/"` + hash + `"`, http.StatusNotModified},
		{"multiple", `"other", "` + hash + `"`, http.StatusNotModified},
		{"other", `"other"`, http.StatusOK},
		{"prefix", `"prefix-` + hash + `"`, http.StatusOK},
		{"wildcard", `*`, http.StatusNotModified},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/media/"+id, nil)
			r.Header.Set("If-None-Match", tc.value)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, r)
			if recorder.Code != tc.status {
				t.Fatalf("status=%d want=%d headers=%v", recorder.Code, tc.status, recorder.Header())
			}
			if tc.status == http.StatusNotModified && recorder.Body.Len() != 0 {
				t.Fatalf("304 body=%q", recorder.Body.Bytes())
			}
		})
	}
	profileConditional := httptest.NewRequest(http.MethodGet, "/media/profile/avatar", nil)
	profileConditional.Header.Set("If-None-Match", `W/"`+hash+`"`)
	profileRecorder := httptest.NewRecorder()
	handler.ServeHTTP(profileRecorder, profileConditional)
	if profileRecorder.Code != http.StatusNotModified || profileRecorder.Body.Len() != 0 {
		t.Fatalf("profile conditional status=%d body=%x", profileRecorder.Code, profileRecorder.Body.Bytes())
	}
	head := request(t, handler, http.MethodHead, "/media/profile/avatar?v="+hash)
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") != contentLengthString(len(body)) {
		t.Fatalf("profile HEAD status=%d headers=%v body=%q", head.Code, head.Header(), head.Body.String())
	}
	rangedRequest := httptest.NewRequest(http.MethodGet, "/media/"+id, nil)
	rangedRequest.Header.Set("Range", "bytes=0-3")
	rangedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(rangedRecorder, rangedRequest)
	if rangedRecorder.Code != http.StatusPartialContent || rangedRecorder.Body.String() != string(body[:4]) {
		t.Fatalf("range status=%d headers=%v body=%x", rangedRecorder.Code, rangedRecorder.Header(), rangedRecorder.Body.Bytes())
	}
	pending := request(t, handler, http.MethodGet, "/media/"+strings.Repeat("b", 64))
	if pending.Code != http.StatusNotFound || pending.Body.String() != `{"error":"media_not_ready"}` {
		t.Fatalf("pending media status=%d body=%s", pending.Code, pending.Body)
	}
	for _, path := range []string{"/media/not-an-id", "/media/" + id + "?v=" + strings.Repeat("0", 64), "/media/profile/nope"} {
		missing := request(t, handler, http.MethodGet, path)
		if missing.Code != http.StatusNotFound || strings.Contains(missing.Body.String(), root) || strings.Contains(missing.Body.String(), "sqlite") {
			t.Fatalf("unsafe media %s status=%d body=%s", path, missing.Code, missing.Body)
		}
	}
}

func contentLengthString(value int) string { return strconv.Itoa(value) }
