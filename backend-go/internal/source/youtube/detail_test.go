package youtube

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// queuedIDs names n synthetic video IDs in queue order.
func queuedIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("Q%010d", i)
	}
	return ids
}

func ordinaryItem(id string) string {
	return `{"id":"` + id + `","snippet":{"title":"upload ` + id + `","liveBroadcastContent":"none"},"contentDetails":{"duration":"PT1M"}}`
}

// detailClient lists one stored upload on every Data API playlist, answers
// videos.list with videos and watch pages with page, and records the IDs of
// each videos.list call and every watch page.
func detailClient(t *testing.T, videos func(call int, ids []string) *http.Response, page func(id string) *http.Response, calls *[][]string, watched *[]string) *Client {
	return NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "www.googleapis.com" && (r.Header.Get("X-Goog-Api-Key") != syntheticKey || strings.Contains(r.URL.String(), syntheticKey)) {
			t.Fatalf("key not confined to the header: %s", r.URL)
		}
		switch r.URL.Path {
		case "/youtube/v3/playlistItems":
			// The per-format playlists name only the listed upload too.
			return response(200, apiItems(playlistItem("LLLLLLLLLLL", "2026-01-02T00:00:00Z"))), nil
		case "/youtube/v3/videos":
			ids := strings.Split(r.URL.Query().Get("id"), ",")
			*calls = append(*calls, ids)
			return videos(len(*calls), ids), nil
		case "/feeds/videos.xml":
			return response(200, syntheticFeed([2]string{"LLLLLLLLLLL", ""})), nil
		case "/channel/" + syntheticChannel + "/streams":
			return response(200, ``), nil
		case "/watch":
			id := r.URL.Query().Get("v")
			*watched = append(*watched, id)
			return page(id), nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})})
}

func answerAll(_ int, ids []string) *http.Response {
	items := []string{}
	for _, id := range ids {
		items = append(items, ordinaryItem(id))
	}
	return response(200, apiItems(items...))
}

func okPage(id string) *http.Response {
	return response(200, watchPage(id, `"isLiveContent":false,"lengthSeconds":"60"`, ""))
}

// storedState holds the given IDs in the rotating queue, plus the listed
// upload, checked and old, so the listing adds no detail candidate.
func storedState(ids []string) State {
	return State{Queue: ids, CheckedIDs: []string{"LLLLLLLLLLL"}, Videos: map[string]Video{"LLLLLLLLLLL": {ID: "LLLLLLLLLLL", Title: "stored", PublishedAt: "2026-01-02T00:00:00Z"}}}
}

func TestRecentAsksTheDataAPIAboutTheWholeQueueInBatches(t *testing.T) {
	var calls [][]string
	var watched []string
	c := detailClient(t, answerAll, func(id string) *http.Response {
		t.Fatalf("watch page %s requested for an answered video", id)
		return nil
	}, &calls, &watched)
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 12, APIKey: "synthetic-api-key"})
	a.Now = syntheticNow
	queue := queuedIDs(120)
	result, err := a.FetchRecent(context.Background(), storedState(queue), 7)
	if err != nil || len(result.Errors) != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	// Queue order is kept; room in the last batch goes to the listed upload.
	want := [][]string{queue[:50], queue[50:100], append(slices.Clone(queue[100:]), "LLLLLLLLLLL")}
	if len(calls) != len(want) {
		t.Fatalf("calls=%d", len(calls))
	}
	for i := range want {
		if !slices.Equal(calls[i], want[i]) {
			t.Fatalf("call %d=%v", i, calls[i])
		}
	}
	// The uploads playlist page and the three videos.list calls.
	if result.APIRequests != 4 || result.Requested != 0 || result.Inspected != 120 || len(result.Queue) != 0 {
		t.Fatalf("apiRequests=%d requested=%d inspected=%d queue=%d", result.APIRequests, result.Requested, result.Inspected, len(result.Queue))
	}
	if _, ok := result.Localized["LLLLLLLLLLL"]; !ok {
		t.Fatalf("listed upload's localized titles not read")
	}
}

