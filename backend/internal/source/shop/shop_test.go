package shop

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type fakeTransport func(*http.Request) (*http.Response, error)

func (f fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func answer(status int, body string, header ...string) *http.Response {
	h := http.Header{}
	for i := 0; i+1 < len(header); i += 2 {
		h.Set(header[i], header[i+1])
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}

// product is a synthetic storefront product with a price and a variant the
// source must not keep.
func product(id int64, handle, title, published, body string, tags ...string) map[string]any {
	return map[string]any{
		"id": id, "handle": handle, "title": title, "tags": tags, "published_at": published, "body_html": body,
		"vendor": "synthetic", "variants": []map[string]any{{"id": id * 10, "price": "1650", "available": false}},
	}
}

func products(items ...map[string]any) string {
	if items == nil {
		items = []map[string]any{}
	}
	encoded, _ := json.Marshal(map[string]any{"products": items})
	return string(encoded)
}

const (
	period    = "<p>商品説明</p><p>＜受注期間＞<br>2026年10月24日 〜 2026年11月24日</p><p>＜発送時期＞<br>〜2027年5月下旬頃</p>"
	permanent = "<p>販売期間 2026年10月1日～</p><p>＜発送時期＞<br>〜2027年5月下旬頃</p>"
)

func TestParsePeriodReadsTheShopsForms(t *testing.T) {
	for _, tc := range []struct {
		name, body, opens, closes, reason string
	}{
		{"order window over a line break", period, "2026-10-24", "2026-11-24", ""},
		{"sale window with a wave dash", "<p>【販売期間】2026年9月20日～2026年10月20日</p>", "2026-09-20", "2026-10-20", ""},
		{"lottery with weekday and time, ASCII tilde", "抽選受付期間：2026年10月3日(土) 19:00 ~ 2026年11月3日(火)", "2026-10-03", "2026-11-03", ""},
		{"application window in full-width digits", "受付期間 ２０２６年１２月２０日 〜 ２０２７年１月５日", "2026-12-20", "2027-01-05", ""},
		{"close without a year runs into the next one", "＜受注期間＞2026年12月20日 〜 1月5日", "2026-12-20", "2027-01-05", ""},
		{"close without a year on the next year's leap day", "＜受注期間＞2027年12月1日 〜 2月29日", "2027-12-01", "2028-02-29", ""},
		{"no-break spaces around the wave dash and inside a date", "<p>受注期間 2026年10月24日&nbsp;〜&nbsp;2026年&nbsp;11月24日</p>", "2026-10-24", "2026-11-24", ""},
		{"permanent item", permanent, "", "", SkipOpenEnded},
		{"no period", "<p>常設商品です</p>", "", "", SkipNoPeriod},
		{"impossible date", "＜受注期間＞2026年2月30日 〜 2026年3月5日", "", "", SkipInvalid},
		{"close before opening", "＜受注期間＞2026年3月5日 〜 2026年3月1日", "", "", SkipInvalid},
	} {
		got, reason := ParsePeriod(tc.body)
		if reason != tc.reason || got.Opens != tc.opens || got.Closes != tc.closes {
			t.Errorf("%s: %+v %q", tc.name, got, reason)
		}
		if reason == "" && (got.Line == "" || strings.ContainsAny(got.Line, "<>\n")) {
			t.Errorf("%s: line %q", tc.name, got.Line)
		}
	}
}

func TestCandidatesKeepOnlyTaggedDatedProducts(t *testing.T) {
	listed, err := ParseProducts([]byte(products(
		product(1, "goods-a", "サンプルコレクション 2026 アクリルスタンド", "2026-10-24T19:00:06+09:00", period, "synthetic-talent", Tag),
		product(2, "goods-b", "サンプルコレクション 2026 缶バッジ", "2026-10-24T19:00:07+09:00", period, "synthetic-talent"),
		product(3, "goods-c", "サンプル常設グッズ", "2026-10-01T12:00:00+09:00", permanent, Tag),
		product(4, "bad/handle", "broken", "2026-10-01T12:00:00+09:00", period, Tag),
		product(5, "goods-e", "説明のないグッズ", "2026-10-01T12:00:00+09:00", "", Tag),
	)))
	if err != nil || len(listed) != 4 {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
	candidates, skipped := Candidates(listed)
	if len(candidates) != 1 || candidates[0].ID != 1 || candidates[0].Period.Key() != "2026-10-24/2026-11-24" {
		t.Fatalf("candidates=%+v", candidates)
	}
	if skipped[SkipUntagged] != 1 || skipped[SkipOpenEnded] != 1 || skipped[SkipNoPeriod] != 1 {
		t.Fatalf("skipped=%v", skipped)
	}
}

func TestClientRequestsFixedPathsWithAnHonestAgent(t *testing.T) {
	seen := []string{}
	client := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("User-Agent") != UserAgent || r.Method != http.MethodGet {
			t.Fatalf("request %s %v", r.Method, r.Header)
		}
		seen = append(seen, r.URL.String())
		if r.URL.Path == "/collections.json" {
			return answer(200, `{"collections":[{"handle":"sample_2026","title":"サンプル コレクション 2026"},{"handle":"bad/handle","title":"x"}]}`), nil
		}
		return answer(200, products()), nil
	})})
	ctx := context.Background()
	if _, err := client.NewArrivals(ctx, ArrivalsLimit); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.CatalogPage(ctx, 2); err != nil {
		t.Fatal(err)
	}
	collections, err := client.Collections(ctx)
	if err != nil || len(collections) != 1 || collections[0].Handle != "sample_2026" {
		t.Fatalf("collections=%+v err=%v", collections, err)
	}
	if _, _, err := client.CatalogPage(ctx, MaxCatalogPages+1); ErrorCode(err) != "shop_invalid_request" {
		t.Fatalf("page beyond the bound: %v", err)
	}
	want := []string{
		"https://shop.milpr.com/collections/new-arrivals/products.json?limit=50",
		"https://shop.milpr.com/products.json?limit=250&page=2",
		"https://shop.milpr.com/collections.json?limit=250",
	}
	if fmt.Sprint(seen) != fmt.Sprint(want) {
		t.Fatalf("requests=%v", seen)
	}
}

