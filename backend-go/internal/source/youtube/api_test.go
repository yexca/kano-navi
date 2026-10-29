package youtube

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	syntheticChannel = "UCAAAAAAAAAAAAAAAAAAAAAA"
	syntheticKey     = "synthetic-api-key"
)

func syntheticNow() time.Time { return time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC) }

func TestRecentFeedFailureKeepsStoredWorkAndReadsTheUploadsTab(t *testing.T) {
	var requested []string
	feeds := 0
	c := feedClient(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/feeds/videos.xml":
			feeds++
			return response(404, ""), nil
		case "/channel/" + syntheticChannel + "/streams":
			return response(200, ``), nil
		case "/channel/" + syntheticChannel + "/videos":
			return response(200, `{"videoId":"AAAAAAAAAAA"},{"videoId":"BBBBBBBBBBB"}`), nil
		case "/watch":
			id := r.URL.Query().Get("v")
			requested = append(requested, id)
			page := watchPage(id, `"isLiveContent":false,"lengthSeconds":"60"`, "")
			// The uploads tab has no times; the page's publishDate supplies one.
			return response(200, strings.Replace(page, `"playabilityStatus"`, `"microformat":{"playerMicroformatRenderer":{"publishDate":"2026-01-02"}},"playabilityStatus"`, 1)), nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 5})
	a.Now = syntheticNow
	state := State{RefreshIDs: []string{"RRRRRRRRRRR"}, Videos: map[string]Video{"RRRRRRRRRRR": {ID: "RRRRRRRRRRR", PublishedAt: "2025-06-01T00:00:00Z"}}}
	result, err := a.FetchRecent(context.Background(), state, 7)
	if err != nil || feeds != 1 || result.Listing != ListingVideosPage || !slices.Equal(result.Errors, []string{"youtube_feed_http_error"}) {
		t.Fatalf("result=%#v feeds=%d err=%v", result, feeds, err)
	}
	if strings.Join(requested, ",") != "RRRRRRRRRRR,AAAAAAAAAAA,BBBBBBBBBBB" || result.Inspected != 3 {
		t.Fatalf("requests=%v inspected=%d", requested, result.Inspected)
	}
	// No listing means no cursor and no unchecked feed rows.
	if result.NextCursor != "" || result.Discovered != 0 {
		t.Fatalf("cursor=%q discovered=%d", result.NextCursor, result.Discovered)
	}
	for _, v := range result.Videos {
		want := "2026-01-02T00:00:00Z"
		if v.ID == "RRRRRRRRRRR" {
			want = "2025-06-01T00:00:00Z"
		}
		if v.FeedOnly || v.PublishedAt != want {
			t.Fatalf("video=%#v", v)
		}
	}
	if strings.Join(result.CheckedIDs, ",") != "AAAAAAAAAAA,BBBBBBBBBBB" {
		t.Fatalf("checked=%v", result.CheckedIDs)
	}
}

func TestRecentFailsOnlyWhenNothingAnswers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state State
	}{
		{name: "nothing stored"},
		{name: "stored work fails too", state: State{RefreshIDs: []string{"RRRRRRRRRRR"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := feedClient(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/feeds/videos.xml" {
					return response(404, ""), nil
				}
				return response(500, ""), nil
			})
			a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 5})
			a.Now = syntheticNow
			result, err := a.FetchRecent(context.Background(), tc.state, 7)
			if err == nil || err.Error() != "youtube_feed_http_error" || result.Listing != "" {
				t.Fatalf("result=%#v err=%v", result, err)
			}
		})
	}
}

// apiItems wraps raw items in a Data API list answer.
func apiItems(items ...string) string { return `{"items":[` + strings.Join(items, ",") + `]}` }

