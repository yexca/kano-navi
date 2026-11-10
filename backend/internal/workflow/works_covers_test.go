package workflow

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/mediadownload"
	bilisource "github.com/yexca/kano-navi/backend/internal/source/bilibili"
	"github.com/yexca/kano-navi/backend/internal/source/niconico"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend/internal/works"
)

const worksPNG = "\x89PNG\r\n\x1a\n"

// worksPlatforms answers the cover lookups and image downloads with
// synthetic data, counting the requests by host and path.
type worksPlatforms struct {
	mu       sync.Mutex
	requests map[string]int
	blocked  bool
}

func (p *worksPlatforms) RoundTrip(r *http.Request) (*http.Response, error) {
	p.mu.Lock()
	p.requests[r.URL.Host+r.URL.Path]++
	blocked := p.blocked
	p.mu.Unlock()
	reply := func(status int, contentType, body string) (*http.Response, error) {
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	switch r.URL.Host {
	case "i.ytimg.com":
		return reply(200, "image/png", worksPNG+r.URL.Path)
	case niconico.APIHost:
		id := strings.TrimPrefix(r.URL.Path, "/api/getthumbinfo/")
		if id == "sm2" {
			return reply(200, "text/xml", `<?xml version="1.0" encoding="UTF-8"?><nicovideo_thumb_response status="fail"><error><code>DELETED</code></error></nicovideo_thumb_response>`)
		}
		return reply(200, "text/xml", `<?xml version="1.0" encoding="UTF-8"?><nicovideo_thumb_response status="ok"><thumb><video_id>`+id+`</video_id><thumbnail_url>https://nicovideo.cdn.nimg.jp/thumbnails/1/1</thumbnail_url></thumb></nicovideo_thumb_response>`)
	case niconico.ThumbnailHost:
		// An old video has no large thumbnail.
		if strings.HasSuffix(r.URL.Path, ".L") {
			return reply(404, "text/plain", "")
		}
		return reply(200, "image/png", worksPNG+r.URL.Path)
	case "api.bilibili.com":
		if r.URL.Path == "/x/frontend/finger/spi" {
			return reply(200, "application/json", `{"code":0,"data":{"b_3":"0A1B2C3D-0A1B-0A1B-0A1B-0A1B2C3D4E5F12345infoc","b_4":"synthetic-buvid4=="}}`)
		}
		if blocked {
			return reply(412, "text/html", "<!DOCTYPE html>")
		}
		bvid := r.URL.Query().Get("bvid")
		return reply(200, "application/json", `{"code":0,"data":{"bvid":"`+bvid+`","aid":1,"title":"合成投稿","pubdate":1767258000,"pic":"http://i0.hdslb.com/bfs/archive/`+bvid+`.jpg","duration":200,"owner":{"mid":1}}}`)
	case "i0.hdslb.com":
		return reply(200, "image/png", worksPNG+r.URL.Path)
	}
	return reply(404, "text/plain", "")
}

func (p *worksPlatforms) count(key string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requests[key]
}

func TestMediaStepFetchesWorksCoversAndAsksDeletedVideosOnce(t *testing.T) {
	s, err := sqlite.OpenInitialized(filepath.Join(t.TempDir(), "data", "database", "snapshot.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	platforms := &worksPlatforms{requests: map[string]int{}}
	client := &http.Client{Transport: platforms}
	index := works.Index{UpdatedAt: "2026-01-02", Uploads: []works.Upload{
		{ID: "2026-01-01-aaaaaa", Date: "2026-01-01", Videos: []works.Video{{Platform: works.YouTube, ID: "aaaaaaaaaaa"}}},
		{ID: "2010-01-01-bbbbbb", Date: "2010-01-01", Videos: []works.Video{{Platform: works.Niconico, ID: "sm1"}}},
		// A deleted niconico video leaves the next platform to try.
		{ID: "2012-01-01-cccccc", Date: "2012-01-01", Videos: []works.Video{{Platform: works.Niconico, ID: "sm2"}, {Platform: works.Bilibili, ID: "BV1AAAAAAAAA"}}},
	}}
	bili := bilisource.NewAdapter(bilisource.NewClient(client), bilisource.Options{Account: bilisource.DefaultAccount, MinInterval: 3 * time.Hour, PageSize: 20})
	steps := &SourceSteps{Store: s, Media: mediadownload.NewClient(client), Niconico: niconico.NewClient(client), Bilibili: bili, Works: &index}
	run := func() AttemptResult {
		t.Helper()
		out, err := steps.Execute(context.Background(), StepInput{StepID: "media"})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	first := run()
	covers, _ := first.Result["works"].(map[string]any)
	if covers["queued"] != 1 || covers["resolved"] != 1 || covers["missing"] != 1 || first.Result["ready"] != 2 {
		t.Fatalf("first=%#v", first.Result)
	}
	// The large niconico size was missing, so the plain one was fetched.
	if platforms.count(niconico.ThumbnailHost+"/thumbnails/1/1.L") != 1 || platforms.count(niconico.ThumbnailHost+"/thumbnails/1/1") != 1 {
		t.Fatalf("niconico requests=%#v", platforms.requests)
	}
	second := run()
	covers, _ = second.Result["works"].(map[string]any)
	if covers["resolved"] != 1 || covers["covered"] != 2 || second.Result["ready"] != 1 {
		t.Fatalf("second=%#v", second.Result)
	}
	third := run()
	covers, _ = third.Result["works"].(map[string]any)
	if covers["covered"] != 3 || covers["lookups"] != 0 || third.Result["attempted"] != 0 {
		t.Fatalf("third=%#v", third.Result)
	}
	// The deleted video was asked once; covered videos are not asked again.
	if platforms.count(niconico.APIHost+"/api/getthumbinfo/sm2") != 1 || platforms.count(niconico.APIHost+"/api/getthumbinfo/sm1") != 1 {
		t.Fatalf("lookups=%#v", platforms.requests)
	}
	overlay, err := s.GetWorksOverlay(context.Background(), index)
	if err != nil || len(overlay.Uploads) != 3 {
		t.Fatalf("overlay=%#v err=%v", overlay, err)
	}
	for _, upload := range overlay.Uploads {
		if upload.Cover == nil || !strings.HasPrefix(upload.Cover.URL, "/media/") || len(upload.Videos) != 0 {
			t.Fatalf("upload=%#v", upload)
		}
	}
}

func TestWorksCoverLookupsKeepBilibilisBackoff(t *testing.T) {
	s, err := sqlite.OpenInitialized(filepath.Join(t.TempDir(), "data", "database", "snapshot.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	platforms := &worksPlatforms{requests: map[string]int{}, blocked: true}
	client := &http.Client{Transport: platforms}
	index := works.Index{UpdatedAt: "2026-01-02", Uploads: []works.Upload{
		{ID: "2026-01-01-aaaaaa", Date: "2026-01-01", Videos: []works.Video{{Platform: works.Bilibili, ID: "BV1AAAAAAAAA"}}},
		{ID: "2026-01-02-bbbbbb", Date: "2026-01-02", Videos: []works.Video{{Platform: works.Bilibili, ID: "BV1BBBBBBBBB"}}},
	}}
	bili := bilisource.NewAdapter(bilisource.NewClient(client), bilisource.Options{Account: bilisource.DefaultAccount, MinInterval: 3 * time.Hour, PageSize: 20})
	steps := &SourceSteps{Store: s, Media: mediadownload.NewClient(client), Bilibili: bili, Works: &index}
	out, err := steps.Execute(context.Background(), StepInput{StepID: "media"})
	if err != nil {
		t.Fatal(err)
	}
	covers, _ := out.Result["works"].(map[string]any)
	// The first captcha ends the lookups and starts the shared backoff.
	if covers["bilibili"] != "blocked" || covers["lookups"] != 0 || platforms.count("api.bilibili.com/x/web-interface/view") != 1 {
		t.Fatalf("out=%#v requests=%#v", out.Result, platforms.requests)
	}
	// The block is shared; the list read's own outcome is not the lookup's.
	state, err := s.BilibiliState(context.Background(), bilisource.DefaultAccount)
	if err != nil || !state.BlockedUntil.After(time.Now()) || !state.LastSuccessAt.IsZero() || state.LastError != "" {
		t.Fatalf("state=%#v err=%v", state, err)
	}
	if _, err = steps.Execute(context.Background(), StepInput{StepID: "media"}); err != nil {
		t.Fatal(err)
	}
	if platforms.count("api.bilibili.com/x/web-interface/view") != 1 {
		t.Fatalf("a blocked source was asked again: %#v", platforms.requests)
	}
}

func TestWorksCoverLookupsStopNiconicoAtItsFirstRateLimit(t *testing.T) {
	s, err := sqlite.OpenInitialized(filepath.Join(t.TempDir(), "data", "database", "snapshot.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var mu sync.Mutex
	asked := 0
	client := &http.Client{Transport: rateLimited(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		asked++
		mu.Unlock()
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	index := works.Index{UpdatedAt: "2026-01-03", Uploads: []works.Upload{
		{ID: "2026-01-01-aaaaaa", Date: "2026-01-01", Videos: []works.Video{{Platform: works.Niconico, ID: "sm1"}}},
		{ID: "2026-01-02-bbbbbb", Date: "2026-01-02", Videos: []works.Video{{Platform: works.Niconico, ID: "sm2"}}},
		{ID: "2026-01-03-cccccc", Date: "2026-01-03", Videos: []works.Video{{Platform: works.Niconico, ID: "sm3"}}},
	}}
	steps := &SourceSteps{Store: s, Niconico: niconico.NewClient(client), Works: &index}
	out, err := steps.workCovers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// The 429 is recorded as a failed lookup and the rest wait for a later run.
	if asked != 1 || out.Lookups != 1 || out.Failed != 1 {
		t.Fatalf("asked=%d out=%+v", asked, out)
	}
	report, err := s.WorkCoverReport(context.Background(), index, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	waiting := 0
	for _, upload := range report.Uploads {
		if video := upload.Videos[0]; video.Code != nil && *video.Code == niconico.CodeRateLimited {
			waiting++
		}
	}
	if waiting != 1 {
		t.Fatalf("report=%#v", report)
	}
}

type rateLimited func(*http.Request) (*http.Response, error)

func (f rateLimited) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
