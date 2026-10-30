package youtube

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// formatPagesClient serves two-page live and long-form playlists and the
// given videos.list items, and records every request. A list named in fail
// answers 403 on its first page.
func formatPagesClient(t *testing.T, items string, fail string, requests *[]string) *Client {
	live, videos := "UULV"+syntheticChannel[2:], "UULF"+syntheticChannel[2:]
	pages := map[string]string{
		live + "/":      `{"nextPageToken":"lv2","items":[` + playlistItem("RRRRRRRRRRR", "2026-01-03T09:00:00Z") + `,` + playlistItem("BBBBBBBBBBB", "2026-01-03T09:00:00Z") + `]}`,
		live + "/lv2":   `{"items":[` + playlistItem("SSSSSSSSSSS", "2024-01-03T09:00:00Z") + `]}`,
		videos + "/":    `{"nextPageToken":"lf2","items":[` + playlistItem("BBBBBBBBBBB", "2026-01-03T09:00:00Z") + `]}`,
		videos + "/lf2": `{"items":[` + playlistItem("PPPPPPPPPPP", "2024-01-03T09:00:00Z") + `]}`,
	}
	return NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "www.googleapis.com" {
			t.Fatalf("a manual fetch requested %s%s", r.URL.Host, r.URL.Path)
		}
		switch r.URL.Path {
		case "/youtube/v3/videos":
			*requests = append(*requests, "videos")
			return response(200, items), nil
		case "/youtube/v3/playlistItems":
			key := r.URL.Query().Get("playlistId") + "/" + r.URL.Query().Get("pageToken")
			*requests = append(*requests, key)
			if fail != "" && key == fail+"/" {
				return response(403, `{}`), nil
			}
			if page, ok := pages[key]; ok {
				return response(200, page), nil
			}
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})})
}

func oldBroadcast(id, title string) string {
	return `{"id":"` + id + `","snippet":{"title":"` + title + `","liveBroadcastContent":"none"},"contentDetails":{"duration":"PT1H"},"liveStreamingDetails":{"scheduledStartTime":"2024-01-03T09:00:00Z","actualStartTime":"2024-01-03T09:00:10Z","actualEndTime":"2024-01-03T10:00:10Z"}}`
}

var oldBroadcasts = apiItems(
	oldBroadcast("SSSSSSSSSSS", "old stream"),
	oldBroadcast("PPPPPPPPPPP", "old premiere"),
	oldBroadcast("BBBBBBBBBBB", "on both lists"),
	oldBroadcast("NNNNNNNNNNN", "on neither list"),
)

func verdicts(described Described) map[string]string {
	out := map[string]string{}
	for _, v := range described.Videos {
		switch {
		case v.Unclassified:
			out[v.ID] = "none"
		case v.Premiere:
			out[v.ID] = "premiere"
		default:
			out[v.ID] = "stream"
		}
	}
	return out
}

func describeOld(t *testing.T, client *Client, formats *FormatSets, ids ...string) Described {
	t.Helper()
	a := NewAdapter(client, Options{ChannelID: syntheticChannel, MaxDetailRequests: 5, APIKey: "synthetic-api-key"})
	a.Now = syntheticNow
	listed := []Video{}
	for _, id := range ids {
		listed = append(listed, listedUpload(id, "listed "+id))
	}
	return a.DescribeVideos(context.Background(), listed, nil, formats)
}

func TestWholeFormatListsClassifyOldBroadcastsOnLaterPages(t *testing.T) {
	var requests []string
	client := formatPagesClient(t, oldBroadcasts, "", &requests)
	formats := NewFormatSets(5)
	described := describeOld(t, client, formats, "SSSSSSSSSSS", "PPPPPPPPPPP", "BBBBBBBBBBB", "NNNNNNNNNNN")
	got := verdicts(described)
	// The old stream and the old premiere are only on the second pages; a
	// video on both lists or on neither gets no verdict.
	if got["SSSSSSSSSSS"] != "stream" || got["PPPPPPPPPPP"] != "premiere" || got["BBBBBBBBBBB"] != "none" || got["NNNNNNNNNNN"] != "none" {
		t.Fatalf("verdicts=%v", got)
	}
	live, videos := "UULV"+syntheticChannel[2:], "UULF"+syntheticChannel[2:]
	if want := []string{"videos", live + "/", live + "/lv2", videos + "/", videos + "/lf2"}; !slices.Equal(requests, want) || described.APIRequests != 5 || len(described.Errors) != 0 {
		t.Fatalf("requests=%v api=%d errors=%v", requests, described.APIRequests, described.Errors)
	}
	// The sets serve the rest of the run without reading the lists again.
	requests = nil
	described = describeOld(t, client, formats, "SSSSSSSSSSS")
	if !slices.Equal(requests, []string{"videos"}) || described.APIRequests != 1 || verdicts(described)["SSSSSSSSSSS"] != "stream" {
		t.Fatalf("second page requests=%v described=%#v", requests, described)
	}
}

func TestFormatListsAreReadOnlyWhenAnEndedBroadcastNeedsThem(t *testing.T) {
	var requests []string
	upload := apiItems(`{"id":"OOOOOOOOOOO","snippet":{"title":"ordinary upload","liveBroadcastContent":"none"},"contentDetails":{"duration":"PT3M"}}`)
	described := describeOld(t, formatPagesClient(t, upload, "", &requests), NewFormatSets(5), "OOOOOOOOOOO")
	if !slices.Equal(requests, []string{"videos"}) || described.APIRequests != 1 || len(described.Videos) != 1 {
		t.Fatalf("requests=%v described=%#v", requests, described)
	}
}

func TestCutOrFailedFormatListsClassifyOnlyWhatTheyRead(t *testing.T) {
	// One page of each list: the old stream and premiere are not reached, so
	// they get no verdict, while what the first pages name still counts.
	var requests []string
	cutItems := apiItems(oldBroadcast("SSSSSSSSSSS", "old stream"), oldBroadcast("PPPPPPPPPPP", "old premiere"), oldBroadcast("RRRRRRRRRRR", "recent stream"))
	described := describeOld(t, formatPagesClient(t, cutItems, "", &requests), NewFormatSets(1), "SSSSSSSSSSS", "PPPPPPPPPPP", "RRRRRRRRRRR")
	got := verdicts(described)
	if got["SSSSSSSSSSS"] != "none" || got["PPPPPPPPPPP"] != "none" || got["RRRRRRRRRRR"] != "stream" {
		t.Fatalf("cut verdicts=%v", got)
	}
	if strings.Join(described.Errors, ",") != "youtube_format_list_incomplete,youtube_format_list_incomplete" || described.APIRequests != 3 {
		t.Fatalf("cut errors=%v api=%d", described.Errors, described.APIRequests)
	}
	// A failed live list records its code and names nothing; the long-form
	// list still names its videos, as a failed newest page leaves the other
	// list to decide in the recent step.
	requests = nil
	described = describeOld(t, formatPagesClient(t, oldBroadcasts, "UULV"+syntheticChannel[2:], &requests), NewFormatSets(5), "SSSSSSSSSSS", "PPPPPPPPPPP", "BBBBBBBBBBB")
	got = verdicts(described)
	if got["SSSSSSSSSSS"] != "none" || got["PPPPPPPPPPP"] != "premiere" || got["BBBBBBBBBBB"] != "premiere" {
		t.Fatalf("failed verdicts=%v", got)
	}
	if strings.Join(described.Errors, ",") != "youtube_api_forbidden" || described.APIRequests != 4 {
		t.Fatalf("failed errors=%v api=%d", described.Errors, described.APIRequests)
	}
}
