package workflow

import (
	"context"
	"sync"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// LiveProbe starts the YouTube live-status probe on its operator interval. It
// runs in the API process only and submits through the single-flight queue,
// so it never overlaps another run. A busy queue, an absent active video or a
// failed lookup simply waits for the next tick.
type LiveProbe struct {
	Store *sqlite.Store
	// Submit queues one probe run and reports whether the queue accepted it.
	Submit func(context.Context) (bool, error)
	// Enabled is false when timers or the YouTube source are disabled for the
	// process (WORKFLOW_SCHEDULER_ENABLED=0, SKIP_YOUTUBE=1).
	Enabled      bool
	TickInterval time.Duration
	Now          func() time.Time
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

func (p *LiveProbe) tick() time.Duration {
	if p.TickInterval <= 0 {
		return 30 * time.Second
	}
	return p.TickInterval
}

func (p *LiveProbe) Start() {
	if !p.Enabled || p.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
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
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
		p.wg.Wait()
	}
}

// Status reports the process side of the probe for the console.
func (p *LiveProbe) Status() map[string]any {
	return map[string]any{"available": p.Enabled, "running": p.cancel != nil, "tickSeconds": int(p.tick() / time.Second)}
}

// Tick queues a probe when it is enabled, its interval has passed since the
// previous probe run and at least one video is active. It reports whether a
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
	if last != nil && now.Before(last.Add(time.Duration(settings.IntervalMinutes)*time.Minute)) {
		return false, nil
	}
	active, err := p.Store.YoutubeLiveCandidates(ctx, now, 1)
	if err != nil || len(active) == 0 {
		return false, err
	}
	return p.Submit(ctx)
}
