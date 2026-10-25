package adminapi

import (
	"fmt"
	"testing"
)

func TestVideoCorrectionsHistoryAndRefresh(t *testing.T) {
	c := newConsole(t, "development", "", &gatedSteps{})
	c.exec(t, `INSERT INTO videos(id,source,title,published_at,url,kind) VALUES('AAAAAAAAAAA','youtube','Song cover premiere','2026-03-01T09:00:00Z','https://www.youtube.com/watch?v=AAAAAAAAAAA','MUSIC')`)
	c.exec(t, `INSERT INTO video_revisions(video_id,origin,title,replaced_at) VALUES('AAAAAAAAAAA','sync','Earlier title','2026-03-01T10:00:00Z')`)

	code, payload := c.call(t, "PUT", "/api/admin/videos/AAAAAAAAAAA", `{"title":"Song cover","kind":"talk"}`)
	video, _ := payload["video"].(map[string]any)
	if code != 200 || video["title"] != "Song cover" || video["sourceTitle"] != "Song cover premiere" || video["kind"] != "TALK" {
		t.Fatalf("update=%d %v", code, payload)
	}
	code, payload = c.call(t, "PUT", "/api/admin/videos/AAAAAAAAAAA", `{"kind":null}`)
	video, _ = payload["video"].(map[string]any)
	if code != 200 || video["title"] != "Song cover" || video["kind"] != "MUSIC" {
		t.Fatalf("clear kind=%d %v", code, payload)
	}
	for body, want := range map[string]int{
		`{"title":""}`:                       400,
		`{"kind":"SHORTS"}`:                  400,
		`{"thumbnail":{"mediaId":"x"}}`:      400,
		`{"description":"not a correction"}`: 400,
		`{"title":7}`:                        400,
	} {
		if code, payload = c.call(t, "PUT", "/api/admin/videos/AAAAAAAAAAA", body); code != want {
			t.Fatalf("%s=%d %v", body, code, payload)
		}
	}
	if code, _ = c.call(t, "PUT", "/api/admin/videos/missing", `{"title":"x"}`); code != 404 {
		t.Fatalf("missing video=%d", code)
	}

	code, payload = c.call(t, "GET", "/api/admin/videos/AAAAAAAAAAA/history", "")
	revisions, _ := payload["revisions"].([]any)
	if code != 200 || len(revisions) != 1 || revisions[0].(map[string]any)["title"] != "Earlier title" {
		t.Fatalf("history=%d %v", code, payload)
	}

	code, payload = c.call(t, "POST", "/api/admin/videos/AAAAAAAAAAA/refresh", "")
	if code != 202 || payload["started"] != true {
		t.Fatalf("refresh=%d %v", code, payload)
	}
	if got := c.text(t, `SELECT refresh_requested_at FROM videos WHERE id='AAAAAAAAAAA'`); got == "<null>" {
		t.Fatal("refresh request was not recorded")
	}
	run := c.waitForRun(t, runID(t, payload), "completed")
	if fmt.Sprint(run["steps"]) != "[youtube media reconcile]" {
		t.Fatalf("run=%v", run)
	}
}
