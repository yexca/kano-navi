package jobs

import (
	"context"
	"errors"

	"github.com/yexca/kano-navi/backend-go/internal/source/history"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/workflow"
)

// StepOrder is the catalog execution order: sources first, then the
// post-processing that consumes what the sources stored, and finally the
// duplicate-event reconciliation over everything written before it.
var StepOrder = []string{"x", "youtube", "media", "schedule", "reconcile"}

// NormalizeSteps returns the known step IDs in catalog order.
func NormalizeSteps(values []string) []string {
	selected := map[string]bool{}
	for _, value := range values {
		selected[value] = true
	}
	steps := []string{}
	for _, step := range StepOrder {
		if selected[step] {
			steps = append(steps, step)
		}
	}
	return steps
}

// QueueManager presents durable workflow runs as operator jobs.
type QueueManager struct {
	Store *sqlite.Store
	Queue *workflow.Queue
}

func reason(value string) *string { return &value }

func (m *QueueManager) Start(ctx context.Context, kind, triggeredBy string, options Options) (StartResult, error) {
	if m == nil || m.Store == nil || m.Queue == nil {
		return StartResult{}, sqlite.ErrWorkflowUnavailable
	}
	if kind != KindScan && kind != KindWorkflow && kind != KindLive {
		kind = KindSync
	}
	if triggeredBy == "" {
		triggeredBy = "admin"
	}
	if len([]rune(triggeredBy)) > 64 {
		triggeredBy = string([]rune(triggeredBy)[:64])
	}
	trigger := workflow.TriggerOperator
	if triggeredBy == "scheduler" || triggeredBy == TriggeredByLiveProbe {
		trigger = workflow.TriggerScheduler
	}
	source := "workflow"
	var run *sqlite.WorkflowRun
	var err error
	switch {
	case kind == KindWorkflow && options.WorkflowID != "":
		run, err = m.Queue.Submit(ctx, options.WorkflowID, trigger, "", nil)
		if errors.Is(err, sqlite.ErrWorkflowNotFound) {
			return StartResult{Reason: reason(ReasonWorkflowNotFound)}, nil
		}
	case kind == KindWorkflow:
		steps := NormalizeSteps(options.Steps)
		if len(steps) == 0 {
			return StartResult{Reason: reason(ReasonNoSteps)}, nil
		}
		windows := map[string]history.Window{}
		for source, window := range options.FetchWindows {
			normalized, windowErr := window.Normalize()
			// Only the X timeline lists a source's newest posts by count.
			if windowErr != nil || (source != "x" && source != "youtube") || (normalized.Mode == history.ModeCount && source != "x") {
				return StartResult{}, sqlite.ErrWorkflowValidation
			}
			windows[source] = normalized
		}
		run, err = m.Queue.SubmitDefinition(ctx, sqlite.WorkflowDefinition{ID: sqlite.AdhocFetchWorkflow, Steps: steps, FetchWindows: windows}, trigger, recentDays(windows))
	case kind == KindLive:
		source = "youtube-live"
		run, err = m.Queue.SubmitDefinition(ctx, sqlite.WorkflowDefinition{ID: sqlite.AdhocLiveWorkflow, Steps: []string{StepLive}}, trigger, nil)
	case kind == KindScan:
		source = "schedule"
		run, err = m.Queue.SubmitDefinition(ctx, sqlite.WorkflowDefinition{ID: sqlite.AdhocScanWorkflow, Steps: []string{"schedule"}}, trigger, nil)
	default:
		source = "manual"
		run, err = m.Queue.SubmitDefinition(ctx, sqlite.WorkflowDefinition{ID: sqlite.AdhocSyncWorkflow, Steps: append([]string{}, StepOrder...)}, trigger, nil)
	}
	if errors.Is(err, sqlite.ErrWorkflowBusy) {
		active, _ := m.Active(ctx)
		return StartResult{Reason: reason(ReasonAlreadyRunning), Job: active}, nil
	}
	if err != nil {
		return StartResult{}, err
	}
	_ = m.Store.LabelSyncRun(ctx, run.ID, source, triggeredBy)
	job, err := m.Get(ctx, run.ID)
	if err != nil {
		return StartResult{}, err
	}
	return StartResult{Accepted: true, Job: job}, nil
}

