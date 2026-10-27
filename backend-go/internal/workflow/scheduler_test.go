package workflow

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

func TestSchedulerIsDisabledByDefault(t *testing.T) {
	s := &Scheduler{}
	s.Start()
	status := s.Status()
	if status["running"] != false || status["enabled"] != false || status["tickSeconds"] != 30 {
		t.Fatalf("unexpected default scheduler status: %#v", status)
	}
	s.Stop()
}

func TestSchedulerRunsOnlySourceSchedulesEarliestFirst(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := sqlite.OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	clock := time.Date(2026, 3, 2, 3, 10, 0, 0, time.UTC)
	now := func() time.Time { return clock }
	// A timed saved workflow from an earlier release is retired: it is
	// never read, run or changed (ADR-0003).
	const retired = `INSERT INTO workflows(id,name,steps_json,schedule_enabled,interval_minutes,next_run_at,created_at,updated_at) VALUES('sweep','Sweep','["translate"]',1,15,'2026-03-02T03:00:00.000Z','2026-03-01T00:00:00.000Z','2026-03-01T00:00:00.000Z')`
	testutil.Exec(t, p, retired)
	enable := func(source string, interval int, sync *sqlite.SourceSync) {
		t.Helper()
		schedule, err := s.SourceScheduleFor(ctx, source, 60)
		if err != nil {
			t.Fatal(err)
		}
		schedule.Enabled, schedule.IntervalMinutes = true, interval
		if sync != nil {
			schedule.Sync = sync
		}
		if _, err = s.SaveSourceSchedule(ctx, schedule, 60, clock); err != nil {
			t.Fatal(err)
		}
	}
	enable("x", 15, &sqlite.SourceSync{Mode: sqlite.SyncCount, Count: 20})
	enable("bilibili", 60, nil)
	testutil.Exec(t, p, `UPDATE source_schedules SET next_run_at=CASE source WHEN 'x' THEN '2026-03-02T03:05:00.000Z' ELSE '2026-03-02T02:50:00.000Z' END`)

	all := map[string]bool{"x": true, "youtube": true, "bilibili": true, "media": true, "schedule": true, "translate": true, "reconcile": true}
	scheduler := &Scheduler{
		Store: s, Executor: &Executor{Store: s, Clock: now, SyntheticSteps: all},
		Enabled: true, BilibiliFloor: 60, Now: now,
	}
	tick := func() *Run {
		t.Helper()
		run, err := scheduler.Tick(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return run
	}
	steps := func(run *Run) []string {
		out := []string{}
		for _, step := range run.Steps {
			out = append(out, step.StepID)
		}
		return out
	}
	// The overdue Bilibili schedule goes before X.
	first := tick()
	if first == nil || first.WorkflowID != "@source-bilibili" || first.Status != "succeeded" || first.TriggerSource != "scheduler" ||
		!slices.Equal(steps(first), []string{"bilibili", "media", "translate", "reconcile"}) || len(first.SourceSync) != 0 {
		t.Fatalf("first=%#v", first)
	}
	history, err := s.SyncRunForJob(ctx, first.ID)
	if err != nil || history == nil || history.TriggeredBy == nil || *history.TriggeredBy != TriggeredBySourceSchedule {
		t.Fatalf("label=%#v %v", history, err)
	}
	third := tick()
	if third == nil || third.WorkflowID != "@source-x" || !slices.Equal(steps(third), []string{"x", "media", "schedule", "translate", "reconcile"}) ||
		third.SourceSync["x"] != (sqlite.SourceSync{Mode: sqlite.SyncCount, Count: 20}) {
		t.Fatalf("third=%#v", third)
	}
	if idle := tick(); idle != nil {
		t.Fatalf("nothing is due, got %#v", idle)
	}
	// Each schedule planned its next run from its claim at 03:10.
	x, _ := s.SourceScheduleFor(ctx, "x", 60)
	bilibili, _ := s.SourceScheduleFor(ctx, "bilibili", 60)
	if *x.NextRunAt != "2026-03-02T03:25:00.000Z" || *bilibili.NextRunAt != "2026-03-02T04:10:00.000Z" || x.LastRun.ID != third.ID {
		t.Fatalf("planned x=%v bilibili=%v", *x.NextRunAt, *bilibili.NextRunAt)
	}
	// SKIP_X leaves the X schedule out; the others still run.
	clock = time.Date(2026, 3, 2, 5, 0, 0, 0, time.UTC)
	scheduler.SkipSources = map[string]bool{"x": true}
	if run := tick(); run == nil || run.WorkflowID != "@source-bilibili" {
		t.Fatalf("after skip=%#v", run)
	}
	if run := tick(); run != nil {
		t.Fatalf("skipped x ran: %#v", run)
	}
	var row string
	if err = sqliteRow(p, `SELECT schedule_enabled||' '||next_run_at||' '||COALESCE(last_run_at,'-') FROM workflows WHERE id='sweep'`, &row); err != nil || row != "1 2026-03-02T03:00:00.000Z -" {
		t.Fatalf("retired workflow row=%q %v", row, err)
	}
	// WORKFLOW_SCHEDULER_ENABLED=0 stops the source schedules.
	scheduler.SkipSources, scheduler.Enabled = nil, false
	if run := tick(); run != nil {
		t.Fatalf("disabled scheduler ran %#v", run)
	}
}

func TestSchedulerLeavesABusySourceDue(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := sqlite.OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	clock := time.Date(2026, 3, 2, 3, 0, 0, 0, time.UTC)
	schedule, _ := s.SourceScheduleFor(ctx, "youtube", 180)
	schedule.Enabled = true
	if _, err = s.SaveSourceSchedule(ctx, schedule, 180, clock.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	// Another run holds the single-flight slot.
	if _, err = s.CreateWorkflowRun(ctx, sqlite.WorkflowDefinition{ID: sqlite.AdhocSyncWorkflow, Steps: []string{"media"}}, "operator", "", map[string]bool{"media": true}, clock); err != nil {
		t.Fatal(err)
	}
	scheduler := &Scheduler{Store: s, Executor: &Executor{Store: s}, Enabled: true, BilibiliFloor: 180, Now: func() time.Time { return clock }}
	if _, err = scheduler.Tick(ctx); err == nil {
		t.Fatal("a busy queue must be reported")
	}
	if due, err := s.DueSourceSchedule(ctx, clock, nil, 180); err != nil || due == nil || due.Source != "youtube" {
		t.Fatalf("youtube must stay due: %+v %v", due, err)
	}
}

// sqliteRow reads one value from the snapshot at path.
func sqliteRow(path, query string, value any) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	return db.QueryRow(query).Scan(value)
}
