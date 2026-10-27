package jobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/yexca/kano-navi/backend-go/internal/source/history"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/workflow"
)

// StepOrder is the catalog execution order: sources first, then the
// post-processing that consumes what the sources stored, and finally the
// duplicate-event reconciliation over everything written before it.
var StepOrder = []string{"x", "youtube", "bilibili", "media", "schedule", StepTranslate, "reconcile"}

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
	// Log receives problems that do not fail the call; nil is standard error.
	Log io.Writer
}

func (m *QueueManager) logf(format string, args ...any) {
	out := m.Log
	if out == nil {
		out = os.Stderr
	}
	fmt.Fprintf(out, "kano-go: "+format+"\n", args...)
}

func reason(value string) *string { return &value }

func (m *QueueManager) Start(ctx context.Context, kind, triggeredBy string, options Options) (StartResult, error) {
	if m == nil || m.Store == nil || m.Queue == nil {
		return StartResult{}, sqlite.ErrWorkflowUnavailable
	}
	if kind != KindScan && kind != KindWorkflow && kind != KindLive && kind != KindTranslate {
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
	switch kind {
	case KindWorkflow:
		definition := sqlite.WorkflowDefinition{ID: sqlite.AdhocFetchWorkflow, Steps: NormalizeSteps(options.Steps), FetchWindows: map[string]history.Window{}}
		if options.Source != "" {
			// Run now: the source's chain, as its timer submits it.
			chain, known := workflow.SourceChains[options.Source]
			if !known {
				return StartResult{}, sqlite.ErrWorkflowValidation
			}
			definition.ID, definition.Steps = sqlite.SourceScheduleWorkflow(options.Source), append([]string{}, chain...)
		}
		if len(definition.Steps) == 0 {
			return StartResult{Reason: reason(ReasonNoSteps)}, nil
		}
		for source, window := range options.FetchWindows {
			normalized, windowErr := window.Normalize()
			if windowErr != nil || (source != "x" && source != "youtube") {
				return StartResult{}, sqlite.ErrWorkflowValidation
			}
			definition.FetchWindows[source] = normalized
		}
		definition.SourceSync, err = m.sourceSync(ctx, definition, options.SourceSync)
		if err != nil {
			return StartResult{}, err
		}
		run, err = m.Queue.SubmitDefinition(ctx, definition, trigger, recentDays(definition.FetchWindows))
	case KindLive:
		source = "youtube-live"
		run, err = m.Queue.SubmitDefinition(ctx, sqlite.WorkflowDefinition{ID: sqlite.AdhocLiveWorkflow, Steps: []string{StepLive}}, trigger, nil)
	case KindTranslate:
		source = "translate"
		run, err = m.Queue.SubmitDefinition(ctx, sqlite.WorkflowDefinition{ID: sqlite.AdhocTranslateWorkflow, Steps: []string{StepTranslate}}, trigger, nil)
	case KindScan:
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
	// The run is already queued, so a missing label only changes how its
	// history reads; the operator still gets the accepted job.
	if labelErr := m.Store.LabelSyncRun(ctx, run.ID, source, triggeredBy); labelErr != nil {
		m.logf("run %s history label not saved: %v", run.ID, labelErr)
	}
	job, err := m.Get(ctx, run.ID)
	if err != nil {
		return StartResult{}, err
	}
	return StartResult{Accepted: true, Job: job}, nil
}

// sourceSync freezes how each source step without a window reads: as given,
// or else as its source schedule reads (ADR-0003). Full syncs, scans and
// the other kinds never get here and keep the environment's incremental
// options.
func (m *QueueManager) sourceSync(ctx context.Context, definition sqlite.WorkflowDefinition, given map[string]sqlite.SourceSync) (map[string]sqlite.SourceSync, error) {
	var schedules map[string]sqlite.SourceSync
	out := map[string]sqlite.SourceSync{}
	for _, step := range definition.Steps {
		if _, windowed := definition.FetchWindows[step]; windowed || !slices.Contains(sqlite.SyncSources, step) {
			continue
		}
		if setting, ok := given[step]; ok {
			out[step] = setting
			continue
		}
		if schedules == nil {
			var err error
			if schedules, err = m.Store.SourceScheduleSync(ctx); err != nil {
				return nil, err
			}
		}
		if setting, ok := schedules[step]; ok {
			out[step] = setting
		}
	}
	return out, nil
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
	history, err := m.Store.SyncRunForJob(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	return toJob(run, history), nil
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
	return m.jobs(ctx, page.Runs)
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
	out, err := m.jobs(ctx, listed.Runs)
	if err != nil {
		return nil, 0, err
	}
	return out, listed.Total, nil
}

// jobs presents a listed page of runs with a fixed number of queries,
// however long the page is.
func (m *QueueManager) jobs(ctx context.Context, headers []sqlite.WorkflowRun) ([]Job, error) {
	ids := make([]string, len(headers))
	for i, header := range headers {
		ids[i] = header.ID
	}
	runs, err := m.Store.WorkflowRunSummaries(ctx, ids)
	if err != nil {
		return nil, err
	}
	histories, err := m.Store.SyncRunsForJobs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Job, 0, len(ids))
	for _, id := range ids {
		run, found := runs[id]
		if !found {
			return nil, sqlite.ErrWorkflowNotFound
		}
		out = append(out, *toJob(run, histories[id]))
	}
	return out, nil
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
	// The queue holds at most one active run, and it only moves from queued
	// to running, so reading queued first cannot miss a run claimed between
	// the two reads.
	for _, status := range []string{"queued", "running"} {
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

// toJob presents a run and its newest history row, which may be nil. It reads
// no step attempts, so a summary and a full run present alike.
func toJob(run *sqlite.WorkflowRun, history *sqlite.JobRun) *Job {
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
	case sqlite.AdhocTranslateWorkflow:
		job.Kind = KindTranslate
	}
	if source, scheduled := sqlite.SourceScheduleOf(run.WorkflowID); scheduled {
		job.SourceSchedule = &source
	}
	// An older run of a retired saved workflow keeps its ID; its name is
	// not read (ADR-0003).
	if !sqlite.IsAdhocWorkflow(run.WorkflowID) {
		id := run.WorkflowID
		job.WorkflowID = &id
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
			} else if sync, ok := run.SourceSync[step]; ok {
				windows[step] = sync
			} else if days, ok := run.SourceLookbackDays[step]; ok {
				// An older run of a retired saved workflow, from before
				// sourceSync replaced its days.
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
	return job
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
