package mcpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/buildinfo"
	"github.com/yexca/kano-navi/backend-go/internal/jobs"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// Synthetic placeholders only: none of these are real credentials or records.
var (
	controlToken = strings.Join([]string{"control", "token", "placeholder"}, "-")
	wrongToken   = strings.Join([]string{"wrong", "token", "placeholder"}, "-")
	fixedNow     = time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
)

// operatorOnly lists values that exist in the seeded snapshot or the fake job
// manager and must never appear in any MCP response.
var operatorOnly = []string{
	"source-post-placeholder", "date-key-placeholder", "cancel-source-placeholder",
	"internal-job-placeholder", "internal-trigger-placeholder", "raw diagnostic message",
	"raw upstream failure", "raw-json-placeholder", "raw job failure", "job-counter-placeholder",
}

func newStore(t *testing.T) *sqlite.Store {
	t.Helper()
	store, err := sqlite.OpenInitialized(filepath.Join(t.TempDir(), "database", "kano.sqlite"), true, true)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func seed(t *testing.T, store *sqlite.Store) {
	t.Helper()
	statements := []string{
		`INSERT INTO events (id,source,source_item_id,source_key,title,detail,starts_on,starts_at,timezone,time_precision,event_type,provenance,manual_locked,confidence,extraction_id,cancellation_status,cancellation_source_item_id,created_at,updated_at,raw_json)
		 VALUES ('automatic-public-event','x','source-post-placeholder','date-key-placeholder','Automatic schedule','Public detail','2026-09-02','2026-09-02T03:00:00.000Z','Asia/Tokyo','exact','stream','automatic',0,0.9,42,'none','cancel-source-placeholder','2026-09-01T00:00:00.000Z','2026-09-01T00:00:00.000Z','{"note":"raw-json-placeholder"}')`,
		`INSERT INTO events (id,source,title,starts_on,timezone,time_precision,event_type,provenance,manual_locked,cancellation_status,created_at,updated_at)
		 VALUES ('manual-public-event','manual','Manual schedule','2026-09-03','Asia/Tokyo','unknown','event','manual',1,'none','2026-09-01T00:00:00.000Z','2026-09-01T00:00:00.000Z')`,
		`INSERT INTO events (id,source,title,starts_on,timezone,time_precision,event_type,provenance,manual_locked,cancellation_status,deleted_at,created_at,updated_at)
		 VALUES ('deleted-public-event','manual','Deleted schedule','2026-09-04','Asia/Tokyo','unknown','event','manual',1,'none','2026-09-01T00:00:00.000Z','2026-09-01T00:00:00.000Z','2026-09-01T00:00:00.000Z')`,
		// An operator hid the first post's quote; the second shows its own.
		`INSERT INTO posts (id,source,account_handle,type,text,published_at,url,raw_json,quote_json,quote_hidden_at)
		 VALUES ('post-1','x','example','post','first synthetic post','2026-09-01T10:00:00.000Z','https://x.com/example/status/1','{"note":"raw-json-placeholder"}','{"id":"1900000000000000001","text":"hidden quoted wording","authorHandle":"quoted_example","url":"https://x.com/quoted_example/status/1900000000000000001"}','2026-09-01T11:00:00.000Z')`,
		`INSERT INTO posts (id,source,account_handle,type,text,published_at,url,quote_json)
		 VALUES ('post-2','x','example','post','second synthetic post','2026-09-01T12:00:00.000Z','https://x.com/example/status/2','{"id":"1900000000000000002","text":"shown quoted wording","authorHandle":"quoted_example","url":"https://x.com/quoted_example/status/1900000000000000002"}')`,
		`INSERT INTO sync_runs (source,started_at,finished_at,status,message,counts_json,triggered_by,job_id)
		 VALUES ('manual','2026-09-01T00:00:00.000Z','2026-09-01T00:00:05.000Z','partial','raw diagnostic message','{"rawError":"raw upstream failure"}','internal-trigger-placeholder','internal-job-placeholder')`,
		`INSERT INTO sync_runs (source,started_at,finished_at,status,message,counts_json,triggered_by,job_id)
		 VALUES ('schedule','2026-09-01T01:00:00.000Z','2026-09-01T01:00:05.000Z','success','ok','{}','admin','scan-job-placeholder')`,
	}
	for _, statement := range statements {
		if _, err := store.DB().Exec(statement); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

type startCall struct{ kind, triggeredBy string }

// fakeJobs is a single-flight manager: the first start is accepted and every
// later one reports the active job.
type fakeJobs struct {
	mu       sync.Mutex
	starts   []startCall
	active   *jobs.Job
	listed   []jobs.Job
	startErr error
	contexts []context.Context
}

func (f *fakeJobs) Start(ctx context.Context, kind, triggeredBy string, _ jobs.Options) (jobs.StartResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, startCall{kind, triggeredBy})
	f.contexts = append(f.contexts, ctx)
	if f.startErr != nil {
		return jobs.StartResult{}, f.startErr
	}
	if f.active != nil {
		reason := jobs.ReasonAlreadyRunning
		return jobs.StartResult{Accepted: false, Reason: &reason, Job: f.active}, nil
	}
	failure := "raw job failure"
	f.active = &jobs.Job{
		ID: "job-1", Kind: kind, Status: jobs.StatusQueued, CreatedAt: "2026-09-02T00:00:00.000Z",
		TriggeredBy: triggeredBy,
		// A real queued job has no diagnostics yet. They are set here to prove
		// that the control projection drops them regardless.
		Error:  &failure,
		Result: map[string]any{"counter": "job-counter-placeholder"},
		Run:    map[string]any{"message": "raw diagnostic message"},
	}
	return jobs.StartResult{Accepted: true, Job: f.active}, nil
}

func (f *fakeJobs) Get(_ context.Context, id string) (*jobs.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.active != nil && f.active.ID == id {
		return f.active, nil
	}
	return nil, nil
}

func (f *fakeJobs) List(_ context.Context, limit int) ([]jobs.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if limit < len(f.listed) {
		return f.listed[:limit], nil
	}
	return f.listed, nil
}

func (f *fakeJobs) Active(context.Context) (*jobs.Job, error) { return f.active, nil }

type reply struct {
	status int
	header http.Header
	body   string
}

func (r reply) json(t *testing.T) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(r.body), &value); err != nil {
		t.Fatalf("response is not a JSON object: %v: %q", err, r.body)
	}
	return value
}

var standardHeaders = map[string]string{
	"Accept":       "application/json, text/event-stream",
	"Content-Type": "application/json",
}

