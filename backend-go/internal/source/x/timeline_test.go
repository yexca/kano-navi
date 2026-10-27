package x

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

var timelineNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// statusAt builds a synthetic Snowflake ID for an instant.
func statusAt(t time.Time) string {
	return strconv.FormatInt((t.UnixMilli()-1288834974657)<<22, 10)
}

// timelineBody is one synthetic FxTwitter page; each status is "id@author".
func timelineBody(next string, statuses ...string) string {
	results := []map[string]any{}
	for _, s := range statuses {
		id, author, _ := strings.Cut(s, "@")
		results = append(results, map[string]any{"id": id, "author": map[string]any{"screen_name": author}})
	}
	body, _ := json.Marshal(map[string]any{"code": 200, "results": results, "cursor": map[string]any{"bottom": next}})
	return string(body)
}

type timelineServer struct {
	pages map[string]*http.Response
	// bodies answer every read of a cursor, for runs that read a page again.
	bodies   map[string]string
	cursors  []string
	profiles int
}

func (s *timelineServer) adapter(opts Options) *Adapter {
	rt := fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case timelineHost:
			if r.URL.Path != "/2/profile/kano/statuses" || r.URL.Query().Get("count") != "20" {
				return response(400, "unexpected"), nil
			}
			cursor := r.URL.Query().Get("cursor")
			s.cursors = append(s.cursors, cursor)
			if page, ok := s.pages[cursor]; ok {
				return page, nil
			}
			if body, ok := s.bodies[cursor]; ok {
				return response(200, body), nil
			}
			return response(404, `{"code":404}`), nil
		case profileHost:
			s.profiles++
			return response(200, `<a href="/kano/status/`+statusAt(timelineNow.Add(-time.Hour))+`">a</a>`), nil
		}
		return response(500, "unexpected"), nil
	})
	a := NewAdapter(NewClient(&http.Client{Transport: rt}), opts)
	a.Now = func() time.Time { return timelineNow }
	return a
}

func TestTimelinePagesBackToStoredCursorAndSkipsReposts(t *testing.T) {
	stored := statusAt(timelineNow.Add(-72 * time.Hour))
	newer := []string{
		statusAt(timelineNow.Add(-1 * time.Hour)),
		statusAt(timelineNow.Add(-20 * time.Hour)),
		statusAt(timelineNow.Add(-30 * time.Hour)),
	}
	older := statusAt(timelineNow.Add(-96 * time.Hour))
	s := &timelineServer{pages: map[string]*http.Response{
		"":       response(200, timelineBody("page-2", newer[0]+"@kano", statusAt(timelineNow.Add(-2*time.Hour))+"@other", newer[1]+"@Kano")),
		"page-2": response(200, timelineBody("page-3", newer[2]+"@kano", stored+"@kano", older+"@kano")),
	}}
	// MaxDetailRequests 0 returns the admitted queue without detail requests.
	a := s.adapter(Options{BootstrapDays: 7, DiscoveryLimit: 50, MaxDetailRequests: 0, KnownIDs: map[string]bool{stored: true}, CursorID: stored})
	got, err := a.FetchRecent(context.Background(), "kano", a.Options)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(s.cursors, ",") != ",page-2" || s.profiles != 0 || got.Discovery != DiscoveryTimeline {
		t.Fatalf("cursors=%q profiles=%d discovery=%s", s.cursors, s.profiles, got.Discovery)
	}
	if strings.Join(got.PendingIDs, ",") != strings.Join(append(newer, older), ",") || got.DiscoveredCount != 5 {
		t.Fatalf("queue=%v discovered=%d", got.PendingIDs, got.DiscoveredCount)
	}
}