func TestRecentPrefersTheDataAPIForListingAndDetails(t *testing.T) {
	var watched, apiIDs []string
	c := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "www.googleapis.com" {
			if r.Header.Get("X-Goog-Api-Key") != syntheticKey || strings.Contains(r.URL.String(), syntheticKey) {
				t.Fatalf("key not confined to the header: %s", r.URL)
			}
			switch r.URL.Path {
			case "/youtube/v3/playlistItems":
				// The live playlist confirms the stored stream; the long-form
				// one does not name the unclassified NNNNNNNNNNN.
				switch r.URL.Query().Get("playlistId") {
				case "UULV" + syntheticChannel[2:]:
					return response(200, apiItems(playlistItem("EEEEEEEEEEE", "2026-01-02T10:00:00Z"))), nil
				case "UULF" + syntheticChannel[2:]:
					return response(200, apiItems(playlistItem("OOOOOOOOOOO", "2026-01-02T05:00:00Z"))), nil
				case "UU" + syntheticChannel[2:]:
				default:
					t.Fatalf("playlist=%s", r.URL.Query().Get("playlistId"))
				}
				return response(200, apiItems(
					`{"snippet":{"title":"Song cover premiere","publishedAt":"2026-01-02T03:00:00Z"},"contentDetails":{"videoId":"PPPPPPPPPPP","videoPublishedAt":"2026-01-02T03:00:00Z"},"status":{"privacyStatus":"public"}}`,
					`{"snippet":{"title":"waiting room","publishedAt":"2026-01-02T04:00:00Z"},"contentDetails":{"videoId":"LLLLLLLLLLL"},"status":{"privacyStatus":"public"}}`,
					`{"snippet":{"title":"ordinary upload","publishedAt":"2026-01-02T05:00:00Z"},"contentDetails":{"videoId":"OOOOOOOOOOO","videoPublishedAt":"2026-01-02T05:00:00Z"},"status":{"privacyStatus":"public"}}`,
					`{"snippet":{"title":"Private video","publishedAt":"2026-01-02T06:00:00Z"},"contentDetails":{"videoId":"XXXXXXXXXXX"},"status":{"privacyStatus":"private"}}`,
				)), nil
			case "/youtube/v3/videos":
				apiIDs = strings.Split(r.URL.Query().Get("id"), ",")
				return response(200, apiItems(
					`{"id":"PPPPPPPPPPP","snippet":{"title":"Song cover premiere","publishedAt":"2026-01-02T03:00:00Z","liveBroadcastContent":"upcoming"},"contentDetails":{"duration":"PT4M49S"},"liveStreamingDetails":{"scheduledStartTime":"2026-01-03T12:00:00Z"}}`,
					`{"id":"LLLLLLLLLLL","snippet":{"title":"waiting room","liveBroadcastContent":"upcoming"},"contentDetails":{"duration":"P0D"},"liveStreamingDetails":{"scheduledStartTime":"2026-01-03T13:00:00Z"}}`,
					`{"id":"OOOOOOOOOOO","snippet":{"title":"ordinary upload","liveBroadcastContent":"none","thumbnails":{"high":{"url":"https://i.ytimg.com/vi/OOOOOOOOOOO/hqdefault.jpg"},"maxres":{"url":"https://example.test/fake.jpg"}}},"contentDetails":{"duration":"PT1M"}}`,
					`{"id":"EEEEEEEEEEE","snippet":{"title":"finished stream","liveBroadcastContent":"none","thumbnails":{"maxres":{"url":"https://i.ytimg.com/vi/EEEEEEEEEEE/maxresdefault.jpg"}}},"contentDetails":{"duration":"PT2H"},"liveStreamingDetails":{"scheduledStartTime":"2026-01-02T09:00:00Z","actualStartTime":"2026-01-02T10:00:00Z","actualEndTime":"2026-01-02T12:00:00Z"}}`,
					`{"id":"NNNNNNNNNNN","snippet":{"title":"Song cover","liveBroadcastContent":"none"},"contentDetails":{"duration":"PT4M49S"},"liveStreamingDetails":{"actualStartTime":"2026-01-02T09:00:09Z","actualEndTime":"2026-01-02T09:06:55Z"}}`,
				)), nil
			}
		}
		if r.URL.Host == "www.youtube.com" {
			switch r.URL.Path {
			case "/feeds/videos.xml":
				// Read only for its Shorts verdicts; its entries are not listed.
				return response(200, syntheticFeed(
					[2]string{"OOOOOOOOOOO", `<link rel="alternate" href="https://www.youtube.com/watch?v=OOOOOOOOOOO"/>`},
					[2]string{"SSSSSSSSSSS", `<link rel="alternate" href="https://www.youtube.com/shorts/SSSSSSSSSSS"/>`},
				)), nil
			case "/channel/" + syntheticChannel + "/streams":
				return response(200, ``), nil
			case "/watch":
				id := r.URL.Query().Get("v")
				watched = append(watched, id)
				return response(200, watchPage(id, `"isLiveContent":false,"lengthSeconds":"289"`, `{"isLiveNow":false,"startTimestamp":"2026-01-02T09:00:09+00:00","endTimestamp":"2026-01-02T09:06:55+00:00"}`)), nil
			}
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})})
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 5, APIKey: "synthetic-api-key"})
	a.Now = syntheticNow
	state := State{
		Videos: map[string]Video{
			// A stream a detail check already described, and an upload only the
			// feed has named so far.
			"EEEEEEEEEEE": {ID: "EEEEEEEEEEE", PublishedAt: "2026-01-01T00:00:00Z", ScheduledAt: "2026-01-02T09:00:00Z", Kind: "LIVE ARCHIVE", PremiereKnown: true, ThumbnailURL: "https://i.ytimg.com/vi/EEEEEEEEEEE/maxresdefault.jpg?v=abc"},
			"NNNNNNNNNNN": {ID: "NNNNNNNNNNN", PublishedAt: "2026-01-02T03:20:58Z", Kind: "MUSIC"},
		},
		ActiveIDs:   []string{"EEEEEEEEEEE"},
		FollowUpIDs: []string{"NNNNNNNNNNN"},
	}
	result, err := a.FetchRecent(context.Background(), state, 7)
	// The uploads playlist, videos.list, and the live and long-form playlists
	// for the queued ended broadcasts.
	if err != nil || len(result.Errors) != 0 || result.Listing != ListingAPI || result.APIRequests != 4 || result.Discovered != 3 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if strings.Join(apiIDs, ",") != "PPPPPPPPPPP,LLLLLLLLLLL,OOOOOOOOOOO,EEEEEEEEEEE,NNNNNNNNNNN" {
		t.Fatalf("api ids=%v", apiIDs)
	}
	if len(result.Shorts) != 2 || result.Shorts["OOOOOOOOOOO"] || !result.Shorts["SSSSSSSSSSS"] {
		t.Fatalf("shorts=%v", result.Shorts)
	}
	// Only the finished broadcast nothing had classified needs its page.
	if strings.Join(watched, ",") != "NNNNNNNNNNN" || result.Requested != 1 || result.Inspected != 5 {
		t.Fatalf("watched=%v requested=%d inspected=%d", watched, result.Requested, result.Inspected)
	}
	if result.NextCursor != "OOOOOOOOOOO" || result.NextCursorTime != "2026-01-02T05:00:00Z" {
		t.Fatalf("cursor=%s %s", result.NextCursor, result.NextCursorTime)
	}
	details := map[string]Video{}
	for _, v := range result.Videos {
		if !v.FeedOnly {
			details[v.ID] = v
		}
	}
	premiere, room, upload, stream, checked := details["PPPPPPPPPPP"], details["LLLLLLLLLLL"], details["OOOOOOOOOOO"], details["EEEEEEEEEEE"], details["NNNNNNNNNNN"]
	if !premiere.Premiere || !premiere.IsUpcoming || premiere.Kind != "MUSIC" || premiere.ScheduledAt != "2026-01-03T12:00:00Z" || premiere.LiveStatus != LiveStatusUpcoming || premiere.DurationSeconds != 0 {
		t.Fatalf("premiere=%#v", premiere)
	}
	if room.Premiere || !room.IsUpcoming || room.Kind != "UPCOMING LIVE" || room.PublishedAt != "2026-01-02T04:00:00Z" {
		t.Fatalf("waiting room=%#v", room)
	}
	if upload.Kind != "VIDEO" || upload.ScheduledAt != "" || upload.DurationSeconds != 60 || upload.ThumbnailURL != "https://i.ytimg.com/vi/OOOOOOOOOOO/hqdefault.jpg" {
		t.Fatalf("upload=%#v", upload)
	}
	if stream.Premiere || stream.Kind != "LIVE ARCHIVE" || stream.LiveStatus != LiveStatusEnded || stream.ScheduledAt != "2026-01-02T10:00:00Z" || stream.EndedAt != "2026-01-02T12:00:00Z" || stream.DurationSeconds != 7200 || stream.PublishedAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("stream=%#v", stream)
	}
	// The same image keeps its stored, versioned URL.
	if stream.ThumbnailURL != "https://i.ytimg.com/vi/EEEEEEEEEEE/maxresdefault.jpg?v=abc" {
		t.Fatalf("stream thumbnail=%s", stream.ThumbnailURL)
	}
	// The page decides: a premiere keeps the kind of its title, not a live kind.
	if !checked.Premiere || checked.Kind != "VIDEO" || checked.EndedAt != "2026-01-02T09:06:55Z" || checked.PublishedAt != "2026-01-02T03:20:58Z" {
		t.Fatalf("watch-checked premiere=%#v", checked)
	}
}

