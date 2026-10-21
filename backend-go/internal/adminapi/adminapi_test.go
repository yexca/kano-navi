package adminapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/adminauth"
	"github.com/yexca/kano-navi/backend-go/internal/httpx"
	"github.com/yexca/kano-navi/backend-go/internal/jobs"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
	"github.com/yexca/kano-navi/backend-go/internal/workflow"
)

// gatedSteps lets a test hold a run open and fail chosen steps.
type gatedSteps struct {
	gate chan struct{}
	fail map[string]bool
	seen chan string
}

func (g *gatedSteps) Execute(ctx context.Context, in workflow.StepInput) (workflow.AttemptResult, error) {
	g.seen <- in.StepID
	if g.gate != nil {
		select {
		case <-g.gate:
		case <-ctx.Done():
			return workflow.AttemptResult{}, workflow.NewStepError("cancelled", workflow.ErrCancelled)
		}
	}
	if g.fail[in.StepID] {
		return workflow.AttemptResult{}, workflow.NewStepError("permanent", workflow.ErrPermanent)
	}
	return workflow.AttemptResult{Status: "succeeded", Result: map[string]any{"count": 1}}, nil
}

type pingModule struct{}

func (pingModule) AdminRoutes() []httpx.Route {
	return []httpx.Route{{Method: http.MethodGet, Path: "/ping", Handler: func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]bool{"pong": true})
	}}}
}

type console struct {
	handler http.Handler
	steps   *gatedSteps
	store   *sqlite.Store
	cookie  string
}

func newAuth(t *testing.T, mode, password string) *adminauth.Auth {
	t.Helper()
	cfg, err := adminauth.NewConfig(mode, password, "")
	if err != nil {
		t.Fatal(err)
	}
	auth, err := adminauth.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return auth
}

