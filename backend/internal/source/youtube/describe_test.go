package youtube

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func listedUpload(id, title string) Video {
	return Video{ID: id, Title: title, PublishedAt: "2026-01-02T00:00:00Z", URL: "https://www.youtube.com/watch?v=" + id, ThumbnailURL: "https://i.ytimg.com/vi/" + id + "/hqdefault.jpg", Kind: Classify(title)}
}

func TestDescribeVideosUsesTheRecentStepsRulesWithoutWatchPages(t *testing.T) {
	var apiIDs, watched, lists []string
	c := probeAPIClient(t, func() *http.Response {
		return response(200, apiItems(
			endedBroadcast("PPPPPPPPPPP", "Letters - cover.", "PT4M5S"),
			endedBroadcast("LLLLLLLLLLL", "game night", "PT2H"),
			endedBroadcast("NNNNNNNNNNN", "on neither list", "PT10M"),
			`{"id":"OOOOOOOOOOO","snippet":{"title":"ordinary upload","liveBroadcastContent":"none"},"contentDetails":{"duration":"PT1M"},"localizations":{"en":{"title":"Ordinary upload"}}}`,
		))
	}, func(playlist string) *http.Response {
		lists = append(lists, playlist)
		if playlist == "UULV"+syntheticChannel[2:] {
			return response(200, apiItems(playlistItem("LLLLLLLLLLL", "2026-01-03T09:00:00Z")))
		}
		return response(200, apiItems(playlistItem("PPPPPPPPPPP", "2026-01-03T09:00:00Z")))
	}, func(id string) *http.Response {
		t.Fatalf("a manual fetch read the watch page of %s", id)
		return nil
	}, &apiIDs, &watched)
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 12, APIKey: "synthetic-api-key"})
	a.Now = probeNow
	listed := []Video{
		listedUpload("PPPPPPPPPPP", "Letters - cover."), listedUpload("LLLLLLLLLLL", "game night"),
		listedUpload("NNNNNNNNNNN", "on neither list"), listedUpload("OOOOOOOOOOO", "ordinary upload"),
		listedUpload("QQQQQQQQQQQ", "not answered"),
	}
	// A stored video keeps its stored publication time.
	stored := map[string]Video{"OOOOOOOOOOO": {ID: "OOOOOOOOOOO", Title: "ordinary upload", PublishedAt: "2025-12-01T00:00:00Z", Kind: "VIDEO"}}
	described := a.DescribeVideos(context.Background(), listed, stored, nil)
	// One videos.list call and both format playlists, read once.
	if len(apiIDs) != 1 || apiIDs[0] != "PPPPPPPPPPP,LLLLLLLLLLL,NNNNNNNNNNN,OOOOOOOOOOO,QQQQQQQQQQQ" || described.APIRequests != 3 || len(described.Errors) != 0 {
		t.Fatalf("calls=%v described=%#v", apiIDs, described)
	}
	if !slices.Equal(lists, []string{"UULV" + syntheticChannel[2:], "UULF" + syntheticChannel[2:]}) || len(watched) != 0 {
		t.Fatalf("lists=%v watched=%v", lists, watched)
	}
	got := map[string]Video{}
	for _, v := range described.Videos {
		got[v.ID] = v
	}
	// The video the API did not answer is left to the recent step.
	if len(got) != 4 || got["QQQQQQQQQQQ"].ID != "" {
		t.Fatalf("videos=%v", described.Videos)
	}
	if v := got["PPPPPPPPPPP"]; !v.Premiere || !v.PremiereKnown || v.Kind != Classify("Letters - cover.") || v.EndedAt == "" || v.LiveStatus != LiveStatusEnded {
		t.Fatalf("premiere=%#v", v)
	}
	if v := got["LLLLLLLLLLL"]; v.Premiere || !v.PremiereKnown || v.Kind != "LIVE ARCHIVE" || v.DurationSeconds != 7200 {
		t.Fatalf("stream=%#v", v)
	}
	// Without a page, an ended broadcast on neither list stands unclassified.
	if v := got["NNNNNNNNNNN"]; !v.Unclassified || v.PremiereKnown {
		t.Fatalf("unclassified=%#v", v)
	}
	if v := got["OOOOOOOOOOO"]; v.PublishedAt != "2025-12-01T00:00:00Z" || v.DurationSeconds != 60 || described.Localized["OOOOOOOOOOO"]["en"] != "Ordinary upload" {
		t.Fatalf("upload=%#v localized=%v", v, described.Localized)
	}
	if v := got["PPPPPPPPPPP"]; v.PublishedAt != "2026-01-02T00:00:00Z" {
		t.Fatalf("a new video lost its listed publication time: %#v", v)
	}
}

func TestDescribeVideosBatchesAndRecordsAFailedCall(t *testing.T) {
	var apiIDs, watched []string
	calls := 0
	c := probeAPIClient(t, func() *http.Response {
		calls++
		if calls == 2 {
			return response(403, `{"error":{"message":"`+syntheticKey+`"}}`)
		}
		return response(200, apiItems(`{"id":"V0000000000","snippet":{"title":"first","liveBroadcastContent":"none"},"contentDetails":{"duration":"PT1M"}}`))
	}, nil, nil, &apiIDs, &watched)
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 12, APIKey: "synthetic-api-key"})
	a.Now = probeNow
	listed := []Video{}
	for i := range MaxAPIVideos + 1 {
		listed = append(listed, listedUpload(fmt.Sprintf("V%010d", i), "upload"))
	}
	described := a.DescribeVideos(context.Background(), listed, nil, nil)
	if len(apiIDs) != 2 || len(strings.Split(apiIDs[0], ",")) != MaxAPIVideos || apiIDs[1] != "V0000000050" || described.APIRequests != 2 {
		t.Fatalf("calls=%v described=%#v", apiIDs, described)
	}
	// The failed call keeps its code and describes none of its videos; the
	// answered one stays.
	if len(described.Videos) != 1 || described.Videos[0].ID != "V0000000000" || strings.Join(described.Errors, ",") != "youtube_api_forbidden" {
		t.Fatalf("described=%#v", described)
	}
	// Without a key nothing is asked.
	a.Options.APIKey = ""
	if described = a.DescribeVideos(context.Background(), listed, nil, nil); described.APIRequests != 0 || len(described.Videos) != 0 || len(apiIDs) != 2 {
		t.Fatalf("keyless=%#v", described)
	}
}
