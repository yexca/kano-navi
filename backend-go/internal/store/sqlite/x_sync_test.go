package sqlite

import (
	"context"
	"database/sql"
	xsource "github.com/yexca/kano-navi/backend-go/internal/source/x"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
	"testing"
)

func TestWriteXRecentAtomicSnapshotAndCursor(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := xsource.Result{Account: "kano", Posts: []xsource.Post{{ID: "1", AccountHandle: "kano", Type: "notice", Label: "SCHEDULE / 日程", Text: "schedule", PublishedAt: "2026-10-11T00:00:00Z", URL: "https://x.com/kano/status/1", MediaURLs: []string{"https://pbs.twimg.com/a.jpg"}, Raw: map[string]any{"x": 1}}}, DiscoveredCount: 1, RequestedCount: 1, NextCursor: "1", NextCursorTime: "2026-10-11T00:00:00Z", ScheduleCandidates: []xsource.ScheduleCandidate{{PostID: "1", SourceURL: "https://x.com/kano/status/1", MediaURL: "https://pbs.twimg.com/a.jpg", AccountHandle: "kano"}}}
	if err = s.WriteXRecent(context.Background(), "kano", r, true); err != nil {
		t.Fatal(err)
	}
	db, _ := sql.Open("sqlite", "file:"+p+"?mode=ro")
	defer db.Close()
	var posts, cursor, media, assets int
	for query, count := range map[string]*int{
		"SELECT COUNT(*) FROM posts":                               &posts,
		"SELECT COUNT(*) FROM sync_state WHERE cursor_id='1'":      &cursor,
		"SELECT COUNT(*) FROM media_assets WHERE status='pending'": &media,
		"SELECT COUNT(*) FROM assets WHERE kind='schedule'":        &assets,
	} {
		if err = db.QueryRow(query).Scan(count); err != nil {
			t.Fatal(err)
		}
	}
	if posts != 1 || cursor != 1 || media != 1 || assets != 1 {
		t.Fatalf("counts %d %d %d %d", posts, cursor, media, assets)
	}
}
