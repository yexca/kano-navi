package jobs

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/source/history"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
	"github.com/yexca/kano-navi/backend-go/internal/workflow"
)

// heldSteps holds every step until released and fails the chosen steps
// with a retryable error.
type heldSteps struct {
	mu   sync.Mutex
	gate chan struct{}
	fail map[string]bool
}

func (h *heldSteps) Execute(ctx context.Context, in workflow.StepInput) (workflow.AttemptResult, error) {
	h.mu.Lock()
	gate, fail := h.gate, h.fail[in.StepID]
	h.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return workflow.AttemptResult{}, workflow.NewStepError("cancelled", workflow.ErrCancelled)
		}
	}
	if fail {
		return workflow.AttemptResult{}, workflow.NewStepError("dependency_unavailable", workflow.ErrDependency)
	}
	return workflow.AttemptResult{Status: "succeeded", Result: map[string]any{"count": 1}}, nil
}

func (h *heldSteps) set(gate chan struct{}, fail map[string]bool) {
	h.mu.Lock()
	h.gate, h.fail = gate, fail
	h.mu.Unlock()
}

func newManager(t *testing.T) (*QueueManager, *heldSteps, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "database", "kano.sqlite")
	store, err := sqlite.OpenInitialized(path, true, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	steps := &heldSteps{}
	queue := &workflow.Queue{Store: store, Executor: &workflow.Executor{Store: store, Steps: steps, MaxAttempts: 1}}
	t.Cleanup(queue.Close)
	return &QueueManager{Store: store, Queue: queue, Log: &bytes.Buffer{}}, steps, path
}

func waitDone(t *testing.T, m *QueueManager, id string) *Job {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		job, err := m.Get(context.Background(), id)
		if err != nil || job == nil {
			t.Fatalf("job=%v err=%v", job, err)
		}
		if job.Status != StatusQueued && job.Status != StatusRunning {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s stuck in %s", id, job.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func mustStart(t *testing.T, m *QueueManager, kind, triggeredBy string, options Options) *Job {
	t.Helper()
	result, err := m.Start(context.Background(), kind, triggeredBy, options)
	if err != nil || !result.Accepted || result.Reason != nil || result.Job == nil {
		t.Fatalf("start %s: result=%+v err=%v", kind, result, err)
	}
	return result.Job
}

func TestNormalizeStepsKeepsKnownStepsInCatalogOrder(t *testing.T) {
	got := NormalizeSteps([]string{"reconcile", "x", "unknown", StepLive, "x", "media"})
	if !reflect.DeepEqual(got, []string{"x", "media", "reconcile"}) {
		t.Fatalf("steps=%v", got)
	}
	if got := NormalizeSteps(nil); got == nil || len(got) != 0 {
		t.Fatalf("empty selection=%#v", got)
	}
}

func TestStartPresentsEachKindWithItsStepsAndLabel(t *testing.T) {
	m, _, _ := newManager(t)
	long := strings.Repeat("あ", 70)
	cases := []struct {
		name, kind, triggeredBy string
		options                 Options
		wantKind, wantSource    string
		wantTrigger             string
		wantSteps               []string
		wantSchedule            string
	}{
		{name: "unknown kind is a full sync", kind: "", triggeredBy: "", wantKind: KindSync, wantSource: "manual", wantTrigger: "admin", wantSteps: StepOrder},
		{name: "scan", kind: KindScan, triggeredBy: "mcp", wantKind: KindScan, wantSource: "schedule", wantTrigger: "mcp", wantSteps: []string{"schedule"}},
		{name: "live probe", kind: KindLive, triggeredBy: TriggeredByLiveProbe, wantKind: KindLive, wantSource: "youtube-live", wantTrigger: TriggeredByLiveProbe, wantSteps: []string{StepLive}},
		{name: "translate", kind: KindTranslate, triggeredBy: "scheduler", wantKind: KindTranslate, wantSource: "translate", wantTrigger: "scheduler", wantSteps: []string{StepTranslate}},
		{name: "step run", kind: KindWorkflow, triggeredBy: long, options: Options{Steps: []string{"youtube", "bogus", "x"}}, wantKind: KindWorkflow, wantSource: "workflow", wantTrigger: strings.Repeat("あ", 64), wantSteps: []string{"x", "youtube"}},
		{name: "run now", kind: KindWorkflow, triggeredBy: "admin", options: Options{Source: "youtube", Steps: []string{"x"}}, wantKind: KindWorkflow, wantSource: "workflow", wantTrigger: "admin", wantSteps: workflow.SourceChains["youtube"], wantSchedule: "youtube"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			job := mustStart(t, m, c.kind, c.triggeredBy, c.options)
			if job.Kind != c.wantKind || job.TriggeredBy != c.wantTrigger || !reflect.DeepEqual(job.Steps, c.wantSteps) || job.WorkflowID != nil {
				t.Fatalf("job=%+v", job)
			}
			if job.Run == nil || job.Run["source"] != c.wantSource {
				t.Fatalf("history=%v", job.Run)
			}
			if (job.SourceSchedule == nil) != (c.wantSchedule == "") || (job.SourceSchedule != nil && *job.SourceSchedule != c.wantSchedule) {
				t.Fatalf("source schedule=%v", job.SourceSchedule)
			}
			done := waitDone(t, m, job.ID)
			if done.Status != StatusCompleted || done.Error != nil || done.Result["status"] != "success" {
				t.Fatalf("done=%+v", done)
			}
			for _, step := range c.wantSteps {
				if done.Progress[step] != "completed" {
					t.Fatalf("progress=%v", done.Progress)
				}
			}
		})
	}
}

