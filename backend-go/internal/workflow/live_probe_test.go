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

func TestLiveProbeRunsOnItsIntervalThroughTheQueue(t *testing.T) {
	s, err := sqlite.OpenInitialized(filepath.Join(t.TempDir(), "data", "database", "snapshot.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	started := now.Add(-5 * time.Minute).Format(time.RFC3339)
	var requested []string
	rt := xTestRT(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "www.youtube.com" || r.URL.Path != "/watch" {
			t.Fatalf("probe requested %s%s", r.URL.Host, r.URL.Path)
		}
		requested = append(requested, r.URL.Query().Get("v"))
		body := `var ytInitialPlayerResponse={"videoDetails":{"videoId":"AAAAAAAAAAA","title":"synthetic stream","isLive":true,"isLiveContent":true},"playabilityStatus":{"status":"OK"},"microformat":{"playerMicroformatRenderer":{"liveBroadcastDetails":{"isLiveNow":true,"startTimestamp":"` + started + `"}}}};`
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	yt := ytsource.NewAdapter(ytsource.NewClient(&http.Client{Transport: rt}), ytsource.Options{ChannelID: "UCAAAAAAAAAAAAAAAAAAAAAA", MaxDetailRequests: 1})
	q := &Queue{Store: s, Executor: &Executor{Store: s, Steps: &SourceSteps{Store: s, Youtube: yt}, SyntheticSteps: map[string]bool{"live": false}}}
	defer q.Close()
	var runs []*Run
	probe := &LiveProbe{Store: s, Enabled: true, Now: func() time.Time { return now }, Submit: func(ctx context.Context) (bool, error) {
		run, err := q.SubmitDefinition(ctx, sqlite.WorkflowDefinition{ID: sqlite.AdhocLiveWorkflow, Steps: []string{"live"}}, TriggerScheduler, nil)
		if err != nil {
			return false, err
		}
		runs = append(runs, run)
		return true, nil
	}}

	// Nothing active: no run and no request.
	if accepted, err := probe.Tick(ctx); accepted || err != nil || len(requested) != 0 {
		t.Fatalf("idle accepted=%v err=%v requests=%v", accepted, err, requested)
	}
	reservation := ytsource.Video{ID: "AAAAAAAAAAA", Title: "synthetic stream", PublishedAt: now.Add(-24 * time.Hour).Format(time.RFC3339), ScheduledAt: now.Add(-10 * time.Minute).Format(time.RFC3339), URL: "https://www.youtube.com/watch?v=AAAAAAAAAAA", Kind: "UPCOMING LIVE", IsUpcoming: true}
	if err = s.WriteYoutubeRecent(ctx, "UCAAAAAAAAAAAAAAAAAAAAAA", ytsource.Result{Videos: []ytsource.Video{reservation}, NextCursor: "AAAAAAAAAAA", NextCursorTime: reservation.PublishedAt}); err != nil {
		t.Fatal(err)
	}
	if accepted, err := probe.Tick(ctx); !accepted || err != nil {
		t.Fatalf("due accepted=%v err=%v", accepted, err)
	}
	done := waitRun(t, s, runs[0].ID, "succeeded", "failed", "partial")
	if done.Status != "succeeded" || len(done.Steps) != 1 || done.Steps[0].Capability != "youtube_live" || len(requested) != 1 {
		t.Fatalf("run=%#v requests=%v", done, requested)
	}
	dashboard, err := s.GetDashboard(ctx, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(dashboard.Videos) != 1 || dashboard.Videos[0].LiveStatus == nil || *dashboard.Videos[0].LiveStatus != "live" || dashboard.Videos[0].IsUpcoming {
		t.Fatalf("videos=%#v", dashboard.Videos)
	}

	// The interval has not passed yet.
	if accepted, _ := probe.Tick(ctx); accepted {
		t.Fatal("probe ran again within its interval")
	}
	probe.Now = func() time.Time { return now.Add(31 * time.Minute) }
	if accepted, err := probe.Tick(ctx); !accepted || err != nil {
		t.Fatalf("next interval accepted=%v err=%v", accepted, err)
	}
	waitRun(t, s, runs[1].ID, "succeeded", "failed", "partial")

	// The operator can turn it off; timers being disabled stops it too.
	if err = s.SaveLiveProbeSettings(ctx, sqlite.LiveProbeSettings{Enabled: false, IntervalMinutes: 5}); err != nil {
		t.Fatal(err)
	}
	probe.Now = func() time.Time { return now.Add(2 * time.Hour) }
	if accepted, _ := probe.Tick(ctx); accepted {
		t.Fatal("disabled probe ran")
	}
	if err = s.SaveLiveProbeSettings(ctx, sqlite.LiveProbeSettings{Enabled: true, IntervalMinutes: 5}); err != nil {
		t.Fatal(err)
	}
	probe.Enabled = false
	if accepted, _ := probe.Tick(ctx); accepted {
		t.Fatal("probe ran with timers disabled")
	}
}