func do(t *testing.T, handler http.Handler, method, target, body string, headers ...map[string]string) reply {
	t.Helper()
	var reader *strings.Reader
	request := httptest.NewRequest(method, target, nil)
	if method == http.MethodPost || body != "" {
		reader = strings.NewReader(body)
		request = httptest.NewRequest(method, target, reader)
	}
	for _, set := range headers {
		for key, value := range set {
			request.Header.Set(key, value)
		}
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	result := recorder.Result()
	return reply{status: result.StatusCode, header: result.Header, body: recorder.Body.String()}
}

// post sends one JSON-RPC payload with the headers every MCP client sends.
func post(t *testing.T, handler http.Handler, payload any, extra ...map[string]string) reply {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	return do(t, handler, http.MethodPost, "/mcp", string(encoded), append([]map[string]string{standardHeaders}, extra...)...)
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func rpc(id any, method string, params any) map[string]any {
	request := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		request["params"] = params
	}
	return request
}

func toolCall(name string, arguments map[string]any) map[string]any {
	if arguments == nil {
		arguments = map[string]any{}
	}
	return rpc(name+"-request", "tools/call", map[string]any{"name": name, "arguments": arguments})
}

// toolValue returns the structured content of a successful tool call after
// checking that the text content carries the same JSON.
func toolValue(t *testing.T, response reply) map[string]any {
	t.Helper()
	if response.status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.status, response.body)
	}
	result, _ := response.json(t)["result"].(map[string]any)
	if result == nil {
		t.Fatalf("no result: %s", response.body)
	}
	if result["isError"] == true {
		t.Fatalf("tool error: %s", response.body)
	}
	content, _ := result["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content = %v", result["content"])
	}
	entry, _ := content[0].(map[string]any)
	if entry["type"] != "text" {
		t.Fatalf("content type = %v", entry["type"])
	}
	var fromText map[string]any
	if err := json.Unmarshal([]byte(entry["text"].(string)), &fromText); err != nil {
		t.Fatalf("text content is not JSON: %v", err)
	}
	structured, _ := result["structuredContent"].(map[string]any)
	if !reflect.DeepEqual(fromText, structured) {
		t.Fatalf("text and structuredContent differ:\n%v\n%v", fromText, structured)
	}
	return structured
}

// toolErrorText returns the text of an isError tool result.
func toolErrorText(t *testing.T, response reply) string {
	t.Helper()
	if response.status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.status, response.body)
	}
	result, _ := response.json(t)["result"].(map[string]any)
	if result == nil || result["isError"] != true {
		t.Fatalf("expected an isError result: %s", response.body)
	}
	if _, present := result["structuredContent"]; present {
		t.Fatalf("an error result must not carry structured content: %s", response.body)
	}
	content, _ := result["content"].([]any)
	entry, _ := content[0].(map[string]any)
	text, _ := entry["text"].(string)
	return text
}

func rpcErrorOf(t *testing.T, response reply) (float64, string) {
	t.Helper()
	detail, _ := response.json(t)["error"].(map[string]any)
	if detail == nil {
		t.Fatalf("no JSON-RPC error: %s", response.body)
	}
	code, _ := detail["code"].(float64)
	text, _ := detail["message"].(string)
	return code, text
}

func keys(object map[string]any) []string {
	names := make([]string, 0, len(object))
	for name := range object {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func toolNames(t *testing.T, response reply) []string {
	t.Helper()
	result, _ := response.json(t)["result"].(map[string]any)
	listed, _ := result["tools"].([]any)
	names := make([]string, 0, len(listed))
	for _, entry := range listed {
		names = append(names, entry.(map[string]any)["name"].(string))
	}
	return names
}

func assertNoOperatorData(t *testing.T, label, body string) {
	t.Helper()
	for _, secret := range append(operatorOnly, controlToken) {
		if strings.Contains(body, secret) {
			t.Fatalf("%s leaks %q: %s", label, secret, body)
		}
	}
}

// options builds an enabled endpoint with a fixed clock. The control value is
// always one of the synthetic placeholders above.
func options(control string, manager jobs.Manager) Options {
	configured := Options{Enabled: true, Jobs: manager, Now: func() time.Time { return fixedNow }}
	configured.ControlToken = control
	return configured
}

func newHandler(t *testing.T, manager jobs.Manager) (http.Handler, *sqlite.Store) {
	t.Helper()
	store := newStore(t)
	seed(t, store)
	return New(store, options(controlToken, manager)), store
}

var publicToolNames = []string{"dashboard_read", "schedule_list", "posts_list", "health_read", "sync_status"}
var controlToolNames = []string{"dashboard_request_reload", "sync_start", "scan_run_automatic"}

func TestInitializeNegotiatesProtocolVersion(t *testing.T) {
	handler, _ := newHandler(t, nil)
	initialize := func(version string) map[string]any {
		response := post(t, handler, rpc(1, "initialize", map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "synthetic-client", "version": "1"},
		}))
		if response.status != http.StatusOK {
			t.Fatalf("status = %d, body = %s", response.status, response.body)
		}
		if got := response.header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content type = %q", got)
		}
		if response.header.Get("Mcp-Session-Id") != "" {
			t.Fatal("a stateless endpoint must not issue a session")
		}
		payload := response.json(t)
		if payload["jsonrpc"] != "2.0" || payload["id"] != float64(1) {
			t.Fatalf("envelope = %s", response.body)
		}
		return payload["result"].(map[string]any)
	}

	for _, version := range supportedProtocolVersions {
		if got := initialize(version)["protocolVersion"]; got != version {
			t.Fatalf("version %s negotiated %v", version, got)
		}
	}
	result := initialize("1999-01-01")
	if result["protocolVersion"] != "2025-11-25" {
		t.Fatalf("unknown version negotiated %v", result["protocolVersion"])
	}
	want := map[string]any{"name": "kano-status-board", "version": buildinfo.Version}
	if !reflect.DeepEqual(result["serverInfo"], want) {
		t.Fatalf("serverInfo = %v", result["serverInfo"])
	}
	if !reflect.DeepEqual(result["capabilities"], map[string]any{"tools": map[string]any{"listChanged": true}}) {
		t.Fatalf("capabilities = %v", result["capabilities"])
	}
	if text, _ := result["instructions"].(string); !strings.Contains(text, "only in /admin") {
		t.Fatalf("instructions = %q", text)
	}

	malformed := post(t, handler, rpc(1, "initialize", map[string]any{}))
	if code, text := rpcErrorOf(t, malformed); code != codeInvalidParams || text != "Invalid params" {
		t.Fatalf("malformed initialize = %v %q", code, text)
	}
}

