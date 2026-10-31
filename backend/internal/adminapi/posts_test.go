package adminapi

import (
	"fmt"
	"testing"
)

func TestPostsFiltersReachStoredHistoryAndUseInclusiveJapanDates(t *testing.T) {
	c := newConsole(t, "development", "", &gatedSteps{})
	for i := range 75 {
		c.exec(t, `INSERT INTO posts(id,source,text,published_at,url) VALUES(?,'x','recent','2026-03-01T00:00:00Z','https://example.invalid/post')`, fmt.Sprintf("synthetic-%02d", i))
	}
	c.exec(t, `INSERT INTO posts(id,source,text,published_at,url) VALUES
	 ('before','x','historical needle','2020-01-01T14:59:59Z','https://example.invalid/post'),
	 ('start','x','historical needle','2020-01-01T15:00:00Z','https://example.invalid/post'),
	 ('end','x','historical needle','2020-01-02T23:59:59+09:00','https://example.invalid/post'),
	 ('after','x','historical needle','2020-01-02T15:00:00Z','https://example.invalid/post');
	 INSERT INTO post_llm_states(post_id,status,updated_at) VALUES('start','failed',''),('end','processed','')`)
	for _, tc := range []struct{ query, want string }{
		{"search=start", "start"},
		{"from=2020-01-02&to=2020-01-02&llm=failed", "start"},
		{"from=2020-01-02&to=2020-01-02&llm=processed", "end"},
	} {
		code, payload := c.call(t, "GET", "/api/admin/posts?"+tc.query, "")
		items, _ := payload["items"].([]any)
		if code != 200 || payload["total"] != float64(1) || len(items) != 1 || items[0].(map[string]any)["id"] != tc.want {
			t.Fatalf("%s: code=%d result=%v", tc.query, code, payload)
		}
	}
	code, payload := c.call(t, "GET", "/api/admin/posts?from=2020-01-02&to=2020-01-02&search=needle&pageSize=1&page=2", "")
	items, _ := payload["items"].([]any)
	if code != 200 || payload["total"] != float64(2) || payload["totalPages"] != float64(2) || payload["page"] != float64(2) || len(items) != 1 || items[0].(map[string]any)["id"] != "start" {
		t.Fatalf("inclusive range pagination: code=%d result=%v", code, payload)
	}
}
