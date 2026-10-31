package workflow

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

// LiveProbe starts the YouTube live-status probe whenever an active video is
// due by its phase (see sqlite.LiveProbeTargets). It runs in the API process
// only and submits through the single-flight queue, so it never overlaps
// another run. A busy queue, nothing due or a failed lookup simply waits for
// the next tick.
type LiveProbe struct {
	Store *sqlite.Store
	// Submit queues one probe run and reports whether the queue accepted it.
	Submit func(context.Context) (bool, error)
	// Enabled is false when timers or the YouTube source are disabled for the
	// process (WORKFLOW_SCHEDULER_ENABLED=0, SKIP_YOUTUBE=1).
	Enabled      bool
	TickInterval time.Duration
	Now          func() time.Time
	// mu orders Start and Stop; running is what Status reads from request
	// goroutines without waiting for a Stop.
	mu      sync.Mutex
	cancel  context.CancelFunc
	running atomic.Bool
	wg      sync.WaitGroup
}

func (p *LiveProbe) tick() time.Duration {
	if p.TickInterval <= 0 {
		return 30 * time.Second
	}
	return p.TickInterval
}

func (p *LiveProbe) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.Enabled || p.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.running.Store(true)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		t := time.NewTicker(p.tick())
		defer t.Stop()
		for {
			select {
			case <-t.C:
				_, _ = p.Tick(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (p *LiveProbe) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
		p.running.Store(false)
		p.wg.Wait()
	}
}

// Status reports the process side of the probe for the console.
func (p *LiveProbe) Status() map[string]any {
	return map[string]any{"available": p.Enabled, "running": p.running.Load(), "tickSeconds": int(p.tick() / time.Second)}
}

// Tick queues a probe when it is enabled and an active video is due, at least
// sqlite.LiveProbeMinGap after the previous probe run. It reports whether a
// run was accepted.
func (p *LiveProbe) Tick(ctx context.Context) (bool, error) {
	if !p.Enabled || p.Store == nil || p.Submit == nil {
		return false, nil
	}
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	settings, err := p.Store.LiveProbeSettings(ctx)
	if err != nil || !settings.Enabled {
		return false, err
	}
	last, err := p.Store.LastLiveProbeAt(ctx)
	if err != nil {
		return false, err
	}
	targets, err := p.Store.LiveProbeTargets(ctx, now, settings)
	if err != nil {
		return false, err
	}
	if next := sqlite.NextLiveProbeAt(targets, last, now); next == nil || now.Before(*next) {
		return false, nil
	}
	return p.Submit(ctx)
}
