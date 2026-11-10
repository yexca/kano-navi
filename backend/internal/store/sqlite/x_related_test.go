package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
)

const (
	relatedMatch = "1900000000000000301"
	relatedOther = "1900000000000000302"
)

// relatedStatus is a related account's timeline status as the adapter maps it.
func relatedStatus(id, text string, media ...string) xsource.Post {
	images := []any{}
	for _, u := range media {
		images = append(images, u)
	}
	raw := map[string]any{"id": id, "text": text, "created_at": "2026-10-10T00:00:00Z", "url": "https://x.com/agency_synthetic/status/" + id,
		"author": map[string]any{"screen_name": "agency_synthetic", "name": "Agency"}, "mediaURLs": images}
	return xsource.Post{ID: id, AccountHandle: "agency_synthetic", Text: text, PublishedAt: "2026-10-10T00:00:00Z",
		URL: "https://x.com/agency_synthetic/status/" + id, MediaURLs: media, AuthorScreenName: "agency_synthetic", Raw: raw}
}

type relatedFixture struct {
	t *testing.T
	s *Store
}

func newRelatedFixture(t *testing.T) *relatedFixture {
	t.Helper()
	s, err := OpenStandalone(filepath.Join(t.TempDir(), "database", "kano.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	f := &relatedFixture{t: t, s: s}
	f.rules(DefaultXRelatedKeywords...)
	return f
}

func (f *relatedFixture) rules(keywords ...string) {
	f.t.Helper()
	if _, err := f.s.SetXRelatedAccounts(context.Background(), []XRelatedAccount{{Handle: "@agency_synthetic", Keywords: keywords}}, []string{"kano_2525"}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *relatedFixture) text(q string, args ...any) string {
	f.t.Helper()
	var out string
	if err := f.s.DB().QueryRow(q, args...).Scan(&out); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
	return out
}

// state is the post's visibility: missing, shown or hidden.
func (f *relatedFixture) state(id string) string {
	return f.text(`SELECT COALESCE((SELECT CASE WHEN hidden_at IS NULL THEN 'shown' ELSE 'hidden' END FROM posts WHERE id=?),'missing')`, id)
}

func (f *relatedFixture) wall() map[string]bool {
	f.t.Helper()
	d, err := f.s.GetDashboard(context.Background(), 30, time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC))
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[string]bool{}
	for _, p := range d.Posts {
		out[p.ID] = p.Promoted
	}
	if d.Summary.LatestPost != nil && d.Summary.LatestPost.Promoted {
		f.t.Fatalf("a promoted post became the latest post")
	}
	return out
}

func TestRelatedAccountsRejectOwnHandlesAndRepeats(t *testing.T) {
	f := newRelatedFixture(t)
	for _, accounts := range [][]XRelatedAccount{
		{{Handle: "Kano_2525"}},
		{{Handle: "a"}, {Handle: "@A"}},
		{{Handle: "not a handle"}},
	} {
		if _, err := f.s.SetXRelatedAccounts(context.Background(), accounts, []string{"kano_2525"}); !errors.Is(err, ErrXRelatedInvalid) {
			t.Fatalf("%v: err=%v", accounts, err)
		}
	}
	accounts, err := f.s.XRelatedAccounts(context.Background())
	if err != nil || len(accounts) != 1 || accounts[0].Handle != "agency_synthetic" || len(accounts[0].Keywords) != 3 {
		t.Fatalf("accounts=%+v err=%v", accounts, err)
	}
}

func TestRelatedStatusesPromoteByKeywordAndStayOffOwnSlots(t *testing.T) {
	f := newRelatedFixture(t)
	ctx := context.Background()
	result := xsource.Result{Account: "agency_synthetic", Posts: []xsource.Post{
		relatedStatus(relatedMatch, "#鹿乃まほろ 出演決定", twimg("notice")),
		relatedStatus(relatedOther, "another talent's news", twimg("other")),
	}, NextCursor: relatedMatch, NextCursorTime: "2026-10-10T00:00:00Z"}
	written, err := f.s.WriteXRelated(ctx, "agency_synthetic", result, true)
	if err != nil || written.Listed != 2 || written.Promoted != 1 {
		t.Fatalf("written=%+v err=%v", written, err)
	}
	if f.state(relatedMatch) != "shown" || f.state(relatedOther) != "missing" {
		t.Fatalf("states=%s,%s", f.state(relatedMatch), f.state(relatedOther))
	}
	// The promoted post carries its own image link and no schedule candidate;
	// a pooled status has no media at all.
	if got := f.text(`SELECT account_handle||'|'||(SELECT COUNT(*) FROM media_links WHERE owner_id=posts.id AND role='post-image')||'|'||(SELECT COUNT(*) FROM media_links WHERE owner_id=?)||'|'||(SELECT COUNT(*) FROM assets) FROM posts WHERE id=?`, relatedOther, relatedMatch); got != "agency_synthetic|1|0|0" {
		t.Fatalf("promoted post=%s", got)
	}
	if got := f.text(`SELECT cursor_id FROM sync_state WHERE source=? AND account_id='agency_synthetic'`, XRelatedSource); got != relatedMatch {
		t.Fatalf("cursor=%s", got)
	}
	// An own post keeps the latest-post slot; the promoted one is on the wall.
	own := quotingPost("1900000000000000310", "2026-10-11T00:00:00Z", nil)
	own.Text = "own"
	if err = f.s.WriteXRecent(ctx, "kano_2525", xsource.Result{Account: "kano_2525", Posts: []xsource.Post{own}}, true); err != nil {
		t.Fatal(err)
	}
	if wall := f.wall(); !wall[relatedMatch] || wall[own.ID] {
		t.Fatalf("wall=%v", wall)
	}

	// Quoted by a shown own post, the promoted post leaves the wall: the quote
	// card carries it. Hiding the quote brings it back.
	quoting := quotingPost("1900000000000000311", "2026-10-11T01:00:00Z", &xsource.Quote{ID: relatedMatch, AuthorHandle: "agency_synthetic", Text: "#鹿乃まほろ 出演決定", URL: "https://x.com/agency_synthetic/status/" + relatedMatch})
	if err = f.s.WriteXRecent(ctx, "kano_2525", xsource.Result{Account: "kano_2525", Posts: []xsource.Post{quoting}}, true); err != nil {
		t.Fatal(err)
	}
	if wall := f.wall(); wall[relatedMatch] || !wallHas(wall, quoting.ID) {
		t.Fatalf("quoted promoted post stayed on the wall: %v", wall)
	}
	if _, err = f.s.DB().Exec(`UPDATE posts SET quote_hidden_at='2026-10-11T02:00:00.000Z' WHERE id=?`, quoting.ID); err != nil {
		t.Fatal(err)
	}
	if wall := f.wall(); !wall[relatedMatch] {
		t.Fatalf("hidden quote kept the promoted post off the wall: %v", wall)
	}
	page, err := f.s.ListAdminXStatuses(ctx, AdminXStatusFilter{Reason: "quoted"})
	if err != nil || page.Total != 1 || page.Items[0].ID != relatedMatch || page.Items[0].State != "promoted" ||
		len(page.Items[0].Reasons) != 2 || page.Items[0].Reasons[0] != "keyword" || len(page.Items[0].QuotedBy) != 1 {
		t.Fatalf("quoted=%+v err=%v", page, err)
	}
}

func TestExternalListLeavesOutOwnAccounts(t *testing.T) {
	f := newRelatedFixture(t)
	ctx := context.Background()
	if _, err := f.s.WriteXRelated(ctx, "agency_synthetic", xsource.Result{Posts: []xsource.Post{
		relatedStatus(relatedMatch, "鹿乃まほろ 出演決定"),
	}}, true); err != nil {
		t.Fatal(err)
	}
	// One own post quotes another stored own post, a second quotes a status
	// of the other own account that Posts does not hold.
	own := quotingPost("1900000000000000320", "2026-10-11T00:00:00Z", nil)
	self := quotingPost("1900000000000000321", "2026-10-11T01:00:00Z", &xsource.Quote{ID: own.ID, AuthorHandle: "kano", Text: own.Text, URL: own.URL})
	sub := quotingPost("1900000000000000322", "2026-10-11T02:00:00Z", &xsource.Quote{ID: "1900000000000000323", AuthorHandle: "Kano_Sub", Text: "sub", URL: "https://x.com/kano_sub/status/1900000000000000323"})
	if err := f.s.WriteXRecent(ctx, "kano", xsource.Result{Account: "kano", Posts: []xsource.Post{own, self, sub}}, true); err != nil {
		t.Fatal(err)
	}
	all, err := f.s.ListAdminXStatuses(ctx, AdminXStatusFilter{})
	if err != nil || all.Total != 3 {
		t.Fatalf("pool=%+v err=%v", all, err)
	}
	page, err := f.s.ListAdminXStatuses(ctx, AdminXStatusFilter{ExcludeOwn: true, Own: []string{"kano", "@kano_sub"}})
	if err != nil || page.Total != 1 || page.Items[0].ID != relatedMatch {
		t.Fatalf("external=%+v err=%v", page, err)
	}
}

func wallHas(wall map[string]bool, id string) bool {
	_, ok := wall[id]
	return ok
}

func TestRuleChangesAndOperatorDecisionsPromoteAndDemote(t *testing.T) {
	f := newRelatedFixture(t)
	ctx := context.Background()
	if _, err := f.s.WriteXRelated(ctx, "agency_synthetic", xsource.Result{Posts: []xsource.Post{
		relatedStatus(relatedMatch, "鹿乃まほろ 出演決定"), relatedStatus(relatedOther, "another talent's news"),
	}}, true); err != nil {
		t.Fatal(err)
	}
	// Rules that no longer match demote by hiding; matching again shows it.
	f.rules("存在しない語")
	if f.state(relatedMatch) != "hidden" {
		t.Fatalf("not demoted: %s", f.state(relatedMatch))
	}
	f.rules(DefaultXRelatedKeywords...)
	if f.state(relatedMatch) != "shown" {
		t.Fatalf("not shown again: %s", f.state(relatedMatch))
	}
	// An operator's hide in Posts outlives the rules.
	if _, err := f.s.DB().Exec(`UPDATE posts SET hidden_at='2026-10-12T00:00:00.000Z' WHERE id=?`, relatedMatch); err != nil {
		t.Fatal(err)
	}
	f.rules("存在しない語")
	f.rules(DefaultXRelatedKeywords...)
	if f.state(relatedMatch) != "hidden" {
		t.Fatalf("rules undid the operator's hide")
	}
	// A manual promote is a lock that shows the post and outlives the rules.
	if _, err := f.s.SetXStatusDecision(ctx, relatedMatch, "promote", ""); !errors.Is(err, ErrXRelatedInvalid) {
		t.Fatalf("promote without a reason: %v", err)
	}
	status, err := f.s.SetXStatusDecision(ctx, relatedOther, "promote", "Kano appears in the photo")
	if err != nil || status.State != "promoted" || status.Decision == nil || *status.Decision != "promote" || status.Reasons[0] != "manual" {
		t.Fatalf("promote=%+v err=%v", status, err)
	}
	f.rules("存在しない語")
	if f.state(relatedOther) != "shown" {
		t.Fatalf("rules demoted a manual promote")
	}
	// A manual demote hides it and outlives matching rules.
	if _, err = f.s.SetXStatusDecision(ctx, relatedOther, "demote", "not about Kano"); err != nil {
		t.Fatal(err)
	}
	f.rules("talent")
	if f.state(relatedOther) != "hidden" {
		t.Fatalf("rules promoted a manual demote")
	}
	// Back to the rules, which match.
	if status, err = f.s.SetXStatusDecision(ctx, relatedOther, "automatic", ""); err != nil || status.Decision != nil || f.state(relatedOther) != "shown" {
		t.Fatalf("automatic=%+v state=%s err=%v", status, f.state(relatedOther), err)
	}
	// Only a related account's status can be promoted.
	if _, err = f.s.SetXStatusDecision(ctx, "1900000000000000999", "promote", "x"); !errors.Is(err, ErrXStatusNotFound) {
		t.Fatalf("missing status: %v", err)
	}
	if _, err = f.s.DB().Exec(`INSERT INTO x_statuses(id,author_handle,text,url,quoted_at,first_seen_at,updated_at) VALUES('1900000000000000320','someone_else','quoted','https://x.com/someone_else/status/1900000000000000320','2026-10-10T00:00:00Z','2026-10-10T00:00:00Z','2026-10-10T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.SetXStatusDecision(ctx, "1900000000000000320", "promote", "x"); !errors.Is(err, ErrXRelatedInvalid) {
		t.Fatalf("promoted another author's status: %v", err)
	}
}

func TestOwnRepostsPoolARelatedStatus(t *testing.T) {
	f := newRelatedFixture(t)
	ctx := context.Background()
	reposted := &xsource.Quote{ID: "1900000000000000330", AuthorHandle: "agency_synthetic", AuthorName: "Agency", Text: "グッズ発売", URL: "https://x.com/agency_synthetic/status/1900000000000000330"}
	keyword := &xsource.Quote{ID: "1900000000000000331", AuthorHandle: "agency_synthetic", Text: "鹿乃 出演", PublishedAt: "2026-10-10T00:00:00Z", URL: "https://x.com/agency_synthetic/status/1900000000000000331"}
	other := &xsource.Quote{ID: "1900000000000000332", AuthorHandle: "someone_else", Text: "x", URL: "https://x.com/someone_else/status/1900000000000000332"}
	if err := f.s.WriteXRecent(ctx, "kano_2525", xsource.Result{Account: "kano_2525", Reposts: []xsource.Repost{
		{ID: reposted.ID, Author: "agency_synthetic", Status: reposted},
		{ID: keyword.ID, Author: "agency_synthetic", Status: keyword},
		{ID: other.ID, Author: "someone_else", Status: other},
	}}, true); err != nil {
		t.Fatal(err)
	}
	if got := f.text(`SELECT group_concat(id||':'||(reposted_at IS NOT NULL),',') FROM (SELECT * FROM x_statuses ORDER BY id)`); got != reposted.ID+":1,"+keyword.ID+":1" {
		t.Fatalf("pool=%s", got)
	}
	if f.state(reposted.ID) != "missing" || f.state(keyword.ID) != "shown" {
		t.Fatalf("states=%s,%s", f.state(reposted.ID), f.state(keyword.ID))
	}
	candidates, err := f.s.ListTranslationCandidates(ctx, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), 10)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, c := range candidates {
		ids[c.OwnerID] = c.OwnerType
	}
	if ids[reposted.ID] != TranslationOwnerXStatus || ids[keyword.ID] != TranslationOwnerPost {
		t.Fatalf("candidates=%v", ids)
	}
}

func TestExternalListLinksEachStatusOnThePage(t *testing.T) {
	f := newRelatedFixture(t)
	ctx := context.Background()
	if _, err := f.s.WriteXRelated(ctx, "agency_synthetic", xsource.Result{Posts: []xsource.Post{
		relatedStatus(relatedMatch, "鹿乃まほろ 出演決定"),
		relatedStatus(relatedOther, "another talent's news"),
	}}, true); err != nil {
		t.Fatal(err)
	}
	quote := func(id string) *xsource.Quote {
		return &xsource.Quote{ID: id, AuthorHandle: "agency_synthetic", Text: "quoted", URL: "https://x.com/agency_synthetic/status/" + id}
	}
	// Six own posts quote one status and one quotes the other; the list keeps
	// the newest five of each.
	own := []xsource.Post{}
	for i := 1; i <= 6; i++ {
		own = append(own, quotingPost(fmt.Sprintf("190000000000000033%d", i), fmt.Sprintf("2026-10-11T0%d:00:00Z", i), quote(relatedMatch)))
	}
	own = append(own, quotingPost("1900000000000000340", "2026-10-11T07:00:00Z", quote(relatedOther)))
	if err := f.s.WriteXRecent(ctx, "kano", xsource.Result{Account: "kano", Posts: own}, true); err != nil {
		t.Fatal(err)
	}
	for _, repost := range [][2]string{{relatedMatch, "kano_b"}, {relatedMatch, "kano_a"}, {relatedOther, "kano_c"}} {
		if _, err := f.s.DB().Exec(`INSERT INTO x_reposts (status_id, account_handle, first_seen_at) VALUES (?, ?, '2026-10-11T08:00:00.000Z')`, repost[0], repost[1]); err != nil {
			t.Fatal(err)
		}
	}
	page, err := f.s.ListAdminXStatuses(ctx, AdminXStatusFilter{IDs: []string{relatedMatch, relatedOther}})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	got := map[string]string{}
	for _, item := range page.Items {
		got[item.ID] = strings.Join(item.QuotedBy, ",") + "|" + strings.Join(item.RepostedBy, ",")
	}
	want := map[string]string{
		relatedMatch: "1900000000000000336,1900000000000000335,1900000000000000334,1900000000000000333,1900000000000000332|kano_a,kano_b",
		relatedOther: "1900000000000000340|kano_c",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("links:\n got  %v\n want %v", got, want)
	}
}

// An account that joins X_HANDLES leaves the related list at the next start,
// and the posts the pool promoted for it become own posts: a post the rules
// hid is shown again, an operator's demote stays hidden.
func TestSettleXRelatedRolesMakesAnOwnAccountOwn(t *testing.T) {
	f := newRelatedFixture(t)
	ctx := context.Background()
	if _, err := f.s.WriteXRelated(ctx, "agency_synthetic", xsource.Result{Posts: []xsource.Post{
		relatedStatus(relatedMatch, "#鹿乃まほろ 出演決定"),
		relatedStatus(relatedOther, "鹿乃まほろ 告知"),
	}}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SetXStatusDecision(ctx, relatedOther, "demote", "synthetic reason"); err != nil {
		t.Fatal(err)
	}
	f.rules("no match")
	if f.state(relatedMatch) != "hidden" || f.state(relatedOther) != "hidden" {
		t.Fatalf("before=%s,%s", f.state(relatedMatch), f.state(relatedOther))
	}
	before := f.text(`SELECT COALESCE((SELECT value FROM app_settings WHERE key='dashboard_revision'),'0')`)
	if err := f.s.SettleXRelatedRoles(ctx, []string{"kano_2525", "@Agency_Synthetic"}); err != nil {
		t.Fatal(err)
	}
	if accounts, err := f.s.XRelatedAccounts(ctx); err != nil || len(accounts) != 0 {
		t.Fatalf("accounts=%+v err=%v", accounts, err)
	}
	if f.state(relatedMatch) != "shown" || f.state(relatedOther) != "hidden" {
		t.Fatalf("after=%s,%s", f.state(relatedMatch), f.state(relatedOther))
	}
	if got := f.text(`SELECT COUNT(*) FROM posts WHERE promoted_at IS NOT NULL`); got != "0" {
		t.Fatalf("promoted=%s", got)
	}
	if got := f.text(`SELECT COUNT(*) FROM x_statuses WHERE demoted_at IS NOT NULL OR manual_decision IS NOT NULL`); got != "0" {
		t.Fatalf("pool marks=%s", got)
	}
	if f.text(`SELECT COALESCE((SELECT value FROM app_settings WHERE key='dashboard_revision'),'0')`) == before {
		t.Fatal("revision unchanged")
	}
	// A second start changes nothing.
	again := f.text(`SELECT COALESCE((SELECT value FROM app_settings WHERE key='dashboard_revision'),'0')`)
	if err := f.s.SettleXRelatedRoles(ctx, []string{"agency_synthetic"}); err != nil || f.text(`SELECT COALESCE((SELECT value FROM app_settings WHERE key='dashboard_revision'),'0')`) != again {
		t.Fatalf("second settle err=%v", err)
	}
}
