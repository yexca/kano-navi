package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend/internal/testutil"
)

// cachedDashboard serves a snapshot at a clock the test moves.
type cachedDashboard struct {
	t       *testing.T
	path    string
	handler http.Handler
	mu      sync.Mutex
	now     time.Time
}

func newCachedDashboard(t *testing.T, now time.Time, setup func(path string)) *cachedDashboard {
	t.Helper()
	path := testutil.NewSnapshot(t)
	if setup != nil {
		setup(path)
	}
	store, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	d := &cachedDashboard{t: t, path: path, now: now}
	d.handler = NewHandlerWithClock(store, func() time.Time {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.now
	})
	return d
}

func (d *cachedDashboard) at(now time.Time) {
	d.mu.Lock()
	d.now = now
	d.mu.Unlock()
}

func (d *cachedDashboard) read(etag string) *httptest.ResponseRecorder {
	d.t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	recorder := httptest.NewRecorder()
	d.handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK && recorder.Code != http.StatusNotModified {
		d.t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body)
	}
	return recorder
}

// addVideo stores an upload without advancing the dashboard revision, as a
// write the cache cannot see would.
func (d *cachedDashboard) addVideo(id string) {
	testutil.Exec(d.t, d.path, `INSERT INTO videos (id,source,title,published_at,url,kind) VALUES (?,'youtube',?,'2026-10-10T00:00:00.000Z',?,'video')`, id, id, "https://www.youtube.com/watch?v="+id)
}

func (d *cachedDashboard) bumpRevision() {
	testutil.Exec(d.t, d.path, `INSERT INTO app_settings (key,value,updated_at) VALUES ('dashboard_revision','1','')
		ON CONFLICT(key) DO UPDATE SET value=CAST(value AS INTEGER)+1`)
}

func lists(recorder *httptest.ResponseRecorder, id string) bool {
	return strings.Contains(recorder.Body.String(), `"id":"`+id+`"`)
}

// An unchanged revision is answered from the prepared response, with the
// same body and validator; a new revision computes the snapshot again.
func TestDashboardCacheFollowsTheRevision(t *testing.T) {
	now := time.Date(2026, 10, 11, 3, 0, 10, 0, time.UTC)
	d := newCachedDashboard(t, now, nil)
	first := d.read("")
	etag := first.Header().Get("ETag")
	d.addVideo("unseen-upload")
	d.at(now.Add(30 * time.Second))
	again := d.read("")
	if again.Body.String() != first.Body.String() || again.Header().Get("ETag") != etag {
		t.Fatalf("an unchanged revision recomputed the snapshot")
	}
	if revalidated := d.read(etag); revalidated.Code != http.StatusNotModified {
		t.Fatalf("revalidation status=%d, want 304", revalidated.Code)
	}
	d.bumpRevision()
	changed := d.read("")
	if !lists(changed, "unseen-upload") || changed.Header().Get("ETag") == etag {
		t.Fatalf("a new revision kept the prepared snapshot")
	}
}

// A prepared response stops at the projection's next transition, and
// otherwise at the next whole minute.
func TestDashboardCacheExpires(t *testing.T) {
	now := time.Date(2026, 10, 11, 3, 0, 10, 0, time.UTC)
	t.Run("at the next transition", func(t *testing.T) {
		d := newCachedDashboard(t, now, func(path string) {
			testutil.Exec(t, path, `INSERT INTO events (id,source,title,starts_on,starts_at,event_type,provenance,created_at,updated_at)
				VALUES ('soon','x','Soon','2026-10-11','2026-10-11T03:00:30Z','stream','automatic','','')`)
		})
		first := d.read("")
		if !strings.Contains(first.Body.String(), `"nextTransitionAt":"2026-10-11T03:00:30Z"`) {
			t.Fatalf("fixture has no transition at 03:00:30: %s", first.Body)
		}
		d.addVideo("unseen-upload")
		d.at(now.Add(19 * time.Second))
		if lists(d.read(""), "unseen-upload") {
			t.Fatal("recomputed before the transition")
		}
		d.at(now.Add(20 * time.Second))
		if !lists(d.read(""), "unseen-upload") {
			t.Fatal("kept the prepared snapshot past the transition")
		}
	})
	t.Run("at the next minute", func(t *testing.T) {
		d := newCachedDashboard(t, now, nil)
		d.read("")
		d.addVideo("unseen-upload")
		d.at(now.Add(49 * time.Second))
		if lists(d.read(""), "unseen-upload") {
			t.Fatal("recomputed within the minute")
		}
		d.at(now.Add(50 * time.Second))
		if !lists(d.read(""), "unseen-upload") {
			t.Fatal("kept the prepared snapshot past the minute")
		}
	})
}

// Readers that miss together share one computation; a failed one is not
// kept, and an older revision never replaces a newer entry.
func TestDashboardCacheSharesOneComputation(t *testing.T) {
	now := time.Date(2026, 10, 11, 3, 0, 10, 0, time.UTC)
	clock := func() time.Time { return now }
	key := dashboardKey{revision: 7, days: 3}
	cache := newDashboardCache()
	var computed atomic.Int32
	release := make(chan struct{})
	compute := func() (*dashboardEntry, error) {
		computed.Add(1)
		<-release
		return &dashboardEntry{key: key, body: []byte("{}"), etag: `W/"seven"`, expiresAt: now.Add(time.Minute)}, nil
	}
	var wg sync.WaitGroup
	results := make([]*dashboardEntry, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entry, err := cache.get(context.Background(), key, clock, compute)
			if err != nil {
				t.Error(err)
			}
			results[i] = entry
		}()
	}
	for computed.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	if computed.Load() != 1 {
		t.Fatalf("computed %d times, want 1", computed.Load())
	}
	for _, entry := range results {
		if entry != results[0] {
			t.Fatal("readers received different entries")
		}
	}

	failing := dashboardKey{revision: 8, days: 3}
	if _, err := cache.get(context.Background(), failing, clock, func() (*dashboardEntry, error) { return nil, errors.New("unavailable") }); err == nil {
		t.Fatal("a failed computation succeeded")
	}
	retried := false
	if _, err := cache.get(context.Background(), failing, clock, func() (*dashboardEntry, error) {
		retried = true
		return &dashboardEntry{key: failing, expiresAt: now.Add(time.Minute)}, nil
	}); err != nil || !retried {
		t.Fatalf("a failed computation was kept: retried=%v err=%v", retried, err)
	}

	older := dashboardKey{revision: 6, days: 3}
	if _, err := cache.get(context.Background(), older, clock, func() (*dashboardEntry, error) {
		return &dashboardEntry{key: older, expiresAt: now.Add(time.Minute)}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if kept := cache.entries[3]; kept == nil || kept.key != failing {
		t.Fatalf("entry=%+v, want revision 8 kept", kept)
	}
}
