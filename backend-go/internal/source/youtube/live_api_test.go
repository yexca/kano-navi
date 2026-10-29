package youtube

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// probeAPIClient answers videos.list with items and watch pages with page,
// recording the IDs asked of each and checking the key stays in its header.
func probeAPIClient(t *testing.T, items func() *http.Response, page func(id string) *http.Response, apiIDs, watched *[]string) *Client {
	return NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Host == "www.googleapis.com" && r.URL.Path == "/youtube/v3/videos":
			if r.Header.Get("X-Goog-Api-Key") != syntheticKey || strings.Contains(r.URL.String(), syntheticKey) {
				t.Fatalf("key not confined to the header: %s", r.URL)
			}
			*apiIDs = append(*apiIDs, r.URL.Query().Get("id"))
			return items(), nil
		case r.URL.Host == "www.youtube.com" && r.URL.Path == "/watch":
			id := r.URL.Query().Get("v")
			*watched = append(*watched, id)
			return page(id), nil
		}
		t.Fatalf("probe requested %s%s", r.URL.Host, r.URL.Path)
		return nil, nil
	})})
}

func probeNow() time.Time { return time.Date(2026, 1, 3, 13, 0, 0, 0, time.UTC) }

func TestProbeAsksTheDataAPIBeforeAnyWatchPage(t *testing.T) {
	var apiIDs, watched []string
	broadcast := func(id, content, duration, details string) string {
		return `{"id":"` + id + `","snippet":{"title":"synthetic ` + id + `","liveBroadcastContent":"` + content + `","thumbnails":{"high":{"url":"https://i.ytimg.com/vi/` + id + `/hqdefault.jpg"}}},"contentDetails":{"duration":"` + duration + `"},"liveStreamingDetails":{` + details + `}}`
	}
	c := probeAPIClient(t, func() *http.Response {
		return response(200, apiItems(
			broadcast("LLLLLLLLLLL", "live", "P0D", `"scheduledStartTime":"2026-01-03T12:00:00Z","actualStartTime":"2026-01-03T12:04:30Z"`),
			broadcast("UUUUUUUUUUU", "upcoming", "P0D", `"scheduledStartTime":"2026-01-03T13:10:00Z"`),
			// Classified by the stored verdict, so no page is needed.
			broadcast("EEEEEEEEEEE", "none", "PT1H55M30S", `"scheduledStartTime":"2026-01-03T10:00:00Z","actualStartTime":"2026-01-03T10:04:30Z","actualEndTime":"2026-01-03T12:00:00Z"`),
			// No stored verdict: the page decides, and fails.
			broadcast("PPPPPPPPPPP", "none", "PT4M49S", `"actualStartTime":"2026-01-03T12:00:09Z","actualEndTime":"2026-01-03T12:06:55Z"`),
			// MMMMMMMMMMM is missing from the answer.
		))
	}, func(id string) *http.Response {
		if id == "PPPPPPPPPPP" {
			return response(404, "")
		}
		return response(200, watchPage(id, `"isLive":true,"isLiveContent":true`, `{"isLiveNow":true,"startTimestamp":"2026-01-03T12:01:00+00:00"}`))
	}, &apiIDs, &watched)
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 12, APIKey: "synthetic-api-key"})
	a.Now = probeNow
	stored := func(id, scheduled string) Video {
		return Video{ID: id, Title: "stored", PublishedAt: "2026-01-02T00:00:00Z", ScheduledAt: scheduled, URL: "https://www.youtube.com/watch?v=" + id, ThumbnailURL: "https://i.ytimg.com/vi/" + id + "/hqdefault.jpg?v=abc", Kind: "UPCOMING LIVE", IsUpcoming: true}
	}
	ended := stored("EEEEEEEEEEE", "2026-01-03T10:00:00Z")
	ended.PremiereKnown = true
	videos := []Video{stored("LLLLLLLLLLL", "2026-01-03T12:00:00Z"), stored("UUUUUUUUUUU", "2026-01-03T13:10:00Z"), ended, stored("PPPPPPPPPPP", "2026-01-03T12:00:00Z"), stored("MMMMMMMMMMM", "2026-01-03T12:00:00Z")}
	result, err := a.ProbeLive(context.Background(), videos)
	if err != nil {
		t.Fatal(err)
	}
	// One call asks about every due video; only the unsettled ones are read.
	if strings.Join(apiIDs, ";") != "LLLLLLLLLLL,UUUUUUUUUUU,EEEEEEEEEEE,PPPPPPPPPPP,MMMMMMMMMMM" || strings.Join(watched, ",") != "PPPPPPPPPPP,MMMMMMMMMMM" {
		t.Fatalf("api=%v watched=%v", apiIDs, watched)
	}
	if result.APIRequests != 1 || result.Requested != 2 || result.Inspected != 5 || len(result.Attempted) != 5 || strings.Join(result.Errors, ",") != "youtube_http_error" {
		t.Fatalf("result=%#v", result)
	}
	// Every answered video reports its localized titles, none here; the one
	// the API left out reports nothing.
	if _, missing := result.Localized["MMMMMMMMMMM"]; len(result.Localized) != 4 || missing || len(result.Localized["PPPPPPPPPPP"]) != 0 {
		t.Fatalf("localized=%v", result.Localized)
	}
	got := map[string]Video{}
	for _, v := range result.Videos {
		got[v.ID] = v
	}
	// The actual start replaces the reservation, as on the watch page; a
	// broadcast without a length waits for its end for a stream verdict.
	if v := got["LLLLLLLLLLL"]; !v.LiveNow || v.LiveStatus != LiveStatusLive || v.IsUpcoming || v.ScheduledAt != "2026-01-03T12:04:30Z" || v.Premiere || v.PremiereKnown || v.Kind != "LIVE ARCHIVE" || v.PublishedAt != "2026-01-02T00:00:00Z" {
		t.Fatalf("live=%#v", v)
	}
	if v := got["UUUUUUUUUUU"]; v.LiveNow || v.LiveStatus != LiveStatusUpcoming || !v.IsUpcoming || v.ScheduledAt != "2026-01-03T13:10:00Z" || v.Kind != "UPCOMING LIVE" {
		t.Fatalf("upcoming=%#v", v)
	}
	// The same image keeps its stored, versioned URL.
	if v := got["EEEEEEEEEEE"]; v.LiveStatus != LiveStatusEnded || v.IsUpcoming || v.LiveNow || v.EndedAt != "2026-01-03T12:00:00Z" || v.ScheduledAt != "2026-01-03T10:04:30Z" || v.DurationSeconds != 6930 || v.Kind != "LIVE ARCHIVE" || v.ThumbnailURL != "https://i.ytimg.com/vi/EEEEEEEEEEE/hqdefault.jpg?v=abc" {
		t.Fatalf("ended=%#v", v)
	}
	// The failed page leaves the unclassified answer, which keeps the kind.
	if v := got["PPPPPPPPPPP"]; !v.Unclassified || v.PremiereKnown || v.LiveStatus != LiveStatusEnded || v.EndedAt != "2026-01-03T12:06:55Z" || v.Kind != "LIVE ARCHIVE" {
		t.Fatalf("unclassified=%#v", v)
	}
	if v := got["MMMMMMMMMMM"]; !v.LiveNow || v.ScheduledAt != "2026-01-03T12:01:00Z" {
		t.Fatalf("missing=%#v", v)
	}
}

