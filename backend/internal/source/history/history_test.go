package history

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	ytsource "github.com/yexca/kano-navi/backend/internal/source/youtube"
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
	// stored holds the record IDs the snapshot already has.
	stored map[string]bool
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
	if m.stored == nil {
		m.stored = map[string]bool{}
	}
	written := Written{ScheduleAssets: len(page.ScheduleCandidates), NewIDs: []string{}}
	ids := []string{}
	for _, post := range page.Posts {
		ids = append(ids, post.ID)
	}
	for _, video := range page.Videos {
		ids = append(ids, video.ID)
	}
	for _, id := range ids {
		if m.stored[id] {
			written.Stored++
			continue
		}
		m.stored[id] = true
		written.New++
		written.NewIDs = append(written.NewIDs, id)
	}
	if page.CheckpointKey == "" {
		return written, nil
	}
	// Round-trip the metadata as JSON, which is how a real checkpoint returns.
	var metadata map[string]any
	raw, _ := json.Marshal(map[string]any{"window": page.Window, "discovered": page.Discovered, "headFetchedAt": page.HeadFetchedAt})
	_ = json.Unmarshal(raw, &metadata)
	checkpoint := page.Checkpoint
	if checkpoint == "" {
		checkpoint = page.Source + "-window"
	}
	m.states[checkpoint+"/"+page.CheckpointKey] = State{Found: true, CursorID: page.Next, Metadata: metadata}
	return written, nil
}

func (m *memoryStore) StoredYoutubeVideos(_ context.Context, ids []string) (map[string]ytsource.Video, error) {
	out := map[string]ytsource.Video{}
	for _, id := range ids {
		if m.stored[id] {
			out[id] = ytsource.Video{ID: id, Title: "stored " + id}
		}
	}
	return out, nil
}

func fixedNow() time.Time { return time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC) }

func newFetcher(store Store, transport fakeRoundTripper, pages int) *Fetcher {
	fetcher := NewFetcher(store, Options{MaxPages: pages, ScheduleKeywords: []string{"schedule"}}, transport)
	fetcher.Now = fixedNow
	return fetcher
}

