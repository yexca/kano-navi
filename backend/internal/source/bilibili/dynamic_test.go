package bilibili

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Synthetic listing in the feed/space shape: a word dynamic with an emoji, a
// picture dynamic, an opus dynamic, another author's forward, an upload, a
// live notice and another author's item.
const spaceFeed = `{"code":0,"message":"0","data":{"has_more":true,"offset":"900000000000000001","items":[
 {"id_str":"900000000000000010","type":"DYNAMIC_TYPE_WORD","visible":true,"modules":{"module_author":{"mid":316381099,"pub_ts":1794024288},"module_dynamic":{"desc":{"text":"x","rich_text_nodes":[
  {"type":"RICH_TEXT_NODE_TYPE_TEXT","text":"今天20点开始播杂谈 "},{"type":"RICH_TEXT_NODE_TYPE_EMOJI","text":"[synthetic_emoji]"},{"type":"RICH_TEXT_NODE_TYPE_TOPIC","text":"#话题#"},{"type":"RICH_TEXT_NODE_TYPE_AT","text":"@someone"}]},"major":null}}},
 {"id_str":"900000000000000009","type":"DYNAMIC_TYPE_DRAW","modules":{"module_author":{"mid":"316381099","pub_ts":"1794020000"},"module_dynamic":{"major":{"type":"MAJOR_TYPE_DRAW","draw":{"items":[{"src":"http://i0.hdslb.com/bfs/new_dyn/synthetic-a.jpg"},{"src":"https://images.invalid/b.jpg"}]}},"desc":{"rich_text_nodes":[{"type":"RICH_TEXT_NODE_TYPE_EMOJI","text":"[only_emoji]"}]}}}},
 {"id_str":"900000000000000008","type":"DYNAMIC_TYPE_DRAW","modules":{"module_author":{"mid":316381099,"pub_ts":1794010000},"module_dynamic":{"major":{"type":"MAJOR_TYPE_OPUS","opus":{"title":"","summary":{"text":"s","rich_text_nodes":[{"type":"RICH_TEXT_NODE_TYPE_TEXT","text":"图文"},{"type":"RICH_TEXT_NODE_TYPE_WEB","text":"网页链接"}]},"pics":[{"url":"//i1.hdslb.com/bfs/new_dyn/synthetic-c.png"}]}}},"module_tag":{"text":"置顶"}}},
 {"id_str":"900000000000000007","type":"DYNAMIC_TYPE_FORWARD","modules":{"module_author":{"mid":43,"pub_ts":1794000000},"module_dynamic":{"desc":{"rich_text_nodes":[{"type":"RICH_TEXT_NODE_TYPE_EMOJI","text":"[synthetic_emoji]"}]}}},"orig":{"id_str":null,"type":"DYNAMIC_TYPE_NONE","modules":{"module_author":{},"module_dynamic":{"major":{"type":"MAJOR_TYPE_NONE","none":{"tips":"deleted"}}}}}},
 {"id_str":"900000000000000006","type":"DYNAMIC_TYPE_AV","modules":{"module_author":{"mid":316381099,"pub_ts":1794000000},"module_dynamic":{"major":{"type":"MAJOR_TYPE_ARCHIVE"}}}},
 {"id_str":"900000000000000005","type":"DYNAMIC_TYPE_LIVE_RCMD","modules":{"module_author":{"mid":316381099,"pub_ts":1794000000},"module_dynamic":{}}},
 {"id_str":"900000000000000004","type":"DYNAMIC_TYPE_WORD","modules":{"module_author":{"mid":42,"pub_ts":1794000000},"module_dynamic":{"desc":{"rich_text_nodes":[{"type":"RICH_TEXT_NODE_TYPE_TEXT","text":"other"}]}}}}
]}}`

