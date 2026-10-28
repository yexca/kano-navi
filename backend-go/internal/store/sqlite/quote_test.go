package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	xsource "github.com/yexca/kano-navi/backend-go/internal/source/x"
	"github.com/yexca/kano-navi/backend-go/internal/testutil"
)

const quotedID = "1900000000000000001"

func twimg(name string) string { return "https://pbs.twimg.com/media/" + name + ".jpg" }

// linkedMedia lists a post's links of one role as their source URLs in order.
func linkedMedia(t *testing.T, db *sql.DB, postID, role string) string {
	t.Helper()
	rows, err := db.Query(`SELECT a.source_url FROM media_links l JOIN media_assets a ON a.id=l.media_id WHERE l.owner_type='post' AND l.owner_id=? AND l.role=? ORDER BY l.position`, postID, role)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var u string
		if err = rows.Scan(&u); err != nil {
			t.Fatal(err)
		}
		out = append(out, u)
	}
	return strings.Join(out, " ")
}

func storedQuote(t *testing.T, db *sql.DB, postID string) string {
	t.Helper()
	var quote sql.NullString
	if err := db.QueryRow(`SELECT quote_json FROM posts WHERE id=?`, postID).Scan(&quote); err != nil {
		t.Fatal(err)
	}
	return quote.String
}

func TestWriteXRecentStoresTheQuoteApartFromOwnImages(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	quote := &xsource.Quote{ID: quotedID, AuthorHandle: "quoted_synthetic", AuthorName: "Quoted", Text: "quoted wording", PublishedAt: "2026-10-10T00:00:00Z",
		URL: "https://x.com/quoted_synthetic/status/" + quotedID, MediaURLs: []string{twimg("quoted-1"), "not a url", twimg("quoted-2")}}
	post := xsource.Post{ID: "1900000000000000100", AccountHandle: "kano", Type: "daily", Text: "look", PublishedAt: "2026-10-11T00:00:00Z",
		URL: "https://x.com/kano/status/1900000000000000100", MediaURLs: []string{twimg("own")}, Quote: quote, Raw: map[string]any{}}
	write := func(p xsource.Post) {
		t.Helper()
		if err := s.WriteXRecent(ctx, "kano", xsource.Result{Account: "kano", Posts: []xsource.Post{p}}, true); err != nil {
			t.Fatal(err)
		}
	}
	write(post)
	db, _ := sql.Open("sqlite", "file:"+p+"?mode=ro")
	defer db.Close()
	// The projection keeps no image addresses; they are quote-image links.
	if got := storedQuote(t, db, post.ID); got != `{"id":"`+quotedID+`","authorHandle":"quoted_synthetic","authorName":"Quoted","text":"quoted wording","publishedAt":"2026-10-10T00:00:00Z","url":"https://x.com/quoted_synthetic/status/`+quotedID+`"}` {
		t.Fatalf("quote_json=%s", got)
	}
	if own, quoted := linkedMedia(t, db, post.ID, "post-image"), linkedMedia(t, db, post.ID, "quote-image"); own != twimg("own") || quoted != twimg("quoted-1")+" "+twimg("quoted-2") {
		t.Fatalf("own=%s quoted=%s", own, quoted)
	}

	// A source that does not read quotes keeps the stored one.
	unread := post
	unread.Quote, unread.QuoteUnread, unread.MediaURLs = nil, true, nil
	write(unread)
	if storedQuote(t, db, post.ID) == "" || linkedMedia(t, db, post.ID, "quote-image") == "" || linkedMedia(t, db, post.ID, "post-image") != "" {
		t.Fatalf("unread source changed the quote: %s", storedQuote(t, db, post.ID))
	}
	// A complete answer without a quote clears it and its images.
	cleared := post
	cleared.Quote = nil
	write(cleared)
	if storedQuote(t, db, post.ID) != "" || linkedMedia(t, db, post.ID, "quote-image") != "" || linkedMedia(t, db, post.ID, "post-image") != twimg("own") {
		t.Fatalf("quote kept: %s", storedQuote(t, db, post.ID))
	}
}

