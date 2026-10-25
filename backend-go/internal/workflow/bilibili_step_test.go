package workflow

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bilisource "github.com/yexca/kano-navi/backend-go/internal/source/bilibili"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

func TestBilibiliStepStoresUploadsOncePerIntervalAndRecordsCaptchas(t *testing.T) {
	s, err := sqlite.OpenInitialized(filepath.Join(t.TempDir(), "data", "database", "snapshot.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	blocked, requests := false, 0
	rt := xTestRT(func(r *http.Request) (*http.Response, error) {
		requests++
		body := `{"code":0,"data":{"archives":[{"aid":1,"bvid":"BV1AAAAAAAAA","title":"鹿乃 翻唱《奏》","pubdate":1767258000,"pic":"http://i0.hdslb.com/bfs/archive/a.jpg","duration":289,"upMid":316381099}]}}`
		status := 200
		switch {
		case r.URL.Path == "/x/frontend/finger/spi":
			body = `{"code":0,"data":{"b_3":"0A1B2C3D-0A1B-0A1B-0A1B-0A1B2C3D4E5F12345infoc","b_4":"synthetic-buvid4=="}}`
		case blocked:
			status, body = 412, "<!DOCTYPE html>"
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	now := time.Now()
	adapter := bilisource.NewAdapter(bilisource.NewClient(&http.Client{Transport: rt}), bilisource.Options{Account: bilisource.DefaultAccount, MinInterval: 3 * time.Hour, PageSize: 20})
	adapter.Now = func() time.Time { return now }
	steps := &SourceSteps{Store: s, Bilibili: adapter}
	run := func() AttemptResult {
		t.Helper()
		result, err := steps.Execute(context.Background(), StepInput{StepID: "bilibili", WorkflowID: sqlite.AdhocSyncWorkflow})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	if first := run(); first.Status != "succeeded" || first.Result["videos"] != 1 || requests != 2 {
		t.Fatalf("first=%#v requests=%d", first, requests)
	}
	// Within the interval the step makes no request.
	if soon := run(); soon.Status != "succeeded" || soon.Result["reason"] != "interval" || requests != 2 {
		t.Fatalf("soon=%#v requests=%d", soon, requests)
	}
	now, blocked = now.Add(4*time.Hour), true
	captcha := run()
	if captcha.Status != "failed" || requests != 3 {
		t.Fatalf("captcha=%#v requests=%d", captcha, requests)
	}
	if errs, _ := captcha.Result["errors"].([]string); len(errs) != 1 || errs[0] != "bilibili_risk_control" {
		t.Fatalf("captcha errors=%#v", captcha.Result)
	}
	state, err := s.BilibiliState(context.Background(), bilisource.DefaultAccount)
	if err != nil || !state.BlockedUntil.After(now) {
		t.Fatalf("state=%#v err=%v", state, err)
	}
	// The stored upload survives the failed read.
	page, err := s.ListAdminVideos(context.Background(), sqlite.AdminVideoFilter{Source: "bilibili"})
	if err != nil || page.Total != 1 {
		t.Fatalf("videos=%#v err=%v", page, err)
	}
}
