package sqlite

import (
	"context"
	"fmt"
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