func TestToolsListSeparatesPublicAndControlTools(t *testing.T) {
	handler, _ := newHandler(t, nil)

	public := post(t, handler, rpc(1, "tools/list", map[string]any{}))
	if public.status != http.StatusOK {
		t.Fatalf("status = %d", public.status)
	}
	if got := toolNames(t, public); !reflect.DeepEqual(got, publicToolNames) {
		t.Fatalf("public tools = %v", got)
	}
	if got := public.header.Get("Vary"); got != "Authorization" {
		t.Fatalf("vary = %q", got)
	}

	control := post(t, handler, rpc(2, "tools/list", nil), bearer(controlToken))
	if got := toolNames(t, control); !reflect.DeepEqual(got, append(append([]string{}, publicToolNames...), controlToolNames...)) {
		t.Fatalf("control tools = %v", got)
	}

	listed := control.json(t)["result"].(map[string]any)["tools"].([]any)
	if len(listed) != 8 {
		t.Fatalf("tool count = %d", len(listed))
	}
	byName := map[string]map[string]any{}
	for _, entry := range listed {
		definition := entry.(map[string]any)
		name := definition["name"].(string)
		byName[name] = definition
		if definition["title"] == "" || definition["description"] == "" {
			t.Fatalf("%s lacks a title or description", name)
		}
		schema, _ := definition["inputSchema"].(map[string]any)
		if schema["type"] != "object" || schema["$schema"] != jsonSchemaDialect {
			t.Fatalf("%s schema = %v", name, schema)
		}
		if _, ok := schema["properties"].(map[string]any); !ok {
			t.Fatalf("%s schema has no properties", name)
		}
		annotations, _ := definition["annotations"].(map[string]any)
		if controlTools[name] {
			if annotations["destructiveHint"] != false {
				t.Fatalf("%s annotations = %v", name, annotations)
			}
		} else if annotations["readOnlyHint"] != true || annotations["openWorldHint"] != false {
			t.Fatalf("%s annotations = %v", name, annotations)
		}
	}

	properties := func(name string) map[string]any {
		return byName[name]["inputSchema"].(map[string]any)["properties"].(map[string]any)
	}
	if got := properties("dashboard_read")["days"]; !reflect.DeepEqual(got, map[string]any{"type": "integer", "minimum": float64(1), "maximum": float64(30)}) {
		t.Fatalf("days schema = %v", got)
	}
	schedule := properties("schedule_list")
	if got := keys(schedule); !reflect.DeepEqual(got, []string{"from", "page", "pageSize", "provenance", "search", "source", "to"}) {
		t.Fatalf("schedule_list properties = %v", got)
	}
	if got := schedule["pageSize"]; !reflect.DeepEqual(got, map[string]any{"type": "integer", "minimum": float64(1), "maximum": float64(100)}) {
		t.Fatalf("pageSize schema = %v", got)
	}
	if got := schedule["provenance"]; !reflect.DeepEqual(got, map[string]any{"type": "string", "enum": []any{"automatic", "manual"}}) {
		t.Fatalf("provenance schema = %v", got)
	}
	if got := schedule["from"]; !reflect.DeepEqual(got, map[string]any{"type": "string", "pattern": `^\d{4}-\d{2}-\d{2}$`}) {
		t.Fatalf("from schema = %v", got)
	}
	if got := properties("sync_status")["jobId"]; !reflect.DeepEqual(got, map[string]any{"type": "string", "maxLength": float64(100)}) {
		t.Fatalf("jobId schema = %v", got)
	}
	if byName["sync_start"]["annotations"].(map[string]any)["openWorldHint"] != true ||
		byName["dashboard_request_reload"]["annotations"].(map[string]any)["openWorldHint"] != false {
		t.Fatal("control tool open-world hints changed")
	}
}

func TestDashboardReadIsAPublicProjection(t *testing.T) {
	handler, _ := newHandler(t, nil)
	response := post(t, handler, toolCall("dashboard_read", nil))
	dashboard := toolValue(t, response)

	want := []string{"assets", "events", "focus", "meta", "posts", "profile", "profileMedia", "resources", "scheduleImages", "summary", "timeline", "videos"}
	if got := keys(dashboard); !reflect.DeepEqual(got, want) {
		t.Fatalf("dashboard keys = %v", got)
	}
	events := dashboard["events"].([]any)
	if len(events) != 2 {
		t.Fatalf("events = %v", events)
	}
	for _, entry := range append(events, dashboard["summary"].(map[string]any)["nextEvent"]) {
		event := entry.(map[string]any)
		for _, field := range []string{"manualLocked", "isUpcoming", "sourceItemId", "sourceKey", "extractionId", "deletedAt", "confidence"} {
			if _, present := event[field]; present {
				t.Fatalf("event exposes %s: %v", field, event)
			}
		}
		// 18 schedule facts plus the linked video's translations and
		// sourceLanguage.
		if len(event) != 20 {
			t.Fatalf("event field count = %d: %v", len(event), keys(event))
		}
	}
	meta := dashboard["meta"].(map[string]any)
	if meta["postWindowDays"] != float64(3) {
		t.Fatalf("default window = %v", meta["postWindowDays"])
	}
	lastSync := meta["lastSync"].(map[string]any)
	if got := keys(lastSync); !reflect.DeepEqual(got, []string{"finishedAt", "id", "message", "source", "startedAt", "status"}) {
		t.Fatalf("lastSync keys = %v", got)
	}
	assertNoOperatorData(t, "dashboard_read", response.body)

	wide := toolValue(t, post(t, handler, toolCall("dashboard_read", map[string]any{"days": 30})))
	if wide["meta"].(map[string]any)["postWindowDays"] != float64(30) {
		t.Fatalf("days=30 window = %v", wide["meta"])
	}
	// Unknown arguments are ignored, not rejected.
	toolValue(t, post(t, handler, toolCall("dashboard_read", map[string]any{"days": 3, "unknown": true})))
	// Arguments are optional at the protocol level.
	toolValue(t, post(t, handler, rpc(1, "tools/call", map[string]any{"name": "dashboard_read"})))
}

func TestScheduleListHidesOperatorMetadata(t *testing.T) {
	handler, _ := newHandler(t, nil)
	response := post(t, handler, toolCall("schedule_list", map[string]any{"pageSize": 100}))
	page := toolValue(t, response)

	if page["total"] != float64(2) || page["page"] != float64(1) || page["pageSize"] != float64(100) ||
		page["totalPages"] != float64(1) || page["hasPrevious"] != false || page["hasNext"] != false {
		t.Fatalf("page = %v", page)
	}
	items := page["items"].([]any)
	ids := []string{}
	for _, entry := range items {
		event := entry.(map[string]any)
		ids = append(ids, event["id"].(string))
		for _, field := range []string{"manualLocked", "deletedAt", "sourceItemId", "sourceKey", "extractionId", "confidence", "cancellationSourceItemId", "createdAt", "updatedAt"} {
			if _, present := event[field]; present {
				t.Fatalf("event exposes %s", field)
			}
		}
		if len(event) != 18 {
			t.Fatalf("event field count = %d: %v", len(event), keys(event))
		}
	}
	if !reflect.DeepEqual(ids, []string{"manual-public-event", "automatic-public-event"}) {
		t.Fatalf("order = %v", ids)
	}
	if !reflect.DeepEqual(page["events"], page["items"]) {
		t.Fatal("events and items differ")
	}
	assertNoOperatorData(t, "schedule_list", response.body)

	filtered := toolValue(t, post(t, handler, toolCall("schedule_list", map[string]any{
		"page": 1, "pageSize": 1, "search": "schedule", "provenance": "manual", "source": "manual",
		"from": "2026-09-01", "to": "2026-09-30",
	})))
	if filtered["total"] != float64(1) || filtered["items"].([]any)[0].(map[string]any)["id"] != "manual-public-event" {
		t.Fatalf("filtered = %v", filtered)
	}

	defaults := toolValue(t, post(t, handler, toolCall("schedule_list", map[string]any{"page": 999})))
	if defaults["pageSize"] != float64(25) || defaults["page"] != float64(1) {
		t.Fatalf("defaults = %v", defaults)
	}
}

