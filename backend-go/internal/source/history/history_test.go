package history

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Synthetic values only: no real account, channel or credential.
const (
	testAccount    = "synthetic_handle"
	testChannel    = "UCAAAAAAAAAAAAAAAAAAAAAA"
	testCredential = "synthetic-source-credential"
)

type fakeRoundTripper func(*http.Request) (*http.Response, error)

func (f fakeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), ContentLength: -1}
}

// memoryStore records pages the way the SQLite store checkpoints them.
type memoryStore struct {
	states map[string]State
	oldest string
	pages  []Page
	fail   error
}

func (m *memoryStore) SyncState(_ context.Context, source, account string) (State, error) {
	if state, ok := m.states[source+"/"+account]; ok {
		return state, nil
	}
	return State{Metadata: map[string]any{}}, nil
}

func (m *memoryStore) OldestSourceTimestamp(context.Context, string, string) (*string, error) {
	if m.oldest == "" {
		return nil, nil
	}
	return &m.oldest, nil
}

func (m *memoryStore) WriteHistoryPage(_ context.Context, page Page) (Written, error) {
	if m.fail != nil {
		return Written{}, m.fail
	}
	m.pages = append(m.pages, page)
	if m.states == nil {
		m.states = map[string]State{}
	}
	// Round-trip the metadata as JSON, which is how a real checkpoint returns.
	var metadata map[string]any
	raw, _ := json.Marshal(map[string]any{"window": page.Window, "discovered": page.Discovered})
	_ = json.Unmarshal(raw, &metadata)
	checkpoint := page.Checkpoint
	if checkpoint == "" {
		checkpoint = page.Source + "-window"
	}
	m.states[checkpoint+"/"+page.CheckpointKey] = State{Found: true, CursorID: page.Next, Metadata: metadata}
	return Written{Posts: len(page.Posts), ScheduleAssets: len(page.ScheduleCandidates)}, nil
}

func fixedNow() time.Time { return time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC) }

func newFetcher(store Store, transport fakeRoundTripper, pages int) *Fetcher {
	fetcher := NewFetcher(store, Options{MaxPages: pages, ScheduleKeywords: []string{"schedule"}}, transport)
	fetcher.Now = fixedNow
	return fetcher
}

