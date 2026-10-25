package adminapi

import (
	"fmt"
	"testing"
)

func TestTranslationConsole(t *testing.T) {
	c := newConsole(t, "development", "", &gatedSteps{})
	for _, id := range []string{"1", "2"} {
		c.exec(t, `INSERT INTO posts (id, source, account_handle, type, text, published_at, url) VALUES (?, 'x', 'synthetic_one', 'daily', ?, '2026-03-01T00:00:00.000Z', ?)`,
			id, "今日の配信 "+id, "https://x.example.invalid/"+id)
	}
	code, payload := c.call(t, "GET", "/api/admin/translations", "")
	posts := payload["counts"].(map[string]any)["post"].(map[string]any)
	if code != 200 || posts["total"] != float64(2) || posts["incomplete"] != float64(2) || fmt.Sprint(payload["locales"]) != "[en zh-CN]" {
		t.Fatalf("summary=%d %v", code, payload)
	}

	code, payload = c.call(t, "PUT", "/api/admin/translations/post/1/en", `{"text":"  Today's stream 1  "}`)
	if code != 200 {
		t.Fatalf("put=%d %v", code, payload)
	}
	en := payload["translation"].(map[string]any)["locales"].([]any)[0].(map[string]any)
	if en["origin"] != "manual" || en["text"] != "Today's stream 1" || en["manual"] != "Today's stream 1" || en["manualStale"] != false {
		t.Fatalf("manual=%v", en)
	}
	for path, want := range map[string]int{
		"/api/admin/translations/post/1/ja":       400,
		"/api/admin/translations/post/missing/en": 404,
		"/api/admin/translations/event/1/en":      404,
	} {
		if code, payload := c.call(t, "PUT", path, `{"text":"synthetic"}`); code != want {
			t.Fatalf("%s=%d %v", path, code, payload)
		}
	}
	if code, payload := c.call(t, "PUT", "/api/admin/translations/post/1/en", `{"text":"   "}`); code != 400 {
		t.Fatalf("empty=%d %v", code, payload)
	}

	// The manual English text alone leaves Chinese missing.
	_, payload = c.call(t, "GET", "/api/admin/posts?translation=incomplete", "")
	if payload["total"] != float64(2) {
		t.Fatalf("incomplete=%v", payload)
	}
	item := payload["items"].([]any)[0].(map[string]any)["translation"].(map[string]any)
	if item["status"] != "incomplete" || item["machineStatus"] != "never" {
		t.Fatalf("item=%v", item)
	}

	code, payload = c.call(t, "POST", "/api/admin/translations/request", `{"missing":true}`)
	if code != 202 || payload["queued"] != float64(2) || payload["busy"] != false {
		t.Fatalf("request=%d %v", code, payload)
	}
	run := c.waitForRun(t, runID(t, payload), "completed")
	if run["kind"] != "translate" || fmt.Sprint(run["steps"]) != "[translate]" || run["run"].(map[string]any)["source"] != "translate" {
		t.Fatalf("run=%v", run)
	}
	if got := <-c.steps.seen; got != "translate" {
		t.Fatalf("step=%s", got)
	}
	_, payload = c.call(t, "GET", "/api/admin/posts?translation=requested", "")
	if payload["total"] != float64(2) {
		t.Fatalf("the synthetic step leaves requests in place: %v", payload)
	}

	code, payload = c.call(t, "DELETE", "/api/admin/translations/post/1/en", "")
	en = payload["translation"].(map[string]any)["locales"].([]any)[0].(map[string]any)
	if code != 200 || en["origin"] != nil || en["status"] != "missing" {
		t.Fatalf("delete=%d %v", code, payload)
	}
	for body, want := range map[string]int{
		`{}`:                                     400,
		`{"ownerType":"post"}`:                   400,
		`{"ownerType":"event","ids":["1"]}`:      400,
		`{"ownerType":"post","ids":["1"],"x":1}`: 400,
		`{"ownerType":"post","ids":["1"]}`:       202,
	} {
		if code, payload := c.call(t, "POST", "/api/admin/translations/request", body); code != want {
			t.Fatalf("%s=%d %v", body, code, payload)
		}
	}
}
