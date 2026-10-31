package mediadownload

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend/internal/testutil"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestMediaDestinationsAndResponseBounds(t *testing.T) {
	for _, raw := range []string{"https://twimg.com.attacker.invalid/a", "https://example.invalid/a", "http://pbs.twimg.com/a", "https://pbs.twimg.com:8443/a", "https://pbs.twimg.com/a?token=synthetic", "https://hdslb.com/a", "https://hdslb.com.attacker.invalid/a", "http://i0.hdslb.com/a"} {
		if AllowedURL(raw) {
			t.Fatalf("allowed %s", raw)
		}
	}
	userURL := &url.URL{Scheme: "https", Host: "pbs.twimg.com", Path: "/a", User: url.User("fixture-user")}
	if AllowedURL(userURL.String()) {
		t.Fatal("accepted userinfo")
	}
	for _, raw := range []string{"https://pbs.twimg.com/a", "https://i.ytimg.com/a", "https://sub.twimg.com/a", "https://i0.hdslb.com/bfs/archive/a.jpg"} {
		if !AllowedURL(raw) {
			t.Fatalf("rejected %s", raw)
		}
	}
	for _, tc := range []struct {
		status                     int
		body, mime, location, code string
	}{
		{200, "<html>not image</html>", "text/html", "", "media_invalid_image"},
		{200, "\x89PNG\r\n\x1a\nfixture", "image/jpeg", "", "media_invalid_image"},
		{200, strings.Repeat("x", MaxBytes+1), "image/png", "", "media_response_oversized"},
		{302, "", "", "https://example.invalid/redirect", "media_destination_rejected"},
		{404, "", "", "", "media_missing"},
		{429, "", "", "", "media_rate_limited"},
		{503, "", "", "", "media_unavailable"},
	} {
		calls := 0
		client := NewClient(&http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": {tc.mime}, "Location": {tc.location}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
		})})
		_, err := client.Fetch(context.Background(), "https://pbs.twimg.com/fixture.png")
		if err == nil || err.Error() != tc.code || calls != 1 {
			t.Fatalf("err=%v calls=%d expected=%s", err, calls, tc.code)
		}
	}
}
func TestDownloadPublishesReadableVersionAndFailureRetainsReadyFile(t *testing.T) {
	p := testutil.NewSnapshot(t)
	store, err := sqlite.OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	u := "https://pbs.twimg.com/fixture.png"
	h := sha256.Sum256([]byte(u))
	id := hex.EncodeToString(h[:])
	testutil.Exec(t, p, `INSERT INTO media_assets(id,source,source_url,status) VALUES(?,'x',?,'pending')`, id, u)
	client := NewClient(&http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(strings.NewReader("\x89PNG\r\n\x1a\nsynthetic"))}, nil
	})})
	ctx := context.Background()
	summary, err := client.DownloadPending(ctx, store, Bounds{Batch: 1, Files: 1, Budget: time.Minute})
	if err != nil || summary.Ready != 1 {
		t.Fatalf("summary=%#v err=%v", summary, err)
	}
	file, err := store.ReadMedia(ctx, id, "")
	if err != nil || string(file.Content) != "\x89PNG\r\n\x1a\nsynthetic" {
		t.Fatalf("file=%#v err=%v", file, err)
	}
	if _, err = store.ReadMedia(ctx, id, strings.Repeat("a", 64)); err != sqlite.ErrMediaVersionNotFound {
		t.Fatalf("version err=%v", err)
	}
	revision, _ := store.GetDashboardRevision(ctx)
	if err = store.SaveMediaDownload(ctx, sqlite.PendingMedia{ID: id, Source: "x", SourceURL: u}, nil, "", "", "", "failed", "media_timeout"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ReadMedia(ctx, id, file.SHA256); err != nil {
		t.Fatalf("failed refresh removed ready snapshot: %v", err)
	}
	db, _ := sql.Open("sqlite", p)
	defer db.Close()
	var status, code string
	_ = db.QueryRow(`SELECT status,last_error FROM media_assets WHERE id=?`, id).Scan(&status, &code)
	if status != "ready" || code != "media_timeout" {
		t.Fatalf("status=%s code=%s", status, code)
	}
	after, _ := store.GetDashboardRevision(ctx)
	if after != revision {
		t.Fatal("failed refresh bumped revision")
	}
}
