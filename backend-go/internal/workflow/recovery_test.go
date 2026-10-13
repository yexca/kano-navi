package workflow

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

type failThenRecover struct{ x, youtube atomic.Int32 }

type pausedTerminalRead struct {
	*sqlite.Store
	published, release chan struct{}
	held               atomic.Bool
}

func (s *pausedTerminalRead) GetWorkflowRun(ctx context.Context, id string) (*sqlite.WorkflowRun, error) {
	r, err := s.Store.GetWorkflowRun(ctx, id)
	if err == nil && r.Status == "failed" && s.held.CompareAndSwap(false, true) {
		close(s.published)
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return r, err
}

func (s *failThenRecover) Execute(_ context.Context, in StepInput) (AttemptResult, error) {
	if in.StepID == "x" {
		s.x.Add(1)
	} else if s.youtube.Add(1) == 1 {
		return AttemptResult{}, NewStepError("dependency_unavailable", ErrDependency)
	}
	return AttemptResult{Status: "succeeded"}, nil
}
func TestRetryUsesFrozenWindowAndSkipsCompletedStep(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := sqlite.OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	def, err := s.SaveWorkflow(ctx, sqlite.WorkflowDefinition{Name: "Retry", Steps: []string{"x", "youtube"}, SourceLookbackDays: map[string]int{"x": 3, "youtube": 5}, IntervalMinutes: 60}, true)
	if err != nil {
		t.Fatal(err)
	}
	steps := &failThenRecover{}
	paused := &pausedTerminalRead{Store: s, published: make(chan struct{}), release: make(chan struct{})}
	q := &Queue{Store: s, Executor: &Executor{Store: paused, Steps: steps, MaxAttempts: 1}}
	defer q.Close()
	defer func() {
		select {
		case <-paused.release:
		default:
			close(paused.release)
		}
	}()
	run, err := q.Submit(ctx, def.ID, TriggerOperator, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	waitRun(t, s, run.ID, "failed")
	select {
	case <-paused.published:
	case <-time.After(5 * time.Second):
		t.Fatal("terminal read was not held")
	}
	def.SourceLookbackDays["x"] = 7
	_, err = s.SaveWorkflow(ctx, *def, false)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := q.Retry(ctx, run.ID)
	if err != nil || retry.ID != run.ID {
		t.Fatalf("retry=%#v err=%v", retry, err)
	}
	close(paused.release)
	finished := waitRun(t, s, run.ID, "succeeded")
	if steps.x.Load() != 1 || steps.youtube.Load() != 2 || finished.SourceLookbackDays["x"] != 3 || finished.ExecutionSource != "recovery" || len(finished.Steps[1].Attempts) != 2 {
		t.Fatalf("retry duplicated completed work or lost input: %#v", finished)
	}
}
func TestRestartRecoversExpiredRunWithOriginalAttempts(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := sqlite.OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_ = s.EnsureXRecentWorkflow(ctx)
	def, _ := s.GetWorkflow(ctx, "x-recent")
	def.SourceLookbackDays["x"] = 4
	run, err := s.CreateWorkflowRun(ctx, *def, "operator", "", map[string]bool{"x": true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	lease, err := s.ClaimWorkflowRun(ctx, run.ID, "old-worker", time.Now(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.StartWorkflowStep(ctx, lease, run.Steps[0].ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	testutil.Exec(t, p, `UPDATE workflow_leases SET expires_at='2026-01-01T00:00:00Z'`)
	s, err = sqlite.OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	q := &Queue{Store: s, Executor: &Executor{Store: s}}
	defer q.Close()
	if err = q.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	done := waitRun(t, s, run.ID, "succeeded")
	if done.RecoveryCount != 1 || done.SourceLookbackDays["x"] != 4 || len(done.Steps[0].Attempts) != 2 || done.Steps[0].Attempts[0].Status != "failed" {
		t.Fatalf("recovery=%#v", done)
	}
}
