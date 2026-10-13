package workflow

import (
	"context"
	xsource "github.com/yexca/kano-navi/backend-go/internal/source/x"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type xTestRT func(*http.Request) (*http.Response, error)

func (f xTestRT) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestXRecentExecutorPersistsWorkflowAttempt(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := sqlite.OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.EnsureXRecentWorkflow(context.Background()); err != nil {
		t.Fatal(err)
	}
	rt := xTestRT(func(r *http.Request) (*http.Response, error) {
		body := `status/111111111111`
		if r.URL.Host == "api.vxtwitter.com" {
			body = `{"tweetID":"111111111111","text":"hello","date":"2026-10-11T00:00:00Z"}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	a := xsource.NewAdapter(xsource.NewClient(&http.Client{Transport: rt}), xsource.Options{Handles: []string{"kano"}, DiscoveryLimit: 1, MaxDetailRequests: 1, BootstrapDays: 7})
	ex := &XRecentExecutor{Store: s, Adapter: a, Options: a.Options}
	run, err := (&Executor{Store: s, Steps: ex, SyntheticSteps: map[string]bool{"x": false}}).Run(context.Background(), "x-recent", TriggerOperator, "test-x")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "succeeded" {
		t.Fatalf("status=%s", run.Status)
	}
	got, err := s.GetWorkflowRun(context.Background(), run.ID)
	if err != nil || len(got.Steps) != 1 || len(got.Steps[0].Attempts) != 1 {
		t.Fatalf("run=%#v err=%v", got, err)
	}
}

func TestXSingleRequestBudgetRotatesAccountsAcrossRestart(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := sqlite.OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	requested := []string{}
	rt := xTestRT(func(r *http.Request) (*http.Response, error) {
		id, account := "111111111111", "fixture_a"
		if strings.Contains(r.URL.Path, "fixture_b") {
			id, account = "222222222222", "fixture_b"
		}
		body := "status/" + id
		if r.URL.Host == "api.vxtwitter.com" {
			requested = append(requested, account)
			body = `{"tweetID":"` + id + `","text":"fixture","date":"2026-01-02T00:00:00Z"}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	opts := xsource.Options{Handles: []string{"fixture_a", "fixture_b"}, DiscoveryLimit: 1, MaxDetailRequests: 1, BootstrapDays: 7}
	a := xsource.NewAdapter(xsource.NewClient(&http.Client{Transport: rt}), opts)
	a.Now = func() time.Time { return time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC) }
	ex := &XRecentExecutor{Store: s, Adapter: a, Options: opts}
	if _, err = ex.Execute(context.Background(), StepInput{StepID: "x"}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = sqlite.OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ex.Store = s
	if _, err = ex.Execute(context.Background(), StepInput{StepID: "x"}); err != nil {
		t.Fatal(err)
	}
	if len(requested) != 2 || requested[0] != "fixture_a" || requested[1] != "fixture_b" {
		t.Fatalf("account budget starved a source: %v", requested)
	}
}
