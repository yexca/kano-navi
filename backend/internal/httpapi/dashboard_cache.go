package httpapi

import (
	"context"
	"sync"
	"time"
)

// dashboardComputeTimeout bounds one shared dashboard computation, which runs
// detached from the request that started it so a departing client does not
// fail the readers waiting on it. It matches the board's own deadline.
const dashboardComputeTimeout = 15 * time.Second

// dashboardKey names one dashboard response: the durable revision it was
// computed at and its post window.
type dashboardKey struct {
	revision int64
	days     int
}

// dashboardEntry is a prepared dashboard response and the time it stops
// being current.
type dashboardEntry struct {
	key       dashboardKey
	body      []byte
	etag      string
	expiresAt time.Time
}

type dashboardCall struct {
	done  chan struct{}
	entry *dashboardEntry
	err   error
}

// dashboardCache holds the newest prepared response per post window. A read
// at the same revision before the entry expires is answered from it; every
// other read computes once, however many readers arrive meanwhile. The
// revision is read from SQLite on each request, so writes by another process
// (kano-go sync) invalidate it as well.
type dashboardCache struct {
	mu      sync.Mutex
	entries map[int]*dashboardEntry
	calls   map[dashboardKey]*dashboardCall
}

func newDashboardCache() *dashboardCache {
	return &dashboardCache{entries: map[int]*dashboardEntry{}, calls: map[dashboardKey]*dashboardCall{}}
}

// get returns the current entry for key, or computes it with compute, which
// concurrent callers of the same key share. now is read under the lock, so
// an entry is used only before its expiry.
func (c *dashboardCache) get(ctx context.Context, key dashboardKey, now func() time.Time, compute func() (*dashboardEntry, error)) (*dashboardEntry, error) {
	c.mu.Lock()
	if entry := c.entries[key.days]; entry != nil && entry.key == key && now().Before(entry.expiresAt) {
		c.mu.Unlock()
		return entry, nil
	}
	if call := c.calls[key]; call != nil {
		c.mu.Unlock()
		select {
		case <-call.done:
			return call.entry, call.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	call := &dashboardCall{done: make(chan struct{})}
	c.calls[key] = call
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.calls, key)
		// An older revision's computation never replaces a newer entry.
		if call.err == nil && call.entry != nil {
			if current := c.entries[key.days]; current == nil || current.key.revision <= key.revision {
				c.entries[key.days] = call.entry
			}
		}
		c.mu.Unlock()
		close(call.done)
	}()
	call.entry, call.err = compute()
	return call.entry, call.err
}

// dashboardExpiry is when a dashboard computed at now stops being current:
// the store's validUntil when it is earlier, otherwise the next whole minute,
// which bounds the sliding post, week and video windows and the source
// freshness times that move without a revision.
func dashboardExpiry(now, validUntil time.Time) time.Time {
	expiry := now.Truncate(time.Minute).Add(time.Minute)
	if !validUntil.IsZero() && validUntil.Before(expiry) {
		return validUntil
	}
	return expiry
}
