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
	// Without jitter, plans land exactly one interval on.
	s.SetScheduleOffset(func(int64) int64 { return 0 })
	return s
}

// offsetOf draws the same fraction of the spread every time.
func offsetOf(numerator, denominator int64) func(int64) int64 {
	return func(spread int64) int64 { return spread * numerator / denominator }
}

func TestPlannedIntervalJittersWithinTheBounds(t *testing.T) {
	s := &Store{}
	for _, c := range []struct {
		name    string
		source  string
		minutes int
		draw    func(int64) int64
		want    time.Duration
	}{
		{"none", "x", 30, offsetOf(0, 1), 30 * time.Minute},
		{"earliest", "x", 30, offsetOf(-1, 1), 27 * time.Minute},
		{"latest", "youtube", 60, offsetOf(1, 1), 66 * time.Minute},
		{"whole seconds", "x", 30, offsetOf(1, 3), 30*time.Minute + 60*time.Second},
		{"out of range draw", "x", 30, offsetOf(5, 1), 33 * time.Minute},
		// Below the 15-minute bound or above 7 days the offset is reflected.
		{"reflected at the lower bound", "x", 15, offsetOf(-1, 2), 15*time.Minute + 45*time.Second},
		{"partly reflected", "x", 16, offsetOf(-1, 1), 16*time.Minute + 96*time.Second},
		{"within the lower bound", "x", 16, offsetOf(-1, 2), 16*time.Minute - 48*time.Second},
		{"reflected at the upper bound", "youtube", SourceScheduleMaxMinutes, offsetOf(1, 1), SourceScheduleMaxMinutes * time.Minute * 9 / 10},
		// Bilibili never runs earlier than its interval.
		{"bilibili earliest", "bilibili", 180, offsetOf(-1, 1), 198 * time.Minute},
		{"bilibili later", "bilibili", 180, offsetOf(1, 2), 189 * time.Minute},
		{"bilibili at 7 days", "bilibili", SourceScheduleMaxMinutes, offsetOf(1, 1), SourceScheduleMaxMinutes * time.Minute},
	} {
		s.SetScheduleOffset(c.draw)
		if got := s.plannedInterval(c.source, c.minutes); got != c.want {
			t.Errorf("%s: %s %d min => %v, want %v", c.name, c.source, c.minutes, got, c.want)
		}
	}

	// The random draw stays within ±10% and the bounds, varies, and never
	// moves Bilibili earlier.
	s.SetScheduleOffset(nil)
	for _, c := range []struct {
		source   string
		minutes  int
		earliest time.Duration
		latest   time.Duration
	}{
		{"x", 30, 27 * time.Minute, 33 * time.Minute},
		{"x", 15, 15 * time.Minute, 15*time.Minute + 90*time.Second},
		{"youtube", SourceScheduleMaxMinutes, SourceScheduleMaxMinutes * time.Minute * 9 / 10, SourceScheduleMaxMinutes * time.Minute},
		{"bilibili", 180, 180 * time.Minute, 198 * time.Minute},
	} {
		seen := map[time.Duration]bool{}
		for range 500 {
			got := s.plannedInterval(c.source, c.minutes)
			if got < c.earliest || got > c.latest || got%time.Second != 0 {
				t.Fatalf("%s %d min => %v, outside %v–%v", c.source, c.minutes, got, c.earliest, c.latest)
			}
			seen[got] = true
		}
		if len(seen) < 10 {
			t.Fatalf("%s %d min drew only %d plans", c.source, c.minutes, len(seen))
		}
	}
}

func TestBilibiliPlansNeverGoBelowTheFloor(t *testing.T) {
	s := openScheduleStore(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 2, 3, 0, 0, 0, time.UTC)
	// The earliest draw is reflected: Bilibili's adapter skips a list read
	// sooner than BILIBILI_MIN_INTERVAL_MINUTES after the last one, so a plan
	// at or after the floor is never skipped for its interval.
	s.SetScheduleOffset(offsetOf(-1, 1))
	const floor = 240
	if err := s.PrepareSourceSchedules(ctx, floor, now); err != nil {
		t.Fatal(err)
	}
	bilibili := scheduleOf(t, s, "bilibili", floor)
	bilibili.Enabled = true
	saved, err := s.SaveSourceSchedule(ctx, bilibili, floor, now)
	if err != nil || saved.IntervalMinutes != floor || *saved.NextRunAt != "2026-03-02T07:24:00.000Z" {
		t.Fatalf("enable=%+v %v", saved, err)
	}
	claim := now.Add(265 * time.Minute)
	run, err := s.CreateWorkflowRun(ctx, WorkflowDefinition{ID: SourceScheduleWorkflow("bilibili"), Steps: []string{"bilibili"}}, "scheduler", "", map[string]bool{"bilibili": true}, claim)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimWorkflowRun(ctx, run.ID, "test", claim, time.Minute); err != nil {
		t.Fatal(err)
	}
	next, err := time.Parse(time.RFC3339, *scheduleOf(t, s, "bilibili", floor).NextRunAt)
	if err != nil || next.Sub(claim) != 264*time.Minute {
		t.Fatalf("planned %v after the claim, %v", next.Sub(claim), err)
	}
}

