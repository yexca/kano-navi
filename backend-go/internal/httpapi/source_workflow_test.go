package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/adminauth"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
	"github.com/yexca/kano-navi/backend-go/internal/workflow"
)

type heldStep struct {
	entered chan workflow.StepInput
	release chan struct{}
}

func (s *heldStep) Execute(ctx context.Context, in workflow.StepInput) (workflow.AttemptResult, error) {
	s.entered <- in
	select {
	case <-s.release:
		return workflow.AttemptResult{Status: "succeeded"}, nil
	case <-ctx.Done():
		return workflow.AttemptResult{}, workflow.NewStepError("cancelled", workflow.ErrCancelled)
	}
}
func TestSourceHTTPAuthenticatesBoundsAndSubmitsAsyncWorkflow(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := sqlite.OpenAdminWrites(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_ = s.EnsureSourceWorkflows(context.Background(), true, true, true)
	step := &heldStep{entered: make(chan workflow.StepInput, 3), release: make(chan struct{})}
	q := &workflow.Queue{Store: s, Executor: &workflow.Executor{Store: s, Steps: step, SyntheticSteps: map[string]bool{"x": false, "youtube": false, "media": false}}}
	defer q.Close()
	cfg, _ := adminauth.NewConfig("development", "", "")
	auth, _ := adminauth.New(cfg)
	h := NewHandlerWithSources(s, nil, auth, map[string]XSyncTrigger{"youtube": &workflow.RecentTrigger{Queue: q, WorkflowID: "youtube-recent", Source: "youtube"}}, q)
	call := func(method, path, body string, crossSite bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if crossSite {
			r.Header.Set("Sec-Fetch-Site", "cross-site")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := call("POST", "/api/admin/sources/youtube/fetch", `{"window":{"mode":"recent","days":3}}`, true); w.Code != 403 {
		t.Fatalf("cross-site=%d", w.Code)
	}
	for _, body := range []string{`{"window":{"mode":"recent","days":0}}`, `{"window":{"mode":"recent","days":366}}`, `{"window":{"mode":"recent","days":1.5}}`, `{"window":{"mode":"recent","days":3,"days":4}}`, `{"window":{"mode":"recent","days":3},"url":"https://example.invalid"}`} {
		if w := call("POST", "/api/admin/sources/youtube/fetch", body, false); w.Code != 400 {
			t.Fatalf("invalid=%d body=%s", w.Code, body)
		}
	}
	if w := call("POST", "/api/admin/sources/youtube/fetch", `{"window":{"mode":"range","startDate":"2026-01-01","endDate":"2026-01-02"}}`, false); w.Code != 400 && w.Code != 501 {
		t.Fatalf("history=%d", w.Code)
	}
	w := call("POST", "/api/admin/sources/youtube/fetch", `{"window":{"mode":"recent","days":3}}`, false)
	if w.Code != 202 {
		t.Fatalf("submit=%d body=%s", w.Code, w.Body)
	}
	var payload struct {
		Run sqlite.WorkflowRun `json:"run"`
	}
	if json.Unmarshal(w.Body.Bytes(), &payload) != nil || payload.Run.Status != "queued" {
		t.Fatalf("response=%s", w.Body)
	}
	select {
	case in := <-step.entered:
		if in.LookbackDays != 3 {
			t.Fatalf("days=%d", in.LookbackDays)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no execution")
	}
	if w := call("POST", "/api/admin/workflows/youtube-recent/run", `{}`, false); w.Code != 409 {
		t.Fatalf("busy=%d body=%s", w.Code, w.Body)
	}
	if w := call("POST", "/api/admin/runs/"+payload.Run.ID+"/cancel", `{}`, false); w.Code != 202 {
		t.Fatalf("cancel=%d", w.Code)
	}
	q.Close()
	if w := call("GET", "/api/admin/runs/"+payload.Run.ID, "", false); w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"cancelled"`) {
		t.Fatalf("cancel read=%d body=%s", w.Code, w.Body)
	}
	prodConfig, _ := adminauth.NewConfig("production", strings.Repeat("synthetic-", 2), "")
	prodAuth, _ := adminauth.New(prodConfig)
	protected := NewHandlerWithSources(s, nil, prodAuth, nil, q)
	if w := request(t, protected, http.MethodPost, "/api/admin/sources/youtube/fetch"); w.Code != 401 {
		t.Fatalf("unauthenticated=%d", w.Code)
	}
}
func TestWorkflowHTTPCRUDValidatesAndRetainsSourceLookback(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := sqlite.OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	q := &workflow.Queue{Store: s, Executor: &workflow.Executor{Store: s}}
	defer q.Close()
	cfg, _ := adminauth.NewConfig("development", "", "")
	auth, _ := adminauth.New(cfg)
	h := NewHandlerWithSources(s, nil, auth, nil, q)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := call("POST", "/api/admin/workflows", `{"name":"Sources","steps":["media","youtube","x","x"],"sourceLookbackDays":{"x":3,"youtube":5},"scheduleEnabled":true,"intervalMinutes":1}`)
	if w.Code != 201 {
		t.Fatalf("create=%d %s", w.Code, w.Body)
	}
	var payload struct {
		Workflow sqlite.WorkflowDefinition `json:"workflow"`
	}
	if json.Unmarshal(w.Body.Bytes(), &payload) != nil {
		t.Fatal("invalid response")
	}
	id := payload.Workflow.ID
	if strings.Join(payload.Workflow.Steps, ",") != "x,youtube,media" || payload.Workflow.IntervalMinutes != 15 || payload.Workflow.NextRunAt == nil {
		t.Fatalf("definition=%#v", payload.Workflow)
	}
	if w = call("PUT", "/api/admin/workflows/"+id, `{"name":"Renamed"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"youtube":5`) {
		t.Fatalf("update=%d %s", w.Code, w.Body)
	}
	for _, body := range []string{`{"steps":["unknown"]}`, `{"sourceLookbackDays":{"x":0}}`, `{"scheduleEnabled":null}`, `{"name":""}`, `{"arbitrary":"data"}`} {
		if w = call("PUT", "/api/admin/workflows/"+id, body); w.Code != 400 {
			t.Fatalf("accepted %s: %d", body, w.Code)
		}
	}
	if w = call("DELETE", "/api/admin/workflows/"+id, ""); w.Code != 204 {
		t.Fatalf("delete=%d", w.Code)
	}
	if w = call("GET", "/api/admin/workflows/"+id, ""); w.Code != 404 {
		t.Fatalf("deleted read=%d", w.Code)
	}
}