func TestStartFreezesStepRunWindowsAndSync(t *testing.T) {
	m, _, _ := newManager(t)
	ctx := context.Background()
	window := history.Window{Mode: history.ModeRecent, Days: 3}
	sync := sqlite.SourceSync{Mode: sqlite.SyncCount, Count: 5}
	job := mustStart(t, m, KindWorkflow, "admin", Options{
		Steps:        []string{"x", "youtube"},
		FetchWindows: map[string]history.Window{"youtube": window},
		SourceSync:   map[string]sqlite.SourceSync{"x": sync},
	})
	normalized, err := window.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if job.FetchWindows["x"] != sync || job.FetchWindows["youtube"] != normalized {
		t.Fatalf("windows=%#v", job.FetchWindows)
	}
	waitDone(t, m, job.ID)

	if _, err := m.Start(ctx, KindWorkflow, "admin", Options{Steps: []string{"x"}, FetchWindows: map[string]history.Window{"bilibili": window}}); !errors.Is(err, sqlite.ErrWorkflowValidation) {
		t.Fatalf("window for a source without windows: err=%v", err)
	}
	if _, err := m.Start(ctx, KindWorkflow, "admin", Options{Source: "unknown"}); !errors.Is(err, sqlite.ErrWorkflowValidation) {
		t.Fatalf("unknown source chain: err=%v", err)
	}
	result, err := m.Start(ctx, KindWorkflow, "admin", Options{Steps: []string{StepLive, "bogus"}})
	if err != nil || result.Accepted || result.Reason == nil || *result.Reason != ReasonNoSteps || result.Job != nil {
		t.Fatalf("no steps: result=%+v err=%v", result, err)
	}
}

func TestStartReportsBusyQueueAndCancelReleasesIt(t *testing.T) {
	m, steps, _ := newManager(t)
	ctx := context.Background()
	if active, err := m.Active(ctx); err != nil || active != nil {
		t.Fatalf("idle active=%v err=%v", active, err)
	}
	gate := make(chan struct{})
	defer close(gate)
	steps.set(gate, nil)
	first := mustStart(t, m, KindScan, "admin", Options{})

	active, err := m.Active(ctx)
	if err != nil || active == nil || active.ID != first.ID {
		t.Fatalf("active=%+v err=%v", active, err)
	}
	busy, err := m.Start(ctx, KindSync, "admin", Options{})
	if err != nil || busy.Accepted || busy.Reason == nil || *busy.Reason != ReasonAlreadyRunning || busy.Job == nil || busy.Job.ID != first.ID {
		t.Fatalf("busy=%+v err=%v", busy, err)
	}

	cancelled, err := m.Cancel(ctx, first.ID)
	if err != nil || cancelled == nil || cancelled.ID != first.ID {
		t.Fatalf("cancel=%+v err=%v", cancelled, err)
	}
	done := waitDone(t, m, first.ID)
	if done.Status != StatusFailed || done.Error == nil || done.Progress["schedule"] != "cancelled" {
		t.Fatalf("cancelled job=%+v", done)
	}
	if active, err = m.Active(ctx); err != nil || active != nil {
		t.Fatalf("active after cancel=%v err=%v", active, err)
	}
	steps.set(nil, nil)
	waitDone(t, m, mustStart(t, m, KindScan, "admin", Options{}).ID)
}