func TestClientMapsAnswersToControlledCodes(t *testing.T) {
	now := time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		response *http.Response
		code     string
		wait     time.Duration
	}{
		{answer(302, "", "Location", "https://elsewhere.example.test/"), "shop_redirect_rejected", 0},
		{answer(429, "local_rate_limited", "Retry-After", "120"), "shop_rate_limited", 2 * time.Minute},
		{answer(429, "", "Retry-After", now.Add(5*time.Minute).Format(http.TimeFormat)), "shop_rate_limited", 5 * time.Minute},
		{answer(429, ""), "shop_rate_limited", time.Minute},
		{answer(429, "", "Retry-After", "999999"), "shop_rate_limited", 24 * time.Hour},
		{answer(503, ""), "shop_http_503", 0},
		{answer(404, ""), "shop_http_404", 0},
		{answer(200, "<!DOCTYPE html>"), "shop_invalid_json", 0},
		{answer(200, `{"items":[]}`), "shop_invalid_json", 0},
		{answer(200, strings.Repeat(" ", MaxResponseBytes+1)), "shop_response_oversized", 0},
	} {
		client := NewClient(&http.Client{Transport: fakeTransport(func(*http.Request) (*http.Response, error) { return tc.response, nil })})
		client.now = func() time.Time { return now }
		_, err := client.NewArrivals(context.Background(), ArrivalsLimit)
		var shopErr *Error
		if ErrorCode(err) != tc.code || tc.wait > 0 && (!errorAs(err, &shopErr) || shopErr.RetryAfter != tc.wait) {
			t.Errorf("%d %s: err=%#v, want %s after %s", tc.response.StatusCode, tc.response.Header, err, tc.code, tc.wait)
		}
	}
}

func errorAs(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}

