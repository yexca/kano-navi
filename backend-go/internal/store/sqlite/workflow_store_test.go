package sqlite

import (
	"context"
	"errors"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Store, string) {
	p := testutil.NewSnapshot(t)
	s, e := OpenWorkflowCore(p, true)
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	now := workflowTime(time.Now())
	testutil.Exec(t, p, `INSERT INTO workflows(id,name,steps_json,source_lookback_json,created_at,updated_at) VALUES ('fixture','Fixture','["x","schedule"]','{}',?,?)`, now, now)
	s, e = OpenWorkflowCore(p, true)
	if e != nil {
		t.Fatal(e)
	}
	return s, p
}
func TestWorkflowMigrationAndLeaseSingleFlight(t *testing.T) {
	s, _ := fixture(t)
	defer s.Close()
	ctx := context.Background()
	def, _ := s.GetWorkflow(ctx, "fixture")
	r, e := s.CreateWorkflowRun(ctx, *def, "operator", "key", map[string]bool{"x": true, "schedule": true}, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.CreateWorkflowRun(ctx, *def, "scheduler", "other", nil, time.Now())
	if !errors.Is(e, ErrWorkflowBusy) {
		t.Fatalf("busy=%v", e)
	}
	l, e := s.ClaimWorkflowRun(ctx, r.ID, "a", time.Now(), time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ClaimWorkflowRun(ctx, r.ID, "b", time.Now(), time.Minute); !errors.Is(e, ErrWorkflowBusy) {
		t.Fatalf("lease=%v", e)
	}
	n, e := s.StartWorkflowStep(ctx, l, r.Steps[0].ID, time.Now())
	if e != nil || n != 1 {
		t.Fatalf("step %v %v", n, e)
	}
	if e = s.FinishWorkflowStep(ctx, l, r.Steps[0].ID, n, WorkflowOutcome{Status: "succeeded"}, false, time.Now()); e != nil {
		t.Fatal(e)
	}
	if e = s.FinishWorkflowRun(ctx, l, "succeeded", "", time.Now()); e != nil {
		t.Fatal(e)
	}
	got, e := s.GetWorkflowRun(ctx, r.ID)
	if e != nil || got.Status != "succeeded" || len(got.Steps) != 2 {
		t.Fatalf("got=%#v e=%v", got, e)
	}
}
func TestWorkflowRecoveryMarksExpiredRunningState(t *testing.T) {
	s, p := fixture(t)
	ctx := context.Background()
	def, _ := s.GetWorkflow(ctx, "fixture")
	r, e := s.CreateWorkflowRun(ctx, *def, "operator", "", nil, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	testutil.Exec(t, p, `UPDATE workflow_runs SET status='running' WHERE id=?`, r.ID)
	testutil.Exec(t, p, `INSERT INTO workflow_leases(scope,run_id,owner_id,acquired_at,expires_at,generation) VALUES('global',?,?,?,?,1)`, r.ID, "dead", "2026-10-12T00:00:00.000Z", "2026-10-12T00:01:00.000Z")
	s, e = OpenWorkflowCore(p, true)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	n, e := s.RecoverWorkflowRuns(ctx, time.Date(2026, 10, 12, 0, 2, 0, 0, time.UTC))
	if e != nil || n != 1 {
		t.Fatalf("recovery=%d %v", n, e)
	}
	got, e := s.GetWorkflowRun(ctx, r.ID)
	if e != nil || got.Status != "queued" || got.RecoveryCount != 1 {
		t.Fatalf("got=%#v e=%v", got, e)
	}
}