func TestRetryRequeuesTheSameFailedJob(t *testing.T) {
	m, steps, _ := newManager(t)
	ctx := context.Background()
	steps.set(nil, map[string]bool{"translate": true})
	job := mustStart(t, m, KindTranslate, "admin", Options{})
	failed := waitDone(t, m, job.ID)
	if failed.Status != StatusFailed || failed.Progress[StepTranslate] != "failed" || failed.Error == nil {
		t.Fatalf("failed=%+v", failed)
	}

	steps.set(nil, nil)
	retried, err := m.Retry(ctx, job.ID)
	if err != nil || retried == nil || retried.ID != job.ID {
		t.Fatalf("retry=%+v err=%v", retried, err)
	}
	done := waitDone(t, m, job.ID)
	if done.Status != StatusCompleted || done.Error != nil || done.Progress[StepTranslate] != "completed" || done.Run["source"] != "translate" {
		t.Fatalf("retried=%+v", done)
	}
	if _, err := m.Retry(ctx, "missing"); !errors.Is(err, sqlite.ErrWorkflowNotFound) {
		t.Fatalf("missing retry err=%v", err)
	}
}

func TestListAndPageMatchEachJobDetail(t *testing.T) {
	m, steps, path := newManager(t)
	ctx := context.Background()
	ids := []string{}
	start := func(kind string, options Options) {
		job := mustStart(t, m, kind, "admin", options)
		waitDone(t, m, job.ID)
		ids = append(ids, job.ID)
	}
	start(KindSync, Options{})
	steps.set(nil, map[string]bool{"schedule": true})
	start(KindScan, Options{})
	steps.set(nil, nil)
	start(KindWorkflow, Options{Steps: []string{"x", "youtube"}, FetchWindows: map[string]history.Window{"x": {Mode: history.ModeRecent, Days: 2}}})
	start(KindWorkflow, Options{Source: "bilibili"})
	start(KindLive, Options{})

	// An older run of a retired saved workflow with frozen lookback days, and
	// a run whose history row is gone.
	legacy, err := m.Store.CreateWorkflowRun(ctx, sqlite.WorkflowDefinition{ID: "legacy-daily", Steps: []string{"x", "media"}, SourceLookbackDays: map[string]int{"x": 4}}, "scheduler", "", map[string]bool{"x": true, "media": true}, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, path, `UPDATE workflow_runs SET status='failed',error_code='timeout' WHERE id=?`, legacy.ID)
	testutil.Exec(t, path, `DELETE FROM sync_runs WHERE job_id=?`, ids[0])
	ids = append(ids, legacy.ID)

	want := map[string]Job{}
	for _, id := range ids {
		job, err := m.Get(ctx, id)
		if err != nil || job == nil {
			t.Fatalf("get %s: %v", id, err)
		}
		want[id] = *job
	}
	if legacy := want[legacy.ID]; legacy.WorkflowID == nil || legacy.FetchWindows["x"] == nil || legacy.TriggeredBy != "scheduler" {
		t.Fatalf("legacy job=%+v", legacy)
	}
	if want[ids[0]].Run != nil {
		t.Fatalf("job without history=%+v", want[ids[0]])
	}

	listed, err := m.List(ctx, 100)
	if err != nil || len(listed) != len(ids) {
		t.Fatalf("list=%d err=%v", len(listed), err)
	}
	for _, job := range listed {
		if !reflect.DeepEqual(job, want[job.ID]) {
			t.Fatalf("listed %s\n got=%+v\nwant=%+v", job.ID, job, want[job.ID])
		}
	}
	paged := []Job{}
	for page := 1; page <= 3; page++ {
		jobs, total, err := m.Page(ctx, page, 2)
		if err != nil || total != len(ids) {
			t.Fatalf("page %d total=%d err=%v", page, total, err)
		}
		paged = append(paged, jobs...)
	}
	if !reflect.DeepEqual(paged, listed) {
		t.Fatalf("pages differ from the list")
	}
	if short, err := m.List(ctx, 2); err != nil || !reflect.DeepEqual(short, listed[:2]) {
		t.Fatalf("limited list=%v err=%v", short, err)
	}
}

func TestStartLogsAnUnsavedHistoryLabel(t *testing.T) {
	m, _, path := newManager(t)
	log := &bytes.Buffer{}
	m.Log = log
	testutil.Exec(t, path, `CREATE TRIGGER refuse_label BEFORE UPDATE OF source ON sync_runs WHEN NEW.source='translate' BEGIN SELECT RAISE(ABORT,'synthetic'); END`)
	job := mustStart(t, m, KindTranslate, "admin", Options{})
	if job.Run == nil || job.Run["source"] != "workflow" {
		t.Fatalf("history=%v", job.Run)
	}
	if !strings.Contains(log.String(), "run "+job.ID+" history label not saved") {
		t.Fatalf("log=%q", log.String())
	}
	waitDone(t, m, job.ID)
}
