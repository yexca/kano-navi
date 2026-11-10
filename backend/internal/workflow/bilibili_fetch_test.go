package workflow

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	bilisource "github.com/yexca/kano-navi/backend/internal/source/bilibili"
	"github.com/yexca/kano-navi/backend/internal/source/history"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

// syntheticUploads answers the upload listing's page pn of size ps out of
// five synthetic uploads, newest first.
func syntheticUploads(r *http.Request) string {
	ps, _ := strconv.Atoi(r.URL.Query().Get("ps"))
	pn, _ := strconv.Atoi(r.URL.Query().Get("pn"))
	archives := []string{}
	for i := (pn - 1) * ps; i < min(pn*ps, 5); i++ {
		archives = append(archives, fmt.Sprintf(`{"aid":%d,"bvid":"BV1AAAAAAAA%c","title":"upload %d","pubdate":%d,"pic":"http://i0.hdslb.com/bfs/archive/a.jpg","duration":60,"upMid":316381099}`,
			i+1, "BCDEF"[i], i+1, 1767258000-i*3600))
	}
	return `{"code":0,"data":{"archives":[` + strings.Join(archives, ",") + `],"page":{"num":` + strconv.Itoa(pn) + `,"size":` + strconv.Itoa(ps) + `,"total":5}}}`
}