func TestSelectValidatesTheMethodAndItsModes(t *testing.T) {
	for _, tc := range []struct {
		source, method, body string
		refresh              bool
		message              string
	}{
		{"x", "", `{"mode":"count","count":5}`, false, "method must be one of timeline, api"},
		{"x", "rss", `{"mode":"count","count":5}`, false, "method must be one of timeline, api"},
		{"youtube", "timeline", `{"mode":"count","count":5}`, false, "method must be one of api, rss"},
		{"x", "timeline", `null`, false, "fetch window is required"},
		{"x", "timeline", `{"mode":"range","startDate":"2026-03-01","endDate":"2026-03-07"}`, false, ErrRetired.Error()},
		{"x", "timeline", `{"mode":"recent","days":7}`, false, ErrRetired.Error()},
		{"x", "api", `{"mode":"all"}`, false, ErrRetired.Error()},
		{"youtube", "api", `{"mode":"range","startDate":"2026-03-01","endDate":"2026-03-07"}`, false, ErrRetired.Error()},
		{"youtube", "rss", `{"mode":"all"}`, false, ErrRetired.Error()},
		{"x", "api", `{"mode":"before","days":0}`, false, "days must be an integer from 1 to 365"},
		{"x", "api", `{"mode":"before","days":"7"}`, false, "days must be an integer from 1 to 365"},
		{"x", "api", `{"mode":"range","startDate":"1"}`, false, "dates must use YYYY-MM-DD"},
		{"x", "api", `{"mode":"range","startDate":"2026-02-30","endDate":"2026-03-01"}`, false, "date is invalid"},
		{"x", "api", `{"mode":"range","startDate":"2026-03-02","endDate":"2026-03-01"}`, false, "date range must cover 1 to 365 days"},
		{"x", "api", `{"mode":"range","startDate":"2025-01-01","endDate":"2026-03-01"}`, false, "date range must cover 1 to 365 days"},
		{"x", "timeline", `{"mode":"count"}`, false, "count must be an integer from 1 to 200"},
		{"x", "timeline", `{"mode":"count","count":201}`, false, "count must be an integer from 1 to 200"},
		{"x", "timeline", `{"mode":"count","count":2.5}`, false, "count must be an integer from 1 to 200"},
		{"youtube", "rss", `{"mode":"count","count":16}`, false, "count must be an integer from 1 to 15"},
		{"x", "timeline", `{"mode":"count","count":5}`, true, "refresh is not supported for this method"},
	} {
		if _, err := Select(tc.source, tc.method, json.RawMessage(tc.body), tc.refresh); err == nil || err.Error() != tc.message || !IsValidation(err) {
			t.Fatalf("%s %s %s => %v", tc.source, tc.method, tc.body, err)
		}
	}
	// Each method keeps only its mode's fields.
	count, err := Select("youtube", "rss", json.RawMessage(`{"mode":"count","count":15,"days":7}`), true)
	if err != nil || count != (Window{Method: MethodRSS, Mode: ModeCount, Count: 15, Refresh: true}) {
		t.Fatalf("count=%+v err=%v", count, err)
	}
	all, err := Select("x", "timeline", json.RawMessage(`{"mode":"all","count":7}`), false)
	if err != nil || all != (Window{Method: MethodTimeline, Mode: ModeAll}) {
		t.Fatalf("all=%+v err=%v", all, err)
	}
	// A window frozen without a method, or with a mode no method serves, is
	// retired.
	for _, frozen := range []Window{{Mode: ModeCount, Count: 50}, {Mode: "recent", Days: 7}, {Method: MethodAPI, Mode: ModeBefore, Days: 7}} {
		if _, err := frozen.Normalize(SourceYoutube); !errors.Is(err, ErrRetired) {
			t.Fatalf("%+v => %v", frozen, err)
		}
	}
}

func TestResolveBoundsAndCheckpointKeys(t *testing.T) {
	window, err := Select("x", "api", json.RawMessage(`{"mode":"range","startDate":"2026-03-01","endDate":"2026-03-07"}`), false)
	if err != nil {
		t.Fatal(err)
	}
	// Range dates are inclusive Japan days with half-open instants.
	resolved, err := window.Resolve("")
	if err != nil || resolved.StartTime != "2026-02-28T15:00:00.000Z" || resolved.EndTime != "2026-03-07T15:00:00.000Z" {
		t.Fatalf("range=%+v err=%v", resolved, err)
	}
	if !resolved.Contains("2026-03-07T23:59:59+09:00") || resolved.Contains("2026-03-08T00:00:00+09:00") || resolved.Contains("not a time") {
		t.Fatal("range bounds are not half-open Japan days")
	}
	// A backfill ends at the oldest stored record and needs one to exist.
	before := Window{Method: MethodAPI, Mode: ModeBefore, Days: 2}
	if _, err = before.Resolve(""); !errors.Is(err, ErrNoRecords) {
		t.Fatalf("empty backfill err=%v", err)
	}
	resolved, err = before.Resolve("2026-03-05T00:00:00.000Z")
	if err != nil || resolved.StartTime != "2026-03-03T00:00:00.000Z" || resolved.EndTime != "2026-03-05T00:00:00.000Z" {
		t.Fatalf("before=%+v err=%v", resolved, err)
	}
	count := Window{Method: MethodTimeline, Mode: ModeCount, Count: 50}
	if resolved, err = count.Resolve(""); err != nil || resolved.StartTime != "" || resolved.EndTime != "" {
		t.Fatalf("count resolved=%+v err=%v", resolved, err)
	}
	if count.checkpointKey(testAccount) == (Window{Mode: ModeCount, Count: 20}).checkpointKey(testAccount) {
		t.Fatal("count selections share a checkpoint")
	}
	// Keys are fixed hashes of the selection, so stored checkpoints resume.
	for want, selection := range map[string]Window{
		testAccount + ":6db7d803e74f1ffa7d8f5adc":   before,
		testAccount + ":50fdf66efb9bfa9b8b39d6b1":   window,
		testAccount + ":" + keyOf(`{"mode":"all"}`): {Method: MethodTimeline, Mode: ModeAll},
	} {
		if got := selection.checkpointKey(testAccount); got != want {
			t.Fatalf("key=%s want %s", got, want)
		}
	}
}

