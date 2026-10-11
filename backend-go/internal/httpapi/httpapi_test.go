package httpapi

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/config"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

func openFixture(t *testing.T) (*sqlite.Store, string) {
	t.Helper()
	path := testutil.NewSnapshot(t)
	store, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("open read-only fixture: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, path
}

func request(t *testing.T, handler http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	handler.ServeHTTP(recorder, req)
	return recorder
}

func decodeObject(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v; body=%q", err, recorder.Body.String())
	}
	return payload
}

func TestHealthEmptySnapshotMatchesContract(t *testing.T) {
	store, _ := openFixture(t)
	handler := NewHandlerWithClock(store, func() time.Time { return time.Date(2026, 10, 11, 1, 2, 3, 456000000, time.FixedZone("JST", 9*60*60)) })
	recorder := request(t, handler, http.MethodGet, "/api/health")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	if got := recorder.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("vary = %q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "" {
		t.Fatalf("health cache control = %q", got)
	}
	var payload struct {
		OK       bool            `json:"ok"`
		Service  string          `json:"service"`
		Now      string          `json:"now"`
		LastSync json.RawMessage `json:"lastSync"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK || payload.Service != "kano-status-board" || payload.Now != "2026-10-10T16:02:03.456Z" || string(payload.LastSync) != "null" {
		t.Fatalf("unexpected health payload: %+v lastSync=%s", payload, payload.LastSync)
	}
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`).MatchString(payload.Now) {
		t.Fatalf("now is not TypeScript ISO format: %q", payload.Now)
	}
}

func TestHealthProjectsSuccessfulSyncWithoutInternalFields(t *testing.T) {
	store, path := openFixture(t)
	testutil.Exec(t, path, `INSERT INTO sync_runs (source, started_at, finished_at, status, message, counts_json, triggered_by, job_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, "x", "2026-10-11T00:00:00.000Z", "2026-10-11T00:00:01.000Z", "success", "raw diagnostic message", `{"posts":1}`, "internal-trigger", "internal-job")
	// Reopen after the writer has closed its connection so the read-only handle
	// observes the committed fixture row.
	_ = store.Close()
	store, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	recorder := request(t, NewHandler(store), http.MethodGet, "/api/health")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body)
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	run, ok := payload["lastSync"].(map[string]any)
	if !ok {
		t.Fatalf("lastSync is not an object: %#v", payload["lastSync"])
	}
	if len(run) != 6 || run["id"] != float64(1) || run["source"] != "x" || run["status"] != "success" || run["message"] != "同步完成" {
		t.Fatalf("unexpected public sync run: %#v", run)
	}
	for _, forbidden := range []string{"counts", "jobId", "triggeredBy", "raw diagnostic", "internal-job"} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("public health leaked %q: %s", forbidden, recorder.Body)
		}
	}
}

func TestRevisionAndSinceContract(t *testing.T) {
	store, path := openFixture(t)
	testutil.Exec(t, path, `INSERT INTO app_settings (key, value, updated_at) VALUES ('dashboard_revision', '17', '2026-10-11T00:00:00.000Z')`)
	_ = store.Close()
	store, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	handler := NewHandler(store)
	unchanged := request(t, handler, http.MethodGet, "/api/dashboard/revision?since=17")
	if unchanged.Code != http.StatusOK || unchanged.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("revision response: status=%d headers=%v", unchanged.Code, unchanged.Header())
	}
	payload := decodeObject(t, unchanged)
	if payload["revision"] != float64(17) || payload["changed"] != false {
		t.Fatalf("unexpected unchanged payload: %#v", payload)
	}
	changed := request(t, handler, http.MethodGet, "/api/dashboard/revision?since=16")
	if decodeObject(t, changed)["changed"] != true {
		t.Fatal("expected revision change")
	}
	invalid := request(t, handler, http.MethodGet, "/api/dashboard/revision?since=not-a-number")
	if decodeObject(t, invalid)["changed"] != true {
		t.Fatal("invalid since should be treated as no comparison")
	}
}

func TestUnknownPathAndMethodUseStableErrors(t *testing.T) {
	store, _ := openFixture(t)
	handler := NewHandler(store)
	method := request(t, handler, http.MethodPost, "/api/health")
	if method.Code != http.StatusMethodNotAllowed || method.Header().Get("Allow") != "GET" {
		t.Fatalf("method response: status=%d headers=%v", method.Code, method.Header())
	}
	if got := decodeObject(t, method)["error"]; got != "method_not_allowed" {
		t.Fatalf("method error = %#v", got)
	}
	missing := request(t, handler, http.MethodGet, "/api/not-a-route")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d", missing.Code)
	}
	if got := decodeObject(t, missing)["error"]; got != "api_not_found" {
		t.Fatalf("missing error = %#v", got)
	}
	for _, body := range []string{method.Body.String(), missing.Body.String()} {
		if strings.Contains(body, "sqlite") || strings.Contains(body, "snapshot") || strings.Contains(body, "database") {
			t.Fatalf("error leaked implementation details: %q", body)
		}
	}
}

func TestClosedDatabaseReturnsStableError(t *testing.T) {
	store, _ := openFixture(t)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	recorder := request(t, NewHandler(store), http.MethodGet, "/api/health")
	if recorder.Code != http.StatusServiceUnavailable || decodeObject(t, recorder)["error"] != "database_unavailable" {
		t.Fatalf("unexpected database error response: status=%d body=%s", recorder.Code, recorder.Body)
	}
	if strings.Contains(recorder.Body.String(), "sql") || strings.Contains(recorder.Body.String(), "snapshot") {
		t.Fatalf("database error leaked details: %s", recorder.Body)
	}
}

func TestGracefulShutdown(t *testing.T) {
	cfg := config.Default()
	server := NewServer(http.NotFoundHandler(), cfg)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := Shutdown(ctx, server); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := <-serveDone; err != http.ErrServerClosed {
		t.Fatalf("serve result = %v", err)
	}
}
