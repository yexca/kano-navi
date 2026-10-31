package workflow

import (
	"context"
	"sync"
	"time"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

// RunPruner deletes finished runs past the operator's retention windows once
// at start and then once a day. It works on the local database only, so it
// is not a run and does not use the queue.
type RunPruner struct {
	Store    *sqlite.Store
	Interval time.Duration
	Now      func() time.Time
	mu       sync.Mutex
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	// last is the newest pass, read by Status.
	last struct {
		at      *time.Time
		deleted int
		failed  bool
	}
}

func (p *RunPruner) interval() time.Duration {
	if p.Interval <= 0 {
		return 24 * time.Hour
	}
	return p.Interval
}

func (p *RunPruner) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Store == nil || p.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		_, _ = p.Prune(ctx)
		t := time.NewTicker(p.interval())
		defer t.Stop()
		for {
			select {
			case <-t.C:
				_, _ = p.Prune(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (p *RunPruner) Stop() {
	p.mu.Lock()
	cancel := p.cancel
	p.cancel = nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
		p.wg.Wait()
	}
}

// Prune runs one pass with the stored retention settings.
func (p *RunPruner) Prune(ctx context.Context) (int, error) {
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	settings, err := p.Store.RunRetention(ctx)
	deleted := 0
	if err == nil {
		deleted, err = p.Store.PruneRuns(ctx, settings, now)
	}
	if ctx.Err() != nil {
		return deleted, ctx.Err()
	}
	p.mu.Lock()
	p.last.at, p.last.deleted, p.last.failed = &now, deleted, err != nil
	p.mu.Unlock()
	return deleted, err
}

// Status reports the newest pass for the console.
func (p *RunPruner) Status() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	var at any
	if p.last.at != nil {
		at = sqlite.ISOTime(*p.last.at)
	}
	return map[string]any{
		"available": true, "running": p.cancel != nil, "intervalHours": int(p.interval() / time.Hour),
		"lastPrunedAt": at, "lastDeleted": p.last.deleted, "lastFailed": p.last.failed,
	}
}