func TestParseAndResolveWindows(t *testing.T) {
	for body, message := range map[string]string{
		`null`:                             "fetch window is required",
		`{"mode":"recent","days":0}`:       "days must be an integer from 1 to 365",
		`{"mode":"recent","days":1.5}`:     "days must be an integer from 1 to 365",
		`{"mode":"before","days":"7"}`:     "days must be an integer from 1 to 365",
		`{"mode":"sideways","days":7}`:     "unsupported fetch mode",
		`{"mode":"range","startDate":"1"}`: "dates must use YYYY-MM-DD",
		`{"mode":"range","startDate":"2026-02-30","endDate":"2026-03-01"}`: "date is invalid",
		`{"mode":"range","startDate":"2026-03-02","endDate":"2026-03-01"}`: "date range must cover 1 to 365 days",
		`{"mode":"range","startDate":"2025-01-01","endDate":"2026-03-01"}`: "date range must cover 1 to 365 days",
		`{"mode":"count"}`:              "count must be an integer from 1 to 200",
		`{"mode":"count","count":201}`:  "count must be an integer from 1 to 200",
		`{"mode":"count","count":2.5}`:  "count must be an integer from 1 to 200",
		`{"mode":"count","count":"20"}`: "count must be an integer from 1 to 200",
	} {
		if _, err := Parse(json.RawMessage(body)); err == nil || err.Error() != message || !IsValidation(err) {
			t.Fatalf("%s => %v", body, err)
		}
	}
	// Range dates are inclusive Japan days with half-open instants.
	window, err := Parse(json.RawMessage(`{"mode":"range","startDate":"2026-03-01","endDate":"2026-03-07"}`))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := window.Resolve("", fixedNow())
	if err != nil || resolved.StartTime != "2026-02-28T15:00:00.000Z" || resolved.EndTime != "2026-03-07T15:00:00.000Z" {
		t.Fatalf("range=%+v err=%v", resolved, err)
	}
	if !resolved.Contains("2026-03-07T23:59:59+09:00") || resolved.Contains("2026-03-08T00:00:00+09:00") || resolved.Contains("not a time") {
		t.Fatal("range bounds are not half-open Japan days")
	}
	// A backfill ends at the oldest stored record and needs one to exist.
	before := Window{Mode: ModeBefore, Days: 2}
	if _, err = before.Resolve("", fixedNow()); !errors.Is(err, ErrNoRecords) {
		t.Fatalf("empty backfill err=%v", err)
	}
	resolved, err = before.Resolve("2026-03-05T00:00:00.000Z", fixedNow())
	if err != nil || resolved.StartTime != "2026-03-03T00:00:00.000Z" || resolved.EndTime != "2026-03-05T00:00:00.000Z" {
		t.Fatalf("before=%+v err=%v", resolved, err)
	}
	// A count window has no bounds and its own checkpoint key.
	count, err := Parse(json.RawMessage(`{"mode":"count","count":50,"days":7}`))
	if err != nil || count != (Window{Mode: ModeCount, Count: 50}) {
		t.Fatalf("count=%+v err=%v", count, err)
	}
	if resolved, err = count.Resolve("", fixedNow()); err != nil || resolved.StartTime != "" || resolved.EndTime != "" {
		t.Fatalf("count resolved=%+v err=%v", resolved, err)
	}
	if count.checkpointKey(testAccount) == (Window{Mode: ModeCount, Count: 20}).checkpointKey(testAccount) {
		t.Fatal("count windows share a checkpoint")
	}
	// Keys match the hashes earlier releases stored, so their checkpoints resume.
	for want, selection := range map[string]Window{
		testAccount + ":6db7d803e74f1ffa7d8f5adc": before,
		testAccount + ":541ec09360c48abab075a5c9": {Mode: ModeRecent, Days: 7},
		testAccount + ":50fdf66efb9bfa9b8b39d6b1": window,
	} {
		if got := selection.checkpointKey(testAccount); got != want {
			t.Fatalf("key=%s want %s", got, want)
		}
	}
}

func TestFetchXPagesWithinBudgetAndResumesFromCheckpoint(t *testing.T) {
	store := &memoryStore{}
	seen := []string{}
	transport := fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.x.com" || r.URL.Path != "/2/tweets/search/all" || r.Header.Get("Authorization") != "Bearer "+testCredential {
			t.Errorf("unexpected request %s %s", r.URL.Host, r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("query") != "from:"+testAccount || query.Get("start_time") != "2026-02-28T15:00:00.000Z" || query.Get("end_time") != "2026-03-07T15:00:00.000Z" {
			t.Errorf("unexpected window %v", query)
		}
		seen = append(seen, query.Get("next_token"))
		switch query.Get("next_token") {
		case "":
			return response(200, `{"data":[
				{"id":"1900000000000000001","text":"weekly schedule","created_at":"2026-03-05T03:00:00.000Z","author_id":"7","attachments":{"media_keys":["m1"]},"public_metrics":{"like_count":5,"retweet_count":2,"reply_count":1}},
				{"id":"1900000000000000002","text":"outside the window","created_at":"2026-03-09T03:00:00.000Z","author_id":"7"}],
				"includes":{"users":[{"id":"7","username":"Synthetic_Handle","name":"Synthetic"}],"media":[{"media_key":"m1","url":"https://media.example.test/board.png"},{"media_key":"m2","url":"https://media.example.test/other.png"}]},
				"meta":{"next_token":"page-2"}}`), nil
		case "page-2":
			return response(200, `{"data":[{"id":"1900000000000000003","text":"second page","created_at":"2026-03-02T03:00:00.000Z","author_id":"7"}],"meta":{"next_token":"page-3"}}`), nil
		}
		return response(200, `{"meta":{"result_count":0}}`), nil
	})
	fetcher := newFetcher(store, transport, 2)
	request := Request{Source: SourceX, Account: testAccount, Credential: testCredential, Window: Window{Mode: ModeRange, StartDate: "2026-03-01", EndDate: "2026-03-07"}}
	result, err := fetcher.Fetch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Count != 2 || result.Discovered != 3 || result.Requested != 2 || result.ScheduleAssets != 1 || !result.HasMore {
		t.Fatalf("result=%+v", result)
	}
	if len(result.Errors) != 1 || result.Errors[0] != "history_page_budget_reached; repeat this fetch to continue" {
		t.Fatalf("errors=%v", result.Errors)
	}
	first := store.pages[0]
	if len(first.Posts) != 1 || first.Posts[0].ID != "1900000000000000001" || first.Posts[0].Likes != 5 || len(first.Posts[0].MediaURLs) != 1 || first.Posts[0].MediaURLs[0] != "https://media.example.test/board.png" {
		t.Fatalf("first page=%+v", first.Posts)
	}
	if first.Next != "page-2" || first.Backfill != nil || first.Discovered != 2 || store.pages[1].Discovered != 3 {
		t.Fatalf("checkpoints=%+v", store.pages)
	}
	// Repeating the same selection continues after the committed page.
	result, err = fetcher.Fetch(context.Background(), request)
	if err != nil || result.HasMore || result.Requested != 1 || len(result.Errors) != 0 {
		t.Fatalf("resume=%+v err=%v", result, err)
	}
	if strings.Join(seen, ",") != ",page-2,page-3" || store.pages[2].Discovered != 3 {
		t.Fatalf("requests=%v discovered=%d", seen, store.pages[2].Discovered)
	}
}

