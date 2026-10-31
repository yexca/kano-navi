package adminapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/adminauth"
	"github.com/yexca/kano-navi/backend/internal/contentadmin"
	"github.com/yexca/kano-navi/backend/internal/httpx"
	"github.com/yexca/kano-navi/backend/internal/jobs"
	"github.com/yexca/kano-navi/backend/internal/source/history"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend/internal/testutil"
	"github.com/yexca/kano-navi/backend/internal/workflow"
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
	runs    *jobs.QueueManager
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
		SyntheticSteps:  map[string]bool{"x": false, "youtube": false, "bilibili": false, "media": false, "schedule": false, "translate": false, "reconcile": false},
		TolerateFailure: map[string]bool{"x": true, "youtube": true, "bilibili": true, "translate": true},
	}
	queue := &workflow.Queue{Store: store, Executor: executor}
	t.Cleanup(queue.Close)
	runs := &jobs.QueueManager{Store: store, Queue: queue}
	handler := New(Options{
		Store:        store,
		Auth:         newAuth(t, mode, password),
		Runs:         runs,
		Modules:      []httpx.AdminModule{pingModule{}},
		Scheduler:    func() map[string]any { return map[string]any{"enabled": true, "running": true, "tickSeconds": 30} },
		SkippedSteps: []string{"media"},
		XHandles:     []string{"synthetic_one"},
		// As in production with YOUTUBE_API_KEY and without
		// X_API_BEARER_TOKEN.
		FetchMethods: func(source string) []history.MethodStatus {
			out := []history.MethodStatus{}
			for _, method := range history.Methods(source) {
				out = append(out, history.MethodStatus{Method: method, Available: source == "youtube" || method.ID == history.MethodTimeline})
			}
			return out
		},
		Now: func() time.Time { return time.Date(2026, 3, 2, 3, 0, 0, 0, time.UTC) },
	})
	return &console{handler: handler, steps: steps, store: store, runs: runs}
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
	for _, path := range []string{"/api/admin/overview", "/api/admin/runs", "/api/admin/runs/steps", "/api/admin/ping", "/api/admin/events", "/api/admin/unknown"} {
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
	if code, payload := c.call(t, "PATCH", "/api/admin/runs", ""); code != 405 || payload["error"] != "method_not_allowed" {
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

func TestOneOffStepRunsReplaceSavedWorkflows(t *testing.T) {
	c := newConsole(t, "development", "", &gatedSteps{})
	code, payload := c.call(t, "GET", "/api/admin/runs/steps", "")
	if code != 200 || len(payload["steps"].([]any)) != 7 {
		t.Fatalf("catalog=%d %v", code, payload)
	}
	if skipped := payload["skippedSteps"].([]any); len(skipped) != 1 || skipped[0] != "media" {
		t.Fatalf("skipped=%v", skipped)
	}
	// There are no saved workflows (ADR-0003): the workflow endpoints are
	// unknown routes and a workflowId is refused.
	for _, route := range [][2]string{{"GET", "/api/admin/workflows"}, {"POST", "/api/admin/workflows"}, {"PUT", "/api/admin/workflows/full-refresh"}, {"DELETE", "/api/admin/workflows/full-refresh"}} {
		if code, payload := c.call(t, route[0], route[1], `{"name":"Flow","steps":["x"]}`); code != 404 || payload["error"] != "not_found" {
			t.Fatalf("%s %s=%d %v", route[0], route[1], code, payload)
		}
	}
	const removed = "saved workflows are removed; send the steps to run once, or use a source's Run now"
	for body, message := range map[string]string{
		`{"kind":"workflow","workflowId":"full-refresh"}`:               removed,
		`{"kind":"workflow","workflowId":"full-refresh","steps":["x"]}`: removed,
		`{"kind":"workflow"}`:                             "steps must be an array of step ids",
		`{"kind":"workflow","steps":"x"}`:                 "steps must be an array of step ids",
		`{"kind":"workflow","steps":["x","unknown"]}`:     "steps contains an unsupported value",
		`{"kind":"workflow","steps":["live"]}`:            "steps contains an unsupported value",
		`{"kind":"workflow","steps":[]}`:                  "steps must not be empty",
		`{"kind":"workflow","steps":["x"],"name":"Flow"}`: "name is not supported for workflow",
	} {
		if code, payload := c.call(t, "POST", "/api/admin/runs", body); code != 400 || payload["error"] != "invalid_input" || payload["message"] != message {
			t.Fatalf("%s => %d %v", body, code, payload)
		}
	}
	// A one-off step run runs in catalog order and reads X as the X schedule
	// does.
	if code, payload = c.call(t, "PUT", "/api/admin/sources/x/schedule", `{"sync":{"mode":"count","count":30}}`); code != 200 {
		t.Fatalf("x schedule=%d %v", code, payload)
	}
	code, payload = c.call(t, "POST", "/api/admin/runs", `{"kind":"workflow","steps":["media"," X ","x"]}`)
	if code != 202 {
		t.Fatalf("run=%d %v", code, payload)
	}
	run := c.waitForRun(t, runID(t, payload), "completed")
	if run["kind"] != "workflow" || run["workflowId"] != nil || run["sourceSchedule"] != nil || run["triggeredBy"] != "admin" || fmt.Sprint(run["steps"]) != "[x media reconcile]" {
		t.Fatalf("run=%v", run)
	}
	if _, named := run["workflowName"]; named {
		t.Fatalf("a run carries no workflow name: %v", run)
	}
	if windows := run["fetchWindows"].(map[string]any); windows["x"].(map[string]any)["count"] != float64(30) || windows["youtube"] != nil {
		t.Fatalf("windows=%v", windows)
	}
	// A stored run of a saved workflow stays in the history with its ID; the
	// definition is not read.
	c.exec(t, `INSERT INTO workflows(id,name,steps_json,created_at,updated_at) VALUES('evening','Evening','["x"]','2026-03-01T00:00:00.000Z','2026-03-01T00:00:00.000Z')`)
	c.exec(t, `INSERT INTO workflow_runs(id,workflow_id,status,trigger_source,execution_source,created_at,started_at,finished_at) VALUES('retired-run','evening','succeeded','scheduler','scheduler','2026-03-01T00:00:00.000Z','2026-03-01T00:00:01.000Z','2026-03-01T00:00:02.000Z')`)
	code, payload = c.call(t, "GET", "/api/admin/runs/retired-run", "")
	if retired := payload["run"].(map[string]any); code != 200 || retired["workflowId"] != "evening" || retired["kind"] != "workflow" || retired["sourceSchedule"] != nil || retired["status"] != "completed" {
		t.Fatalf("retired=%d %v", code, payload)
	}
	// A fetch frozen with a selection no method serves any more still reads
	// as it was stored.
	retired, err := c.store.CreateWorkflowRun(context.Background(), sqlite.WorkflowDefinition{ID: sqlite.AdhocFetchWorkflow, Steps: []string{"youtube", "media"}, FetchWindows: map[string]history.Window{"youtube": {Mode: history.ModeRange, StartDate: "2026-02-01", EndDate: "2026-02-07"}}}, "operator", "", map[string]bool{"youtube": true, "media": true}, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	code, payload = c.call(t, "GET", "/api/admin/runs/"+retired.ID, "")
	if window, _ := payload["run"].(map[string]any)["fetchWindows"].(map[string]any)["youtube"].(map[string]any); code != 200 || window["mode"] != "range" || window["startDate"] != "2026-02-01" {
		t.Fatalf("retired fetch=%d %v", code, payload)
	}
	if code, payload = c.call(t, "GET", "/api/admin/runs?pageSize=5", ""); code != 200 || payload["total"] != float64(3) {
		t.Fatalf("history=%d %v", code, payload)
	}
}

func TestRunsAreSingleFlightAndSurviveOneFailedSource(t *testing.T) {
	steps := &gatedSteps{gate: make(chan struct{}), fail: map[string]bool{"x": true}}
	c := newConsole(t, "development", "", steps)
	for body, message := range map[string]string{
		`{}`:                              "kind must be sync, scan, translate, workflow or fetch",
		`{"kind":"party"}`:                "kind must be sync, scan, translate, workflow or fetch",
		`{"kind":"sync","source":"x"}`:    "source is not supported for sync",
		`{"kind":"workflow"}`:             "steps must be an array of step ids",
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
	if bilibili, ok := sources["bilibili"].(map[string]any); !ok || bilibili["skipped"] != false {
		t.Fatalf("bilibili source=%v", sources["bilibili"])
	}
	if payload["activeRun"] != nil || len(payload["recentRuns"].([]any)) != 1 {
		t.Fatalf("overview runs=%v", payload)
	}
	// A scan is its own kind and records a schedule history row; its events
	// are reconciled afterwards.
	code, payload = c.call(t, "POST", "/api/admin/runs", `{"kind":"scan"}`)
	if code != 202 {
		t.Fatalf("scan=%d %v", code, payload)
	}
	scan := c.waitForRun(t, runID(t, payload), "completed")
	if scan["kind"] != "scan" || scan["run"].(map[string]any)["source"] != "schedule" || fmt.Sprint(scan["steps"]) != "[schedule reconcile]" {
		t.Fatalf("scan=%v", scan)
	}
}

func TestSourceFetchMethods(t *testing.T) {
	c := newConsole(t, "development", "", &gatedSteps{})
	code, payload := c.call(t, "GET", "/api/admin/sources", "")
	x := payload["x"].(map[string]any)
	youtube := payload["youtube"].(map[string]any)
	if code != 200 || len(x["accounts"].([]any)) != 1 || youtube["oldest"] != nil || youtube["push"].(map[string]any)["enabled"] != false {
		t.Fatalf("status=%d %v", code, payload)
	}
	// Each method reports its modes, bounds and the variable it needs,
	// never a credential.
	describe := func(state map[string]any) string {
		out := []string{}
		for _, item := range state["methods"].([]any) {
			method := item.(map[string]any)
			out = append(out, fmt.Sprintf("%v %v %v %v %v", method["id"], method["modes"], method["maxCount"], method["available"], method["requires"]))
		}
		return strings.Join(out, "; ")
	}
	if got := describe(x); got != "timeline [count all] 200 true <nil>; api [count range before] 200 false X_API_BEARER_TOKEN" {
		t.Fatalf("x methods=%s", got)
	}
	if got := describe(youtube); got != "api [count all] 200 true YOUTUBE_API_KEY; rss [count] 15 true <nil>; ytdlp [count all] 200 true YTDLP_BINARY" {
		t.Fatalf("youtube methods=%s", got)
	}
	if _, legacy := x["windows"]; legacy {
		t.Fatalf("x=%v", x)
	}
	for body, message := range map[string]string{
		`{"kind":"fetch","source":"x","window":{"mode":"count","count":5}}`:                                                           "method must be one of timeline, api",
		`{"kind":"fetch","source":"x","method":"timeline"}`:                                                                           "fetch window is required",
		`{"kind":"fetch","source":"x","method":"timeline","window":{"mode":"range","startDate":"2026-03-01","endDate":"2026-03-07"}}`: "this fetch uses a retired selection; start a new fetch",
		`{"kind":"fetch","source":"youtube","method":"api","window":{"mode":"before","days":7}}`:                                      "this fetch uses a retired selection; start a new fetch",
		`{"kind":"fetch","source":"x","method":"api","window":{"mode":"range","startDate":"2026-02-30","endDate":"2026-03-01"}}`:      "date is invalid",
		`{"kind":"fetch","source":"x","method":"api","window":{"mode":"before","days":7}}`:                                            "X_API_BEARER_TOKEN is required for the api method",
		`{"kind":"fetch","source":"x","method":"timeline","window":{"mode":"count","count":0}}`:                                       "count must be an integer from 1 to 200",
		`{"kind":"fetch","source":"youtube","method":"rss","window":{"mode":"count","count":16}}`:                                     "count must be an integer from 1 to 15",
		`{"kind":"fetch","source":"youtube","method":"rss","window":{"mode":"count","count":5},"options":{"extract":true}}`:           "extract is not supported for this method",
		`{"kind":"fetch","source":"x","method":"timeline","window":{"mode":"all"},"options":{"refresh":true}}`:                        "refresh is not supported for this method",
		`{"kind":"fetch","source":"x","method":"timeline","window":{"mode":"all"},"options":{"translate":"yes"}}`:                     "options.translate must be true or false",
		`{"kind":"fetch","source":"x","method":"timeline","window":{"mode":"all"},"options":{"fallback":true}}`:                       "options.fallback is not supported",
		`{"kind":"fetch","source":"x","method":"timeline","window":{"mode":"all"},"options":[]}`:                                      "options must be an object",
	} {
		if code, payload := c.call(t, "POST", "/api/admin/runs", body); code != 400 || payload["message"] != message {
			t.Fatalf("%s => %d %v", body, code, payload)
		}
	}
	// The selection is frozen on the run with its method, and the options
	// add their steps.
	code, payload = c.call(t, "POST", "/api/admin/runs", `{"kind":"fetch","source":"youtube","method":"api","window":{"mode":"all","count":9},"options":{"refresh":true,"translate":true}}`)
	if code != 202 {
		t.Fatalf("api=%d %v", code, payload)
	}
	all := c.waitForRun(t, runID(t, payload), "completed")
	if window := all["fetchWindows"].(map[string]any)["youtube"].(map[string]any); window["method"] != "api" || window["mode"] != "all" || window["refresh"] != true || window["count"] != nil {
		t.Fatalf("all window=%v", window)
	}
	if fmt.Sprint(all["steps"]) != "[youtube media translate reconcile]" {
		t.Fatalf("steps=%v", all["steps"])
	}
	code, payload = c.call(t, "POST", "/api/admin/runs", `{"kind":"fetch","source":"x","method":"timeline","window":{"mode":"count","count":20},"options":{"extract":true,"translate":false}}`)
	if code != 202 {
		t.Fatalf("count=%d %v", code, payload)
	}
	latest := c.waitForRun(t, runID(t, payload), "completed")
	if window := latest["fetchWindows"].(map[string]any)["x"].(map[string]any); window["method"] != "timeline" || window["mode"] != "count" || window["count"] != float64(20) {
		t.Fatalf("count window=%v", window)
	}
	if latest["kind"] != "workflow" || latest["workflowId"] != nil || fmt.Sprint(latest["steps"]) != "[x media schedule reconcile]" {
		t.Fatalf("run=%v", latest)
	}
	// A failed method fails the run; the later steps do not go on without it.
	c.steps.fail = map[string]bool{"x": true}
	code, payload = c.call(t, "POST", "/api/admin/runs", `{"kind":"fetch","source":"x","method":"timeline","window":{"mode":"all"}}`)
	if code != 202 {
		t.Fatalf("failing=%d %v", code, payload)
	}
	failed := c.waitForRun(t, runID(t, payload), "failed")
	if progress := failed["progress"].(map[string]any); progress["x"] != "failed" || progress["media"] == "completed" {
		t.Fatalf("failed run=%v", failed)
	}
}

func TestFetchMethodsFailClosedWithoutHistoryWiring(t *testing.T) {
	a := &api{}
	for _, source := range []string{"x", "youtube"} {
		methods := a.fetchMethods(source)
		want := 2
		if source == "youtube" {
			want = 3
		}
		if len(methods) != want {
			t.Fatalf("%s methods=%v", source, methods)
		}
		for _, method := range methods {
			if method.Available || a.methodAvailable(source, method.ID) {
				t.Fatalf("%s/%s unexpectedly available without history wiring", source, method.ID)
			}
		}
	}
}

func TestOverviewCountsWhatNeedsReview(t *testing.T) {
	c := newConsole(t, "development", "", &gatedSteps{})
	code, settings := c.call(t, "GET", "/api/admin/settings/visitor-region", "")
	if code != 200 || settings["enabled"] != false {
		t.Fatalf("visitor region default=%d %v", code, settings)
	}
	if code, settings = c.call(t, "PUT", "/api/admin/settings/visitor-region", `{}`); code != 400 || settings["message"] != "enabled is required" {
		t.Fatalf("visitor region missing=%d %v", code, settings)
	}
	if code, settings = c.call(t, "PUT", "/api/admin/settings/visitor-region", `{"enabled":true,"providerName":"Provider","providerUrl":"https://provider.example.test","mirrorUrl":"https://mirror.example.test"}`); code != 200 || settings["enabled"] != true || settings["providerName"] != "Provider" {
		t.Fatalf("visitor region enable=%d %v", code, settings)
	}
	if code, settings = c.call(t, "GET", "/api/admin/overview", ""); code != 200 || settings["visitorRegionEnabled"] != true {
		t.Fatalf("visitor region overview=%d %v", code, settings)
	}
	c.exec(t, `INSERT INTO events(id,source,title,starts_on,timezone,time_precision,provenance,manual_locked,cancellation_status,created_at,updated_at) VALUES
	 ('past','x','Past','2026-03-01','Asia/Tokyo','unknown','automatic',0,'none','2026-01-01T00:00:00.000Z','2026-01-01T00:00:00.000Z'),
	 ('soon','x','Soon','2026-03-02','Asia/Tokyo','unknown','automatic',0,'llm_suspected','2026-01-01T00:00:00.000Z','2026-01-01T00:00:00.000Z'),
	 ('off','x','Off','2026-03-03','Asia/Tokyo','unknown','manual',1,'manual_confirmed','2026-01-01T00:00:00.000Z','2026-01-01T00:00:00.000Z')`)
	c.exec(t, `INSERT INTO posts(id,source,type,label,text,published_at,url) VALUES
	 ('p1','x','post','label','one','2026-03-01T00:00:00.000Z','https://post.example.test/1'),
	 ('p2','x','post','label','two','2026-03-01T00:00:00.000Z','https://post.example.test/2')`)
	c.exec(t, `INSERT INTO post_llm_states(post_id,status,reprocess_requested,updated_at) VALUES ('p1','failed',1,'2026-03-01T00:00:00.000Z'),('p2','processed',0,'2026-03-01T00:00:00.000Z')`)
	// A schedule row without an image URL has nothing to review, with or
	// without a recorded scan; an unchecked image with a URL does.
	c.exec(t, `INSERT INTO assets(id,kind,url,period_basis) VALUES
	 ('schedule-unchecked','schedule','https://media.example.test/board.png','inferred'),
	 ('weekly-schedule','schedule','','legacy'),
	 ('schedule-blank','schedule','  ','legacy')`)
	c.exec(t, `INSERT INTO schedule_asset_reviews(asset_id,llm_status,manual_status,updated_at) VALUES ('schedule-blank','pending','unreviewed','2026-03-01T00:00:00.000Z')`)
	code, payload := c.call(t, "GET", "/api/admin/overview", "")
	if code != 200 {
		t.Fatalf("overview=%d %v", code, payload)
	}
	counts := payload["counts"].(map[string]any)
	review := payload["review"].(map[string]any)
	if counts["events"] != float64(3) || counts["upcomingEvents"] != float64(1) || counts["posts"] != float64(2) || counts["videos"] != float64(0) {
		t.Fatalf("counts=%v", counts)
	}
	if review["suspectedCancellations"] != float64(1) || review["failedPosts"] != float64(1) || review["reprocessRequested"] != float64(1) || review["pendingImages"] != float64(1) {
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

func TestLiveProbeSettings(t *testing.T) {
	c := newConsole(t, "development", "", &gatedSteps{})
	code, payload := c.call(t, "GET", "/api/admin/sources", "")
	probe := payload["youtube"].(map[string]any)["liveProbe"].(map[string]any)
	if code != 200 || probe["enabled"] != true || probe["intervalMinutes"] != float64(15) || probe["minIntervalMinutes"] != float64(5) ||
		probe["maxIntervalMinutes"] != float64(360) || probe["available"] != false || probe["lastRunAt"] != nil || probe["nextRunAt"] != nil ||
		probe["activeVideos"] != float64(0) || probe["maxVideos"] != float64(5) {
		t.Fatalf("status=%d %v", code, probe)
	}
	for body, message := range map[string]string{
		`{"intervalMinutes":4}`:    "intervalMinutes must be an integer from 5 to 360",
		`{"intervalMinutes":361}`:  "intervalMinutes must be an integer from 5 to 360",
		`{"intervalMinutes":12.5}`: "intervalMinutes must be an integer from 5 to 360",
		`{"enabled":"yes"}`:        "enabled must be a boolean",
		`{"videos":["x"]}`:         "videos is not a live probe field",
	} {
		if code, payload := c.call(t, "PUT", "/api/admin/sources/youtube/live-probe", body); code != 400 || payload["message"] != message {
			t.Fatalf("%s => %d %v", body, code, payload)
		}
	}
	code, payload = c.call(t, "PUT", "/api/admin/sources/youtube/live-probe", `{"intervalMinutes":10}`)
	if code != 200 || payload["intervalMinutes"] != float64(10) || payload["enabled"] != true {
		t.Fatalf("save=%d %v", code, payload)
	}
	code, payload = c.call(t, "PUT", "/api/admin/sources/youtube/live-probe", `{"enabled":false}`)
	if code != 200 || payload["intervalMinutes"] != float64(10) || payload["enabled"] != false {
		t.Fatalf("toggle=%d %v", code, payload)
	}
	if got := c.text(t, `SELECT value FROM app_settings WHERE key='youtube_live_probe_interval_minutes'`); got != "10" {
		t.Fatalf("stored interval=%s", got)
	}
	// A probe run reads as its own kind, labelled by its trigger.
	runs := &jobs.QueueManager{Store: c.store, Queue: &workflow.Queue{Store: c.store, Executor: &workflow.Executor{Store: c.store, Steps: c.steps}}}
	started, err := runs.Start(context.Background(), jobs.KindLive, jobs.TriggeredByLiveProbe, jobs.Options{})
	if err != nil || !started.Accepted {
		t.Fatalf("start=%#v err=%v", started, err)
	}
	run := c.waitForRun(t, started.Job.ID, "completed")
	if run["kind"] != "live" || run["triggeredBy"] != "live-probe" || len(run["steps"].([]any)) != 1 || run["steps"].([]any)[0] != "live" {
		t.Fatalf("run=%v", run)
	}
	if _, payload = c.call(t, "GET", "/api/admin/sources/youtube/live-probe", ""); payload["lastRunAt"] == nil {
		t.Fatalf("lastRunAt missing: %v", payload)
	}
}

// The post quote decisions and the refresh request need the admin session
// like every other content route; an anonymous request changes nothing.
func TestPostQuoteRoutesRequireTheSession(t *testing.T) {
	store, err := sqlite.OpenInitialized(filepath.Join(t.TempDir(), "database", "kano.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if _, err = store.DB().Exec(`INSERT INTO posts(id,source,account_handle,text,published_at,url,quote_json) VALUES('1900000000000000100','x','synthetic','look','2026-10-01T00:00:00.000Z','https://x.com/synthetic/status/1900000000000000100','{"id":"1900000000000000001","url":"https://x.com/other/status/1900000000000000001"}')`); err != nil {
		t.Fatal(err)
	}
	c := &console{store: store, handler: New(Options{Store: store, Auth: newAuth(t, "production", "synthetic-password"), Modules: []httpx.AdminModule{contentadmin.New(store, contentadmin.Options{})}})}
	for _, call := range [][3]string{
		{"PUT", "/api/admin/posts/1900000000000000100/quote/hidden", `{"hidden":true}`},
		{"POST", "/api/admin/posts/1900000000000000100/refresh", ""},
	} {
		if code, payload := c.call(t, call[0], call[1], call[2]); code != 401 || payload["error"] != "admin_auth_required" {
			t.Fatalf("%s %s=%d %v", call[0], call[1], code, payload)
		}
	}
	var hidden, refresh *string
	if err = store.DB().QueryRow(`SELECT quote_hidden_at, refresh_requested_at FROM posts`).Scan(&hidden, &refresh); err != nil || hidden != nil || refresh != nil {
		t.Fatalf("anonymous request changed the post: %v %v %v", hidden, refresh, err)
	}
	if code, _ := c.call(t, "POST", "/api/admin/session", `{"password":"synthetic-password"}`); code != 200 {
		t.Fatalf("login=%d", code)
	}
	if code, payload := c.call(t, "PUT", "/api/admin/posts/1900000000000000100/quote/hidden", `{"hidden":true}`); code != 200 || payload["state"] == nil {
		t.Fatalf("signed-in hide=%d %v", code, payload)
	}
}

func TestRunRetentionSettings(t *testing.T) {
	c := newConsole(t, "development", "", &gatedSteps{})
	code, payload := c.call(t, "GET", "/api/admin/runs/retention", "")
	if code != 200 || payload["succeededDays"] != float64(30) || payload["failedDays"] != float64(90) || payload["maxDays"] != float64(3650) ||
		payload["pruner"].(map[string]any)["available"] != false {
		t.Fatalf("status=%d %v", code, payload)
	}
	for body, message := range map[string]string{
		`{"succeededDays":-1}`:   "succeededDays must be an integer from 0 to 3650",
		`{"failedDays":3651}`:    "failedDays must be an integer from 0 to 3650",
		`{"failedDays":1.5}`:     "failedDays must be an integer from 0 to 3650",
		`{"keepDays":10}`:        "keepDays is not a run retention field",
		`{"succeededDays":"30"}`: "succeededDays must be an integer from 0 to 3650",
	} {
		if code, payload := c.call(t, "PUT", "/api/admin/runs/retention", body); code != 400 || payload["message"] != message {
			t.Fatalf("%s => %d %v", body, code, payload)
		}
	}
	code, payload = c.call(t, "PUT", "/api/admin/runs/retention", `{"succeededDays":0}`)
	if code != 200 || payload["succeededDays"] != float64(0) || payload["failedDays"] != float64(90) {
		t.Fatalf("save=%d %v", code, payload)
	}
	if got := c.text(t, `SELECT value FROM app_settings WHERE key='run_retention_succeeded_days'`); got != "0" {
		t.Fatalf("stored=%s", got)
	}
}
