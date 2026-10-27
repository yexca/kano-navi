package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openScheduleStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStandalone(filepath.Join(t.TempDir(), "database", "kano.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func scheduleOf(t *testing.T, s *Store, source string, floor int) SourceSchedule {
	t.Helper()
	schedule, err := s.SourceScheduleFor(context.Background(), source, floor)
	if err != nil {
		t.Fatal(err)
	}
	return schedule
}

func TestPrepareSourceSchedulesMovesWorkflowTimersOnce(t *testing.T) {
	s := openScheduleStore(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 2, 3, 0, 0, 0, time.UTC)
	for _, statement := range []string{
		// Timed workflows that read sources, as an earlier release kept them.
		`INSERT INTO workflows(id,name,steps_json,source_sync_json,schedule_enabled,interval_minutes,next_run_at,created_at,updated_at) VALUES('wf-frequent','Frequent','["x","youtube","media"]','{"x":{"mode":"count","count":30}}',1,20,'2026-03-02T03:10:00.000Z','2026-03-01T00:00:00.000Z','2026-03-01T00:00:00.000Z')`,
		`INSERT INTO workflows(id,name,steps_json,source_sync_json,schedule_enabled,interval_minutes,next_run_at,created_at,updated_at) VALUES('wf-slow','Slow','["x","bilibili","media"]','{"x":{"mode":"incremental","max":10}}',1,60,'2026-03-02T03:30:00.000Z','2026-03-01T00:00:01.000Z','2026-03-01T00:00:01.000Z')`,
		// A timed sweep without a source and a manual source workflow stay as they are.
		`INSERT INTO workflows(id,name,steps_json,schedule_enabled,interval_minutes,next_run_at,created_at,updated_at) VALUES('wf-sweep','Sweep','["translate"]',1,45,'2026-03-02T04:00:00.000Z','2026-03-01T00:00:02.000Z','2026-03-01T00:00:02.000Z')`,
		`INSERT INTO workflows(id,name,steps_json,schedule_enabled,interval_minutes,created_at,updated_at) VALUES('wf-manual','Manual','["youtube"]',0,15,'2026-03-01T00:00:03.000Z','2026-03-01T00:00:03.000Z')`,
	} {
		if _, err := s.db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if err := s.PrepareSourceSchedules(ctx, 240, now); err != nil {
		t.Fatal(err)
	}
	x, youtube, bilibili := scheduleOf(t, s, "x", 240), scheduleOf(t, s, "youtube", 240), scheduleOf(t, s, "bilibili", 240)
	if !x.Enabled || x.IntervalMinutes != 20 || *x.Sync != (SourceSync{Mode: SyncCount, Count: 30}) || *x.NextRunAt != "2026-03-02T03:20:00.000Z" {
		t.Fatalf("x took the smallest interval and its workflow's sync: %+v %+v", x, x.Sync)
	}
	if !youtube.Enabled || youtube.IntervalMinutes != 20 || *youtube.Sync != (SourceSync{Mode: SyncIncremental, Max: DefaultSyncMax}) {
		t.Fatalf("youtube=%+v %+v", youtube, youtube.Sync)
	}
	if !bilibili.Enabled || bilibili.IntervalMinutes != 240 || bilibili.Sync != nil {
		t.Fatalf("bilibili was not raised to the floor: %+v", bilibili)
	}
	workflowState := func(id string) string {
		t.Helper()
		var enabled int
		var next *string
		if err := s.db.QueryRow(`SELECT schedule_enabled,next_run_at FROM workflows WHERE id=?`, id).Scan(&enabled, &next); err != nil {
			t.Fatal(err)
		}
		if next == nil {
			return map[int]string{0: "off", 1: "on"}[enabled]
		}
		return map[int]string{0: "off", 1: "on"}[enabled] + " " + *next
	}
	for id, want := range map[string]string{"wf-frequent": "off", "wf-slow": "off", "wf-sweep": "on 2026-03-02T04:00:00.000Z", "wf-manual": "off"} {
		if got := workflowState(id); got != want {
			t.Fatalf("%s timer=%q, want %q", id, got, want)
		}
	}
	var steps string
	if err := s.db.QueryRow(`SELECT steps_json FROM workflows WHERE id='wf-slow'`).Scan(&steps); err != nil || steps != `["x","bilibili","media"]` {
		t.Fatalf("steps changed: %s %v", steps, err)
	}
	// The migration runs once: a timer the operator turns back on and a
	// schedule they turn off stay that way on the next start.
	if _, err := s.db.Exec(`UPDATE workflows SET schedule_enabled=1,next_run_at='2026-03-02T05:00:00.000Z' WHERE id='wf-slow'`); err != nil {
		t.Fatal(err)
	}
	x.Enabled = false
	if _, err := s.SaveSourceSchedule(ctx, x, 240, now); err != nil {
		t.Fatal(err)
	}
	if err := s.PrepareSourceSchedules(ctx, 240, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if scheduleOf(t, s, "x", 240).Enabled || workflowState("wf-slow") != "on 2026-03-02T05:00:00.000Z" {
		t.Fatal("a second start repeated the migration")
	}
	// Every start raises a Bilibili interval below a raised floor.
	if err := s.PrepareSourceSchedules(ctx, 300, now); err != nil {
		t.Fatal(err)
	}
	if got := scheduleOf(t, s, "bilibili", 300).IntervalMinutes; got != 300 {
		t.Fatalf("bilibili interval=%d", got)
	}
}

func TestPrepareSourceSchedulesKeepsAFreshInstallationDisabled(t *testing.T) {
	s := openScheduleStore(t)
	ctx := context.Background()
	if err := s.PrepareSourceSchedules(ctx, 180, time.Now()); err != nil {
		t.Fatal(err)
	}
	schedules, err := s.SourceSchedules(ctx, 180)
	if err != nil || len(schedules) != 3 {
		t.Fatalf("schedules=%+v %v", schedules, err)
	}
	for i, want := range []struct {
		source   string
		interval int
		sync     bool
	}{{"x", 30, true}, {"youtube", 60, true}, {"bilibili", 180, false}} {
		got := schedules[i]
		if got.Source != want.source || got.Enabled || got.IntervalMinutes != want.interval || (got.Sync != nil) != want.sync || got.NextRunAt != nil || got.LastRun != nil {
			t.Fatalf("default %s=%+v", want.source, got)
		}
	}
	var migrated string
	if err = s.db.QueryRow(`SELECT value FROM app_settings WHERE key='source_schedules_migrated'`).Scan(&migrated); err != nil || migrated != "1" {
		t.Fatalf("migration marker=%q %v", migrated, err)
	}
}

func TestSourceSchedulePlansItsNextRun(t *testing.T) {
	s := openScheduleStore(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 2, 3, 0, 0, 0, time.UTC)
	x := scheduleOf(t, s, "x", 180)
	x.Enabled = true
	saved, err := s.SaveSourceSchedule(ctx, x, 180, now)
	if err != nil || *saved.NextRunAt != "2026-03-02T03:30:00.000Z" {
		t.Fatalf("enable=%+v %v", saved, err)
	}
	// A sync change keeps the plan; an interval change plans one interval on.
	saved.Sync = &SourceSync{Mode: SyncCount, Count: 20}
	if saved, err = s.SaveSourceSchedule(ctx, saved, 180, now.Add(10*time.Minute)); err != nil || *saved.NextRunAt != "2026-03-02T03:30:00.000Z" || saved.Sync.Count != 20 {
		t.Fatalf("sync=%+v %v", saved, err)
	}
	saved.IntervalMinutes = 45
	if saved, err = s.SaveSourceSchedule(ctx, saved, 180, now.Add(10*time.Minute)); err != nil || *saved.NextRunAt != "2026-03-02T03:55:00.000Z" {
		t.Fatalf("interval=%+v %v", saved, err)
	}
	bilibili := scheduleOf(t, s, "bilibili", 180)
	for name, input := range map[string]SourceSchedule{
		"short":         {Source: "x", IntervalMinutes: 10, Sync: saved.Sync},
		"below floor":   {Source: "bilibili", IntervalMinutes: 120},
		"bilibili sync": {Source: "bilibili", IntervalMinutes: 180, Sync: saved.Sync},
		"x without":     {Source: "x", IntervalMinutes: 30},
		"invalid sync":  {Source: "x", IntervalMinutes: 30, Sync: &SourceSync{Mode: SyncCount, Count: MaxSyncLimit + 1}},
	} {
		if _, err = s.SaveSourceSchedule(ctx, input, 180, now); !errors.Is(err, ErrWorkflowValidation) {
			t.Fatalf("%s err=%v", name, err)
		}
	}
	if _, err = s.SaveSourceSchedule(ctx, SourceSchedule{Source: "mastodon", IntervalMinutes: 30}, 180, now); !errors.Is(err, ErrWorkflowNotFound) {
		t.Fatalf("unknown source err=%v", err)
	}
	if bilibili.Enabled {
		t.Fatal("bilibili changed")
	}
	// Due at its planned time, unless SKIP_X disables the source.
	if due, err := s.DueSourceSchedule(ctx, now.Add(54*time.Minute), nil, 180); err != nil || due != nil {
		t.Fatalf("early due=%+v %v", due, err)
	}
	due, err := s.DueSourceSchedule(ctx, now.Add(55*time.Minute), nil, 180)
	if err != nil || due == nil || due.Source != "x" {
		t.Fatalf("due=%+v %v", due, err)
	}
	if due, err = s.DueSourceSchedule(ctx, now.Add(55*time.Minute), map[string]bool{"x": true}, 180); err != nil || due != nil {
		t.Fatalf("skipped due=%+v %v", due, err)
	}
	// Claiming its run plans the next one interval after the claim.
	run, err := s.CreateWorkflowRun(ctx, WorkflowDefinition{ID: SourceScheduleWorkflow("x"), Steps: []string{"x"}}, "scheduler", "", map[string]bool{"x": true}, now.Add(56*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimWorkflowRun(ctx, run.ID, "test", now.Add(57*time.Minute), time.Minute); err != nil {
		t.Fatal(err)
	}
	claimed := scheduleOf(t, s, "x", 180)
	if *claimed.NextRunAt != "2026-03-02T04:42:00.000Z" || claimed.LastRun == nil || claimed.LastRun.ID != run.ID || claimed.LastRun.Status != "running" {
		t.Fatalf("claimed=%+v %+v", claimed, claimed.LastRun)
	}
	if source, ok := SourceScheduleOf(run.WorkflowID); !ok || source != "x" {
		t.Fatalf("source of %s=%q %v", run.WorkflowID, source, ok)
	}
	if _, ok := SourceScheduleOf("@source-mastodon"); ok {
		t.Fatal("unknown source schedule accepted")
	}
	// Disabling clears the plan.
	claimed.Enabled = false
	if saved, err = s.SaveSourceSchedule(ctx, claimed, 180, now); err != nil || saved.NextRunAt != nil {
		t.Fatalf("disable=%+v %v", saved, err)
	}
}