// recentDays gives the incremental synchronization the lookback of a recent
// window, which it uses when the source API credential is absent.
func recentDays(windows map[string]history.Window) map[string]int {
	days := map[string]int{}
	for source, window := range windows {
		if window.Mode == history.ModeRecent {
			days[source] = window.Days
		}
	}
	return days
}

func (m *QueueManager) Get(ctx context.Context, id string) (*Job, error) {
	if m == nil || m.Store == nil {
		return nil, sqlite.ErrWorkflowUnavailable
	}
	run, err := m.Store.GetWorkflowRun(ctx, id)
	if errors.Is(err, sqlite.ErrWorkflowNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names, err := m.Store.WorkflowNames(ctx)
	if err != nil {
		return nil, err
	}
	return m.job(ctx, run, names)
}

func (m *QueueManager) List(ctx context.Context, limit int) ([]Job, error) {
	if m == nil || m.Store == nil {
		return nil, sqlite.ErrWorkflowUnavailable
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	page, err := m.Store.ListWorkflowRuns(ctx, 1, limit, "", "")
	if err != nil {
		return nil, err
	}
	names, err := m.Store.WorkflowNames(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Job, 0, len(page.Runs))
	for _, header := range page.Runs {
		run, err := m.Store.GetWorkflowRun(ctx, header.ID)
		if err != nil {
			return nil, err
		}
		job, err := m.job(ctx, run, names)
		if err != nil {
			return nil, err
		}
		out = append(out, *job)
	}
	return out, nil
}

// Page lists jobs newest first and reports the total number of runs.
func (m *QueueManager) Page(ctx context.Context, page, pageSize int) ([]Job, int, error) {
	if m == nil || m.Store == nil {
		return nil, 0, sqlite.ErrWorkflowUnavailable
	}
	listed, err := m.Store.ListWorkflowRuns(ctx, page, pageSize, "", "")
	if err != nil {
		return nil, 0, err
	}
	names, err := m.Store.WorkflowNames(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Job, 0, len(listed.Runs))
	for _, header := range listed.Runs {
		run, err := m.Store.GetWorkflowRun(ctx, header.ID)
		if err != nil {
			return nil, 0, err
		}
		job, err := m.job(ctx, run, names)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *job)
	}
	return out, listed.Total, nil
}

// Cancel requests cancellation of a queued or running job.
func (m *QueueManager) Cancel(ctx context.Context, id string) (*Job, error) {
	if m == nil || m.Queue == nil {
		return nil, sqlite.ErrWorkflowUnavailable
	}
	if err := m.Queue.Cancel(ctx, id); err != nil {
		return nil, err
	}
	return m.Get(ctx, id)
}

// Retry queues a new attempt of a failed job; a busy queue reports
// sqlite.ErrWorkflowBusy.
func (m *QueueManager) Retry(ctx context.Context, id string) (*Job, error) {
	if m == nil || m.Queue == nil {
		return nil, sqlite.ErrWorkflowUnavailable
	}
	run, err := m.Queue.Retry(ctx, id)
	if err != nil {
		return nil, err
	}
	return m.Get(ctx, run.ID)
}

func (m *QueueManager) Active(ctx context.Context) (*Job, error) {
	if m == nil || m.Store == nil {
		return nil, sqlite.ErrWorkflowUnavailable
	}
	for _, status := range []string{"running", "queued"} {
		page, err := m.Store.ListWorkflowRuns(ctx, 1, 1, status, "")
		if err != nil {
			return nil, err
		}
		if len(page.Runs) > 0 {
			return m.Get(ctx, page.Runs[0].ID)
		}
	}
	return nil, nil
}

func (m *QueueManager) job(ctx context.Context, run *sqlite.WorkflowRun, names map[string]string) (*Job, error) {
	history, err := m.Store.SyncRunForJob(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	job := &Job{
		ID: run.ID, Kind: KindWorkflow, CreatedAt: run.CreatedAt, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
		TriggeredBy: "admin", Steps: []string{}, Progress: map[string]string{},
	}
	switch run.WorkflowID {
	case sqlite.AdhocSyncWorkflow:
		job.Kind = KindSync
	case sqlite.AdhocScanWorkflow:
		job.Kind = KindScan
	case sqlite.AdhocLiveWorkflow:
		job.Kind = KindLive
	}
	if !sqlite.IsAdhocWorkflow(run.WorkflowID) {
		id := run.WorkflowID
		job.WorkflowID = &id
		if name, ok := names[id]; ok {
			job.WorkflowName = &name
		}
	}
	if run.TriggerSource == "scheduler" {
		job.TriggeredBy = "scheduler"
	}
	counts := map[string]any{}
	if history != nil {
		counts = history.Counts
		if history.TriggeredBy != nil && *history.TriggeredBy != "" && *history.TriggeredBy != "operator" && *history.TriggeredBy != "recovery" {
			job.TriggeredBy = *history.TriggeredBy
		}
		job.Run = map[string]any{
			"id": history.ID, "source": history.Source, "startedAt": history.StartedAt, "finishedAt": history.FinishedAt,
			"status": history.Status, "message": history.Message, "counts": history.Counts,
			"triggeredBy": history.TriggeredBy, "jobId": history.JobID,
		}
	}
	terminalFailure := run.Status == "failed" || run.Status == "cancelled" || run.Status == "blocked"
	for _, step := range run.Steps {
		job.Steps = append(job.Steps, step.StepID)
		job.Progress[step.StepID] = stepProgress(step, counts, terminalFailure)
	}
	if job.Kind == KindWorkflow {
		windows := map[string]any{}
		for _, step := range job.Steps {
			if step != "x" && step != "youtube" {
				continue
			}
			if window, selected := run.FetchWindows[step]; selected {
				windows[step] = window
			} else if days, ok := run.SourceLookbackDays[step]; ok {
				windows[step] = map[string]any{"mode": "recent", "days": days}
			}
		}
		if len(windows) > 0 {
			job.FetchWindows = windows
		}
	}
	switch run.Status {
	case "queued":
		job.Status = StatusQueued
	case "running":
		job.Status = StatusRunning
	case "succeeded":
		job.Status = StatusCompleted
		job.Result = map[string]any{"status": "success", "results": counts}
	case "partial":
		job.Status = StatusCompleted
		job.Result = map[string]any{"status": "partial", "results": counts}
	default:
		job.Status = StatusFailed
		job.Result = map[string]any{"status": "failed", "results": counts}
		code := "failed"
		if run.ErrorCode != nil {
			code = *run.ErrorCode
		}
		job.Error = &code
	}
	return job, nil
}

func stepProgress(step sqlite.WorkflowStep, counts map[string]any, terminalFailure bool) string {
	switch step.Status {
	case "pending":
		return "pending"
	case "running":
		return "running"
	case "succeeded":
		key := step.StepID
		if key == "schedule" {
			key = "schedules"
		}
		if result, ok := counts[key].(map[string]any); ok && result["skipped"] == true {
			return "skipped"
		}
		return "completed"
	case "partial":
		return "partial"
	case "failed":
		return "failed"
	case "blocked":
		// Dependent steps that never ran after an aborted job.
		if step.StartedAt == nil {
			if terminalFailure {
				return "cancelled"
			}
			return "skipped"
		}
		return "failed"
	}
	return "cancelled"
}
