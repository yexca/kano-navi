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

func newHistorySource(t *testing.T, transport *historyTransport) (*HistorySource, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.OpenStandalone(filepath.Join(t.TempDir(), "database", "kano.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	fetcher := history.NewFetcher(store, history.Options{MaxPages: 4}, transport)
	fetcher.Now = func() time.Time { return time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC) }
	return &HistorySource{
		Fetcher: fetcher, Store: store, XToken: "synthetic-source-credential", YoutubeKey: "synthetic-source-credential",
		Handles: []string{"synthetic_one", "synthetic_two"}, ChannelID: "UCAAAAAAAAAAAAAAAAAAAAAA", RequestLimit: 4,
	}, store
}

func TestHistoryXKeepsWorkingAccountsRotatesBudgetAndReportsWindows(t *testing.T) {
	transport := &historyTransport{respond: func(r *http.Request) (int, string) {
		if r.URL.Query().Get("query") == "from:synthetic_two" {
			return 503, ""
		}
		return 200, `{"data":[{"id":"1900000000000000001","text":"stored","created_at":"2026-03-05T03:00:00.000Z"}],"meta":{}}`
	}}
	source, store := newHistorySource(t, transport)
	ctx := context.Background()
	window := history.Window{Mode: history.ModeRange, StartDate: "2026-03-01", EndDate: "2026-03-07"}
	out, err := source.FetchX(ctx, window)
	if err != nil || out.Status != "partial" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	accounts := out.Result["accounts"].([]map[string]any)
	if out.Result["count"] != 1 || out.Result["requested"] != 2 || len(accounts) != 1 || accounts[0]["handle"] != "synthetic_one" {
		t.Fatalf("result=%v", out.Result)
	}
	if resolved := accounts[0]["window"].(history.Window); resolved.StartTime != "2026-02-28T15:00:00.000Z" || resolved.EndTime != "2026-03-07T15:00:00.000Z" {
		t.Fatalf("window=%+v", resolved)
	}
	if messages := out.Result["errors"].([]string); len(messages) != 1 || messages[0] != "synthetic_two: source_api_http_503" {
		t.Fatalf("errors=%v", messages)
	}
	// The next run starts with the other account, so a short budget is shared.
	if _, err = source.FetchX(ctx, window); err != nil {
		t.Fatal(err)
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
	out, err = source.FetchX(ctx, history.Window{Mode: history.ModeBefore, Days: 3})
	if err != nil || out.Status != "partial" {
		t.Fatalf("before=%+v err=%v", out, err)
	}
	accounts = out.Result["accounts"].([]map[string]any)
	if len(transport.accounts) != 1 || transport.accounts[0] != "synthetic_one" || len(accounts) != 2 {
		t.Fatalf("requests=%v accounts=%v", transport.accounts, accounts)
	}
}

func TestHistoryStepsRequireCredentialAndFailWithoutAnyAccount(t *testing.T) {
	transport := &historyTransport{respond: func(*http.Request) (int, string) { return 401, "" }}
	source, _ := newHistorySource(t, transport)
	ctx := context.Background()
	window := history.Window{Mode: history.ModeRange, StartDate: "2026-03-01", EndDate: "2026-03-07"}
	out, err := source.FetchX(ctx, window)
	if err != nil || out.Status != "failed" || out.Result["error"] != "X history search failed for every account (synthetic_one: source_api_http_401)" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if out, err = source.FetchYoutube(ctx, window); err != nil || out.Status != "failed" || out.Result["error"] != "source_api_http_401" {
		t.Fatalf("youtube=%+v err=%v", out, err)
	}
	source.XToken, source.YoutubeKey = "", " "
	sent := len(transport.accounts)
	steps := &SourceSteps{History: source}
	for step, message := range map[string]string{"x": "X_API_BEARER_TOKEN is required for history search", "youtube": "YOUTUBE_API_KEY is required for history search"} {
		out, err = steps.Execute(ctx, StepInput{StepID: step, Window: &window})
		if err != nil || out.Status != "failed" || out.Result["error"] != message {
			t.Fatalf("%s=%+v err=%v", step, out, err)
		}
	}
	if len(transport.accounts) != sent || source.Available("x") || source.Available("youtube") || (*HistorySource)(nil).Available("x") {
		t.Fatal("a missing credential must never reach the network")
	}
}

// stubTimeline lists one synthetic own status per account inside March 2026.
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

func TestXWindowsPageTheTimelineWithoutCredential(t *testing.T) {
	transport := &historyTransport{respond: func(*http.Request) (int, string) { return 500, "" }}
	source, store := newHistorySource(t, transport)
	timeline := &stubTimeline{}
	source.Fetcher.Timeline = timeline
	source.XToken = ""
	ctx := context.Background()
	if _, err := store.DB().Exec(`INSERT INTO sync_state(source,account_id,cursor_id,cursor_time,metadata_json) VALUES('x','synthetic_one','1900000000000000009','2026-03-09T00:00:00.000Z','{}')`); err != nil {
		t.Fatal(err)
	}
	steps := &SourceSteps{History: source}
	window := history.Window{Mode: history.ModeRange, StartDate: "2026-03-01", EndDate: "2026-03-07"}
	out, err := steps.Execute(ctx, StepInput{StepID: "x", Window: &window})
	if err != nil || out.Status != "succeeded" || out.Result["discovery"] != xsource.DiscoveryTimeline || out.Result["count"] != 2 || out.Result["discovered"] != 2 {
		t.Fatalf("range=%+v err=%v", out, err)
	}
	// A recent window pages back to its start too, past the incremental cursor.
	recent := history.Window{Mode: history.ModeRecent, Days: 7}
	if out, err = steps.Execute(ctx, StepInput{StepID: "x", WorkflowID: sqlite.AdhocFetchWorkflow, Window: &recent, LookbackDays: 7}); err != nil || out.Result["discovery"] != xsource.DiscoveryTimeline {
		t.Fatalf("recent=%+v err=%v", out, err)
	}
	var cursor string
	if err = store.DB().QueryRow(`SELECT cursor_id FROM sync_state WHERE source='x' AND account_id='synthetic_one'`).Scan(&cursor); err != nil || cursor != "1900000000000000009" {
		t.Fatalf("incremental cursor=%s err=%v", cursor, err)
	}
	var checkpoints int
	if err = store.DB().QueryRow(`SELECT COUNT(*) FROM sync_state WHERE source=?`, history.TimelineCheckpoint).Scan(&checkpoints); err != nil || checkpoints != 4 {
		t.Fatalf("checkpoints=%d err=%v", checkpoints, err)
	}
	// A backfill still needs the API; it never pages the timeline.
	sent := len(timeline.accounts)
	before := history.Window{Mode: history.ModeBefore, Days: 3}
	if out, err = source.FetchX(ctx, before); err != nil || out.Status != "failed" || out.Result["error"] != "X_API_BEARER_TOKEN is required for history search" || len(timeline.accounts) != sent {
		t.Fatalf("before=%+v err=%v", out, err)
	}
	// A count window reads the timeline even with the credential.
	withCredential, _ := newHistorySource(t, transport)
	withCredential.Fetcher.Timeline = timeline
	count := history.Window{Mode: history.ModeCount, Count: 20}
	if out, err = (&SourceSteps{History: withCredential}).Execute(ctx, StepInput{StepID: "x", Window: &count}); err != nil || out.Result["discovery"] != xsource.DiscoveryTimeline || len(transport.accounts) != 0 {
		t.Fatalf("count=%+v err=%v api=%v", out, err, transport.accounts)
	}
	if out, err = steps.Execute(ctx, StepInput{StepID: "youtube", Window: &count}); err != nil || out.Status != "failed" {
		t.Fatalf("youtube count=%+v err=%v", out, err)
	}
}

func TestWindowAvailabilityFollowsCredentialAndTimeline(t *testing.T) {
	transport := &historyTransport{respond: func(*http.Request) (int, string) { return 500, "" }}
	source, _ := newHistorySource(t, transport)
	source.Fetcher.Timeline = &stubTimeline{}
	modes := func(name string) string {
		out := []string{}
		for _, mode := range []string{history.ModeRecent, history.ModeBefore, history.ModeRange, history.ModeCount} {
			if source.WindowAvailable(name, mode) {
				out = append(out, mode)
			}
		}
		return strings.Join(out, ",")
	}
	if x, youtube := modes("x"), modes("youtube"); x != "recent,before,range,count" || youtube != "recent,before,range" {
		t.Fatalf("with credentials x=%s youtube=%s", x, youtube)
	}
	source.XToken, source.YoutubeKey = "", ""
	if x, youtube := modes("x"), modes("youtube"); x != "recent,range,count" || youtube != "recent" {
		t.Fatalf("without credentials x=%s youtube=%s", x, youtube)
	}
	source.Fetcher.Timeline = nil
	if x := modes("x"); x != "recent" {
		t.Fatalf("without timeline x=%s", x)
	}
}

func TestSavedWorkflowUsesSourceAPIForItsRecentWindowOnlyWithCredential(t *testing.T) {
	transport := &historyTransport{respond: func(*http.Request) (int, string) { return 200, `{"meta":{}}` }}
	source, _ := newHistorySource(t, transport)
	// The timeline serves operator windows only; timers stay incremental.
	timeline := &stubTimeline{}
	source.Fetcher.Timeline = timeline
	defer func() {
		if len(timeline.accounts) != 0 {
			t.Errorf("a saved workflow paged the timeline: %v", timeline.accounts)
		}
	}()
	steps := &SourceSteps{History: source}
	ctx := context.Background()
	out, err := steps.Execute(ctx, StepInput{StepID: "x", WorkflowID: "full-refresh", LookbackDays: 7})
	if err != nil || out.Status != "succeeded" || len(transport.accounts) != 2 {
		t.Fatalf("saved workflow out=%+v err=%v requests=%v", out, err, transport.accounts)
	}
	if window := out.Result["accounts"].([]map[string]any)[0]["window"].(history.Window); window.Mode != history.ModeRecent || window.Days != 7 {
		t.Fatalf("window=%+v", window)
	}
	// A full sync has no window and stays on the incremental adapter, as does
	// a saved workflow when the credential is absent.
	for _, input := range []StepInput{
		{StepID: "x", WorkflowID: sqlite.AdhocSyncWorkflow, LookbackDays: 7},
		{StepID: "x", WorkflowID: "full-refresh", LookbackDays: 7},
		{StepID: "youtube", WorkflowID: "full-refresh", LookbackDays: 14},
	} {
		if input.WorkflowID == "full-refresh" {
			source.XToken, source.YoutubeKey = "", ""
		}
		if _, err = steps.Execute(ctx, input); err == nil {
			t.Fatalf("%+v must reach the (absent) incremental adapter", input)
		}
	}
	if len(transport.accounts) != 2 {
		t.Fatalf("incremental runs called the source API: %v", transport.accounts)
	}
}
