package workflow

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/source/history"
	ytsource "github.com/yexca/kano-navi/backend-go/internal/source/youtube"
)

// A YouTube API fetch checks the details of the videos it lists through the
// recent step's rules and upsert: lengths, stream verdicts from the format
// playlists, reservation events, and manual locks that hold.
func TestYoutubeAPIFetchWritesDetailsThroughTheRecentUpsert(t *testing.T) {
	const channel = "UCAAAAAAAAAAAAAAAAAAAAAA"
	playlistItem := func(id, title string) string {
		return `{"snippet":{"title":"` + title + `","publishedAt":"2026-01-02T00:00:00Z"},"contentDetails":{"videoId":"` + id + `","videoPublishedAt":"2026-01-02T00:00:00Z"},"status":{"privacyStatus":"public"}}`
	}
	ended := func(id, title string) string {
		return `{"id":"` + id + `","snippet":{"title":"` + title + `","liveBroadcastContent":"none"},"contentDetails":{"duration":"PT2H"},"liveStreamingDetails":{"scheduledStartTime":"2026-01-02T09:00:00Z","actualStartTime":"2026-01-02T09:00:10Z","actualEndTime":"2026-01-02T11:00:10Z"}}`
	}
	failDetails := false
	var watched []string
	client := ytsource.NewClient(&http.Client{Transport: xTestRT(func(r *http.Request) (*http.Response, error) {
		body, status := "", 200
		switch {
		case r.URL.Path == "/youtube/v3/playlistItems" && r.URL.Query().Get("playlistId") == "UU"+channel[2:]:
			body = `{"items":[` + playlistItem("SSSSSSSSSSS", "game night") + `,` + playlistItem("LLLLLLLLLLL", "locked stream") + `,` + playlistItem("OOOOOOOOOOO", "ordinary upload") + `]}`
		case r.URL.Path == "/youtube/v3/playlistItems" && r.URL.Query().Get("playlistId") == "UULV"+channel[2:]:
			body = `{"items":[` + playlistItem("SSSSSSSSSSS", "game night") + `,` + playlistItem("LLLLLLLLLLL", "locked stream") + `]}`
		case r.URL.Path == "/youtube/v3/playlistItems" && r.URL.Query().Get("playlistId") == "UULF"+channel[2:]:
			body = `{"items":[` + playlistItem("OOOOOOOOOOO", "ordinary upload") + `]}`
		case r.URL.Path == "/youtube/v3/videos" && failDetails:
			status, body = 403, `{}`
		case r.URL.Path == "/youtube/v3/videos":
			body = `{"items":[` + ended("SSSSSSSSSSS", "game night") + `,` + ended("LLLLLLLLLLL", "locked stream") + `,{"id":"OOOOOOOOOOO","snippet":{"title":"ordinary upload","liveBroadcastContent":"none"},"contentDetails":{"duration":"PT3M"}}]}`
		case r.URL.Path == "/watch":
			watched = append(watched, r.URL.Query().Get("v"))
			status = 500
		default:
			t.Fatalf("unexpected request %s%s", r.URL.Host, r.URL.Path)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})})
	adapter := ytsource.NewAdapter(client, ytsource.Options{ChannelID: channel, APIKey: "synthetic-api-key", MaxDetailRequests: 5})
	adapter.Now = func() time.Time { return time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC) }
	transport := &historyTransport{respond: func(*http.Request) (int, string) { return 500, "" }}
	source, store := newHistorySource(t, transport)
	source.Fetcher.Uploads = adapter
	db := store.DB()
	// An operator locked the reservation event of LLLLLLLLLLL before its
	// video was ever stored.
	if _, err := db.Exec(`INSERT INTO events(id,source,source_item_id,source_key,title,detail,starts_on,starts_at,timezone,time_precision,status,event_type,url,provenance,manual_locked,created_at,updated_at,raw_json) VALUES('youtube-LLLLLLLLLLL','youtube','LLLLLLLLLLL','reservation','operator title','YouTube Live','2026-01-02','2026-01-02T09:00:00Z','Asia/Tokyo','exact','scheduled','stream','https://www.youtube.com/watch?v=LLLLLLLLLLL','manual',1,'2026-01-01T00:00:00.000Z','2026-01-01T00:00:00.000Z','{}')`); err != nil {
		t.Fatal(err)
	}
	all := history.Window{Method: history.MethodAPI, Mode: history.ModeAll}
	out, err := (&SourceSteps{History: source}).Execute(context.Background(), StepInput{StepID: "youtube", Window: &all})
	// One listing page, one videos.list call and both format playlists.
	if err != nil || out.Status != "succeeded" || out.Result["new"] != 3 || out.Result["inspected"] != 3 || out.Result["apiRequests"] != 4 || len(watched) != 0 {
		t.Fatalf("out=%+v err=%v watched=%v", out, err, watched)
	}
	query := func(statement string) string {
		t.Helper()
		var value string
		if err := db.QueryRow(statement).Scan(&value); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
		return value
	}
	for statement, want := range map[string]string{
		`SELECT duration_seconds||'/'||live_status||'/'||is_premiere||'/'||kind||'/'||(live_checked_at IS NOT NULL) FROM videos WHERE id='SSSSSSSSSSS'`: "7200/ended/0/LIVE ARCHIVE/1",
		`SELECT event_type||'/'||status||'/'||ends_at FROM events WHERE id='youtube-SSSSSSSSSSS'`:                                                       "stream/ended/2026-01-02T11:00:10Z",
		`SELECT title||'/'||status FROM events WHERE id='youtube-LLLLLLLLLLL'`:                                                                          "operator title/scheduled",
		`SELECT duration_seconds||'/'||COUNT(*) FROM videos WHERE id='OOOOOOOOOOO'`:                                                                     "180/1",
		`SELECT COUNT(*) FROM events WHERE source_item_id='OOOOOOOOOOO'`:                                                                                "0",
	} {
		if got := query(statement); got != want {
			t.Fatalf("%s = %q, want %q", statement, got, want)
		}
	}
	// A failed details call keeps the listing and its checkpoint and leaves
	// the step partial.
	failDetails = true
	refresh := history.Window{Method: history.MethodAPI, Mode: history.ModeAll, Refresh: true}
	out, err = (&SourceSteps{History: source}).Execute(context.Background(), StepInput{StepID: "youtube", Window: &refresh})
	if err != nil || out.Status != "partial" || out.Result["stored"] != 3 || out.Result["inspected"] != 0 || out.Result["reachedEnd"] != true {
		t.Fatalf("failed details=%+v err=%v", out, err)
	}
	if errors := out.Result["errors"].([]string); strings.Join(errors, ",") != "youtube_api_forbidden" {
		t.Fatalf("errors=%v", errors)
	}
	if got := query(`SELECT COUNT(*) FROM sync_state WHERE source='youtube-window'`); got != "1" {
		t.Fatalf("checkpoints=%s", got)
	}
}
