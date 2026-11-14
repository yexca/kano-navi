package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/yexca/kano-navi/backend/internal/source/history"
	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
)

func TestEveryRetweetIDStaysBlockedAcrossRestartAndImports(t *testing.T) {
	ctx := context.Background()
	filename := filepath.Join(t.TempDir(), "database", "kano.sqlite")
	s, err := OpenStandalone(filename)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for _, id := range []string{storedRetweet, retweetSibling, storedRetweet} {
		if err = s.WriteXRecent(ctx, "kano_2525", xsource.Result{Reposts: []xsource.Repost{{ID: retweetedID, Author: "someone", RetweetID: id}}}, true); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStandalone(filename)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{storedRetweet, retweetSibling} {
		post := xsource.Post{ID: id, AccountHandle: "kano_2525", Type: "daily", Text: "RT @someone: notice", PublishedAt: "2026-10-10T00:00:00Z",
			URL: "https://x.com/kano_2525/status/" + id, Raw: map[string]any{}}
		if err = s.WriteXRecent(ctx, "kano_2525", xsource.Result{Posts: []xsource.Post{post}}, true); err != nil {
			t.Fatal(err)
		}
		written, writeErr := s.WriteHistoryPage(ctx, history.Page{Source: "x", Account: "kano_2525", Window: history.Window{Mode: "count", Count: 20}, Posts: []xsource.Post{post}})
		if writeErr != nil || written.New != 0 {
			t.Fatalf("history resurrected %s: %+v err=%v", id, written, writeErr)
		}
	}
	var count int
	if err = s.DB().QueryRow(`SELECT COUNT(*) FROM posts`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("reposts resurrected: count=%d err=%v", count, err)
	}
	if err = s.DB().QueryRow(`SELECT COUNT(*) FROM x_repost_ids`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("durable identities: count=%d err=%v", count, err)
	}
}

func TestStartupBackfillsLegacyRetweetIdentity(t *testing.T) {
	ctx := context.Background()
	filename := filepath.Join(t.TempDir(), "database", "kano.sqlite")
	s, err := OpenStandalone(filename)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	const stamp = "2026-10-10T00:00:00Z"
	for _, statement := range []string{
		`INSERT INTO x_reposts(status_id,account_handle,retweet_id,first_seen_at) VALUES('` + retweetedID + `','kano_2525','` + storedRetweet + `','` + stamp + `')`,
		`INSERT INTO posts(id,source,text,published_at,url,account_handle) VALUES('` + retweetSibling + `','x','kept','` + stamp + `','https://x.com/kano_2525/status/` + retweetSibling + `','kano_2525')`,
		`DROP TABLE x_repost_ids`,
	} {
		if _, err = s.DB().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if old, openErr := OpenInitialized(filename, true, true); openErr == nil {
		old.Close()
		t.Fatal("the schema verifier accepted a missing repost identity table")
	}
	for range 2 {
		s, err = OpenStandalone(filename)
		if err != nil {
			t.Fatal(err)
		}
		var got string
		if err = s.DB().QueryRow(`SELECT retweet_id||'|'||status_id||'|'||account_handle||'|'||first_seen_at FROM x_repost_ids`).Scan(&got); err != nil || got != storedRetweet+"|"+retweetedID+"|kano_2525|"+stamp {
			t.Fatalf("backfilled identity=%q err=%v", got, err)
		}
		post := xsource.Post{ID: storedRetweet, AccountHandle: "kano_2525", Type: "daily", Text: "RT @someone: notice", PublishedAt: stamp,
			URL: "https://x.com/kano_2525/status/" + storedRetweet, Raw: map[string]any{}}
		if err = s.WriteXRecent(ctx, "kano_2525", xsource.Result{Posts: []xsource.Post{post}}, true); err != nil {
			t.Fatal(err)
		}
		if err = s.DB().QueryRow(`SELECT id||'|'||text FROM posts`).Scan(&got); err != nil || got != retweetSibling+"|kept" {
			t.Fatalf("snapshot after upgrade/import=%q err=%v", got, err)
		}
		if err = s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

const (
	storedRetweet  = "1900000000000000701"
	retweetedID    = "1900000000000000702"
	retweetSibling = "1900000000000000703"
)

// A stored post that a detail answer shows to be the account's repost is
// soft-deleted with everything keyed to it kept, no reader shows it, its
// evidence retires the events it alone supports, the reposted status enters
// the pool, and no later write stores the repost's ID as a post again.
func TestDetailRepostSoftDeletesTheStoredPost(t *testing.T) {
	f := newRelatedFixture(t)
	ctx := context.Background()
	db := f.s.DB()
	imported := xsource.Post{ID: storedRetweet, AccountHandle: "kano_2525", Type: "daily", Text: "RT @yura_synthetic: 配信予定", PublishedAt: "2026-10-10T00:00:00Z",
		URL: "https://x.com/kano_2525/status/" + storedRetweet, MediaURLs: []string{twimg("retweeted")}, Raw: map[string]any{"import": map[string]any{"source": "synthetic"}}}
	sibling := xsource.Post{ID: retweetSibling, AccountHandle: "kano_2525", Type: "daily", Text: "own", PublishedAt: "2026-10-10T01:00:00Z",
		URL: "https://x.com/kano_2525/status/" + retweetSibling, Raw: map[string]any{}}
	if err := f.s.WriteXRecent(ctx, "kano_2525", xsource.Result{Account: "kano_2525", Posts: []xsource.Post{imported, sibling},
		ScheduleCandidates: []xsource.ScheduleCandidate{{PostID: storedRetweet, SourceURL: imported.URL, MediaURL: twimg("retweeted"), AccountHandle: "kano_2525"}}}, true); err != nil {
		t.Fatal(err)
	}
	stamp := "2026-10-11T00:00:00.000Z"
	for _, statement := range []string{
		`UPDATE posts SET refresh_requested_at='` + stamp + `' WHERE id='` + storedRetweet + `'`,
		`INSERT INTO post_llm_states(post_id,status,updated_at) VALUES('` + storedRetweet + `','processed','` + stamp + `')`,
		`INSERT INTO schedule_extractions(source,source_item_id,content_fingerprint,extractor_version,model,status,created_at,updated_at) VALUES('x','` + storedRetweet + `','f','v','m','success','` + stamp + `','` + stamp + `')`,
		`INSERT INTO translation_states(owner_type,owner_id,field,status,updated_at) VALUES('post','` + storedRetweet + `','text','done','` + stamp + `')`,
		`INSERT INTO translations(owner_type,owner_id,field,locale,origin,text,source_hash,created_at,updated_at) VALUES('post','` + storedRetweet + `','text','en','machine','RT','h','` + stamp + `','` + stamp + `')`,
		// The retweet alone supports an automatic event and a locked one; a
		// second post also supports another; a rule merged one into the first.
		`INSERT INTO events(id,source,title,starts_on,created_at,updated_at) VALUES('ev-alone','x','alone','2026-10-20','` + stamp + `','` + stamp + `'),('ev-shared','x','shared','2026-10-21','` + stamp + `','` + stamp + `')`,
		`INSERT INTO events(id,source,title,starts_on,provenance,manual_locked,created_at,updated_at) VALUES('ev-locked','x','locked','2026-10-22','manual',1,'` + stamp + `','` + stamp + `')`,
		`INSERT INTO events(id,source,title,starts_on,merged_into,merge_source,created_at,updated_at) VALUES('ev-merged','x','merged','2026-10-20','ev-alone','rule','` + stamp + `','` + stamp + `')`,
		`INSERT INTO event_sources(event_id,source,source_item_id,source_key,created_at,updated_at) VALUES
			('ev-alone','x','` + storedRetweet + `','a','` + stamp + `','` + stamp + `'),('ev-shared','x','` + storedRetweet + `','b','` + stamp + `','` + stamp + `'),
			('ev-shared','x','` + retweetSibling + `','b','` + stamp + `','` + stamp + `'),('ev-locked','x','` + storedRetweet + `','c','` + stamp + `','` + stamp + `')`,
		// The retweet raised a model's cancellation warning on one event; an
		// operator confirmed another's cancellation from it.
		`UPDATE events SET cancellation_status='llm_suspected',cancellation_source='llm',cancellation_reason='r',cancellation_source_item_id='` + storedRetweet + `',cancellation_at='` + stamp + `' WHERE id='ev-shared'`,
		`UPDATE events SET cancellation_status='manual_confirmed',cancellation_source='manual',cancellation_source_item_id='` + storedRetweet + `',cancellation_at='` + stamp + `' WHERE id='ev-locked'`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	revision := f.text(`SELECT value FROM app_settings WHERE key='dashboard_revision'`)

	// The operator's refresh answers as the repost.
	original := &xsource.Quote{ID: retweetedID, AuthorHandle: "yura_synthetic", AuthorName: "Yura", Text: "配信予定", PublishedAt: "2026-10-09T23:00:00Z",
		URL: "https://x.com/yura_synthetic/status/" + retweetedID, MediaURLs: []string{twimg("retweeted")}}
	repost := xsource.Result{Account: "kano_2525", RefreshOnly: true, Refreshed: []string{storedRetweet},
		Dropped: []xsource.DroppedStatus{{ID: storedRetweet, Code: xsource.DroppedRepost}},
		Reposts: []xsource.Repost{{ID: retweetedID, Author: "yura_synthetic", Status: original, RetweetID: storedRetweet}}}
	if err := f.s.WriteXRecent(ctx, "kano_2525", repost, true); err != nil {
		t.Fatal(err)
	}
	keyed := `SELECT (SELECT COUNT(*) FROM posts WHERE id=$1)||','||(SELECT COUNT(*) FROM media_links WHERE owner_id IN ($1,'schedule-x-'||$1))||','||
		(SELECT COUNT(*) FROM assets WHERE id='schedule-x-'||$1)||','||(SELECT COUNT(*) FROM schedule_asset_reviews WHERE asset_id='schedule-x-'||$1)||','||
		(SELECT COUNT(*) FROM post_llm_states WHERE post_id=$1)||','||(SELECT COUNT(*) FROM schedule_extractions WHERE source_item_id=$1)||','||
		(SELECT COUNT(*) FROM translations WHERE owner_id=$1)||','||(SELECT COUNT(*) FROM translation_states WHERE owner_id=$1)`
	// A soft delete: the row and everything keyed to it stay.
	if got := f.text(keyed, storedRetweet); got != "1,2,1,1,1,1,1,1" {
		t.Fatalf("rows keyed to the deleted post: %s", got)
	}
	if got := f.text(`SELECT (deleted_at IS NOT NULL)||','||(refresh_requested_at IS NULL)||','||(hidden_at IS NULL) FROM posts WHERE id=?`, storedRetweet); got != "1,1,1" {
		t.Fatalf("deleted post state: %s", got)
	}
	if got := f.text(`SELECT COUNT(*) FROM posts WHERE id=? AND deleted_at IS NULL`, retweetSibling); got != "1" {
		t.Fatal("the account's other post was deleted")
	}
	// No reader shows the deleted post or its schedule-image candidate.
	archive, err := f.s.GetPostArchive(ctx, "2026-10-01", "2026-10-31", "", 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if archive.Total != 1 || len(archive.Items) != 1 || archive.Items[0].ID != retweetSibling {
		t.Fatalf("public archive: total=%d items=%+v", archive.Total, archive.Items)
	}
	admin, err := f.s.ListAdminPosts(ctx, AdminPostFilter{ID: storedRetweet})
	if err != nil || admin.Total != 0 {
		t.Fatalf("admin posts: %+v err=%v", admin, err)
	}
	if got := f.text(`SELECT COUNT(*) FROM assets a WHERE ` + LiveScheduleAssetSQL("a")); got != "0" {
		t.Fatalf("live schedule candidates: %s", got)
	}
	if got := f.text(`SELECT COUNT(*) FROM posts p WHERE `+TranslatedPostSQL("p")+` AND p.id=?`, storedRetweet); got != "0" {
		t.Fatalf("translation scope keeps the deleted post: %s", got)
	}
	if got := f.text(`SELECT status_id||'|'||account_handle||'|'||author_handle||'|'||retweet_id FROM x_reposts`); got != retweetedID+"|kano_2525|yura_synthetic|"+storedRetweet {
		t.Fatalf("repost link: %s", got)
	}
	if got := f.text(`SELECT text||'|'||(reposted_at IS NOT NULL) FROM x_statuses WHERE id=?`, retweetedID); got != "配信予定|1" {
		t.Fatalf("pool row: %s", got)
	}
	events := `SELECT group_concat(id||':'||(deleted_at IS NOT NULL)||':'||COALESCE(merged_into,'-'),',') FROM (SELECT * FROM events ORDER BY id)`
	if got := f.text(events); got != "ev-alone:1:-,ev-locked:0:-,ev-merged:0:-,ev-shared:0:-" {
		t.Fatalf("events: %s", got)
	}
	cancellations := `SELECT group_concat(id||':'||cancellation_status||':'||COALESCE(cancellation_source_item_id,'-'),',') FROM (SELECT * FROM events WHERE id IN ('ev-shared','ev-locked') ORDER BY id)`
	if got := f.text(cancellations); got != "ev-locked:manual_confirmed:"+storedRetweet+",ev-shared:none:-" {
		t.Fatalf("cancellations: %s", got)
	}
	if got := f.text(`SELECT COUNT(*) FROM event_sources WHERE source_item_id=? AND deleted_at IS NULL`, storedRetweet); got != "0" {
		t.Fatalf("active evidence of the removed post: %s", got)
	}
	if f.text(`SELECT value FROM app_settings WHERE key='dashboard_revision'`) == revision {
		t.Fatal("deleting a post must change the public snapshot revision")
	}
	deletedAt := f.text(`SELECT deleted_at FROM posts WHERE id=?`, storedRetweet)

	// Neither a listing, a fetch page nor an import write stores it again.
	if err := f.s.WriteXRecent(ctx, "kano_2525", xsource.Result{Account: "kano_2525", Posts: []xsource.Post{imported, sibling},
		ScheduleCandidates: []xsource.ScheduleCandidate{{PostID: storedRetweet, SourceURL: imported.URL, MediaURL: twimg("retweeted"), AccountHandle: "kano_2525"}}}, true); err != nil {
		t.Fatal(err)
	}
	written, err := f.s.WriteHistoryPage(ctx, history.Page{Source: "x", Account: "kano_2525", Window: history.Window{Mode: "count", Count: 20}, Posts: []xsource.Post{imported}})
	if err != nil || written.New != 0 {
		t.Fatalf("history written=%+v err=%v", written, err)
	}
	if got := f.text(`SELECT COALESCE(deleted_at,'')||'|'||text FROM posts WHERE id=?`, storedRetweet); got != deletedAt+"|"+imported.Text {
		t.Fatalf("the repost came back: %s", got)
	}
	// A second repost answer leaves the deleted post as it is.
	if err := f.s.WriteXRecent(ctx, "kano_2525", repost, true); err != nil {
		t.Fatal(err)
	}
	if got := f.text(`SELECT deleted_at FROM posts WHERE id=?`, storedRetweet); got != deletedAt {
		t.Fatalf("deleted_at moved: %s", got)
	}
}

// Startup marks pooled statuses an own account reposted before the pool took
// every repost, from the stored links alone.
func TestStartupMarksPooledStatusesReposted(t *testing.T) {
	f := newRelatedFixture(t)
	db := f.s.DB()
	for _, statement := range []string{
		`INSERT INTO x_statuses(id,author_handle,text,url,quoted_at,first_seen_at,updated_at) VALUES('` + retweetedID + `','yura_synthetic','quoted','https://x.com/yura_synthetic/status/` + retweetedID + `','2026-10-01T00:00:00.000Z','2026-10-01T00:00:00.000Z','2026-10-01T00:00:00.000Z')`,
		`INSERT INTO x_reposts(status_id,account_handle,author_handle,first_seen_at) VALUES('` + retweetedID + `','kano_2525','yura_synthetic','2026-10-03T00:00:00.000Z'),('` + retweetedID + `','_kanotic','yura_synthetic','2026-10-02T00:00:00.000Z'),('1900000000000000799','kano_2525',NULL,'2026-10-02T00:00:00.000Z')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = backfillRepostedStatuses(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	var got string
	if err = tx.QueryRow(`SELECT group_concat(id||':'||COALESCE(reposted_at,'-'),',') FROM x_statuses`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != retweetedID+":2026-10-02T00:00:00.000Z" {
		t.Fatalf("pool after backfill: %s", got)
	}
}

// A status by an own account, which another own account reposted, is never
// promoted: the configured handles and the own posts stored both name it.
func TestOwnAccountsRepostIsNeverPromoted(t *testing.T) {
	f := newRelatedFixture(t)
	ctx := context.Background()
	const ownID = "1900000000000000710"
	status := &xsource.Quote{ID: ownID, AuthorHandle: "kano_2525", AuthorName: "Kano", Text: "own notice",
		PublishedAt: "2026-10-10T00:00:00Z", URL: "https://x.com/kano_2525/status/" + ownID}
	if err := f.s.WriteXRecent(ctx, "_SyntheticB", xsource.Result{Account: "_SyntheticB",
		Reposts: []xsource.Repost{{ID: ownID, Author: "kano_2525", Status: status}}}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SetXStatusDecision(ctx, ownID, xDecisionPromote, "own post", "@Kano_2525"); !errors.Is(err, ErrXRelatedInvalid) {
		t.Fatalf("promote by configured handle: %v", err)
	}
	own := quotingPost("1900000000000000711", "2026-10-11T00:00:00Z", nil)
	own.AccountHandle = "kano_2525"
	if err := f.s.WriteXRecent(ctx, "kano_2525", xsource.Result{Account: "kano_2525", Posts: []xsource.Post{own}}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SetXStatusDecision(ctx, ownID, xDecisionPromote, "own post"); !errors.Is(err, ErrXRelatedInvalid) {
		t.Fatalf("promote by stored own posts: %v", err)
	}
	if f.state(ownID) != "missing" {
		t.Fatalf("own status stored as a post: %s", f.state(ownID))
	}
}

// Another own account reposting a promoted post changes its public
// repostedBy, so the revision moves even when the listing carries no post.
func TestNewReposterOfPromotedPostChangesTheRevision(t *testing.T) {
	f := newRelatedFixture(t)
	ctx := context.Background()
	const otherID = "1900000000000000720"
	other := &xsource.Quote{ID: otherID, AuthorHandle: "composer_example", AuthorName: "Composer", Text: "new song",
		PublishedAt: "2026-10-10T00:00:00Z", URL: "https://x.com/composer_example/status/" + otherID}
	repost := []xsource.Repost{{ID: otherID, Author: "composer_example", Status: other}}
	if err := f.s.WriteXRecent(ctx, "kano_2525", xsource.Result{Account: "kano_2525", Reposts: repost}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SetXStatusDecision(ctx, otherID, xDecisionPromote, "collaborator"); err != nil {
		t.Fatal(err)
	}
	revision := f.text(`SELECT value FROM app_settings WHERE key='dashboard_revision'`)
	if err := f.s.WriteXRecent(ctx, "kano_2525", xsource.Result{Account: "kano_2525", Reposts: repost}, true); err != nil {
		t.Fatal(err)
	}
	if f.text(`SELECT value FROM app_settings WHERE key='dashboard_revision'`) != revision {
		t.Fatal("a repeated sighting changed the revision")
	}
	if err := f.s.WriteXRecent(ctx, "_SyntheticB", xsource.Result{Account: "_SyntheticB", Reposts: repost}, true); err != nil {
		t.Fatal(err)
	}
	if f.text(`SELECT value FROM app_settings WHERE key='dashboard_revision'`) == revision {
		t.Fatal("a new reposter must change the revision")
	}
}