func TestDashboardQuoteListsOnlyReadyImages(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "database", "kano.sqlite")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(testutil.NewSnapshot(t), path); err != nil {
		t.Fatal(err)
	}
	content := []byte("synthetic quoted image")
	hash := sha256Hex(string(content))
	cache := "x/sha256/" + hash + ".jpg"
	if err := os.MkdirAll(filepath.Join(root, "x", "sha256"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(cache)), content, 0o644); err != nil {
		t.Fatal(err)
	}
	quoteURL := "https://x.com/quoted_synthetic/status/" + quotedID
	for id, quote := range map[string]string{
		"quoting":     `{"id":"` + quotedID + `","authorHandle":"quoted_synthetic","authorName":"Quoted","text":"quoted wording","url":"` + quoteURL + `"}`,
		"unavailable": `{"id":"` + quotedID + `","url":"https://x.com/i/status/` + quotedID + `","unavailable":true,"text":"ignored"}`,
		"foreign-url": `{"id":"` + quotedID + `","text":"ignored","url":"https://evil.example.invalid/status/` + quotedID + `"}`,
	} {
		testutil.Exec(t, path, `INSERT INTO posts (id,source,account_handle,type,text,published_at,url,quote_json) VALUES (?,'x','kano','daily','text','2026-10-11T00:00:00.000Z',?,?)`, id, "https://x.com/kano/status/"+id, quote)
	}
	for position, name := range []string{"ready", "pending"} {
		status, cachePath := "ready", any(cache)
		if name == "pending" {
			status, cachePath = "pending", nil
		}
		testutil.Exec(t, path, `INSERT INTO media_assets(id,source,source_url,status,sha256,cache_path,created_at,updated_at) VALUES(?,'x',?,?,?,?,'2026-10-10T00:00:00Z','2026-10-10T00:00:00Z')`, sha256Hex(twimg(name)), twimg(name), status, hash, cachePath)
		testutil.Exec(t, path, `INSERT INTO media_links(media_id,owner_type,owner_id,role,position,created_at,updated_at) VALUES(?,'post','quoting','quote-image',?,'2026-10-10T00:00:00Z','2026-10-10T00:00:00Z')`, sha256Hex(twimg(name)), position)
	}
	store, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	dashboard, err := store.GetDashboard(context.Background(), 30, time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	quotes := map[string]*PostQuote{}
	for _, post := range dashboard.Posts {
		quotes[post.ID] = post.Quote
		// Quoted images never become the post's own.
		if len(post.Media) != 0 {
			t.Fatalf("%s media=%+v", post.ID, post.Media)
		}
	}
	q := quotes["quoting"]
	if q == nil || q.Text != "quoted wording" || q.URL != quoteURL || q.Unavailable || len(q.Media) != 1 || stringValue(q.Media[0].URL) != "/media/"+sha256Hex(twimg("ready"))+"?v="+hash {
		t.Fatalf("quote=%+v", q)
	}
	if u := quotes["unavailable"]; u == nil || !u.Unavailable || u.Text != "" || len(u.Media) != 0 {
		t.Fatalf("unavailable=%+v", u)
	}
	if quotes["foreign-url"] != nil {
		t.Fatalf("foreign url projected: %+v", quotes["foreign-url"])
	}
}

