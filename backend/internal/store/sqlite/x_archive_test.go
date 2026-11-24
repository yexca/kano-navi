package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"testing"

	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
)

const (
	archiveListed    = "1900000000000000801"
	archiveSynced    = "1900000000000000802"
	archiveMisfiled  = "1900000000000000803"
	archiveReply     = "1900000000000000804"
	archiveThread    = "1900000000000000805"
	archiveForeign   = "1900000000000000806"
	archiveOperator  = "1900000000000000807"
	archiveNew       = "1900000000000000808"
	archiveUnitAcct  = "unit_synthetic"
	archiveOwnAcct   = "kano_2525"
	archiveCaptureAt = "2026-11-18T00:00:00Z"
)

func archiveStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStandalone(filepath.Join(t.TempDir(), "database", "kano.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func archiveExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

// seedImported stores a post as an earlier third-party import wrote it.
func seedImported(t *testing.T, db *sql.DB, id, account, text string) {
	t.Helper()
	archiveExec(t, db, `INSERT INTO posts(id,source,account_handle,type,text,published_at,url,raw_json)
		VALUES(?,'x',?,'daily',?,'2020-01-01T00:00:00Z',?,'{"import":{"origin":"https://third-party.example.invalid"}}')`,
		id, account, text, "https://x.com/"+account+"/status/"+id)
}

func archivePost(id, account, text string, media ...string) xsource.Post {
	kinds := make([]string, len(media))
	for i := range kinds {
		kinds[i] = xsource.MediaPhoto
	}
	return xsource.Post{ID: id, AccountHandle: account, Type: "daily", Label: xsource.LabelDaily, Text: text,
		PublishedAt: "2020-01-02T00:00:00Z", URL: "https://x.com/" + account + "/status/" + id, Likes: 3,
		MediaURLs: media, MediaKinds: kinds, Raw: map[string]any{"archive": map[string]any{"origin": xsource.ArchiveOrigin}}}
}

func postState(t *testing.T, db *sql.DB, id string) (account, text string, hidden bool, reconcile string) {
	t.Helper()
	if err := db.QueryRow(`SELECT account_handle, text, hidden_at IS NOT NULL, COALESCE(json_extract(raw_json,'$.import.reconcile'),'') FROM posts WHERE id=?`, id).
		Scan(&account, &text, &hidden, &reconcile); err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	return
}

// An export rewrites the earlier import's posts it lists, adds the missing
// ones and moves a status filed under the wrong account to its owner, while
// a synchronized post keeps its record. The earlier import's unlisted posts
// are hidden with their reason, except one an operator already hid.
func TestImportXArchivesRewritesListedAndHidesUnlistedImports(t *testing.T) {
	s := archiveStore(t)
	db := s.DB()
	ctx := context.Background()
	seedImported(t, db, archiveListed, archiveOwnAcct, "third-party wording")
	seedImported(t, db, archiveMisfiled, archiveOwnAcct, "the unit's status, filed as Kano's")
	seedImported(t, db, archiveReply, archiveOwnAcct, "@someone_synthetic thanks!")
	seedImported(t, db, archiveThread, archiveOwnAcct, "@kano_2525 continued")
	seedImported(t, db, archiveForeign, archiveOwnAcct, "a fan's post")
	seedImported(t, db, archiveOperator, archiveOwnAcct, "hidden by hand")
	archiveExec(t, db, `UPDATE posts SET hidden_at='2026-01-01T00:00:00.000Z' WHERE id=?`, archiveOperator)
	archiveExec(t, db, `INSERT INTO posts(id,source,account_handle,type,text,published_at,url,raw_json)
		VALUES(?,'x',?,'daily','synchronized wording','2026-01-01T00:00:00Z',?,'{"tweetID":"x"}')`, archiveSynced, archiveOwnAcct, "https://x.com/kano_2525/status/"+archiveSynced)

	imports := []XArchiveImport{
		{Account: archiveOwnAcct, Synchronized: true, HideUnlisted: true, Posts: []xsource.Post{
			archivePost(archiveListed, archiveOwnAcct, "export wording", twimg("listed")),
			archivePost(archiveSynced, archiveOwnAcct, "export wording of a synchronized post"),
			archivePost(archiveNew, archiveOwnAcct, "not imported before"),
		}},
		{Account: archiveUnitAcct, HideUnlisted: true, Posts: []xsource.Post{
			archivePost(archiveMisfiled, archiveUnitAcct, "the unit's own wording"),
		}},
	}

	preview, err := s.ImportXArchives(ctx, imports, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, text, hidden, _ := postState(t, db, archiveListed); text != "third-party wording" || hidden {
		t.Fatalf("dry run wrote: %q hidden=%v", text, hidden)
	}
	results, err := s.ImportXArchives(ctx, imports, false)
	if err != nil {
		t.Fatal(err)
	}
	if preview[0] != results[0] || preview[1] != results[1] {
		t.Fatalf("dry run counted %+v, the import did %+v", preview, results)
	}
	own, unit := results[0], results[1]
	if own.Added != 1 || own.Rewritten != 1 || own.KeptSynchronized != 1 || own.HiddenReply != 1 || own.HiddenUnlisted != 2 || own.AlreadyHidden != 1 || own.ArchiveAccountAdded {
		t.Fatalf("own account: %+v", own)
	}
	if unit.Rewritten != 1 || unit.Reassigned != 1 || !unit.ArchiveAccountAdded {
		t.Fatalf("unit account: %+v", unit)
	}

	if _, text, hidden, _ := postState(t, db, archiveListed); text != "export wording" || hidden {
		t.Fatalf("listed post: %q hidden=%v", text, hidden)
	}
	if got := linkedMedia(t, db, archiveListed, "post-image"); got != twimg("listed") {
		t.Fatalf("listed post media: %s", got)
	}
	if _, text, _, _ := postState(t, db, archiveSynced); text != "synchronized wording" {
		t.Fatalf("synchronized post rewritten: %q", text)
	}
	if account, text, hidden, _ := postState(t, db, archiveMisfiled); account != archiveUnitAcct || text != "the unit's own wording" || hidden {
		t.Fatalf("misfiled post: %s %q hidden=%v", account, text, hidden)
	}
	for id, reason := range map[string]string{archiveReply: XReconcileReasonReply, archiveThread: XReconcileReasonUnlisted, archiveForeign: XReconcileReasonUnlisted} {
		_, _, hidden, raw := postState(t, db, id)
		var record XReconcile
		if !hidden || json.Unmarshal([]byte(raw), &record) != nil || record.Reason != reason || record.Decision != XReconcileHidden || record.At == "" {
			t.Fatalf("%s: hidden=%v reconcile=%s", id, hidden, raw)
		}
	}
	if _, _, hidden, raw := postState(t, db, archiveOperator); !hidden || raw != "" {
		t.Fatalf("operator's hide: hidden=%v reconcile=%s", hidden, raw)
	}

	accounts, err := s.GetPostArchiveAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts.X) != 3 || accounts.X[2] != archiveUnitAcct {
		t.Fatalf("archive accounts: %v", accounts.X)
	}

	// A second run rewrites what the first wrote and hides nothing again.
	again, err := s.ImportXArchives(ctx, imports, false)
	if err != nil {
		t.Fatal(err)
	}
	if again[0].Rewritten != 2 || again[0].Added != 0 || again[0].HiddenReply+again[0].HiddenUnlisted != 0 || again[1].ArchiveAccountAdded {
		t.Fatalf("second run: %+v", again)
	}

	// An export that lists a post an earlier run hid shows it again.
	imports[0].Posts = append(imports[0].Posts, archivePost(archiveThread, archiveOwnAcct, "continued"))
	if _, err = s.ImportXArchives(ctx, imports, false); err != nil {
		t.Fatal(err)
	}
	if _, text, hidden, _ := postState(t, db, archiveThread); hidden || text != "continued" {
		t.Fatalf("listed later: %q hidden=%v", text, hidden)
	}
}

// A review shows again only the own posts and thread replies that the
// import alone hid; replies, other authors' statuses and gone ones stay
// hidden, and an operator's later decision is left alone.
func TestApplyXReconcileShowsOnlyWhatTheImportAloneHid(t *testing.T) {
	s := archiveStore(t)
	db := s.DB()
	ctx := context.Background()
	ids := []string{archiveListed, archiveThread, archiveReply, archiveForeign, archiveOperator}
	for _, id := range ids {
		seedImported(t, db, id, archiveOwnAcct, "wording")
	}
	if _, err := s.ImportXArchives(ctx, []XArchiveImport{{Account: archiveOwnAcct, Synchronized: true, HideUnlisted: true}}, false); err != nil {
		t.Fatal(err)
	}
	candidates, err := s.XReconcileCandidates(ctx)
	if err != nil || len(candidates) != len(ids) {
		t.Fatalf("candidates=%v err=%v", candidates, err)
	}
	// The operator hid this one again after the import, at another time.
	archiveExec(t, db, `UPDATE posts SET hidden_at='2026-12-01T00:00:00.000Z' WHERE id=?`, archiveOperator)

	review := func(id, outcome string) XReconcileOutcome {
		return XReconcileOutcome{ID: id, Account: archiveOwnAcct, Outcome: outcome, CheckedAt: archiveCaptureAt}
	}
	applied, err := s.ApplyXReconcile(ctx, []XReconcileOutcome{
		review(archiveListed, xsource.StatusTopLevel),
		review(archiveThread, xsource.StatusSelfReply),
		review(archiveReply, xsource.StatusReply),
		review(archiveForeign, xsource.StatusForeign),
		review(archiveOperator, xsource.StatusTopLevel),
		review(archiveNew, xsource.StatusTopLevel),
		{ID: archiveReply, Account: "someone_synthetic", Outcome: xsource.StatusTopLevel},
		review(archiveReply, "unexpected"),
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Shown != 2 || applied.Kept != 3 || applied.Unknown != 3 {
		t.Fatalf("applied %+v", applied)
	}
	for id, shown := range map[string]bool{archiveListed: true, archiveThread: true, archiveReply: false, archiveForeign: false, archiveOperator: false} {
		if _, _, hidden, raw := postState(t, db, id); hidden == shown || !json.Valid([]byte(raw)) {
			t.Fatalf("%s: hidden=%v reconcile=%s", id, hidden, raw)
		}
	}
	if left, _ := s.XReconcileCandidates(ctx); len(left) != 0 {
		t.Fatalf("reviewed posts still wait: %v", left)
	}
}

// An export's image is stored for its pending asset, and only once.
func TestSaveXArchiveImageStoresAPendingImageOnce(t *testing.T) {
	s := archiveStore(t)
	ctx := context.Background()
	if _, err := s.ImportXArchives(ctx, []XArchiveImport{{Account: archiveOwnAcct, Synchronized: true,
		Posts: []xsource.Post{archivePost(archiveNew, archiveOwnAcct, "with a photo", twimg("photo"))}}}, false); err != nil {
		t.Fatal(err)
	}
	// The export's post is the status known to exist for a review.
	if control, err := s.XArchiveControl(ctx); err != nil || control.ID != archiveNew || control.Account != archiveOwnAcct {
		t.Fatalf("control=%+v err=%v", control, err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	var content bytes.Buffer
	if err := png.Encode(&content, img); err != nil {
		t.Fatal(err)
	}
	for attempt, want := range []bool{true, false} {
		saved, err := s.SaveXArchiveImage(ctx, twimg("photo"), content.Bytes())
		if err != nil || saved != want {
			t.Fatalf("attempt %d: saved=%v err=%v", attempt, saved, err)
		}
	}
	if saved, err := s.SaveXArchiveImage(ctx, twimg("unlinked"), content.Bytes()); err != nil || saved {
		t.Fatalf("unlinked image: saved=%v err=%v", saved, err)
	}
	var status string
	if err := s.DB().QueryRow(`SELECT a.status FROM media_assets a JOIN media_links l ON l.media_id=a.id WHERE l.owner_id=?`, archiveNew).Scan(&status); err != nil || status != "ready" {
		t.Fatalf("status=%s err=%v", status, err)
	}
}
