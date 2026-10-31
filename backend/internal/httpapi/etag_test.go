package httpapi

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend/internal/testutil"
)

func TestPolledReadsRevalidateAcrossEncodings(t *testing.T) {
	path := testutil.NewSnapshot(t)
	// Enough stored text to cross the compression threshold.
	for i := 0; i < 8; i++ {
		testutil.Exec(t, path, `INSERT INTO posts (id,source,type,text,published_at,url) VALUES (?,?,?,?,?,?)`,
			fmt.Sprintf("post-%d", i), "x", "daily", strings.Repeat("synthetic ", 20), "2026-10-10T12:00:00.000Z", fmt.Sprintf("https://x.com/kano_2525/status/%d", i))
		testutil.Exec(t, path, `INSERT INTO videos (id,source,title,published_at,url,kind,is_upcoming) VALUES (?,?,?,?,?,?,0)`,
			fmt.Sprintf("video-%d", i), "youtube", strings.Repeat("synthetic ", 20), "2026-10-10T12:00:00.000Z", fmt.Sprintf("https://www.youtube.com/watch?v=synthetic%d", i), "video")
	}
	testutil.Exec(t, path, `INSERT INTO events (id,source,title,starts_on,starts_at,timezone,time_precision,status,event_type,provenance,manual_locked,cancellation_status,cancellation_evidence,created_at,updated_at) VALUES ('stream','x','Stream','2026-10-11','2026-10-11T20:00:00+09:00','Asia/Tokyo','exact','scheduled','stream','automatic',0,'none','','','')`)
	store, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	// Every read sees a later clock, so meta.generatedAt never repeats.
	var clock sync.Mutex
	now := time.Date(2026, 10, 11, 3, 0, 0, 0, time.UTC)
	server := httptest.NewServer(Compress(NewHandlerWithClock(store, func() time.Time {
		clock.Lock()
		defer clock.Unlock()
		now = now.Add(time.Millisecond)
		return now
	})))
	defer server.Close()
	// DisableCompression keeps the transport from transparently decoding.
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	get := func(path, encoding, ifNoneMatch string) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if encoding != "" {
			req.Header.Set("Accept-Encoding", encoding)
		}
		if ifNoneMatch != "" {
			req.Header.Set("If-None-Match", ifNoneMatch)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		return response, body
	}

	for _, path := range []string{"/api/dashboard?days=3", "/api/calendar?from=2026-10-05&to=2026-10-11",
		"/api/posts?from=2026-10-01&to=2026-10-31", "/api/videos?from=2026-10-01&to=2026-10-31"} {
		identity, body := get(path, "", "")
		etag := identity.Header.Get("ETag")
		if identity.StatusCode != http.StatusOK || identity.Header.Get("Cache-Control") != "no-cache" || !strings.HasPrefix(etag, `W/"`) ||
			identity.Header.Get("Vary") != "Accept-Encoding" {
			t.Fatalf("%s identity: status=%d headers=%v", path, identity.StatusCode, identity.Header)
		}
		if len(body) >= compressThreshold {
			gzipped, _ := get(path, "gzip", "")
			if gzipped.Header.Get("Content-Encoding") != "gzip" || gzipped.Header.Get("ETag") != etag || gzipped.Header.Get("Vary") != "Accept-Encoding" {
				t.Fatalf("%s gzip: headers=%v", path, gzipped.Header)
			}
		}
		// A weak or strong spelling of the same tag, from either encoding,
		// revalidates without a body.
		for _, candidate := range []string{etag, strings.TrimPrefix(etag, "W/"), `"other", ` + etag} {
			unchanged, body := get(path, "gzip", candidate)
			if unchanged.StatusCode != http.StatusNotModified || len(body) != 0 || unchanged.Header.Get("Content-Encoding") != "" ||
				unchanged.Header.Get("ETag") != etag || unchanged.Header.Get("Cache-Control") != "no-cache" {
				t.Fatalf("%s If-None-Match %s: status=%d headers=%v body=%q", path, candidate, unchanged.StatusCode, unchanged.Header, body)
			}
		}
		stale, body := get(path, "", `W/"stale"`)
		if stale.StatusCode != http.StatusOK || len(body) == 0 || stale.Header.Get("ETag") != etag {
			t.Fatalf("%s stale tag: status=%d headers=%v", path, stale.StatusCode, stale.Header)
		}
	}

	// A different representation has a different validator.
	week, _ := get("/api/calendar?from=2026-10-05&to=2026-10-11", "", "")
	other, _ := get("/api/calendar?from=2026-10-12&to=2026-10-18", "", "")
	if week.Header.Get("ETag") == other.Header.Get("ETag") {
		t.Fatalf("calendar ranges share ETag %s", week.Header.Get("ETag"))
	}
}
