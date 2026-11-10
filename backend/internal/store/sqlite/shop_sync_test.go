package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	shopsource "github.com/yexca/kano-navi/backend/internal/source/shop"
)

// shopSale is a synthetic period group as a read would hand it over.
func shopSale(title, opens, closes string, products ...shopsource.ProductRef) shopsource.Sale {
	key := strings.Join(strings.Fields(title), "")
	return shopsource.Sale{
		Key: key, Period: shopsource.Period{Opens: opens, Closes: closes, Line: "受注期間＞ synthetic"},
		Title: title, URL: "https://shop.milpr.com/collections/synthetic_goods", Collection: "synthetic_goods", Products: products,
	}
}

func shopProduct(id int64, published string) shopsource.ProductRef {
	return shopsource.ProductRef{ID: id, Handle: "synthetic-" + string(rune('a'+id)), Title: "サンプル加入記念グッズ item", PublishedAt: published}
}

func openShopStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStandalone(filepath.Join(t.TempDir(), "database", "kano.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

type shopRow struct {
	id, title, startsOn, startsAt, endsOn, endsAt, precision, url, platform, eventType, provenance, raw string
	detail, deleted                                                                                     sql.NullString
}

func readShopRow(t *testing.T, s *Store) shopRow {
	t.Helper()
	var r shopRow
	var startsAt, endsAt sql.NullString
	if err := s.DB().QueryRow(`SELECT id,title,starts_on,starts_at,ends_on,ends_at,time_precision,url,platform,event_type,provenance,raw_json,detail,deleted_at FROM events WHERE source='shop'`).
		Scan(&r.id, &r.title, &r.startsOn, &startsAt, &r.endsOn, &endsAt, &r.precision, &r.url, &r.platform, &r.eventType, &r.provenance, &r.raw, &r.detail, &r.deleted); err != nil {
		t.Fatal(err)
	}
	r.startsAt, r.endsAt = startsAt.String, endsAt.String
	return r
}

func revision(t *testing.T, s *Store) string {
	t.Helper()
	var value string
	if err := s.DB().QueryRow(`SELECT value FROM app_settings WHERE key='dashboard_revision'`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestShopReadWritesOneSaleAndKeepsItWhileTheShopMovesOn(t *testing.T) {
	s := openShopStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)
	sale := shopSale("サンプル 加入記念グッズ", "2026-10-24", "2026-11-24", shopProduct(1, "2026-10-24T10:00:06Z"), shopProduct(2, "2026-10-24T10:00:09Z"))
	changed, err := s.WriteShopRead(ctx, shopsource.Result{Sales: []shopsource.Sale{sale}, State: shopsource.State{LastAttemptAt: now, LastSuccessAt: now}})
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	row := readShopRow(t, s)
	if row.title != "サンプル 加入記念グッズ" || row.startsOn != "2026-10-24" || row.startsAt != "2026-10-24T10:00:00.000Z" || row.precision != "exact" ||
		row.endsOn != "2026-11-24" || row.endsAt != "" || row.eventType != "sale" || row.platform != "other" || row.provenance != "automatic" ||
		row.url != "https://shop.milpr.com/collections/synthetic_goods" || row.detail.Valid || !strings.HasPrefix(row.id, "shop-") {
		t.Fatalf("row=%+v", row)
	}
	if !strings.Contains(row.raw, `"collection":"synthetic_goods"`) || !strings.Contains(row.raw, `"id":2`) {
		t.Fatalf("raw=%s", row.raw)
	}
	// The shop states its platform as the announced array, not by default.
	var platforms string
	var inferred int
	if err = s.DB().QueryRow(`SELECT platforms,platforms_inferred FROM events WHERE id=?`, row.id).Scan(&platforms, &inferred); err != nil || platforms != `["other"]` || inferred != 0 {
		t.Fatalf("platforms=%q inferred=%d err=%v", platforms, inferred, err)
	}
	var key, period string
	if err = s.DB().QueryRow(`SELECT source_item_id,source_key FROM event_sources WHERE source='shop' AND event_id=?`, row.id).Scan(&key, &period); err != nil || key != "サンプル加入記念グッズ" || period != "2026-10-24/2026-11-24" {
		t.Fatalf("evidence=%q %q err=%v", key, period, err)
	}
	stored, err := s.ShopSales(ctx)
	if err != nil || len(stored) != 1 || stored[0].Key != key || stored[0].Period.Key() != period || len(stored[0].Products) != 2 || stored[0].Collection != "synthetic_goods" {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	state, err := s.ShopState(ctx)
	if err != nil || !state.LastSuccessAt.Equal(now) || state.LastError != "" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	// The same read again changes nothing and leaves the revision alone.
	before := revision(t, s)
	if changed, err = s.WriteShopRead(ctx, shopsource.Result{Sales: []shopsource.Sale{sale}, State: state}); err != nil || changed || revision(t, s) != before {
		t.Fatalf("again changed=%v err=%v", changed, err)
	}
	// A read whose window no longer lists the products retires nothing.
	if changed, err = s.WriteShopRead(ctx, shopsource.Result{State: state}); err != nil || changed {
		t.Fatalf("empty changed=%v err=%v", changed, err)
	}
	// A failed read stores its outcome and keeps the entry.
	failed := state
	failed.LastError, failed.BlockedUntil = "shop_rate_limited", now.Add(time.Minute)
	if changed, err = s.WriteShopRead(ctx, shopsource.Result{State: failed}); err != nil || changed {
		t.Fatalf("failed changed=%v err=%v", changed, err)
	}
	if kept := readShopRow(t, s); kept.deleted.Valid || kept.title != row.title {
		t.Fatalf("kept=%+v", kept)
	}
	if state, err = s.ShopState(ctx); err != nil || state.LastError != "shop_rate_limited" || !state.BlockedUntil.Equal(now.Add(time.Minute)) || !state.LastSuccessAt.Equal(now) {
		t.Fatalf("failed state=%+v err=%v", state, err)
	}
	status, err := s.ShopStatus(ctx)
	if err != nil || status.Sales != 1 || status.BlockedUntil == nil || status.LastError == nil || *status.LastError != "shop_rate_limited" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	// A product published before the opening date leaves the time unknown.
	early := shopSale("サンプル 加入記念グッズ", "2026-10-24", "2026-11-24", shopProduct(1, "2026-10-20T10:00:00Z"))
	if changed, err = s.WriteShopRead(ctx, shopsource.Result{Sales: []shopsource.Sale{early}, State: state}); err != nil || !changed {
		t.Fatalf("early changed=%v err=%v", changed, err)
	}
	if got := readShopRow(t, s); got.startsAt != "" || got.precision != "unknown" || got.id != row.id {
		t.Fatalf("early=%+v", got)
	}
}

func TestShopReadLeavesLockedAndDeletedEntriesAsTheOperatorLeftThem(t *testing.T) {
	s := openShopStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)
	state := shopsource.State{LastAttemptAt: now, LastSuccessAt: now}
	sale := shopSale("サンプル 加入記念グッズ", "2026-10-24", "2026-11-24", shopProduct(1, "2026-10-24T10:00:06Z"))
	if _, err := s.WriteShopRead(ctx, shopsource.Result{Sales: []shopsource.Sale{sale}, State: state}); err != nil {
		t.Fatal(err)
	}
	id := readShopRow(t, s).id
	// An operator's edit locks the entry, close time included.
	if _, err := s.DB().Exec(`UPDATE events SET title='Operator title',ends_at='2026-11-24T14:59:00.000Z',provenance='manual',manual_locked=1 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	moved := shopSale("サンプル 加入記念グッズ 改", "2026-10-24", "2026-11-24", shopProduct(1, "2026-10-24T10:00:06Z"), shopProduct(2, "2026-10-24T10:00:07Z"))
	moved.Key = sale.Key
	if changed, err := s.WriteShopRead(ctx, shopsource.Result{Sales: []shopsource.Sale{moved}, State: state}); err != nil || changed {
		t.Fatalf("locked changed=%v err=%v", changed, err)
	}
	if row := readShopRow(t, s); row.title != "Operator title" || row.endsAt != "2026-11-24T14:59:00.000Z" || row.provenance != "manual" {
		t.Fatalf("locked=%+v", row)
	}
	// The evidence still follows the shop, so the group keeps its products.
	if stored, err := s.ShopSales(ctx); err != nil || len(stored) != 1 || len(stored[0].Products) != 2 {
		t.Fatalf("evidence=%+v err=%v", stored, err)
	}
	// A deletion tombstone is never resurrected.
	if _, err := s.DB().Exec(`UPDATE events SET deleted_at='2026-11-08T03:00:00.000Z' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.WriteShopRead(ctx, shopsource.Result{Sales: []shopsource.Sale{sale}, State: state}); err != nil || changed {
		t.Fatalf("deleted changed=%v err=%v", changed, err)
	}
	var count int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM events WHERE source='shop' AND deleted_at IS NULL`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("visible=%d err=%v", count, err)
	}
}

func TestAPostsSaleMergesIntoTheShopsPeriod(t *testing.T) {
	f := newMergeFixture(t)
	sale := shopSale("サンプル 加入記念グッズ", "2026-10-24", "2026-11-24", shopProduct(1, "2026-10-24T10:00:06Z"))
	if _, err := f.s.WriteShopRead(context.Background(), shopsource.Result{Sales: []shopsource.Sale{sale}}); err != nil {
		f.t.Fatal(err)
	}
	var shopID string
	if err := f.s.DB().QueryRow(`SELECT id FROM events WHERE source='shop'`).Scan(&shopID); err != nil {
		f.t.Fatal(err)
	}
	// A post announces the same period by date only and links itself; one
	// linking the collection is the same sale by its link. Another shop's
	// sale in the same period stays.
	f.add("x-post", "x", "サンプル加入記念グッズ 受注開始", "2026-10-24", "", "sale", "ends_on='2026-11-24'")
	f.add("x-link", "x", "受注は本日まで", "2026-11-24", "", "sale", "url='https://shop.milpr.com/collections/synthetic_goods/'", "ends_on='2026-11-24'")
	f.add("x-other", "x", "別ブランドのグッズ", "2026-10-24", "", "sale", "url='https://other.example.test/goods'", "ends_on='2026-11-24'")
	f.reconcile()
	f.expect(map[string]string{
		"x-post":  shopID + "|rule|rule_c:sale_period",
		"x-link":  shopID + "|rule|rule_c:sale_period",
		"x-other": "||",
		shopID:    "||",
	})
}