func TestPostsListReadsTheStoredSnapshot(t *testing.T) {
	handler, _ := newHandler(t, nil)
	response := post(t, handler, toolCall("posts_list", nil))
	result := toolValue(t, response)
	if got := keys(result); !reflect.DeepEqual(got, []string{"days", "generatedAt", "posts"}) {
		t.Fatalf("keys = %v", got)
	}
	posts := result["posts"].([]any)
	if len(posts) != 2 || result["days"] != float64(3) || result["generatedAt"] != "2026-09-02T00:00:00.000Z" {
		t.Fatalf("result = %v", result)
	}
	if posts[0].(map[string]any)["id"] != "post-2" {
		t.Fatalf("newest post first, got %v", posts[0])
	}
	// A quote an operator hid is left out entirely.
	if quote, _ := posts[0].(map[string]any)["quote"].(map[string]any); quote["text"] != "shown quoted wording" {
		t.Fatalf("shown quote = %v", posts[0])
	}
	if hidden := posts[1].(map[string]any)["quote"]; hidden != nil || strings.Contains(response.body, "hidden quoted wording") {
		t.Fatalf("hidden quote = %v", hidden)
	}
	assertNoOperatorData(t, "posts_list", response.body)

	limited := toolValue(t, post(t, handler, toolCall("posts_list", map[string]any{"days": 30, "limit": 1})))
	if len(limited["posts"].([]any)) != 1 || limited["days"] != float64(30) {
		t.Fatalf("limited = %v", limited)
	}
}

func TestHealthReadReportsSanitizedSyncSummary(t *testing.T) {
	handler, _ := newHandler(t, nil)
	response := post(t, handler, toolCall("health_read", map[string]any{"ignored": 1}))
	health := toolValue(t, response)
	if got := keys(health); !reflect.DeepEqual(got, []string{"lastSync", "now", "ok", "revision", "service"}) {
		t.Fatalf("keys = %v", got)
	}
	if health["ok"] != true || health["service"] != "kano-status-board" || health["now"] != "2026-09-02T00:00:00.000Z" {
		t.Fatalf("health = %v", health)
	}
	if _, ok := health["revision"].(float64); !ok {
		t.Fatalf("revision = %v", health["revision"])
	}
	lastSync := health["lastSync"].(map[string]any)
	if got := keys(lastSync); !reflect.DeepEqual(got, []string{"finishedAt", "id", "message", "source", "startedAt", "status"}) {
		t.Fatalf("lastSync keys = %v", got)
	}
	if lastSync["source"] != "schedule" || lastSync["status"] != "success" || lastSync["message"] != "同步完成" {
		t.Fatalf("lastSync = %v", lastSync)
	}
	assertNoOperatorData(t, "health_read", response.body)
}

func TestSyncStatusReadsStoredRunsWithoutAManager(t *testing.T) {
	handler, _ := newHandler(t, nil)

	response := post(t, handler, toolCall("sync_status", nil))
	want := map[string]any{"jobs": []any{
		map[string]any{"id": float64(2), "kind": "scan", "status": "success", "startedAt": "2026-09-01T01:00:00.000Z", "finishedAt": "2026-09-01T01:00:05.000Z"},
		map[string]any{"id": float64(1), "kind": "sync", "status": "partial", "startedAt": "2026-09-01T00:00:00.000Z", "finishedAt": "2026-09-01T00:00:05.000Z"},
	}}
	if got := toolValue(t, response); !reflect.DeepEqual(got, want) {
		t.Fatalf("jobs = %v", got)
	}
	assertNoOperatorData(t, "sync_status", response.body)

	limited := toolValue(t, post(t, handler, toolCall("sync_status", map[string]any{"limit": 1})))
	if len(limited["jobs"].([]any)) != 1 {
		t.Fatalf("limited = %v", limited)
	}

	// A stored run is found by its job id or its numeric id; only the public
	// job fields come back.
	for _, id := range []string{"internal-job-placeholder", "1"} {
		lookup := post(t, handler, toolCall("sync_status", map[string]any{"jobId": id}))
		job := toolValue(t, lookup)["job"].(map[string]any)
		if !reflect.DeepEqual(job, want["jobs"].([]any)[1]) {
			t.Fatalf("lookup %q = %v", id, job)
		}
		assertNoOperatorData(t, "sync_status lookup", lookup.body)
	}
	missing := toolValue(t, post(t, handler, toolCall("sync_status", map[string]any{"jobId": "no-such-job"})))
	if job, present := missing["job"]; !present || job != nil {
		t.Fatalf("missing = %v", missing)
	}
	// An empty job id lists instead of looking up.
	if _, present := toolValue(t, post(t, handler, toolCall("sync_status", map[string]any{"jobId": ""})))["jobs"]; !present {
		t.Fatal("empty jobId should list jobs")
	}
}

func TestSyncStatusPrefersTheJobManager(t *testing.T) {
	started := "2026-09-02T00:00:01.000Z"
	failure := "raw job failure"
	manager := &fakeJobs{listed: []jobs.Job{
		{ID: "job-2", Kind: jobs.KindScan, Status: jobs.StatusRunning, CreatedAt: "2026-09-02T00:00:00.000Z", StartedAt: &started,
			TriggeredBy: "internal-trigger-placeholder", Error: &failure, Result: map[string]any{"counter": "job-counter-placeholder"}},
		{ID: "job-1", Status: jobs.StatusCompleted, CreatedAt: "2026-09-01T00:00:00.000Z"},
	}}
	manager.active = &manager.listed[0]
	handler, _ := newHandler(t, manager)

	response := post(t, handler, toolCall("sync_status", nil))
	want := []any{
		map[string]any{"id": "job-2", "kind": "scan", "status": "running", "createdAt": "2026-09-02T00:00:00.000Z", "startedAt": started, "finishedAt": nil},
		map[string]any{"id": "job-1", "kind": "sync", "status": "completed", "createdAt": "2026-09-01T00:00:00.000Z", "startedAt": nil, "finishedAt": nil},
	}
	if got := toolValue(t, response)["jobs"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("jobs = %v", got)
	}
	assertNoOperatorData(t, "sync_status", response.body)

	lookup := post(t, handler, toolCall("sync_status", map[string]any{"jobId": "job-2"}))
	if got := toolValue(t, lookup)["job"]; !reflect.DeepEqual(got, want[0]) {
		t.Fatalf("job = %v", got)
	}
	assertNoOperatorData(t, "sync_status lookup", lookup.body)

	// Unknown to the manager: fall back to the stored run history.
	fallback := toolValue(t, post(t, handler, toolCall("sync_status", map[string]any{"jobId": "scan-job-placeholder"})))
	if fallback["job"].(map[string]any)["id"] != float64(2) {
		t.Fatalf("fallback = %v", fallback)
	}
}

