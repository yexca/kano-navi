package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yexca/kano-navi/backend-go/internal/adminauth"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

type fakeXTrigger struct{}

func (fakeXTrigger) Trigger(context.Context, int) (*sqlite.WorkflowRun, error) {
	return &sqlite.WorkflowRun{ID: "run-x", WorkflowID: "x-recent", Status: "succeeded"}, nil
}

func TestXRecentAdminTriggerRequiresSessionAndSupportsRecentOnly(t *testing.T) {
	path := testutil.NewSnapshot(t)
	store, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg, _ := adminauth.NewConfig("production", "synthetic-admin-password", "")
	auth, _ := adminauth.New(cfg)
	h := NewHandlerWithAdminAndX(store, nil, auth, fakeXTrigger{})
	unauth := request(t, h, http.MethodPost, "/api/admin/sources/x/fetch")
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status=%d", unauth.Code)
	}
	login := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"password":"synthetic-admin-password"}`))
	login.Header.Set("Content-Type", "application/json")
	lr := httptest.NewRecorder()
	h.ServeHTTP(lr, login)
	if lr.Code != http.StatusOK {
		t.Fatalf("login=%d", lr.Code)
	}
	cookie := strings.Split(lr.Header().Get("Set-Cookie"), ";")[0]
	req := httptest.NewRequest(http.MethodPost, "/api/admin/sources/x/fetch", strings.NewReader(`{"window":{"mode":"recent","days":7}}`))
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("trigger=%d body=%s", rec.Code, rec.Body)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/admin/sources/x/fetch", strings.NewReader(`{"window":{"mode":"range","days":7}}`))
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("unsupported=%d", rec.Code)
	}
}

func TestAdminAuthAndReadOnlyProjections(t *testing.T) {
	path := testutil.NewSnapshot(t)
	testutil.Exec(t, path, `INSERT INTO posts (id, source, account_handle, type, label, text, published_at, url, media_url, media_alt, raw_json) VALUES ('post-1','x','@kano','post','label','hello schedule','2026-10-11T00:00:00.000Z','https://x.example.invalid/1','/media/m1','alt','{"secret":"must-not-leak"}')`)
	testutil.Exec(t, path, `INSERT INTO post_llm_states (post_id,status,route,last_error,reprocess_requested,updated_at) VALUES ('post-1','failed','schedule_message','provider stack /private/path',1,'2026-10-11T00:00:00.000Z')`)
	testutil.Exec(t, path, `INSERT INTO videos (id,source,title,published_at,scheduled_at,url,thumbnail_url,kind,is_upcoming) VALUES ('video-1','youtube','Video','2026-10-10T00:00:00.000Z',NULL,'https://youtube.example.invalid/v','/media/t','live',0)`)
	testutil.Exec(t, path, `INSERT INTO events (id,source,source_item_id,source_key,title,detail,starts_on,starts_at,ends_at,timezone,time_precision,status,event_type,url,provenance,manual_locked,cancellation_status,cancellation_source,cancellation_reason,cancellation_evidence,cancellation_source_item_id,cancellation_confidence,cancellation_at,created_at,updated_at) VALUES ('event-1','x','internal-source-id','key','Event','detail','2026-10-11','2026-10-11T01:00:00.000Z',NULL,'Asia/Tokyo','exact','confirmed','stream','https://example.invalid/event','manual',1,'llm_suspected','llm','reason','evidence','cancel-id',0.8,NULL,'2026-10-10T00:00:00.000Z','2026-10-11T00:00:00.000Z')`)
	testutil.Exec(t, path, `INSERT INTO sync_runs (source,started_at,finished_at,status,message,counts_json,triggered_by,job_id) VALUES ('x','2026-10-11T00:00:00.000Z','2026-10-11T00:00:01.000Z','failed','diagnostic', '{"posts":1,"stack":"hidden","path":"C:\\private\\db"}','admin','job-1')`)
	store, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg, err := adminauth.NewConfig("production", "synthetic-admin-password", "")
	if err != nil {
		t.Fatal(err)
	}
	auth, err := adminauth.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithAdmin(store, nil, auth)
	beforeBytes, _ := os.ReadFile(path)
	beforeHash := sha256.Sum256(beforeBytes)
	beforeEntries, _ := os.ReadDir(filepath.Dir(path))
	unauth := request(t, handler, http.MethodGet, "/api/admin/events?page=1&pageSize=1")
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status = %d", unauth.Code)
	}
	login := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"password":"synthetic-admin-password"}`))
	login.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, login)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", loginRec.Code, loginRec.Body)
	}
	cookie := strings.Split(loginRec.Header().Get("Set-Cookie"), ";")[0]
	read := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Cookie", cookie)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	for _, path := range []string{"/api/admin/posts/llm?page=1&pageSize=1", "/api/admin/videos?page=1&pageSize=1", "/api/admin/events?page=1&pageSize=1", "/api/admin/sync/runs?page=1&pageSize=1"} {
		rec := read(path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", path, rec.Code, rec.Body)
		}
		if strings.Contains(rec.Body.String(), "raw_json") || strings.Contains(rec.Body.String(), "must-not-leak") || strings.Contains(rec.Body.String(), "provider stack") || strings.Contains(rec.Body.String(), "C:\\private") {
			t.Fatalf("sensitive field leaked by %s: %s", path, rec.Body)
		}
	}
	var events map[string]any
	if err := json.Unmarshal(read("/api/admin/events?page=1&pageSize=1").Body.Bytes(), &events); err != nil {
		t.Fatal(err)
	}
	item := events["items"].([]any)[0].(map[string]any)
	for _, key := range []string{"manualLocked", "cancellationStatus", "provenance"} {
		if _, ok := item[key]; !ok {
			t.Fatalf("event projection missing %q: %#v", key, item)
		}
	}
	post := read("/api/admin/posts/llm?page=1&pageSize=1")
	if !strings.Contains(post.Body.String(), `"llmStatus":"failed"`) || !strings.Contains(post.Body.String(), `"llmReprocessRequested":true`) {
		t.Fatalf("post projection: %s", post.Body)
	}
	logout := httptest.NewRequest(http.MethodPost, "/api/admin/logout", nil)
	logout.Header.Set("Cookie", cookie)
	logoutRec := httptest.NewRecorder()
	handler.ServeHTTP(logoutRec, logout)
	if logoutRec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d", logoutRec.Code)
	}
	if request(t, handler, http.MethodGet, "/api/admin/session").Code != http.StatusOK {
		t.Fatal("session request failed")
	}
	afterBytes, _ := os.ReadFile(path)
	afterHash := sha256.Sum256(afterBytes)
	afterEntries, _ := os.ReadDir(filepath.Dir(path))
	if beforeHash != afterHash || len(beforeEntries) != len(afterEntries) {
		t.Fatal("admin reads changed SQLite snapshot or directory entries")
	}
}

func TestAdminCrossSiteMutationRejected(t *testing.T) {
	store, _ := openFixture(t)
	cfg, _ := adminauth.NewConfig("development", "", "")
	auth, _ := adminauth.New(cfg)
	handler := NewHandlerWithAdmin(store, nil, auth)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/unknown", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site mutation status = %d", rec.Code)
	}
}