func TestFetchFailuresKeepCommittedPagesAndControlledCodes(t *testing.T) {
	window := Window{Mode: ModeRange, StartDate: "2026-03-01", EndDate: "2026-03-07"}
	for name, tc := range map[string]struct {
		respond func(call int) (*http.Response, error)
		code    string
		later   string
	}{
		"first page status": {respond: func(int) (*http.Response, error) { return response(429, testCredential), nil }, code: "source_api_http_429"},
		"transport":         {respond: func(int) (*http.Response, error) { return nil, errors.New("dial " + testCredential) }, code: "source_api_request_failed"},
		"redirect":          {respond: func(int) (*http.Response, error) { return response(302, ""), nil }, code: "source_api_http_302"},
		"api error": {respond: func(int) (*http.Response, error) {
			return response(200, `{"errors":[{"message":"`+testCredential+`"}],"meta":{}}`), nil
		}, code: "source_api_invalid_response"},
		"other account": {respond: func(int) (*http.Response, error) {
			return response(200, `{"data":[{"id":"1900000000000000001","text":"x","created_at":"2026-03-05T03:00:00.000Z","author_id":"9"}],"includes":{"users":[{"id":"9","username":"someone_else"}]},"meta":{}}`), nil
		}, code: "source_api_invalid_records"},
		"oversized": {respond: func(int) (*http.Response, error) {
			return response(200, `{"data":"`+strings.Repeat("a", maxResponseBytes)+`"}`), nil
		}, code: "source_api_response_too_large"},
		"later page": {respond: func(call int) (*http.Response, error) {
			if call == 1 {
				return response(200, `{"data":[{"id":"1900000000000000001","text":"kept","created_at":"2026-03-05T03:00:00.000Z"}],"meta":{"next_token":"page-2"}}`), nil
			}
			return response(503, ""), nil
		}, later: "source_api_http_503"},
		"repeated page": {respond: func(int) (*http.Response, error) {
			return response(200, `{"data":[{"id":"1900000000000000001","text":"kept","created_at":"2026-03-05T03:00:00.000Z"}],"meta":{"next_token":"page-2"}}`), nil
		}, later: "source_api_repeated_page"},
	} {
		store := &memoryStore{}
		calls := 0
		fetcher := newFetcher(store, func(*http.Request) (*http.Response, error) { calls++; return tc.respond(calls) }, 4)
		result, err := fetcher.Fetch(context.Background(), Request{Source: SourceX, Account: testAccount, Credential: testCredential, Window: window})
		if tc.code != "" {
			var controlled *Error
			if !errors.As(err, &controlled) || controlled.Code != tc.code || controlled.Requested != 1 || len(store.pages) != 0 {
				t.Fatalf("%s: err=%v pages=%d", name, err, len(store.pages))
			}
			if strings.Contains(err.Error(), testCredential) {
				t.Fatalf("%s: the error echoes the credential", name)
			}
			continue
		}
		if err != nil || len(result.Errors) != 1 || result.Errors[0] != tc.later || !result.HasMore || result.Count != 1 || len(store.pages) != 1 {
			t.Fatalf("%s: result=%+v err=%v", name, result, err)
		}
	}
	// A missing credential or an unresolvable backfill never reaches the network.
	fetcher := newFetcher(&memoryStore{}, func(*http.Request) (*http.Response, error) {
		t.Error("request sent without a usable selection")
		return response(500, ""), nil
	}, 4)
	if _, err := fetcher.Fetch(context.Background(), Request{Source: SourceX, Account: testAccount, Window: window}); err == nil || err.Error() != "X_API_BEARER_TOKEN is required for history search" {
		t.Fatalf("credential err=%v", err)
	}
	if _, err := fetcher.Fetch(context.Background(), Request{Source: SourceYoutube, Account: testChannel, Credential: testCredential, Window: Window{Mode: ModeBefore, Days: 7}}); !errors.Is(err, ErrNoRecords) {
		t.Fatalf("backfill err=%v", err)
	}
}