func TestEndedBroadcastTakesItsPremiereVerdictFromTheStoredCheck(t *testing.T) {
	items, err := ParseAPIVideos([]byte(apiItems(`{"id":"PPPPPPPPPPP","snippet":{"title":"初配信 premiere","liveBroadcastContent":"none"},"contentDetails":{"duration":"PT4M49S"},"liveStreamingDetails":{"scheduledStartTime":"2026-01-02T09:00:00Z","actualStartTime":"2026-01-02T09:00:09Z","actualEndTime":"2026-01-02T09:06:55Z"}}`)))
	if err != nil {
		t.Fatal(err)
	}
	item := items["PPPPPPPPPPP"]
	start := "2026-01-02T09:00:00Z"
	// A premiere whose title reads as a stream keeps the stored verdict.
	v, ok := fromAPI(item, Video{ScheduledAt: start, Kind: "LIVE ARCHIVE", Premiere: true, PremiereKnown: true}, listedNowhere)
	if !ok || !v.Premiere || !v.PremiereKnown {
		t.Fatalf("premiere=%#v ok=%v", v, ok)
	}
	if v, ok = fromAPI(item, Video{ScheduledAt: start, Kind: "MUSIC", PremiereKnown: true}, listedNowhere); !ok || v.Premiere {
		t.Fatalf("stream=%#v ok=%v", v, ok)
	}
	// A row stored before verdicts were kept, with a start time and a live
	// kind, is left to its watch page.
	if _, ok = fromAPI(item, Video{ScheduledAt: start, Kind: "LIVE ARCHIVE"}, listedNowhere); ok {
		t.Fatal("an unknown verdict was guessed from the kind")
	}
}

