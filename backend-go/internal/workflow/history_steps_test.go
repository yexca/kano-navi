package workflow

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/source/history"
	xsource "github.com/yexca/kano-navi/backend-go/internal/source/x"
	ytsource "github.com/yexca/kano-navi/backend-go/internal/source/youtube"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

type historyTransport struct {
	mu       sync.Mutex
	accounts []string
	respond  func(*http.Request) (int, string)
}

func (h *historyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	h.mu.Lock()
	h.accounts = append(h.accounts, strings.TrimPrefix(r.URL.Query().Get("query"), "from:"))
	h.mu.Unlock()
	status, body := h.respond(r)
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), ContentLength: -1}, nil
}

// stubUploads lists two synthetic uploads, or fails with a controlled code.
type stubUploads struct {
	fail  error
	calls []string
}

func (s *stubUploads) UploadsPage(_ context.Context, token string) ([]ytsource.Video, string, error) {
	s.calls = append(s.calls, "api:"+token)
	if s.fail != nil {
		return nil, "", s.fail
	}
	return []ytsource.Video{
		{ID: "AAAAAAAAAAA", Title: "newest upload", PublishedAt: "2026-03-09T03:00:00Z", URL: "https://www.youtube.com/watch?v=AAAAAAAAAAA", Kind: "VIDEO"},
		{ID: "BBBBBBBBBBB", Title: "older upload", PublishedAt: "2026-03-08T03:00:00Z", URL: "https://www.youtube.com/watch?v=BBBBBBBBBBB", Kind: "VIDEO"},
	}, "", nil
}

func (s *stubUploads) FeedUploads(context.Context) ([]ytsource.Video, error) {
	s.calls = append(s.calls, "rss")
	if s.fail != nil {
		return nil, s.fail
	}
	return []ytsource.Video{{ID: "AAAAAAAAAAA", Title: "newest upload", PublishedAt: "2026-03-09T03:00:00Z", URL: "https://www.youtube.com/watch?v=AAAAAAAAAAA", Kind: "VIDEO"}}, nil
}

func (s *stubUploads) DescribeVideos(_ context.Context, listed []ytsource.Video, _ map[string]ytsource.Video, _ *ytsource.FormatSets) ytsource.Described {
	s.calls = append(s.calls, "details")
	return ytsource.Described{Videos: listed, Localized: map[string]map[string]string{}, Errors: []string{}, APIRequests: 1}
}

