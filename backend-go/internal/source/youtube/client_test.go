package youtube

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type fakeTransport func(*http.Request) (*http.Response, error)

func (f fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func TestWatchRequiresCompleteMatchingPlayer(t *testing.T) {
	for _, body := range []string{
		`<meta property="og:title" content="title">`,
		`var ytInitialPlayerResponse={"videoDetails":{"videoId":"BBBBBBBBBBB","title":"title"},"playabilityStatus":{"status":"OK"}};`,
		`var ytInitialPlayerResponse={"videoDetails":{"videoId":"AAAAAAAAAAA","title":"title","isUpcoming":true},"playabilityStatus":{"status":"OK"}};`,
		`var ytInitialPlayerResponse={"videoDetails":`,
	} {
		if _, err := ParseWatch([]byte(body), "AAAAAAAAAAA"); err == nil {
			t.Fatalf("accepted incomplete player: %s", body)
		}
	}
	v, err := ParseWatch([]byte(`var ytInitialPlayerResponse={"videoDetails":{"videoId":"AAAAAAAAAAA","title":"A } and escaped \" title"},"playabilityStatus":{"status":"OK"}};`), "AAAAAAAAAAA")
	if err != nil || v.Title != `A } and escaped " title` {
		t.Fatalf("video=%#v err=%v", v, err)
	}
	v, err = ParseWatch([]byte(`<meta content="reservation" property="og:title"><script>{"isUpcoming":true,"scheduledStartTime":"2026-01-03T02:00:00+09:00"}</script>`), "AAAAAAAAAAA")
	if err != nil || v.ScheduledAt != "2026-01-02T17:00:00Z" || !v.IsUpcoming {
		t.Fatalf("reservation=%#v err=%v", v, err)
	}
	encoded := `{"player_response":"{\"videoDetails\":{\"videoId\":\"AAAAAAAAAAA\",\"title\":\"ordinary\"},\"playabilityStatus\":{\"status\":\"OK\"}}"}`
	if _, err := ParseWatch([]byte(encoded), "AAAAAAAAAAA"); err != nil {
		t.Fatal(err)
	}
}
func TestClientRejectsRedirectOversizeAndInvalidIdentity(t *testing.T) {
	for _, tc := range []struct {
		status     int
		body, code string
	}{
		{302, "", "youtube_redirect_rejected"}, {429, "", "youtube_rate_limited"}, {200, strings.Repeat("x", MaxResponseBytes+1), "youtube_response_oversized"},
	} {
		c := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
			if r.URL.Host != "www.youtube.com" || r.URL.Scheme != "https" {
				t.Fatal("unexpected destination")
			}
			return response(tc.status, tc.body), nil
		})})
		_, err := c.Watch(context.Background(), "AAAAAAAAAAA")
		if err == nil || err.Error() != tc.code {
			t.Fatalf("err=%v expected=%s", err, tc.code)
		}
	}
	c := NewClient(&http.Client{Transport: fakeTransport(func(*http.Request) (*http.Response, error) { t.Fatal("invalid identity made request"); return nil, nil })})
	if _, err := c.Watch(context.Background(), "bad/path"); err == nil {
		t.Fatal("accepted invalid id")
	}
}
func TestRecentQueueRotatesFailureAndCompletesOrdinaryVideo(t *testing.T) {
	var requested []string
	c := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/feeds/videos.xml":
			return response(200, `<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015"><entry><yt:videoId>CCCCCCCCCCC</yt:videoId><title>Song &amp; cover</title><published>2026-01-02T00:00:00Z</published></entry></feed>`), nil
		case "/channel/UCAAAAAAAAAAAAAAAAAAAAAA/streams":
			return response(200, `"videoId":"AAAAAAAAAAA","videoId":"BBBBBBBBBBB"`), nil
		case "/watch":
			id := r.URL.Query().Get("v")
			requested = append(requested, id)
			if id == "AAAAAAAAAAA" {
				return response(404, ""), nil
			}
			return response(200, `var ytInitialPlayerResponse={"videoDetails":{"videoId":"BBBBBBBBBBB","title":"ordinary"},"playabilityStatus":{"status":"OK"}};`), nil
		}
		t.Fatalf("unexpected path %s", r.URL.Path)
		return nil, nil
	})})
	a := NewAdapter(c, Options{ChannelID: "UCAAAAAAAAAAAAAAAAAAAAAA", MaxDetailRequests: 1})
	a.Now = func() time.Time { return time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC) }
	first, err := a.FetchRecent(context.Background(), State{Queue: []string{"AAAAAAAAAAA", "BBBBBBBBBBB"}}, 7)
	if err != nil || len(first.Queue) != 2 || first.Queue[0] != "BBBBBBBBBBB" || first.Requested != 1 || len(first.Errors) != 1 {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	second, err := a.FetchRecent(context.Background(), State{Queue: first.Queue}, 7)
	if err != nil || second.Inspected != 1 || len(second.Queue) != 1 || second.Queue[0] != "AAAAAAAAAAA" || len(requested) != 2 || requested[1] != "BBBBBBBBBBB" {
		t.Fatalf("second=%#v requests=%v err=%v", second, requested, err)
	}
}
