package adminapi

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bilisource "github.com/yexca/kano-navi/backend-go/internal/source/bilibili"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

type bilibiliTransport func(*http.Request) (*http.Response, error)

func (f bilibiliTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBilibiliUploadsAreAddedAndRefreshedByOperators(t *testing.T) {
	store, err := sqlite.OpenInitialized(filepath.Join(t.TempDir(), "database", "kano.sqlite"), true, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	owner, blocked, requests := "316381099", false, 0
	client := bilisource.NewClient(&http.Client{Transport: bilibiliTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		body := `{"code":0,"data":{"bvid":"BV1AAAAAAAAA","aid":1,"title":"鹿乃 翻唱《奏》","pubdate":1767258000,"pic":"http://i0.hdslb.com/bfs/archive/a.jpg","duration":289,"owner":{"mid":` + owner + `}}}`
		status := 200
		switch {
		case r.URL.Path == "/x/frontend/finger/spi":
			body = `{"code":0,"data":{"b_3":"0A1B2C3D-0A1B-0A1B-0A1B-0A1B2C3D4E5F12345infoc","b_4":"synthetic-buvid4=="}}`
		case blocked:
			status, body = 412, "<!DOCTYPE html>"
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	adapter := bilisource.NewAdapter(client, bilisource.Options{Account: "316381099", MinInterval: 3 * time.Hour, PageSize: 20})
	c := &console{store: store, handler: New(Options{Store: store, Auth: newAuth(t, "development", ""), Bilibili: adapter})}

	code, payload := c.call(t, "POST", "/api/admin/videos/bilibili", `{"input":"https://www.bilibili.com/video/BV1AAAAAAAAA/?spm_id_from=synthetic"}`)
	video, _ := payload["video"].(map[string]any)
	if code != 200 || video["source"] != "bilibili" || video["title"] != "鹿乃 翻唱《奏》" || video["durationSeconds"] != float64(289) {
		t.Fatalf("add=%d %v", code, payload)
	}
	for body, want := range map[string]int{`{"input":"https://short-link.invalid/synthetic"}`: 400, `{}`: 400} {
		if code, payload = c.call(t, "POST", "/api/admin/videos/bilibili", body); code != want {
			t.Fatalf("%s=%d %v", body, code, payload)
		}
	}
	owner = "42"
	if code, payload = c.call(t, "POST", "/api/admin/videos/bilibili", `{"input":"BV1AAAAAAAAA"}`); code != 400 {
		t.Fatalf("other account=%d %v", code, payload)
	}
	owner = "316381099"
	if code, payload = c.call(t, "POST", "/api/admin/videos/BV1AAAAAAAAA/refresh", ""); code != 200 || payload["video"].(map[string]any)["id"] != "BV1AAAAAAAAA" {
		t.Fatalf("refresh=%d %v", code, payload)
	}

	// A captcha answer starts the backoff; the next request waits for it.
	blocked = true
	if code, payload = c.call(t, "POST", "/api/admin/videos/BV1AAAAAAAAA/refresh", ""); code != 503 || payload["error"] != "bilibili_blocked" || payload["blockedUntil"] == nil {
		t.Fatalf("blocked=%d %v", code, payload)
	}
	before := requests
	if code, _ = c.call(t, "POST", "/api/admin/videos/bilibili", `{"input":"BV1AAAAAAAAA"}`); code != 503 || requests != before {
		t.Fatalf("waiting=%d requests %d→%d", code, before, requests)
	}

	code, payload = c.call(t, "GET", "/api/admin/sources", "")
	status, _ := payload["bilibili"].(map[string]any)
	if code != 200 || status["videos"] != float64(1) || status["blockedUntil"] == nil || status["lastError"] != "bilibili_risk_control" || status["account"] != "316381099" {
		t.Fatalf("sources=%d %v", code, payload["bilibili"])
	}
	if raw, _ := json.Marshal(status); strings.Contains(string(raw), "buvid") || strings.Contains(string(raw), "infoc") {
		t.Fatalf("status exposes the session: %s", raw)
	}
}
