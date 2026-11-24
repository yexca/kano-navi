package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/testutil"
)

func TestDashboardPostsFillTheNewestEightBeyondTheWindow(t *testing.T) {
	now := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name               string
		recent, old        int
		wantPosts, wantNew int64
	}{
		{"quiet window", 2, 9, 8, 2},
		{"empty window", 0, 3, 3, 0},
		{"busy window", 9, 4, 9, 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := testutil.NewSnapshot(t)
			insert := func(prefix string, n int, start time.Time) {
				for i := range n {
					id := fmt.Sprintf("%s-%02d", prefix, i)
					at := start.Add(-time.Duration(i) * time.Hour).Format(time.RFC3339Nano)
					testutil.Exec(t, path, `INSERT INTO posts (id,source,account_handle,text,published_at,url) VALUES (?,'x','kano',?,?,?)`, id, id, at, "https://example.invalid/"+id)
				}
			}
			insert("recent", tc.recent, now.Add(-time.Hour))
			insert("old", tc.old, now.AddDate(0, 0, -10))
			store, err := OpenReadOnly(path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			dashboard, err := store.GetDashboard(context.Background(), 3, now)
			if err != nil {
				t.Fatal(err)
			}
			if int64(len(dashboard.Posts)) != tc.wantPosts || dashboard.Summary.Counts.Posts != tc.wantNew {
				t.Fatalf("posts=%d count=%d, want %d and %d", len(dashboard.Posts), dashboard.Summary.Counts.Posts, tc.wantPosts, tc.wantNew)
			}
			for i := 1; i < len(dashboard.Posts); i++ {
				if dashboard.Posts[i-1].PublishedAt < dashboard.Posts[i].PublishedAt {
					t.Fatalf("posts not newest first: %s before %s", dashboard.Posts[i-1].ID, dashboard.Posts[i].ID)
				}
			}
		})
	}
}

func TestDashboardPostsListEachAccountsNewestEight(t *testing.T) {
	now := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	path := testutil.NewSnapshot(t)
	insert := func(account string, n int, start time.Time) {
		for i := range n {
			id := fmt.Sprintf("%s-%03d", account, i)
			at := start.Add(-time.Duration(i) * time.Minute).Format(time.RFC3339Nano)
			testutil.Exec(t, path, `INSERT INTO posts (id,source,account_handle,text,published_at,url) VALUES (?,'x',?,?,?,?)`, id, account, id, at, "https://example.invalid/"+id)
		}
	}
	// A busy account fills the window's 100; the quiet one posted long ago.
	insert("busy", 120, now.Add(-time.Hour))
	insert("Quiet", 10, now.AddDate(0, 0, -20))
	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	dashboard, err := store.GetDashboard(context.Background(), 3, now)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, post := range dashboard.Posts {
		if post.AccountHandle != nil {
			counts[*post.AccountHandle]++
		}
	}
	if counts["busy"] != 100 || counts["Quiet"] != 8 || len(dashboard.Posts) != 108 {
		t.Fatalf("posts=%d per account=%v, want 100 busy and 8 Quiet", len(dashboard.Posts), counts)
	}
	if dashboard.Summary.Counts.Posts != 120 {
		t.Fatalf("count=%d, want the window's 120", dashboard.Summary.Counts.Posts)
	}
}

func TestDashboardPostsLeaveOutArchiveOnlyAccounts(t *testing.T) {
	now := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	path := testutil.NewSnapshot(t)
	testutil.Exec(t, path, `INSERT INTO app_settings(key,value,updated_at) VALUES
		('x_accounts','["board"]',''),
		('x_archive_accounts','["Archived","board"]','')`)
	insert := func(id, account string, at time.Time, promoted bool) {
		var promotedAt any
		if promoted {
			promotedAt = at.Format(time.RFC3339Nano)
		}
		testutil.Exec(t, path, `INSERT INTO posts (id,source,account_handle,text,published_at,url,promoted_at) VALUES (?,'x',?,?,?,?,?)`,
			id, account, id, at.Format(time.RFC3339Nano), "https://example.invalid/"+id, promotedAt)
	}
	// The archive-only account posted last, inside the window; the board's
	// account posted long ago.
	insert("board-old", "board", now.AddDate(0, 0, -20), false)
	insert("archived-new", "archived", now.Add(-time.Hour), false)
	insert("archived-promoted", "Archived", now.Add(-2*time.Hour), true)
	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	dashboard, err := store.GetDashboard(context.Background(), 3, now)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, post := range dashboard.Posts {
		ids = append(ids, post.ID)
	}
	if fmt.Sprint(ids) != "[archived-promoted board-old]" {
		t.Fatalf("board posts=%v", ids)
	}
	if dashboard.Summary.LatestPost == nil || dashboard.Summary.LatestPost.ID != "board-old" {
		t.Fatalf("latest post=%+v", dashboard.Summary.LatestPost)
	}
	if dashboard.Summary.Counts.Posts != 1 {
		t.Fatalf("window count=%d, want the promoted post alone", dashboard.Summary.Counts.Posts)
	}
	// The post archive still lists the account's posts.
	page, err := store.GetPostTimeline(context.Background(), "", "archived", 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("archive items=%d, want 2", len(page.Items))
	}
}

// queryPlan lists the details of a statement's query plan steps.
func queryPlan(t *testing.T, db *sql.DB, statement string, args ...any) []string {
	t.Helper()
	rows, err := db.Query(`EXPLAIN QUERY PLAN `+statement, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	steps := []string{}
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		steps = append(steps, detail)
	}
	if err := rows.Err(); err != nil || len(steps) == 0 {
		t.Fatalf("plan steps=%d err=%v", len(steps), err)
	}
	return steps
}

// The board's post reads run on every snapshot read, so none of them may
// walk the posts table: each account's newest posts, the Bilibili accounts
// and the upstream-deleted schedule candidates are read through indexes.
func TestDashboardPostReadsUseIndexes(t *testing.T) {
	store, err := OpenReadOnly(testutil.NewSnapshot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	scope := ` AND NOT (source='x' AND promoted_at IS NULL AND lower(COALESCE(account_handle,'')) IN (?))`
	for _, tc := range []struct {
		name, statement, index string
		args                   []any
	}{
		{"newest posts per account", dashboardPostsSQL(""), "posts_account_instant_idx", []any{"2026-10-28T00:00:00Z", 8}},
		{"newest posts per account outside the archive", dashboardPostsSQL(scope), "posts_account_instant_idx", []any{"archived", "2026-10-28T00:00:00Z", "archived", 8}},
		{"bilibili accounts", bilibiliAccountsSQL, "posts_bilibili_account_idx", nil},
		{"upstream-deleted schedule candidates", `SELECT a.id FROM assets a WHERE ` + publicScheduleAssetSQL("a"), "(id=?)", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steps := queryPlan(t, store.DB(), tc.statement, tc.args...)
			plan := strings.Join(steps, "; ")
			for _, step := range steps {
				if step == "SCAN posts" || step == "SCAN up" {
					t.Errorf("plan walks the posts: %s", plan)
				}
			}
			if !strings.Contains(plan, tc.index) {
				t.Errorf("plan does not use %s: %s", tc.index, plan)
			}
		})
	}
}
