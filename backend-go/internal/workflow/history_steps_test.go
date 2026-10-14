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

func TestSavedWorkflowUsesSourceAPIForItsRecentWindowOnlyWithCredential(t *testing.T) {
	transport := &historyTransport{respond: func(*http.Request) (int, string) { return 200, `{"meta":{}}` }}
	source, _ := newHistorySource(t, transport)
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
		{StepID: "youtube", WorkflowID: "full-refresh", LookbackDays: 14},
	} {
		if input.StepID == "youtube" {
			source.YoutubeKey = ""
		}
		if _, err = steps.Execute(ctx, input); err == nil {
			t.Fatalf("%+v must reach the (absent) incremental adapter", input)
		}
	}
	if len(transport.accounts) != 2 {
		t.Fatalf("incremental runs called the source API: %v", transport.accounts)
	}
}
