package youtube

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// watchPage builds a synthetic watch page around one player response.
func watchPage(id, details, broadcast string) string {
	player := `{"videoDetails":{"videoId":"` + id + `","title":"synthetic stream",` + details + `},"playabilityStatus":{"status":"OK"}`
	if broadcast != "" {
		player += `,"microformat":{"playerMicroformatRenderer":{"liveBroadcastDetails":` + broadcast + `}}`
	}
	return `<html><script>var ytInitialPlayerResponse=` + player + `};</script></html>`
}

func TestWatchReportsTheBroadcastState(t *testing.T) {
	for _, tc := range []struct {
		name, details, broadcast string
		status, scheduled, ended string
		liveNow, upcoming        bool
	}{
		{
			name: "live now", details: `"isLive":true,"isLiveContent":true`,
			broadcast: `{"isLiveNow":true,"startTimestamp":"2026-01-03T12:04:30+00:00"}`,
			status:    LiveStatusLive, scheduled: "2026-01-03T12:04:30Z", liveNow: true,
		},
		{
			// The broadcast flag alone is enough when the player omits isLive.
			name: "broadcast flag only", details: `"isLiveContent":true`,
			broadcast: `{"isLiveNow":true,"startTimestamp":"2026-01-03T12:04:30+00:00"}`,
			status:    LiveStatusLive, scheduled: "2026-01-03T12:04:30Z", liveNow: true,
		},
		{
			name: "ended with end", details: `"isLiveContent":true`,
			broadcast: `{"isLiveNow":false,"startTimestamp":"2026-01-03T12:04:30+00:00","endTimestamp":"2026-01-03T14:00:00+00:00"}`,
			status:    LiveStatusEnded, scheduled: "2026-01-03T12:04:30Z", ended: "2026-01-03T14:00:00Z",
		},
		{
			// An end beats a stale live flag.
			name: "stale live flag", details: `"isLive":true,"isLiveContent":true`,
			broadcast: `{"isLiveNow":true,"startTimestamp":"2026-01-03T12:04:30+00:00","endTimestamp":"2026-01-03T14:00:00+00:00"}`,
			status:    LiveStatusEnded, scheduled: "2026-01-03T12:04:30Z", ended: "2026-01-03T14:00:00Z",
		},
		{
			name: "ended without end", details: `"isLiveContent":true`,
			broadcast: `{"isLiveNow":false,"startTimestamp":"2026-01-03T12:04:30+00:00"}`,
			status:    LiveStatusEnded, scheduled: "2026-01-03T12:04:30Z",
		},
		{
			name: "waiting room", details: `"isUpcoming":true,"isLiveContent":true`,
			broadcast: `{"isLiveNow":false,"startTimestamp":"2026-01-03T12:00:00+00:00"}`,
			status:    LiveStatusUpcoming, scheduled: "2026-01-03T12:00:00Z", upcoming: true,
		},
		{name: "ordinary upload", details: `"isLiveContent":false`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseWatch([]byte(watchPage("AAAAAAAAAAA", tc.details, tc.broadcast)), "AAAAAAAAAAA")
			if err != nil {
				t.Fatal(err)
			}
			if v.LiveStatus != tc.status || v.LiveNow != tc.liveNow || v.IsUpcoming != tc.upcoming || v.ScheduledAt != tc.scheduled || v.EndedAt != tc.ended {
				t.Fatalf("video=%#v", v)
			}
		})
	}
}

