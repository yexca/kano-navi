package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/adminauth"
	"github.com/yexca/kano-navi/backend-go/internal/httpx"
	"github.com/yexca/kano-navi/backend-go/internal/jobs"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/workflow"
)

// gatedSteps lets a test hold a job open and fail chosen steps.
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

func (pingModule) ServeAdmin(w http.ResponseWriter, r *http.Request, path string) bool {
	if path != "/ping" {
		return false
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"pong": true})
	return true
}

type consoleFixture struct {
	handler http.Handler
	steps   *gatedSteps
	store   *sqlite.Store
}

func newConsole(t *testing.T, mode, password string, steps *gatedSteps) consoleFixture {
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
	cfg, err := adminauth.NewConfig(mode, password, "")
	if err != nil {
		t.Fatal(err)
	}
	auth, err := adminauth.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewStandaloneHandler(store, nil, auth, Standalone{Queue: queue, Compat: &Compat{
		Jobs:              &jobs.QueueManager{Store: store, Queue: queue},
		Modules:           []httpx.AdminModule{pingModule{}},
		SkippedSteps:      []string{"media"},
		XHandles:          []string{"synthetic_one"},
		HistoryCredential: func(source string) bool { return source == "youtube" },
	}})
	return consoleFixture{handler: handler, steps: steps, store: store}
}

func (c consoleFixture) call(t *testing.T, method, path, body string, headers ...string) (int, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	c.handler.ServeHTTP(w, r)
	payload := map[string]any{}
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s %s returned non-JSON %q", method, path, w.Body.String())
		}
	}
	return w.Code, payload
}