func TestPremiereWithoutALengthIsLeftForItsEnd(t *testing.T) {
	items, err := ParseAPIVideos([]byte(apiItems(
		`{"id":"UUUUUUUUUUU","snippet":{"title":"Letters - cover.","liveBroadcastContent":"upcoming"},"contentDetails":{"duration":"P0D"},"liveStreamingDetails":{"scheduledStartTime":"2026-01-03T09:00:00Z"}}`,
		`{"id":"LLLLLLLLLLL","snippet":{"title":"Letters - cover.","liveBroadcastContent":"live"},"contentDetails":{"duration":"P0D"},"liveStreamingDetails":{"actualStartTime":"2026-01-03T09:00:10Z"}}`,
	)))
	if err != nil {
		t.Fatal(err)
	}
	// A premiere can report P0D before it ends, so no stream verdict is
	// stored that a later answer would repeat.
	for _, id := range []string{"UUUUUUUUUUU", "LLLLLLLLLLL"} {
		if v, ok := fromAPI(items[id], Video{}, listedNowhere); !ok || v.Premiere || v.PremiereKnown {
			t.Fatalf("%s=%#v ok=%v", id, v, ok)
		}
	}
}

func TestRefreshRequestReadsThePageOfAClassifiedBroadcast(t *testing.T) {
	var watched []string
	ended := func(id string) string {
		return `{"id":"` + id + `","snippet":{"title":"Letters - cover.","liveBroadcastContent":"none"},"contentDetails":{"duration":"PT4M5S"},"liveStreamingDetails":{"scheduledStartTime":"2026-01-02T09:00:00Z","actualStartTime":"2026-01-02T09:00:10Z","actualEndTime":"2026-01-02T09:06:10Z"}}`
	}
	c := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/youtube/v3/playlistItems":
			return response(200, apiItems()), nil
		case "/youtube/v3/videos":
			return response(200, apiItems(ended("RRRRRRRRRRR"), ended("FFFFFFFFFFF"))), nil
		case "/feeds/videos.xml":
			return response(200, syntheticFeed()), nil
		case "/channel/" + syntheticChannel + "/streams", "/channel/" + syntheticChannel + "/videos":
			return response(200, ``), nil
		case "/watch":
			id := r.URL.Query().Get("v")
			watched = append(watched, id)
			if id == "FFFFFFFFFFF" {
				return response(404, ""), nil
			}
			page := watchPage(id, `"isLiveContent":false,"lengthSeconds":"245"`, `{"isLiveNow":false,"startTimestamp":"2026-01-02T09:00:10+00:00","endTimestamp":"2026-01-02T09:06:10+00:00"}`)
			return response(200, strings.Replace(page, "synthetic stream", "Letters - cover.", 1)), nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})})
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 5, APIKey: "synthetic-api-key"})
	a.Now = syntheticNow
	// Premieres a check before their end stored as streams.
	stored := func(id string) Video {
		return Video{ID: id, Title: "Letters - cover.", PublishedAt: "2026-01-01T13:58:38Z", ScheduledAt: "2026-01-02T09:00:10Z", Kind: "LIVE ARCHIVE", PremiereKnown: true}
	}
	state := State{RefreshIDs: []string{"RRRRRRRRRRR", "FFFFFFFFFFF"}, Videos: map[string]Video{"RRRRRRRRRRR": stored("RRRRRRRRRRR"), "FFFFFFFFFFF": stored("FFFFFFFFFFF")}}
	result, err := a.FetchRecent(context.Background(), state, 7)
	if err != nil || strings.Join(watched, ",") != "RRRRRRRRRRR,FFFFFFFFFFF" || result.Requested != 2 || result.Inspected != 2 || !slices.Contains(result.Errors, "youtube_http_error") {
		t.Fatalf("result=%#v watched=%v err=%v", result, watched, err)
	}
	got := map[string]Video{}
	for _, v := range result.Videos {
		got[v.ID] = v
	}
	// The page settles the premiere; a failed page leaves the API answer.
	if v := got["RRRRRRRRRRR"]; !v.Premiere || !v.PremiereKnown || v.Kind != "MUSIC" || v.LiveStatus != LiveStatusEnded {
		t.Fatalf("refreshed=%#v", v)
	}
	if v := got["FFFFFFFFFFF"]; v.Premiere || !v.PremiereKnown || v.Kind != "LIVE ARCHIVE" || v.EndedAt != "2026-01-02T09:06:10Z" {
		t.Fatalf("failed page=%#v", v)
	}
}

