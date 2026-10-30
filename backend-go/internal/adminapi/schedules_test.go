package adminapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

func scheduleBySource(t *testing.T, payload map[string]any, source string) map[string]any {
	t.Helper()
	schedules, ok := payload["schedules"].([]any)
	if !ok || len(schedules) != 3 {
		t.Fatalf("schedules=%v", payload["schedules"])
	}
	for _, entry := range schedules {
		if schedule := entry.(map[string]any); schedule["source"] == source {
			return schedule
		}
	}
	t.Fatalf("no %s schedule in %v", source, schedules)
	return nil
}

func TestSourceSchedulesAPI(t *testing.T) {
	c := newConsole(t, "development", "", &gatedSteps{})
	// Without jitter, plans land exactly one interval on.
	c.store.SetScheduleOffset(func(int64) int64 { return 0 })
	code, payload := c.call(t, "GET", "/api/admin/sources", "")
	if code != 200 {
		t.Fatalf("sources=%d %v", code, payload)
	}
	x := scheduleBySource(t, payload, "x")
	sync, _ := json.Marshal(x["sync"])
	if x["enabled"] != false || x["intervalMinutes"] != float64(30) || x["minIntervalMinutes"] != float64(15) || x["maxIntervalMinutes"] != float64(10080) ||
		string(sync) != `{"max":50,"mode":"incremental"}` || x["nextRunAt"] != nil || x["lastRun"] != nil || x["skipped"] != false {
		t.Fatalf("x=%v", x)
	}
	if _, reported := x["reason"]; reported {
		t.Fatalf("an active source has no skip reason: %v", x)
	}
	// Without a Bilibili reader the floor is the environment default.
	if bilibili := scheduleBySource(t, payload, "bilibili"); bilibili["sync"] != nil || bilibili["intervalMinutes"] != float64(180) || bilibili["minIntervalMinutes"] != float64(180) {
		t.Fatalf("bilibili=%v", bilibili)
	}
	for path, cases := range map[string]map[string]string{
		"/api/admin/sources/x/schedule": {
			`{"enabled":"yes"}`:                              "enabled must be a boolean",
			`{"intervalMinutes":10}`:                         "intervalMinutes must be an integer from 15 to 10080",
			`{"intervalMinutes":20.5}`:                       "intervalMinutes must be an integer from 15 to 10080",
			`{"sync":{"mode":"incremental","max":0}}`:        "max must be an integer from 1 to 200",
			`{"sync":{"mode":"count"}}`:                      "count must be an integer from 1 to 200",
			`{"sync":{"mode":"days","days":7}}`:              "days is not a sync field",
			`{"sync":{"mode":"recent"}}`:                     "sync mode must be incremental or count",
			`{"sync":{"mode":"incremental","count":5}}`:      "incremental sync takes max, not count",
			`{"sync":"count"}`:                               "sync must be an object",
			`{"steps":["x"]}`:                                "steps is not a source schedule field",
			`{"sync":{"mode":"count","count":5,"max":5}}`:    "count sync takes count, not max",
			`{"intervalMinutes":10081}`:                      "intervalMinutes must be an integer from 15 to 10080",
			`{"sync":{"mode":"incremental","max":201}}`:      "max must be an integer from 1 to 200",
			`{"sync":{"mode":"count","count":200,"x":true}}`: "x is not a sync field",
		},
		"/api/admin/sources/bilibili/schedule": {
			`{"intervalMinutes":120}`:                  "intervalMinutes must be an integer from 180 to 10080",
			`{"sync":{"mode":"count","count":20}}`:     "bilibili has no sync setting",
			`{"enabled":true,"intervalMinutes":179.0}`: "intervalMinutes must be an integer from 180 to 10080",
		},
	} {
		for body, message := range cases {
			if code, payload := c.call(t, "PUT", path, body); code != 400 || payload["error"] != "invalid_input" || payload["message"] != message {
				t.Fatalf("%s %s => %d %v", path, body, code, payload)
			}
		}
	}
	if code, payload = c.call(t, "PUT", "/api/admin/sources/mastodon/schedule", `{"enabled":true}`); code != 404 {
		t.Fatalf("unknown source=%d %v", code, payload)
	}
	// Enabling plans the next run one interval after now; omitted fields keep
	// their value.
	code, payload = c.call(t, "PUT", "/api/admin/sources/x/schedule", `{"enabled":true,"intervalMinutes":20}`)
	if code != 200 || payload["enabled"] != true || payload["intervalMinutes"] != float64(20) || payload["nextRunAt"] != "2026-03-02T03:20:00.000Z" {
		t.Fatalf("enable=%d %v", code, payload)
	}
	code, payload = c.call(t, "PUT", "/api/admin/sources/x/schedule", `{"sync":{"mode":"count","count":40}}`)
	sync, _ = json.Marshal(payload["sync"])
	if code != 200 || payload["intervalMinutes"] != float64(20) || payload["nextRunAt"] != "2026-03-02T03:20:00.000Z" || string(sync) != `{"count":40,"mode":"count"}` {
		t.Fatalf("sync=%d %v", code, payload)
	}
	if code, payload = c.call(t, "PUT", "/api/admin/sources/bilibili/schedule", `{"enabled":true}`); code != 200 || payload["nextRunAt"] != "2026-03-02T06:00:00.000Z" {
		t.Fatalf("bilibili=%d %v", code, payload)
	}
	// Enabled source schedules are the overview's timers; a timed row in
	// the workflows table is not counted.
	c.exec(t, `INSERT INTO workflows(id,name,steps_json,schedule_enabled,interval_minutes,next_run_at,created_at,updated_at) VALUES('sweep','Sweep','["translate"]',1,15,'2026-03-02T03:05:00.000Z','2026-03-01T00:00:00.000Z','2026-03-01T00:00:00.000Z')`)
	_, payload = c.call(t, "GET", "/api/admin/overview", "")
	if timers := payload["timers"].(map[string]any); timers["scheduled"] != float64(2) || timers["nextRunAt"] != "2026-03-02T03:20:00.000Z" {
		t.Fatalf("overview timers=%v", timers)
	}
	if _, listed := payload["workflows"]; listed {
		t.Fatalf("the overview lists no workflows: %v", payload)
	}
	_, payload = c.call(t, "GET", "/api/admin/sources", "")
	if x = scheduleBySource(t, payload, "x"); x["enabled"] != true || x["nextRunAt"] != "2026-03-02T03:20:00.000Z" {
		t.Fatalf("listed x=%v", x)
	}
	// SKIP_BILIBILI disables the Bilibili schedule in this process.
	skipped := &console{store: c.store, handler: New(Options{
		Store: c.store, Auth: newAuth(t, "development", ""), SkippedSteps: []string{"bilibili"},
		Now: func() time.Time { return time.Date(2026, 3, 2, 3, 0, 0, 0, time.UTC) },
	})}
	_, payload = skipped.call(t, "GET", "/api/admin/sources", "")
	if bilibili := scheduleBySource(t, payload, "bilibili"); bilibili["skipped"] != true || bilibili["reason"] != "environment" || bilibili["enabled"] != true {
		t.Fatalf("skipped bilibili=%v", bilibili)
	}
	_, payload = skipped.call(t, "GET", "/api/admin/overview", "")
	if timers := payload["timers"].(map[string]any); timers["scheduled"] != float64(1) {
		t.Fatalf("a skipped schedule is not a timer: %v", timers)
	}
	// A schedule's run is labelled by its source and shows its frozen sync.
	ctx := context.Background()
	run, err := c.store.CreateWorkflowRun(ctx, sqlite.WorkflowDefinition{ID: sqlite.SourceScheduleWorkflow("x"), Steps: []string{"x", "media"}, SourceSync: map[string]sqlite.SourceSync{"x": {Mode: sqlite.SyncCount, Count: 40}}},
		"scheduler", "", map[string]bool{"x": true, "media": true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err = c.store.LabelSyncRun(ctx, run.ID, "workflow", "source-schedule"); err != nil {
		t.Fatal(err)
	}
	_, payload = c.call(t, "GET", "/api/admin/runs/"+run.ID, "")
	listed := payload["run"].(map[string]any)
	if listed["sourceSchedule"] != "x" || listed["triggeredBy"] != "source-schedule" || listed["kind"] != "workflow" || listed["workflowId"] != nil ||
		listed["fetchWindows"].(map[string]any)["x"].(map[string]any)["count"] != float64(40) {
		t.Fatalf("run=%v", listed)
	}
	_, payload = c.call(t, "GET", "/api/admin/sources", "")
	if last := scheduleBySource(t, payload, "x")["lastRun"].(map[string]any); last["id"] != run.ID || last["status"] != "queued" || last["at"] == nil {
		t.Fatalf("last run=%v", last)
	}
}

func TestRunNowSubmitsTheSourceChainOnce(t *testing.T) {
	steps := &gatedSteps{gate: make(chan struct{})}
	c := newConsole(t, "development", "", steps)
	c.store.SetScheduleOffset(func(int64) int64 { return 0 })
	if code, payload := c.call(t, "PUT", "/api/admin/sources/x/schedule", `{"enabled":true,"sync":{"mode":"count","count":40}}`); code != 200 || payload["nextRunAt"] != "2026-03-02T03:30:00.000Z" {
		t.Fatalf("x schedule=%d %v", code, payload)
	}
	if code, payload := c.call(t, "POST", "/api/admin/sources/mastodon/run", ""); code != 404 || payload["error"] != "not_found" {
		t.Fatalf("unknown source=%d %v", code, payload)
	}
	code, payload := c.call(t, "POST", "/api/admin/sources/x/run", "")
	if code != 202 {
		t.Fatalf("run now=%d %v", code, payload)
	}
	id := runID(t, payload)
	<-steps.seen
	// One run at a time: a second Run now finds the queue busy.
	code, payload = c.call(t, "POST", "/api/admin/sources/bilibili/run", "")
	if code != 409 || payload["error"] != "busy" || runID(t, payload) != id {
		t.Fatalf("busy=%d %v", code, payload)
	}
	close(steps.gate)
	run := c.waitForRun(t, id, "completed")
	if run["kind"] != "workflow" || run["sourceSchedule"] != "x" || run["triggeredBy"] != "admin" || run["workflowId"] != nil ||
		fmt.Sprint(run["steps"]) != "[x media schedule translate reconcile]" {
		t.Fatalf("run=%v", run)
	}
	// The chain reads X with the schedule's sync, frozen on the run.
	if window := run["fetchWindows"].(map[string]any)["x"].(map[string]any); window["mode"] != "count" || window["count"] != float64(40) {
		t.Fatalf("sync=%v", window)
	}
	// Run now leaves the timer's plan alone and shows as the last run.
	_, payload = c.call(t, "GET", "/api/admin/sources", "")
	x := scheduleBySource(t, payload, "x")
	if x["nextRunAt"] != "2026-03-02T03:30:00.000Z" || x["lastRun"].(map[string]any)["id"] != id {
		t.Fatalf("x schedule=%v", x)
	}
	// Bilibili's chain has no sync setting.
	code, payload = c.call(t, "POST", "/api/admin/sources/bilibili/run", "")
	if code != 202 {
		t.Fatalf("bilibili=%d %v", code, payload)
	}
	bilibili := c.waitForRun(t, runID(t, payload), "completed")
	if bilibili["sourceSchedule"] != "bilibili" || fmt.Sprint(bilibili["steps"]) != "[bilibili media translate reconcile]" || bilibili["fetchWindows"] != nil {
		t.Fatalf("bilibili run=%v", bilibili)
	}
	// SKIP_* refuses Run now for that source.
	skipped := &console{store: c.store, handler: New(Options{
		Store: c.store, Auth: newAuth(t, "development", ""), Runs: c.runs, SkippedSteps: []string{"youtube"},
		Now: func() time.Time { return time.Date(2026, 3, 2, 3, 0, 0, 0, time.UTC) },
	})}
	if code, payload = skipped.call(t, "POST", "/api/admin/sources/youtube/run", ""); code != 400 || payload["message"] != "the youtube source is disabled for this process" {
		t.Fatalf("skipped=%d %v", code, payload)
	}
}

func TestSourceScheduleChainAPI(t *testing.T) {
	c := newConsole(t, "development", "", &gatedSteps{})
	_, payload := c.call(t, "GET", "/api/admin/sources", "")
	chain, _ := json.Marshal(scheduleBySource(t, payload, "x")["chain"])
	// The default is the former order; the API needs its credential, and the
	// console shows today's usage and each account's streak.
	if !strings.Contains(string(chain), `"methods":[{"available":true,"enabled":true,"id":"timeline"},{"available":true,"enabled":true,"id":"profile"},{"available":false,"enabled":false,"id":"api","requires":"X_API_BEARER_TOKEN"}]`) ||
		!strings.Contains(string(chain), `"apiGate":3`) || !strings.Contains(string(chain), `"apiDailyCap":100`) || !strings.Contains(string(chain), `"apiUsage":{"day":"2026-03-02","posts":0}`) {
		t.Fatalf("x chain=%s", chain)
	}
	if bilibili, _ := json.Marshal(scheduleBySource(t, payload, "bilibili")["chain"]); string(bilibili) != `{"fixed":true,"methods":[{"available":true,"enabled":true,"id":"list"}]}` {
		t.Fatalf("bilibili chain=%s", bilibili)
	}
	for path, cases := range map[string]map[string]string{
		"/api/admin/sources/x/schedule": {
			`{"chain":[]}`: "chain must be an object",
			`{"chain":{"methods":[{"id":"timeline","enabled":true},{"id":"search","enabled":true},{"id":"api","enabled":true}]}}`:     "search is not a method of the x chain",
			`{"chain":{"methods":[{"id":"timeline","enabled":true},{"id":"timeline","enabled":true},{"id":"api","enabled":true}]}}`:   "the chain lists timeline twice",
			`{"chain":{"methods":[{"id":"timeline","enabled":true},{"id":"api","enabled":true}]}}`:                                    "the chain must list every method: timeline, profile, api",
			`{"chain":{"methods":[{"id":"timeline","enabled":false},{"id":"profile","enabled":false},{"id":"api","enabled":false}]}}`: "the chain must enable at least one method",
			`{"chain":{"methods":[{"id":"timeline"}]}}`: "chain methods must be a list of { id, enabled }",
			`{"chain":{"apiGate":101}}`:                 "apiGate must be an integer from 0 to 100",
			`{"chain":{"apiDailyCap":0}}`:               "apiDailyCap must be an integer from 1 to 10000",
		},
		"/api/admin/sources/youtube/schedule": {
			`{"chain":{"apiGate":3}}`: "apiGate is not a field of the youtube chain",
		},
		"/api/admin/sources/bilibili/schedule": {
			`{"chain":{"methods":[{"id":"list","enabled":true}]}}`: "the bilibili chain is fixed",
		},
	} {
		for body, message := range cases {
			if code, payload := c.call(t, "PUT", path, body); code != 400 || payload["message"] != message {
				t.Fatalf("%s %s => %d %v", path, body, code, payload)
			}
		}
	}
	code, payload := c.call(t, "PUT", "/api/admin/sources/x/schedule", `{"chain":{"methods":[{"id":"api","enabled":true},{"id":"timeline","enabled":true},{"id":"profile","enabled":false}],"apiGate":0,"apiDailyCap":40}}`)
	chain, _ = json.Marshal(payload["chain"])
	if code != 200 || !strings.Contains(string(chain), `"methods":[{"available":false,"enabled":true,"id":"api","requires":"X_API_BEARER_TOKEN"},{"available":true,"enabled":true,"id":"timeline"},{"available":true,"enabled":false,"id":"profile"}]`) ||
		!strings.Contains(string(chain), `"apiGate":0`) || !strings.Contains(string(chain), `"apiDailyCap":40`) {
		t.Fatalf("saved=%d %s", code, chain)
	}
	// Omitted fields keep their value, the chain included.
	if code, payload = c.call(t, "PUT", "/api/admin/sources/x/schedule", `{"chain":{"apiDailyCap":50}}`); code != 200 {
		t.Fatalf("cap=%d %v", code, payload)
	}
	if chain, _ = json.Marshal(payload["chain"]); !strings.Contains(string(chain), `"enabled":true,"id":"api"`) || !strings.Contains(string(chain), `"apiGate":0`) {
		t.Fatalf("kept=%s", chain)
	}
}
