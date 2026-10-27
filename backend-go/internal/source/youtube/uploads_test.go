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

func TestIncrementalCapLeavesNoUploadsBehindTheCursor(t *testing.T) {
	// E and F are stored and E is the cursor; A to D arrived since. With a cap
	// of one, each run takes the newest upload not stored yet, pages past the
	// stored ones to reach it, and keeps the cursor until the read reaches E.
	var requested []string
	a := NewAdapter(uploadsClient(t, &requested), Options{ChannelID: syntheticChannel, MaxDetailRequests: 1, APIKey: "synthetic-api-key"})
	a.Now = syntheticNow
	state := State{CursorID: "EEEEEEEEEEE", CursorTime: "2026-01-02T02:00:00Z", Videos: map[string]Video{
		"EEEEEEEEEEE": {ID: "EEEEEEEEEEE", PublishedAt: "2026-01-02T02:00:00Z"},
		"FFFFFFFFFFF": {ID: "FFFFFFFFFFF", PublishedAt: "2026-01-02T01:00:00Z"},
	}}
	for i, want := range []struct{ adds, pages, cursor string }{
		{"AAAAAAAAAAA", "", ""},
		{"BBBBBBBBBBB", "", ""},
		{"CCCCCCCCCCC", ",p2", ""},
		{"DDDDDDDDDDD", ",p2", ""},
		{"", ",p2,p3", "AAAAAAAAAAA"},
	} {
		requested = nil
		result, err := a.FetchUploads(context.Background(), state, List{Limit: 1})
		if err != nil || len(result.Errors) != 0 {
			t.Fatalf("run %d: result=%#v err=%v", i, result, err)
		}
		if got := strings.Join(feedOnlyIDs(result), ","); got != want.adds || strings.Join(requested, ",") != want.pages || result.NextCursor != want.cursor {
			t.Fatalf("run %d: adds=%q pages=%q cursor=%q", i, got, requested, result.NextCursor)
		}
		for _, v := range result.Videos {
			state.Videos[v.ID] = v
		}
	}
}

// datedFeed lists the IDs newest first, an hour apart, the first at 23:00.
func datedFeed(ids ...string) string {
	out := `<?xml version="1.0" encoding="UTF-8"?><feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="urn:synthetic:media" xmlns="urn:synthetic:atom">`
	for i, id := range ids {
		out += fmt.Sprintf(`<entry><id>yt:video:%s</id><yt:videoId>%s</yt:videoId><title>synthetic %s</title><published>2026-01-02T%02d:00:00+00:00</published></entry>`, id, id, id, 23-i)
	}
	return out + `</feed>`
}

func TestIncrementalFeedCapCountsOnlyNewUploads(t *testing.T) {
	ids := []string{}
	for i := 0; i < 10; i++ {
		ids = append(ids, fmt.Sprintf("V%010d", 9-i))
	}
	c := feedClient(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/feeds/videos.xml":
			return response(200, datedFeed(ids...)), nil
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
	// The three oldest entries are stored, the newest of them is the cursor,
	// and seven uploads arrived since.
	stored := func() State {
		s := State{CursorID: ids[7], CursorTime: "2026-01-02T16:00:00Z", Videos: map[string]Video{}}
		for _, id := range ids[7:] {
			s.Videos[id] = Video{ID: id}
		}
		return s
	}
	state := stored()
	first, err := a.FetchUploads(context.Background(), state, List{Limit: 5})
	if err != nil || strings.Join(feedOnlyIDs(first), ",") != strings.Join(sorted(ids[:5]), ",") || first.NextCursor != "" {
		t.Fatalf("first: adds=%v cursor=%q err=%v", feedOnlyIDs(first), first.NextCursor, err)
	}
	for _, v := range first.Videos {
		state.Videos[v.ID] = v
	}
	second, err := a.FetchUploads(context.Background(), state, List{Limit: 5})
	if err != nil || strings.Join(feedOnlyIDs(second), ",") != strings.Join(sorted(ids[5:7]), ",") || second.NextCursor != ids[0] {
		t.Fatalf("second: adds=%v cursor=%q err=%v", feedOnlyIDs(second), second.NextCursor, err)
	}
	// Without a cursor the first run takes the newest five and moves on, as
	// a bootstrap does not fill older history.
	bootstrap, err := a.FetchUploads(context.Background(), State{}, List{Limit: 5})
	if err != nil || len(feedOnlyIDs(bootstrap)) != 5 || bootstrap.NextCursor != ids[0] {
		t.Fatalf("bootstrap: adds=%v cursor=%q err=%v", feedOnlyIDs(bootstrap), bootstrap.NextCursor, err)
	}
	// A count still takes the newest entries, stored or not.
	count, err := a.FetchUploads(context.Background(), stored(), List{Limit: 5, Count: true})
	if err != nil || strings.Join(feedOnlyIDs(count), ",") != strings.Join(sorted(ids[:5]), ",") || count.NextCursor != ids[0] {
		t.Fatalf("count: adds=%v cursor=%q err=%v", feedOnlyIDs(count), count.NextCursor, err)
	}
}

func sorted(ids []string) []string {
	out := slices.Clone(ids)
	slices.Sort(out)
	return out
}
