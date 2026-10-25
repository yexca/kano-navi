package youtube

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFollowUpDueOncePerPassedOffset(t *testing.T) {
	anchor := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name         string
		now, checked time.Time
		due          bool
	}{
		{name: "before the first offset", now: anchor.Add(59 * time.Minute), due: false},
		{name: "first offset, never checked", now: anchor.Add(time.Hour), due: true},
		{name: "first offset, checked before it", now: anchor.Add(2 * time.Hour), checked: anchor.Add(30 * time.Minute), due: true},
		{name: "first offset, checked after it", now: anchor.Add(2 * time.Hour), checked: anchor.Add(61 * time.Minute), due: false},
		{name: "a day later, checked at the first offset", now: anchor.Add(25 * time.Hour), checked: anchor.Add(61 * time.Minute), due: true},
		{name: "a week later, checked on day three", now: anchor.Add(7*24*time.Hour + time.Minute), checked: anchor.Add(73 * time.Hour), due: true},
		{name: "a week later, checked after it", now: anchor.Add(7*24*time.Hour + time.Hour), checked: anchor.Add(7*24*time.Hour + time.Minute), due: false},
		{name: "outside the window", now: anchor.Add(FollowUpWindow + time.Minute), due: false},
		{name: "no anchor", now: anchor, due: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := anchor
			if tc.name == "no anchor" {
				a = time.Time{}
			}
			if got := FollowUpDue(a, tc.checked, tc.now); got != tc.due {
				t.Fatalf("FollowUpDue=%v, want %v", got, tc.due)
			}
		})
	}
}

func TestWatchReadsTheLengthOfFinishedVideosOnly(t *testing.T) {
	for _, tc := range []struct {
		name, details, broadcast string
		duration                 int
	}{
		{name: "ordinary upload", details: `"isLiveContent":false,"lengthSeconds":"289"`, duration: 289},
		{
			name: "finished premiere", details: `"isLiveContent":false,"lengthSeconds":"289"`,
			broadcast: `{"isLiveNow":false,"startTimestamp":"2026-01-02T09:00:09+00:00","endTimestamp":"2026-01-02T09:06:55+00:00"}`,
			duration:  289,
		},
		{
			name: "on air", details: `"isLive":true,"isLiveContent":true,"lengthSeconds":"0"`,
			broadcast: `{"isLiveNow":true,"startTimestamp":"2026-01-03T12:04:30+00:00"}`,
		},
		{
			name: "waiting room", details: `"isUpcoming":true,"isLiveContent":true,"lengthSeconds":"0"`,
			broadcast: `{"isLiveNow":false,"startTimestamp":"2026-01-03T12:00:00+00:00"}`,
		},
		{name: "unreadable", details: `"isLiveContent":false,"lengthSeconds":"long"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := ParseWatch([]byte(watchPage("AAAAAAAAAAA", tc.details, tc.broadcast)), "AAAAAAAAAAA")
			if err != nil || v.DurationSeconds != tc.duration {
				t.Fatalf("video=%#v err=%v", v, err)
			}
		})
	}
}

func TestRecentChecksRefreshRequestsFirstAndFollowUpsLast(t *testing.T) {
	var requested []string
	c := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/feeds/videos.xml":
			return response(200, `<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015"><entry><yt:videoId>CCCCCCCCCCC</yt:videoId><title>new upload</title><published>2026-01-02T23:00:00Z</published></entry></feed>`), nil
		case "/channel/UCAAAAAAAAAAAAAAAAAAAAAA/streams":
			return response(200, ``), nil
		case "/watch":
			id := r.URL.Query().Get("v")
			requested = append(requested, id)
			if id == "FFFFFFFFFFF" {
				return response(404, ""), nil
			}
			return response(200, watchPage(id, `"isLiveContent":false,"lengthSeconds":"60"`, "")), nil
		}
		t.Fatalf("unexpected path %s", r.URL.Path)
		return nil, nil
	})})
	a := NewAdapter(c, Options{ChannelID: "UCAAAAAAAAAAAAAAAAAAAAAA", MaxDetailRequests: 4})
	a.Now = func() time.Time { return time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC) }
	state := State{
		Queue:       []string{"QQQQQQQQQQQ"},
		RefreshIDs:  []string{"RRRRRRRRRRR"},
		FollowUpIDs: []string{"FFFFFFFFFFF", "GGGGGGGGGGG"},
		Videos:      map[string]Video{"RRRRRRRRRRR": {ID: "RRRRRRRRRRR", PublishedAt: "2025-06-01T00:00:00Z"}},
	}
	result, err := a.FetchRecent(context.Background(), state, 7)
	if err != nil || strings.Join(requested, ",") != "RRRRRRRRRRR,QQQQQQQQQQQ,CCCCCCCCCCC,FFFFFFFFFFF" {
		t.Fatalf("requests=%v err=%v", requested, err)
	}
	if strings.Join(result.InspectedIDs, ",") != "RRRRRRRRRRR,QQQQQQQQQQQ,CCCCCCCCCCC" {
		t.Fatalf("inspected=%v", result.InspectedIDs)
	}
	// Finished ordinary videos leave the queue; the failed follow-up rotates
	// behind the one this run had no budget for.
	if strings.Join(result.Queue, ",") != "GGGGGGGGGGG,FFFFFFFFFFF" {
		t.Fatalf("queue=%v", result.Queue)
	}
	for _, v := range result.Videos {
		if v.ID == "RRRRRRRRRRR" && (v.PublishedAt != "2025-06-01T00:00:00Z" || v.DurationSeconds != 60) {
			t.Fatalf("refreshed=%#v", v)
		}
	}
}