func TestSameImageIgnoresSizesHostsAndMissingVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{"https://i.ytimg.com/vi/AAAAAAAAAAA/hqdefault.jpg", "https://i.ytimg.com/vi/AAAAAAAAAAA/maxresdefault.jpg?v=abc", true},
		{"https://i4.ytimg.com/vi/AAAAAAAAAAA/hqdefault.jpg", "https://i.ytimg.com/vi_webp/AAAAAAAAAAA/sddefault.webp", true},
		{"https://i.ytimg.com/vi/AAAAAAAAAAA/maxresdefault.jpg?v=abc", "https://i.ytimg.com/vi/AAAAAAAAAAA/maxresdefault.jpg?v=def", false},
		{"https://i.ytimg.com/vi/AAAAAAAAAAA/maxresdefault_live.jpg", "https://i.ytimg.com/vi/AAAAAAAAAAA/maxresdefault.jpg", false},
		{"https://i.ytimg.com/vi/AAAAAAAAAAA/hqdefault.jpg", "https://i.ytimg.com/vi/BBBBBBBBBBB/hqdefault.jpg", false},
		{"https://i.ytimg.com/case/page.png", "https://i9.ytimg.com/case/page.png", true},
		{"", "https://i.ytimg.com/vi/AAAAAAAAAAA/hqdefault.jpg", false},
	} {
		if SameImage(c.a, c.b) != c.same || SameImage(c.b, c.a) != c.same {
			t.Errorf("SameImage(%q, %q) != %v", c.a, c.b, c.same)
		}
	}
}