func TestSpaceFeedKeepsOwnPostsAndDropsEmoji(t *testing.T) {
	data, err := envelope([]byte(spaceFeed))
	if err != nil {
		t.Fatal(err)
	}
	page, err := ParseSpaceFeed(data, DefaultAccount)
	if err != nil || len(page.Dynamics) != 3 || page.Seen != 7 || !page.HasMore {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	word, draw, opus := page.Dynamics[0], page.Dynamics[1], page.Dynamics[2]
	if word.PostID() != "bilibili-900000000000000010" || word.Text != "今天20点开始播杂谈 #话题#@someone" ||
		word.PublishedAt != "2026-11-07T04:04:48Z" || word.URL() != "https://www.bilibili.com/opus/900000000000000010" || len(word.Images) != 0 {
		t.Fatalf("word=%#v", word)
	}
	// The emoji stays in the raw nodes.
	if nodes, _ := word.Raw["nodes"].([]richNode); len(nodes) != 4 || nodes[1].Type != emojiNode {
		t.Fatalf("raw=%#v", word.Raw)
	}
	// An emoji-only dynamic has no text; pictures off the CDN are dropped.
	if draw.Text != "" || len(draw.Images) != 1 || draw.Images[0] != "https://i0.hdslb.com/bfs/new_dyn/synthetic-a.jpg" {
		t.Fatalf("draw=%#v", draw)
	}
	if opus.Text != "图文网页链接" || len(opus.Images) != 1 || opus.Images[0] != "https://i1.hdslb.com/bfs/new_dyn/synthetic-c.png" || opus.Raw["tag"] != "置顶" {
		t.Fatalf("opus=%#v", opus)
	}
}

// A supporters-only dynamic never becomes a post, and a forward of one quotes
// it as unavailable, even when a supporter's listing carries it in full.
func TestSpaceFeedDropsSupportersOnlyDynamics(t *testing.T) {
	const feed = `{"code":0,"message":"0","data":{"has_more":false,"offset":"","items":[
 {"id_str":"900000000000000020","type":"DYNAMIC_TYPE_WORD","basic":{"is_only_fans":true},"modules":{"module_author":{"mid":316381099,"pub_ts":1794024288},"module_dynamic":{"desc":{"rich_text_nodes":[{"type":"RICH_TEXT_NODE_TYPE_TEXT","text":"supporters only"}]}}}},
 {"id_str":"900000000000000019","type":"DYNAMIC_TYPE_FORWARD","modules":{"module_author":{"mid":316381099,"pub_ts":1794020000},"module_dynamic":{"desc":{"rich_text_nodes":[{"type":"RICH_TEXT_NODE_TYPE_TEXT","text":"forward"}]}}},
  "orig":{"id_str":"900000000000000018","type":"DYNAMIC_TYPE_WORD","basic":{"is_only_fans":true},"modules":{"module_author":{"mid":43,"pub_ts":1794010000},"module_dynamic":{"desc":{"rich_text_nodes":[{"type":"RICH_TEXT_NODE_TYPE_TEXT","text":"paid wording"}]}}}}}
]}}`
	data, err := envelope([]byte(feed))
	if err != nil {
		t.Fatal(err)
	}
	page, err := ParseSpaceFeed(data, DefaultAccount)
	if err != nil || len(page.Dynamics) != 1 {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	forward := page.Dynamics[0]
	if forward.Quote == nil || !forward.Quote.Unavailable || strings.Contains(forward.Quote.Text, "paid") {
		t.Fatalf("quote=%#v", forward.Quote)
	}
}

func TestSpaceFeedReportsVoucherAndLogin(t *testing.T) {
	if _, err := ParseSpaceFeed([]byte(`{"v_voucher":"voucher_synthetic"}`), DefaultAccount); ErrorCode(err) != "bilibili_risk_control_voucher" {
		t.Fatalf("voucher err=%v", err)
	}
	if _, err := envelope([]byte(`{"code":-101,"message":"账号未登录"}`)); ErrorCode(err) != "bilibili_login_expired" {
		t.Fatalf("login err=%v", err)
	}
}

func TestParseDynamicIDAcceptsOpusAndDynamicLinks(t *testing.T) {
	for input, want := range map[string]string{
		"900000000000000010": "900000000000000010",
		"https://www.bilibili.com/opus/900000000000000010?spm_id_from=1": "900000000000000010",
		"https://t.bilibili.com/900000000000000010":                      "900000000000000010",
		"https://www.bilibili.com/video/BV1AAAAAAAAA":                    "",
		"https://bilibili.com.invalid/opus/900000000000000010":           "",
		"https://t.bilibili.com/900000000000000010/extra":                "",
		"javascript:alert(1)":                                            "",
	} {
		got, ok := ParseDynamicID(input)
		if got != want || ok != (want != "") {
			t.Fatalf("%q => %q %v, want %q", input, got, ok, want)
		}
	}
}

const opusPage = `<!DOCTYPE html><html><head><script>window.__INITIAL_STATE__={"id":"900000000000000010","detail":{"id_str":"900000000000000010","type":0,"basic":{"uid":"316381099","comment_type":11},"modules":[
 {"module_type":"MODULE_TYPE_AUTHOR","module_author":{"mid":316381099,"name":"synthetic","pub_ts":"1794024288"}},
 {"module_type":"MODULE_TYPE_CONTENT","module_content":{"paragraphs":[
  {"para_type":1,"text":{"nodes":[{"type":"TEXT_NODE_TYPE_WORD","word":{"words":"大家好！今天北京时间17点上传。\n"}},{"type":"TEXT_NODE_TYPE_RICH","rich":{"type":"RICH_TEXT_NODE_TYPE_EMOJI","text":"[synthetic_emoji]"}}]}},
  {"para_type":2,"pic":{"pics":[{"url":"http://i0.hdslb.com/bfs/new_dyn/synthetic-d.jpg"}]}},
  {"para_type":1,"text":{"nodes":[{"type":"TEXT_NODE_TYPE_RICH","rich":{"type":"RICH_TEXT_NODE_TYPE_TOPIC","text":"#话题#"}}]}}]}},
 {"module_type":"MODULE_TYPE_STAT","module_stat":{}}]}};(function(){var s;}())</script></head></html>`

func TestOpusPageReadsTheServerState(t *testing.T) {
	d, err := ParseOpusPage([]byte(opusPage), "900000000000000010")
	if err != nil || d.OwnerMid != 316381099 || d.Text != "大家好！今天北京时间17点上传。\n#话题#" || d.PublishedAt != "2026-11-07T04:04:48Z" ||
		len(d.Images) != 1 || d.Images[0] != "https://i0.hdslb.com/bfs/new_dyn/synthetic-d.jpg" {
		t.Fatalf("dynamic=%#v err=%v", d, err)
	}
	if _, err = ParseOpusPage([]byte(opusPage), "900000000000000011"); err == nil {
		t.Fatal("accepted another dynamic's page")
	}
	if _, err = ParseOpusPage([]byte("<html>no state</html>"), "900000000000000010"); ErrorCode(err) != "bilibili_risk_control_page" {
		t.Fatalf("missing state err=%v", err)
	}
}

func TestWBISignatureFollowsTheDocumentedExample(t *testing.T) {
	keys := WBIKeys{Img: "7cd084941338484aae1ad9425b84077c", Sub: "4932caff0ff746eab6f01bf08b70ac45"}
	if got := keys.mixinKey(); got != "ea1db124af3c7062474693fa704f4ff8" {
		t.Fatalf("mixin=%s", got)
	}
	signed := signWBI(url.Values{"foo": {"114"}, "bar": {"514"}, "zab": {"1919810"}}, keys, time.Unix(1702204169, 0))
	if !strings.HasPrefix(signed, "bar=514&foo=114&wts=1702204169&zab=1919810&w_rid=") || len(signed) != len("bar=514&foo=114&wts=1702204169&zab=1919810&w_rid=")+32 {
		t.Fatalf("signed=%s", signed)
	}
	if got := signWBI(url.Values{"a": {"one one(四)!"}}, keys, time.Unix(1, 0)); !strings.HasPrefix(got, "a=one%20one%E5%9B%9B&wts=1&") {
		t.Fatalf("escaped=%s", got)
	}
}

func TestNavKeysReadSignedOutAnswers(t *testing.T) {
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	keys, signedIn, err := parseNavKeys([]byte(`{"code":-101,"data":{"isLogin":false,"wbi_img":{"img_url":"https://i0.hdslb.com/bfs/wbi/0123456789abcdef0123456789abcdef.png","sub_url":"https://i0.hdslb.com/bfs/wbi/fedcba9876543210fedcba9876543210.png"}}}`), now)
	if err != nil || signedIn || keys.Img != "0123456789abcdef0123456789abcdef" || keys.Sub != "fedcba9876543210fedcba9876543210" || !keys.fresh(now) || keys.fresh(now.Add(13*time.Hour)) {
		t.Fatalf("keys=%#v signedIn=%v err=%v", keys, signedIn, err)
	}
	if _, _, err = parseNavKeys([]byte(`{"code":-352}`), now); ErrorCode(err) != "bilibili_risk_control_352" {
		t.Fatalf("captcha err=%v", err)
	}
}

func TestLiveStatusReadsOnlyALiveBroadcastAsLive(t *testing.T) {
	for body, want := range map[string]Room{
		`{"code":0,"data":{"316381099":{"title":"synthetic","room_id":1000001,"live_status":1,"live_time":1794049200}}}`: {RoomID: "1000001", Title: "synthetic", LiveStatus: 1, LiveTime: "2026-11-07T11:00:00Z"},
		`{"code":0,"data":{"316381099":{"title":"rotation","room_id":1000001,"live_status":2,"live_time":1794049200}}}`:  {RoomID: "1000001", Title: "rotation", LiveStatus: 2},
		`{"code":0,"data":{"316381099":{"title":"off","room_id":"1000001","live_status":0,"live_time":0}}}`:              {RoomID: "1000001", Title: "off"},
	} {
		data, err := envelope([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if room, err := ParseLiveStatus(data, DefaultAccount); err != nil || room != want {
			t.Fatalf("%s => %#v %v", body, room, err)
		}
	}
	if _, err := ParseLiveStatus([]byte(`{}`), DefaultAccount); err == nil {
		t.Fatal("accepted an answer without the account")
	}
}

const navKeys = `{"code":0,"data":{"isLogin":true,"wbi_img":{"img_url":"https://i0.hdslb.com/bfs/wbi/0123456789abcdef0123456789abcdef.png","sub_url":"https://i0.hdslb.com/bfs/wbi/fedcba9876543210fedcba9876543210.png"}}}`

func TestDynamicsNeedALoginAndShareTheBackoff(t *testing.T) {
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	login := "synthetic0login%2C1767225600%2Cabcde%2A11"
	type call struct{ host, path, cookie, query string }
	calls := []call{}
	reply := map[string]string{"/x/frontend/finger/spi": session, ticketPath: ticket, "/x/web-interface/nav": navKeys, "/x/polymer/web-dynamic/v1/feed/space": spaceFeed}
	adapter := func(sessdata string) *Adapter {
		c := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
			calls = append(calls, call{r.URL.Host, r.URL.Path, r.Header.Get("Cookie"), r.URL.RawQuery})
			return response(200, reply[r.URL.Path]), nil
		})})
		a := NewAdapter(c, Options{Account: DefaultAccount, MinInterval: 3 * time.Hour, SESSDATA: sessdata})
		a.Now = func() time.Time { return now }
		return a
	}
	// Without the login cookie nothing is requested.
	if result, err := adapter("").FetchDynamics(context.Background(), State{}); err != nil || result.Skipped != "login_required" || len(calls) != 0 {
		t.Fatalf("anonymous=%#v calls=%v err=%v", result, calls, err)
	}
	result, err := adapter(login).FetchDynamics(context.Background(), State{})
	// The device identity, the ticket, the daily login check and the listing.
	if err != nil || len(result.Dynamics) != 3 || result.Requested != 4 || !result.State.WBI.valid() || !result.State.Dynamics.LastSuccessAt.Equal(now) ||
		!result.State.Login.SignedIn || !result.State.Login.CheckedAt.Equal(now) {
		t.Fatalf("signed in=%#v err=%v", result, err)
	}
	listing := calls[3]
	if listing.host != "api.bilibili.com" || !strings.Contains(listing.cookie, "SESSDATA="+login) || !strings.Contains(listing.query, "host_mid=316381099") || !strings.Contains(listing.query, "&w_rid=") {
		t.Fatalf("listing=%#v", listing)
	}
	// Within the interval, and while the shared block lasts, no request.
	calls = nil
	if soon, _ := adapter(login).FetchDynamics(context.Background(), result.State); soon.Skipped != "interval" || len(calls) != 0 {
		t.Fatalf("soon=%#v calls=%v", soon, calls)
	}
	state := result.State
	state.Dynamics.LastAttemptAt, state.BlockedUntil = time.Time{}, now.Add(time.Hour)
	if blockedRead, _ := adapter(login).FetchDynamics(context.Background(), state); blockedRead.Skipped != "blocked" || len(calls) != 0 {
		t.Fatalf("blocked=%#v calls=%v", blockedRead, calls)
	}
	// An expired login is a controlled code; a captcha starts the shared block.
	state.BlockedUntil = time.Time{}
	reply["/x/polymer/web-dynamic/v1/feed/space"] = `{"code":-101,"message":"账号未登录"}`
	expired, err := adapter(login).FetchDynamics(context.Background(), state)
	if ErrorCode(err) != "bilibili_login_expired" || expired.State.Dynamics.LastError != "bilibili_login_expired" || !expired.State.BlockedUntil.IsZero() || expired.Requested != 1 {
		t.Fatalf("expired=%#v err=%v", expired, err)
	}
	reply["/x/polymer/web-dynamic/v1/feed/space"] = `{"code":0,"data":{"v_voucher":"voucher_synthetic"}}`
	captcha, err := adapter(login).FetchDynamics(context.Background(), state)
	if ErrorCode(err) != "bilibili_risk_control_voucher" || !captcha.State.BlockedUntil.Equal(now.Add(6*time.Hour)) || captcha.State.Failures != 1 || captcha.State.WBI.valid() {
		t.Fatalf("captcha=%#v err=%v", captcha, err)
	}
}

