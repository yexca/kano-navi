package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/yexca/kano-navi/backend/internal/source/history"
	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
	"github.com/yexca/kano-navi/backend/internal/testutil"
)

const agencyID = "1900000000000000201"

func agencyQuote(text string) *xsource.Quote {
	return &xsource.Quote{ID: agencyID, AuthorHandle: "agency_synthetic", AuthorName: "Agency", Text: text, PublishedAt: "2026-10-09T00:00:00Z",
		URL: "https://x.com/agency_synthetic/status/" + agencyID, MediaURLs: []string{twimg("agency")}}
}

func quotingPost(id, published string, quote *xsource.Quote) xsource.Post {
	return xsource.Post{ID: id, AccountHandle: "kano", Type: "daily", Text: "これ！", PublishedAt: published,
		URL: "https://x.com/kano/status/" + id, Quote: quote, Raw: map[string]any{}}
}

func poolRow(t *testing.T, db *sql.DB, id string) (text, quotedAt, raw string) {
	t.Helper()
	var q, r sql.NullString
	if err := db.QueryRow(`SELECT text,quoted_at,raw_json FROM x_statuses WHERE id=?`, id).Scan(&text, &q, &r); err != nil {
		t.Fatalf("pool row %s: %v", id, err)
	}
	return text, q.String, r.String
}

