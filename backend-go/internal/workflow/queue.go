package workflow

import (
	"context"
	"sync"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// Queue gives HTTP submissions a process lifetime, while SQLite owns the
// single-flight reservation shared with timers and recovery.
type Queue struct {
	Executor       *Executor
	Store          *sqlite.Store
	mu             sync.Mutex
	closed         bool
	wg             sync.WaitGroup
	cancels        map[string]context.CancelFunc
	recoveryCancel context.CancelFunc
}

func (q *Queue) Submit(ctx context.Context, id string, trigger TriggerSource, key string, days map[string]int) (*Run, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || q.Executor == nil {
		return nil, sqlite.ErrWorkflowUnavailable
	}
	run, err := q.Executor.Prepare(ctx, id, trigger, key, days)
	if err != nil {
		return nil, err
	}
	if run.Status == "queued" {
		q.dispatch(run)
	}
	return run, nil
}

// Caller holds mu. A bounded background context deliberately outlives the
// initiating request. Cancel/Close stop active network requests as well.
func (q *Queue) dispatch(run *Run) {
	if q.cancels == nil {
		q.cancels = map[string]context.CancelFunc{}
	}
	if _, exists := q.cancels[run.ID]; exists {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	q.cancels[run.ID] = cancel
	q.wg.Add(1)
	go func() {
		defer q.wg.Done()
		defer cancel()
		_, _ = q.Executor.ExecuteRun(ctx, run)
		q.mu.Lock()
		delete(q.cancels, run.ID)
		// A retry may reserve this same durable ID after its terminal status
		// commits but before this worker releases its process slot. Hand it off
		// here instead of leaving the accepted retry queued indefinitely.
		if !q.closed {
			if next, err := q.Store.GetWorkflowRun(context.Background(), run.ID); err == nil && next.Status == "queued" {
				q.dispatch(next)
			}
		}
		q.mu.Unlock()
	}()
}
func (q *Queue) Cancel(ctx context.Context, id string) error {
	if err := q.Executor.Cancel(ctx, id); err != nil {
		return err
	}
	q.mu.Lock()
	if cancel := q.cancels[id]; cancel != nil {
		cancel()
	}
	q.mu.Unlock()
	return nil
}
func (q *Queue) Recover(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return sqlite.ErrWorkflowUnavailable
	}
	if _, err := q.Store.RecoverWorkflowRuns(ctx, time.Now()); err != nil {
		return err
	}
	page, err := q.Store.ListWorkflowRuns(ctx, 1, 1, "queued", "")
	if err != nil {
		return err
	}
	if len(page.Runs) > 0 {
		run, err := q.Store.GetWorkflowRun(ctx, page.Runs[0].ID)
		if err != nil {
			return err
		}
		q.dispatch(run)
	}
	return nil
}
func (q *Queue) StartRecovery() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || q.recoveryCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	q.recoveryCancel = cancel
	q.wg.Add(1)
	go func() {
		defer q.wg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = q.Recover(ctx)
			}
		}
	}()
}
func (q *Queue) Retry(ctx context.Context, id string) (*Run, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return nil, sqlite.ErrWorkflowUnavailable
	}
	existing, lookupErr := q.Store.GetWorkflowRun(ctx, id)
	if lookupErr != nil {
		return nil, lookupErr
	}
	if _, lookupErr = q.Store.GetWorkflow(ctx, existing.WorkflowID); lookupErr != nil {
		return nil, lookupErr
	}
	if err := q.Store.RetryWorkflowRun(ctx, id, time.Now()); err != nil {
		return nil, err
	}
	run, err := q.Store.GetWorkflowRun(ctx, id)
	if err != nil {
		return nil, err
	}
	q.dispatch(run)
	return run, nil
}
func (q *Queue) Close() {
	q.mu.Lock()
	q.closed = true
	if q.recoveryCancel != nil {
		q.recoveryCancel()
	}
	for _, cancel := range q.cancels {
		cancel()
	}
	q.mu.Unlock()
	q.wg.Wait()
}

type RecentTrigger struct {
	Queue              *Queue
	WorkflowID, Source string
}

func (t *RecentTrigger) Trigger(ctx context.Context, days int) (*sqlite.WorkflowRun, error) {
	if days < 1 || days > 365 {
		return nil, NewStepError("invalid_recent_days", ErrValidation)
	}
	return t.Queue.Submit(ctx, t.WorkflowID, TriggerOperator, "", map[string]int{t.Source: days})
}