func TestToolArgumentValidationReportsToolErrors(t *testing.T) {
	handler, _ := newHandler(t, nil)
	cases := []struct {
		name      string
		arguments map[string]any
		want      string
	}{
		{"dashboard_read", map[string]any{"days": 31}, "Number must be less than or equal to 30 at days"},
		{"dashboard_read", map[string]any{"days": 0}, "Number must be greater than or equal to 1 at days"},
		{"dashboard_read", map[string]any{"days": 1.5}, "Expected integer, received float at days"},
		{"dashboard_read", map[string]any{"days": "3"}, "Expected number, received string at days"},
		{"dashboard_read", map[string]any{"days": nil}, "Expected number, received null at days"},
		{"dashboard_read", map[string]any{"days": []any{}}, "Expected number, received array at days"},
		{"posts_list", map[string]any{"limit": 101}, "Number must be less than or equal to 100 at limit"},
		{"schedule_list", map[string]any{"provenance": "other"}, "Invalid enum value. Expected 'automatic' | 'manual', received 'other' at provenance"},
		{"schedule_list", map[string]any{"provenance": true}, "Expected 'automatic' | 'manual', received boolean at provenance"},
		{"schedule_list", map[string]any{"from": "2026/01/01"}, "Invalid at from"},
		{"schedule_list", map[string]any{"source": strings.Repeat("s", 41)}, "String must contain at most 40 character(s) at source"},
		{"schedule_list", map[string]any{"search": map[string]any{}}, "Expected string, received object at search"},
		{"schedule_list", map[string]any{"from": "2026/01/01", "pageSize": 0, "search": strings.Repeat("x", 121)},
			"Number must be greater than or equal to 1 at pageSize\nString must contain at most 120 character(s) at search\nInvalid at from"},
		{"sync_status", map[string]any{"jobId": strings.Repeat("j", 101)}, "String must contain at most 100 character(s) at jobId"},
	}
	for _, c := range cases {
		got := toolErrorText(t, post(t, handler, toolCall(c.name, c.arguments)))
		want := "MCP error -32602: Input validation error: Invalid arguments for tool " + c.name + ": " + c.want
		if got != want {
			t.Errorf("%s %v:\n got %q\nwant %q", c.name, c.arguments, got, want)
		}
	}

	if got := toolErrorText(t, post(t, handler, toolCall("no_such_tool", nil))); got != "MCP error -32602: Tool no_such_tool not found" {
		t.Fatalf("unknown tool = %q", got)
	}

	// A malformed tools/call envelope is a protocol error, not a tool result.
	for _, params := range []any{
		map[string]any{},
		map[string]any{"name": 5},
		map[string]any{"name": "dashboard_read", "arguments": nil},
		map[string]any{"name": "dashboard_read", "arguments": []any{}},
		nil,
	} {
		response := post(t, handler, rpc(1, "tools/call", params))
		if code, text := rpcErrorOf(t, response); response.status != http.StatusOK || code != codeInvalidParams || text != "Invalid params" {
			t.Fatalf("params %v: %d %s", params, response.status, response.body)
		}
	}
}

func TestControlToolsRequireTheControlToken(t *testing.T) {
	manager := &fakeJobs{}
	handler, store := newHandler(t, manager)

	assertUnauthorized := func(label string, response reply) {
		t.Helper()
		if response.status != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, body = %s", label, response.status, response.body)
		}
		if got := response.header.Get("WWW-Authenticate"); got != `Bearer realm="kano-mcp"` {
			t.Fatalf("%s: challenge = %q", label, got)
		}
		if strings.TrimSpace(response.body) != `{"error":"mcp_control_auth_required"}` {
			t.Fatalf("%s: body = %s", label, response.body)
		}
		if got := response.header.Get("Cache-Control"); got != "no-store, no-transform" {
			t.Fatalf("%s: cache-control = %q", label, got)
		}
	}

	for _, name := range controlToolNames {
		assertUnauthorized(name+" without a token", post(t, handler, toolCall(name, nil)))
		assertUnauthorized(name+" with a wrong token", post(t, handler, toolCall(name, nil), bearer(wrongToken)))
	}
	// An admin session cookie is not an MCP credential.
	assertUnauthorized("cookie only", post(t, handler, toolCall("dashboard_request_reload", nil), map[string]string{"Cookie": "kano_admin_session=not-a-real-session"}))
	// A wrong token is rejected even when only public tools are requested.
	assertUnauthorized("wrong token on a public read", post(t, handler, rpc(1, "tools/list", nil), bearer(wrongToken)))
	// One control call anywhere in a batch protects the whole batch.
	assertUnauthorized("batch", post(t, handler, []any{toolCall("health_read", nil), toolCall("sync_start", nil)}))
	if len(manager.starts) != 0 {
		t.Fatalf("unauthorized requests started jobs: %v", manager.starts)
	}
	// Another authorization scheme is not a bearer token: it stays public.
	basic := post(t, handler, rpc(1, "tools/list", nil), map[string]string{"Authorization": "Basic c3ludGhldGlj"})
	if got := toolNames(t, basic); !reflect.DeepEqual(got, publicToolNames) {
		t.Fatalf("basic auth tools = %v", got)
	}

	before, err := store.GetDashboardRevision(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	reload := toolValue(t, post(t, handler, toolCall("dashboard_request_reload", nil), bearer(controlToken)))
	if !reflect.DeepEqual(reload, map[string]any{"revision": float64(before + 1)}) {
		t.Fatalf("reload = %v, before = %d", reload, before)
	}
	// The scheme name is case-insensitive.
	again := toolValue(t, post(t, handler, toolCall("dashboard_request_reload", nil), map[string]string{"Authorization": "bearer   " + controlToken}))
	if again["revision"] != float64(before+2) {
		t.Fatalf("second reload = %v", again)
	}
	after, _ := store.GetDashboardRevision(context.Background())
	if after != before+2 {
		t.Fatalf("stored revision = %d", after)
	}
}

func TestControlToolsStartSingleFlightJobs(t *testing.T) {
	manager := &fakeJobs{}
	handler, _ := newHandler(t, manager)

	first := post(t, handler, toolCall("sync_start", nil), bearer(controlToken))
	accepted := toolValue(t, first)
	if accepted["accepted"] != true || accepted["reason"] != nil {
		t.Fatalf("accepted = %v", accepted)
	}
	job := accepted["job"].(map[string]any)
	wantKeys := []string{"createdAt", "error", "fetchWindows", "finishedAt", "id", "kind", "progress", "result", "run", "startedAt", "status", "steps", "triggeredBy", "workflowId"}
	if got := keys(job); !reflect.DeepEqual(got, wantKeys) {
		t.Fatalf("job keys = %v", got)
	}
	if job["id"] != "job-1" || job["kind"] != "sync" || job["status"] != "queued" || job["triggeredBy"] != "mcp" {
		t.Fatalf("job = %v", job)
	}
	// Diagnostics never cross the MCP boundary, even with the control token.
	if job["error"] != nil || job["result"] != nil || job["run"] != nil {
		t.Fatalf("job diagnostics = %v", job)
	}
	assertNoOperatorData(t, "sync_start", first.body)

	second := post(t, handler, toolCall("scan_run_automatic", nil), bearer(controlToken))
	busy := toolValue(t, second)
	if busy["accepted"] != false || busy["reason"] != "already_running" || busy["job"].(map[string]any)["id"] != "job-1" {
		t.Fatalf("busy = %v", busy)
	}
	assertNoOperatorData(t, "scan_run_automatic", second.body)

	if !reflect.DeepEqual(manager.starts, []startCall{{jobs.KindSync, "mcp"}, {jobs.KindScan, "mcp"}}) {
		t.Fatalf("starts = %v", manager.starts)
	}

	// A queue failure is a fixed tool error; its text stays server-side.
	manager.startErr = errors.New("raw job failure")
	if got := toolErrorText(t, post(t, handler, toolCall("sync_start", nil), bearer(controlToken))); got != "Job could not be started" {
		t.Fatalf("start failure = %q", got)
	}
}