func keyOf(selection string) string {
	sum := sha256.Sum256([]byte(selection))
	return hex.EncodeToString(sum[:])[:24]
}

func TestMethodsDescribeTheirCapabilities(t *testing.T) {
	x, youtube := Methods(SourceX), Methods(SourceYoutube)
	if len(x) != 2 || x[0].ID != MethodTimeline || x[1].ID != MethodAPI || x[1].Requires != "X_API_BEARER_TOKEN" || x[0].Refresh || !x[0].Extract {
		t.Fatalf("x=%+v", x)
	}
	if len(youtube) != 2 || youtube[0].Requires != "YOUTUBE_API_KEY" || youtube[1].ID != MethodRSS || youtube[1].MaxCount != 15 || youtube[1].Extract || !youtube[1].Refresh {
		t.Fatalf("youtube=%+v", youtube)
	}
	// Callers get copies.
	x[0].Modes[0] = "changed"
	if Methods(SourceX)[0].Modes[0] != ModeCount {
		t.Fatal("the method table was changed through a copy")
	}
	if UnavailableMessage(SourceX, MethodAPI) != "X_API_BEARER_TOKEN is required for the api method" || UnavailableMessage(SourceX, MethodTimeline) != "the timeline method is unavailable" {
		t.Fatal("unavailable messages")
	}
}

func searchRange() Window {
	return Window{Method: MethodAPI, Mode: ModeRange, StartDate: "2026-03-01", EndDate: "2026-03-07"}
}

func TestFetchSearchPagesWithinBudgetAndResumesFromCheckpoint(t *testing.T) {
	store := &memoryStore{stored: map[string]bool{"1900000000000000003": true}}
	seen := []string{}
	transport := fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.x.com" || r.URL.Path != "/2/tweets/search/all" || r.Header.Get("Authorization") != "Bearer "+testCredential {
			t.Errorf("unexpected request %s %s", r.URL.Host, r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("query") != "from:"+testAccount || query.Get("start_time") != "2026-02-28T15:00:00.000Z" || query.Get("end_time") != "2026-03-07T15:00:00.000Z" || query.Get("max_results") != "100" {
			t.Errorf("unexpected window %v", query)
		}
		seen = append(seen, query.Get("next_token"))
		switch query.Get("next_token") {
		case "":
			return response(200, `{"data":[
				{"id":"1900000000000000001","text":"weekly schedule\nsecond line","created_at":"2026-03-05T03:00:00.000Z","author_id":"7","attachments":{"media_keys":["m1"]},"public_metrics":{"like_count":5,"retweet_count":2,"reply_count":1}},
				{"id":"1900000000000000002","text":"outside the window","created_at":"2026-03-09T03:00:00.000Z","author_id":"7"}],
				"includes":{"users":[{"id":"7","username":"Synthetic_Handle","name":"Synthetic"}],"media":[{"media_key":"m1","url":"https://media.example.test/board.png"},{"media_key":"m2","url":"https://media.example.test/other.png"}]},
				"meta":{"next_token":"page-2"}}`), nil
		case "page-2":
			return response(200, `{"data":[{"id":"1900000000000000003","text":"second page","created_at":"2026-03-02T03:00:00.000Z","author_id":"7"}],"meta":{"next_token":"page-3"}}`), nil
		}
		return response(200, `{"meta":{"result_count":0}}`), nil
	})
	fetcher := newFetcher(store, transport, 2)
	request := Request{Source: SourceX, Account: testAccount, Credential: testCredential, Window: searchRange()}
	result, err := fetcher.FetchSearch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Listed != 2 || result.New != 1 || result.Stored != 1 || result.Requested != 2 || result.ScheduleAssets != 1 || !result.HasMore {
		t.Fatalf("result=%+v", result)
	}
	// Only new posts are named, by the first line of their text.
	if len(result.Items) != 1 || result.Items[0] != (Item{ID: "1900000000000000001", Title: "weekly schedule", Account: testAccount}) {
		t.Fatalf("items=%+v", result.Items)
	}
	if len(result.Errors) != 1 || result.Errors[0] != "history_page_budget_reached; repeat this fetch to continue" {
		t.Fatalf("errors=%v", result.Errors)
	}
	first := store.pages[0]
	if len(first.Posts) != 1 || first.Posts[0].ID != "1900000000000000001" || first.Posts[0].Likes != 5 || len(first.Posts[0].MediaURLs) != 1 || first.Posts[0].MediaURLs[0] != "https://media.example.test/board.png" {
		t.Fatalf("first page=%+v", first.Posts)
	}
	// Stored posts keep what they have, so their fingerprints stay.
	if !first.KeepStored || first.Next != "page-2" || first.Backfill != nil || first.Discovered != 1 || store.pages[1].Discovered != 2 {
		t.Fatalf("checkpoints=%+v", store.pages)
	}
	// Repeating the same selection continues after the committed page.
	result, err = fetcher.FetchSearch(context.Background(), request)
	if err != nil || result.HasMore || result.Requested != 1 || len(result.Errors) != 0 {
		t.Fatalf("resume=%+v err=%v", result, err)
	}
	if strings.Join(seen, ",") != ",page-2,page-3" || store.pages[2].Discovered != 2 {
		t.Fatalf("requests=%v discovered=%d", seen, store.pages[2].Discovered)
	}
}

