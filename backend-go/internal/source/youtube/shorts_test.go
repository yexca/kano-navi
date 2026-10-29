package youtube

import (
	"context"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// shortsClient lists two uploads through the Data API while the feed answers
// 404, and serves the Shorts playlist with shorts. It counts the Shorts
// playlist requests.
func shortsClient(t *testing.T, shorts func() *http.Response, requests *int) *Client {
	return NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/youtube/v3/playlistItems":
			if r.Header.Get("X-Goog-Api-Key") != syntheticKey || strings.Contains(r.URL.String(), syntheticKey) || r.URL.Host != "www.googleapis.com" {
				t.Fatalf("key not confined to the header: %s", r.URL)
			}
			switch r.URL.Query().Get("playlistId") {
			case "UU" + syntheticChannel[2:]:
				return response(200, apiItems(playlistItem("AAAAAAAAAAA", "2026-01-02T06:00:00Z"), playlistItem("BBBBBBBBBBB", "2026-01-02T05:00:00Z"))), nil
			case "UUSH" + syntheticChannel[2:]:
				*requests++
				return shorts(), nil
			}
		case "/youtube/v3/videos":
			return response(200, `{"items":[]}`), nil
		case "/feeds/videos.xml":
			return response(404, ""), nil
		case "/channel/" + syntheticChannel + "/streams":
			return response(200, ``), nil
		case "/watch":
			id := r.URL.Query().Get("v")
			return response(200, watchPage(id, `"isLiveContent":false,"lengthSeconds":"60"`, "")), nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})})
}

func TestFeedFailureMarksShortsFromTheShortsPlaylist(t *testing.T) {
	requests := 0
	// The playlist names a listed upload, a stored one and one this run knows
	// nothing of; the listed BBBBBBBBBBB is absent, which proves nothing.
	c := shortsClient(t, func() *http.Response {
		return response(200, apiItems(playlistItem("AAAAAAAAAAA", "2026-01-02T06:00:00Z"), playlistItem("CCCCCCCCCCC", "2025-12-01T00:00:00Z"), playlistItem("ZZZZZZZZZZZ", "2025-11-01T00:00:00Z")))
	}, &requests)
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 1, APIKey: "synthetic-api-key"})
	a.Now = syntheticNow
	stored := State{Videos: map[string]Video{"CCCCCCCCCCC": {ID: "CCCCCCCCCCC", PublishedAt: "2025-12-01T00:00:00Z"}}}
	result, err := a.FetchUploads(context.Background(), stored, List{Limit: 50})
	if err != nil || result.Listing != ListingAPI || requests != 1 || result.APIRequests != 3 {
		t.Fatalf("result=%#v requests=%d err=%v", result, requests, err)
	}
	if !maps.Equal(result.Shorts, map[string]bool{"AAAAAAAAAAA": true, "CCCCCCCCCCC": true}) {
		t.Fatalf("shorts=%v", result.Shorts)
	}
	// The feed's failure stays visible.
	if !slices.Equal(result.Errors, []string{"youtube_feed_http_error"}) {
		t.Fatalf("errors=%v", result.Errors)
	}
}

func TestShortsPlaylistFailureIsRecordedAfterTheFeeds(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		answer     *http.Response
	}{
		{"no Shorts playlist", "youtube_api_http_error", response(404, `{"error":{"code":404}}`)},
		{"empty playlist", "youtube_api_invalid_response", response(200, `{"items":[]}`)},
		{"refused key", "youtube_api_forbidden", response(403, "")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			a := NewAdapter(shortsClient(t, func() *http.Response { return tc.answer }, &requests), Options{ChannelID: syntheticChannel, MaxDetailRequests: 1, APIKey: "synthetic-api-key"})
			a.Now = syntheticNow
			result, err := a.FetchUploads(context.Background(), State{}, List{Limit: 50})
			if err != nil || result.Listing != ListingAPI || requests != 1 || result.APIRequests != 3 || len(result.Shorts) != 0 {
				t.Fatalf("result=%#v requests=%d err=%v", result, requests, err)
			}
			if !slices.Equal(result.Errors, []string{"youtube_feed_http_error", tc.code}) {
				t.Fatalf("errors=%v", result.Errors)
			}
		})
	}
}

func TestWithoutAKeyTheShortsPlaylistIsNotRead(t *testing.T) {
	c := feedClient(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/feeds/videos.xml":
			return response(404, ""), nil
		case "/channel/" + syntheticChannel + "/streams", "/channel/" + syntheticChannel + "/videos":
			return response(200, ``), nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 1})
	a.Now = syntheticNow
	result, err := a.FetchUploads(context.Background(), State{}, List{Limit: 50})
	if err == nil || err.Error() != "youtube_feed_http_error" || result.APIRequests != 0 || len(result.Shorts) != 0 || !slices.Equal(result.Errors, []string{"youtube_feed_http_error"}) {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestShortsPlaylistRejectsAnInvalidChannel(t *testing.T) {
	c := NewClient(&http.Client{Transport: fakeTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid channel made a request")
		return nil, nil
	})})
	if _, err := c.ShortsPlaylist(context.Background(), syntheticKey, "UUAAAAAAAAAAAAAAAAAAAAAA"); err == nil || err.Error() != "youtube_invalid_channel" {
		t.Fatalf("err=%v", err)
	}
}
