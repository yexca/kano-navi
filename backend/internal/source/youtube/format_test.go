package youtube

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// botCheckPage is the 200 answer a host YouTube asks to sign in gets: a
// player without video details or broadcast times.
const botCheckPage = `<html><head><meta property="og:title" content="synthetic"></head><script>var ytInitialPlayerResponse = {"playabilityStatus":{"status":"LOGIN_REQUIRED","reason":"Sign in to confirm you're not a bot"}};</script></html>`

func endedBroadcast(id, title, duration string) string {
	return `{"id":"` + id + `","snippet":{"title":"` + title + `","liveBroadcastContent":"none"},"contentDetails":{"duration":"` + duration + `"},"liveStreamingDetails":{"scheduledStartTime":"2026-01-03T09:00:00Z","actualStartTime":"2026-01-03T09:00:10Z","actualEndTime":"2026-01-03T09:06:10Z"}}`
}

func TestEndedBroadcastsTakeTheirVerdictFromTheFormatPlaylists(t *testing.T) {
	var apiIDs, watched, lists []string
	c := probeAPIClient(t, func() *http.Response {
		return response(200, apiItems(
			endedBroadcast("PPPPPPPPPPP", "Letters - cover.", "PT4M5S"),
			endedBroadcast("SSSSSSSSSSS", "Kanade - cover.", "PT4M49S"),
			endedBroadcast("LLLLLLLLLLL", "game night", "PT2H"),
			endedBroadcast("XXXXXXXXXXX", "on both lists", "PT10M"),
			`{"id":"UUUUUUUUUUU","snippet":{"title":"Waiting - cover.","liveBroadcastContent":"upcoming"},"contentDetails":{"duration":"P0D"},"liveStreamingDetails":{"scheduledStartTime":"2026-01-03T14:00:00Z"}}`,
		))
	}, func(playlist string) *http.Response {
		lists = append(lists, playlist)
		if playlist == "UULV"+syntheticChannel[2:] {
			return response(200, apiItems(playlistItem("LLLLLLLLLLL", "2026-01-03T09:00:00Z"), playlistItem("XXXXXXXXXXX", "2026-01-03T09:00:00Z")))
		}
		// An upcoming premiere listed here before it starts gets no verdict.
		return response(200, apiItems(playlistItem("PPPPPPPPPPP", "2026-01-03T09:00:00Z"), playlistItem("SSSSSSSSSSS", "2026-01-03T09:00:00Z"), playlistItem("XXXXXXXXXXX", "2026-01-03T09:00:00Z"), playlistItem("UUUUUUUUUUU", "2026-01-03T08:00:00Z")))
	}, func(string) *http.Response {
		return response(200, botCheckPage)
	}, &apiIDs, &watched)
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 12, APIKey: "synthetic-api-key"})
	a.Now = probeNow
	stored := func(id string) Video {
		return Video{ID: id, Title: "stored", ScheduledAt: "2026-01-03T09:00:00Z", Kind: "UPCOMING LIVE", IsUpcoming: true}
	}
	// SSSSSSSSSSS holds the stream verdict an upcoming answer without a
	// length once stored.
	misread := stored("SSSSSSSSSSS")
	misread.PremiereKnown = true
	upcoming := stored("UUUUUUUUUUU")
	upcoming.ScheduledAt = "2026-01-03T14:00:00Z"
	result, err := a.ProbeLive(context.Background(), []Video{stored("PPPPPPPPPPP"), misread, stored("LLLLLLLLLLL"), stored("XXXXXXXXXXX"), upcoming})
	if err != nil {
		t.Fatal(err)
	}
	// Both playlists are read once for the whole call; only the video on
	// both lists still needs its page, which asks to sign in.
	if !slices.Equal(lists, []string{"UULV" + syntheticChannel[2:], "UULF" + syntheticChannel[2:]}) || strings.Join(watched, ",") != "XXXXXXXXXXX" {
		t.Fatalf("lists=%v watched=%v", lists, watched)
	}
	if result.APIRequests != 3 || result.Requested != 1 || result.Inspected != 5 || strings.Join(result.Errors, ",") != CodeLoginRequired {
		t.Fatalf("result=%#v", result)
	}
	got := map[string]Video{}
	for _, v := range result.Videos {
		got[v.ID] = v
	}
	for _, id := range []string{"PPPPPPPPPPP", "SSSSSSSSSSS"} {
		if v := got[id]; !v.Premiere || !v.PremiereKnown || v.Unclassified || v.Kind != "MUSIC" || v.LiveStatus != LiveStatusEnded || v.EndedAt != "2026-01-03T09:06:10Z" {
			t.Fatalf("premiere %s=%#v", id, v)
		}
	}
	if v := got["LLLLLLLLLLL"]; v.Premiere || !v.PremiereKnown || v.Kind != "LIVE ARCHIVE" || v.DurationSeconds != 7200 {
		t.Fatalf("stream=%#v", v)
	}
	if v := got["XXXXXXXXXXX"]; !v.Unclassified || v.PremiereKnown || v.Kind != "LIVE ARCHIVE" {
		t.Fatalf("on both lists=%#v", v)
	}
	if v := got["UUUUUUUUUUU"]; v.Premiere || v.PremiereKnown || !v.IsUpcoming || v.Kind != "UPCOMING LIVE" {
		t.Fatalf("upcoming=%#v", v)
	}
}