func TestFetchYoutubeBackfillAdvancesBehindOldestRecord(t *testing.T) {
	store := &memoryStore{oldest: "2026-03-05T00:00:00.000Z"}
	transport := fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		query := r.URL.Query()
		if r.URL.Host != "www.googleapis.com" || r.Header.Get("X-Goog-Api-Key") != testCredential || query.Get("key") != "" {
			t.Errorf("unexpected request %s %v", r.URL.Host, query)
		}
		// publishedAfter is exclusive at the API, so it starts one second early.
		if query.Get("channelId") != testChannel || query.Get("publishedAfter") != "2026-03-02T23:59:59.000Z" || query.Get("publishedBefore") != "2026-03-05T00:00:00.000Z" {
			t.Errorf("unexpected window %v", query)
		}
		return response(200, `{"items":[
			{"id":{"videoId":"AAAAAAAAAAA"},"snippet":{"title":"雑談 archive","channelId":"`+testChannel+`","publishedAt":"2026-03-04T10:00:00Z","liveBroadcastContent":"none","thumbnails":{"high":{"url":"https://media.example.test/a.jpg"}}}},
			{"id":{"videoId":"BBBBBBBBBBB"},"snippet":{"title":"upcoming","channelId":"`+testChannel+`","publishedAt":"2026-03-03T10:00:00Z","liveBroadcastContent":"upcoming","thumbnails":{"default":{"url":"https://media.example.test/b.jpg"}}}}]}`), nil
	})
	result, err := newFetcher(store, transport, 4).Fetch(context.Background(), Request{Source: SourceYoutube, Account: testChannel, Credential: testCredential, Window: Window{Mode: ModeBefore, Days: 2}})
	if err != nil || result.Count != 2 || result.HasMore || len(result.Errors) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	page := store.pages[0]
	if page.Backfill == nil || page.Backfill.Pending || page.Backfill.SearchLimitReached || page.Next != "" {
		t.Fatalf("backfill=%+v", page.Backfill)
	}
	if page.Videos[0].Kind != "TALK" || page.Videos[0].ThumbnailURL != "https://media.example.test/a.jpg" || page.Videos[1].Kind != "UPCOMING LIVE" || !page.Videos[1].IsUpcoming {
		t.Fatalf("videos=%+v", page.Videos)
	}
	// An interrupted interval is finished before a new one is opened.
	store.states["youtube-backfill/"+testChannel] = State{Found: true, CursorTime: "2026-03-05T00:00:00.000Z", Metadata: map[string]any{
		"pendingWindow": map[string]any{"mode": "before", "days": float64(2), "startTime": "2026-03-03T00:00:00.000Z", "endTime": "2026-03-05T00:00:00.000Z"},
	}}
	store.oldest = "2026-03-03T10:00:00.000Z"
	if result, err = newFetcher(store, transport, 4).Fetch(context.Background(), Request{Source: SourceYoutube, Account: testChannel, Credential: testCredential, Window: Window{Mode: ModeBefore, Days: 2}}); err != nil || result.Window.EndTime != "2026-03-05T00:00:00.000Z" {
		t.Fatalf("pending interval=%+v err=%v", result, err)
	}
}