func TestTimelineStopsAtCursorOrCutoffWithoutKnownPosts(t *testing.T) {
	recent := statusAt(timelineNow.Add(-1 * time.Hour))
	for name, tc := range map[string]struct {
		last   time.Duration
		cursor string
		want   string
	}{
		// An unstored post older than the cursor ends the gap.
		"cursor": {last: -48 * time.Hour, cursor: statusAt(timelineNow.Add(-24 * time.Hour)), want: recent + "," + statusAt(timelineNow.Add(-48*time.Hour))},
		// Without a cursor the bootstrap window ends paging, and its older
		// posts are not admitted.
		"cutoff": {last: -8 * 24 * time.Hour, want: recent},
	} {
		t.Run(name, func(t *testing.T) {
			s := &timelineServer{pages: map[string]*http.Response{
				"": response(200, timelineBody("page-2", recent+"@kano", statusAt(timelineNow.Add(tc.last))+"@kano")),
			}}
			a := s.adapter(Options{BootstrapDays: 7, DiscoveryLimit: 50, CursorID: tc.cursor})
			got, err := a.FetchRecent(context.Background(), "kano", a.Options)
			if err != nil {
				t.Fatal(err)
			}
			if len(s.cursors) != 1 || strings.Join(got.PendingIDs, ",") != tc.want {
				t.Fatalf("cursors=%q queue=%v", s.cursors, got.PendingIDs)
			}
		})
	}
}

func TestTimelinePageBudgetFollowsDiscoveryLimit(t *testing.T) {
	pages := map[string]*http.Response{}
	for i := 0; i < 5; i++ {
		cursor := ""
		if i > 0 {
			cursor = "page-" + strconv.Itoa(i)
		}
		pages[cursor] = response(200, timelineBody("page-"+strconv.Itoa(i+1), statusAt(timelineNow.Add(-time.Duration(i+1)*time.Hour))+"@kano"))
	}
	s := &timelineServer{pages: pages}
	// 40 IDs allow two pages of 20.
	a := s.adapter(Options{BootstrapDays: 7, DiscoveryLimit: 40})
	got, err := a.FetchRecent(context.Background(), "kano", a.Options)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(s.cursors, ",") != ",page-1" || got.DiscoveredCount != 2 {
		t.Fatalf("cursors=%q discovered=%d", s.cursors, got.DiscoveredCount)
	}
}

func TestTimelineFailureFallsBackToProfilePage(t *testing.T) {
	for name, first := range map[string]*http.Response{
		"server error": response(503, "unavailable"),
		"rate limited": response(429, "slow down"),
		"invalid json": response(200, "<html></html>"),
		"error code":   response(200, `{"code":404,"message":"User not found"}`),
		"only reposts": response(200, timelineBody("", statusAt(timelineNow.Add(-time.Hour))+"@other")),
	} {
		t.Run(name, func(t *testing.T) {
			s := &timelineServer{pages: map[string]*http.Response{"": first}}
			a := s.adapter(Options{BootstrapDays: 7, DiscoveryLimit: 50})
			got, err := a.FetchRecent(context.Background(), "kano", a.Options)
			if err != nil {
				t.Fatal(err)
			}
			if s.profiles != 1 || got.Discovery != DiscoveryProfile || got.DiscoveredCount != 1 {
				t.Fatalf("profiles=%d discovery=%s discovered=%d", s.profiles, got.Discovery, got.DiscoveredCount)
			}
		})
	}
}

func TestTimelineLaterPageFailureKeepsNewerPages(t *testing.T) {
	newest := statusAt(timelineNow.Add(-time.Hour))
	s := &timelineServer{pages: map[string]*http.Response{
		"":       response(200, timelineBody("page-2", newest+"@kano")),
		"page-2": response(502, "bad gateway"),
	}}
	a := s.adapter(Options{BootstrapDays: 7, DiscoveryLimit: 50})
	got, err := a.FetchRecent(context.Background(), "kano", a.Options)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.cursors) != 2 || s.profiles != 0 || got.Discovery != DiscoveryTimeline || strings.Join(got.PendingIDs, ",") != newest {
		t.Fatalf("cursors=%q profiles=%d result=%#v", s.cursors, s.profiles, got)
	}
}

