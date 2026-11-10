package contentadmin

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

const quotingID = "1900000000000000100"

func TestQuoteHiddenDecision(t *testing.T) {
	f := newFixture(t, true)
	f.exec(`INSERT INTO posts(id,source,account_handle,text,published_at,url,quote_json) VALUES(?,'x','synthetic','look','2026-10-01T00:00:00.000Z',?,?)`,
		quotingID, "https://x.com/synthetic/status/"+quotingID, `{"id":"1900000000000000001","text":"quoted","url":"https://x.com/other/status/1900000000000000001"}`)
	f.exec(`INSERT INTO posts(id,source,account_handle,text,published_at,url) VALUES('1900000000000000101','x','synthetic','plain','2026-10-01T00:00:00.000Z','https://x.com/synthetic/status/1900000000000000101')`)
	f.module.now = func() time.Time { return time.Date(2026, 10, 13, 1, 2, 3, 0, time.UTC) }
	before := f.revision()

	hidden := f.put("/posts/"+quotingID+"/quote/hidden", map[string]any{"hidden": true})
	hidden.expect(t, http.StatusOK, "")
	state := object(t, hidden.json["state"])
	if state["postId"] != quotingID || state["quoteHiddenAt"] != "2026-10-13T01:02:03.000Z" || state["refreshRequestedAt"] != nil {
		t.Fatalf("hidden state: %#v", state)
	}
	if f.revision() == before {
		t.Fatal("hiding a quote must change the public snapshot revision")
	}
	// Hiding again keeps the first decision and the revision.
	f.module.now = func() time.Time { return time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC) }
	again := f.revision()
	if object(t, f.put("/posts/"+quotingID+"/quote/hidden", map[string]any{"hidden": true}).json["state"])["quoteHiddenAt"] != "2026-10-13T01:02:03.000Z" || f.revision() != again {
		t.Fatal("a repeated hide replaced the decision")
	}
	if object(t, f.put("/posts/"+quotingID+"/quote/hidden", map[string]any{"hidden": false}).json["state"])["quoteHiddenAt"] != nil || f.revision() == again {
		t.Fatal("restore failed")
	}

	f.put("/posts/"+quotingID+"/quote/hidden", map[string]any{"hidden": "yes"}).expectInvalid(t, "hidden must be true or false")
	f.put("/posts/"+quotingID+"/quote/hidden", map[string]any{}).expectInvalid(t, "hidden must be true or false")
	f.put("/posts/1900000000000000101/quote/hidden", map[string]any{"hidden": true}).expect(t, http.StatusNotFound, "quote_not_found")
	f.put("/posts/unknown/quote/hidden", map[string]any{"hidden": true}).expect(t, http.StatusNotFound, "post_not_found")
	// The quote's own data is never written here.
	if f.text(`SELECT quote_json FROM posts WHERE id=?`, quotingID) != `{"id":"1900000000000000001","text":"quoted","url":"https://x.com/other/status/1900000000000000001"}` {
		t.Fatal("quote data changed")
	}
}

func TestPostRefreshRequestQueuesWorkOnly(t *testing.T) {
	f := newFixture(t, true)
	f.exec(`INSERT INTO posts(id,source,account_handle,text,published_at,url) VALUES(?,'x','Synthetic','look','2026-10-01T00:00:00.000Z',?)`, quotingID, "https://x.com/synthetic/status/"+quotingID)
	f.exec(`INSERT INTO posts(id,source,account_handle,text,published_at,url) VALUES('manual-1','x','synthetic','legacy','2026-10-01T00:00:00.000Z','https://x.com/synthetic/status/1')`)
	f.exec(`INSERT INTO sync_state(source,account_id,metadata_json) VALUES('x','synthetic',?)`,
		`{"pendingStatusIds":["`+quotingID+`","1900000000000000102"],"pendingRetries":{"`+quotingID+`":{"failures":2,"retryAt":"2026-10-20T00:00:00Z"},"1900000000000000102":{"failures":1}}}`)
	f.module.now = func() time.Time { return time.Date(2026, 10, 13, 1, 2, 3, 0, time.UTC) }

	queued := f.request(http.MethodPost, "/posts/"+quotingID+"/refresh", "", nil)
	queued.expect(t, http.StatusOK, "")
	if state := object(t, queued.json["state"]); state["refreshRequestedAt"] != "2026-10-13T01:02:03.000Z" || state["quoteHiddenAt"] != nil {
		t.Fatalf("refresh state: %#v", state)
	}
	// Only this status's backoff is cleared; the pending queue stays.
	if got := f.text(`SELECT metadata_json FROM sync_state`); got != `{"pendingStatusIds":["`+quotingID+`","1900000000000000102"],"pendingRetries":{"1900000000000000102":{"failures":1}}}` {
		t.Fatalf("metadata = %s", got)
	}
	// A second request keeps the first request's place in line.
	f.module.now = func() time.Time { return time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC) }
	if object(t, f.request(http.MethodPost, "/posts/"+quotingID+"/refresh", "", nil).json["state"])["refreshRequestedAt"] != "2026-10-13T01:02:03.000Z" {
		t.Fatal("a repeated refresh moved the request")
	}
	f.request(http.MethodPost, "/posts/manual-1/refresh", "", nil).expectInvalid(t, "only stored X posts can be refreshed from the source")
	f.request(http.MethodPost, "/posts/unknown/refresh", "", nil).expect(t, http.StatusNotFound, "post_not_found")
	if len(f.network.visited) != 0 {
		t.Fatalf("a refresh request reached the network: %v", f.network.visited)
	}
}