func TestQuotedStatusesAndRepostsEnterThePool(t *testing.T) {
	p := testutil.NewSnapshot(t)
	s, err := OpenWorkflowCore(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	first := quotingPost("1900000000000000110", "2026-10-11T00:00:00Z", agencyQuote("10月20日 20時から配信"))
	gone := quotingPost("1900000000000000111", "2026-10-11T01:00:00Z", &xsource.Quote{ID: "1900000000000000202", URL: "https://x.com/i/status/1900000000000000202", Unavailable: true})
	result := xsource.Result{Account: "kano", Posts: []xsource.Post{first, gone}, Reposts: []xsource.Repost{
		{ID: "1900000000000000203", Author: "agency_synthetic", Status: agencyQuote("reposted")},
		{ID: "1900000000000000204"},
	}}
	if err = s.WriteXRecent(ctx, "kano", result, true); err != nil {
		t.Fatal(err)
	}
	db := s.DB()
	// The pool holds the quote with the projection's text and its images.
	text, quotedAt, raw := poolRow(t, db, agencyID)
	var projected string
	if err = db.QueryRow(`SELECT json_extract(quote_json,'$.text') FROM posts WHERE id=?`, first.ID).Scan(&projected); err != nil {
		t.Fatal(err)
	}
	if text != projected || quotedAt != first.PublishedAt || raw != `{"author":{"name":"Agency","screen_name":"agency_synthetic"},"created_at":"2026-10-09T00:00:00Z","id":"`+agencyID+`","mediaURLs":["`+twimg("agency")+`"],"text":"10月20日 20時から配信","url":"https://x.com/agency_synthetic/status/`+agencyID+`"}` {
		t.Fatalf("pool text=%q projected=%q quoted_at=%q raw=%s", text, projected, quotedAt, raw)
	}
	// An unavailable quote carries no status; nothing of the quote gets a
	// card, a media link or a schedule candidate of its own.
	var pooled, links, assets int
	if err = db.QueryRow(`SELECT (SELECT COUNT(*) FROM x_statuses),(SELECT COUNT(*) FROM media_links WHERE owner_type<>'post' OR role NOT IN ('post-image','quote-image')),(SELECT COUNT(*) FROM assets)`).Scan(&pooled, &links, &assets); err != nil {
		t.Fatal(err)
	}
	if pooled != 1 || links != 0 || assets != 0 {
		t.Fatalf("pooled=%d links=%d assets=%d", pooled, links, assets)
	}
	// Reposts record links only: the board shows none, and a status of an
	// author that is not a related account stays out of the pool.
	var reposts int
	var author sql.NullString
	if err = db.QueryRow(`SELECT COUNT(*),MAX(author_handle) FROM x_reposts WHERE account_handle='kano'`).Scan(&reposts, &author); err != nil {
		t.Fatal(err)
	}
	if reposts != 2 || author.String != "agency_synthetic" {
		t.Fatalf("reposts=%d author=%v", reposts, author)
	}

	// A later quote of the same status moves quoted_at forward; an older one
	// keeps it.
	later := quotingPost("1900000000000000112", "2026-10-12T00:00:00Z", agencyQuote("10月20日 20時から配信"))
	older := quotingPost("1900000000000000113", "2026-10-01T00:00:00Z", agencyQuote("10月20日 20時から配信"))
	for _, post := range []xsource.Post{later, older} {
		if err = s.WriteXRecent(ctx, "kano", xsource.Result{Account: "kano", Posts: []xsource.Post{post}}, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, quotedAt, _ = poolRow(t, db, agencyID); quotedAt != later.PublishedAt {
		t.Fatalf("quoted_at=%q", quotedAt)
	}
}

func TestStartupFillsThePoolFromStoredQuotes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database", "kano.sqlite")
	s, err := OpenStandalone(path)
	if err != nil {
		t.Fatal(err)
	}
	qrt := `{"tweetID":"` + agencyID + `","text":"quoted wording","date_epoch":1760054400,"user_screen_name":"agency_synthetic","user_name":"Agency","tweetURL":"https://x.com/agency_synthetic/status/` + agencyID + `","mediaURLs":["` + twimg("agency") + `"],"media_extended":[]}`
	for _, statement := range []string{
		// A quote the backfill of quote_json still has to project, and one
		// stored before the pool existed.
		`INSERT INTO posts(id,source,account_handle,text,published_at,url,raw_json) VALUES('1900000000000000120','x','kano','look','2026-10-11T00:00:00.000Z','https://x.com/kano/status/1900000000000000120','{"tweetID":"1900000000000000120","mediaURLs":[],"qrt":` + qrt + `}')`,
		`INSERT INTO posts(id,source,account_handle,text,published_at,url,raw_json,quote_json) VALUES('1900000000000000121','x','kano','again','2026-10-12T00:00:00.000Z','https://x.com/kano/status/1900000000000000121','{"tweetID":"1900000000000000121","mediaURLs":[],"qrt":` + qrt + `}','{"id":"` + agencyID + `","authorHandle":"agency_synthetic","authorName":"Agency","text":"quoted wording","url":"https://x.com/agency_synthetic/status/` + agencyID + `"}')`,
	} {
		if _, err = s.DB().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	for range 2 {
		if s, err = OpenStandalone(path); err != nil {
			t.Fatal(err)
		}
		text, quotedAt, raw := poolRow(t, s.DB(), agencyID)
		if text != "quoted wording" || quotedAt != "2026-10-12T00:00:00.000Z" || raw == "" {
			t.Fatalf("text=%q quoted_at=%q raw=%s", text, quotedAt, raw)
		}
		s.Close()
	}
}

// A quoting post whose time does not read is filled once, not rewritten on
// every later startup.
func TestStartupFillsAnUnreadableQuoteTimeOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database", "kano.sqlite")
	s, err := OpenStandalone(path)
	if err != nil {
		t.Fatal(err)
	}
	qrt := `{"tweetID":"` + agencyID + `","text":"quoted wording","date_epoch":1760054400,"user_screen_name":"agency_synthetic","user_name":"Agency","tweetURL":"https://x.com/agency_synthetic/status/` + agencyID + `","mediaURLs":[],"media_extended":[]}`
	if _, err = s.DB().Exec(`INSERT INTO posts(id,source,account_handle,text,published_at,url,raw_json,quote_json) VALUES('1900000000000000130','x','kano','look','not a time','https://x.com/kano/status/1900000000000000130','{"tweetID":"1900000000000000130","mediaURLs":[],"qrt":` + qrt + `}','{"id":"` + agencyID + `","authorHandle":"agency_synthetic","text":"quoted wording","url":"https://x.com/agency_synthetic/status/` + agencyID + `"}')`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	const pinned = "2000-01-01T00:00:00.000Z"
	for i := range 2 {
		if s, err = OpenStandalone(path); err != nil {
			t.Fatal(err)
		}
		var updated string
		err = s.DB().QueryRow(`SELECT updated_at FROM x_statuses WHERE id=?`, agencyID).Scan(&updated)
		if err == nil {
			_, err = s.DB().Exec(`UPDATE x_statuses SET updated_at=? WHERE id=?`, pinned, agencyID)
		}
		s.Close()
		if err != nil {
			t.Fatal(err)
		}
		if i == 1 && updated != pinned {
			t.Fatalf("startup rewrote the pool row: updated_at=%s", updated)
		}
	}
}

func TestTranslationTakesPooledQuotesAfterPosts(t *testing.T) {
	s, err := OpenStandalone(filepath.Join(t.TempDir(), "database", "kano.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	for _, statement := range []string{
		`INSERT INTO posts(id,source,account_handle,text,published_at,url) VALUES('1900000000000000130','x','kano','今日は配信','2026-10-19T00:00:00.000Z','https://x.com/kano/status/1900000000000000130')`,
		// Quoted within the lookback, though published long before it.
		`INSERT INTO x_statuses(id,author_handle,text,published_at,url,quoted_at,first_seen_at,updated_at) VALUES('` + agencyID + `','agency_synthetic','告知','2026-01-01T00:00:00.000Z','https://x.com/agency_synthetic/status/` + agencyID + `','2026-10-19T12:00:00.000Z','2026-10-19T12:00:00.000Z','2026-10-19T12:00:00.000Z')`,
		// Quoted only before the lookback.
		`INSERT INTO x_statuses(id,author_handle,text,url,quoted_at,first_seen_at,updated_at) VALUES('1900000000000000205','agency_synthetic','古い告知','https://x.com/agency_synthetic/status/1900000000000000205','2026-09-01T00:00:00.000Z','2026-09-01T00:00:00.000Z','2026-09-01T00:00:00.000Z')`,
		// A self-quote: the stored post owns the translation.
		`INSERT INTO x_statuses(id,author_handle,text,url,quoted_at,first_seen_at,updated_at) VALUES('1900000000000000130','kano','今日は配信','https://x.com/kano/status/1900000000000000130','2026-10-19T13:00:00.000Z','2026-10-19T13:00:00.000Z','2026-10-19T13:00:00.000Z')`,
	} {
		if _, err = s.DB().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	candidates, err := s.ListTranslationCandidates(ctx, now.AddDate(0, 0, -7), 10)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, c := range candidates {
		got = append(got, c.OwnerType+":"+c.OwnerID+":"+c.Source)
	}
	if want := []string{"post:1900000000000000130:x", "x_status:" + agencyID + ":x"}; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("candidates=%v", got)
	}
	saved, err := s.SaveMachineTranslation(ctx, MachineTranslation{OwnerType: TranslationOwnerXStatus, OwnerID: agencyID, SourceHash: TranslationHash("告知"),
		SourceLanguage: "ja", Texts: map[string]string{"en": "Announcement", "zh-CN": "公告"}, ProviderID: "p", ModelID: "m"}, now)
	if err != nil || !saved {
		t.Fatalf("saved=%v err=%v", saved, err)
	}
	if candidates, err = s.ListTranslationCandidates(ctx, now.AddDate(0, 0, -7), 10); err != nil || len(candidates) != 1 || candidates[0].OwnerType != TranslationOwnerPost {
		t.Fatalf("after translation=%+v err=%v", candidates, err)
	}
}

func TestQuoteCardsResolvePooledTranslationsWithoutThePool(t *testing.T) {
	path := testutil.NewSnapshot(t)
	quote := func(text string) string {
		return `{"id":"` + agencyID + `","authorHandle":"agency_synthetic","text":"` + text + `","url":"https://x.com/agency_synthetic/status/` + agencyID + `"}`
	}
	testutil.Exec(t, path, `INSERT INTO posts (id,source,account_handle,type,text,published_at,url,quote_json) VALUES ('current','x','kano','daily','これ','2026-10-11T00:00:00.000Z','https://x.com/kano/status/1900000000000000140',?)`, quote("新しい告知"))
	testutil.Exec(t, path, `INSERT INTO posts (id,source,account_handle,type,text,published_at,url,quote_json) VALUES ('edited','x','kano','daily','これも','2026-10-11T01:00:00.000Z','https://x.com/kano/status/1900000000000000141',?)`, quote("古い告知"))
	current := TranslationHash("新しい告知")
	for _, row := range []struct{ locale, origin, text, hash string }{
		{"en", "machine", "New announcement", current},
		{"zh-CN", "machine", "旧公告", TranslationHash("古い告知")},
		{"ja", "manual", "運営の告知", ""},
	} {
		testutil.Exec(t, path, `INSERT INTO translations (owner_type,owner_id,field,locale,origin,text,source_hash,created_at,updated_at) VALUES ('x_status',?,'text',?,?,?,?,'','')`, agencyID, row.locale, row.origin, row.text, row.hash)
	}
	testutil.Exec(t, path, `INSERT INTO translation_states (owner_type,owner_id,field,source_hash,source_language,status,updated_at) VALUES ('x_status',?,'text',?,'zh','success','')`, agencyID, current)
	// Public reads never touch the pool.
	testutil.Exec(t, path, `DROP TABLE x_statuses`)
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
	}
	if q := quotes["current"]; q == nil || q.Translations.Locales["en"].Text != "New announcement" || q.Translations.Locales["ja"].Text != "運営の告知" ||
		len(q.Translations.Locales) != 2 || q.Translations.SourceLanguage == nil || *q.Translations.SourceLanguage != "zh" {
		t.Fatalf("current quote=%+v", q)
	}
	// Machine rows and the language follow the wording each quote shows.
	if q := quotes["edited"]; q == nil || q.Translations.Locales["zh-CN"].Text != "旧公告" || q.Translations.Locales["ja"].Text != "運営の告知" ||
		len(q.Translations.Locales) != 2 || q.Translations.SourceLanguage != nil {
		t.Fatalf("edited quote=%+v", q)
	}
}

func TestFetchPagesRecordRepostsAndQuotedStatuses(t *testing.T) {
	s, query := openHistoryStore(t)
	ctx := context.Background()
	page := history.Page{Source: "x", Account: "synthetic", Window: history.Window{Mode: "count", Count: 20},
		Posts:   []xsource.Post{quotingPost("1900000000000000150", "2026-10-11T00:00:00Z", agencyQuote("告知"))},
		Reposts: []xsource.Repost{{ID: "1900000000000000206", Author: "agency_synthetic"}}}
	if _, err := s.WriteHistoryPage(ctx, page); err != nil {
		t.Fatal(err)
	}
	if got := query(`SELECT (SELECT text FROM x_statuses WHERE id='` + agencyID + `')||'/'||(SELECT account_handle||':'||author_handle FROM x_reposts)`); got != "告知/synthetic:agency_synthetic" {
		t.Fatalf("pool and reposts=%s", got)
	}
}