func (c consoleFixture) waitForJob(t *testing.T, id, status string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		code, payload := c.call(t, "GET", "/api/admin/sync/jobs/"+id, "")
		if code != 200 {
			t.Fatalf("job read=%d %v", code, payload)
		}
		job := payload["job"].(map[string]any)
		if job["status"] == status {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s never reached %s", id, status)
	return nil
}

func TestConsoleWorkflowLifecycleKeepsEstablishedShapes(t *testing.T) {
	c := newConsole(t, "development", "", &gatedSteps{})
	code, payload := c.call(t, "GET", "/api/admin/workflows", "")
	if code != 200 || len(payload["workflows"].([]any)) != 0 || len(payload["steps"].([]any)) != 4 || payload["activeJob"] != nil {
		t.Fatalf("empty console=%d %v", code, payload)
	}
	if limits := payload["limits"].(map[string]any); limits["minIntervalMinutes"] != float64(15) || limits["maxIntervalMinutes"] != float64(10080) {
		t.Fatalf("limits=%v", limits)
	}
	if skipped := payload["skippedByEnvironment"].([]any); len(skipped) != 1 || skipped[0] != "media" {
		t.Fatalf("skipped=%v", skipped)
	}
	for body, message := range map[string]string{
		`{"steps":["x"]}`:                                             "name is required",
		`{"name":"Flow"}`:                                             "steps must be an array",
		`{"name":"Flow","steps":["x","unknown"]}`:                     "steps contains an unsupported value",
		`{"name":"Flow","steps":[]}`:                                  "steps must not be empty",
		`{"name":"Flow","steps":["x"],"intervalMinutes":5}`:           "intervalMinutes must be an integer between 15 and 10080",
		`{"name":"Flow","steps":["x"],"scheduleEnabled":"sometimes"}`: "scheduleEnabled must be a boolean",
		`{"name":"Flow","steps":["x"],"sourceLookbackDays":{"x":0}}`:  "days must be an integer from 1 to 365",
	} {
		if code, payload := c.call(t, "POST", "/api/admin/workflows", body); code != 400 || payload["error"] != "invalid_input" || payload["message"] != message {
			t.Fatalf("%s => %d %v", body, code, payload)
		}
	}
	code, payload = c.call(t, "POST", "/api/admin/workflows", `{"name":" Evening ","steps":["media","x"],"scheduleEnabled":true,"intervalMinutes":30,"sourceLookbackDays":{"x":3}}`)
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
	if len(payload["workflows"].([]any)) != 1 {
		t.Fatalf("create should return the console state: %v", payload)
	}
	code, payload = c.call(t, "PUT", "/api/admin/workflows/"+id, `{"scheduleEnabled":false}`)
	if updated := payload["workflow"].(map[string]any); code != 200 || updated["scheduleEnabled"] != false || updated["nextRunAt"] != nil || updated["name"] != "Evening" {
		t.Fatalf("update=%d %v", code, payload)
	}
	if code, payload = c.call(t, "PUT", "/api/admin/workflows/missing", `{"name":"x"}`); code != 404 || payload["error"] != "workflow_not_found" {
		t.Fatalf("missing update=%d %v", code, payload)
	}
	code, payload = c.call(t, "POST", "/api/admin/workflows/"+id+"/run", "")
	if code != 202 || payload["accepted"] != true || payload["workflows"] == nil {
		t.Fatalf("run=%d %v", code, payload)
	}
	job := c.waitForJob(t, payload["job"].(map[string]any)["id"].(string), "completed")
	if job["kind"] != "workflow" || job["workflowId"] != id || job["workflowName"] != "Evening" || job["triggeredBy"] != "admin" {
		t.Fatalf("job=%v", job)
	}
	if windows := job["fetchWindows"].(map[string]any); windows["x"].(map[string]any)["days"] != float64(3) || windows["youtube"] != nil {
		t.Fatalf("windows=%v", windows)
	}
	_, payload = c.call(t, "GET", "/api/admin/workflows", "")
	if listed := payload["workflows"].([]any)[0].(map[string]any); listed["lastStatus"] != "success" || listed["lastJobId"] != job["id"] {
		t.Fatalf("listed=%v", listed)
	}
	if code, payload = c.call(t, "POST", "/api/admin/workflows/missing/run", ""); code != 404 || payload["error"] != "workflow_not_found" {
		t.Fatalf("missing run=%d %v", code, payload)
	}
	if code, payload = c.call(t, "DELETE", "/api/admin/workflows/"+id, ""); code != 200 || len(payload["workflows"].([]any)) != 0 {
		t.Fatalf("delete=%d %v", code, payload)
	}
	if code, payload = c.call(t, "DELETE", "/api/admin/workflows/"+id, ""); code != 404 {
		t.Fatalf("second delete=%d %v", code, payload)
	}
}

func TestConsoleJobsAreSingleFlightAndSurviveOneFailedSource(t *testing.T) {
	steps := &gatedSteps{gate: make(chan struct{}), fail: map[string]bool{"x": true}}
	c := newConsole(t, "development", "", steps)
	code, payload := c.call(t, "POST", "/api/admin/sync", "")
	if code != 202 || payload["accepted"] != true || payload["reason"] != nil {
		t.Fatalf("start=%d %v", code, payload)
	}
	id := payload["job"].(map[string]any)["id"].(string)
	<-steps.seen
	code, payload = c.call(t, "POST", "/api/admin/scan/automatic", "")
	if code != 409 || payload["accepted"] != false || payload["reason"] != "already_running" || payload["job"].(map[string]any)["id"] != id {
		t.Fatalf("busy=%d %v", code, payload)
	}
	_, payload = c.call(t, "GET", "/api/admin/workflows", "")
	if active := payload["activeJob"].(map[string]any); active["id"] != id || active["status"] != "running" || active["progress"].(map[string]any)["x"] != "running" {
		t.Fatalf("active=%v", payload["activeJob"])
	}
	close(steps.gate)
	job := c.waitForJob(t, id, "completed")
	progress := job["progress"].(map[string]any)
	if job["kind"] != "sync" || progress["x"] != "failed" || progress["youtube"] != "completed" || progress["schedule"] != "completed" {
		t.Fatalf("job=%v", job)
	}
	if result := job["result"].(map[string]any); result["status"] != "partial" {
		t.Fatalf("result=%v", result)
	}
	run := job["run"].(map[string]any)
	if run["source"] != "manual" || run["status"] != "partial" || run["triggeredBy"] != "admin" || run["counts"].(map[string]any)["x"].(map[string]any)["error"] != "permanent" {
		t.Fatalf("run=%v", run)
	}
	code, payload = c.call(t, "GET", "/api/admin/sync/jobs?limit=5", "")
	if code != 200 || len(payload["jobs"].([]any)) != 1 {
		t.Fatalf("list=%d %v", code, payload)
	}
	code, payload = c.call(t, "GET", "/api/admin/sync/runs?limit=5", "")
	if code != 200 || len(payload["runs"].([]any)) != 1 {
		t.Fatalf("runs=%d %v", code, payload)
	}
	if code, payload = c.call(t, "GET", "/api/admin/sync/jobs/unknown", ""); code != 404 || payload["error"] != "sync_job_not_found" {
		t.Fatalf("unknown=%d %v", code, payload)
	}
	// A scan is its own kind and records a schedule history row.
	code, payload = c.call(t, "POST", "/api/admin/scan/automatic", "")
	if code != 202 {
		t.Fatalf("scan=%d %v", code, payload)
	}
	scan := c.waitForJob(t, payload["job"].(map[string]any)["id"].(string), "completed")
	if scan["kind"] != "scan" || scan["run"].(map[string]any)["source"] != "schedule" || len(scan["steps"].([]any)) != 1 {
		t.Fatalf("scan=%v", scan)
	}
}

func TestConsoleSourceFetchWindows(t *testing.T) {
	c := newConsole(t, "development", "", &gatedSteps{})
	code, payload := c.call(t, "GET", "/api/admin/sources/fetch", "")
	x := payload["x"].(map[string]any)
	if code != 200 || x["historyAvailable"] != false || len(x["accounts"].([]any)) != 1 || payload["youtube"].(map[string]any)["oldest"] != nil {
		t.Fatalf("status=%d %v", code, payload)
	}
	for body, message := range map[string]string{
		`{}`:                                    "fetch window is required",
		`{"window":{"mode":"recent","days":0}}`: "days must be an integer from 1 to 365",
		`{"window":{"mode":"recent","days":"7"}}`:                                     "days must be an integer from 1 to 365",
		`{"window":{"mode":"sideways","days":7}}`:                                     "unsupported fetch mode",
		`{"window":{"mode":"range","startDate":"2026-02-30","endDate":"2026-03-01"}}`: "date is invalid",
		`{"window":{"mode":"range","startDate":"2026-03-02","endDate":"2026-03-01"}}`: "date range must cover 1 to 365 days",
		`{"window":{"mode":"before","days":7}}`:                                       "X_API_BEARER_TOKEN is required for history search",
	} {
		if code, payload := c.call(t, "POST", "/api/admin/sources/x/fetch", body); code != 400 || payload["message"] != message {
			t.Fatalf("%s => %d %v", body, code, payload)
		}
	}
	if code, payload = c.call(t, "POST", "/api/admin/sources/youtube/fetch", `{"window":{"mode":"before","days":7}}`); code != 400 || payload["message"] != "history search is not available in this backend yet" {
		t.Fatalf("history=%d %v", code, payload)
	}
	if code, payload = c.call(t, "POST", "/api/admin/sources/rss/fetch", `{"window":{"mode":"recent","days":7}}`); code != 400 || payload["message"] != "unsupported source" {
		t.Fatalf("source=%d %v", code, payload)
	}
	code, payload = c.call(t, "POST", "/api/admin/sources/youtube/fetch", `{"window":{"mode":"recent","days":21}}`)
	if code != 202 {
		t.Fatalf("fetch=%d %v", code, payload)
	}
	job := c.waitForJob(t, payload["job"].(map[string]any)["id"].(string), "completed")
	steps := job["steps"].([]any)
	if job["kind"] != "workflow" || job["workflowId"] != nil || len(steps) != 2 || steps[0] != "youtube" || steps[1] != "media" {
		t.Fatalf("job=%v", job)
	}
	if window := job["fetchWindows"].(map[string]any)["youtube"].(map[string]any); window["mode"] != "recent" || window["days"] != float64(21) {
		t.Fatalf("window=%v", window)
	}
	// An unsaved fetch never appears as a saved workflow.
	if _, payload = c.call(t, "GET", "/api/admin/workflows", ""); len(payload["workflows"].([]any)) != 0 {
		t.Fatalf("workflows=%v", payload["workflows"])
	}
}

func TestConsoleRequiresSessionAndSameOrigin(t *testing.T) {
	c := newConsole(t, "production", "synthetic-password", &gatedSteps{})
	for _, path := range []string{"/api/admin/workflows", "/api/admin/sync/jobs", "/api/admin/ping", "/api/admin/events"} {
		if code, payload := c.call(t, "GET", path, ""); code != 401 || payload["error"] != "admin_auth_required" {
			t.Fatalf("%s=%d %v", path, code, payload)
		}
	}
	if code, payload := c.call(t, "POST", "/api/admin/sync", ""); code != 401 {
		t.Fatalf("anonymous start=%d %v", code, payload)
	}
	open := newConsole(t, "development", "", &gatedSteps{})
	if code, payload := open.call(t, "POST", "/api/admin/sync", "", "Sec-Fetch-Site", "cross-site"); code != 403 || payload["error"] != "csrf_origin_mismatch" {
		t.Fatalf("cross-site=%d %v", code, payload)
	}
	if code, payload := open.call(t, "GET", "/api/admin/ping", ""); code != 200 || payload["pong"] != true {
		t.Fatalf("module=%d %v", code, payload)
	}
	if code, payload := open.call(t, "GET", "/api/admin/unknown", ""); code != 404 || payload["error"] != "admin_not_found" {
		t.Fatalf("unknown=%d %v", code, payload)
	}
}

func TestStaticSiteServesBuildWithFallbackAndContainment(t *testing.T) {
	root := t.TempDir()
	write := func(name, content string) {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", "<!doctype html><title>synthetic</title>")
	write("assets/index-AbCd1234.js", "console.log(1)")
	write("assets/unlisted-AbCd1234.js", "console.log(2)")
	write("assets/kano-avatar.jpg", "jpg")
	write(".vite/manifest.json", `{"index.html":{"file":"assets/index-AbCd1234.js","css":[]}}`)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	site := NewStaticSite(root)
	if site == nil {
		t.Fatal("static site unavailable")
	}
	if NewStaticSite(t.TempDir()) != nil || NewStaticSite("") != nil {
		t.Fatal("a directory without index.html must not become a site")
	}
	get := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		site.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}
	const revalidate, immutable = "public, max-age=0, must-revalidate", "public, max-age=31536000, immutable"
	for path, want := range map[string][3]string{
		"/":                            {"text/html; charset=utf-8", revalidate, "synthetic"},
		"/admin":                       {"text/html; charset=utf-8", revalidate, "synthetic"},
		"/history/2024":                {"text/html; charset=utf-8", revalidate, "synthetic"},
		"/assets/index-AbCd1234.js":    {"text/javascript; charset=utf-8", immutable, "console.log(1)"},
		"/assets/unlisted-AbCd1234.js": {"text/javascript; charset=utf-8", revalidate, "console.log(2)"},
		"/assets/kano-avatar.jpg":      {"image/jpeg", revalidate, "jpg"},
		"/.vite/manifest.json":         {"text/html; charset=utf-8", revalidate, "synthetic"},
		"/../outside.txt":              {"text/html; charset=utf-8", revalidate, "synthetic"},
		"/assets/":                     {"text/html; charset=utf-8", revalidate, "synthetic"},
	} {
		w := get("GET", path)
		if w.Code != 200 || w.Header().Get("Content-Type") != want[0] || w.Header().Get("Cache-Control") != want[1] || !strings.Contains(w.Body.String(), want[2]) {
			t.Fatalf("%s => %d %q %q %q", path, w.Code, w.Header().Get("Content-Type"), w.Header().Get("Cache-Control"), w.Body.String())
		}
	}
	if w := get("GET", "/assets/index-AbCd1234.js"); w.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("vary=%q", w.Header().Get("Vary"))
	}
	if w := get("POST", "/"); w.Code != 404 {
		t.Fatalf("post=%d", w.Code)
	}
	if w := get("HEAD", "/"); w.Code != 200 || w.Body.Len() != 0 {
		t.Fatalf("head=%d %d", w.Code, w.Body.Len())
	}
}
