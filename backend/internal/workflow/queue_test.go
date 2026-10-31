package workflow

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend/internal/testutil"
)

type blockingStep struct {
	entered chan StepInput
	release chan struct{}
	calls   atomic.Int32
}

type heartbeatStore struct {
	*sqlite.Store
	clock   atomic.Int64
	renewed chan struct{}
	count   atomic.Int32
}

func (s *heartbeatStore) RenewWorkflowLease(ctx context.Context, l sqlite.WorkflowLease, _ time.Time, ttl time.Duration) error {
	now := time.UnixMilli(s.clock.Add(ttl.Milliseconds() / 2))
	err := s.Store.RenewWorkflowLease(ctx, l, now, ttl)
	if err == nil && s.count.Add(1) == 3 {
		close(s.renewed)
	}
	return err
}

func (s *blockingStep) Execute(ctx context.Context, in StepInput) (AttemptResult, error) {
	s.calls.Add(1)
	s.entered <- in
	select {
	case <-s.release:
		return AttemptResult{Status: "succeeded"}, nil
	case <-ctx.Done():
		return AttemptResult{}, NewStepError("cancelled", ErrCancelled)
	}
}
func waitRun(t *testing.T, s *sqlite.Store, id string, statuses ...string) *Run {
	t.Helper()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		r, err := s.GetWorkflowRun(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		for _, status := range statuses {
			if r.Status == status {
				return r
			}
		}
		select {
		case <-deadline.C:
			t.Fatalf("run stuck in %s expected %v", r.Status, statuses)
		case <-tick.C:
		}
	}
}

// xRecent is a one-off run of the X step.
var xRecent = sqlite.WorkflowDefinition{ID: sqlite.AdhocFetchWorkflow, Steps: []string{"x"}}

func TestQueueReturnsBeforeWorkSurvivesRequestAndAllowsNextRefresh(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := sqlite.OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	step := &blockingStep{entered: make(chan StepInput, 2), release: make(chan struct{})}
	tracked := &heartbeatStore{Store: s, renewed: make(chan struct{})}
	tracked.clock.Store(time.Now().UnixMilli())
	q := &Queue{Store: s, Executor: &Executor{Store: tracked, Steps: step, Clock: func() time.Time { return time.UnixMilli(tracked.clock.Load()) }, LeaseTTL: 300 * time.Millisecond, SyntheticSteps: map[string]bool{"x": true}}}
	defer q.Close()
	ctx, cancel := context.WithCancel(context.Background())
	sync := sqlite.SourceSync{Mode: sqlite.SyncCount, Count: 20}
	run, err := q.SubmitDefinition(ctx, sqlite.WorkflowDefinition{ID: sqlite.AdhocFetchWorkflow, Steps: []string{"x"}, SourceSync: map[string]sqlite.SourceSync{"x": sync}}, TriggerOperator, map[string]int{"x": 3})
	if err != nil || run.Status != "queued" {
		t.Fatalf("run=%#v err=%v", run, err)
	}
	cancel()
	select {
	case in := <-step.entered:
		if in.LookbackDays != 3 || in.Sync != sync {
			t.Fatalf("window=%d sync=%+v", in.LookbackDays, in.Sync)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("work did not start")
	}
	if _, err = q.SubmitDefinition(context.Background(), xRecent, TriggerOperator, nil); !errors.Is(err, sqlite.ErrWorkflowBusy) {
		t.Fatalf("concurrent submission err=%v", err)
	}
	// Advance a controlled clock past the original lease, one renewal at a
	// time. Host scheduling speed does not decide whether the lease is valid.
	select {
	case <-tracked.renewed:
	case <-time.After(5 * time.Second):
		t.Fatal("heartbeat did not renew")
	}
	close(step.release)
	finished := waitRun(t, s, run.ID, "succeeded")
	if finished.SourceLookbackDays["x"] != 3 || len(finished.Steps[0].Attempts) != 1 {
		t.Fatalf("finished=%#v", finished)
	}
	next, err := q.SubmitDefinition(context.Background(), xRecent, TriggerOperator, map[string]int{"x": 7})
	if err != nil || next.ID == run.ID {
		t.Fatalf("refresh was reused %#v %v", next, err)
	}
	waitRun(t, s, next.ID, "succeeded")
}
func TestQueueCancelStopsInFlightWorkAndPersistsCancelled(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := sqlite.OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	step := &blockingStep{entered: make(chan StepInput, 1), release: make(chan struct{})}
	q := &Queue{Store: s, Executor: &Executor{Store: s, Steps: step}}
	defer q.Close()
	r, err := q.SubmitDefinition(context.Background(), xRecent, TriggerOperator, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-step.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("not started")
	}
	if err = q.Cancel(context.Background(), r.ID); err != nil {
		t.Fatal(err)
	}
	got := waitRun(t, s, r.ID, "cancelled")
	if got.Steps[0].Status != "cancelled" {
		t.Fatalf("step=%#v", got.Steps[0])
	}
}
