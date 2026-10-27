package workflow

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/source/history"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
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
	// LookbackDays are the days of an operator recent window.
	LookbackDays int
	// Sync is the source schedule's setting frozen on the run; the zero value
	// is the step's incremental default.
	Sync sqlite.SourceSync
	// Window is the operator-selected source window frozen on the run, or nil
	// for the incremental synchronization.
	Window *history.Window
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
	GetWorkflowRun(context.Context, string) (*sqlite.WorkflowRun, error)
	RenewWorkflowLease(context.Context, sqlite.WorkflowLease, time.Time, time.Duration) error
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
	// TolerateFailure lists steps whose final failure does not abort the run:
	// one unavailable source must not stop the other source or post-processing.
	TolerateFailure map[string]bool
	initMu          sync.Mutex
}

// SourceSyncReader is an optional Persistence capability that supplies the
// source schedules' sync settings to manual runs of saved workflows.
type SourceSyncReader interface {
	SourceScheduleSync(context.Context) (map[string]sqlite.SourceSync, error)
}

// ResultRecorder is an optional Persistence capability that keeps each step's
// counters with the run history.
type ResultRecorder interface {
	RecordStepResult(context.Context, string, string, map[string]any) error
}

// stepResultKey matches the keys the admin console already reads.
func stepResultKey(step string) string {
	if step == "schedule" {
		return "schedules"
	}
	return step
}

func (e *Executor) record(runID, step string, result map[string]any) {
	recorder, ok := e.Store.(ResultRecorder)
	if !ok || result == nil {
		return
	}
	_ = recorder.RecordStepResult(context.Background(), runID, stepResultKey(step), result)
}

func (e *Executor) defaults() {
	e.initMu.Lock()
	defer e.initMu.Unlock()
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
	run, err := e.Prepare(ctx, workflowID, trigger, key, nil)
	if err != nil || run.Status != "queued" {
		return run, err
	}
	return e.ExecuteRun(ctx, run)
}