func newConsole(t *testing.T, mode, password string, steps *gatedSteps) *console {
	t.Helper()
	store, err := sqlite.OpenInitialized(filepath.Join(t.TempDir(), "database", "kano.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	steps.seen = make(chan string, 32)
	executor := &workflow.Executor{
		Store: store, Steps: steps,
		SyntheticSteps:  map[string]bool{"x": false, "youtube": false, "media": false, "schedule": false},
		TolerateFailure: map[string]bool{"x": true, "youtube": true},
	}
	queue := &workflow.Queue{Store: store, Executor: executor}
	t.Cleanup(queue.Close)
	handler := New(Options{
		Store:            store,
		Auth:             newAuth(t, mode, password),
		Runs:             &jobs.QueueManager{Store: store, Queue: queue},
		Modules:          []httpx.AdminModule{pingModule{}},
		Scheduler:        func() map[string]any { return map[string]any{"enabled": true, "running": true, "tickSeconds": 30} },
		SkippedSteps:     []string{"media"},
		XHandles:         []string{"synthetic_one"},
		HistoryAvailable: func(source string) bool { return source == "youtube" },
		Now:              func() time.Time { return time.Date(2026, 3, 2, 3, 0, 0, 0, time.UTC) },
	})
	return &console{handler: handler, steps: steps, store: store}
}

func (c *console) call(t *testing.T, method, path, body string, headers ...string) (int, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if c.cookie != "" {
		r.Header.Set("Cookie", c.cookie)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	if cookie := w.Header().Get("Set-Cookie"); cookie != "" {
		c.cookie = strings.Split(cookie, ";")[0]
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store, no-transform" {
		t.Fatalf("%s %s Cache-Control=%q", method, path, got)
	}
	payload := map[string]any{}
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s %s returned non-JSON %q", method, path, w.Body.String())
		}
	}
	return w.Code, payload
}

func (c *console) exec(t *testing.T, statement string, args ...any) {
	t.Helper()
	if _, err := c.store.DB().Exec(statement, args...); err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
}

func (c *console) text(t *testing.T, statement string, args ...any) string {
	t.Helper()
	var value *string
	if err := c.store.DB().QueryRow(statement, args...).Scan(&value); err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
	if value == nil {
		return "<null>"
	}
	return *value
}

func (c *console) waitForRun(t *testing.T, id, status string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		code, payload := c.call(t, "GET", "/api/admin/runs/"+id, "")
		if code != 200 {
			t.Fatalf("run read=%d %v", code, payload)
		}
		run := payload["run"].(map[string]any)
		if run["status"] == status {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run %s never reached %s", id, status)
	return nil
}

func runID(t *testing.T, payload map[string]any) string {
	t.Helper()
	run, ok := payload["run"].(map[string]any)
	if !ok {
		t.Fatalf("no run in %v", payload)
	}
	return run["id"].(string)
}

func TestSessionGuardsEveryRoute(t *testing.T) {
	c := newConsole(t, "production", "synthetic-password", &gatedSteps{})
	for _, path := range []string{"/api/admin/overview", "/api/admin/runs", "/api/admin/workflows", "/api/admin/ping", "/api/admin/events", "/api/admin/unknown"} {
		if code, payload := c.call(t, "GET", path, ""); code != 401 || payload["error"] != "admin_auth_required" {
			t.Fatalf("%s=%d %v", path, code, payload)
		}
	}
	if code, payload := c.call(t, "POST", "/api/admin/runs", `{"kind":"sync"}`); code != 401 {
		t.Fatalf("anonymous start=%d %v", code, payload)
	}
	if code, payload := c.call(t, "GET", "/api/admin/session", ""); code != 200 || payload["authenticated"] != false || payload["requiresPassword"] != true {
		t.Fatalf("session=%d %v", code, payload)
	}
	if code, payload := c.call(t, "POST", "/api/admin/session", `{"password":"wrong-password"}`); code != 401 || payload["error"] != "invalid_credentials" {
		t.Fatalf("bad login=%d %v", code, payload)
	}
	if code, _ := c.call(t, "POST", "/api/admin/session", `{"password":"synthetic-password"}`, "Sec-Fetch-Site", "cross-site"); code != 403 {
		t.Fatalf("cross-site login=%d", code)
	}
	if code, payload := c.call(t, "POST", "/api/admin/session", `{"password":"synthetic-password"}`); code != 200 || payload["authenticated"] != true {
		t.Fatalf("login=%d %v", code, payload)
	}
	if code, payload := c.call(t, "GET", "/api/admin/ping", ""); code != 200 || payload["pong"] != true {
		t.Fatalf("module=%d %v", code, payload)
	}
	if code, payload := c.call(t, "GET", "/api/admin/unknown", ""); code != 404 || payload["error"] != "not_found" {
		t.Fatalf("unknown=%d %v", code, payload)
	}
	if code, payload := c.call(t, "PATCH", "/api/admin/workflows", ""); code != 405 || payload["error"] != "method_not_allowed" {
		t.Fatalf("method=%d %v", code, payload)
	}
	if code, payload := c.call(t, "POST", "/api/admin/runs", `{"kind":"sync"}`, "Sec-Fetch-Site", "cross-site"); code != 403 || payload["error"] != "csrf_origin_mismatch" {
		t.Fatalf("cross-site=%d %v", code, payload)
	}
	if code, _ := c.call(t, "DELETE", "/api/admin/session", ""); code != 204 {
		t.Fatalf("logout=%d", code)
	}
	if code, _ := c.call(t, "GET", "/api/admin/overview", ""); code != 401 {
		t.Fatalf("after logout=%d", code)
	}
}

func TestWorkflowLifecycle(t *testing.T) {
	c := newConsole(t, "development", "", &gatedSteps{})
	code, payload := c.call(t, "GET", "/api/admin/workflows", "")
	if code != 200 || len(payload["items"].([]any)) != 0 || len(payload["steps"].([]any)) != 4 {
		t.Fatalf("empty=%d %v", code, payload)
	}
	if limits := payload["limits"].(map[string]any); limits["minIntervalMinutes"] != float64(15) || limits["maxIntervalMinutes"] != float64(10080) {
		t.Fatalf("limits=%v", limits)
	}
	if skipped := payload["skippedSteps"].([]any); len(skipped) != 1 || skipped[0] != "media" {
		t.Fatalf("skipped=%v", skipped)
	}
	for body, message := range map[string]string{
		`{"steps":["x"]}`:                                             "name is required",
		`{"name":"Flow"}`:                                             "steps must be an array of step ids",
		`{"name":"Flow","steps":["x","unknown"]}`:                     "steps contains an unsupported value",
		`{"name":"Flow","steps":[]}`:                                  "steps must not be empty",
		`{"name":"Flow","steps":["x"],"intervalMinutes":5}`:           "intervalMinutes must be an integer from 15 to 10080",
		`{"name":"Flow","steps":["x"],"scheduleEnabled":"sometimes"}`: "scheduleEnabled must be a boolean",
		`{"name":"Flow","steps":["x"],"sourceLookbackDays":{"x":0}}`:  "days must be an integer from 1 to 365",
		`{"name":"Flow","steps":["x"],"arbitrary":"data"}`:            "arbitrary is not a workflow field",
	} {
		if code, payload := c.call(t, "POST", "/api/admin/workflows", body); code != 400 || payload["error"] != "invalid_input" || payload["message"] != message {
			t.Fatalf("%s => %d %v", body, code, payload)
		}
	}
	code, payload = c.call(t, "POST", "/api/admin/workflows", `{"name":" Evening ","steps":["media","x","x"],"scheduleEnabled":true,"intervalMinutes":30,"sourceLookbackDays":{"x":3}}`)
	if code != 201 {
		t.Fatalf("create=%d %v", code, payload)
	}
	created := payload["workflow"].(map[string]any)
	id := created["id"].(string)
	steps := created["steps"].([]any)
	if created["name"] != "Evening" || len(steps) != 2 || steps[0] != "x" || steps[1] != "media" || created["scheduleEnabled"] != true || created["nextRunAt"] == nil {
		t.Fatalf("created=%v", created)
	}
	if lookback := created["sourceLookbackDays"].(map[string]any); lookback["x"] != float64(3) || lookback["youtube"] != float64(14) {
		t.Fatalf("lookback=%v", lookback)
	}
	code, payload = c.call(t, "PUT", "/api/admin/workflows/"+id, `{"scheduleEnabled":false,"sourceLookbackDays":{"youtube":5}}`)
	if updated := payload["workflow"].(map[string]any); code != 200 || updated["scheduleEnabled"] != false || updated["nextRunAt"] != nil || updated["name"] != "Evening" ||
		updated["sourceLookbackDays"].(map[string]any)["x"] != float64(3) || updated["sourceLookbackDays"].(map[string]any)["youtube"] != float64(5) {
		t.Fatalf("update=%d %v", code, payload)
	}
	if code, payload = c.call(t, "PUT", "/api/admin/workflows/missing", `{"name":"x"}`); code != 404 || payload["error"] != "workflow_not_found" {
		t.Fatalf("missing update=%d %v", code, payload)
	}
	code, payload = c.call(t, "POST", "/api/admin/runs", `{"kind":"workflow","workflowId":"`+id+`"}`)
	if code != 202 {
		t.Fatalf("run=%d %v", code, payload)
	}
	run := c.waitForRun(t, runID(t, payload), "completed")
	if run["kind"] != "workflow" || run["workflowId"] != id || run["workflowName"] != "Evening" || run["triggeredBy"] != "admin" {
		t.Fatalf("run=%v", run)
	}
	if windows := run["fetchWindows"].(map[string]any); windows["x"].(map[string]any)["days"] != float64(3) || windows["youtube"] != nil {
		t.Fatalf("windows=%v", windows)
	}
	_, payload = c.call(t, "GET", "/api/admin/workflows", "")
	if listed := payload["items"].([]any)[0].(map[string]any); listed["lastStatus"] != "succeeded" || listed["lastJobId"] != run["id"] {
		t.Fatalf("listed=%v", listed)
	}
	if code, payload = c.call(t, "POST", "/api/admin/runs", `{"kind":"workflow","workflowId":"missing"}`); code != 404 || payload["error"] != "workflow_not_found" {
		t.Fatalf("missing run=%d %v", code, payload)
	}
	if code, _ = c.call(t, "DELETE", "/api/admin/workflows/"+id, ""); code != 204 {
		t.Fatalf("delete=%d", code)
	}
	if code, _ = c.call(t, "DELETE", "/api/admin/workflows/"+id, ""); code != 404 {
		t.Fatalf("second delete=%d", code)
	}
}

func TestRunsAreSingleFlightAndSurviveOneFailedSource(t *testing.T) {
	steps := &gatedSteps{gate: make(chan struct{}), fail: map[string]bool{"x": true}}
	c := newConsole(t, "development", "", steps)
	for body, message := range map[string]string{
		`{}`:                              "kind must be sync, scan, workflow or fetch",
		`{"kind":"party"}`:                "kind must be sync, scan, workflow or fetch",
		`{"kind":"sync","source":"x"}`:    "source is not supported for sync",
		`{"kind":"workflow"}`:             "workflowId is required",
		`{"kind":"fetch","source":"rss"}`: "source must be x or youtube",
	} {
		if code, payload := c.call(t, "POST", "/api/admin/runs", body); code != 400 || payload["message"] != message {
			t.Fatalf("%s => %d %v", body, code, payload)
		}
	}
	code, payload := c.call(t, "POST", "/api/admin/runs", `{"kind":"sync"}`)
	if code != 202 {
		t.Fatalf("start=%d %v", code, payload)
	}
	id := runID(t, payload)
	<-steps.seen
	code, payload = c.call(t, "POST", "/api/admin/runs", `{"kind":"scan"}`)
	if code != 409 || payload["error"] != "busy" || runID(t, payload) != id {
		t.Fatalf("busy=%d %v", code, payload)
	}
	_, payload = c.call(t, "GET", "/api/admin/overview", "")
	if active := payload["activeRun"].(map[string]any); active["id"] != id || active["status"] != "running" || active["progress"].(map[string]any)["x"] != "running" {
		t.Fatalf("active=%v", payload["activeRun"])
	}
	close(steps.gate)
	run := c.waitForRun(t, id, "completed")
	progress := run["progress"].(map[string]any)
	if run["kind"] != "sync" || progress["x"] != "failed" || progress["youtube"] != "completed" || progress["schedule"] != "completed" {
		t.Fatalf("run=%v", run)
	}
	if result := run["result"].(map[string]any); result["status"] != "partial" {
		t.Fatalf("result=%v", result)
	}
	history := run["run"].(map[string]any)
	if history["source"] != "manual" || history["status"] != "partial" || history["counts"].(map[string]any)["x"].(map[string]any)["error"] != "permanent" {
		t.Fatalf("history=%v", history)
	}
	code, payload = c.call(t, "GET", "/api/admin/runs?pageSize=5", "")
	if code != 200 || len(payload["items"].([]any)) != 1 || payload["total"] != float64(1) || payload["pageSize"] != float64(5) {
		t.Fatalf("list=%d %v", code, payload)
	}
	if code, payload = c.call(t, "GET", "/api/admin/runs/unknown", ""); code != 404 || payload["error"] != "run_not_found" {
		t.Fatalf("unknown=%d %v", code, payload)
	}
	_, payload = c.call(t, "GET", "/api/admin/overview", "")
	sources := payload["sources"].(map[string]any)
	if x := sources["x"].(map[string]any); x["lastStep"].(map[string]any)["status"] != "failed" || x["skipped"] != false {
		t.Fatalf("x source=%v", x)
	}
	if payload["activeRun"] != nil || len(payload["recentRuns"].([]any)) != 1 {
		t.Fatalf("overview runs=%v", payload)
	}
	// A scan is its own kind and records a schedule history row.
	code, payload = c.call(t, "POST", "/api/admin/runs", `{"kind":"scan"}`)
	if code != 202 {
		t.Fatalf("scan=%d %v", code, payload)
	}
	scan := c.waitForRun(t, runID(t, payload), "completed")
	if scan["kind"] != "scan" || scan["run"].(map[string]any)["source"] != "schedule" || len(scan["steps"].([]any)) != 1 {
		t.Fatalf("scan=%v", scan)
	}
}

func TestSourceFetchWindows(t *testing.T) {
	c := newConsole(t, "development", "", &gatedSteps{})
	code, payload := c.call(t, "GET", "/api/admin/sources", "")
	x := payload["x"].(map[string]any)
	youtube := payload["youtube"].(map[string]any)
	if code != 200 || x["historyAvailable"] != false || len(x["accounts"].([]any)) != 1 || youtube["oldest"] != nil || youtube["historyAvailable"] != true ||
		youtube["push"].(map[string]any)["enabled"] != false {
		t.Fatalf("status=%d %v", code, payload)
	}
	for body, message := range map[string]string{
		`{"kind":"fetch","source":"x"}`:                                                                           "fetch window is required",
		`{"kind":"fetch","source":"x","window":{"mode":"recent","days":0}}`:                                       "days must be an integer from 1 to 365",
		`{"kind":"fetch","source":"x","window":{"mode":"recent","days":"7"}}`:                                     "days must be an integer from 1 to 365",
		`{"kind":"fetch","source":"x","window":{"mode":"sideways","days":7}}`:                                     "unsupported fetch mode",
		`{"kind":"fetch","source":"x","window":{"mode":"range","startDate":"2026-02-30","endDate":"2026-03-01"}}`: "date is invalid",
		`{"kind":"fetch","source":"x","window":{"mode":"range","startDate":"2026-03-02","endDate":"2026-03-01"}}`: "date range must cover 1 to 365 days",
		`{"kind":"fetch","source":"x","window":{"mode":"before","days":7}}`:                                       "X_API_BEARER_TOKEN is required for history search",
	} {
		if code, payload := c.call(t, "POST", "/api/admin/runs", body); code != 400 || payload["message"] != message {
			t.Fatalf("%s => %d %v", body, code, payload)
		}
	}
	// A past window is accepted once the source credential is configured and
	// stays frozen on the run exactly as the operator selected it.
	code, payload = c.call(t, "POST", "/api/admin/runs", `{"kind":"fetch","source":"youtube","window":{"mode":"range","startDate":"2026-03-01","endDate":"2026-03-07"}}`)
	if code != 202 {
		t.Fatalf("history=%d %v", code, payload)
	}
	past := c.waitForRun(t, runID(t, payload), "completed")
	if window := past["fetchWindows"].(map[string]any)["youtube"].(map[string]any); window["mode"] != "range" || window["startDate"] != "2026-03-01" || window["endDate"] != "2026-03-07" || window["days"] != nil {
		t.Fatalf("past window=%v", window)
	}
	code, payload = c.call(t, "POST", "/api/admin/runs", `{"kind":"fetch","source":"youtube","window":{"mode":"recent","days":21}}`)
	if code != 202 {
		t.Fatalf("fetch=%d %v", code, payload)
	}
	run := c.waitForRun(t, runID(t, payload), "completed")
	steps := run["steps"].([]any)
	if run["kind"] != "workflow" || run["workflowId"] != nil || len(steps) != 2 || steps[0] != "youtube" || steps[1] != "media" {
		t.Fatalf("run=%v", run)
	}
	// An unsaved fetch never appears as a saved workflow.
	if _, payload = c.call(t, "GET", "/api/admin/workflows", ""); len(payload["items"].([]any)) != 0 {
		t.Fatalf("workflows=%v", payload["items"])
	}
}

func TestOverviewCountsWhatNeedsReview(t *testing.T) {
	c := newConsole(t, "development", "", &gatedSteps{})
	c.exec(t, `INSERT INTO events(id,source,title,starts_on,timezone,time_precision,provenance,manual_locked,cancellation_status,created_at,updated_at) VALUES
	 ('past','x','Past','2026-03-01','Asia/Tokyo','unknown','automatic',0,'none','2026-01-01T00:00:00.000Z','2026-01-01T00:00:00.000Z'),
	 ('soon','x','Soon','2026-03-02','Asia/Tokyo','unknown','automatic',0,'llm_suspected','2026-01-01T00:00:00.000Z','2026-01-01T00:00:00.000Z'),
	 ('off','x','Off','2026-03-03','Asia/Tokyo','unknown','manual',1,'manual_confirmed','2026-01-01T00:00:00.000Z','2026-01-01T00:00:00.000Z')`)
	c.exec(t, `INSERT INTO posts(id,source,type,label,text,published_at,url) VALUES
	 ('p1','x','post','label','one','2026-03-01T00:00:00.000Z','https://post.example.test/1'),
	 ('p2','x','post','label','two','2026-03-01T00:00:00.000Z','https://post.example.test/2')`)
	c.exec(t, `INSERT INTO post_llm_states(post_id,status,reprocess_requested,updated_at) VALUES ('p1','failed',1,'2026-03-01T00:00:00.000Z'),('p2','processed',0,'2026-03-01T00:00:00.000Z')`)
	code, payload := c.call(t, "GET", "/api/admin/overview", "")
	if code != 200 {
		t.Fatalf("overview=%d %v", code, payload)
	}
	counts := payload["counts"].(map[string]any)
	review := payload["review"].(map[string]any)
	if counts["events"] != float64(3) || counts["upcomingEvents"] != float64(1) || counts["posts"] != float64(2) || counts["videos"] != float64(0) {
		t.Fatalf("counts=%v", counts)
	}
	if review["suspectedCancellations"] != float64(1) || review["failedPosts"] != float64(1) || review["reprocessRequested"] != float64(1) || review["pendingImages"] != float64(0) {
		t.Fatalf("review=%v", review)
	}
	if scheduler := payload["scheduler"].(map[string]any); scheduler["running"] != true {
		t.Fatalf("scheduler=%v", scheduler)
	}
	if media := payload["sources"].(map[string]any)["x"].(map[string]any); media["lastStep"] != nil || media["lastSuccessAt"] != nil {
		t.Fatalf("x before any run=%v", media)
	}
	code, payload = c.call(t, "GET", "/api/admin/events?cancellation=llm_suspected", "")
	if items := payload["items"].([]any); code != 200 || len(items) != 1 || items[0].(map[string]any)["id"] != "soon" {
		t.Fatalf("suspected=%d %v", code, payload)
	}
	code, payload = c.call(t, "GET", "/api/admin/posts?llm=failed&reprocess=1", "")
	if items := payload["items"].([]any); code != 200 || len(items) != 1 || items[0].(map[string]any)["id"] != "p1" {
		t.Fatalf("failed posts=%d %v", code, payload)
	}
}

func TestReadOnlySnapshotServesSanitizedListsAndRefusesWrites(t *testing.T) {
	path := testutil.NewSnapshot(t)
	testutil.Exec(t, path, `INSERT INTO posts (id, source, account_handle, type, label, text, published_at, url, media_url, media_alt, raw_json) VALUES ('post-1','x','@kano','post','label','hello schedule','2026-10-11T00:00:00.000Z','https://x.example.invalid/1','/media/m1','alt','{"secret":"must-not-leak"}')`)
	testutil.Exec(t, path, `INSERT INTO post_llm_states (post_id,status,route,last_error,reprocess_requested,updated_at) VALUES ('post-1','failed','schedule_message','provider stack /private/path',1,'2026-10-11T00:00:00.000Z')`)
	testutil.Exec(t, path, `INSERT INTO videos (id,source,title,published_at,scheduled_at,url,thumbnail_url,kind,is_upcoming) VALUES ('video-1','youtube','Video','2026-10-10T00:00:00.000Z',NULL,'https://youtube.example.invalid/v','/media/t','live',0)`)
	store, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	c := &console{store: store, handler: New(Options{Store: store, Auth: newAuth(t, "development", "")})}
	for _, path := range []string{"/api/admin/posts?page=1&pageSize=1", "/api/admin/videos?pageSize=1", "/api/admin/events", "/api/admin/overview"} {
		code, payload := c.call(t, "GET", path, "")
		raw, _ := json.Marshal(payload)
		if code != 200 {
			t.Fatalf("%s=%d %s", path, code, raw)
		}
		for _, leak := range []string{"raw_json", "must-not-leak", "provider stack", "/private/path"} {
			if strings.Contains(string(raw), leak) {
				t.Fatalf("%s leaked %q: %s", path, leak, raw)
			}
		}
	}
	_, payload := c.call(t, "GET", "/api/admin/posts", "")
	post := payload["items"].([]any)[0].(map[string]any)
	if post["llmStatus"] != "failed" || post["llmReprocessRequested"] != true || post["llmLastError"] != "provider_error" {
		t.Fatalf("post=%v", post)
	}
	if code, payload := c.call(t, "POST", "/api/admin/events", `{"title":"x","startsOn":"2026-03-02"}`); code != 503 || payload["error"] != "read_only" {
		t.Fatalf("write=%d %v", code, payload)
	}
	if code, payload := c.call(t, "GET", "/api/admin/runs", ""); code != 503 || payload["error"] != "unavailable" {
		t.Fatalf("runs without a queue=%d %v", code, payload)
	}
	if code, payload := c.call(t, "POST", "/api/admin/session", `{"password":""}`); code != 200 || payload["authenticated"] != true {
		t.Fatalf("development login on a read-only store=%d %v", code, payload)
	}
}