func candidate(id int64, handle, title, published, body string) Candidate {
	listed, _ := ParseProducts([]byte(products(product(id, handle, title, published, body, Tag))))
	found, _ := Candidates(listed)
	return found[0]
}

func TestGroupFollowsCollectionsThenTitlePrefixes(t *testing.T) {
	other := "＜受注期間＞2026年9月20日 〜 2026年10月20日"
	candidates := []Candidate{
		candidate(11, "join-acrylic", "サンプル加入記念グッズ アクリルスタンド", "2026-10-24T19:00:08+09:00", period),
		candidate(12, "join-badge", "サンプル加入記念グッズ 缶バッジ", "2026-10-24T19:00:06+09:00", period),
		// No collection: the products share their title before the item,
		// which may be more than one word.
		candidate(21, "season-board", "サンプルシーズン 2026 アクリルボード", "2026-10-24T19:00:12+09:00", period),
		candidate(22, "season-badge", "サンプルシーズン 2026 ランダム缶バッジ 全8種", "2026-10-24T19:00:09+09:00", period),
		// A product published before its opening date has no opening time.
		candidate(31, "voice-2026", "サンプルボイスコレクション", "2026-09-17T17:12:40+09:00", other),
	}
	collections := []Collection{{Handle: "join_goods", Title: "サンプル 加入記念グッズ"}, {Handle: "all", Title: "全て"}}
	sales := Group(candidates, nil, collections)
	if len(sales) != 3 {
		t.Fatalf("sales=%+v", sales)
	}
	byTitle := map[string]Sale{}
	for _, s := range sales {
		byTitle[s.Title] = s
	}
	join := byTitle["サンプル 加入記念グッズ"]
	if join.Key != "サンプル加入記念グッズ" || join.URL != "https://shop.milpr.com/collections/join_goods" || join.Collection != "join_goods" || len(join.Products) != 2 || join.Products[0].ID != 12 {
		t.Fatalf("join=%+v", join)
	}
	if at, ok := join.StartsAt(); !ok || at.Format(time.RFC3339) != "2026-10-24T10:00:00Z" {
		t.Fatalf("join opens %v %v", at, ok)
	}
	season := byTitle["サンプルシーズン 2026"]
	if season.Key != "サンプルシーズン2026" || season.URL != "https://shop.milpr.com/products/season-badge" || season.Collection != "" || len(season.Products) != 2 {
		t.Fatalf("season=%+v", season)
	}
	voice := byTitle["サンプルボイスコレクション"]
	if _, ok := voice.StartsAt(); ok || voice.Period.Key() != "2026-09-20/2026-10-20" || voice.URL != "https://shop.milpr.com/products/voice-2026" {
		t.Fatalf("voice=%+v", voice)
	}
	// The evidence holds no description, price or stock.
	raw, _ := json.Marshal(join)
	for _, private := range []string{"1650", "available", "variants", "商品説明", "発送時期"} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("evidence keeps %s: %s", private, raw)
		}
	}
}