func TestAFailedFormatPlaylistLeavesTheOtherVerdicts(t *testing.T) {
	var apiIDs, watched []string
	c := probeAPIClient(t, func() *http.Response {
		return response(200, apiItems(endedBroadcast("PPPPPPPPPPP", "Letters - cover.", "PT4M5S"), endedBroadcast("LLLLLLLLLLL", "game night", "PT2H")))
	}, func(playlist string) *http.Response {
		if playlist == "UULV"+syntheticChannel[2:] {
			return response(404, "")
		}
		return response(200, apiItems(playlistItem("PPPPPPPPPPP", "2026-01-03T09:00:00Z")))
	}, func(string) *http.Response {
		return response(200, botCheckPage)
	}, &apiIDs, &watched)
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 12, APIKey: "synthetic-api-key"})
	a.Now = probeNow
	result, err := a.ProbeLive(context.Background(), []Video{{ID: "PPPPPPPPPPP", ScheduledAt: "2026-01-03T09:00:00Z", Kind: "UPCOMING LIVE"}, {ID: "LLLLLLLLLLL", ScheduledAt: "2026-01-03T09:00:00Z", Kind: "UPCOMING LIVE"}})
	if err != nil || strings.Join(watched, ",") != "LLLLLLLLLLL" || strings.Join(result.Errors, ",") != "youtube_api_http_error,"+CodeLoginRequired {
		t.Fatalf("watched=%v result=%#v err=%v", watched, result, err)
	}
	for _, v := range result.Videos {
		if v.ID == "PPPPPPPPPPP" && (!v.Premiere || !v.PremiereKnown) || v.ID == "LLLLLLLLLLL" && (!v.Unclassified || v.PremiereKnown) {
			t.Fatalf("video=%#v", v)
		}
	}
}

func TestOnlyDetailChecksReadTheFormatPlaylists(t *testing.T) {
	c := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/youtube/v3/playlistItems":
			if r.URL.Query().Get("playlistId") != "UU"+syntheticChannel[2:] {
				t.Fatalf("playlist=%s", r.URL.Query().Get("playlistId"))
			}
			return response(200, apiItems(playlistItem("EEEEEEEEEEE", "2025-12-01T00:00:00Z"))), nil
		case "/youtube/v3/videos":
			return response(200, apiItems(endedBroadcast("EEEEEEEEEEE", "old stream", "PT2H"))), nil
		case "/feeds/videos.xml":
			return response(200, syntheticFeed([2]string{"EEEEEEEEEEE", `<link rel="alternate" href="https://www.youtube.com/watch?v=EEEEEEEEEEE"/>`})), nil
		case "/channel/" + syntheticChannel + "/streams":
			return response(200, ``), nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})})
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 5, APIKey: "synthetic-api-key"})
	a.Now = syntheticNow
	// A checked old broadcast the listing names again is answered only for
	// its localized titles, so its verdict is not looked up.
	state := State{Videos: map[string]Video{"EEEEEEEEEEE": {ID: "EEEEEEEEEEE", PublishedAt: "2025-12-01T00:00:00Z", ScheduledAt: "2025-12-01T00:00:00Z", Kind: "LIVE ARCHIVE"}}, CheckedIDs: []string{"EEEEEEEEEEE"}}
	result, err := a.FetchUploads(context.Background(), state, List{Limit: 50})
	if err != nil || result.APIRequests != 2 || result.Inspected != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestAStoredPremiereVerdictOutranksTheLivePlaylist(t *testing.T) {
	items, err := ParseAPIVideos([]byte(apiItems(endedBroadcast("PPPPPPPPPPP", "Letters - cover.", "PT4M5S"), `{"id":"NNNNNNNNNNN","snippet":{"title":"no start","liveBroadcastContent":"none"},"liveStreamingDetails":{"actualEndTime":"2026-01-03T09:06:10Z"}}`)))
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := fromAPI(items["PPPPPPPPPPP"], Video{ScheduledAt: "2026-01-03T09:00:00Z", Premiere: true, PremiereKnown: true}, listedLive); !ok || !v.Premiere {
		t.Fatalf("premiere=%#v ok=%v", v, ok)
	}
	if needsListing(items["PPPPPPPPPPP"], Video{Premiere: true, PremiereKnown: true}) || !needsListing(items["PPPPPPPPPPP"], Video{PremiereKnown: true}) {
		t.Fatal("a stored premiere was looked up, or a stored stream was not")
	}
	// A listed broadcast without any start is still left to its page.
	if _, ok := fromAPI(items["NNNNNNNNNNN"], Video{}, listedVideos); ok {
		t.Fatal("classified a broadcast without a start")
	}
}

func TestFormatPlaylistsRejectAnInvalidChannel(t *testing.T) {
	c := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})})
	for _, read := range []func(context.Context, string, string) ([]byte, error){c.LivePlaylist, c.VideosPlaylist} {
		if _, err := read(context.Background(), syntheticKey, "UUAAAAAAAAAAAAAAAAAAAAAA"); err == nil || err.Error() != "youtube_invalid_channel" {
			t.Fatalf("err=%v", err)
		}
	}
}
