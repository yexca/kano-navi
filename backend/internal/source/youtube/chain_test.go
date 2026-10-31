package youtube

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// TestListingsFollowTheChainOrder puts the feed first: the uploads playlist
// is never read while the feed answers, and a feed without entries falls to
// the next listing.
func TestListingsFollowTheChainOrder(t *testing.T) {
	for name, tc := range map[string]struct {
		feed    *http.Response
		key     string
		chain   []string
		listing string
		tried   string
	}{
		"feed first": {
			feed:    response(200, syntheticFeed([2]string{"AAAAAAAAAAA", ""})),
			key:     "synthetic-api-key",
			chain:   []string{ListingFeed, ListingAPI, ListingVideosPage},
			listing: ListingFeed, tried: "rss",
		},
		"unusable feed": {
			feed:    response(200, syntheticFeed()),
			chain:   []string{ListingFeed, ListingVideosPage},
			listing: ListingVideosPage, tried: "rss: youtube_invalid_feed,videos_page",
		},
		"api without a key": {
			feed:    response(200, syntheticFeed([2]string{"AAAAAAAAAAA", ""})),
			chain:   []string{ListingAPI, ListingFeed},
			listing: ListingFeed, tried: "api: youtube_api_key_missing,rss",
		},
	} {
		t.Run(name, func(t *testing.T) {
			requests := []string{}
			c := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
				requests = append(requests, r.URL.Path)
				switch r.URL.Path {
				case "/feeds/videos.xml":
					return tc.feed, nil
				case "/channel/" + syntheticChannel + "/videos":
					return response(200, `{"videoId":"BBBBBBBBBBB"}`), nil
				case "/youtube/v3/videos":
					return response(200, apiItems()), nil
				}
				return response(404, ""), nil
			})})
			a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 1, APIKey: tc.key})
			a.Now = syntheticNow
			result, err := a.FetchUploads(context.Background(), State{}, List{Limit: 50, Listings: tc.chain})
			if err != nil || result.Listing != tc.listing || strings.Join(result.Tried, ",") != tc.tried {
				t.Fatalf("listing=%q tried=%v err=%v", result.Listing, result.Tried, err)
			}
			for _, path := range requests {
				if path == "/youtube/v3/playlistItems" {
					t.Fatalf("the uploads playlist was read: %v", requests)
				}
			}
		})
	}
}
