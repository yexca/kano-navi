package workflow

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	xsource "github.com/yexca/kano-navi/backend-go/internal/source/x"
	ytsource "github.com/yexca/kano-navi/backend-go/internal/source/youtube"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

// chainAPI answers every read with one new post, newer than the cursor.
type chainAPI struct{ reads int }

func (c *chainAPI) AccountPosts(_ context.Context, q xsource.APIQuery) (xsource.APIPage, error) {
	c.reads++
	at := time.Date(2026, 3, 2, c.reads, 0, 0, 0, time.UTC)
	// A Snowflake ID of that instant.
	id := strconv.FormatInt((at.UnixMilli()-1288834974657)<<22, 10)
	post := xsource.Post{ID: id, AccountHandle: q.Account, Type: "daily", Text: "from the api", PublishedAt: at.Format(time.RFC3339Nano), URL: "https://x.com/kano/status/" + id, Raw: map[string]any{"mediaURLs": []any{}}}
	return xsource.APIPage{Posts: []xsource.Post{post}, Read: 1}, nil
}

func TestXChainOpensTheAPIAfterTheGateAndKeepsItsState(t *testing.T) {
	s, err := sqlite.OpenInitialized(filepath.Join(t.TempDir(), "data", "database", "snapshot.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	timelineUp := false
	rt := xTestRT(func(r *http.Request) (*http.Response, error) {
		status, body := 503, "down"
		if r.URL.Host == "api.fxtwitter.com" && timelineUp {
			status, body = 200, `{"code":200,"results":[{"id":"1900000000000000999","text":"timeline","created_timestamp":1772420000,"author":{"screen_name":"kano"}}],"cursor":{"bottom":""}}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	opts := xsource.Options{Handles: []string{"kano"}, DiscoveryLimit: 5, MaxDetailRequests: 0, BootstrapDays: 30}
	a := xsource.NewAdapter(xsource.NewClient(&http.Client{Transport: rt}), opts)
	api := &chainAPI{}
	a.API = api
	now := time.Date(2026, 3, 2, 3, 0, 0, 0, time.UTC)
	a.Now = func() time.Time { return now }
	ex := &XRecentExecutor{Store: s, Adapter: a, Options: opts, Now: func() time.Time { return now }}
	chain := sqlite.DefaultSourceChain("x")
	chain.Methods = []sqlite.ChainMethod{{ID: "timeline", Enabled: true}, {ID: "api", Enabled: true}, {ID: "profile", Enabled: false}}
	chain.APIGate, chain.APIDailyCap = 2, 100
	run := func() AttemptResult {
		t.Helper()
		out, err := ex.Execute(ctx, StepInput{StepID: "x", Chain: &chain})
		if err != nil && api.reads == 0 && timelineUp {
			t.Fatal(err)
		}
		return out
	}
	// The first failure is below the gate: the chain has nothing left.
	run()
	if failures, _ := s.XTimelineFailures(ctx, "kano"); failures != 1 || api.reads != 0 {
		t.Fatalf("failures=%d reads=%d", failures, api.reads)
	}
	// The second consecutive failure opens the API in the same run.
	out := run()
	if failures, _ := s.XTimelineFailures(ctx, "kano"); failures != 2 || api.reads != 1 {
		t.Fatalf("failures=%d reads=%d", failures, api.reads)
	}
	if out.Result["apiPosts"] != 1 || out.Result["apiRead"] != 1 || strings.Join(out.Result["discovery"].([]string), ",") != "kano: api" ||
		strings.Join(out.Result["tried"].([]string), ",") != "kano: timeline: x_upstream_5xx,kano: api" {
		t.Fatalf("result=%v", out.Result)
	}
	if usage, _ := s.XAPIUsageOn(ctx, sqlite.JapanDay(now)); usage.Posts != 1 {
		t.Fatalf("usage=%+v", usage)
	}
	// At the cap the chain skips the API; with nothing else left the
	// account fails as before.
	chain.APIDailyCap = 5
	if err = s.AddXAPIUsage(ctx, sqlite.JapanDay(now), 2); err != nil {
		t.Fatal(err)
	}
	if _, err = ex.Execute(ctx, StepInput{StepID: "x", Chain: &chain}); err == nil || api.reads != 1 {
		t.Fatalf("capped run err=%v reads=%d", err, api.reads)
	}
	// A timeline that serves again starts the streak over.
	timelineUp = true
	out = run()
	if failures, _ := s.XTimelineFailures(ctx, "kano"); failures != 0 || api.reads != 1 || strings.Join(out.Result["discovery"].([]string), ",") != "kano: timeline" {
		t.Fatalf("failures=%d result=%v", failures, out.Result)
	}
}

// TestYoutubeChainWithoutTheAPIMakesNoDataAPICall gives the process a key and
// turns the chain's api method off: listing, details and the live probe use
// public pages only.
func TestYoutubeChainWithoutTheAPIMakesNoDataAPICall(t *testing.T) {
	s, err := sqlite.OpenInitialized(filepath.Join(t.TempDir(), "data", "database", "snapshot.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rt := xTestRT(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "www.youtube.com" {
			t.Errorf("requested %s%s", r.URL.Host, r.URL.Path)
		}
		body := ""
		if r.URL.Path == "/feeds/videos.xml" {
			body = `<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015"><entry><yt:videoId>CCCCCCCCCCC</yt:videoId><title>new upload</title><published>2026-01-02T23:00:00Z</published></entry></feed>`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	yt := ytsource.NewAdapter(ytsource.NewClient(&http.Client{Transport: rt}), ytsource.Options{ChannelID: "UCAAAAAAAAAAAAAAAAAAAAAA", MaxDetailRequests: 1, APIKey: "synthetic-api-key"})
	steps := &SourceSteps{Store: s, Youtube: yt}
	chain := sqlite.SourceChain{Methods: []sqlite.ChainMethod{{ID: "api", Enabled: false}, {ID: "rss", Enabled: true}, {ID: "videos_page", Enabled: true}}}
	out, err := steps.Execute(context.Background(), StepInput{StepID: "youtube", Chain: &chain})
	if err != nil || out.Result["listing"] != ytsource.ListingFeed || out.Result["apiRequests"] != 0 {
		t.Fatalf("result=%+v err=%v", out, err)
	}
	// The process's adapter keeps its key for runs whose chain enables it.
	if keyed, _ := steps.youtubeFor(nil); keyed.Options.APIKey == "" || yt.Options.APIKey == "" {
		t.Fatal("the shared adapter lost its key")
	}
	if _, err = steps.youtubeLive(context.Background(), StepInput{StepID: "live", Chain: &chain}); err != nil {
		t.Fatal(err)
	}
}
