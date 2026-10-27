package adminapi

import (
	"context"
	"encoding/json"
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
	// Enabled source schedules count as timers on the overview.
	_, payload = c.call(t, "GET", "/api/admin/overview", "")
	if workflows := payload["workflows"].(map[string]any); workflows["scheduled"] != float64(2) || workflows["nextRunAt"] != "2026-03-02T03:20:00.000Z" {
		t.Fatalf("overview workflows=%v", workflows)
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
	if workflows := payload["workflows"].(map[string]any); workflows["scheduled"] != float64(1) {
		t.Fatalf("a skipped schedule is not a timer: %v", workflows)
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
