package workflow

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ytsource "github.com/yexca/kano-navi/backend-go/internal/source/youtube"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

func TestLiveProbeFollowsTheStartThroughTheQueue(t *testing.T) {
	s, err := sqlite.OpenInitialized(filepath.Join(t.TempDir(), "data", "database", "snapshot.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	// Stored checks carry the real time, so the probe's clock starts there and
	// only moves forward.
	now := time.Now().UTC()
	clock := now
	scheduled := now.Add(-time.Minute).Format(time.RFC3339)
	onAir := false
	var requested []string
	rt := xTestRT(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "www.youtube.com" || r.URL.Path != "/watch" {
			t.Fatalf("probe requested %s%s", r.URL.Host, r.URL.Path)
		}
		requested = append(requested, r.URL.Query().Get("v"))
		state := `"isUpcoming":true,"isLiveContent":true},"playabilityStatus":{"status":"OK"},"microformat":{"playerMicroformatRenderer":{"liveBroadcastDetails":{"isLiveNow":false,"startTimestamp":"` + scheduled + `"}}}`
		if onAir {
			state = `"isLive":true,"isLiveContent":true},"playabilityStatus":{"status":"OK"},"microformat":{"playerMicroformatRenderer":{"liveBroadcastDetails":{"isLiveNow":true,"startTimestamp":"` + scheduled + `"}}}`
		}
		body := `var ytInitialPlayerResponse={"videoDetails":{"videoId":"AAAAAAAAAAA","title":"synthetic stream",` + state + `};`
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	yt := ytsource.NewAdapter(ytsource.NewClient(&http.Client{Transport: rt}), ytsource.Options{ChannelID: "UCAAAAAAAAAAAAAAAAAAAAAA", MaxDetailRequests: 1})
	yt.Now = func() time.Time { return clock }
	q := &Queue{Store: s, Executor: &Executor{Store: s, Steps: &SourceSteps{Store: s, Youtube: yt}, SyntheticSteps: map[string]bool{"live": false}}}
	defer q.Close()
	var runs []*Run
	probe := &LiveProbe{Store: s, Enabled: true, Now: func() time.Time { return clock }, Submit: func(ctx context.Context) (bool, error) {
		run, err := q.SubmitDefinition(ctx, sqlite.WorkflowDefinition{ID: sqlite.AdhocLiveWorkflow, Steps: []string{"live"}}, TriggerScheduler, nil)
		if err != nil {
			return false, err
		}
		runs = append(runs, run)
		return true, nil
	}}
	at := func(offset time.Duration) bool {
		t.Helper()
		clock = now.Add(offset)
		accepted, err := probe.Tick(ctx)
		if err != nil {
			t.Fatalf("tick at %s: %v", offset, err)
		}
		if accepted {
			done := waitRun(t, s, runs[len(runs)-1].ID, "succeeded", "failed", "partial")
			if done.Status != "succeeded" || len(done.Steps) != 1 || done.Steps[0].Capability != "youtube_live" {
				t.Fatalf("run at %s=%#v", offset, done)
			}
		}
		return accepted
	}
	status := func() string {
		t.Helper()
		dashboard, err := s.GetDashboard(ctx, 3, clock)
		if err != nil || len(dashboard.Videos) != 1 || dashboard.Videos[0].LiveStatus == nil {
			t.Fatalf("videos=%#v err=%v", dashboard.Videos, err)
		}
		return *dashboard.Videos[0].LiveStatus
	}

	// Nothing active: no run and no request.
	if at(0) || len(requested) != 0 {
		t.Fatalf("idle requests=%v", requested)
	}
	// A feed entry whose start has just passed is checked at once.
	reservation := ytsource.Video{ID: "AAAAAAAAAAA", Title: "synthetic stream", PublishedAt: now.Add(-24 * time.Hour).Format(time.RFC3339), ScheduledAt: scheduled, URL: "https://www.youtube.com/watch?v=AAAAAAAAAAA", Kind: "UPCOMING LIVE", IsUpcoming: true, FeedOnly: true}
	if err = s.WriteYoutubeRecent(ctx, "UCAAAAAAAAAAAAAAAAAAAAAA", ytsource.Result{Videos: []ytsource.Video{reservation}, NextCursor: "AAAAAAAAAAA", NextCursorTime: reservation.PublishedAt}); err != nil {
		t.Fatal(err)
	}
	if !at(0) || len(requested) != 1 || status() != "upcoming" {
		t.Fatalf("first check requests=%v", requested)
	}
	// Still waiting: the next check comes a starting interval later, not a
	// whole operator interval.
	if at(time.Minute) {
		t.Fatal("probe ran again within the starting interval")
	}
	onAir = true
	if !at(3*time.Minute+5*time.Second) || len(requested) != 2 || status() != "live" {
		t.Fatalf("start requests=%v", requested)
	}
	// On air, the operator interval (15 minutes by default) paces it.
	if at(10 * time.Minute) {
		t.Fatal("probe ran again within its interval while live")
	}
	if !at(16*time.Minute) || len(requested) != 3 {
		t.Fatalf("live requests=%v", requested)
	}

	// The operator can turn it off; timers being disabled stops it too.
	if err = s.SaveLiveProbeSettings(ctx, sqlite.LiveProbeSettings{Enabled: false, IntervalMinutes: 5}); err != nil {
		t.Fatal(err)
	}
	if at(2 * time.Hour) {
		t.Fatal("disabled probe ran")
	}
	if err = s.SaveLiveProbeSettings(ctx, sqlite.LiveProbeSettings{Enabled: true, IntervalMinutes: 5}); err != nil {
		t.Fatal(err)
	}
	probe.Enabled = false
	if at(2 * time.Hour) {
		t.Fatal("probe ran with timers disabled")
	}
}

func TestLiveProbeAsksTheDataAPIWhenAKeyIsSet(t *testing.T) {
	s, err := sqlite.OpenInitialized(filepath.Join(t.TempDir(), "data", "database", "snapshot.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	// A reservation only the feed has named, and a stream a detail check
	// already called a stream while it was on air.
	reservation := ytsource.Video{ID: "AAAAAAAAAAA", Title: "synthetic stream", PublishedAt: now.Add(-24 * time.Hour).Format(time.RFC3339), ScheduledAt: now.Add(-time.Minute).Format(time.RFC3339), URL: "https://www.youtube.com/watch?v=AAAAAAAAAAA", Kind: "UPCOMING LIVE", IsUpcoming: true, FeedOnly: true}
	stream := ytsource.Video{ID: "EEEEEEEEEEE", Title: "synthetic stream", PublishedAt: now.Add(-24 * time.Hour).Format(time.RFC3339), ScheduledAt: now.Add(-2 * time.Hour).Format(time.RFC3339), URL: "https://www.youtube.com/watch?v=EEEEEEEEEEE", Kind: "LIVE ARCHIVE", LiveNow: true, LiveStatus: ytsource.LiveStatusLive, PremiereKnown: true}
	if err = s.WriteYoutubeRecent(ctx, "UCAAAAAAAAAAAAAAAAAAAAAA", ytsource.Result{Videos: []ytsource.Video{reservation, stream}, NextCursor: "AAAAAAAAAAA", NextCursorTime: reservation.PublishedAt}); err != nil {
		t.Fatal(err)
	}
	clock := now.Add(16 * time.Minute)
	end := now.Add(10 * time.Minute).Format(time.RFC3339)
	rt := xTestRT(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "www.googleapis.com" || r.URL.Path != "/youtube/v3/videos" || r.Header.Get("X-Goog-Api-Key") != "synthetic-api-key" || strings.Contains(r.URL.String(), "synthetic-api-key") {
			t.Fatalf("probe requested %s%s", r.URL.Host, r.URL.Path)
		}
		body := `{"items":[{"id":"AAAAAAAAAAA","snippet":{"title":"synthetic stream","liveBroadcastContent":"live"},"contentDetails":{"duration":"P0D"},"liveStreamingDetails":{"actualStartTime":"` + reservation.ScheduledAt + `"}},` +
			`{"id":"EEEEEEEEEEE","snippet":{"title":"synthetic stream","liveBroadcastContent":"none"},"contentDetails":{"duration":"PT2H10M"},"liveStreamingDetails":{"actualStartTime":"` + stream.ScheduledAt + `","actualEndTime":"` + end + `"}}]}`
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	yt := ytsource.NewAdapter(ytsource.NewClient(&http.Client{Transport: rt}), ytsource.Options{ChannelID: "UCAAAAAAAAAAAAAAAAAAAAAA", MaxDetailRequests: 1, APIKey: "synthetic-api-key"})
	yt.Now = func() time.Time { return clock }
	steps := &SourceSteps{Store: s, Youtube: yt}
	got, err := steps.youtubeLive(ctx)
	if err != nil || got.Status != "succeeded" {
		t.Fatalf("result=%#v err=%v", got, err)
	}
	// The stored stream verdict settles the ended broadcast without its page.
	if got.Result["apiRequests"] != 1 || got.Result["requested"] != 0 || got.Result["inspected"] != 2 || got.Result["live"] != 1 || got.Result["changed"] != true {
		t.Fatalf("counts=%v", got.Result)
	}
	// The ended stream leaves the plan; the live one stays, in its live phase,
	// with the API's start and the stream verdict.
	targets, err := s.LiveProbeTargets(ctx, clock, sqlite.LiveProbeSettings{Enabled: true, IntervalMinutes: 15})
	if err != nil || len(targets) != 1 || targets[0].Video.ID != "AAAAAAAAAAA" || targets[0].Phase != sqlite.LivePhaseLive || !targets[0].Video.PremiereKnown || targets[0].Video.Premiere {
		t.Fatalf("targets=%#v err=%v", targets, err)
	}
	dashboard, err := s.GetDashboard(ctx, 3, clock)
	if err != nil || len(dashboard.Events) != 2 {
		t.Fatalf("events=%#v err=%v", dashboard.Events, err)
	}
	// The live event has no end; the ended one has the API's.
	for _, e := range dashboard.Events {
		if e.LiveStatus == nil || e.EndsAt == nil != (*e.LiveStatus == ytsource.LiveStatusLive) {
			t.Fatalf("event=%#v", e)
		}
	}
}

// Status is read by console requests while the process starts and stops the
// probe; run with -race.
func TestLiveProbeStatusIsSafeDuringStartAndStop(t *testing.T) {
	probe := &LiveProbe{Enabled: true, TickInterval: time.Hour}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			_ = probe.Status()
		}
	}()
	for range 50 {
		probe.Start()
		probe.Stop()
	}
	<-done
	probe.Start()
	if probe.Status()["running"] != true {
		t.Fatal("a started probe reports not running")
	}
	probe.Stop()
	if probe.Status()["running"] != false {
		t.Fatal("a stopped probe reports running")
	}
}
