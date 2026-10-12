package workflow

import (
	"context"
	"errors"
	"fmt"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"time"
)

type TriggerSource string

const (
	TriggerOperator  TriggerSource = "operator"
	TriggerScheduler TriggerSource = "scheduler"
	TriggerRecovery  TriggerSource = "recovery"
)

type ErrorCategory string

const (
	ErrPermanent   ErrorCategory = "permanent"
	ErrSchema      ErrorCategory = "schema_failure"
	ErrValidation  ErrorCategory = "validation_failure"
	ErrTimeout     ErrorCategory = "timeout"
	ErrRateLimited ErrorCategory = "rate_limited"
	ErrDependency  ErrorCategory = "dependency_unavailable"
	ErrCancelled   ErrorCategory = "cancelled"
	ErrBlocked     ErrorCategory = "blocked"
)

type StepError struct {
	Code      string
	Category  ErrorCategory
	Retryable bool
}

func (e *StepError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code
}
func NewStepError(code string, category ErrorCategory) *StepError {
	return &StepError{Code: code, Category: category, Retryable: category == ErrTimeout || category == ErrRateLimited || category == ErrDependency}
}
func classify(err error) (string, bool) {
	var se *StepError
	if errors.As(err, &se) {
		return se.Code, se.Retryable
	}
	return "internal", false
}

type AttemptResult struct {
	Status string
	Result map[string]any
}
type StepInput struct {
	RunID, WorkflowID, StepID string
	Attempt                   int
}
type StepExecutor interface {
	Execute(context.Context, StepInput) (AttemptResult, error)
}
type SyntheticStepExecutor struct{}

func (SyntheticStepExecutor) Execute(_ context.Context, in StepInput) (AttemptResult, error) {
	return AttemptResult{Status: "succeeded", Result: map[string]any{"capability": "synthetic", "step": in.StepID}}, nil
}

type Persistence interface {
	GetWorkflow(context.Context, string) (*sqlite.WorkflowDefinition, error)
	CreateWorkflowRun(context.Context, sqlite.WorkflowDefinition, string, string, map[string]bool, time.Time) (*sqlite.WorkflowRun, error)
	ClaimWorkflowRun(context.Context, string, string, time.Time, time.Duration) (sqlite.WorkflowLease, error)
	StartWorkflowStep(context.Context, sqlite.WorkflowLease, int64, time.Time) (int, error)
	FinishWorkflowStep(context.Context, sqlite.WorkflowLease, int64, int, sqlite.WorkflowOutcome, bool, time.Time) error
	FinishWorkflowRun(context.Context, sqlite.WorkflowLease, string, string, time.Time) error
	RequestWorkflowCancellation(context.Context, string, time.Time) error
	WorkflowCancellationRequested(context.Context, string) (bool, error)
}
type Run = sqlite.WorkflowRun
type Executor struct {
	Store          Persistence
	Steps          StepExecutor
	OwnerID        string
	LeaseTTL       time.Duration
	MaxAttempts    int
	Clock          func() time.Time
	SyntheticSteps map[string]bool
}

func (e *Executor) defaults() {
	if e.Steps == nil {
		e.Steps = SyntheticStepExecutor{}
	}
	if e.OwnerID == "" {
		e.OwnerID = "go-workflow"
	}
	if e.LeaseTTL <= 0 {
		e.LeaseTTL = 2 * time.Minute
	}
	if e.MaxAttempts < 1 {
		e.MaxAttempts = 2
	}
	if e.Clock == nil {
		e.Clock = time.Now
	}
}
func (e *Executor) Run(ctx context.Context, workflowID string, trigger TriggerSource, key string) (*Run, error) {
	e.defaults()
	if e.Store == nil {
		return nil, errors.New("workflow store is required")
	}
	def, err := e.Store.GetWorkflow(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	if e.SyntheticSteps == nil {
		e.SyntheticSteps = make(map[string]bool, len(def.Steps))
		for _, step := range def.Steps {
			e.SyntheticSteps[step] = true
		}
	}
	now := e.Clock().UTC()
	run, err := e.Store.CreateWorkflowRun(ctx, *def, string(trigger), key, e.SyntheticSteps, now)
	if err != nil {
		return nil, err
	}
	if run.Status != "queued" {
		return run, nil
	}
	lease, err := e.Store.ClaimWorkflowRun(ctx, run.ID, e.OwnerID, now, e.LeaseTTL)
	if err != nil {
		return nil, err
	}
	finished := false
	defer func() {
		if !finished {
			_ = e.Store.FinishWorkflowRun(context.Background(), lease, "failed", "internal", e.Clock().UTC())
		}
	}()
	partial := false
	for _, step := range run.Steps {
		success := false
		for attempt := 1; attempt <= e.MaxAttempts; attempt++ {
			cancelled, cancelErr := e.Store.WorkflowCancellationRequested(ctx, run.ID)
			if cancelErr != nil {
				return nil, cancelErr
			}
			if cancelled {
				_ = e.Store.FinishWorkflowRun(ctx, lease, "cancelled", "cancelled", e.Clock().UTC())
				finished = true
				return run, nil
			}
			num, err := e.Store.StartWorkflowStep(ctx, lease, step.ID, e.Clock().UTC())
			if err != nil {
				return nil, err
			}
			out, execErr := e.Steps.Execute(ctx, StepInput{run.ID, workflowID, step.StepID, num})
			if execErr != nil {
				code, retry := classify(execErr)
				if err = e.Store.FinishWorkflowStep(ctx, lease, step.ID, num, sqlite.WorkflowOutcome{Status: "failed", Code: code, Retryable: retry}, retry && attempt < e.MaxAttempts, e.Clock().UTC()); err != nil {
					return nil, err
				}
				if !retry || attempt == e.MaxAttempts {
					_ = e.Store.FinishWorkflowRun(ctx, lease, "failed", code, e.Clock().UTC())
					finished = true
					return run, nil
				}
				continue
			}
			if out.Status == "" {
				out.Status = "succeeded"
			}
			if err = e.Store.FinishWorkflowStep(ctx, lease, step.ID, num, sqlite.WorkflowOutcome{Status: out.Status}, false, e.Clock().UTC()); err != nil {
				return nil, err
			}
			if out.Status == "failed" || out.Status == "blocked" {
				_ = e.Store.FinishWorkflowRun(ctx, lease, out.Status, "blocked", e.Clock().UTC())
				finished = true
				return run, nil
			}
			if out.Status == "partial" {
				partial = true
			}
			success = true
			break
		}
		if !success {
			_ = e.Store.FinishWorkflowRun(ctx, lease, "failed", "internal", e.Clock().UTC())
			finished = true
			return run, nil
		}
	}
	finalStatus := "succeeded"
	if partial {
		finalStatus = "partial"
	}
	if err = e.Store.FinishWorkflowRun(ctx, lease, finalStatus, "", e.Clock().UTC()); err != nil {
		return nil, err
	}
	finished = true
	run.Status = finalStatus
	return run, nil
}
func (e *Executor) Cancel(ctx context.Context, id string) error {
	e.defaults()
	return e.Store.RequestWorkflowCancellation(ctx, id, e.Clock().UTC())
}
func (e *Executor) RetryFailedSteps(ctx context.Context, workflowID string) (*Run, error) {
	e.defaults()
	// A recovery run receives a fresh idempotency key. The store keeps the
	// previous attempt history, while the selected synthetic steps are replayed
	// through the same lease and retry policy.
	return e.Run(ctx, workflowID, TriggerRecovery, fmt.Sprintf("recovery:%s:%d", workflowID, e.Clock().UnixNano()))
}