func TestLiveSignalsDoNotLoosenCompleteness(t *testing.T) {
	for name, body := range map[string]string{
		// A live page for another video is still rejected.
		"other video": watchPage("BBBBBBBBBBB", `"isLive":true`, `{"isLiveNow":true,"startTimestamp":"2026-01-03T12:04:30+00:00"}`),
		// An upcoming page still needs a start time.
		"upcoming without start": watchPage("AAAAAAAAAAA", `"isUpcoming":true`, `{"isLiveNow":false}`),
		// A live flag does not replace the title.
		"no title":        `var ytInitialPlayerResponse={"videoDetails":{"videoId":"AAAAAAAAAAA","isLive":true},"playabilityStatus":{"status":"OK"},"microformat":{"playerMicroformatRenderer":{"liveBroadcastDetails":{"isLiveNow":true,"startTimestamp":"2026-01-03T12:04:30+00:00"}}}};`,
		"malformed start": watchPage("AAAAAAAAAAA", `"isLive":true`, `{"isLiveNow":true,"startTimestamp":"soon"}`),
	} {
		if v, err := ParseWatch([]byte(body), "AAAAAAAAAAA"); err == nil {
			t.Fatalf("%s accepted: %#v", name, v)
		}
	}
}

func TestProbeRequestsOnlyBoundedWatchPages(t *testing.T) {
	ids := []string{"AAAAAAAAAAA", "BBBBBBBBBBB", "CCCCCCCCCCC", "DDDDDDDDDDD", "EEEEEEEEEEE", "FFFFFFFFFFF", "GGGGGGGGGGG"}
	var requested []string
	c := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "www.youtube.com" || r.URL.Path != "/watch" {
			t.Fatalf("probe requested %s%s", r.URL.Host, r.URL.Path)
		}
		id := r.URL.Query().Get("v")
		requested = append(requested, id)
		switch id {
		case "AAAAAAAAAAA":
			return response(200, watchPage(id, `"isLive":true,"isLiveContent":true`, `{"isLiveNow":true,"startTimestamp":"2026-01-03T12:04:30+00:00"}`)), nil
		case "BBBBBBBBBBB":
			return response(503, ""), nil
		case "CCCCCCCCCCC":
			return response(200, `<html>no player</html>`), nil
		}
		return response(200, watchPage(id, `"isLiveContent":true`, `{"isLiveNow":false,"startTimestamp":"2026-01-03T10:00:00+00:00","endTimestamp":"2026-01-03T11:00:00+00:00"}`)), nil
	})})
	a := NewAdapter(c, Options{ChannelID: "UCAAAAAAAAAAAAAAAAAAAAAA", MaxDetailRequests: 12})
	a.Now = func() time.Time { return time.Date(2026, 1, 3, 13, 0, 0, 0, time.UTC) }
	stored := []Video{}
	for _, id := range ids {
		stored = append(stored, Video{ID: id, Title: "stored", PublishedAt: "2026-01-02T00:00:00Z", ScheduledAt: "2026-01-03T12:00:00Z", ThumbnailURL: "https://i.ytimg.com/vi/" + id + "/hqdefault.jpg", IsUpcoming: true})
	}
	result, err := a.ProbeLive(context.Background(), stored)
	if err != nil {
		t.Fatal(err)
	}
	// Without a key the probe reads only watch pages.
	if strings.Join(requested, ",") != strings.Join(ids[:MaxLiveProbes], ",") || result.Requested != MaxLiveProbes || result.Inspected != 3 || result.APIRequests != 0 {
		t.Fatalf("requested=%v result=%#v", requested, result)
	}
	if strings.Join(result.Errors, ",") != "youtube_upstream_5xx,youtube_watch_details_incomplete" {
		t.Fatalf("errors=%v", result.Errors)
	}
	live := result.Videos[0]
	// The actual start replaces the reservation; stored fields fill the gaps.
	if live.ID != "AAAAAAAAAAA" || !live.LiveNow || live.IsUpcoming || live.ScheduledAt != "2026-01-03T12:04:30Z" || live.PublishedAt != "2026-01-02T00:00:00Z" || live.Kind != "LIVE ARCHIVE" || live.ThumbnailURL == "" {
		t.Fatalf("live=%#v", live)
	}
	if ended := result.Videos[1]; ended.LiveStatus != LiveStatusEnded || ended.EndedAt != "2026-01-03T11:00:00Z" || ended.IsUpcoming {
		t.Fatalf("ended=%#v", ended)
	}
}
