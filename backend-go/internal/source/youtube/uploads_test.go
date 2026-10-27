package youtube

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func playlistItem(id, published string) string {
	return `{"snippet":{"title":"upload ` + id + `","publishedAt":"` + published + `"},"contentDetails":{"videoId":"` + id + `","videoPublishedAt":"` + published + `"},"status":{"privacyStatus":"public"}}`
}

// uploadsClient serves a three-page uploads playlist, newest first, and
// records the pages it was asked for.
func uploadsClient(t *testing.T, requested *[]string) *Client {
	pages := map[string]string{
		"":   `{"nextPageToken":"p2","items":[` + playlistItem("AAAAAAAAAAA", "2026-01-02T06:00:00Z") + `,` + playlistItem("BBBBBBBBBBB", "2026-01-02T05:00:00Z") + `]}`,
		"p2": `{"nextPageToken":"p3","items":[` + playlistItem("CCCCCCCCCCC", "2026-01-02T04:00:00Z") + `,` + playlistItem("DDDDDDDDDDD", "2026-01-02T03:00:00Z") + `]}`,
		"p3": `{"items":[` + playlistItem("EEEEEEEEEEE", "2026-01-02T02:00:00Z") + `,` + playlistItem("FFFFFFFFFFF", "2026-01-02T01:00:00Z") + `]}`,
	}
	return NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/youtube/v3/playlistItems":
			if r.Header.Get("X-Goog-Api-Key") != syntheticKey || strings.Contains(r.URL.String(), syntheticKey) {
				t.Fatalf("key not confined to the header: %s", r.URL)
			}
			page := r.URL.Query().Get("pageToken")
			*requested = append(*requested, page)
			return response(200, pages[page]), nil
		case "/youtube/v3/videos":
			return response(200, `{"items":[]}`), nil
		case "/feeds/videos.xml":
			return response(200, syntheticFeed([2]string{"AAAAAAAAAAA", ""})), nil
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

func feedOnlyIDs(result Result) []string {
	ids := []string{}
	for _, v := range result.Videos {
		if v.FeedOnly {
			ids = append(ids, v.ID)
		}
	}
	slices.Sort(ids)
	return ids
}

func TestUploadsPlaylistPagesToAStoredVideoOrTheCount(t *testing.T) {
	for name, tc := range map[string]struct {
		list        List
		pages, adds string
		discovered  int
	}{
		// Incremental: page 2 holds a stored video, so page 3 is not read.
		"incremental": {list: List{Limit: 50}, pages: ",p2", adds: "AAAAAAAAAAA,BBBBBBBBBBB,DDDDDDDDDDD", discovered: 4},
		// The cap ends paging too, and only the newest entries are taken.
		"capped": {list: List{Limit: 1}, pages: "", adds: "AAAAAAAAAAA", discovered: 1},
		// A count reads past stored videos and keeps the newest five.
		"count": {list: List{Limit: 5, Count: true}, pages: ",p2,p3", adds: "AAAAAAAAAAA,BBBBBBBBBBB,DDDDDDDDDDD,EEEEEEEEEEE", discovered: 5},
	} {
		t.Run(name, func(t *testing.T) {
			stored := State{Videos: map[string]Video{"CCCCCCCCCCC": {ID: "CCCCCCCCCCC", PublishedAt: "2026-01-02T04:00:00Z"}}}
			var requested []string
			a := NewAdapter(uploadsClient(t, &requested), Options{ChannelID: syntheticChannel, MaxDetailRequests: 1, APIKey: "synthetic-api-key"})
			a.Now = syntheticNow
			result, err := a.FetchUploads(context.Background(), stored, tc.list)
			if err != nil || result.Listing != ListingAPI || len(result.Errors) != 0 {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			if strings.Join(requested, ",") != tc.pages || strings.Join(feedOnlyIDs(result), ",") != tc.adds || result.Discovered != tc.discovered {
				t.Fatalf("pages=%q adds=%v discovered=%d", requested, feedOnlyIDs(result), result.Discovered)
			}
			// One unit per playlist page and one videos.list call.
			if result.APIRequests != len(requested)+1 || result.NextCursor != "AAAAAAAAAAA" {
				t.Fatalf("api=%d cursor=%s", result.APIRequests, result.NextCursor)
			}
		})
	}
}

func TestCountBeyondTheFeedIsPartialWithoutAKey(t *testing.T) {
	entries := [][2]string{}
	for i := 0; i < feedLimit; i++ {
		entries = append(entries, [2]string{fmt.Sprintf("V%010d", i), ""})
	}
	c := feedClient(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/feeds/videos.xml":
			return response(200, syntheticFeed(entries...)), nil
		case "/channel/" + syntheticChannel + "/streams":
			return response(200, ``), nil
		case "/watch":
			return response(200, watchPage(r.URL.Query().Get("v"), `"isLiveContent":false,"lengthSeconds":"60"`, "")), nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 1})
	a.Now = syntheticNow
	for count, limited := range map[int]bool{feedLimit: false, 20: true} {
		result, err := a.FetchUploads(context.Background(), State{}, List{Limit: count, Count: true})
		if err != nil || result.Listing != ListingFeed || result.Discovered != feedLimit || slices.Contains(result.Errors, "youtube_count_limited") != limited {
			t.Fatalf("count %d: discovered=%d errors=%v err=%v", count, result.Discovered, result.Errors, err)
		}
	}
	if _, err := a.FetchUploads(context.Background(), State{}, List{Limit: MaxListLimit + 1}); err == nil {
		t.Fatal("a list above the limit must be rejected")
	}
}