func TestControlToolsWithoutConfiguredTokenOrManager(t *testing.T) {
	store := newStore(t)
	manager := &fakeJobs{}
	unconfigured := New(store, options("", manager))
	for label, headers := range map[string]map[string]string{
		"no token":  nil,
		"any token": bearer(wrongToken),
	} {
		response := post(t, unconfigured, toolCall("sync_start", nil), headers)
		if response.status != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d", label, response.status)
		}
	}
	// With no configured token every bearer token is a wrong token.
	if response := post(t, unconfigured, rpc(1, "tools/list", nil), bearer(wrongToken)); response.status != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.status)
	}
	if len(manager.starts) != 0 {
		t.Fatalf("starts = %v", manager.starts)
	}

	// Without a job manager the control tools exist but fail as tool errors,
	// and the public status read keeps working from stored runs.
	withoutManager := New(store, options(controlToken, nil))
	for _, name := range []string{"sync_start", "scan_run_automatic"} {
		if got := toolErrorText(t, post(t, withoutManager, toolCall(name, nil), bearer(controlToken))); got != "Job manager unavailable" {
			t.Fatalf("%s = %q", name, got)
		}
	}
	if got := toolValue(t, post(t, withoutManager, toolCall("sync_status", nil))); !reflect.DeepEqual(got, map[string]any{"jobs": []any{}}) {
		t.Fatalf("status = %v", got)
	}
}

func TestDisabledEndpointAnswersNotFound(t *testing.T) {
	disabled := options(controlToken, nil)
	disabled.Enabled = false
	handler := New(newStore(t), disabled)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodOptions, http.MethodPut} {
		response := do(t, handler, method, "/mcp", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, standardHeaders, bearer(controlToken))
		if response.status != http.StatusNotFound || strings.TrimSpace(response.body) != `{"error":"mcp_disabled"}` {
			t.Fatalf("%s: %d %s", method, response.status, response.body)
		}
		if got := response.header.Get("Cache-Control"); got != "no-store, no-transform" {
			t.Fatalf("%s: cache-control = %q", method, got)
		}
	}
}

func TestUnsupportedHTTPMethods(t *testing.T) {
	handler, _ := newHandler(t, nil)
	cases := []struct {
		method, target string
		status         int
		body, allow    string
	}{
		{http.MethodGet, "/mcp", http.StatusMethodNotAllowed, `{"error":"mcp_get_not_supported"}`, "POST"},
		{http.MethodGet, "/mcp/", http.StatusMethodNotAllowed, `{"error":"mcp_get_not_supported"}`, "POST"},
		{http.MethodDelete, "/mcp", http.StatusMethodNotAllowed, `{"error":"mcp_delete_not_supported"}`, "POST"},
		{http.MethodPut, "/mcp", http.StatusMethodNotAllowed, `{"error":"method_not_allowed"}`, "POST"},
		{http.MethodOptions, "/mcp", http.StatusNoContent, "", "POST"},
		{http.MethodGet, "/mcp/other", http.StatusNotFound, `{"error":"mcp_not_found"}`, ""},
		{http.MethodPost, "/mcp/other", http.StatusNotFound, `{"error":"mcp_not_found"}`, ""},
	}
	for _, c := range cases {
		response := do(t, handler, c.method, c.target, "", map[string]string{"Accept": "text/event-stream"})
		if response.status != c.status || strings.TrimSpace(response.body) != c.body || response.header.Get("Allow") != c.allow {
			t.Errorf("%s %s: %d %q allow=%q", c.method, c.target, response.status, response.body, response.header.Get("Allow"))
		}
		if response.header.Get("Cache-Control") != "no-store, no-transform" || response.header.Get("X-Content-Type-Options") != "nosniff" || response.header.Get("Vary") != "Authorization" {
			t.Errorf("%s %s: headers = %v", c.method, c.target, response.header)
		}
	}

	// The handler also works behind http.StripPrefix.
	stripped := http.StripPrefix("/mcp", handler)
	if response := do(t, stripped, http.MethodPost, "/mcp", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, standardHeaders); response.status != http.StatusOK {
		t.Fatalf("stripped prefix: %d %s", response.status, response.body)
	}
}

