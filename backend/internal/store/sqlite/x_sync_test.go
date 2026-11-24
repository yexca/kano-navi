package sqlite

import (
	"context"
	"database/sql"
	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
	"github.com/yexca/kano-navi/backend/internal/testutil"
	"strings"
	"testing"
)

func TestWriteXRecentAtomicSnapshotAndCursor(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := xsource.Result{Account: "kano", Posts: []xsource.Post{{ID: "1", AccountHandle: "kano", Type: "notice", Label: xsource.LabelSchedule, Text: "schedule", PublishedAt: "2026-10-11T00:00:00Z", URL: "https://x.com/kano/status/1", MediaURLs: []string{"https://pbs.twimg.com/a.jpg"}, Raw: map[string]any{"x": 1}}}, DiscoveredCount: 1, RequestedCount: 1, NextCursor: "1", NextCursorTime: "2026-10-11T00:00:00Z", ScheduleCandidates: []xsource.ScheduleCandidate{{PostID: "1", SourceURL: "https://x.com/kano/status/1", MediaURL: "https://pbs.twimg.com/a.jpg", AccountHandle: "kano"}}, Discovery: xsource.DiscoveryTimeline}
	if err = s.WriteXRecent(context.Background(), "kano", r, true); err != nil {
		t.Fatal(err)
	}
	// The next run pages the timeline back to this cursor.
	state, err := s.XRecentState(context.Background(), "kano")
	if err != nil || state.CursorID != "1" || !state.KnownIDs["1"] {
		t.Fatalf("state=%#v err=%v", state, err)
	}
	db, _ := sql.Open("sqlite", "file:"+p+"?mode=ro")
	defer db.Close()
	var posts, cursor, media, assets, timeline int
	for query, count := range map[string]*int{
		"SELECT COUNT(*) FROM posts":                                                                 &posts,
		"SELECT COUNT(*) FROM sync_state WHERE cursor_id='1'":                                        &cursor,
		"SELECT COUNT(*) FROM media_assets WHERE status='pending'":                                   &media,
		"SELECT COUNT(*) FROM assets WHERE kind='schedule'":                                          &assets,
		"SELECT COUNT(*) FROM sync_state WHERE json_extract(metadata_json,'$.discovery')='timeline'": &timeline,
	} {
		if err = db.QueryRow(query).Scan(count); err != nil {
			t.Fatal(err)
		}
	}
	if posts != 1 || cursor != 1 || media != 1 || assets != 1 || timeline != 1 {
		t.Fatalf("counts %d %d %d %d %d", posts, cursor, media, assets, timeline)
	}
}

func TestWriteXRecentKeepsStoredTimelinePostsAndPendingRetries(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	post := func(id, text string) xsource.Post {
		return xsource.Post{ID: id, AccountHandle: "kano", Type: "notice", Label: xsource.LabelSchedule, Text: text, PublishedAt: "2026-10-1" + id + "T00:00:00Z", URL: "https://x.com/kano/status/" + id, MediaURLs: []string{"https://pbs.twimg.com/media/" + id + ".jpg"}, Raw: map[string]any{}}
	}
	candidate := func(id, media string) xsource.ScheduleCandidate {
		return xsource.ScheduleCandidate{PostID: id, SourceURL: "https://x.com/kano/status/" + id, MediaURL: media, AccountHandle: "kano"}
	}
	// An older row still lists its queue without failure counts.
	if _, err = s.db.Exec(`INSERT INTO sync_state(source,account_id,metadata_json) VALUES('x','kano','{"pendingStatusIds":["7","8","9"]}')`); err != nil {
		t.Fatal(err)
	}
	if state, stateErr := s.XRecentState(ctx, "kano"); stateErr != nil || len(state.PendingIDs) != 3 || state.PendingRetries != nil {
		t.Fatalf("state=%#v err=%v", state, stateErr)
	}
	if err = s.WriteXRecent(ctx, "kano", xsource.Result{Account: "kano", Posts: []xsource.Post{post("1", "detail text")}, ScheduleCandidates: []xsource.ScheduleCandidate{candidate("1", "https://pbs.twimg.com/media/1.jpg")}, NextCursor: "1", NextCursorTime: "2026-10-11T00:00:00Z"}, true); err != nil {
		t.Fatal(err)
	}
	// The timeline lists 1 again in another form and 2 for the first time: 1
	// keeps its row and candidate, 2 is written. 7 was dropped and must not
	// come back from the earlier queue; 8 failed once.
	retry := xsource.PendingRetry{Failures: 1, RetryAt: "2026-10-12T01:00:00Z"}
	result := xsource.Result{Account: "kano", DiscoveredCount: 2, RequestedCount: 1,
		Posts:              []xsource.Post{post("1", "timeline text"), post("2", "new")},
		ScheduleCandidates: []xsource.ScheduleCandidate{candidate("1", "https://pbs.twimg.com/media/other.jpg"), candidate("2", "https://pbs.twimg.com/media/2.jpg")},
		FromListing:        map[string]bool{"1": true, "2": true},
		PendingIDs:         []string{"8"}, PendingRetries: map[string]xsource.PendingRetry{"8": retry},
		Dropped:    []xsource.DroppedStatus{{ID: "7", Code: xsource.DroppedGone}},
		NextCursor: "2", NextCursorTime: "2026-10-12T00:00:00Z"}
	if err = s.WriteXRecent(ctx, "kano", result, true); err != nil {
		t.Fatal(err)
	}
	state, err := s.XRecentState(ctx, "kano")
	if err != nil || strings.Join(state.PendingIDs, ",") != "8,9" || len(state.PendingRetries) != 1 || state.PendingRetries["8"] != retry || state.CursorID != "2" {
		t.Fatalf("state=%#v err=%v", state, err)
	}
	for query, want := range map[string]string{
		`SELECT text FROM posts WHERE id='1'`:                                                   "detail text",
		`SELECT text FROM posts WHERE id='2'`:                                                   "new",
		`SELECT url FROM assets WHERE id='schedule-x-1'`:                                        "https://pbs.twimg.com/media/1.jpg",
		`SELECT url FROM assets WHERE id='schedule-x-2'`:                                        "https://pbs.twimg.com/media/2.jpg",
		`SELECT COUNT(*) FROM media_links WHERE owner_id='1'`:                                   "1",
		`SELECT json_type(metadata_json,'$.pendingStatusIds') FROM sync_state WHERE source='x'`: "array",
	} {
		var got string
		if err = s.db.QueryRow(query).Scan(&got); err != nil || got != want {
			t.Fatalf("%s = %q, want %q (err=%v)", query, got, want, err)
		}
	}
}