func TestRecentFallsBackToPublicPagesWhenTheDataAPIFails(t *testing.T) {
	var watched []string
	apiCalls := 0
	c := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Host == "www.googleapis.com":
			apiCalls++
			return response(403, `{"error":{"code":403}}`), nil
		case r.URL.Path == "/feeds/videos.xml":
			return response(200, `<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015"><entry><yt:videoId>CCCCCCCCCCC</yt:videoId><title>new upload</title><published>2026-01-02T23:00:00Z</published></entry></feed>`), nil
		case r.URL.Path == "/channel/"+syntheticChannel+"/streams":
			return response(200, ``), nil
		case r.URL.Path == "/watch":
			id := r.URL.Query().Get("v")
			watched = append(watched, id)
			return response(200, watchPage(id, `"isLiveContent":false,"lengthSeconds":"60"`, "")), nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})})
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 5, APIKey: "synthetic-api-key"})
	a.Now = syntheticNow
	result, err := a.FetchRecent(context.Background(), State{}, 7)
	if err != nil || result.Listing != ListingFeed || apiCalls != 2 || result.APIRequests != 2 || !slices.Equal(result.Errors, []string{"youtube_api_forbidden", "youtube_api_forbidden"}) {
		t.Fatalf("result=%#v calls=%d err=%v", result, apiCalls, err)
	}
	if strings.Join(watched, ",") != "CCCCCCCCCCC" || result.Inspected != 1 {
		t.Fatalf("watched=%v inspected=%d", watched, result.Inspected)
	}
}

func TestDataAPIClientKeepsItsDestinationAndBounds(t *testing.T) {
	requests := 0
	c := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Scheme != "https" || r.URL.Host != "www.googleapis.com" {
			t.Fatalf("unexpected destination %s", r.URL)
		}
		return response(302, ""), nil
	})})
	ctx := context.Background()
	if _, err := c.PlaylistItems(ctx, syntheticKey, syntheticChannel); err == nil || err.Error() != "youtube_api_redirect_rejected" {
		t.Fatalf("redirect err=%v", err)
	}
	for _, tc := range []struct {
		name string
		call func() error
		code string
	}{
		{"missing key", func() error { _, err := c.Videos(ctx, " ", []string{"AAAAAAAAAAA"}); return err }, "youtube_api_key_missing"},
		{"no ids", func() error { _, err := c.Videos(ctx, syntheticKey, nil); return err }, "youtube_api_invalid_request"},
		{"too many ids", func() error {
			_, err := c.Videos(ctx, syntheticKey, slices.Repeat([]string{"AAAAAAAAAAA"}, MaxAPIVideos+1))
			return err
		}, "youtube_api_invalid_request"},
		{"invalid id", func() error { _, err := c.Videos(ctx, syntheticKey, []string{"bad/id"}); return err }, "youtube_invalid_video"},
		{"invalid channel", func() error { _, err := c.PlaylistItems(ctx, syntheticKey, "bad"); return err }, "youtube_invalid_channel"},
	} {
		if err := tc.call(); err == nil || err.Error() != tc.code {
			t.Fatalf("%s: err=%v, want %s", tc.name, err, tc.code)
		}
	}
	if requests != 1 {
		t.Fatalf("requests=%d", requests)
	}
}