func TestOperatorDynamicReadsTheOpusPageWithoutTheLogin(t *testing.T) {
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	var page *http.Request
	c := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/x/frontend/finger/spi" {
			return response(200, session), nil
		}
		page = r
		return response(200, opusPage), nil
	})})
	a := NewAdapter(c, Options{Account: DefaultAccount, MinInterval: 3 * time.Hour, SESSDATA: "synthetic0login%2C1767225600%2Cabcde%2A11"})
	a.Now = func() time.Time { return now }
	d, _, err := a.FetchDynamic(context.Background(), State{}, "900000000000000010", false)
	if err != nil || d.IDStr != "900000000000000010" || page.URL.Host != "www.bilibili.com" || page.URL.Path != "/opus/900000000000000010" || strings.Contains(page.Header.Get("Cookie"), "SESSDATA") {
		t.Fatalf("dynamic=%#v page=%v err=%v", d, page.URL, err)
	}
	a.Options.Account = "42"
	if _, _, err = a.FetchDynamic(context.Background(), State{}, "900000000000000010", false); ErrorCode(err) != "bilibili_other_account" {
		t.Fatalf("other err=%v", err)
	}
}

func TestLiveChecksUseTheLiveHostWithoutCookiesAndBackOff(t *testing.T) {
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	status, body := 200, `{"code":0,"data":{"316381099":{"title":"synthetic","room_id":1000001,"live_status":1,"live_time":1767311000}}}`
	var seen *http.Request
	c := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		seen = r
		return response(status, body), nil
	})})
	a := NewAdapter(c, Options{Account: DefaultAccount, SESSDATA: "synthetic0login%2C1767225600%2Cabcde%2A11"})
	a.Now = func() time.Time { return now }
	state, requested, err := a.CheckLive(context.Background(), LiveState{})
	if err != nil || !requested || state.Room.LiveStatus != 1 || !state.CheckedAt.Equal(now) || seen.URL.Host != "api.live.bilibili.com" || seen.Header.Get("Cookie") != "" || seen.URL.Query().Get("uids[]") != "316381099" {
		t.Fatalf("state=%#v url=%v err=%v", state, seen.URL, err)
	}
	status = 412
	failedState, _, err := a.CheckLive(context.Background(), state)
	if ErrorCode(err) != "bilibili_risk_control_http" || !failedState.BlockedUntil.Equal(now.Add(6*time.Hour)) || failedState.Room != state.Room {
		t.Fatalf("failed=%#v err=%v", failedState, err)
	}
	seen = nil
	if _, requested, _ = a.CheckLive(context.Background(), failedState); requested || seen != nil {
		t.Fatal("requested during the live block")
	}
}