func TestProbeFallsBackToWatchPagesWhenTheDataAPIFails(t *testing.T) {
	for code, answer := range map[string]func() *http.Response{
		"youtube_api_forbidden":        func() *http.Response { return response(403, `{"error":{"errors":[{"reason":"quotaExceeded"}]}}`) },
		"youtube_api_upstream_5xx":     func() *http.Response { return response(503, "") },
		"youtube_api_invalid_response": func() *http.Response { return response(200, `{"kind":"youtube#videoListResponse"}`) },
	} {
		t.Run(code, func(t *testing.T) {
			var apiIDs, watched []string
			c := probeAPIClient(t, answer, func(id string) *http.Response {
				return response(200, watchPage(id, `"isUpcoming":true,"isLiveContent":true`, `{"isLiveNow":false,"startTimestamp":"2026-01-03T13:10:00+00:00"}`))
			}, &apiIDs, &watched)
			a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 12, APIKey: "synthetic-api-key"})
			a.Now = probeNow
			videos := []Video{{ID: "AAAAAAAAAAA", Title: "stored", ScheduledAt: "2026-01-03T13:10:00Z", IsUpcoming: true}, {ID: "BBBBBBBBBBB", Title: "stored", ScheduledAt: "2026-01-03T13:10:00Z", IsUpcoming: true}}
			result, err := a.ProbeLive(context.Background(), videos)
			if err != nil || len(apiIDs) != 1 || strings.Join(watched, ",") != "AAAAAAAAAAA,BBBBBBBBBBB" {
				t.Fatalf("api=%v watched=%v err=%v", apiIDs, watched, err)
			}
			if result.APIRequests != 1 || result.Requested != 2 || result.Inspected != 2 || strings.Join(result.Errors, ",") != code {
				t.Fatalf("result=%#v", result)
			}
			for _, v := range result.Videos {
				if v.LiveStatus != LiveStatusUpcoming || !v.IsUpcoming {
					t.Fatalf("video=%#v", v)
				}
			}
		})
	}
}

func TestProbeReadsThePageWhenTheAPINamesNoBroadcastState(t *testing.T) {
	var apiIDs, watched []string
	c := probeAPIClient(t, func() *http.Response {
		return response(200, apiItems(
			// No liveStreamingDetails, and an upcoming answer without a start.
			`{"id":"AAAAAAAAAAA","snippet":{"title":"plain","liveBroadcastContent":"none"},"contentDetails":{"duration":"PT1M"}}`,
			`{"id":"BBBBBBBBBBB","snippet":{"title":"no start","liveBroadcastContent":"upcoming"},"contentDetails":{"duration":"P0D"},"liveStreamingDetails":{}}`,
		))
	}, func(id string) *http.Response {
		return response(200, watchPage(id, `"isLiveContent":true`, `{"isLiveNow":false,"startTimestamp":"2026-01-03T10:00:00+00:00","endTimestamp":"2026-01-03T11:00:00+00:00"}`))
	}, &apiIDs, &watched)
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 12, APIKey: "synthetic-api-key"})
	a.Now = probeNow
	result, err := a.ProbeLive(context.Background(), []Video{{ID: "AAAAAAAAAAA", ScheduledAt: "2026-01-03T10:00:00Z"}, {ID: "BBBBBBBBBBB", ScheduledAt: "2026-01-03T10:00:00Z"}})
	if err != nil || strings.Join(watched, ",") != "AAAAAAAAAAA,BBBBBBBBBBB" || result.Inspected != 2 || len(result.Errors) != 0 {
		t.Fatalf("watched=%v result=%#v err=%v", watched, result, err)
	}
	for _, v := range result.Videos {
		if v.LiveStatus != LiveStatusEnded || v.EndedAt != "2026-01-03T11:00:00Z" {
			t.Fatalf("video=%#v", v)
		}
	}
}