func TestClaimingATimedRunJittersItsNextRun(t *testing.T) {
	s := openScheduleStore(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 2, 3, 0, 0, 0, time.UTC)
	s.SetScheduleOffset(offsetOf(1, 2))
	x := scheduleOf(t, s, "x", 180)
	x.Enabled = true
	saved, err := s.SaveSourceSchedule(ctx, x, 180, now)
	if err != nil || *saved.NextRunAt != "2026-03-02T03:31:30.000Z" {
		t.Fatalf("enable=%+v %v", saved, err)
	}
	s.SetScheduleOffset(offsetOf(-1, 1))
	run, err := s.CreateWorkflowRun(ctx, WorkflowDefinition{ID: SourceScheduleWorkflow("x"), Steps: []string{"x"}}, "scheduler", "", map[string]bool{"x": true}, now.Add(32*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimWorkflowRun(ctx, run.ID, "test", now.Add(32*time.Minute), time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := scheduleOf(t, s, "x", 180); *got.NextRunAt != "2026-03-02T03:59:00.000Z" {
		t.Fatalf("claimed=%v", *got.NextRunAt)
	}
}

func scheduleOf(t *testing.T, s *Store, source string, floor int) SourceSchedule {
	t.Helper()
	schedule, err := s.SourceScheduleFor(context.Background(), source, floor)
	if err != nil {
		t.Fatal(err)
	}
	return schedule
}

func TestPrepareSourceSchedulesLeavesRetiredWorkflowsAlone(t *testing.T) {
	s := openScheduleStore(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 2, 3, 0, 0, 0, time.UTC)
	// A timed workflow that reads sources, as an earlier release kept it.
	// Saved workflows are retired without a migration (ADR-0003).
	const row = `wf-frequent|Frequent|["x","youtube","media"]|1|20|2026-03-02T03:10:00.000Z`
	if _, err := s.db.Exec(`INSERT INTO workflows(id,name,steps_json,source_sync_json,schedule_enabled,interval_minutes,next_run_at,created_at,updated_at) VALUES('wf-frequent','Frequent','["x","youtube","media"]','{"x":{"mode":"count","count":30}}',1,20,'2026-03-02T03:10:00.000Z','2026-03-01T00:00:00.000Z','2026-03-01T00:00:00.000Z')`); err != nil {
		t.Fatal(err)
	}
	if err := s.PrepareSourceSchedules(ctx, 240, now); err != nil {
		t.Fatal(err)
	}
	for _, source := range ScheduleSources {
		if schedule := scheduleOf(t, s, source, 240); schedule.Enabled || schedule.NextRunAt != nil {
			t.Fatalf("%s took over a workflow timer: %+v", source, schedule)
		}
	}
	var got string
	if err := s.db.QueryRow(`SELECT id||'|'||name||'|'||steps_json||'|'||schedule_enabled||'|'||interval_minutes||'|'||next_run_at FROM workflows`).Scan(&got); err != nil || got != row {
		t.Fatalf("workflow row=%q %v", got, err)
	}
	var markers int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM app_settings WHERE key='source_schedules_migrated'`).Scan(&markers); err != nil || markers != 0 {
		t.Fatalf("migration marker=%d %v", markers, err)
	}
	// Every start raises a Bilibili interval below a raised floor.
	bilibili := scheduleOf(t, s, "bilibili", 240)
	if _, err := s.SaveSourceSchedule(ctx, bilibili, 240, now); err != nil {
		t.Fatal(err)
	}
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

func TestRunNowLeavesTheSchedulePlanAlone(t *testing.T) {
	s := openScheduleStore(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 2, 3, 0, 0, 0, time.UTC)
	x := scheduleOf(t, s, "x", 180)
	x.Enabled = true
	if _, err := s.SaveSourceSchedule(ctx, x, 180, now); err != nil {
		t.Fatal(err)
	}
	// The operator's Run now is the same chain, triggered by the operator.
	run, err := s.CreateWorkflowRun(ctx, WorkflowDefinition{ID: SourceScheduleWorkflow("x"), Steps: []string{"x"}}, "operator", "", map[string]bool{"x": true}, now.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimWorkflowRun(ctx, run.ID, "test", now.Add(6*time.Minute), time.Minute); err != nil {
		t.Fatal(err)
	}
	got := scheduleOf(t, s, "x", 180)
	if got.NextRunAt == nil || *got.NextRunAt != "2026-03-02T03:30:00.000Z" || got.LastRun == nil || got.LastRun.ID != run.ID {
		t.Fatalf("run now moved the plan: %+v %+v", got, got.LastRun)
	}
}