func TestTimelineStatusesNormalizeOwnPostsFromThePage(t *testing.T) {
	newest := statusAt(timelineNow.Add(-time.Hour))
	quoting := statusAt(timelineNow.Add(-2 * time.Hour))
	repost := statusAt(timelineNow.Add(-3 * time.Hour))
	empty := statusAt(timelineNow.Add(-4 * time.Hour))
	body := strings.NewReplacer("NEWEST", newest, "QUOTING", quoting, "REPOST", repost, "EMPTY", empty, "EPOCH", strconv.FormatInt(timelineNow.Add(-time.Hour).Unix(), 10)).Replace(`{"code":200,"cursor":{"top":"top","bottom":"page-2"},"results":[
		{"type":"status","id":"NEWEST","url":"https://x.com/kano/status/NEWEST","text":"今週の配信予定","created_at":"Sat Oct 24 11:00:00 +0000 2026","created_timestamp":EPOCH,"likes":5,"reposts":2,"replies":1,
			"author":{"screen_name":"Kano","name":"Kano","description":"the whole profile repeats on every status"},
			"media":{"all":[{"type":"photo","url":"https://pbs.twimg.com/media/a.jpg"},{"type":"video","url":"https://video.twimg.com/v.mp4","thumbnail_url":"https://pbs.twimg.com/thumb/v.jpg"},{"type":"mosaic_photo","url":"https://pbs.twimg.com/mosaic/m.jpg"}]}},
		{"type":"status","id":"QUOTING","text":"look","created_timestamp":EPOCH,"author":{"screen_name":"kano"},
			"quote":{"type":"status","id":"1","text":"quoted","author":{"screen_name":"other"},"media":{"photos":[{"type":"photo","url":"https://pbs.twimg.com/media/q.jpg"}]}}},
		{"type":"status","id":"REPOST","text":"another author","created_timestamp":EPOCH,"author":{"screen_name":"other"}},
		{"type":"status","id":"EMPTY","author":{"screen_name":"kano"}}]}`)
	requests := 0
	rt := fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Host != timelineHost || r.URL.Path != "/2/profile/kano/statuses" || r.URL.Query().Get("count") != "100" || r.URL.Query().Get("cursor") != "page-1" {
			return response(400, "unexpected"), nil
		}
		return response(200, body), nil
	})
	a := NewAdapter(NewClient(&http.Client{Transport: rt}), Options{})
	page, err := a.TimelineStatuses(context.Background(), "@kano", "page-1", 500)
	if err != nil || page.Next != "page-2" || len(page.Statuses) != 3 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	first := page.Statuses[0].Post
	if first == nil || first.Text != "今週の配信予定" || first.PublishedAt != "2026-10-24T11:00:00Z" || page.Statuses[0].PublishedAt != first.PublishedAt || first.Type != "notice" {
		t.Fatalf("first=%+v", first)
	}
	if first.URL != "https://x.com/kano/status/"+newest || first.Likes != 5 || first.Reposts != 2 || first.Replies != 1 || first.AuthorScreenName != "Kano" {
		t.Fatalf("first=%+v", first)
	}
	// Photos keep their image and videos give their thumbnail, in post order.
	if strings.Join(first.MediaURLs, ",") != "https://pbs.twimg.com/media/a.jpg,https://pbs.twimg.com/thumb/v.jpg" {
		t.Fatalf("media=%v", first.MediaURLs)
	}
	if author := first.Raw["author"].(map[string]any); author["description"] != nil || author["screen_name"] != "Kano" {
		t.Fatalf("raw author=%v", author)
	}
	if quote := page.Statuses[1].Post; quote == nil || strings.Join(quote.MediaURLs, ",") != "https://pbs.twimg.com/media/q.jpg" {
		t.Fatalf("quote=%+v", quote)
	}
	// An entry without content keeps its ID and Snowflake time for the window.
	if last := page.Statuses[2]; last.ID != empty || last.Post != nil || last.PublishedAt != "2026-10-24T08:00:00Z" {
		t.Fatalf("empty=%+v", last)
	}
	if _, err = a.TimelineStatuses(context.Background(), "not a handle", "", 20); err == nil || requests != 1 {
		t.Fatalf("invalid account err=%v requests=%d", err, requests)
	}
}

func TestIDAfterComparesDecimalIDs(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{{"1900000000000000002", "1900000000000000001", true}, {"999999999999", "1000000000000", false}, {"0123", "123", false}, {"124", "123", true}} {
		if got := idAfter(tc.a, tc.b); got != tc.want {
			t.Fatalf("idAfter(%s,%s)=%v", tc.a, tc.b, got)
		}
	}
}