func TestDataAPIParsing(t *testing.T) {
	for value, want := range map[string]int{"PT4M49S": 289, "PT2H": 7200, "P1DT1S": 86401, "P0D": 0, "PT": 0, "4:49": 0, "": 0, "PT99999999999S": 0} {
		if got := parseDuration(value); got != want {
			t.Fatalf("parseDuration(%q)=%d, want %d", value, got, want)
		}
	}
	for _, body := range []string{`{}`, `not json`, apiItems(`{"snippet":{"title":"Private video"},"contentDetails":{"videoId":"XXXXXXXXXXX","videoPublishedAt":"2026-01-02T00:00:00Z"},"status":{"privacyStatus":"private"}}`)} {
		if _, err := ParseUploads([]byte(body)); err == nil || err.Error() != "youtube_api_invalid_response" {
			t.Fatalf("ParseUploads(%s) err=%v", body, err)
		}
	}
	if _, err := ParseAPIVideos([]byte(`{"kind":"youtube#videoListResponse"}`)); err == nil {
		t.Fatal("accepted an answer without items")
	}
	items, err := ParseAPIVideos([]byte(apiItems(
		`{"id":"AAAAAAAAAAA","snippet":{"title":"ended","liveBroadcastContent":"none"},"liveStreamingDetails":{"actualEndTime":"2026-01-02T00:00:00Z"}}`,
		`{"id":"BBBBBBBBBBB","snippet":{"title":"reservation without a time","liveBroadcastContent":"upcoming"},"liveStreamingDetails":{}}`,
		`{"id":"CCCCCCCCCCC","snippet":{"title":" ","liveBroadcastContent":"none"}}`,
	)))
	if err != nil || len(items) != 3 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	// An ended broadcast with no stored detail check, an upcoming one without
	// a start and an untitled video are all left to their pages.
	for id, item := range items {
		if v, ok := fromAPI(item, Video{}, listedNowhere); ok {
			t.Fatalf("%s classified: %#v", id, v)
		}
	}
}

func TestRecentListedByTheUploadsTabSucceedsWithoutADetailCheck(t *testing.T) {
	c := feedClient(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/feeds/videos.xml":
			return response(404, ""), nil
		case "/channel/" + syntheticChannel + "/videos":
			return response(200, `{"videoId":"AAAAAAAAAAA"}`), nil
		}
		return response(500, ""), nil
	})
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 5})
	a.Now = syntheticNow
	result, err := a.FetchRecent(context.Background(), State{Queue: []string{"QQQQQQQQQQQ"}}, 7)
	if err != nil || result.Listing != ListingVideosPage || result.Inspected != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	// Both failed checks stay queued, rotated behind each other.
	if strings.Join(result.Queue, ",") != "QQQQQQQQQQQ,AAAAAAAAAAA" {
		t.Fatalf("queue=%v", result.Queue)
	}
}