func newHistorySource(t *testing.T, transport *historyTransport) (*HistorySource, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.OpenStandalone(filepath.Join(t.TempDir(), "database", "kano.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	fetcher := history.NewFetcher(store, history.Options{MaxPages: 4}, transport)
	fetcher.Now = func() time.Time { return time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC) }
	fetcher.Uploads = &stubUploads{}
	return &HistorySource{
		Fetcher: fetcher, Store: store, XToken: "synthetic-source-credential", YoutubeKey: "synthetic-source-credential",
		Handles: []string{"synthetic_one", "synthetic_two"}, ChannelID: "UCAAAAAAAAAAAAAAAAAAAAAA", RequestLimit: 4,
	}, store
}

func apiRange() history.Window {
	return history.Window{Method: history.MethodAPI, Mode: history.ModeRange, StartDate: "2026-03-01", EndDate: "2026-03-07"}
}

func TestHistoryXKeepsWorkingAccountsRotatesBudgetAndReportsCounts(t *testing.T) {
	transport := &historyTransport{respond: func(r *http.Request) (int, string) {
		if r.URL.Query().Get("query") == "from:synthetic_two" {
			return 503, ""
		}
		return 200, `{"data":[{"id":"1900000000000000001","text":"stored","created_at":"2026-03-05T03:00:00.000Z"}],"meta":{}}`
	}}
	source, store := newHistorySource(t, transport)
	ctx := context.Background()
	out, err := source.FetchX(ctx, apiRange())
	if err != nil || out.Status != "partial" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	accounts := out.Result["accounts"].([]map[string]any)
	if out.Result["listed"] != 1 || out.Result["new"] != 1 || out.Result["stored"] != 0 || out.Result["pages"] != 2 || out.Result["reachedEnd"] != false || out.Result["discovery"] != "api" {
		t.Fatalf("result=%v", out.Result)
	}
	if len(accounts) != 1 || accounts[0]["handle"] != "synthetic_one" || accounts[0]["reachedEnd"] != true {
		t.Fatalf("accounts=%v", accounts)
	}
	if items := out.Result["items"].([]history.Item); len(items) != 1 || items[0] != (history.Item{ID: "1900000000000000001", Title: "stored", Account: "synthetic_one"}) {
		t.Fatalf("items=%v", items)
	}
	if messages := out.Result["errors"].([]string); len(messages) != 1 || messages[0] != "synthetic_two: source_api_http_503" {
		t.Fatalf("errors=%v", messages)
	}
	// The next run starts with the other account, so a short budget is
	// shared; the post it lists again counts as stored.
	if out, err = source.FetchX(ctx, apiRange()); err != nil || out.Result["new"] != 0 || out.Result["stored"] != 1 {
		t.Fatalf("again=%v err=%v", out.Result, err)
	}
	if got := strings.Join(transport.accounts, ","); got != "synthetic_one,synthetic_two,synthetic_two,synthetic_one" {
		t.Fatalf("order=%s", got)
	}
	var stored int
	if err = store.DB().QueryRow(`SELECT COUNT(*) FROM posts WHERE account_handle='synthetic_one'`).Scan(&stored); err != nil || stored != 1 {
		t.Fatalf("stored=%d err=%v", stored, err)
	}
	// A backfill needs a stored record per account; only one account has one.
	transport.accounts = nil
	out, err = source.FetchX(ctx, history.Window{Method: history.MethodAPI, Mode: history.ModeBefore, Days: 3})
	if err != nil || out.Status != "partial" {
		t.Fatalf("before=%+v err=%v", out, err)
	}
	accounts = out.Result["accounts"].([]map[string]any)
	if len(transport.accounts) != 1 || transport.accounts[0] != "synthetic_one" || len(accounts) != 2 {
		t.Fatalf("requests=%v accounts=%v", transport.accounts, accounts)
	}
}

func TestHistoryStepsFailWithTheMethodsCodeAndNeverFallBack(t *testing.T) {
	transport := &historyTransport{respond: func(*http.Request) (int, string) { return 401, "" }}
	source, _ := newHistorySource(t, transport)
	timeline := &stubTimeline{}
	source.Fetcher.Timeline = timeline
	ctx := context.Background()
	out, err := source.FetchX(ctx, apiRange())
	if err != nil || out.Status != "failed" || out.Result["error"] != "X fetch failed for every account (synthetic_one: source_api_http_401)" || len(timeline.accounts) != 0 {
		t.Fatalf("out=%+v err=%v timeline=%v", out, err, timeline.accounts)
	}
	uploads := &stubUploads{fail: &ytsource.Error{Code: "youtube_api_forbidden"}}
	source.Fetcher.Uploads = uploads
	if out, err = source.FetchYoutube(ctx, history.Window{Method: history.MethodAPI, Mode: history.ModeAll}); err != nil || out.Status != "failed" || out.Result["error"] != "youtube_api_forbidden" || strings.Join(uploads.calls, ",") != "api:" {
		t.Fatalf("youtube=%+v err=%v calls=%v", out, err, uploads.calls)
	}
	// A missing credential fails its method before any request.
	source.XToken, source.YoutubeKey = "", " "
	sent := len(transport.accounts)
	steps := &SourceSteps{History: source}
	for step, tc := range map[string]struct {
		window  history.Window
		message string
	}{
		"x":       {apiRange(), "X_API_BEARER_TOKEN is required for the api method"},
		"youtube": {history.Window{Method: history.MethodAPI, Mode: history.ModeCount, Count: 5}, "YOUTUBE_API_KEY is required for the api method"},
	} {
		out, err = steps.Execute(ctx, StepInput{StepID: step, Window: &tc.window})
		if err != nil || out.Status != "failed" || out.Result["error"] != tc.message {
			t.Fatalf("%s=%+v err=%v", step, out, err)
		}
	}
	if len(transport.accounts) != sent || len(uploads.calls) != 1 || len(timeline.accounts) != 0 {
		t.Fatal("a missing credential must never reach the network")
	}
}

func TestRetiredWindowsFailCleanly(t *testing.T) {
	transport := &historyTransport{respond: func(*http.Request) (int, string) { return 500, "" }}
	source, _ := newHistorySource(t, transport)
	timeline := &stubTimeline{}
	source.Fetcher.Timeline = timeline
	uploads := source.Fetcher.Uploads.(*stubUploads)
	steps := &SourceSteps{History: source}
	// Windows frozen before fetches named their method, and YouTube date
	// searches, no longer run.
	for step, windows := range map[string][]history.Window{
		"x":       {{Mode: "recent", Days: 7}, {Mode: history.ModeCount, Count: 20}, {Mode: history.ModeRange, StartDate: "2026-03-01", EndDate: "2026-03-07"}},
		"youtube": {{Mode: "recent", Days: 14}, {Mode: history.ModeBefore, Days: 30}, {Mode: history.ModeCount, Count: 20}},
	} {
		for _, window := range windows {
			out, err := steps.Execute(context.Background(), StepInput{StepID: step, Window: &window})
			if err != nil || out.Status != "failed" || out.Result["error"] != history.ErrRetired.Error() {
				t.Fatalf("%s %+v => %+v err=%v", step, window, out, err)
			}
		}
	}
	if len(transport.accounts) != 0 || len(timeline.accounts) != 0 || len(uploads.calls) != 0 {
		t.Fatal("a retired window reached the network")
	}
}

// stubTimeline lists one synthetic own status per account, then ends.
type stubTimeline struct {
	mu       sync.Mutex
	accounts []string
}

func (s *stubTimeline) TimelineStatuses(_ context.Context, account, cursor string, count int) (xsource.TimelineStatuses, error) {
	s.mu.Lock()
	s.accounts = append(s.accounts, account)
	s.mu.Unlock()
	id := map[string]string{"synthetic_one": "1900000000000000001", "synthetic_two": "1900000000000000002"}[account]
	post := &xsource.Post{ID: id, AccountHandle: account, Type: "daily", Label: "DAILY / 近况", Text: "from the timeline", PublishedAt: "2026-03-05T03:00:00Z", URL: "https://x.com/" + account + "/status/" + id, Raw: map[string]any{"id": id}}
	return xsource.TimelineStatuses{Statuses: []xsource.TimelineStatus{
		{ID: id, PublishedAt: post.PublishedAt, Post: post},
		{ID: "1800000000000000001", PublishedAt: "2026-02-01T00:00:00Z"},
	}}, nil
}

func TestXTimelineMethodLeavesTheIncrementalCursor(t *testing.T) {
	transport := &historyTransport{respond: func(*http.Request) (int, string) { return 500, "" }}
	source, store := newHistorySource(t, transport)
	timeline := &stubTimeline{}
	source.Fetcher.Timeline = timeline
	ctx := context.Background()
	if _, err := store.DB().Exec(`INSERT INTO sync_state(source,account_id,cursor_id,cursor_time,metadata_json) VALUES('x','synthetic_one','1900000000000000009','2026-03-09T00:00:00.000Z','{}')`); err != nil {
		t.Fatal(err)
	}
	steps := &SourceSteps{History: source}
	// The timeline method runs even with the API credential present.
	all := history.Window{Method: history.MethodTimeline, Mode: history.ModeAll}
	out, err := steps.Execute(ctx, StepInput{StepID: "x", Window: &all})
	if err != nil || out.Status != "partial" || out.Result["discovery"] != xsource.DiscoveryTimeline || out.Result["listed"] != 4 || out.Result["new"] != 2 || out.Result["failed"] != 2 || out.Result["reachedEnd"] != true {
		t.Fatalf("all=%+v err=%v", out, err)
	}
	var cursor string
	if err = store.DB().QueryRow(`SELECT cursor_id FROM sync_state WHERE source='x' AND account_id='synthetic_one'`).Scan(&cursor); err != nil || cursor != "1900000000000000009" {
		t.Fatalf("incremental cursor=%s err=%v", cursor, err)
	}
	var checkpoints int
	if err = store.DB().QueryRow(`SELECT COUNT(*) FROM sync_state WHERE source=?`, history.TimelineCheckpoint).Scan(&checkpoints); err != nil || checkpoints != 2 || len(transport.accounts) != 0 {
		t.Fatalf("checkpoints=%d err=%v api=%v", checkpoints, err, transport.accounts)
	}
	// Without the timeline adapter the method is unavailable.
	source.Fetcher.Timeline = nil
	if out, err = steps.Execute(ctx, StepInput{StepID: "x", Window: &all}); err != nil || out.Result["error"] != "the timeline method is unavailable" {
		t.Fatalf("no timeline=%+v err=%v", out, err)
	}
}

func TestYoutubeMethodsListUploadsWithoutTheRecentStep(t *testing.T) {
	transport := &historyTransport{respond: func(*http.Request) (int, string) { return 500, "" }}
	source, store := newHistorySource(t, transport)
	uploads := source.Fetcher.Uploads.(*stubUploads)
	steps := &SourceSteps{History: source}
	ctx := context.Background()
	rss := history.Window{Method: history.MethodRSS, Mode: history.ModeCount, Count: 15}
	out, err := steps.Execute(ctx, StepInput{StepID: "youtube", Window: &rss})
	if err != nil || out.Status != "succeeded" || out.Result["listed"] != 1 || out.Result["new"] != 1 || out.Result["pages"] != 1 || out.Result["reachedEnd"] != true {
		t.Fatalf("rss=%+v err=%v", out, err)
	}
	all := history.Window{Method: history.MethodAPI, Mode: history.ModeAll}
	out, err = steps.Execute(ctx, StepInput{StepID: "youtube", Window: &all})
	// The new upload gets its details checked: one more quota unit.
	if err != nil || out.Result["listed"] != 2 || out.Result["new"] != 1 || out.Result["stored"] != 1 || out.Result["apiRequests"] != 2 || out.Result["inspected"] != 1 {
		t.Fatalf("api=%+v err=%v", out, err)
	}
	if items := out.Result["items"].([]history.Item); len(items) != 1 || items[0] != (history.Item{ID: "BBBBBBBBBBB", Title: "older upload"}) {
		t.Fatalf("items=%v", items)
	}
	if strings.Join(uploads.calls, ",") != "rss,api:,details" || len(transport.accounts) != 0 {
		t.Fatalf("calls=%v", uploads.calls)
	}
	var count int
	if err = store.DB().QueryRow(`SELECT COUNT(*) FROM sync_state WHERE source='youtube'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("incremental youtube state=%d err=%v", count, err)
	}
}

func TestMethodAvailabilityFollowsCredentialsAndAdapters(t *testing.T) {
	transport := &historyTransport{respond: func(*http.Request) (int, string) { return 500, "" }}
	source, _ := newHistorySource(t, transport)
	source.Fetcher.Timeline = &stubTimeline{}
	available := func(name string) string {
		out := []string{}
		for _, method := range source.Methods(name) {
			if method.Available {
				out = append(out, method.ID)
			}
		}
		return strings.Join(out, ",")
	}
	if x, youtube := available("x"), available("youtube"); x != "timeline,api" || youtube != "api,rss" {
		t.Fatalf("with credentials x=%s youtube=%s", x, youtube)
	}
	source.XToken, source.YoutubeKey = "", ""
	if x, youtube := available("x"), available("youtube"); x != "timeline" || youtube != "rss" {
		t.Fatalf("without credentials x=%s youtube=%s", x, youtube)
	}
	source.Fetcher.Timeline, source.Fetcher.Uploads = nil, nil
	if x, youtube := available("x"), available("youtube"); x != "" || youtube != "" {
		t.Fatalf("without adapters x=%s youtube=%s", x, youtube)
	}
	if (*HistorySource)(nil).MethodAvailable("x", history.MethodTimeline) || len(source.Methods("bilibili")) != 0 {
		t.Fatal("a missing source or fetcher offers methods")
	}
}

func TestSavedWorkflowsReadBySyncSettingWhateverTheCredentials(t *testing.T) {
	transport := &historyTransport{respond: func(*http.Request) (int, string) { return 200, `{"meta":{}}` }}
	source, _ := newHistorySource(t, transport)
	timeline := &stubTimeline{}
	source.Fetcher.Timeline = timeline
	steps := &SourceSteps{History: source}
	ctx := context.Background()
	// Incremental settings, full syncs and old runs with lookback days reach
	// the incremental adapters (absent here), never a date search.
	for _, input := range []StepInput{
		{StepID: "x", WorkflowID: "full-refresh", Sync: sqlite.SourceSync{Mode: sqlite.SyncIncremental, Max: 20}},
		{StepID: "x", WorkflowID: "full-refresh", LookbackDays: 7},
		{StepID: "x", WorkflowID: sqlite.AdhocSyncWorkflow},
		{StepID: "youtube", WorkflowID: "full-refresh", Sync: sqlite.SourceSync{Mode: sqlite.SyncCount, Count: 30}},
		{StepID: "youtube", WorkflowID: "full-refresh", LookbackDays: 14},
	} {
		if _, err := steps.Execute(ctx, input); err == nil {
			t.Fatalf("%+v must reach the (absent) incremental adapter", input)
		}
	}
	if len(transport.accounts) != 0 || len(timeline.accounts) != 0 {
		t.Fatalf("incremental runs searched: api=%v timeline=%v", transport.accounts, timeline.accounts)
	}
	// A count setting reads each account's newest posts from the timeline,
	// with the credential too.
	out, err := steps.Execute(ctx, StepInput{StepID: "x", WorkflowID: "full-refresh", Sync: sqlite.SourceSync{Mode: sqlite.SyncCount, Count: 30}})
	if err != nil || out.Result["discovery"] != xsource.DiscoveryTimeline || len(timeline.accounts) != 2 || len(transport.accounts) != 0 {
		t.Fatalf("count out=%+v err=%v api=%v", out, err, transport.accounts)
	}
}

// A count setting serves operator refreshes after its timeline read: each
// takes one detail request, its answer is stored and the request cleared,
// and the incremental state stays as it is.
func TestCountSettingServesOperatorRefreshes(t *testing.T) {
	transport := &historyTransport{respond: func(*http.Request) (int, string) { return 200, `{"meta":{}}` }}
	source, store := newHistorySource(t, transport)
	source.Fetcher.Timeline = &stubTimeline{}
	id := "1900000000000000050"
	metadata := `{"pendingStatusIds":["1900000000000000040"],"timelineGap":{"floor":"1900000000000000030"}}`
	for _, statement := range []string{
		`INSERT INTO posts(id,source,account_handle,text,published_at,url,raw_json,refresh_requested_at) VALUES('` + id + `','x','synthetic_two','stale','2026-03-04T00:00:00.000Z','https://x.com/synthetic_two/status/` + id + `','{}','2026-03-09T00:00:00.000Z')`,
		`INSERT INTO sync_state(source,account_id,cursor_id,cursor_time,metadata_json) VALUES('x','synthetic_two','1900000000000000009','2026-03-09T00:00:00.000Z','` + metadata + `')`,
	} {
		if _, err := store.DB().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	paths := []string{}
	rt := xTestRT(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Host+r.URL.Path)
		body := `{"tweetID":"` + id + `","text":"refreshed wording","date":"2026-03-04T00:00:00Z","user_screen_name":"synthetic_two"}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	adapter := xsource.NewAdapter(xsource.NewClient(&http.Client{Transport: rt}), xsource.Options{Handles: source.Handles, MaxDetailRequests: 3, BootstrapDays: 7})
	steps := &SourceSteps{History: source, X: &XRecentExecutor{Store: store, Adapter: adapter, Options: adapter.Options}}
	out, err := steps.Execute(context.Background(), StepInput{StepID: "x", WorkflowID: "full-refresh", Sync: sqlite.SourceSync{Mode: sqlite.SyncCount, Count: 30}})
	if err != nil || out.Result["discovery"] != xsource.DiscoveryTimeline || out.Result["refreshed"] != 1 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if strings.Join(paths, ",") != "api.vxtwitter.com/synthetic_two/status/"+id {
		t.Fatalf("requests=%v", paths)
	}
	var text, requested, cursor, stored string
	if err = store.DB().QueryRow(`SELECT text,COALESCE(refresh_requested_at,'') FROM posts WHERE id=?`, id).Scan(&text, &requested); err != nil || text != "refreshed wording" || requested != "" {
		t.Fatalf("text=%q requested=%q err=%v", text, requested, err)
	}
	if err = store.DB().QueryRow(`SELECT cursor_id,metadata_json FROM sync_state WHERE source='x' AND account_id='synthetic_two'`).Scan(&cursor, &stored); err != nil || cursor != "1900000000000000009" || stored != metadata {
		t.Fatalf("cursor=%s metadata=%s err=%v", cursor, stored, err)
	}
}