func TestRecentDataAPICallsAreCappedAndTheRestUseTheWatchBudget(t *testing.T) {
	var calls [][]string
	var watched []string
	c := detailClient(t, answerAll, okPage, &calls, &watched)
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 3, APIKey: "synthetic-api-key"})
	a.Now = syntheticNow
	queue := queuedIDs(300)
	result, err := a.FetchRecent(context.Background(), storedState(queue), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != MaxAPIDetailCalls || !slices.Equal(calls[MaxAPIDetailCalls-1], queue[200:250]) {
		t.Fatalf("calls=%d last=%v", len(calls), calls[len(calls)-1])
	}
	if !slices.Equal(watched, queue[250:253]) || result.Inspected != 253 || result.APIRequests != 1+MaxAPIDetailCalls {
		t.Fatalf("watched=%v inspected=%d apiRequests=%d", watched, result.Inspected, result.APIRequests)
	}
	// The IDs no check reached keep their place ahead of nothing checked.
	if !slices.Equal(result.Queue, queue[253:]) {
		t.Fatalf("queue=%v", result.Queue)
	}
}

func TestAFailedDataAPIBatchFallsBackToWatchPages(t *testing.T) {
	var calls [][]string
	var watched []string
	c := detailClient(t, func(call int, ids []string) *http.Response {
		if call == 2 {
			return response(500, "")
		}
		return answerAll(call, ids)
	}, okPage, &calls, &watched)
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 2, APIKey: "synthetic-api-key"})
	a.Now = syntheticNow
	queue := queuedIDs(120)
	result, err := a.FetchRecent(context.Background(), storedState(queue), 7)
	if err != nil {
		t.Fatal(err)
	}
	// The failure ends the lookup; the first batch still counts, and the
	// failed batch's IDs take the watch budget in queue order.
	if len(calls) != 2 || result.APIRequests != 3 || !slices.Equal(result.Errors, []string{"youtube_api_upstream_5xx"}) {
		t.Fatalf("calls=%d result=%#v", len(calls), result)
	}
	if !slices.Equal(watched, queue[50:52]) || result.Inspected != 52 {
		t.Fatalf("watched=%v inspected=%d", watched, result.Inspected)
	}
}

func TestSignInPageStopsWatchRequestsForTheRun(t *testing.T) {
	var calls [][]string
	var watched []string
	ids := []string{"AAAAAAAAAAA", "BBBBBBBBBBB", "CCCCCCCCCCC", "DDDDDDDDDDD"}
	c := detailClient(t, func(int, []string) *http.Response {
		// Only BBBBBBBBBBB is answered: an ended broadcast no check can
		// classify, which stands in once pages stop.
		return response(200, apiItems(`{"id":"BBBBBBBBBBB","snippet":{"title":"finished stream","liveBroadcastContent":"none"},"contentDetails":{"duration":"PT2H"},"liveStreamingDetails":{"actualStartTime":"2026-01-02T09:00:00Z","actualEndTime":"2026-01-02T11:00:00Z"}}`))
	}, func(string) *http.Response {
		return response(200, botCheckPage)
	}, &calls, &watched)
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 12, APIKey: "synthetic-api-key"})
	a.Now = syntheticNow
	state := storedState(ids)
	state.Videos["BBBBBBBBBBB"] = Video{ID: "BBBBBBBBBBB", Title: "stream", ScheduledAt: "2026-01-02T09:00:00Z", Kind: "UPCOMING LIVE"}
	result, err := a.FetchRecent(context.Background(), state, 7)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(watched, []string{"AAAAAAAAAAA"}) || result.Requested != 1 || !slices.Equal(result.Errors, []string{CodeLoginRequired}) {
		t.Fatalf("watched=%v requested=%d errors=%v", watched, result.Requested, result.Errors)
	}
	if !slices.Equal(result.InspectedIDs, []string{"BBBBBBBBBBB"}) {
		t.Fatalf("inspected=%v", result.InspectedIDs)
	}
	for _, v := range result.Videos {
		if v.ID == "BBBBBBBBBBB" && (!v.Unclassified || v.Kind != "LIVE ARCHIVE") {
			t.Fatalf("stand-in=%#v", v)
		}
	}
	// The failed ID and the stand-in rotate behind the unread ones.
	if !slices.Equal(result.Queue, []string{"CCCCCCCCCCC", "DDDDDDDDDDD", "AAAAAAAAAAA", "BBBBBBBBBBB"}) {
		t.Fatalf("queue=%v", result.Queue)
	}
}

func TestSignInPageStopsTheLiveProbe(t *testing.T) {
	var watched []string
	c := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/watch" {
			t.Fatalf("unexpected request %s", r.URL)
		}
		watched = append(watched, r.URL.Query().Get("v"))
		return response(200, botCheckPage), nil
	})})
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 12})
	a.Now = probeNow
	videos := []Video{{ID: "AAAAAAAAAAA"}, {ID: "BBBBBBBBBBB"}, {ID: "CCCCCCCCCCC"}}
	result, err := a.ProbeLive(context.Background(), videos)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(watched, []string{"AAAAAAAAAAA"}) || result.Requested != 1 || result.Inspected != 0 || !slices.Equal(result.Errors, []string{CodeLoginRequired}) || len(result.Attempted) != 3 {
		t.Fatalf("watched=%v result=%#v", watched, result)
	}
}

