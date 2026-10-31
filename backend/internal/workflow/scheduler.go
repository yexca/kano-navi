package workflow

import (
	"context"
	"slices"
	"sync"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
	"time"
)

// TriggeredBySourceSchedule labels the runs of source schedules in the run
// history.
const TriggeredBySourceSchedule = "source-schedule"

// SourceChains are the fixed steps a due source schedule submits
// (ADR-0003). reconcile is appended as for every run with a source step.
// YouTube pushes submit the youtube chain as well.
var SourceChains = map[string][]string{
	"x":        {"x", "media", "schedule", "translate"},
	"youtube":  {"youtube", "media", "translate"},
	"bilibili": {"bilibili", "media", "translate"},
}

// Scheduler starts due source schedules through the single-flight queue.
type Scheduler struct {
	Store    *sqlite.Store
	Executor *Executor
	Queue    *Queue
	Enabled  bool
	// SkipSources are the sources disabled by SKIP_*; their schedules never
	// run in this process.
	SkipSources map[string]bool
	// BilibiliFloor is BILIBILI_MIN_INTERVAL_MINUTES.
	BilibiliFloor int
	TickInterval  time.Duration
	Now           func() time.Time
	mu            sync.Mutex
	cancel        context.CancelFunc
	wg            sync.WaitGroup
}

func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.Enabled || s.cancel != nil {
		return
	}
	interval := s.TickInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				// A source stays due until it runs, so a failed lookup or
				// submission is retried on the next tick.
				_, _ = s.Tick(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
		s.wg.Wait()
	}
}
func (s *Scheduler) Status() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	interval := s.TickInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return map[string]any{"running": s.cancel != nil, "enabled": s.Enabled, "tickSeconds": int(interval / time.Second)}
}

// Tick submits the due source schedule with the earliest next run, so a
// frequent source cannot starve another. A busy queue leaves it due for the
// next tick.
func (s *Scheduler) Tick(ctx context.Context) (*Run, error) {
	if !s.Enabled || s.Store == nil || (s.Executor == nil && s.Queue == nil) {
		return nil, nil
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	source, e := s.Store.DueSourceSchedule(ctx, now, s.SkipSources, s.BilibiliFloor)
	if e != nil || source == nil {
		return nil, e
	}
	return s.submitSource(ctx, *source)
}

// submitSource queues a source's fixed chain with its sync setting and
// method chain frozen on the run, labelled source-schedule in the run
// history.
func (s *Scheduler) submitSource(ctx context.Context, schedule sqlite.SourceSchedule) (*Run, error) {
	definition := sqlite.WorkflowDefinition{
		ID:    sqlite.SourceScheduleWorkflow(schedule.Source),
		Steps: append([]string{}, SourceChains[schedule.Source]...),
	}
	if schedule.Sync != nil {
		definition.SourceSync = map[string]sqlite.SourceSync{schedule.Source: *schedule.Sync}
	}
	if slices.Contains(sqlite.ChainSources, schedule.Source) {
		definition.SourceChain = map[string]sqlite.SourceChain{schedule.Source: schedule.Chain}
	}
	var run *Run
	var err error
	if s.Queue != nil {
		run, err = s.Queue.SubmitDefinition(ctx, definition, TriggerScheduler, nil)
		if err == nil {
			_ = s.Store.LabelSyncRun(ctx, run.ID, "workflow", TriggeredBySourceSchedule)
		}
		return run, err
	}
	run, err = s.Executor.PrepareDefinition(ctx, definition, TriggerScheduler, "", nil)
	if err != nil || run.Status != "queued" {
		return run, err
	}
	_ = s.Store.LabelSyncRun(ctx, run.ID, "workflow", TriggeredBySourceSchedule)
	return s.Executor.ExecuteRun(ctx, run)
}