func TestGroupKeepsAStoredGroupWhateverTheWindowShows(t *testing.T) {
	stored := []Sale{{
		Key: "サンプルシーズン2026", Period: Period{Opens: "2026-10-24", Closes: "2026-11-24"}, Title: "サンプルシーズン 2026",
		URL: "https://shop.milpr.com/products/season-board", Products: []ProductRef{{ID: 21, Handle: "season-board", Title: "サンプルシーズン 2026 アクリルボード", PublishedAt: "2026-10-24T10:00:12Z"}},
	}}
	// Only the two-word item is left in the window: alone it would be its own
	// group, but the stored sale holds it, and a new product of the period
	// joins by prefix. No collection list is needed for a known product.
	known := []Candidate{candidate(21, "season-board", "サンプルシーズン 2026 ランダム缶バッジ 全8種", "2026-10-24T19:00:12+09:00", period)}
	if NeedsCollections(known, stored) {
		t.Fatal("a known product needs no collection list")
	}
	fresh := append(known, candidate(23, "season-pen", "サンプルシーズン 2026 ボールペン", "2026-10-24T19:00:14+09:00", period))
	if !NeedsCollections(fresh, stored) {
		t.Fatal("a new product needs the collection list")
	}
	sales := Group(fresh, stored, nil)
	if len(sales) != 1 || sales[0].Key != "サンプルシーズン2026" || len(sales[0].Products) != 2 || sales[0].URL != "https://shop.milpr.com/products/season-board" {
		t.Fatalf("sales=%+v", sales)
	}
	// The same product in another period is another group.
	later := candidate(21, "season-board", "サンプルシーズン 2026 アクリルボード", "2026-10-24T19:00:12+09:00", "＜受注期間＞2027年1月10日 〜 2027年2月10日")
	if !NeedsCollections([]Candidate{later}, stored) {
		t.Fatal("a new period needs the collection list")
	}
	if sales = Group([]Candidate{later}, stored, nil); len(sales) != 1 || sales[0].Period.Key() != "2027-01-10/2027-02-10" {
		t.Fatalf("later=%+v", sales)
	}
	if _, ok := sales[0].StartsAt(); ok {
		t.Fatal("a product published before the opening date gives no opening time")
	}
}

func TestReadListsOnceReadsCollectionsOnlyForNewProductsAndHonorsRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 8, 3, 0, 0, 0, time.UTC)
	paths := []string{}
	limited := false
	pages := map[string]int{"1": CatalogPageSize, "2": 3}
	client := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path+"?"+r.URL.RawQuery)
		switch {
		case limited:
			return answer(429, "local_rate_limited", "Retry-After", "60"), nil
		case r.URL.Path == "/collections.json":
			return answer(200, `{"collections":[{"handle":"join_goods","title":"サンプル加入記念グッズ"}]}`), nil
		case r.URL.Path == "/products.json":
			items := []map[string]any{}
			page := r.URL.Query().Get("page")
			for i := range pages[page] {
				items = append(items, product(int64(1000*int(page[0]-'0')+i), fmt.Sprintf("filler-%s-%d", r.URL.Query().Get("page"), i), "サンプル他社グッズ", "2026-01-01T12:00:00+09:00", period))
			}
			items = append(items, product(11, "join-acrylic", "サンプル加入記念グッズ アクリルスタンド", "2026-10-24T19:00:08+09:00", period, Tag))
			return answer(200, products(items...)), nil
		}
		return answer(200, products(product(11, "join-acrylic", "サンプル加入記念グッズ アクリルスタンド", "2026-10-24T19:00:08+09:00", period, Tag))), nil
	})})
	adapter := NewAdapter(client)
	adapter.Options = Options{Now: func() time.Time { return now }}
	ctx := context.Background()

	first, err := adapter.Read(ctx, State{}, nil, false)
	if err != nil || first.Requested != 2 || !first.Collections || len(first.Sales) != 1 || first.Sales[0].Collection != "join_goods" || first.State.LastSuccessAt != now {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	// A product a stored sale holds needs only the listing.
	paths = nil
	again, err := adapter.Read(ctx, first.State, first.Sales, false)
	if err != nil || again.Requested != 1 || again.Collections || len(paths) != 1 {
		t.Fatalf("again=%+v paths=%v err=%v", again, paths, err)
	}
	// The full read pages until a short page.
	paths = nil
	all, err := adapter.Read(ctx, again.State, again.Sales, true)
	if err != nil || all.Pages != 2 || !all.ReachedEnd || all.Listed != CatalogPageSize+3+1 || len(all.Sales) != 1 {
		t.Fatalf("all=%+v err=%v", all, err)
	}
	// A 429 fails the read without sales and holds every read back until
	// its Retry-After passes.
	limited, paths = true, nil
	failed, err := adapter.Read(ctx, all.State, all.Sales, false)
	if ErrorCode(err) != "shop_rate_limited" || failed.Sales != nil || failed.State.LastError != "shop_rate_limited" || !failed.State.BlockedUntil.Equal(now.Add(time.Minute)) || failed.State.LastSuccessAt != now {
		t.Fatalf("failed=%+v err=%v", failed, err)
	}
	paths = nil
	held, err := adapter.Read(ctx, failed.State, all.Sales, true)
	if err != nil || held.Skipped != SkippedRateLimited || len(paths) != 0 {
		t.Fatalf("held=%+v paths=%v err=%v", held, paths, err)
	}
	now, limited = now.Add(2*time.Minute), false
	if after, err := adapter.Read(ctx, failed.State, all.Sales, false); err != nil || after.Skipped != "" || !after.State.BlockedUntil.IsZero() || after.State.LastError != "" {
		t.Fatalf("after=%+v err=%v", after, err)
	}
}

