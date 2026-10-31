package youtube

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// syntheticFeed is a channel feed in YouTube's Atom layout, with synthetic
// URNs standing in for the Atom and Media RSS namespaces. Each entry is a
// video ID and that entry's raw <link> elements.
func syntheticFeed(entries ...[2]string) string {
	out := `<?xml version="1.0" encoding="UTF-8"?><feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="urn:synthetic:media" xmlns="urn:synthetic:atom">`
	for _, e := range entries {
		out += `<entry><id>yt:video:` + e[0] + `</id><yt:videoId>` + e[0] + `</yt:videoId><title>synthetic ` + e[0] + `</title>` + e[1] + `<published>2026-01-02T00:00:00+00:00</published><media:group><media:thumbnail url="https://i.ytimg.com/vi/` + e[0] + `/hqdefault.jpg" width="480" height="360"/></media:group></entry>`
	}
	return out + `</feed>`
}

func TestFeedAlternateLinkNamesShortsWithoutGuessing(t *testing.T) {
	feed := syntheticFeed(
		[2]string{"AAAAAAAAAAA", `<link rel="alternate" href="https://www.youtube.com/shorts/AAAAAAAAAAA"/>`},
		[2]string{"BBBBBBBBBBB", `<link rel="alternate" href="https://www.youtube.com/watch?v=BBBBBBBBBBB"/>`},
		// No link, a link for another video, another host and a non-alternate
		// relation all leave the format unknown.
		[2]string{"CCCCCCCCCCC", ``},
		[2]string{"DDDDDDDDDDD", `<link rel="alternate" href="https://www.youtube.com/shorts/AAAAAAAAAAA"/>`},
		[2]string{"EEEEEEEEEEE", `<link rel="alternate" href="https://shorts.example.test/shorts/EEEEEEEEEEE"/>`},
		[2]string{"FFFFFFFFFFF", `<link rel="related" href="https://www.youtube.com/shorts/FFFFFFFFFFF"/><link href="http://www.youtube.com/shorts/FFFFFFFFFFF"/>`},
		// Atom's default relation is alternate; the first naming link decides.
		[2]string{"GGGGGGGGGGG", `<link href="https://youtube.com/shorts/GGGGGGGGGGG"/><link rel="alternate" href="https://www.youtube.com/watch?v=GGGGGGGGGGG"/>`},
	)
	videos, err := ParseFeed([]byte(feed))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"AAAAAAAAAAA": "short", "BBBBBBBBBBB": "regular", "CCCCCCCCCCC": "unknown", "DDDDDDDDDDD": "unknown", "EEEEEEEEEEE": "unknown", "FFFFFFFFFFF": "unknown", "GGGGGGGGGGG": "short"}
	if len(videos) != len(want) {
		t.Fatalf("videos=%#v", videos)
	}
	for _, v := range videos {
		got := "unknown"
		if v.Short != nil && *v.Short {
			got = "short"
		} else if v.Short != nil {
			got = "regular"
		}
		if got != want[v.ID] {
			t.Fatalf("%s format=%s, want %s", v.ID, got, want[v.ID])
		}
		// The stored link stays the canonical watch page.
		if v.URL != "https://www.youtube.com/watch?v="+v.ID {
			t.Fatalf("%s url=%s", v.ID, v.URL)
		}
	}
}

func TestRecentReportsFeedFormatsForStoredAndNewEntries(t *testing.T) {
	c := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/feeds/videos.xml":
			return response(200, syntheticFeed(
				[2]string{"AAAAAAAAAAA", `<link rel="alternate" href="https://www.youtube.com/shorts/AAAAAAAAAAA"/>`},
				[2]string{"BBBBBBBBBBB", `<link rel="alternate" href="https://www.youtube.com/watch?v=BBBBBBBBBBB"/>`},
				[2]string{"CCCCCCCCCCC", ``},
			)), nil
		case "/channel/UCAAAAAAAAAAAAAAAAAAAAAA/streams":
			return response(200, ``), nil
		case "/watch":
			id := r.URL.Query().Get("v")
			return response(200, `var ytInitialPlayerResponse={"videoDetails":{"videoId":"`+id+`","title":"checked","lengthSeconds":"31"},"playabilityStatus":{"status":"OK"}};`), nil
		}
		t.Fatalf("unexpected path %s", r.URL.Path)
		return nil, nil
	})})
	a := NewAdapter(c, Options{ChannelID: "UCAAAAAAAAAAAAAAAAAAAAAA", MaxDetailRequests: 5})
	a.Now = func() time.Time { return time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC) }
	// BBBBBBBBBBB is already stored, so the feed only adds the other two.
	stored := State{Videos: map[string]Video{"BBBBBBBBBBB": {ID: "BBBBBBBBBBB", PublishedAt: "2026-01-02T00:00:00Z"}}}
	result, err := a.FetchRecent(context.Background(), stored, 7)
	if err != nil || len(result.Errors) != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if len(result.Shorts) != 2 || !result.Shorts["AAAAAAAAAAA"] || result.Shorts["BBBBBBBBBBB"] {
		t.Fatalf("shorts=%v", result.Shorts)
	}
	// A watch-page read carries no format of its own.
	for _, v := range result.Videos {
		if !v.FeedOnly && v.Short != nil {
			t.Fatalf("watch read named a format: %#v", v)
		}
	}
}
