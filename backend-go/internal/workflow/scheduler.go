package workflow

import (
	"context"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"sync"
	"time"
)

type Scheduler struct {
	Store        *sqlite.Store
	Executor     *Executor
	Queue        *Queue
	Enabled      bool
	TickInterval time.Duration
	Now          func() time.Time
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

func (s *Scheduler) Start() {
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
				// A workflow stays due until it runs, so a failed lookup or
				// submission is retried on the next tick.
				_, _ = s.Tick(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
		s.wg.Wait()
	}
}
func (s *Scheduler) Status() map[string]any {
	interval := s.TickInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return map[string]any{"running": s.cancel != nil, "enabled": s.Enabled, "tickSeconds": int(interval / time.Second)}
}
func (s *Scheduler) Tick(ctx context.Context) (*Run, error) {
	if !s.Enabled || s.Store == nil || (s.Executor == nil && s.Queue == nil) {
		return nil, nil
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	due, e := s.Store.ListDueWorkflows(ctx, now)
	if e != nil || len(due) == 0 {
		return nil, e
	}
	w := due[0]
	key := "scheduler:" + w.ID + ":" + now.UTC().Format("2006-01-02T15:04")
	if s.Queue != nil {
		return s.Queue.Submit(ctx, w.ID, TriggerScheduler, key, nil)
	}
	return s.Executor.Run(ctx, w.ID, TriggerScheduler, key)
}