func TestPostHiddenDecision(t *testing.T) {
	f := newFixture(t, true)
	f.exec(`INSERT INTO posts(id,source,account_handle,text,published_at,url) VALUES('1900000000000000101','x','synthetic','plain','2026-10-01T00:00:00.000Z','https://x.com/synthetic/status/1900000000000000101')`)
	f.module.now = func() time.Time { return time.Date(2026, 10, 13, 1, 2, 3, 0, time.UTC) }
	before := f.revision()

	hidden := f.put("/posts/1900000000000000101/hidden", map[string]any{"hidden": true})
	hidden.expect(t, http.StatusOK, "")
	if state := object(t, hidden.json["state"]); state["hiddenAt"] != "2026-10-13T01:02:03.000Z" || state["quoteHiddenAt"] != nil {
		t.Fatalf("hidden state: %#v", state)
	}
	if f.revision() == before {
		t.Fatal("hiding a post must change the public snapshot revision")
	}
	f.module.now = func() time.Time { return time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC) }
	again := f.revision()
	if object(t, f.put("/posts/1900000000000000101/hidden", map[string]any{"hidden": true}).json["state"])["hiddenAt"] != "2026-10-13T01:02:03.000Z" || f.revision() != again {
		t.Fatal("a repeated hide replaced the decision")
	}
	if object(t, f.put("/posts/1900000000000000101/hidden", map[string]any{"hidden": false}).json["state"])["hiddenAt"] != nil || f.revision() == again {
		t.Fatal("restore failed")
	}
	f.put("/posts/1900000000000000101/hidden", map[string]any{"hidden": 1}).expectInvalid(t, "hidden must be true or false")
	f.put("/posts/unknown/hidden", map[string]any{"hidden": true}).expect(t, http.StatusNotFound, "post_not_found")
}

// Showing a promoted post the rules demoted is the operator's promotion: a
// later reconcile under rules that no longer match keeps it shown, and hiding
// it again withdraws that promotion.
func TestPostShownKeepsItsPromotion(t *testing.T) {
	f := newFixture(t, true)
	const id = "1900000000000000301"
	f.exec(`INSERT INTO posts(id,source,account_handle,text,published_at,url,hidden_at,promoted_at) VALUES(?,'x','agency_synthetic','notice','2026-10-01T00:00:00.000Z','https://x.com/agency_synthetic/status/1900000000000000301','2026-10-02T00:00:00.000Z','2026-10-01T00:00:00.000Z')`, id)
	f.exec(`INSERT INTO x_statuses(id,author_handle,text,url,demoted_at,first_seen_at,updated_at) VALUES(?,'agency_synthetic','notice','https://x.com/agency_synthetic/status/1900000000000000301','2026-10-02T00:00:00.000Z','2026-10-01T00:00:00.000Z','2026-10-02T00:00:00.000Z')`, id)
	f.put("/posts/"+id+"/hidden", map[string]any{"hidden": false}).expect(t, http.StatusOK, "")
	if got := f.text(`SELECT COALESCE(manual_decision,'')||'|'||COALESCE(demoted_at,'') FROM x_statuses WHERE id=?`, id); got != "promote|" {
		t.Fatalf("decision after show = %s", got)
	}
	if _, err := f.store.SetXRelatedAccounts(context.Background(), []sqlite.XRelatedAccount{{Handle: "agency_synthetic", Keywords: []string{"unrelated"}}}, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.text(`SELECT CASE WHEN hidden_at IS NULL THEN 'shown' ELSE 'hidden' END FROM posts WHERE id=?`, id); got != "shown" {
		t.Fatalf("the rules hid a post the operator showed: %s", got)
	}
	f.put("/posts/"+id+"/hidden", map[string]any{"hidden": true}).expect(t, http.StatusOK, "")
	if got := f.text(`SELECT COALESCE(manual_decision,'none') FROM x_statuses WHERE id=?`, id); got != "none" {
		t.Fatalf("decision after hide = %s", got)
	}
}

// A promoted post the pool hid becomes the operator's to hide once they show
// or hide it in Posts: the pool's mark goes, so a later automatic promotion
// never undoes the operator's hide.
func TestPostHiddenDecisionTakesThePoolsHide(t *testing.T) {
	f := newFixture(t, true)
	const id = "1900000000000000201"
	f.exec(`INSERT INTO posts(id,source,account_handle,text,published_at,url,hidden_at,promoted_at) VALUES(?,'x','agency_synthetic','notice','2026-10-01T00:00:00.000Z','https://x.com/agency_synthetic/status/1900000000000000201','2026-10-02T00:00:00.000Z','2026-10-01T00:00:00.000Z')`, id)
	f.exec(`INSERT INTO x_statuses(id,author_handle,text,url,demoted_at,first_seen_at,updated_at) VALUES(?,'agency_synthetic','notice','https://x.com/agency_synthetic/status/1900000000000000201','2026-10-02T00:00:00.000Z','2026-10-01T00:00:00.000Z','2026-10-02T00:00:00.000Z')`, id)
	f.put("/posts/"+id+"/hidden", map[string]any{"hidden": false}).expect(t, http.StatusOK, "")
	f.put("/posts/"+id+"/hidden", map[string]any{"hidden": true}).expect(t, http.StatusOK, "")
	if got := f.text(`SELECT demoted_at FROM x_statuses WHERE id=?`, id); got != "<null>" {
		t.Fatalf("demoted_at=%s", got)
	}
	if _, err := f.store.SetXRelatedAccounts(context.Background(), []sqlite.XRelatedAccount{{Handle: "agency_synthetic", Keywords: []string{"notice"}}}, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.text(`SELECT CASE WHEN hidden_at IS NULL THEN 'shown' ELSE 'hidden' END FROM posts WHERE id=?`, id); got != "hidden" {
		t.Fatalf("an automatic promotion undid the operator's hide: %s", got)
	}
}
