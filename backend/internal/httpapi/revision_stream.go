package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	// revisionStreamLimit caps the open revision streams of one process.
	revisionStreamLimit = 1000
	// revisionWatchInterval is how often the watcher reads the revision.
	// Other processes, such as `kano-go sync`, write SQLite directly, so
	// reading the stored value is the only signal that covers every writer.
	revisionWatchInterval = time.Second
	// revisionHeartbeat keeps proxies and browsers from closing a quiet
	// stream as idle.
	revisionHeartbeat = 25 * time.Second
	// revisionWriteTimeout bounds each write to a stream, so a client that
	// stops reading releases its subscription.
	revisionWriteTimeout = 10 * time.Second
	// revisionRetryMS is the reconnect delay the stream asks EventSource for.
	revisionRetryMS = 5000
)

// revisionHub fans the stored dashboard revision out to the open revision
// streams. One watcher goroutine reads the revision while any stream is
// open; each stream receives only changes, never snapshot data.
type revisionHub struct {
	read      func(context.Context) (int64, error)
	interval  time.Duration
	heartbeat time.Duration
	limit     int

	mu      sync.Mutex
	streams map[chan int64]struct{}
	running bool
	closed  bool
	done    chan struct{}
	// latest is the revision the watcher last read, valid once known is set.
	latest int64
	known  bool
}

func newRevisionHub(read func(context.Context) (int64, error)) *revisionHub {
	return &revisionHub{
		read: read, interval: revisionWatchInterval, heartbeat: revisionHeartbeat, limit: revisionStreamLimit,
		streams: map[chan int64]struct{}{}, done: make(chan struct{}),
	}
}

// subscribe registers a stream and starts the watcher if it is idle. It
// reports false when the hub is full or closed.
func (h *revisionHub) subscribe() (chan int64, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || len(h.streams) >= h.limit {
		return nil, false
	}
	updates := make(chan int64, 1)
	h.streams[updates] = struct{}{}
	if !h.running {
		h.running = true
		go h.watch()
	}
	return updates, true
}

func (h *revisionHub) unsubscribe(updates chan int64) {
	h.mu.Lock()
	delete(h.streams, updates)
	h.mu.Unlock()
}

// watch reads the revision every interval and offers each change to every
// stream. It stops once no stream is open, so an idle process keeps no
// timer, and forgets the last revision so a later stream starts fresh.
func (h *revisionHub) watch() {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-ticker.C:
		}
		h.mu.Lock()
		if len(h.streams) == 0 {
			h.running, h.known = false, false
			h.mu.Unlock()
			return
		}
		h.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), h.interval*5)
		revision, err := h.read(ctx)
		cancel()
		if err != nil {
			// A failed read keeps the last known revision; streams stay open.
			continue
		}
		h.mu.Lock()
		if !h.known || revision != h.latest {
			h.latest, h.known = revision, true
			for updates := range h.streams {
				// Each stream keeps only the newest revision it has not sent.
				select {
				case <-updates:
				default:
				}
				updates <- revision
			}
		}
		h.mu.Unlock()
	}
}

// close ends every open stream and refuses new ones. The server calls it
// when shutdown begins, so long-lived streams do not hold the shutdown.
func (h *revisionHub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closed {
		h.closed = true
		close(h.done)
	}
}

// CloseStreams ends the open revision streams; pass it to
// http.Server.RegisterOnShutdown.
func (h *Handler) CloseStreams() {
	h.streams.close()
}

// revisionEvents serves GET /api/dashboard/events, a text/event-stream that
// sends the current dashboard revision on connect and each later change.
// It carries the same public number as /api/dashboard/revision and nothing
// else; comments keep a quiet stream alive.
func (h *Handler) revisionEvents(w http.ResponseWriter, r *http.Request) {
	updates, ok := h.streams.subscribe()
	if !ok {
		w.Header().Set("Retry-After", "30")
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "stream_unavailable"}, "no-store")
		return
	}
	defer h.streams.unsubscribe(updates)
	revision, err := h.store.GetDashboardRevision(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database_unavailable"}, "no-store")
		return
	}
	controller := http.NewResponseController(w)
	// The listener's read and write timeouts suit bounded replies; this
	// stream instead bounds each write and ends with the client or shutdown.
	_ = controller.SetReadDeadline(time.Time{})
	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-store, no-transform")
	header.Set("X-Accel-Buffering", "no")
	send := func(chunk string) bool {
		_ = controller.SetWriteDeadline(time.Now().Add(revisionWriteTimeout))
		if _, err := w.Write([]byte(chunk)); err != nil {
			return false
		}
		return controller.Flush() == nil
	}
	w.WriteHeader(http.StatusOK)
	sent := revision
	if !send("retry: " + strconv.Itoa(revisionRetryMS) + "\ndata: " + strconv.FormatInt(revision, 10) + "\n\n") {
		return
	}
	heartbeat := time.NewTicker(h.streams.heartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-h.streams.done:
			return
		case <-heartbeat.C:
			if !send(": keepalive\n\n") {
				return
			}
		case next := <-updates:
			if next == sent {
				continue
			}
			sent = next
			if !send("data: " + strconv.FormatInt(next, 10) + "\n\n") {
				return
			}
		}
	}
}