func bilibiliFetchSteps(t *testing.T, respond func(*http.Request) (int, string)) (*SourceSteps, *sqlite.Store, *[]string) {
	t.Helper()
	s, err := sqlite.OpenInitialized(filepath.Join(t.TempDir(), "data", "database", "snapshot.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	paths := []string{}
	rt := xTestRT(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		status, body := 200, ""
		switch r.URL.Path {
		case "/x/frontend/finger/spi":
			body = `{"code":0,"data":{"b_3":"0A1B2C3D-0A1B-0A1B-0A1B-0A1B2C3D4E5F12345infoc","b_4":"synthetic-buvid4=="}}`
		case "/bapis/bilibili.api.ticket.v1.Ticket/GenWebTicket":
			body = strings.Replace(syntheticTicket, "1767225600", strconv.FormatInt(time.Now().Unix(), 10), 1)
		case "/x/web-interface/nav":
			body = syntheticNav
		default:
			status, body = respond(r)
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	adapter := bilisource.NewAdapter(bilisource.NewClient(&http.Client{Transport: rt}), bilisource.Options{Account: bilisource.DefaultAccount, MinInterval: 3 * time.Hour, PageSize: 2})
	return &SourceSteps{Store: s, Bilibili: adapter, BilibiliPageGap: -1}, s, &paths
}

func fetchStep(t *testing.T, steps *SourceSteps, step string, window history.Window) AttemptResult {
	t.Helper()
	result, err := steps.Execute(context.Background(), StepInput{StepID: step, WorkflowID: sqlite.AdhocFetchWorkflow, Window: &window})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestBilibiliUploadFetchPagesWithinTheBudgetAndResumes(t *testing.T) {
	steps, s, paths := bilibiliFetchSteps(t, func(r *http.Request) (int, string) {
		if r.URL.Path != "/x/series/recArchivesByKeywords" {
			t.Fatalf("unexpected request %s", r.URL)
		}
		return 200, syntheticUploads(r)
	})
	ctx := context.Background()
	settings, err := s.SyncSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.FetchMaxPages = 2
	if err = s.SaveSyncSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	window := history.Window{Method: history.MethodSpace, Mode: history.ModeCount, Count: 5}
	// Two pages of two uploads, then the budget stops the fetch.
	first := fetchStep(t, steps, "bilibili", window)
	if first.Status != "partial" || first.Result["listed"] != 4 || first.Result["new"] != 4 || first.Result["pages"] != 2 || first.Result["reachedEnd"] != false {
		t.Fatalf("first=%#v", first.Result)
	}
	if errs, _ := first.Result["errors"].([]string); len(errs) != 1 || errs[0] != history.BudgetMessage {
		t.Fatalf("first errors=%#v", first.Result["errors"])
	}
	if items, _ := first.Result["items"].([]history.Item); len(items) != 4 || items[0].ID != "BV1AAAAAAAAB" || items[0].Title != "upload 1" {
		t.Fatalf("items=%#v", first.Result["items"])
	}
	// Repeating it reads the third page only: the fifth upload ends the count.
	*paths = nil
	second := fetchStep(t, steps, "bilibili", window)
	if second.Status != "succeeded" || second.Result["listed"] != 1 || second.Result["new"] != 1 || second.Result["pages"] != 1 || second.Result["reachedEnd"] != true {
		t.Fatalf("second=%#v", second.Result)
	}
	if strings.Join(*paths, ",") != "/x/series/recArchivesByKeywords" {
		t.Fatalf("second paths=%v", *paths)
	}
	// A completed fetch begins again at the newest page; its uploads are stored.
	third := fetchStep(t, steps, "bilibili", history.Window{Method: history.MethodSpace, Mode: history.ModeCount, Count: 3})
	if third.Result["listed"] != 3 || third.Result["new"] != 0 || third.Result["stored"] != 3 || third.Result["reachedEnd"] != true {
		t.Fatalf("third=%#v", third.Result)
	}
	page, err := s.ListAdminVideos(ctx, sqlite.AdminVideoFilter{Source: "bilibili"})
	if err != nil || page.Total != 5 {
		t.Fatalf("videos=%#v err=%v", page, err)
	}
	// The fetch leaves the timer's interval clock alone: the timed read still
	// reads now.
	state, err := s.BilibiliState(ctx, bilisource.DefaultAccount)
	if err != nil || !state.LastAttemptAt.IsZero() || state.LastSuccessAt.IsZero() {
		t.Fatalf("state=%#v err=%v", state, err)
	}
	timed, err := steps.Execute(ctx, StepInput{StepID: "bilibili", WorkflowID: sqlite.AdhocSyncWorkflow})
	if err != nil || timed.Status != "succeeded" || timed.Result["reason"] != nil {
		t.Fatalf("timed=%#v err=%v", timed, err)
	}
}

func TestBilibiliFetchRespectsTheBackoffAndKeepsPagesBeforeACaptcha(t *testing.T) {
	captcha := false
	steps, s, paths := bilibiliFetchSteps(t, func(r *http.Request) (int, string) {
		if captcha && r.URL.Query().Get("pn") == "2" {
			return 412, "<!DOCTYPE html>"
		}
		return 200, syntheticUploads(r)
	})
	ctx := context.Background()
	captcha = true
	window := history.Window{Method: history.MethodSpace, Mode: history.ModeAll}
	// The second page meets the captcha: the first page stays, the backoff
	// starts and the fetch stops without reading further.
	partial := fetchStep(t, steps, "bilibili", window)
	if partial.Status != "partial" || partial.Result["new"] != 2 || partial.Result["pages"] != 2 || partial.Result["reachedEnd"] != false {
		t.Fatalf("partial=%#v", partial.Result)
	}
	if errs, _ := partial.Result["errors"].([]string); len(errs) != 1 || errs[0] != "bilibili_risk_control_http" {
		t.Fatalf("partial errors=%#v", partial.Result["errors"])
	}
	state, err := s.BilibiliState(ctx, bilisource.DefaultAccount)
	if err != nil || !state.BlockedUntil.After(time.Now()) {
		t.Fatalf("state=%#v err=%v", state, err)
	}
	// While the backoff lasts a fetch fails at once without a request; it is
	// never forced.
	*paths = nil
	blocked := fetchStep(t, steps, "bilibili", window)
	if blocked.Status != "failed" || blocked.Result["error"] != "bilibili_blocked" || len(*paths) != 0 {
		t.Fatalf("blocked=%#v paths=%v", blocked.Result, *paths)
	}
}

func TestBilibiliFetchClearsARejectedContinuationAfterCommittedPages(t *testing.T) {
	steps, s, _ := bilibiliFetchSteps(t, func(r *http.Request) (int, string) {
		if r.URL.Query().Get("pn") == "1" {
			return 200, syntheticUploads(r)
		}
		return 200, `{"code":-400,"message":"synthetic bad request"}`
	})
	ctx := context.Background()
	window := history.Window{Method: history.MethodSpace, Mode: history.ModeAll}
	// The first page commits continuation 2, which Bilibili rejects: the
	// first page stays and the continuation is cleared at once.
	partial := fetchStep(t, steps, "bilibili", window)
	if partial.Status != "partial" || partial.Result["new"] != 2 || partial.Result["pages"] != 2 || partial.Result["reachedEnd"] != false {
		t.Fatalf("partial=%#v", partial.Result)
	}
	if errs, _ := partial.Result["errors"].([]string); len(errs) != 1 || errs[0] != "bilibili_api_error" {
		t.Fatalf("partial errors=%#v", partial.Result["errors"])
	}
	checkpoint, err := s.SyncState(ctx, "bilibili-window", window.CheckpointKey(bilisource.DefaultAccount))
	if err != nil || checkpoint.CursorID != "" {
		t.Fatalf("checkpoint=%#v err=%v", checkpoint, err)
	}
}

func TestBilibiliDynamicsFetchNeedsTheLoginAndFollowsTheOffset(t *testing.T) {
	steps, s, paths := bilibiliFetchSteps(t, func(r *http.Request) (int, string) {
		if r.URL.Path != "/x/polymer/web-dynamic/v1/feed/space" {
			t.Fatalf("unexpected request %s", r.URL)
		}
		item := func(id, text string) string {
			return `{"id_str":"` + id + `","type":"DYNAMIC_TYPE_WORD","modules":{"module_author":{"mid":316381099,"pub_ts":1794024288},"module_dynamic":{"desc":{"rich_text_nodes":[{"type":"RICH_TEXT_NODE_TYPE_TEXT","text":"` + text + `"}]}}}}`
		}
		if r.URL.Query().Get("offset") == "" {
			return 200, `{"code":0,"data":{"has_more":true,"offset":"900000000000000010","items":[` + item("900000000000000011", "今天20点开播") + `,` + item("900000000000000010", "明天休息") + `]}}`
		}
		if r.URL.Query().Get("offset") != "900000000000000010" {
			t.Fatalf("offset=%s", r.URL.Query().Get("offset"))
		}
		return 200, `{"code":0,"data":{"has_more":false,"offset":"","items":[` + item("900000000000000009", "周末歌回") + `]}}`
	})
	window := history.Window{Method: history.MethodSpace, Mode: history.ModeAll}
	// Without a login the fetch fails before any request.
	anonymous := fetchStep(t, steps, sqlite.BilibiliDynamicsSource, window)
	if anonymous.Status != "failed" || anonymous.Result["error"] != "bilibili_login_required" || len(*paths) != 0 {
		t.Fatalf("anonymous=%#v paths=%v", anonymous.Result, *paths)
	}
	steps.Bilibili.Options.SESSDATA = syntheticLogin
	all := fetchStep(t, steps, sqlite.BilibiliDynamicsSource, window)
	if all.Status != "succeeded" || all.Result["listed"] != 3 || all.Result["new"] != 3 || all.Result["pages"] != 2 || all.Result["reachedEnd"] != true {
		t.Fatalf("all=%#v", all.Result)
	}
	if items, _ := all.Result["items"].([]history.Item); len(items) != 3 || items[0].ID != "bilibili-900000000000000011" || items[0].Title != "今天20点开播" {
		t.Fatalf("items=%#v", all.Result["items"])
	}
	state, err := s.BilibiliState(context.Background(), bilisource.DefaultAccount)
	if err != nil || !state.Dynamics.LastAttemptAt.IsZero() || state.Dynamics.LastSuccessAt.IsZero() || !state.Login.SignedIn {
		t.Fatalf("state=%#v err=%v", state, err)
	}
	// A count of one reads only the newest page.
	*paths = nil
	one := fetchStep(t, steps, sqlite.BilibiliDynamicsSource, history.Window{Method: history.MethodSpace, Mode: history.ModeCount, Count: 1})
	if one.Result["listed"] != 1 || one.Result["stored"] != 1 || one.Result["reachedEnd"] != true || strings.Count(strings.Join(*paths, ","), "feed/space") != 1 {
		t.Fatalf("one=%#v paths=%v", one.Result, *paths)
	}
}