func TestWriteXRecentKeepsTheTimelineGapUntilARunClosesIt(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	gap := &xsource.Gap{Cursor: "page-3", Floor: "1"}
	opened := xsource.Result{Account: "kano", DiscoveredCount: 2, PendingIDs: []string{"3", "2"}, Gap: gap}
	if err = s.WriteXRecent(ctx, "kano", opened, false); err != nil {
		t.Fatal(err)
	}
	state, err := s.XRecentState(ctx, "kano")
	if err != nil || state.Gap == nil || *state.Gap != *gap || len(state.PendingIDs) != 2 {
		t.Fatalf("state=%#v err=%v", state, err)
	}
	if err = s.WriteXRecent(ctx, "kano", xsource.Result{Account: "kano", DiscoveredCount: 1}, false); err != nil {
		t.Fatal(err)
	}
	if state, err = s.XRecentState(ctx, "kano"); err != nil || state.Gap != nil {
		t.Fatalf("closed gap=%#v err=%v", state.Gap, err)
	}
}

func TestWriteXRecentKeepsAPickedScheduleImage(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	post := func(media string) xsource.Result {
		return xsource.Result{Account: "kano", Posts: []xsource.Post{{ID: "1", AccountHandle: "kano", Type: "notice", Label: xsource.LabelSchedule, Text: "schedule", PublishedAt: "2026-10-11T00:00:00Z", URL: "https://x.com/kano/status/1", MediaURLs: []string{media}, Raw: map[string]any{"x": 1}}},
			DiscoveredCount: 1, RequestedCount: 1, NextCursor: "1", NextCursorTime: "2026-10-11T00:00:00Z", Discovery: xsource.DiscoveryTimeline,
			ScheduleCandidates: []xsource.ScheduleCandidate{{PostID: "1", SourceURL: "https://x.com/kano/status/1", MediaURL: media, AccountHandle: "kano"}}}
	}
	if err = s.WriteXRecent(context.Background(), "kano", post("https://pbs.twimg.com/a.jpg"), true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`UPDATE assets SET url='https://pbs.twimg.com/picked.jpg', manual_image_at='2026-10-12T00:00:00.000Z' WHERE id='schedule-x-1'`); err != nil {
		t.Fatal(err)
	}
	var before int
	if err = s.DB().QueryRow(`SELECT asset_version FROM assets WHERE id='schedule-x-1'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err = s.WriteXRecent(context.Background(), "kano", post("https://pbs.twimg.com/b.jpg"), true); err != nil {
		t.Fatal(err)
	}
	var url string
	var version int
	if err = s.DB().QueryRow(`SELECT url, asset_version FROM assets WHERE id='schedule-x-1'`).Scan(&url, &version); err != nil {
		t.Fatal(err)
	}
	if url != "https://pbs.twimg.com/picked.jpg" || version != before {
		t.Fatalf("synchronization replaced the picked image: %s v%d", url, version)
	}
}

// A replaced image takes no verdict of the old one: the model's returns to
// pending and an operator's schedule label stops approving it, but the label
// stays visible as the previous decision and the asset waits in the Review
// queue; a not_schedule label keeps hiding the new image until the operator
// answers, and the period correction stays.
func TestWriteXRecentAsksAgainForAReplacedScheduleImage(t *testing.T) {
	s, _ := openHistoryStore(t)
	var err error
	post := func(media string) xsource.Result {
		return xsource.Result{Account: "kano", Posts: []xsource.Post{{ID: "1", AccountHandle: "kano", Type: "notice", Label: xsource.LabelSchedule, Text: "schedule", PublishedAt: "2026-10-11T00:00:00Z", URL: "https://x.com/kano/status/1", MediaURLs: []string{media}, Raw: map[string]any{"x": 1}}},
			DiscoveredCount: 1, RequestedCount: 1, NextCursor: "1", NextCursorTime: "2026-10-11T00:00:00Z", Discovery: xsource.DiscoveryTimeline,
			ScheduleCandidates: []xsource.ScheduleCandidate{{PostID: "1", SourceURL: "https://x.com/kano/status/1", MediaURL: media, AccountHandle: "kano"}}}
	}
	read := func() string {
		t.Helper()
		var row string
		if err := s.DB().QueryRow(`SELECT r.llm_status||'/'||r.manual_status||'/'||COALESCE(r.manual_reason,'-')||'/'||COALESCE(a.manual_period_start,'-')
		 FROM assets a JOIN schedule_asset_reviews r ON r.asset_id=a.id WHERE a.id='schedule-x-1'`).Scan(&row); err != nil {
			t.Fatal(err)
		}
		return row
	}
	if err = s.WriteXRecent(context.Background(), "kano", post("https://pbs.twimg.com/a.jpg"), true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`UPDATE schedule_asset_reviews SET llm_status='schedule', manual_status='schedule', manual_reason='approved', manual_checked_at='2026-10-12T00:00:00.000Z' WHERE asset_id='schedule-x-1'`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`UPDATE assets SET manual_period_start='2026-10-12', manual_period_end='2026-10-18', manual_period_reason='corrected' WHERE id='schedule-x-1'`); err != nil {
		t.Fatal(err)
	}
	if err = s.WriteXRecent(context.Background(), "kano", post("https://pbs.twimg.com/a.jpg"), true); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "schedule/schedule/approved/2026-10-12" {
		t.Fatalf("the same image lost its review: %s", got)
	}
	if err = s.WriteXRecent(context.Background(), "kano", post("https://pbs.twimg.com/b.jpg"), true); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "pending/unreviewed/-/2026-10-12" {
		t.Fatalf("a replaced image kept the old verdict: %s", got)
	}
	previous := func() string {
		t.Helper()
		var row string
		if err := s.DB().QueryRow(`SELECT COALESCE(previous_manual_status,'-')||'/'||COALESCE(previous_manual_reason,'-')||'/'||COALESCE(previous_manual_checked_at,'-')
		 FROM schedule_asset_reviews WHERE asset_id='schedule-x-1'`).Scan(&row); err != nil {
			t.Fatal(err)
		}
		return row
	}
	if got := previous(); got != "schedule/approved/2026-10-12T00:00:00.000Z" {
		t.Fatalf("the label on the old image is not kept as the previous decision: %s", got)
	}
	pending := func() int {
		t.Helper()
		var count int
		if err := s.DB().QueryRow(`SELECT COUNT(*) FROM assets a LEFT JOIN schedule_asset_reviews r ON r.asset_id=a.id WHERE a.id='schedule-x-1' AND ` + ScheduleImagePendingSQL("a", "r")).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	// The model approving the new image does not answer the operator's
	// question, and a second replacement keeps the first previous decision.
	if _, err = s.DB().Exec(`UPDATE schedule_asset_reviews SET llm_status='schedule' WHERE asset_id='schedule-x-1'`); err != nil {
		t.Fatal(err)
	}
	if pending() != 1 {
		t.Fatal("a replaced image under an operator's label left the Review queue before the operator answered")
	}
	if err = s.WriteXRecent(context.Background(), "kano", post("https://pbs.twimg.com/c.jpg"), true); err != nil {
		t.Fatal(err)
	}
	if got := previous(); got != "schedule/approved/2026-10-12T00:00:00.000Z" {
		t.Fatalf("a second replacement lost the previous decision: %s", got)
	}

	if _, err = s.DB().Exec(`UPDATE schedule_asset_reviews SET llm_status='schedule', manual_status='not_schedule', manual_reason='not a board', manual_checked_at='2026-10-13T00:00:00.000Z',
	   previous_manual_status=NULL, previous_manual_reason=NULL, previous_manual_checked_at=NULL WHERE asset_id='schedule-x-1'`); err != nil {
		t.Fatal(err)
	}
	if err = s.WriteXRecent(context.Background(), "kano", post("https://pbs.twimg.com/d.jpg"), true); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "pending/not_schedule/not a board/2026-10-12" {
		t.Fatalf("a not_schedule label stopped hiding a replaced image: %s", got)
	}
	if got := previous(); got != "not_schedule/not a board/2026-10-13T00:00:00.000Z" || pending() != 1 {
		t.Fatalf("a replaced image under a not_schedule label does not wait for the operator: %s", got)
	}
}
