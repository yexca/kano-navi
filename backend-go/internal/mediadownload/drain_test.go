package mediadownload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

// queueStore opens a snapshot with n never-checked synthetic X images.
func queueStore(t *testing.T, n int) (*sqlite.Store, string) {
	t.Helper()
	p := testutil.NewSnapshot(t)
	store, err := sqlite.OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	for i := range n {
		u := fmt.Sprintf("https://pbs.twimg.com/media/drain-%d.png", i)
		h := sha256.Sum256([]byte(u))
		testutil.Exec(t, p, `INSERT INTO media_assets(id,source,source_url,status) VALUES(?,'x',?,'pending')`, hex.EncodeToString(h[:]), u)
	}
	return store, p
}

// pngClient answers every request with a distinct PNG, or with the status
// fail returns for that request number (1-based) when it is not zero.
func pngClient(requests *int, fail func(int) int) *Client {
	return NewClient(&http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		*requests++
		if status := fail(*requests); status != 0 {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(strings.NewReader("\x89PNG\r\n\x1a\n" + r.URL.Path))}, nil
	})})
}

func never(int) int { return 0 }

func TestDownloadPendingDrainsTheQueueInBatches(t *testing.T) {
	store, _ := queueStore(t, 7)
	requests := 0
	summary, err := pngClient(&requests, never).DownloadPending(context.Background(), store, Bounds{Batch: 3, Files: 100, Budget: time.Minute})
	if err != nil || summary != (Summary{Attempted: 7, Ready: 7}) || requests != 7 {
		t.Fatalf("summary=%#v requests=%d err=%v", summary, requests, err)
	}
}

func TestDownloadPendingStopsAtTheFileCapAndReportsRemaining(t *testing.T) {
	store, _ := queueStore(t, 7)
	requests := 0
	summary, err := pngClient(&requests, never).DownloadPending(context.Background(), store, Bounds{Batch: 3, Files: 5, Budget: time.Minute})
	if err != nil || summary != (Summary{Attempted: 5, Ready: 5, Remaining: 2, Stopped: StoppedFileCap}) || requests != 5 {
		t.Fatalf("summary=%#v requests=%d err=%v", summary, requests, err)
	}
}

func TestDownloadPendingStopsAtTheTimeBudget(t *testing.T) {
	store, _ := queueStore(t, 7)
	requests := 0
	client := pngClient(&requests, never)
	// Each request takes a synthetic minute; no file starts after 150 seconds.
	start := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	client.now = func() time.Time { return start.Add(time.Duration(requests) * time.Minute) }
	summary, err := client.DownloadPending(context.Background(), store, Bounds{Batch: 2, Files: 100, Budget: 150 * time.Second})
	if err != nil || summary != (Summary{Attempted: 3, Ready: 3, Remaining: 4, Stopped: StoppedTimeBudget}) || requests != 3 {
		t.Fatalf("summary=%#v requests=%d err=%v", summary, requests, err)
	}
}

func TestDownloadPendingStopsOnRateLimit(t *testing.T) {
	store, _ := queueStore(t, 7)
	requests := 0
	limited := func(n int) int {
		if n == 2 {
			return http.StatusTooManyRequests
		}
		return 0
	}
	summary, err := pngClient(&requests, limited).DownloadPending(context.Background(), store, Bounds{Batch: 5, Files: 100, Budget: time.Minute})
	if err != nil || summary != (Summary{Attempted: 2, Ready: 1, Failed: 1, Remaining: 5, Stopped: StoppedRateLimited}) || requests != 2 {
		t.Fatalf("summary=%#v requests=%d err=%v", summary, requests, err)
	}
}

func TestDownloadPendingDoesNotRepeatAnAssetThatStaysQueued(t *testing.T) {
	store, p := queueStore(t, 0)
	// A watch page read "after" every check (a skewed clock) keeps the
	// thumbnail due for revalidation however often it answers 304.
	now := time.Now().UTC()
	testutil.Exec(t, p, `INSERT INTO videos(id,source,title,published_at,url,live_checked_at) VALUES('skewed','youtube','synthetic',?,'https://www.youtube.com/watch?v=skewed',?)`, now.Add(-72*time.Hour).Format(time.RFC3339), now.Add(time.Hour).Format(time.RFC3339))
	u := "https://i.ytimg.com/vi/skewedvideo/hqdefault.jpg"
	h := sha256.Sum256([]byte(u))
	id := hex.EncodeToString(h[:])
	testutil.Exec(t, p, `INSERT INTO media_assets(id,source,source_url,status,etag,last_checked_at) VALUES(?,'youtube',?,'ready','"synthetic"',?)`, id, u, now.Add(-time.Hour).Format(time.RFC3339))
	testutil.Exec(t, p, `INSERT INTO media_links(media_id,owner_type,owner_id,role,position) VALUES(?,'video','skewed','thumbnail',0)`, id)
	requests := 0
	client := NewClient(&http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusNotModified, Body: io.NopCloser(strings.NewReader(""))}, nil
	})})
	summary, err := client.DownloadPending(context.Background(), store, Bounds{Batch: 5, Files: 100, Budget: time.Minute})
	if err != nil || summary != (Summary{Attempted: 1, Unchanged: 1, Stopped: StoppedNoProgress}) || requests != 1 {
		t.Fatalf("summary=%#v requests=%d err=%v", summary, requests, err)
	}
	if again, _ := store.PendingMedia(context.Background(), 5); len(again) != 1 {
		t.Fatalf("fixture no longer stays queued: %#v", again)
	}
}