func TestRequestBodyErrors(t *testing.T) {
	handler, _ := newHandler(t, nil)
	raw := func(body string, headers ...map[string]string) reply {
		return do(t, handler, http.MethodPost, "/mcp", body, append([]map[string]string{standardHeaders}, headers...)...)
	}

	for _, body := range []string{"{not json", `{"jsonrpc":"2.0"`, "5", `"text"`, "null", "   ", `{"jsonrpc":"2.0","id":1,"method":"ping"} trailing`} {
		response := raw(body)
		if response.status != http.StatusBadRequest || strings.TrimSpace(response.body) != `{"error":"invalid_json"}` {
			t.Errorf("body %q: %d %s", body, response.status, response.body)
		}
	}

	padding := strings.Repeat("x", bodyLimit)
	large := raw(`{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"` + padding + `"}}`)
	if large.status != http.StatusRequestEntityTooLarge || strings.TrimSpace(large.body) != `{"error":"request_too_large"}` {
		t.Fatalf("large body: %d %s", large.status, large.body)
	}
	// A body of exactly the limit is still read.
	prefix, suffix := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"`, `"}}`
	exact := raw(prefix + strings.Repeat("x", bodyLimit-len(prefix)-len(suffix)) + suffix)
	if exact.status != http.StatusOK {
		t.Fatalf("body at the limit: %d %s", exact.status, exact.body)
	}

	if response := raw(`{"jsonrpc":"2.0","id":1,"method":"ping"}`, map[string]string{"Content-Encoding": "gzip"}); response.status != http.StatusUnsupportedMediaType || strings.TrimSpace(response.body) != `{"error":"unsupported_encoding"}` {
		t.Fatalf("encoded body: %d %s", response.status, response.body)
	}

	// An empty body parses as nothing useful and fails as a JSON-RPC message.
	empty := raw("")
	if code, text := rpcErrorOf(t, empty); empty.status != http.StatusBadRequest || code != codeParseError || text != "Parse error: Invalid JSON-RPC message" {
		t.Fatalf("empty body: %d %s", empty.status, empty.body)
	}
}

func TestTransportHeaderRequirements(t *testing.T) {
	handler, _ := newHandler(t, nil)
	ping := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	send := func(headers map[string]string) reply {
		return do(t, handler, http.MethodPost, "/mcp", ping, headers)
	}
	expect := func(label string, response reply, status int, code float64, text string) {
		t.Helper()
		gotCode, gotText := rpcErrorOf(t, response)
		if response.status != status || gotCode != code || gotText != text {
			t.Fatalf("%s: %d %s", label, response.status, response.body)
		}
		if id, present := response.json(t)["id"]; !present || id != nil {
			t.Fatalf("%s: id = %v", label, id)
		}
		if got := response.header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("%s: content type = %q", label, got)
		}
	}

	notAcceptable := "Not Acceptable: Client must accept both application/json and text/event-stream"
	expect("no accept", send(map[string]string{"Content-Type": "application/json"}), http.StatusNotAcceptable, codeTransport, notAcceptable)
	expect("json only", send(map[string]string{"Content-Type": "application/json", "Accept": "application/json"}), http.StatusNotAcceptable, codeTransport, notAcceptable)
	expect("wildcard", send(map[string]string{"Content-Type": "application/json", "Accept": "*/*"}), http.StatusNotAcceptable, codeTransport, notAcceptable)

	unsupported := "Unsupported Media Type: Content-Type must be application/json"
	expect("text/plain", send(map[string]string{"Content-Type": "text/plain", "Accept": standardHeaders["Accept"]}), http.StatusUnsupportedMediaType, codeTransport, unsupported)
	expect("no content type", send(map[string]string{"Accept": standardHeaders["Accept"]}), http.StatusUnsupportedMediaType, codeTransport, unsupported)
	expect("smuggled media type", send(map[string]string{"Content-Type": "text/plain; a=application/json", "Accept": standardHeaders["Accept"]}), http.StatusUnsupportedMediaType, codeTransport, unsupported)

	// A control call in a body that is not JSON is never parsed, so it cannot
	// run; it fails on the media type instead.
	unparsed := do(t, handler, http.MethodPost, "/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sync_start"}}`,
		map[string]string{"Content-Type": "text/plain", "Accept": standardHeaders["Accept"]})
	expect("unparsed control call", unparsed, http.StatusUnsupportedMediaType, codeTransport, unsupported)

	if response := send(map[string]string{"Content-Type": "application/json; charset=utf-8", "Accept": standardHeaders["Accept"]}); response.status != http.StatusOK {
		t.Fatalf("charset parameter: %d %s", response.status, response.body)
	}

	with := func(extra map[string]string) reply {
		return do(t, handler, http.MethodPost, "/mcp", ping, standardHeaders, extra)
	}
	expect("old protocol header", with(map[string]string{"MCP-Protocol-Version": "1999-01-01"}), http.StatusBadRequest, codeTransport,
		"Bad Request: Unsupported protocol version: 1999-01-01 (supported versions: 2025-11-25, 2025-06-18, 2025-03-26, 2024-11-05, 2024-10-07)")
	for _, version := range supportedProtocolVersions {
		if response := with(map[string]string{"MCP-Protocol-Version": version}); response.status != http.StatusOK {
			t.Fatalf("protocol %s: %d %s", version, response.status, response.body)
		}
	}
	// Session headers are ignored: there are no sessions.
	if response := with(map[string]string{"Mcp-Session-Id": "synthetic-session"}); response.status != http.StatusOK || response.header.Get("Mcp-Session-Id") != "" {
		t.Fatalf("session header: %d %s", response.status, response.body)
	}
}

func TestJSONRPCDispatch(t *testing.T) {
	handler, _ := newHandler(t, nil)

	ping := post(t, handler, rpc("p", "ping", nil))
	if ping.status != http.StatusOK || !reflect.DeepEqual(ping.json(t), map[string]any{"jsonrpc": "2.0", "id": "p", "result": map[string]any{}}) {
		t.Fatalf("ping: %d %s", ping.status, ping.body)
	}

	for _, method := range []string{"does/not/exist", "resources/list", "prompts/list", "logging/setLevel"} {
		response := post(t, handler, rpc(7, method, nil))
		if code, text := rpcErrorOf(t, response); response.status != http.StatusOK || code != codeMethodNotFound || text != "Method not found" {
			t.Fatalf("%s: %d %s", method, response.status, response.body)
		}
		if response.json(t)["id"] != float64(7) {
			t.Fatalf("%s: id not echoed: %s", method, response.body)
		}
	}

	// Notifications and client responses are accepted without a body.
	for label, payload := range map[string]any{
		"initialized":        map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"},
		"other notification": map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": 1}},
		"client result":      map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}},
		"client error":       map[string]any{"jsonrpc": "2.0", "id": 1, "error": map[string]any{"code": -1, "message": "synthetic"}},
		"notification batch": []any{map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}},
		"empty batch":        []any{},
	} {
		response := post(t, handler, payload)
		if response.status != http.StatusAccepted || response.body != "" || response.header.Get("Content-Type") != "" {
			t.Fatalf("%s: %d %q %q", label, response.status, response.body, response.header.Get("Content-Type"))
		}
	}

	invalid := []any{
		map[string]any{"jsonrpc": "2.0", "id": 1},
		map[string]any{"jsonrpc": "1.0", "id": 1, "method": "ping"},
		map[string]any{"id": 1, "method": "ping"},
		map[string]any{"jsonrpc": "2.0", "id": nil, "method": "ping"},
		map[string]any{"jsonrpc": "2.0", "id": 1.5, "method": "ping"},
		map[string]any{"jsonrpc": "2.0", "id": true, "method": "ping"},
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": 5},
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping", "params": []any{1}},
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping", "extra": 1},
		map[string]any{"jsonrpc": "2.0", "id": 1, "result": "text"},
		map[string]any{},
		[]any{rpc(1, "ping", nil), 5},
	}
	for _, payload := range invalid {
		response := post(t, handler, payload)
		if code, text := rpcErrorOf(t, response); response.status != http.StatusBadRequest || code != codeParseError || text != "Parse error: Invalid JSON-RPC message" {
			t.Fatalf("payload %v: %d %s", payload, response.status, response.body)
		}
	}
}

