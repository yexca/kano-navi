package sqlite

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/testutil"
)

// An event names a YouTube video by its entry ID, its source item, its event
// sources or its stream URL; the board's active videos and its cancellations
// honour every one of them.
func TestDashboardVideosMatchEventsByEveryIdentity(t *testing.T) {
	now := time.Date(2026, 11, 2, 4, 0, 0, 0, time.UTC)
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	path := testutil.NewSnapshot(t)
	exec := func(q string, args ...any) { testutil.Exec(t, path, q, args...) }
	// Newer streams and uploads fill the shelf's own pools, so an old stream
	// is listed only as an active video.
	for i := range 12 {
		for _, kind := range []string{"stream", "video"} {
			id := fmt.Sprintf("%s-%02d", kind, i)
			exec(`INSERT INTO videos (id,source,title,published_at,url,kind) VALUES (?,'youtube',?,?,?,?)`, id, id, at(-24*time.Hour), "https://www.youtube.com/watch?v="+id, kind)
		}
	}
	videoID := func(state, identity string) string { return (state + identity + "___________")[:11] }
	videoURL := func(id string) string { return "https://www.youtube.com/watch?v=" + id }
	// link stores an event naming the video by identity.
	link := func(identity, video, endsAt, cancellation string) {
		id, source, item, url := "ev-"+video, "x", any(nil), any(nil)
		switch identity {
		case "entry":
			id = "youtube-" + video
		case "item":
			source, item = "youtube", video
		case "url":
			url = "https://youtu.be/" + video
		}
		exec(`INSERT INTO events (id,source,source_item_id,title,starts_on,starts_at,ends_at,url,provenance,manual_locked,cancellation_status,created_at,updated_at)
			VALUES (?,?,?,?,'2026-11-02',?,?,?,'automatic',0,?,'','')`, id, source, item, id, at(-2*time.Hour), endsAt, url, cancellation)
		if identity == "source" {
			exec(`INSERT INTO event_sources (event_id,source,source_item_id,source_key,created_at,updated_at) VALUES (?,'youtube',?,?,'','')`, id, video, id)
		}
	}
	identities := []string{"entry", "item", "source", "url"}
	for _, identity := range identities {
		// An old stream whose event is still open is active.
		open := videoID("open", identity)
		exec(`INSERT INTO videos (id,source,title,published_at,url,kind) VALUES (?,'youtube',?,?,?,'stream')`, open, open, at(-90*24*time.Hour), videoURL(open))
		link(identity, open, at(time.Hour), "none")
		// A waiting room whose event has ended is not.
		ended := videoID("ended", identity)
		exec(`INSERT INTO videos (id,source,title,published_at,url,kind,is_upcoming) VALUES (?,'youtube',?,?,?,'stream',1)`, ended, ended, at(-90*24*time.Hour), videoURL(ended))
		link(identity, ended, at(-time.Hour), "none")
		// An operator-confirmed cancellation names its video.
		cancelled := videoID("cancel", identity)
		exec(`INSERT INTO videos (id,source,title,published_at,url,kind) VALUES (?,'youtube',?,?,?,'stream')`, cancelled, cancelled, at(-90*24*time.Hour), videoURL(cancelled))
		link(identity, cancelled, at(time.Hour), "manual_confirmed")
	}
	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	videos, err := store.queryVideos(context.Background(), now, now.Add(-10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, video := range videos {
		listed[video.ID] = true
	}
	candidates := []Video{}
	for _, identity := range identities {
		if open := videoID("open", identity); !listed[open] {
			t.Errorf("%s: open event's video %s not listed", identity, open)
		}
		if ended := videoID("ended", identity); listed[ended] {
			t.Errorf("%s: ended event's video %s listed", identity, ended)
		}
		id := videoID("cancel", identity)
		candidates = append(candidates, Video{ID: id, URL: videoURL(id)})
	}
	candidates = append(candidates, Video{ID: "stream-00", URL: videoURL("stream-00")})
	cancelled, err := store.cancelledVideos(context.Background(), candidates)
	if err != nil {
		t.Fatal(err)
	}
	for _, video := range candidates {
		if want := video.ID != "stream-00"; cancelled[video.ID] != want {
			t.Errorf("%s cancelled=%v, want %v", video.ID, cancelled[video.ID], want)
		}
	}
}

// A read stays valid until a broadcast state it trusts stops counting, when
// that comes before the projection's next transition; a state already stale
// does not shorten it.
func TestDashboardValidUntilEndsWithATrustedLiveState(t *testing.T) {
	now := time.Date(2026, 11, 2, 4, 0, 0, 0, time.UTC)
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339) }
	path := testutil.NewSnapshot(t)
	// On air since two hours ago, checked five minutes ago: the state counts
	// for two probe intervals (30 minutes by default) after its check, before
	// the stream's three-hour grace ends.
	testutil.Exec(t, path, `INSERT INTO videos (id,source,title,scheduled_at,url,kind,live_status,live_checked_at) VALUES ('onair','youtube','On air',?,?,'stream','live',?)`,
		at(-2*time.Hour), "https://www.youtube.com/watch?v=onair", at(-5*time.Minute))
	testutil.Exec(t, path, `INSERT INTO videos (id,source,title,scheduled_at,url,kind,live_status,live_checked_at) VALUES ('stale','youtube','Stale',?,?,'stream','upcoming',?)`,
		at(-2*time.Hour), "https://www.youtube.com/watch?v=stale", at(-40*time.Minute))
	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	dashboard, err := store.GetDashboard(context.Background(), 3, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := now.Add(25 * time.Minute); !dashboard.ValidUntil.Equal(want) {
		t.Fatalf("validUntil=%v, want %v (next transition %v)", dashboard.ValidUntil, want, dashboard.Projection.NextTransitionAt)
	}
}

// The board's video reads run on every snapshot read, so they look each
// video's events up through the identity indexes instead of walking the
// events for every video.
func TestDashboardVideoReadsLookEventsUpByIdentity(t *testing.T) {
	store, err := OpenReadOnly(testutil.NewSnapshot(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, tc := range []struct {
		name, statement string
		args            []any
	}{
		{"active videos", dashboardVideosSQL(), []any{"2026-11-02T04:00:00Z", "2026-11-02T03:50:00Z"}},
		{"cancelled videos", cancelledVideosSQL(2), []any{"AAAAAAAAAAA", "https://www.youtube.com/watch?v=AAAAAAAAAAA", "BBBBBBBBBBB", "https://youtu.be/BBBBBBBBBBB"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steps := queryPlan(t, store.DB(), tc.statement, tc.args...)
			for _, step := range steps {
				if strings.HasPrefix(step, "SCAN e ") || step == "SCAN e" || strings.HasPrefix(step, "SCAN es") || strings.HasPrefix(step, "SCAN events") {
					t.Errorf("plan walks the events: %s", strings.Join(steps, "; "))
					break
				}
			}
		})
	}
}