// Earlier releases linked a quoted status's images after the post's own as
// post-image. The upgrade moves exactly those to quote-image.
func TestUpgradeBackfillsQuotesAndMovesQuotedImages(t *testing.T) {
	p, _ := olderSnapshot(t)
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	qrt := `{"tweetID":"` + quotedID + `","text":"quoted wording","date_epoch":1760054400,"user_screen_name":"quoted_synthetic","user_name":"Quoted","tweetURL":"https://x.com/quoted_synthetic/status/` + quotedID + `","mediaURLs":["` + twimg("quoted") + `","` + twimg("shared") + `"],"media_extended":[]}`
	link := func(post, u, role string, position int) {
		t.Helper()
		id, normalized, err := mediaIdentity(u)
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range []string{
			`INSERT OR IGNORE INTO media_assets(id,source,source_url,status,created_at,updated_at) VALUES(?1,'x',?2,'pending','2026-03-01T00:00:00Z','2026-03-01T00:00:00Z')`,
			`INSERT INTO media_links(media_id,owner_type,owner_id,role,position,created_at,updated_at) VALUES(?1,'post',?3,?4,?5,'2026-03-01T00:00:00Z','2026-03-01T00:00:00Z')`,
		} {
			if _, err = db.Exec(statement, id, normalized, post, role, position); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, statement := range []string{
		`ALTER TABLE posts DROP COLUMN quote_json`,
		// Own image, then the quote's: one only the quote has, one both have.
		`INSERT INTO posts(id,source,account_handle,text,published_at,url,media_url,raw_json) VALUES('both','x','kano','look','2026-03-01T00:00:00.000Z','https://x.com/kano/status/1900000000000000100','` + twimg("own") + `','{"tweetID":"1900000000000000100","mediaURLs":["` + twimg("own") + `","` + twimg("shared") + `"],"qrt":` + qrt + `}')`,
		`INSERT INTO posts(id,source,account_handle,text,published_at,url,media_url,raw_json) VALUES('only-quote','x','kano','look','2026-03-02T00:00:00.000Z','https://x.com/kano/status/1900000000000000101','` + twimg("quoted") + `','{"tweetID":"1900000000000000101","mediaURLs":[],"qrt":` + qrt + `}')`,
		`INSERT INTO posts(id,source,account_handle,text,published_at,url,media_url,raw_json) VALUES('gone','x','kano','look','2026-03-03T00:00:00.000Z','https://x.com/kano/status/1900000000000000102',NULL,'{"tweetID":"1900000000000000102","mediaURLs":[],"qrt":null,"qrtURL":"https://x.com/quoted_synthetic/status/` + quotedID + `"}')`,
		`INSERT INTO posts(id,source,account_handle,text,published_at,url,media_url,raw_json) VALUES('plain','x','kano','plain','2026-03-04T00:00:00.000Z','https://x.com/kano/status/1900000000000000103','` + twimg("plain") + `','{"tweetID":"1900000000000000103","mediaURLs":["` + twimg("plain") + `"],"qrt":null}')`,
		`INSERT INTO posts(id,source,account_handle,text,published_at,url,media_url,raw_json) VALUES('labelled','x','kano','look','2026-03-05T00:00:00.000Z','https://x.com/kano/status/1900000000000000104','` + twimg("quoted") + `','{"tweetID":"1900000000000000104","mediaURLs":[],"qrt":` + qrt + `}')`,
		// Schedule candidates: a quoted image taken as the post's own, the
		// post's own image, and a quoted image an operator labelled.
		`INSERT INTO assets(id,kind,url,source_url,updated_at) VALUES('schedule-x-only-quote','schedule','` + twimg("quoted") + `','https://x.com/kano/status/1900000000000000101','2026-03-02T00:00:00Z')`,
		`INSERT INTO schedule_asset_reviews(asset_id,llm_status,manual_status,updated_at) VALUES('schedule-x-only-quote','schedule','unreviewed','2026-03-02T00:00:00Z')`,
		`INSERT INTO assets(id,kind,url,source_url,updated_at) VALUES('schedule-x-both','schedule','` + twimg("own") + `','https://x.com/kano/status/1900000000000000100','2026-03-01T00:00:00Z')`,
		`INSERT INTO assets(id,kind,url,source_url,updated_at) VALUES('schedule-x-labelled','schedule','` + twimg("quoted") + `','https://x.com/kano/status/1900000000000000104','2026-03-05T00:00:00Z')`,
		`INSERT INTO schedule_asset_reviews(asset_id,llm_status,manual_status,updated_at) VALUES('schedule-x-labelled','pending','schedule','2026-03-05T00:00:00Z')`,
	} {
		if _, err = db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	link("both", twimg("own"), "post-image", 0)
	link("both", twimg("shared"), "post-image", 1)
	link("both", twimg("quoted"), "post-image", 2)
	link("only-quote", twimg("quoted"), "post-image", 0)
	link("only-quote", twimg("shared"), "post-image", 1)
	link("plain", twimg("plain"), "post-image", 0)
	link("labelled", twimg("quoted"), "post-image", 0)
	for _, asset := range []string{"schedule-x-only-quote", "schedule-x-labelled"} {
		id, _, _ := mediaIdentity(twimg("quoted"))
		if _, err = db.Exec(`INSERT INTO media_links(media_id,owner_type,owner_id,role,position,created_at,updated_at) VALUES(?,'asset',?,'schedule-image',0,'2026-03-01T00:00:00Z','2026-03-01T00:00:00Z')`, id, asset); err != nil {
			t.Fatal(err)
		}
	}
	var revision string
	_ = db.QueryRow(`SELECT COALESCE((SELECT value FROM app_settings WHERE key='dashboard_revision'),'0')`).Scan(&revision)
	db.Close()

	for run := 0; run < 2; run++ {
		s, err := OpenStandalone(p)
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
	}
	db, _ = sql.Open("sqlite", "file:"+p+"?mode=ro")
	defer db.Close()
	quote := `{"id":"` + quotedID + `","authorHandle":"quoted_synthetic","authorName":"Quoted","text":"quoted wording","publishedAt":"2025-10-10T00:00:00Z","url":"https://x.com/quoted_synthetic/status/` + quotedID + `"}`
	for post, want := range map[string][4]string{
		"both":       {quote, twimg("own") + " " + twimg("shared"), twimg("quoted") + " " + twimg("shared"), twimg("own")},
		"only-quote": {quote, "", twimg("quoted") + " " + twimg("shared"), ""},
		"gone":       {`{"id":"` + quotedID + `","url":"https://x.com/i/status/` + quotedID + `","unavailable":true}`, "", "", ""},
		"plain":      {"", twimg("plain"), "", twimg("plain")},
		"labelled":   {quote, "", twimg("quoted") + " " + twimg("shared"), ""},
	} {
		var mediaURL sql.NullString
		if err = db.QueryRow(`SELECT media_url FROM posts WHERE id=?`, post).Scan(&mediaURL); err != nil {
			t.Fatal(err)
		}
		got := [4]string{storedQuote(t, db, post), linkedMedia(t, db, post, "post-image"), linkedMedia(t, db, post, "quote-image"), mediaURL.String}
		if got != want {
			t.Errorf("%s:\n got %q\nwant %q", post, got, want)
		}
	}
	for asset, want := range map[string]int{"schedule-x-only-quote": 0, "schedule-x-both": 1, "schedule-x-labelled": 1} {
		var assets, reviews, links int
		_ = db.QueryRow(`SELECT (SELECT COUNT(*) FROM assets WHERE id=?1),(SELECT COUNT(*) FROM schedule_asset_reviews WHERE asset_id=?1),(SELECT COUNT(*) FROM media_links WHERE owner_type='asset' AND owner_id=?1)`, asset).Scan(&assets, &reviews, &links)
		if assets != want || (want == 0 && reviews+links != 0) {
			t.Errorf("%s: assets=%d reviews=%d links=%d, want %d", asset, assets, reviews, links, want)
		}
	}
	var after string
	if err = db.QueryRow(`SELECT value FROM app_settings WHERE key='dashboard_revision'`).Scan(&after); err != nil || after == revision {
		t.Fatalf("revision %s -> %s err=%v", revision, after, err)
	}
}

// An operator's hide outlives every rewrite of the stored quote: a sync
// write, the X API window and the upgrade backfill. The public dashboard and
// archive leave a hidden quote out; the console still reads it.
func TestHiddenQuoteSurvivesRewritesAndStaysOffPublicPages(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	id := "1900000000000000100"
	quote := func(text string) *xsource.Quote {
		return &xsource.Quote{ID: quotedID, AuthorHandle: "quoted_synthetic", Text: text, URL: "https://x.com/quoted_synthetic/status/" + quotedID}
	}
	post := xsource.Post{ID: id, AccountHandle: "kano", Type: "daily", Text: "look", PublishedAt: "2026-10-11T00:00:00Z", URL: "https://x.com/kano/status/" + id, Quote: quote("first"), Raw: map[string]any{}}
	write := func(p xsource.Post) {
		t.Helper()
		if err := s.WriteXRecent(ctx, "kano", xsource.Result{Account: "kano", Posts: []xsource.Post{p}}, true); err != nil {
			t.Fatal(err)
		}
	}
	write(post)
	if _, err = s.DB().Exec(`UPDATE posts SET quote_hidden_at='2026-10-12T00:00:00.000Z' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	// A detail rewrite updates the stored quote underneath the decision.
	post.Quote = quote("rewritten")
	write(post)
	unread := post
	unread.Quote, unread.QuoteUnread = nil, true
	write(unread)
	// The backfill reads only rows without a projection; clear it to make
	// the backfill rewrite this row from its raw_json as an upgrade would.
	if _, err = s.DB().Exec(`UPDATE posts SET quote_json=NULL,raw_json=? WHERE id=?`, `{"quote":{"type":"status","id":"`+quotedID+`","text":"backfilled","author":{"screen_name":"quoted_synthetic"}}}`, id); err != nil {
		t.Fatal(err)
	}
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = backfillQuotes(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var hidden, text sql.NullString
	if err = s.DB().QueryRow(`SELECT quote_hidden_at,json_extract(quote_json,'$.text') FROM posts WHERE id=?`, id).Scan(&hidden, &text); err != nil || hidden.String != "2026-10-12T00:00:00.000Z" || text.String != "backfilled" {
		t.Fatalf("hidden=%v text=%v err=%v", hidden, text, err)
	}

	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	dashboard, err := s.GetDashboard(ctx, 30, now)
	if err != nil || len(dashboard.Posts) != 1 || dashboard.Posts[0].Quote != nil || dashboard.Summary.LatestPost == nil || dashboard.Summary.LatestPost.Quote != nil {
		t.Fatalf("dashboard quote=%+v err=%v", dashboard, err)
	}
	archive, err := s.GetPostArchive(ctx, "2026-10-01", "2026-10-31", "", 1, 20)
	if err != nil || len(archive.Items) != 1 || archive.Items[0].Quote != nil {
		t.Fatalf("archive=%+v err=%v", archive, err)
	}
	admin, err := s.ListAdminPosts(ctx, AdminPostFilter{})
	if err != nil || len(admin.Items) != 1 || admin.Items[0].Quote == nil || stringValue(admin.Items[0].Quote.Text) != "backfilled" || stringValue(admin.Items[0].QuoteHiddenAt) != "2026-10-12T00:00:00.000Z" {
		t.Fatalf("admin=%+v err=%v", admin, err)
	}
}

// A queued refresh is listed for the account's next run and cleared once
// the run made its detail request; the detail answer replaces the quote.
func TestRefreshRequestIsReadAndClearedByTheRun(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	id := "1900000000000000100"
	post := xsource.Post{ID: id, AccountHandle: "kano", Type: "daily", Text: "look", PublishedAt: "2026-10-11T00:00:00Z", URL: "https://x.com/kano/status/" + id,
		Quote: &xsource.Quote{ID: quotedID, AuthorHandle: "quoted_synthetic", Text: "old", URL: "https://x.com/quoted_synthetic/status/" + quotedID}, Raw: map[string]any{}}
	if err = s.WriteXRecent(ctx, "kano", xsource.Result{Account: "kano", Posts: []xsource.Post{post}}, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`UPDATE posts SET refresh_requested_at='2026-10-12T00:00:00.000Z',quote_hidden_at='2026-10-12T00:00:00.000Z' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	state, err := s.XRecentState(ctx, "KANO")
	if err != nil || strings.Join(state.RefreshIDs, ",") != id {
		t.Fatalf("refresh ids=%v err=%v", state.RefreshIDs, err)
	}
	post.Quote = &xsource.Quote{ID: quotedID, AuthorHandle: "quoted_synthetic", Text: "fresh", URL: "https://x.com/quoted_synthetic/status/" + quotedID, MediaURLs: []string{twimg("fresh")}}
	if err = s.WriteXRecent(ctx, "kano", xsource.Result{Account: "kano", Posts: []xsource.Post{post}, Refreshed: []string{id}}, true); err != nil {
		t.Fatal(err)
	}
	var refresh, hidden, text sql.NullString
	if err = s.DB().QueryRow(`SELECT refresh_requested_at,quote_hidden_at,json_extract(quote_json,'$.text') FROM posts WHERE id=?`, id).Scan(&refresh, &hidden, &text); err != nil || refresh.Valid || !hidden.Valid || text.String != "fresh" {
		t.Fatalf("refresh=%v hidden=%v text=%v err=%v", refresh, hidden, text, err)
	}
	db, _ := sql.Open("sqlite", "file:"+p+"?mode=ro")
	defer db.Close()
	if got := linkedMedia(t, db, id, "quote-image"); got != twimg("fresh") {
		t.Fatalf("quote images=%s", got)
	}
	if state, err = s.XRecentState(ctx, "kano"); err != nil || len(state.RefreshIDs) != 0 {
		t.Fatalf("refresh ids after the run=%v err=%v", state.RefreshIDs, err)
	}
}
