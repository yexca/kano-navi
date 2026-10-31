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
	// A format correction lies over the feed's, which sync keeps writing.
	c.exec(t, `UPDATE videos SET is_short=0 WHERE id='AAAAAAAAAAA'`)
	code, payload = c.call(t, "PUT", "/api/admin/videos/AAAAAAAAAAA", `{"isShort":true}`)
	video, _ = payload["video"].(map[string]any)
	if code != 200 || video["isShort"] != true || video["sourceIsShort"] != false || video["manualIsShort"] != true || video["title"] != "Song cover" {
		t.Fatalf("mark short=%d %v", code, payload)
	}
	code, payload = c.call(t, "PUT", "/api/admin/videos/AAAAAAAAAAA", `{"isShort":null}`)
	video, _ = payload["video"].(map[string]any)
	if code != 200 || video["isShort"] != false || video["manualIsShort"] != nil {
		t.Fatalf("clear short=%d %v", code, payload)
	}
	for body, want := range map[string]int{
		`{"title":""}`:                       400,
		`{"kind":"SHORTS"}`:                  400,
		`{"isShort":"yes"}`:                  400,
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

func TestVideoMergeRoutes(t *testing.T) {
	c := newConsole(t, "development", "", &gatedSteps{})
	c.exec(t, `INSERT INTO videos(id,source,title,scheduled_at,published_at,url,kind,duration_seconds) VALUES('e6acfMolexU','youtube','奏（スキマスイッチ） - 鹿乃まほろ cover.','2026-10-24T09:00:09Z','2026-10-20T12:00:00Z','https://www.youtube.com/watch?v=e6acfMolexU','MUSIC',289)`)
	c.exec(t, `INSERT INTO videos(id,source,title,published_at,url,kind,duration_seconds) VALUES('BV1CmHw6eEGE','bilibili','鹿乃 翻唱《奏》','2026-10-24T09:05:00Z','https://www.bilibili.com/video/BV1CmHw6eEGE','MUSIC',289)`)
	c.exec(t, `INSERT INTO videos(id,source,title,published_at,url,kind) VALUES('BV1other00000','bilibili','鹿乃 翻唱《春》','2026-08-01T00:00:00Z','https://www.bilibili.com/video/BV1other00000','MUSIC')`)

	code, payload := c.call(t, "GET", "/api/admin/videos/merges", "")
	items, _ := payload["items"].([]any)
	if code != 200 || len(items) != 1 {
		t.Fatalf("suggestions=%d %v", code, payload)
	}
	item := items[0].(map[string]any)
	candidates, _ := item["candidates"].([]any)
	if item["status"] != "rule" || item["bilibili"].(map[string]any)["id"] != "BV1CmHw6eEGE" || len(candidates) != 1 ||
		candidates[0].(map[string]any)["youtube"].(map[string]any)["id"] != "e6acfMolexU" || candidates[0].(map[string]any)["complete"] != true {
		t.Fatalf("suggestion=%v", item)
	}

	code, payload = c.call(t, "POST", "/api/admin/videos/BV1CmHw6eEGE/merge", `{"into":"https://youtu.be/e6acfMolexU"}`)
	video, _ := payload["video"].(map[string]any)
	if code != 200 || video["mergedInto"] != "e6acfMolexU" || video["mergeSource"] != "manual" || video["mergedIntoTitle"] != "奏（スキマスイッチ） - 鹿乃まほろ cover." {
		t.Fatalf("merge=%d %v", code, payload)
	}
	code, payload = c.call(t, "GET", "/api/admin/videos?source=youtube", "")
	listed, _ := payload["items"].([]any)
	if code != 200 || len(listed) != 1 {
		t.Fatalf("videos=%d %v", code, payload)
	}
	alternates, _ := listed[0].(map[string]any)["alternates"].([]any)
	if len(alternates) != 1 || alternates[0].(map[string]any)["id"] != "BV1CmHw6eEGE" || alternates[0].(map[string]any)["url"] != "https://www.bilibili.com/video/BV1CmHw6eEGE" {
		t.Fatalf("alternates=%v", listed[0])
	}
	if code, payload = c.call(t, "POST", "/api/admin/videos/BV1other00000/merge", `{"into":"e6acfMolexU"}`); code != 409 || payload["error"] != "merge_conflict" {
		t.Fatalf("conflict=%d %v", code, payload)
	}
	for body, want := range map[string]int{`{}`: 400, `{"into":7}`: 400, `{"into":"https://media.example.test/v"}`: 400, `{"into":"BV1other00000"}`: 400} {
		if code, payload = c.call(t, "POST", "/api/admin/videos/BV1CmHw6eEGE/merge", body); code != want {
			t.Fatalf("%s=%d %v", body, code, payload)
		}
	}
	if code, _ = c.call(t, "POST", "/api/admin/videos/missing/merge", `{"into":"e6acfMolexU"}`); code != 404 {
		t.Fatalf("missing upload=%d", code)
	}

	code, payload = c.call(t, "POST", "/api/admin/videos/BV1other00000/separate", "")
	video, _ = payload["video"].(map[string]any)
	if code != 200 || video["mergedInto"] != nil || video["mergeSource"] != "manual" || video["mergeReason"] != "kept_separate" {
		t.Fatalf("keep separate=%d %v", code, payload)
	}
	for path, want := range map[string]int{"BV1CmHw6eEGE": 409, "e6acfMolexU": 400, "missing": 404} {
		if code, payload = c.call(t, "POST", "/api/admin/videos/"+path+"/separate", ""); code != want {
			t.Fatalf("separate %s=%d %v", path, code, payload)
		}
	}

	code, payload = c.call(t, "POST", "/api/admin/videos/BV1CmHw6eEGE/unmerge", "")
	video, _ = payload["video"].(map[string]any)
	if code != 200 || video["mergedInto"] != nil || video["mergeSource"] != "manual" || video["mergeReason"] != "unmerged_from:e6acfMolexU" {
		t.Fatalf("unmerge=%d %v", code, payload)
	}
	if code, payload = c.call(t, "POST", "/api/admin/videos/BV1CmHw6eEGE/unmerge", ""); code != 400 {
		t.Fatalf("second unmerge=%d %v", code, payload)
	}
	code, payload = c.call(t, "GET", "/api/admin/videos/BV1CmHw6eEGE/merges", "")
	items, _ = payload["items"].([]any)
	if code != 200 || len(items) != 1 || items[0].(map[string]any)["status"] != "separate" {
		t.Fatalf("separate=%d %v", code, payload)
	}
	if got := c.text(t, `SELECT COUNT(*) FROM translations WHERE owner_id='e6acfMolexU' AND platform='bilibili'`); got != "0" {
		t.Fatalf("undone merge kept %s translations", got)
	}
}
