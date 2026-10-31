package sqlite

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"
)

var retentionNow = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

// insertRetentionRun stores a run with one step and attempt, and its
// sync_runs row, finished the given number of days before retentionNow
// (created then as well; a negative age leaves it unfinished).
func insertRetentionRun(t *testing.T, s *Store, id, workflow, status string, age int, syncStatus string) {
	t.Helper()
	at := workflowTime(retentionNow.AddDate(0, 0, -max(age, -age)))
	var finished any
	if age >= 0 {
		finished = at
	}
	db := s.db
	if _, err := db.Exec(`INSERT INTO workflow_runs(id,workflow_id,status,trigger_source,execution_source,created_at,started_at,finished_at) VALUES(?,?,?,'scheduler','scheduler',?,?,?)`, id, workflow, status, at, at, finished); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec(`INSERT INTO workflow_run_steps(run_id,step_id,step_order,status,capability) VALUES(?,'x',0,'succeeded','x_recent')`, id)
	if err != nil {
		t.Fatal(err)
	}
	step, _ := result.LastInsertId()
	if _, err = db.Exec(`INSERT INTO workflow_step_attempts(step_id,attempt_number,status,started_at,finished_at) VALUES(?,1,'succeeded',?,?)`, step, at, finished); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO sync_runs(source,started_at,finished_at,status,job_id) VALUES('workflow',?,?,?,?)`, at, finished, syncStatus, id); err != nil {
		t.Fatal(err)
	}
}

func storedRunIDs(t *testing.T, s *Store) []string {
	t.Helper()
	rows, err := s.db.Query(`SELECT id FROM workflow_runs ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func countRows(t *testing.T, s *Store, query string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPruneRunsAppliesWindowsByStatus(t *testing.T) {
	s, _ := fixture(t)
	defer s.Close()
	insertRetentionRun(t, s, "sched-old", "@source-x", "succeeded", 40, "success")
	insertRetentionRun(t, s, "sched-new", "@source-x", "succeeded", 10, "success")
	insertRetentionRun(t, s, "fetch-cancelled", "@fetch", "cancelled", 40, "failed")
	insertRetentionRun(t, s, "fetch-failed-60", "@fetch", "failed", 60, "failed")
	insertRetentionRun(t, s, "fetch-partial-100", "@fetch", "partial", 100, "partial")
	insertRetentionRun(t, s, "fetch-blocked-95", "@fetch", "blocked", 95, "failed")
	insertRetentionRun(t, s, "fetch-newest", "@fetch", "failed", 5, "failed")
	// The only probe run paces the next probe, however old.
	insertRetentionRun(t, s, "live-only", "@live", "succeeded", 200, "success")
	insertRetentionRun(t, s, "sync-running", "@sync", "running", -400, "running")
	insertRetentionRun(t, s, "sync-old", "@sync", "succeeded", 300, "success")
	insertRetentionRun(t, s, "sync-new", "@sync", "succeeded", 1, "success")

	deleted, err := s.PruneRuns(context.Background(), RunRetention{SucceededDays: 30, FailedDays: 90}, retentionNow)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 5 {
		t.Fatalf("deleted=%d", deleted)
	}
	want := []string{"fetch-failed-60", "fetch-newest", "live-only", "sched-new", "sync-new", "sync-running"}
	if got := storedRunIDs(t, s); !slices.Equal(got, want) {
		t.Fatalf("kept=%v", got)
	}
	if n := countRows(t, s, `SELECT COUNT(*) FROM workflow_run_steps WHERE run_id NOT IN(SELECT id FROM workflow_runs)`); n != 0 {
		t.Fatalf("orphan steps=%d", n)
	}
	if n := countRows(t, s, `SELECT COUNT(*) FROM workflow_step_attempts WHERE step_id NOT IN(SELECT id FROM workflow_run_steps)`); n != 0 {
		t.Fatalf("orphan attempts=%d", n)
	}
	if n := countRows(t, s, `SELECT COUNT(*) FROM sync_runs`); n != len(want) {
		t.Fatalf("sync_runs=%d", n)
	}
}

func TestPruneRunsKeepsNewestSuccessfulSyncAndUnlinkedRows(t *testing.T) {
	s, _ := fixture(t)
	defer s.Close()
	insertRetentionRun(t, s, "youtube-success", "@source-youtube", "succeeded", 50, "success")
	insertRetentionRun(t, s, "youtube-failed", "@source-youtube", "failed", 2, "failed")
	for _, row := range []struct {
		status string
		age    int
	}{{"success", 400}, {"failed", 120}, {"failed", 30}} {
		at := workflowTime(retentionNow.AddDate(0, 0, -row.age))
		if _, err := s.db.Exec(`INSERT INTO sync_runs(source,started_at,finished_at,status) VALUES('x',?,?,?)`, at, at, row.status); err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := s.PruneRuns(context.Background(), RunRetention{SucceededDays: 30, FailedDays: 90}, retentionNow)
	if err != nil {
		t.Fatal(err)
	}
	// The 50-day success is the board's newest sync; the old unlinked rows go.
	if deleted != 2 {
		t.Fatalf("deleted=%d", deleted)
	}
	if got := storedRunIDs(t, s); !slices.Equal(got, []string{"youtube-failed", "youtube-success"}) {
		t.Fatalf("kept=%v", got)
	}
	if n := countRows(t, s, `SELECT COUNT(*) FROM sync_runs WHERE job_id IS NULL`); n != 1 {
		t.Fatalf("unlinked=%d", n)
	}
}

func TestPruneRunsZeroDaysKeepsForever(t *testing.T) {
	s, _ := fixture(t)
	defer s.Close()
	insertRetentionRun(t, s, "old-success", "@sync", "succeeded", 500, "success")
	insertRetentionRun(t, s, "old-failed", "@sync", "failed", 400, "failed")
	insertRetentionRun(t, s, "newest", "@sync", "succeeded", 1, "success")
	deleted, err := s.PruneRuns(context.Background(), RunRetention{}, retentionNow)
	if err != nil || deleted != 0 {
		t.Fatalf("deleted=%d err=%v", deleted, err)
	}
	deleted, err = s.PruneRuns(context.Background(), RunRetention{FailedDays: 90}, retentionNow)
	if err != nil || deleted != 1 {
		t.Fatalf("deleted=%d err=%v", deleted, err)
	}
	if got := storedRunIDs(t, s); !slices.Equal(got, []string{"newest", "old-success"}) {
		t.Fatalf("kept=%v", got)
	}
}

func TestPruneRunsDeletesInBatches(t *testing.T) {
	s, _ := fixture(t)
	defer s.Close()
	for i := range runPruneBatch + 7 {
		insertRetentionRun(t, s, "old-"+strconv.Itoa(i), "@sync", "succeeded", 100+i, "success")
	}
	insertRetentionRun(t, s, "newest", "@sync", "succeeded", 1, "success")
	deleted, err := s.PruneRuns(context.Background(), RunRetention{SucceededDays: 30, FailedDays: 90}, retentionNow)
	if err != nil || deleted != runPruneBatch+7 {
		t.Fatalf("deleted=%d err=%v", deleted, err)
	}
}

func TestRunRetentionSettings(t *testing.T) {
	s, _ := fixture(t)
	defer s.Close()
	ctx := context.Background()
	got, err := s.RunRetention(ctx)
	if err != nil || got != (RunRetention{SucceededDays: 30, FailedDays: 90}) {
		t.Fatalf("defaults=%#v err=%v", got, err)
	}
	s.writable = true
	if err = s.SaveRunRetention(ctx, RunRetention{SucceededDays: 0, FailedDays: 180}); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.RunRetention(ctx); got != (RunRetention{SucceededDays: 0, FailedDays: 180}) {
		t.Fatalf("saved=%#v", got)
	}
	if err = s.SaveRunRetention(ctx, RunRetention{SucceededDays: RunRetentionMaxDays + 1}); !errors.Is(err, ErrWorkflowValidation) {
		t.Fatalf("over max err=%v", err)
	}
}
