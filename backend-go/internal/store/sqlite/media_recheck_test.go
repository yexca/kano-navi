package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

func TestPendingMediaRetriesTransientFailuresAndRechecksLiveThumbnails(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now()
	ago := func(d time.Duration) string { return workflowTime(now.Add(-d)) }
	for _, v := range []struct {
		id, start string
		upcoming  int
	}{
		{"upcoming", workflowTime(now.Add(48 * time.Hour)), 1},
		{"recent", ago(2 * time.Hour), 0},
		{"old", ago(72 * time.Hour), 0},
	} {
		testutil.Exec(t, p, `INSERT INTO videos (id,source,title,published_at,url,is_upcoming) VALUES (?,'youtube',?,?,?,?)`, v.id, v.id, v.start, "https://www.youtube.com/watch?v="+v.id, v.upcoming)
	}
	cases := []struct {
		name, source, status, lastError, checked, owner string
		want                                            bool
	}{
		{"never checked", "x", "pending", "", "", "", true},
		{"transient failure", "x", "failed", "media_timeout", ago(time.Hour), "", true},
		{"fresh failure", "x", "failed", "media_timeout", ago(time.Minute), "", false},
		{"permanent failure", "x", "failed", "media_invalid_image", ago(time.Hour), "", false},
		{"missing", "youtube", "missing", "media_missing", ago(time.Hour), "", false},
		{"upcoming stream", "youtube", "ready", "", ago(time.Hour), "upcoming", true},
		{"recent video", "youtube", "ready", "", ago(time.Hour), "recent", true},
		{"recently checked", "youtube", "ready", "", ago(time.Minute), "upcoming", false},
		{"old video", "youtube", "ready", "", ago(time.Hour), "old", false},
		{"unlinked ready", "youtube", "ready", "", ago(time.Hour), "", false},
	}
	for _, c := range cases {
		u := "https://i.ytimg.com/vi/" + c.name + "/hqdefault.jpg"
		testutil.Exec(t, p, `INSERT INTO media_assets(id,source,source_url,status,last_error,last_checked_at,etag,created_at,updated_at) VALUES(?,?,?,?,NULLIF(?,''),NULLIF(?,''),'"synthetic"',?,?)`, sha256Hex(u), c.source, u, c.status, c.lastError, c.checked, ago(96*time.Hour), ago(96*time.Hour))
		if c.owner != "" {
			testutil.Exec(t, p, `INSERT INTO media_links(media_id,owner_type,owner_id,role,position) VALUES(?,'video',?,'thumbnail',0)`, sha256Hex(u), c.owner)
		}
	}
	pending, err := s.PendingMedia(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	selected := map[string]PendingMedia{}
	for _, item := range pending {
		selected[item.SourceURL] = item
	}
	for _, c := range cases {
		item, got := selected["https://i.ytimg.com/vi/"+c.name+"/hqdefault.jpg"]
		if got != c.want {
			t.Fatalf("%s selected = %v, want %v", c.name, got, c.want)
		}
		// Only a ready file is revalidated with its stored validator.
		if got && (item.ETag != "") != (c.status == "ready") {
			t.Fatalf("%s etag = %q", c.name, item.ETag)
		}
	}
}
