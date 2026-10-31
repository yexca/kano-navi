package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/config"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend/internal/testutil"
)

// streamFixture serves a snapshot through the production middleware and a
// listener whose write timeout is far shorter than the test's streams.
type streamFixture struct {
	t       *testing.T
	path    string
	handler *Handler
	server  *http.Server
	url     string
}

func newStreamFixture(t *testing.T) *streamFixture {
	t.Helper()
	path := testutil.NewSnapshot(t)
	testutil.Exec(t, path, `INSERT INTO app_settings (key,value,updated_at) VALUES ('dashboard_revision','7','')`)
	store, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	handler := NewHandlerWithAdmin(store, nil, nil)
	handler.streams.interval = 10 * time.Millisecond
	cfg := config.Default()
	cfg.WriteTimeout, cfg.ReadTimeout = 150*time.Millisecond, 150*time.Millisecond
	server := NewServer(RequestLog(Compress(handler), io.Discard), cfg)
	server.RegisterOnShutdown(handler.CloseStreams)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	return &streamFixture{t: t, path: path, handler: handler, server: server, url: "http://" + listener.Addr().String()}
}

func (f *streamFixture) setRevision(value string) {
	f.t.Helper()
	testutil.Exec(f.t, f.path, `UPDATE app_settings SET value=? WHERE key='dashboard_revision'`, value)
}

type eventStream struct {
	t        *testing.T
	response *http.Response
	lines    chan string
}

func (f *streamFixture) open() *eventStream {
	f.t.Helper()
	request, _ := http.NewRequest(http.MethodGet, f.url+"/api/dashboard/events", nil)
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Accept-Encoding", "gzip")
	response, err := (&http.Client{Transport: &http.Transport{DisableCompression: true}}).Do(request)
	if err != nil {
		f.t.Fatal(err)
	}
	stream := &eventStream{t: f.t, response: response, lines: make(chan string, 64)}
	go func() {
		defer close(stream.lines)
		reader := bufio.NewReader(response.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			stream.lines <- strings.TrimSuffix(line, "\n")
		}
	}()
	f.t.Cleanup(func() { _ = response.Body.Close() })
	return stream
}

// next returns the next line, "" at a blank separator, or fails after wait.
// A closed stream reports ok false.
func (s *eventStream) next(wait time.Duration) (string, bool) {
	s.t.Helper()
	select {
	case line, ok := <-s.lines:
		return line, ok
	case <-time.After(wait):
		s.t.Fatal("no stream line in time")
		return "", false
	}
}

// expect reads lines until want appears.
func (s *eventStream) expect(want string, wait time.Duration) {
	s.t.Helper()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		line, ok := s.next(time.Until(deadline))
		if !ok {
			s.t.Fatalf("stream closed before %q", want)
		}
		if line == want {
			return
		}
	}
	s.t.Fatalf("no %q in time", want)
}

func TestRevisionStreamSendsTheCurrentRevisionThenEachChange(t *testing.T) {
	fixture := newStreamFixture(t)
	stream := fixture.open()
	header := stream.response.Header
	if stream.response.StatusCode != http.StatusOK || header.Get("Content-Type") != "text/event-stream" ||
		header.Get("Cache-Control") != "no-store, no-transform" || header.Get("X-Accel-Buffering") != "no" ||
		header.Get("Content-Encoding") != "" || header.Get("ETag") != "" {
		t.Fatalf("stream status=%d headers=%v", stream.response.StatusCode, header)
	}
	for _, want := range []string{"retry: 5000", "data: 7", ""} {
		if line, _ := stream.next(time.Second); line != want {
			t.Fatalf("stream line %q, want %q", line, want)
		}
	}
	// The change arrives after the listener's write timeout has passed.
	time.Sleep(300 * time.Millisecond)
	fixture.setRevision("8")
	stream.expect("data: 8", 2*time.Second)
	fixture.setRevision("9")
	stream.expect("data: 9", 2*time.Second)
}

func TestRevisionStreamSendsHeartbeatsAndOnlyTheRevision(t *testing.T) {
	fixture := newStreamFixture(t)
	fixture.handler.streams.heartbeat = 40 * time.Millisecond
	stream := fixture.open()
	stream.expect("data: 7", time.Second)
	stream.expect(": keepalive", time.Second)
	fixture.setRevision("8")
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		line, ok := stream.next(time.Until(deadline))
		if !ok {
			t.Fatal("stream closed")
		}
		if line == "data: 8" {
			return
		}
		if line != "" && line != ": keepalive" {
			t.Fatalf("unexpected stream line %q", line)
		}
	}
	t.Fatal("no change in time")
}

func TestRevisionStreamRefusesStreamsBeyondTheCap(t *testing.T) {
	fixture := newStreamFixture(t)
	fixture.handler.streams.limit = 1
	first := fixture.open()
	first.expect("data: 7", time.Second)
	response, err := http.Get(fixture.url + "/api/dashboard/events")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var payload map[string]string
	_ = json.NewDecoder(response.Body).Decode(&payload)
	if response.StatusCode != http.StatusServiceUnavailable || payload["error"] != "stream_unavailable" ||
		response.Header.Get("Retry-After") == "" || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("over cap: status=%d headers=%v body=%v", response.StatusCode, response.Header, payload)
	}
	// A closed stream frees its place.
	_ = first.response.Body.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		fixture.handler.streams.mu.Lock()
		open := len(fixture.handler.streams.streams)
		fixture.handler.streams.mu.Unlock()
		if open == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("closed stream kept its place")
		}
		time.Sleep(10 * time.Millisecond)
	}
	fixture.open().expect("data: 7", time.Second)
	if method := request(t, fixture.handler, http.MethodPost, "/api/dashboard/events"); method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d", method.Code)
	}
}

func TestRevisionStreamWatcherStopsWithoutStreams(t *testing.T) {
	fixture := newStreamFixture(t)
	stream := fixture.open()
	stream.expect("data: 7", time.Second)
	_ = stream.response.Body.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		fixture.handler.streams.mu.Lock()
		running := fixture.handler.streams.running
		fixture.handler.streams.mu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("watcher kept running without streams")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// A later stream restarts it and still sees changes.
	next := fixture.open()
	next.expect("data: 7", time.Second)
	fixture.setRevision("10")
	next.expect("data: 10", 2*time.Second)
}

func TestRevisionStreamsEndWhenShutdownBegins(t *testing.T) {
	fixture := newStreamFixture(t)
	stream := fixture.open()
	stream.expect("data: 7", time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	started := time.Now()
	if err := fixture.server.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown with an open stream: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("shutdown waited %v for the stream", elapsed)
	}
	for {
		if _, ok := stream.next(time.Second); !ok {
			break
		}
	}
	if subscribed, ok := fixture.handler.streams.subscribe(); ok || subscribed != nil {
		t.Fatal("a closed hub accepted a stream")
	}
}

func TestRevisionStreamReportsAnUnavailableDatabase(t *testing.T) {
	store, _ := openFixture(t)
	handler := NewHandlerWithAdmin(store, nil, nil)
	_ = store.Close()
	recorder := request(t, handler, http.MethodGet, "/api/dashboard/events")
	if recorder.Code != http.StatusServiceUnavailable || recorder.Body.String() != `{"error":"database_unavailable"}` ||
		recorder.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body)
	}
	handler.streams.mu.Lock()
	open := len(handler.streams.streams)
	handler.streams.mu.Unlock()
	if open != 0 {
		t.Fatal("a failed stream kept its subscription")
	}
}