// A full catalog page ends the read by what it lists: a product left out as
// unusable does not make a full page look like the last one.
func TestFullReadCountsAFullPageWithAnUnusableProduct(t *testing.T) {
	requested := 0
	client := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/products.json" {
			return answer(200, `{"collections":[]}`), nil
		}
		requested++
		items := []map[string]any{}
		if r.URL.Query().Get("page") == "1" {
			items = append(items, product(1, "bad handle", "サンプルグッズ", "2026-10-01T12:00:00+09:00", period, Tag))
			for i := 1; i < CatalogPageSize; i++ {
				items = append(items, product(int64(1+i), fmt.Sprintf("filler-%d", i), "サンプル他社グッズ", "2026-10-01T12:00:00+09:00", period))
			}
		}
		return answer(200, products(items...)), nil
	})})
	adapter := NewAdapter(client)
	adapter.Options = Options{}
	all, err := adapter.Read(context.Background(), State{}, nil, true)
	if err != nil || all.Pages != 2 || requested != 2 || !all.ReachedEnd || all.Listed != CatalogPageSize-1 {
		t.Fatalf("all=%+v requested=%d err=%v", all, requested, err)
	}
}

func TestDecomposedKanaMatchesItsPrecomposedForm(t *testing.T) {
	// The collection title spells ボ as ホ + U+3099 and ポ as ホ + U+309A;
	// the product title uses the precomposed kana.
	collections, err := ParseCollections([]byte(`{"collections":[{"handle":"sample_voice","title":"サンプル おやすみホ\u3099イスコレクション ホ\u309aスター"}]}`))
	if err != nil || len(collections) != 1 || collections[0].Title != "サンプル おやすみボイスコレクション ポスター" {
		t.Fatalf("collections=%q err=%v", collections, err)
	}
	sales := Group([]Candidate{candidate(41, "sample-voice-full", "サンプル おやすみボイスコレクション ポスター フルセット", "2026-09-20T19:00:00+09:00", "＜受注期間＞2026年9月20日 〜 2026年10月20日")}, nil, []Collection{{Handle: "sample_voice", Title: "サンプル おやすみホ\u3099イスコレクション ホ\u309aスター"}})
	if len(sales) != 1 || sales[0].URL != "https://shop.milpr.com/collections/sample_voice" || sales[0].Key != "サンプルおやすみボイスコレクションポスター" {
		t.Fatalf("sales=%+v", sales)
	}
	// A stored key read before composition still joins its products.
	if normalize("ホ\u3099イス") != normalize("ボイス") {
		t.Fatal("normalize keeps the decomposed form")
	}
	for in, want := range map[string]string{
		"\u3099ガ":                      "\u3099ガ", // a mark with nothing before it stays
		"A\u3099":                      "A\u3099", // and so does one after a letter that does not compose
		"ウ\u3099ヽ\u3099ハ\u309aは\u309a": "ヴヾパぱ",
		"ン\u3099":                      "ン\u3099",
	} {
		if got := composeKana(in); got != want {
			t.Errorf("composeKana(%q) = %q, want %q", in, got, want)
		}
	}
	if len(composedKana) != 58 {
		t.Fatalf("composedKana has %d pairs, want 58", len(composedKana))
	}
}
