package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/testutil"
)

func TestDashboardSynthetic10kCollectionsStayBounded(t *testing.T) {
	store := openSynthetic10k(t)
	defer store.Close()
	dashboard, err := store.GetDashboard(context.Background(), 3, syntheticNow())
	if err != nil {
		t.Fatal(err)
	}
	if len(dashboard.Posts) > 100 {
		t.Fatalf("posts=%d, want at most 100", len(dashboard.Posts))
	}
	if len(dashboard.Events) > 130 {
		t.Fatalf("events=%d, want at most 130", len(dashboard.Events))
	}
	if len(dashboard.Videos) > 61 {
		t.Fatalf("videos=%d, want at most 61", len(dashboard.Videos))
	}
	if dashboard.Summary.Counts.Posts != 10000 || dashboard.Summary.Counts.Events != 10000 || dashboard.Summary.Counts.Videos != int64(len(dashboard.Videos)) {
		t.Fatalf("counts=%+v payload posts=%d events=%d videos=%d", dashboard.Summary.Counts, len(dashboard.Posts), len(dashboard.Events), len(dashboard.Videos))
	}
}

func BenchmarkDashboardSynthetic10k(b *testing.B) {
	store := openSynthetic10k(b)
	defer store.Close()
	ctx := context.Background()
	now := syntheticNow()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.GetDashboard(ctx, 3, now); err != nil {
			b.Fatal(err)
		}
	}
}

func openSynthetic10k(tb testing.TB) *Store {
	tb.Helper()
	path := testutil.NewSnapshot(tb)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		tb.Fatalf("open synthetic writer: %v", err)
	}
	tb.Cleanup(func() { _ = db.Close() })
	tx, err := db.Begin()
	if err != nil {
		tb.Fatalf("begin synthetic fixture: %v", err)
	}
	postStmt, err := tx.Prepare(`INSERT INTO posts (id,source,type,text,published_at,url) VALUES (?,?,?,?,?,?)`)
	if err != nil {
		tb.Fatalf("prepare posts: %v", err)
	}
	defer postStmt.Close()
	eventStmt, err := tx.Prepare(`INSERT INTO events (id,source,source_item_id,title,starts_on,starts_at,timezone,time_precision,status,event_type,url,provenance,manual_locked,cancellation_status,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		tb.Fatalf("prepare events: %v", err)
	}
	defer eventStmt.Close()
	videoStmt, err := tx.Prepare(`INSERT INTO videos (id,source,title,published_at,scheduled_at,url,kind,is_upcoming) VALUES (?,?,?,?,?,?,?,?)`)
	if err != nil {
		tb.Fatalf("prepare videos: %v", err)
	}
	defer videoStmt.Close()
	for i := 0; i < 10000; i++ {
		id := fmt.Sprintf("synthetic-%05d", i)
		if _, err := postStmt.Exec("post-"+id, "x", "daily", "synthetic", "2026-10-10T12:00:00Z", "https://x.com/synthetic/"+id); err != nil {
			tb.Fatalf("insert post %d: %v", i, err)
		}
		videoURL := "https://www.youtube.com/watch?v=" + id
		if _, err := videoStmt.Exec(id, "youtube", "Synthetic video", "2026-10-10T12:00:00Z", "2026-10-11T04:00:00Z", videoURL, "stream", 1); err != nil {
			tb.Fatalf("insert video %d: %v", i, err)
		}
		if _, err := eventStmt.Exec("youtube-"+id, "youtube", id, "Synthetic event", "2026-10-11", "2026-10-11T04:00:00Z", "Asia/Tokyo", "exact", "scheduled", "stream", "https://youtu.be/"+id+"?si=fixture", "automatic", 0, "none", "", ""); err != nil {
			tb.Fatalf("insert event %d: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		tb.Fatalf("commit synthetic fixture: %v", err)
	}
	if _, err := db.Exec(`CREATE INDEX events_stream_identity_idx ON events (stream_identity(url), id) WHERE url IS NOT NULL;
CREATE INDEX events_source_item_idx ON events (source, source_item_id, id);
CREATE INDEX videos_schedule_instant_idx ON videos (julianday(scheduled_at), id);`); err != nil {
		tb.Fatalf("index synthetic fixture: %v", err)
	}
	if err := db.Close(); err != nil {
		tb.Fatalf("close synthetic writer: %v", err)
	}
	store, err := OpenReadOnly(path)
	if err != nil {
		tb.Fatalf("open synthetic fixture: %v", err)
	}
	return store
}

func syntheticNow() time.Time {
	return time.Date(2026, 10, 11, 3, 0, 0, 0, time.FixedZone("JST", 9*60*60))
}