func TestFetchSearchCountReadsTheNewestPostsOfTheArchive(t *testing.T) {
	store := &memoryStore{}
	sizes := []string{}
	transport := fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		query := r.URL.Query()
		// Without a start time the search would cover only 30 days.
		if query.Get("start_time") != xArchiveStart || query.Get("end_time") != "2026-03-09T23:59:50.000Z" {
			t.Errorf("unexpected bounds %v", query)
		}
		sizes = append(sizes, query.Get("max_results"))
		if query.Get("next_token") == "" {
			return response(200, `{"data":[
				{"id":"1900000000000000005","text":"a","created_at":"2026-03-05T03:00:00.000Z"},
				{"id":"1900000000000000004","text":"b","created_at":"2026-03-04T03:00:00.000Z"}],"meta":{"next_token":"page-2"}}`), nil
		}
		return response(200, `{"data":[
			{"id":"1900000000000000003","text":"c schedule","created_at":"2026-03-03T03:00:00.000Z","attachments":{"media_keys":["m1"]}},
			{"id":"1900000000000000002","text":"d schedule","created_at":"2026-03-02T03:00:00.000Z","attachments":{"media_keys":["m2"]}}],
			"includes":{"media":[{"media_key":"m1","url":"https://media.example.test/c.png"},{"media_key":"m2","url":"https://media.example.test/d.png"}]},
			"meta":{"next_token":"page-3"}}`), nil
	})
	result, err := newFetcher(store, transport, 4).FetchSearch(context.Background(), Request{
		Source: SourceX, Account: testAccount, Credential: testCredential, Window: Window{Method: MethodAPI, Mode: ModeCount, Count: 3},
	})
	if err != nil || result.Listed != 3 || result.New != 3 || result.HasMore || result.Requested != 2 || result.ScheduleAssets != 1 || len(result.Errors) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	// The endpoint answers at least 10 posts a page; the rest of the last
	// page and its continuation are left.
	last := store.pages[1]
	if strings.Join(sizes, ",") != "10,10" || len(last.Posts) != 1 || last.Posts[0].ID != "1900000000000000003" || len(last.ScheduleCandidates) != 1 || last.Next != "" {
		t.Fatalf("sizes=%v last=%+v", sizes, last)
	}
}