func TestJSONRPCBatches(t *testing.T) {
	handler, _ := newHandler(t, nil)

	response := post(t, handler, []any{
		rpc(1, "ping", nil),
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"},
		rpc("health", "tools/call", map[string]any{"name": "health_read", "arguments": map[string]any{}}),
		rpc(3, "does/not/exist", nil),
	})
	if response.status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.status, response.body)
	}
	var batch []map[string]any
	if err := json.Unmarshal([]byte(response.body), &batch); err != nil {
		t.Fatalf("batch response is not an array: %v: %s", err, response.body)
	}
	if len(batch) != 3 {
		t.Fatalf("responses = %d", len(batch))
	}
	if batch[0]["id"] != float64(1) || !reflect.DeepEqual(batch[0]["result"], map[string]any{}) {
		t.Fatalf("first = %v", batch[0])
	}
	if batch[1]["id"] != "health" || batch[1]["result"].(map[string]any)["structuredContent"].(map[string]any)["ok"] != true {
		t.Fatalf("second = %v", batch[1])
	}
	if batch[2]["id"] != float64(3) || batch[2]["error"].(map[string]any)["code"] != float64(codeMethodNotFound) {
		t.Fatalf("third = %v", batch[2])
	}

	// One request in a batch is answered with a bare object.
	single := post(t, handler, []any{rpc(1, "ping", nil)})
	if single.status != http.StatusOK || single.json(t)["id"] != float64(1) {
		t.Fatalf("single: %d %s", single.status, single.body)
	}

	tooMany := make([]any, 0, maxBatchSize+1)
	for len(tooMany) <= maxBatchSize {
		tooMany = append(tooMany, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	}
	oversized := post(t, handler, tooMany)
	if code, text := rpcErrorOf(t, oversized); oversized.status != http.StatusBadRequest || code != codeInvalidRequest || text != "Invalid Request: Batch must not exceed 100 messages" {
		t.Fatalf("oversized: %d %s", oversized.status, oversized.body)
	}

	initialize := rpc(1, "initialize", map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "synthetic-client", "version": "1"},
	})
	mixed := post(t, handler, []any{initialize, rpc(2, "ping", nil)})
	if code, text := rpcErrorOf(t, mixed); mixed.status != http.StatusBadRequest || code != codeInvalidRequest || text != "Invalid Request: Only one initialization request is allowed" {
		t.Fatalf("mixed initialize: %d %s", mixed.status, mixed.body)
	}
}

func TestStoreFailuresAreFixedToolErrors(t *testing.T) {
	store := newStore(t)
	handler := New(store, options(controlToken, nil))
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"dashboard_read":           "Snapshot unavailable",
		"schedule_list":            "Snapshot unavailable",
		"posts_list":               "Snapshot unavailable",
		"health_read":              "Snapshot unavailable",
		"sync_status":              "Job status unavailable",
		"dashboard_request_reload": "Dashboard reload unavailable",
	}
	for name, text := range want {
		response := post(t, handler, toolCall(name, nil), bearer(controlToken))
		if got := toolErrorText(t, response); got != text {
			t.Errorf("%s = %q", name, got)
		}
		if strings.Contains(response.body, "sql") || strings.Contains(response.body, "database") || strings.Contains(response.body, "closed") {
			t.Errorf("%s leaks error text: %s", name, response.body)
		}
	}

	// A handler without any store degrades the same way instead of panicking.
	storeless := New(nil, Options{Enabled: true})
	if got := toolErrorText(t, post(t, storeless, toolCall("health_read", nil))); got != "Snapshot unavailable" {
		t.Fatalf("storeless = %q", got)
	}
}

func TestStartOutlivesTheRequestContext(t *testing.T) {
	manager := &fakeJobs{}
	handler, _ := newHandler(t, manager)
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sync_start"}}`)).WithContext(ctx)
	for key, value := range standardHeaders {
		request.Header.Set(key, value)
	}
	request.Header.Set("Authorization", "Bearer "+controlToken)
	handler.ServeHTTP(httptest.NewRecorder(), request)
	cancel()
	if len(manager.contexts) != 1 || manager.contexts[0].Err() != nil {
		t.Fatal("the job start context must not be cancelled with the request")
	}
}

func TestConstantTimeTokenMatch(t *testing.T) {
	cases := []struct {
		actual, expected string
		want             bool
	}{
		{controlToken, controlToken, true},
		{wrongToken, controlToken, false},
		{controlToken + "x", controlToken, false},
		{"", controlToken, false},
		{controlToken, "", false},
		{"", "", false},
	}
	for _, c := range cases {
		if got := constantTimeTokenMatch(c.actual, c.expected); got != c.want {
			t.Errorf("match(%q, %q) = %v", c.actual, c.expected, got)
		}
	}
}

func TestUsesControlTool(t *testing.T) {
	call := func(name any) map[string]any {
		return map[string]any{"jsonrpc": "2.0", "id": json.Number("1"), "method": "tools/call", "params": map[string]any{"name": name}}
	}
	cases := []struct {
		body any
		want bool
	}{
		{call("sync_start"), true},
		{call("scan_run_automatic"), true},
		{call("dashboard_request_reload"), true},
		{call("sync_status"), false},
		{call(json.Number("5")), false},
		{[]any{call("health_read"), call("sync_start")}, true},
		{[]any{call("health_read"), "text"}, false},
		{map[string]any{"method": "tools/list", "params": map[string]any{"name": "sync_start"}}, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := usesControlTool(c.body); got != c.want {
			t.Errorf("usesControlTool(%v) = %v", c.body, got)
		}
	}
}

func TestRequestIDsAreEchoedAsIntegersOrStrings(t *testing.T) {
	handler, _ := newHandler(t, nil)
	raw := func(body string) reply {
		return do(t, handler, http.MethodPost, "/mcp", body, standardHeaders)
	}
	for id, want := range map[string]string{
		`7`:      `7`,
		`-3`:     `-3`,
		`1.0`:    `1`,
		`2e2`:    `200`,
		`"7"`:    `"7"`,
		`"a<b>"`: `"a<b>"`,
	} {
		response := raw(`{"jsonrpc":"2.0","id":` + id + `,"method":"ping"}`)
		if response.status != http.StatusOK || !strings.Contains(response.body, `"id":`+want+`,`) {
			t.Errorf("id %s: %d %s", id, response.status, response.body)
		}
	}
	// The number 1 and the string "1" are different requests; a repeated id
	// is answered once.
	distinct := raw(`[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","id":"1","method":"ping"},{"jsonrpc":"2.0","id":1,"method":"ping"}]`)
	var responses []map[string]any
	if err := json.Unmarshal([]byte(distinct.body), &responses); err != nil || len(responses) != 2 {
		t.Fatalf("distinct ids: %s", distinct.body)
	}
}

func TestScheduleListOmitsMergedDuplicates(t *testing.T) {
	handler, store := newHandler(t, nil)
	if _, err := store.DB().Exec(`INSERT INTO events (id,source,title,starts_on,timezone,time_precision,event_type,provenance,manual_locked,cancellation_status,created_at,updated_at,merged_into,merge_source,merge_reason,merged_at)
		 VALUES ('merged-duplicate','x','Automatic schedule','2026-09-02','Asia/Tokyo','unknown','stream','automatic',0,'none','2026-09-01T00:00:00.000Z','2026-09-01T00:00:00.000Z','automatic-public-event','rule','rule_a:title','2026-09-01T00:00:00.000Z')`); err != nil {
		t.Fatal(err)
	}
	response := post(t, handler, toolCall("schedule_list", map[string]any{"pageSize": 100}))
	page := toolValue(t, response)
	if page["total"] != float64(2) || len(page["items"].([]any)) != 2 {
		t.Fatalf("page = %v", page)
	}
	if strings.Contains(response.body, "merged-duplicate") || strings.Contains(response.body, "rule_a") {
		t.Fatalf("merged duplicate leaked: %s", response.body)
	}
}