// Prepare reserves the durable single-flight slot before returning to HTTP.
// Source parameters are frozen on the run so recovery never changes its window.
func (e *Executor) Prepare(ctx context.Context, workflowID string, trigger TriggerSource, key string, lookback map[string]int) (*Run, error) {
	e.defaults()
	if e.Store == nil {
		return nil, errors.New("workflow store is required")
	}
	def, err := e.Store.GetWorkflow(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	// A saved workflow reads each source as that source's schedule does.
	if reader, ok := e.Store.(SourceSyncReader); ok {
		settings, err := reader.SourceScheduleSync(ctx)
		if err != nil {
			return nil, err
		}
		def.SourceSync = map[string]sqlite.SourceSync{}
		for _, step := range def.Steps {
			if setting, configured := settings[step]; configured {
				def.SourceSync[step] = setting
			}
		}
	}
	return e.PrepareDefinition(ctx, *def, trigger, key, lookback)
}

// PrepareDefinition reserves a run for a definition that need not be saved,
// such as an operator's full sync, scan or single-source fetch.
func (e *Executor) PrepareDefinition(ctx context.Context, definition sqlite.WorkflowDefinition, trigger TriggerSource, key string, lookback map[string]int) (*Run, error) {
	e.defaults()
	if e.Store == nil {
		return nil, errors.New("workflow store is required")
	}
	def := &definition
	if _, declared := e.SyntheticSteps["reconcile"]; declared {
		def.Steps = withReconcile(def.Steps)
	}
	if def.SourceLookbackDays == nil {
		def.SourceLookbackDays = map[string]int{}
	}
	synthetic := e.SyntheticSteps
	if synthetic == nil {
		synthetic = make(map[string]bool, len(def.Steps))
		for _, step := range def.Steps {
			synthetic[step] = true
		}
	}
	for k, v := range lookback {
		if (k != "x" && k != "youtube") || v < 1 || v > 365 {
			return nil, sqlite.ErrWorkflowValidation
		}
		def.SourceLookbackDays[k] = v
	}
	now := e.Clock().UTC()
	return e.Store.CreateWorkflowRun(ctx, *def, string(trigger), key, synthetic, now)
}

// withReconcile appends duplicate reconciliation to a run whose steps write
// events or videos, so every timed, manual, push or MCP run leaves merges
// current without editing saved workflows.
func withReconcile(steps []string) []string {
	writes := false
	for _, step := range steps {
		switch step {
		case "reconcile":
			return steps
		case "x", "youtube", "bilibili", "schedule":
			writes = true
		}
	}
	if !writes {
		return steps
	}
	return append(append([]string{}, steps...), "reconcile")
}

func (e *Executor) ExecuteRun(ctx context.Context, run *Run) (*Run, error) {
	e.defaults()
	if run.Status != "queued" {
		return run, nil
	}
	lease, err := e.Store.ClaimWorkflowRun(ctx, run.ID, e.OwnerID, e.Clock().UTC(), e.LeaseTTL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(sqlite.WithSourceLease(ctx, lease))
	defer cancel()
	// Network batches can exceed a lease TTL. Losing ownership cancels outbound
	// work; source transactions independently check the lease before committing.
	stopHeartbeat := make(chan struct{})
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		interval := e.LeaseTTL / 3
		if interval < time.Millisecond {
			interval = time.Millisecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stopHeartbeat:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if e.Store.RenewWorkflowLease(ctx, lease, e.Clock().UTC(), e.LeaseTTL) != nil {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { close(stopHeartbeat); <-heartbeatDone }()
	finished := false
	defer func() {
		if !finished {
			status, code := "failed", "internal"
			if ctx.Err() != nil {
				status, code = "cancelled", "cancelled"
			}
			_ = e.Store.FinishWorkflowRun(context.Background(), lease, status, code, e.Clock().UTC())
		}
	}()
	partial := false
	completed, tolerated := 0, 0
	toleratedCode := ""
	for _, step := range run.Steps {
		if step.Status == "succeeded" {
			completed++
			continue
		}
		if step.Status == "partial" {
			partial = true
			completed++
			continue
		}
		success := false
		for attempt := 1; attempt <= e.MaxAttempts; attempt++ {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			cancelled, cancelErr := e.Store.WorkflowCancellationRequested(ctx, run.ID)
			if cancelErr != nil {
				return nil, cancelErr
			}
			if cancelled {
				if err = e.Store.FinishWorkflowRun(context.Background(), lease, "cancelled", "cancelled", e.Clock().UTC()); err != nil {
					return nil, err
				}
				finished = true
				return e.Store.GetWorkflowRun(context.Background(), run.ID)
			}
			num, err := e.Store.StartWorkflowStep(ctx, lease, step.ID, e.Clock().UTC())
			if err != nil {
				return nil, err
			}
			var out AttemptResult
			var execErr error
			if step.Capability == "capability_not_migrated" {
				execErr = NewStepError("capability_not_migrated", ErrBlocked)
			} else {
				input := StepInput{RunID: run.ID, WorkflowID: run.WorkflowID, StepID: step.StepID, Attempt: num, LookbackDays: run.SourceLookbackDays[step.StepID], Sync: run.SourceSync[step.StepID]}
				if window, selected := run.FetchWindows[step.StepID]; selected {
					input.Window = &window
				}
				out, execErr = e.Steps.Execute(ctx, input)
			}
			if ctx.Err() != nil {
				execErr = NewStepError("cancelled", ErrCancelled)
			}
			if execErr != nil {
				code, retry := classify(execErr)
				finishCtx := ctx
				if ctx.Err() != nil {
					code = "cancelled"
					retry = false
					finishCtx = context.Background()
				}
				outStatus := "failed"
				if code == "cancelled" {
					outStatus = "cancelled"
				}
				if code == "capability_not_migrated" {
					outStatus = "blocked"
				}
				if err = e.Store.FinishWorkflowStep(finishCtx, lease, step.ID, num, sqlite.WorkflowOutcome{Status: outStatus, Code: code, Retryable: retry}, retry && attempt < e.MaxAttempts, e.Clock().UTC()); err != nil {
					return nil, err
				}
				if (!retry || attempt == e.MaxAttempts) && outStatus == "failed" && e.TolerateFailure[step.StepID] {
					e.record(run.ID, step.StepID, map[string]any{"error": code})
					tolerated++
					toleratedCode = code
					success = true
					break
				}
				if !retry || attempt == e.MaxAttempts {
					if err = e.Store.FinishWorkflowRun(finishCtx, lease, outStatus, code, e.Clock().UTC()); err != nil {
						return nil, err
					}
					finished = true
					return e.Store.GetWorkflowRun(context.Background(), run.ID)
				}
				continue
			}
			if out.Status == "" {
				out.Status = "succeeded"
			}
			if err = e.Store.FinishWorkflowStep(ctx, lease, step.ID, num, sqlite.WorkflowOutcome{Status: out.Status}, false, e.Clock().UTC()); err != nil {
				return nil, err
			}
			e.record(run.ID, step.StepID, out.Result)
			if out.Status == "failed" && e.TolerateFailure[step.StepID] {
				tolerated++
				toleratedCode = "permanent"
				success = true
				break
			}
			if out.Status == "failed" || out.Status == "blocked" {
				// A step that ran and reported failure is not a missing capability.
				code := "blocked"
				if out.Status == "failed" {
					code = "permanent"
				}
				if err = e.Store.FinishWorkflowRun(ctx, lease, out.Status, code, e.Clock().UTC()); err != nil {
					return nil, err
				}
				finished = true
				return e.Store.GetWorkflowRun(context.Background(), run.ID)
			}
			if out.Status == "partial" {
				partial = true
			}
			completed++
			success = true
			break
		}
		if !success {
			_ = e.Store.FinishWorkflowRun(ctx, lease, "failed", "internal", e.Clock().UTC())
			finished = true
			return run, nil
		}
	}
	finalStatus, finalCode := "succeeded", ""
	if partial || tolerated > 0 {
		finalStatus = "partial"
	}
	if tolerated > 0 && completed == 0 {
		finalStatus, finalCode = "failed", toleratedCode
	}
	if err = e.Store.FinishWorkflowRun(ctx, lease, finalStatus, finalCode, e.Clock().UTC()); err != nil {
		return nil, err
	}
	finished = true
	return e.Store.GetWorkflowRun(context.Background(), run.ID)
}
func (e *Executor) Cancel(ctx context.Context, id string) error {
	e.defaults()
	return e.Store.RequestWorkflowCancellation(ctx, id, e.Clock().UTC())
}