func TestFetchSearchFailuresKeepCommittedPagesAndControlledCodes(t *testing.T) {
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
		result, err := fetcher.FetchSearch(context.Background(), Request{Source: SourceX, Account: testAccount, Credential: testCredential, Window: searchRange()})
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
		if err != nil || len(result.Errors) != 1 || result.Errors[0] != tc.later || !result.HasMore || result.New != 1 || len(store.pages) != 1 {
			t.Fatalf("%s: result=%+v err=%v", name, result, err)
		}
	}
	// A missing credential, another method or an unresolvable backfill never
	// reaches the network.
	fetcher := newFetcher(&memoryStore{}, func(*http.Request) (*http.Response, error) {
		t.Error("request sent without a usable selection")
		return response(500, ""), nil
	}, 4)
	if _, err := fetcher.FetchSearch(context.Background(), Request{Source: SourceX, Account: testAccount, Window: searchRange()}); err == nil || err.Error() != "X_API_BEARER_TOKEN is required for the X API method" {
		t.Fatalf("credential err=%v", err)
	}
	if _, err := fetcher.FetchSearch(context.Background(), Request{Source: SourceX, Account: testAccount, Credential: testCredential, Window: Window{Method: MethodTimeline, Mode: ModeCount, Count: 5}}); !errors.Is(err, ErrRetired) {
		t.Fatalf("method err=%v", err)
	}
	if _, err := fetcher.FetchSearch(context.Background(), Request{Source: SourceX, Account: testAccount, Credential: testCredential, Window: Window{Method: MethodAPI, Mode: ModeBefore, Days: 7}}); !errors.Is(err, ErrNoRecords) {
		t.Fatalf("backfill err=%v", err)
	}
}

func TestFetchSearchBackfillFinishesItsPendingInterval(t *testing.T) {
	store := &memoryStore{oldest: "2026-03-05T00:00:00.000Z"}
	transport := fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		query := r.URL.Query()
		if query.Get("start_time") != "2026-03-03T00:00:00.000Z" || query.Get("end_time") != "2026-03-05T00:00:00.000Z" {
			t.Errorf("unexpected window %v", query)
		}
		return response(200, `{"data":[{"id":"1900000000000000001","text":"older","created_at":"2026-03-04T03:00:00.000Z"}],"meta":{}}`), nil
	})
	before := Window{Method: MethodAPI, Mode: ModeBefore, Days: 2}
	result, err := newFetcher(store, transport, 4).FetchSearch(context.Background(), Request{Source: SourceX, Account: testAccount, Credential: testCredential, Window: before})
	if err != nil || result.New != 1 || result.HasMore || len(result.Errors) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if page := store.pages[0]; page.Backfill == nil || page.Backfill.Pending || page.Next != "" {
		t.Fatalf("backfill=%+v", page.Backfill)
	}
	// An interrupted interval is finished before a new one is opened.
	store.states["x-backfill/"+testAccount] = State{Found: true, CursorTime: "2026-03-05T00:00:00.000Z", Metadata: map[string]any{
		"pendingWindow": map[string]any{"method": "api", "mode": "before", "days": float64(2), "startTime": "2026-03-03T00:00:00.000Z", "endTime": "2026-03-05T00:00:00.000Z"},
	}}
	store.oldest = "2026-03-04T03:00:00.000Z"
	if result, err = newFetcher(store, transport, 4).FetchSearch(context.Background(), Request{Source: SourceX, Account: testAccount, Credential: testCredential, Window: before}); err != nil || result.Window.EndTime != "2026-03-05T00:00:00.000Z" {
		t.Fatalf("pending interval=%+v err=%v", result, err)
	}
}

func TestPostTitlesAreTheFirstLineCut(t *testing.T) {
	if got := postTitle("  first line \nsecond"); got != "first line" {
		t.Fatalf("title=%q", got)
	}
	if got := postTitle(strings.Repeat("あ", 100)); len([]rune(got)) != itemTitleRunes || !strings.HasSuffix(got, "…") {
		t.Fatalf("title=%q", got)
	}
}