// signInPlayer is a LOGIN_REQUIRED page with the given playability fields.
func signInPlayer(fields string) string {
	return `<script>var ytInitialPlayerResponse = {"playabilityStatus":{"status":"LOGIN_REQUIRED",` + fields + `}};</script>`
}

func TestAnAgeGateDoesNotStopWatchRequests(t *testing.T) {
	var watched []string
	c := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/feeds/videos.xml":
			return response(200, syntheticFeed([2]string{"LLLLLLLLLLL", ""})), nil
		case "/channel/" + syntheticChannel + "/streams":
			return response(200, ``), nil
		case "/watch":
			id := r.URL.Query().Get("v")
			watched = append(watched, id)
			if id == "AAAAAAAAAAA" {
				return response(200, signInPlayer(`"reason":"Sign in to confirm your age"`)), nil
			}
			return okPage(id), nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	})})
	a := NewAdapter(c, Options{ChannelID: syntheticChannel, MaxDetailRequests: 12})
	a.Now = syntheticNow
	ids := []string{"AAAAAAAAAAA", "BBBBBBBBBBB", "CCCCCCCCCCC"}
	result, err := a.FetchRecent(context.Background(), storedState(ids), 7)
	if err != nil || !slices.Equal(watched, ids) || !slices.Equal(result.Errors, []string{"youtube_watch_details_incomplete"}) {
		t.Fatalf("watched=%v result=%#v err=%v", watched, result, err)
	}
	if !slices.Equal(result.InspectedIDs, ids[1:]) {
		t.Fatalf("inspected=%v", result.InspectedIDs)
	}
}

func TestWatchTellsASignInPageFromAnIncompleteOne(t *testing.T) {
	const id = "AAAAAAAAAAA"
	for name, tc := range map[string]struct{ body, code string }{
		"bot check player":    {botCheckPage, CodeLoginRequired},
		"bot check subreason": {signInPlayer(`"errorScreen":{"playerErrorMessageRenderer":{"reason":{"simpleText":"Sign in"},"subreason":{"runs":[{"text":"to confirm you’re not a bot"}]}}}`), CodeLoginRequired},
		"no reason":           {signInPlayer(`"reason":""`), CodeLoginRequired},
		"age gate":            {signInPlayer(`"reason":"Sign in to confirm your age","messages":["This video may be inappropriate for some users."]`), "youtube_watch_details_incomplete"},
		"age gate subreason":  {signInPlayer(`"errorScreen":{"playerErrorMessageRenderer":{"subreason":{"simpleText":"This video is age-restricted"}}}`), "youtube_watch_details_incomplete"},
		"private video":       {signInPlayer(`"messages":["This is a private video. Please sign in to verify that you may see it."]`), "youtube_watch_details_incomplete"},
		"encoded player":      {`{"player_response":"{\"playabilityStatus\":{\"status\":\"LOGIN_REQUIRED\"}}"}`, CodeLoginRequired},
		"bot check page":      {`<html><title>synthetic</title><p>Sign in to confirm you’re not a bot</p></html>`, CodeLoginRequired},
		"escaped bot check":   {`<html><meta property="og:title" content="synthetic">{"text":"Sign in to confirm you're not a bot"}</html>`, CodeLoginRequired},
		"members only":        {`<script>var ytInitialPlayerResponse = {"playabilityStatus":{"status":"UNPLAYABLE","reason":"Join this channel to get access to members-only content"}};</script>`, "youtube_watch_details_incomplete"},
		"no player":           {`<html>no player</html>`, "youtube_watch_details_incomplete"},
		"mismatched video":    {watchPage("BBBBBBBBBBB", `"lengthSeconds":"60"`, ""), "youtube_watch_details_incomplete"},
	} {
		if _, err := ParseWatch([]byte(tc.body), id); err == nil || err.Error() != tc.code {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
	v, err := ParseWatch([]byte(watchPage(id, `"isLiveContent":false,"lengthSeconds":"60"`, "")), id)
	if err != nil || v.Title != "synthetic stream" || v.DurationSeconds != 60 {
		t.Fatalf("ok page=%#v err=%v", v, err)
	}
}
