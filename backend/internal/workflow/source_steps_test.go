package workflow

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/mediadownload"
	"github.com/yexca/kano-navi/backend/internal/source/history"
	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
	ytsource "github.com/yexca/kano-navi/backend/internal/source/youtube"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend/internal/testutil"
)

func TestWindowStepWithoutHistoryReturnsDependencyInsteadOfPanicking(t *testing.T) {
	for _, step := range []string{"x", "youtube"} {
		t.Run(step, func(t *testing.T) {
			steps := &SourceSteps{}
			window := history.Window{Method: history.MethodAPI, Mode: history.ModeCount, Count: 7}
			_, err := steps.Execute(context.Background(), StepInput{StepID: step, Window: &window})
			var stepErr *StepError
			if !errors.As(err, &stepErr) || stepErr.Code != "history_unavailable" || stepErr.Category != ErrDependency {
				t.Fatalf("err=%#v", err)
			}
		})
	}
}

func TestGoSourceWorkflowProducesPostsReservationsAndReadableMedia(t *testing.T) {
	p := filepath.Join(t.TempDir(), "data", "database", "snapshot.sqlite")
	s, err := sqlite.OpenInitialized(p, true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	testutil.Exec(t, p, `CREATE UNIQUE INDEX IF NOT EXISTS media_assets_source_url_idx ON media_assets(source_url)`)
	testutil.Exec(t, p, `CREATE UNIQUE INDEX IF NOT EXISTS media_links_owner_position_idx ON media_links(owner_type,owner_id,role,position)`)
	def := sqlite.WorkflowDefinition{ID: sqlite.AdhocFetchWorkflow, Steps: []string{"x", "youtube", "media"}}
	rt := xTestRT(func(r *http.Request) (*http.Response, error) {
		body, mime := "", "text/html"
		switch r.URL.Host {
		case "api.fxtwitter.com":
			body = `{"code":200,"results":[{"id":"111111111111","author":{"screen_name":"fixture"}}],"cursor":{"bottom":""}}`
			mime = "application/json"
		case "api.vxtwitter.com":
			body = `{"tweetID":"111111111111","text":"fixture post","date":"2026-01-02T00:00:00Z","mediaURLs":["https://pbs.twimg.com/fixture.png"]}`
			mime = "application/json"
		case "www.youtube.com":
			switch r.URL.Path {
			case "/feeds/videos.xml":
				body = `<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015"><entry><yt:videoId>AAAAAAAAAAA</yt:videoId><title>fixture stream</title><published>2026-01-02T00:00:00Z</published></entry></feed>`
			case "/channel/UCAAAAAAAAAAAAAAAAAAAAAA/streams":
				body = `"videoId":"AAAAAAAAAAA"`
			case "/watch":
				body = `var ytInitialPlayerResponse={"videoDetails":{"videoId":"AAAAAAAAAAA","title":"fixture stream","isUpcoming":true},"playabilityStatus":{"status":"OK"},"scheduledStartTime":"2026-01-04T00:00:00Z"};`
			default:
				t.Fatalf("unexpected YouTube path %s", r.URL.Path)
			}
		case "pbs.twimg.com", "i.ytimg.com":
			body = "\x89PNG\r\n\x1a\nsynthetic fixture"
			mime = "image/png"
		default:
			t.Fatalf("unexpected destination %s", r.URL.Host)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {mime}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	xopts := xsource.Options{Handles: []string{"fixture"}, BootstrapDays: 7, MaxDetailRequests: 1, DiscoveryLimit: 1}
	x := xsource.NewAdapter(xsource.NewClient(&http.Client{Transport: rt}), xopts)
	yt := ytsource.NewAdapter(ytsource.NewClient(&http.Client{Transport: rt}), ytsource.Options{ChannelID: "UCAAAAAAAAAAAAAAAAAAAAAA", MaxDetailRequests: 1})
	x.Now = func() time.Time { return time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC) }
	yt.Now = x.Now
	steps := &SourceSteps{Store: s, X: &XRecentExecutor{Store: s, Adapter: x, Options: xopts}, Youtube: yt, Media: mediadownload.NewClient(&http.Client{Transport: rt})}
	q := &Queue{Store: s, Executor: &Executor{Store: s, Steps: steps, SyntheticSteps: map[string]bool{"x": false, "youtube": false, "media": false}}}
	defer q.Close()
	run, err := q.SubmitDefinition(context.Background(), def, TriggerOperator, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := waitRun(t, s, run.ID, "succeeded", "failed", "partial")
	if done.Status != "succeeded" || len(done.Steps) != 3 || done.Steps[1].Capability != "youtube_recent" || done.Steps[2].Capability != "media_download" {
		t.Fatalf("run=%#v", done)
	}
	posts, err := s.ListAdminPosts(context.Background(), sqlite.AdminPostFilter{Page: 1, PageSize: 10})
	if err != nil || len(posts.Items) != 1 {
		t.Fatalf("posts=%#v err=%v", posts, err)
	}
	state, err := s.YoutubeRecentState(context.Background(), yt.Options.ChannelID)
	if err != nil || state.Videos["AAAAAAAAAAA"].ScheduledAt != "2026-01-04T00:00:00Z" {
		t.Fatalf("videos=%#v err=%v", state, err)
	}
	events, err := s.ListAdminEvents(context.Background(), sqlite.AdminEventFilter{Page: 1, PageSize: 10})
	if err != nil || len(events.Items) != 1 {
		t.Fatalf("events=%#v err=%v", events, err)
	}
	latest, err := s.GetLatestSync(context.Background())
	if err != nil || latest == nil || latest.Status != "success" {
		t.Fatalf("latest=%#v err=%v", latest, err)
	}
	pending, err := s.PendingMedia(context.Background(), 20)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending=%#v err=%v", pending, err)
	}
	if _, err = s.GetDashboardRevision(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestYoutubeRecentFeedOutageIsRetryableDependencyFailure(t *testing.T) {
	for _, tc := range []struct {
		name        string
		feed, feeds int
		category    ErrorCategory
	}{
		{"feed 5xx is retried", 500, 3, ErrDependency},
		{"feed 404", 404, 1, ErrDependency},
		{"feed rate limit", 429, 1, ErrRateLimited},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := sqlite.OpenInitialized(filepath.Join(t.TempDir(), "data", "database", "snapshot.sqlite"), true, true)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			// Every public page is down and nothing is stored to recheck, so
			// the run learns nothing: it fails as the feed's retryable outage.
			requests := map[string]int{}
			c := ytsource.NewClient(&http.Client{Transport: xTestRT(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "www.youtube.com" {
					t.Fatalf("unexpected request %s", r.URL)
				}
				requests[r.URL.Path]++
				status := 500
				if r.URL.Path == "/feeds/videos.xml" {
					status = tc.feed
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
			})})
			c.FeedRetryDelays = []time.Duration{time.Millisecond, time.Millisecond}
			steps := &SourceSteps{Store: s, Youtube: ytsource.NewAdapter(c, ytsource.Options{ChannelID: "UCAAAAAAAAAAAAAAAAAAAAAA", MaxDetailRequests: 1})}
			_, err = steps.youtubeRecent(context.Background(), StepInput{})
			var se *StepError
			if !errors.As(err, &se) || se.Category != tc.category || !se.Retryable || requests["/feeds/videos.xml"] != tc.feeds || requests["/channel/UCAAAAAAAAAAAAAAAAAAAAAA/videos"] != 1 {
				t.Fatalf("err=%#v requests=%v", err, requests)
			}
		})
	}
}

func TestYoutubeRecentFeedFailureFallsBackToTheUploadsTab(t *testing.T) {
	p := filepath.Join(t.TempDir(), "data", "database", "snapshot.sqlite")
	s, err := sqlite.OpenInitialized(p, true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := ytsource.NewClient(&http.Client{Transport: xTestRT(func(r *http.Request) (*http.Response, error) {
		status, body := 200, ""
		switch r.URL.Path {
		case "/feeds/videos.xml":
			status = 404
		case "/channel/UCAAAAAAAAAAAAAAAAAAAAAA/streams":
		case "/channel/UCAAAAAAAAAAAAAAAAAAAAAA/videos":
			body = `{"videoId":"AAAAAAAAAAA"}`
		case "/watch":
			body = `var ytInitialPlayerResponse={"videoDetails":{"videoId":"AAAAAAAAAAA","title":"fixture upload","isLiveContent":false,"lengthSeconds":"60"},"playabilityStatus":{"status":"OK"},"microformat":{"playerMicroformatRenderer":{"publishDate":"2026-01-02T01:00:00-08:00"}}};`
		default:
			t.Fatalf("unexpected request %s", r.URL)
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	yt := ytsource.NewAdapter(c, ytsource.Options{ChannelID: "UCAAAAAAAAAAAAAAAAAAAAAA", MaxDetailRequests: 1})
	yt.Now = func() time.Time { return time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC) }
	steps := &SourceSteps{Store: s, Youtube: yt}
	result, err := steps.youtubeRecent(context.Background(), StepInput{})
	if err != nil || result.Status != "partial" || result.Result["listing"] != ytsource.ListingVideosPage || result.Result["inspected"] != 1 || !slices.Equal(result.Result["errors"].([]string), []string{"youtube_feed_http_error"}) {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	state, err := s.YoutubeRecentState(context.Background(), yt.Options.ChannelID)
	if v := state.Videos["AAAAAAAAAAA"]; err != nil || v.Title != "fixture upload" || v.PublishedAt != "2026-01-02T09:00:00Z" {
		t.Fatalf("video=%#v err=%v", v, err)
	}
}

func TestYoutubeRecentFailureKeepsTheRotatedQueueOnly(t *testing.T) {
	s, err := sqlite.OpenInitialized(filepath.Join(t.TempDir(), "data", "database", "snapshot.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const channel = "UCAAAAAAAAAAAAAAAAAAAAAA"
	for _, statement := range []string{
		`INSERT INTO sync_state(source,account_id,cursor_id,cursor_time,last_success_at,metadata_json) VALUES('youtube','` + channel + `','CCCCCCCCCCC','2026-01-01T00:00:00Z','2026-01-01T00:00:00.000Z','{"inspected":4}')`,
		`INSERT INTO sync_state(source,account_id,metadata_json) VALUES('youtube-details','` + channel + `','{"queue":["AAAAAAAAAAA","BBBBBBBBBBB"]}')`,
	} {
		if _, err = s.DB().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	// Every page is down and the queue's head keeps failing.
	watched := []string{}
	c := ytsource.NewClient(&http.Client{Transport: xTestRT(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/watch" {
			watched = append(watched, r.URL.Query().Get("v"))
		}
		return &http.Response{StatusCode: 500, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})})
	c.FeedRetryDelays = []time.Duration{time.Millisecond, time.Millisecond}
	steps := &SourceSteps{Store: s, Youtube: ytsource.NewAdapter(c, ytsource.Options{ChannelID: channel, MaxDetailRequests: 1})}
	for range 2 {
		_, err = steps.youtubeRecent(context.Background(), StepInput{})
		var se *StepError
		if !errors.As(err, &se) || se.Category != ErrDependency {
			t.Fatalf("err=%#v", err)
		}
	}
	// Each run asked for the next ID, and the cursor and its metadata stayed.
	if strings.Join(watched, ",") != "AAAAAAAAAAA,BBBBBBBBBBB" {
		t.Fatalf("watched=%v", watched)
	}
	var cursor string
	if err = s.DB().QueryRow(`SELECT cursor_id||'/'||last_success_at||'/'||metadata_json FROM sync_state WHERE source='youtube'`).Scan(&cursor); err != nil || cursor != `CCCCCCCCCCC/2026-01-01T00:00:00.000Z/{"inspected":4}` {
		t.Fatalf("cursor=%s err=%v", cursor, err)
	}
	var videos int
	if err = s.DB().QueryRow(`SELECT COUNT(*) FROM videos`).Scan(&videos); err != nil || videos != 0 {
		t.Fatalf("videos=%d err=%v", videos, err)
	}
}

func TestTallyCodesListsEachCodeOnceWithItsCount(t *testing.T) {
	got := tallyCodes([]string{"youtube_http_error", "youtube_watch_details_incomplete", "youtube_http_error", "youtube_http_error"})
	if !slices.Equal(got, []string{"youtube_http_error ×3", "youtube_watch_details_incomplete"}) {
		t.Fatalf("tally=%v", got)
	}
}