func TestRecentUsesUnclassifiedAPIAnswersOnlyWhenTheWatchPageFails(t *testing.T) {
	var watched []string
	c := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/youtube/v3/playlistItems":
			// The feed fails, and this channel has no Shorts playlist.
			if strings.HasPrefix(r.URL.Query().Get("playlistId"), "UUSH") {
				return response(404, ""), nil
			}
			return response(200, apiItems(`{"snippet":{"title":"ordinary upload","publishedAt":"2025-12-01T00:00:00Z"},"contentDetails":{"videoId":"ZZZZZZZZZZZ"},"status":{"privacyStatus":"public"}}`)), nil
		case "/youtube/v3/videos":
			ended := func(id, title string) string {
				return `{"id":"` + id + `","snippet":{"title":"` + title + `","liveBroadcastContent":"none","thumbnails":{"maxres":{"url":"https://i.ytimg.com/vi/` + id + `/maxresdefault.jpg"}}},"contentDetails":{"duration":"PT4M49S"},"liveStreamingDetails":{"actualStartTime":"2026-01-02T09:00:09Z","actualEndTime":"2026-01-02T09:06:55Z"}}`
			}
			return response(200, apiItems(ended("PPPPPPPPPPP", "Kanade - cover."), ended("SSSSSSSSSSS", "finished stream"), ended("WWWWWWWWWWW", "readable page"),
				// Ended without a start: nothing to place, so no fallback.
				`{"id":"NNNNNNNNNNN","snippet":{"title":"no start","liveBroadcastContent":"none"},"liveStreamingDetails":{"actualEndTime":"2026-01-02T12:00:00Z"}}`)), nil
		case "/feeds/videos.xml", "/channel/" + syntheticChannel + "/streams":
			return response(404, ""), nil
		case "/watch":
			id := r.URL.Query().Get("v")
			watched = append(watched, id)
			if id == "WWWWWWWWWWW" {
				return response(200, watchPage(id, `"isLiveContent":false,"lengthSeconds":"289"`, `{"isLiveNow":false,"startTimestamp":"2026-01-02T09:00:09+00:00","endTimestamp":"2026-01-02T09:06:55+00:00"}`)), nil
			}
			if id == "SSSSSSSSSSS" {
				return response(200, `<html>no player</html>`), nil
			}
			return response(404, ""), nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})})
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 5, APIKey: "synthetic-api-key"})
	a.Now = syntheticNow
	state := State{
		RefreshIDs: []string{"PPPPPPPPPPP", "SSSSSSSSSSS", "WWWWWWWWWWW", "NNNNNNNNNNN"},
		Videos: map[string]Video{
			"PPPPPPPPPPP": {ID: "PPPPPPPPPPP", Title: "??? - cover.", PublishedAt: "2026-01-02T03:20:58Z", Kind: "MUSIC", ThumbnailURL: "https://i2.ytimg.com/vi/PPPPPPPPPPP/hqdefault.jpg"},
			"SSSSSSSSSSS": {ID: "SSSSSSSSSSS", Title: "finished stream", PublishedAt: "2026-01-01T00:00:00Z", ScheduledAt: "2026-01-02T09:00:00Z", Kind: "UPCOMING LIVE"},
		},
	}
	result, err := a.FetchRecent(context.Background(), state, 7)
	if err != nil || strings.Join(watched, ",") != "PPPPPPPPPPP,SSSSSSSSSSS,WWWWWWWWWWW,NNNNNNNNNNN" {
		t.Fatalf("watched=%v result=%#v err=%v", watched, result, err)
	}
	if strings.Join(result.InspectedIDs, ",") != "PPPPPPPPPPP,SSSSSSSSSSS,WWWWWWWWWWW" {
		t.Fatalf("inspected=%v errors=%v", result.InspectedIDs, result.Errors)
	}
	// Each failed page is still recorded.
	if !slices.Equal(result.Errors, []string{"youtube_feed_http_error", "youtube_api_http_error", "youtube_http_error", "youtube_http_error", "youtube_watch_details_incomplete", "youtube_http_error"}) {
		t.Fatalf("errors=%v", result.Errors)
	}
	details := map[string]Video{}
	for _, v := range result.Videos {
		if !v.FeedOnly {
			details[v.ID] = v
		}
	}
	premiere, stream, page := details["PPPPPPPPPPP"], details["SSSSSSSSSSS"], details["WWWWWWWWWWW"]
	// The API answer updates the title, times and length but keeps the stored
	// kind and verdict; another size of the same image keeps the stored URL.
	if !premiere.Unclassified || premiere.PremiereKnown || premiere.Title != "Kanade - cover." || premiere.Kind != "MUSIC" ||
		premiere.ScheduledAt != "2026-01-02T09:00:09Z" || premiere.EndedAt != "2026-01-02T09:06:55Z" || premiere.DurationSeconds != 289 ||
		premiere.ThumbnailURL != "https://i2.ytimg.com/vi/PPPPPPPPPPP/hqdefault.jpg" || premiere.PublishedAt != "2026-01-02T03:20:58Z" {
		t.Fatalf("premiere=%#v", premiere)
	}
	// A stored waiting-room kind turns into its ended form.
	if !stream.Unclassified || stream.Kind != "LIVE ARCHIVE" || stream.LiveStatus != LiveStatusEnded || stream.IsUpcoming {
		t.Fatalf("stream=%#v", stream)
	}
	// A readable page decides as before.
	if page.Unclassified || !page.PremiereKnown || !page.Premiere {
		t.Fatalf("page=%#v", page)
	}
}
