package bilibili

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// forwardFeed is a synthetic feed/space page of forwards: of another
// author's opus post, of a picture post without wording of its own, of an
// upload, of a deleted dynamic, of a live room, of a forward, of an article
// in its old form, and a forward by another author.
func forwardFeed() string {
	pics := []string{}
	for i := 0; i < 11; i++ {
		pics = append(pics, fmt.Sprintf(`{"url":"https://i0.hdslb.com/bfs/new_dyn/synthetic-p%d.jpg"}`, i))
	}
	pics = append([]string{`{"url":"https://images.invalid/synthetic.jpg"}`}, pics...)
	own := func(id, nodes, orig string) string {
		return `{"id_str":"` + id + `","type":"DYNAMIC_TYPE_FORWARD","modules":{"module_author":{"mid":316381099,"name":"own","pub_ts":1794030000},"module_dynamic":{"desc":{"rich_text_nodes":` + nodes + `},"major":null}},"orig":` + orig + `}`
	}
	words := func(text string) string {
		return `[{"type":"RICH_TEXT_NODE_TYPE_TEXT","text":"` + text + `"},{"type":"RICH_TEXT_NODE_TYPE_EMOJI","text":"[synthetic_emoji]"}]`
	}
	author := `"module_author":{"mid":"42","name":"  Synthetic &amp; Agency  ","pub_ts":1794020000}`
	items := []string{
		own("900000000000000110", words("看这个"), `{"id_str":"900000000000000210","type":"DYNAMIC_TYPE_DRAW","modules":{`+author+`,"module_dynamic":{"major":{"type":"MAJOR_TYPE_OPUS","opus":{"title":"活动公告","summary":{"rich_text_nodes":[{"type":"RICH_TEXT_NODE_TYPE_TEXT","text":"明天20点直播"},{"type":"RICH_TEXT_NODE_TYPE_EMOJI","text":"[e]"}]},"pics":[`+strings.Join(pics, ",")+`]}}}}}`),
		own("900000000000000109", `[]`, `{"id_str":"900000000000000209","type":"DYNAMIC_TYPE_DRAW","modules":{"module_author":{"mid":316381099,"name":"own","pub_ts":1794010000},"module_dynamic":{"desc":{"rich_text_nodes":[{"type":"RICH_TEXT_NODE_TYPE_TEXT","text":"图片动态"}]},"major":{"type":"MAJOR_TYPE_DRAW","draw":{"items":[{"src":"//i1.hdslb.com/bfs/new_dyn/synthetic-d.png"}]}}}}}`),
		own("900000000000000108", words("投稿了"), `{"id_str":"900000000000000208","type":"DYNAMIC_TYPE_AV","modules":{`+author+`,"module_dynamic":{"major":{"type":"MAJOR_TYPE_ARCHIVE","archive":{"bvid":"BV1AAAAAAAAA","title":"合唱视频","cover":"http://i2.hdslb.com/bfs/archive/synthetic-cover.jpg"}}}}}`),
		own("900000000000000107", words("原动态没了"), `{"id_str":null,"type":"DYNAMIC_TYPE_NONE","modules":{"module_author":{},"module_dynamic":{"major":{"type":"MAJOR_TYPE_NONE","none":{"tips":"deleted"}}}}}`),
		own("900000000000000106", words("直播间"), `{"id_str":"900000000000000206","type":"DYNAMIC_TYPE_LIVE_RCMD","modules":{`+author+`,"module_dynamic":{"major":{"type":"MAJOR_TYPE_LIVE_RCMD"}}}}`),
		own("900000000000000105", words("再转"), `{"id_str":"900000000000000205","type":"DYNAMIC_TYPE_FORWARD","modules":{`+author+`,"module_dynamic":{"desc":{"rich_text_nodes":[{"type":"RICH_TEXT_NODE_TYPE_TEXT","text":"转发理由"}]}}},"orig":{"id_str":"900000000000000305","type":"DYNAMIC_TYPE_WORD","modules":{`+author+`,"module_dynamic":{"desc":{"rich_text_nodes":[{"type":"RICH_TEXT_NODE_TYPE_TEXT","text":"nested"}]}}}}}`),
		own("900000000000000104", words("专栏"), `{"id_str":"900000000000000204","type":"DYNAMIC_TYPE_ARTICLE","modules":{`+author+`,"module_dynamic":{"major":{"type":"MAJOR_TYPE_ARTICLE","article":{"title":"old form"}}}}}`),
		`{"id_str":"900000000000000103","type":"DYNAMIC_TYPE_FORWARD","modules":{"module_author":{"mid":42,"pub_ts":1794030000},"module_dynamic":{"desc":{"rich_text_nodes":[{"type":"RICH_TEXT_NODE_TYPE_TEXT","text":"other"}]}}},"orig":{"id_str":"900000000000000203","type":"DYNAMIC_TYPE_WORD","modules":{` + author + `,"module_dynamic":{"desc":{"rich_text_nodes":[{"type":"RICH_TEXT_NODE_TYPE_TEXT","text":"x"}]}}}}}`,
	}
	return `{"has_more":false,"offset":"","items":[` + strings.Join(items, ",") + `]}`
}

func TestForwardsQuoteTheForwardedDynamicFromTheListing(t *testing.T) {
	page, err := ParseSpaceFeed(json.RawMessage(forwardFeed()), DefaultAccount)
	if err != nil || page.Seen != 8 || len(page.Dynamics) != 7 {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	byID := map[string]Dynamic{}
	for _, d := range page.Dynamics {
		if d.Quote == nil || len(d.Images) != 0 || d.Raw["type"] != ForwardType {
			t.Fatalf("forward=%#v", d)
		}
		byID[d.IDStr] = d
	}
	if _, ok := byID["900000000000000103"]; ok {
		t.Fatal("kept another author's forward")
	}

	opus := byID["900000000000000110"]
	q := opus.Quote
	if opus.Text != "看这个" || opus.PublishedAt != "2026-11-07T05:40:00Z" || opus.URL() != "https://www.bilibili.com/opus/900000000000000110" {
		t.Fatalf("forward=%#v", opus)
	}
	if q.ID != "bilibili-900000000000000210" || q.URL != "https://www.bilibili.com/opus/900000000000000210" || q.AuthorHandle != "42" ||
		q.AuthorName != "Synthetic & Agency" || q.Text != "活动公告\n明天20点直播" || q.PublishedAt != "2026-11-07T02:53:20Z" || q.Unavailable {
		t.Fatalf("quote=%#v", q)
	}
	// Only CDN pictures, at most nine, and never in the projection.
	if len(q.MediaURLs) != maxDynamicImages || q.MediaURLs[0] != "https://i0.hdslb.com/bfs/new_dyn/synthetic-p0.jpg" {
		t.Fatalf("quote media=%v", q.MediaURLs)
	}
	if body, _ := json.Marshal(q); strings.Contains(string(body), "hdslb") || strings.Contains(string(body), "source") {
		t.Fatalf("projection=%s", body)
	}

	// No wording of its own: just the quote.
	if d := byID["900000000000000109"]; d.Text != "" || d.Quote.Text != "图片动态" || len(d.Quote.MediaURLs) != 1 || d.Quote.MediaURLs[0] != "https://i1.hdslb.com/bfs/new_dyn/synthetic-d.png" {
		t.Fatalf("textless=%#v quote=%#v", d, d.Quote)
	}
	if q := byID["900000000000000108"].Quote; q.URL != "https://www.bilibili.com/video/BV1AAAAAAAAA" || q.Text != "合唱视频" ||
		len(q.MediaURLs) != 1 || q.MediaURLs[0] != "https://i2.hdslb.com/bfs/archive/synthetic-cover.jpg" || q.ID != "bilibili-900000000000000208" {
		t.Fatalf("upload quote=%#v", q)
	}
	// A deleted dynamic without an ID links to the forward itself.
	if q := byID["900000000000000107"].Quote; !q.Unavailable || q.ID != "" || q.URL != "https://www.bilibili.com/opus/900000000000000107" || q.Text != "" {
		t.Fatalf("deleted quote=%#v", q)
	}
	for _, id := range []string{"900000000000000106", "900000000000000104"} {
		q := byID[id].Quote
		want := "900000000000000" + "2" + id[16:]
		if !q.Unavailable || q.ID != PostPrefix+want || q.URL != "https://www.bilibili.com/opus/"+want || q.AuthorHandle != "" || q.Text != "" || len(q.MediaURLs) != 0 {
			t.Fatalf("%s quote=%#v", id, q)
		}
	}
	// A forwarded forward shows its own wording only.
	if q := byID["900000000000000105"].Quote; q.Unavailable || q.Text != "转发理由" || len(q.MediaURLs) != 0 || q.ID != "bilibili-900000000000000205" {
		t.Fatalf("nested quote=%#v", q)
	}
}

// A forward is kept whatever became of the dynamic it forwards: the store
// decides whether one without wording of its own is shown.
func TestForwardsOfADeletedDynamicAreKept(t *testing.T) {
	feed := `{"items":[{"id_str":"900000000000000110","type":"DYNAMIC_TYPE_FORWARD","modules":{"module_author":{"mid":316381099,"pub_ts":1794030000},"module_dynamic":{"desc":null}},"orig":null},
	 {"id_str":"900000000000000111","type":"DYNAMIC_TYPE_FORWARD","modules":{"module_author":{"mid":316381099,"pub_ts":1794030000},"module_dynamic":{"desc":{"rich_text_nodes":[{"type":"RICH_TEXT_NODE_TYPE_TEXT","text":"转发"}]}}}}]}`
	page, err := ParseSpaceFeed(json.RawMessage(feed), DefaultAccount)
	if err != nil || len(page.Dynamics) != 2 {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	for _, d := range page.Dynamics {
		if !d.Quote.Unavailable || d.Quote.ID != "" || d.Quote.URL != d.URL() {
			t.Fatalf("forward=%#v quote=%#v", d, d.Quote)
		}
	}
	if page.Dynamics[0].Text != "" || page.Dynamics[1].Text != "转发" {
		t.Fatalf("texts=%q %q", page.Dynamics[0].Text, page.Dynamics[1].Text)
	}
}

// A forwarded dynamic of an unexpected shape never fails the page: its quote
// is unavailable, with its ID when that still reads.
func TestAnUnreadableForwardedDynamicIsAnUnavailableQuote(t *testing.T) {
	origs := map[string]string{
		`{"id_str":"900000000000000213","type":"DYNAMIC_TYPE_WORD","visible":"yes","modules":{}}`:                                                                  "900000000000000213",
		`{"id_str":"900000000000000213","type":"DYNAMIC_TYPE_WORD","modules":{"module_author":{"mid":42,"pub_ts":1794020000,"name":7}}}`:                           "900000000000000213",
		`{"id_str":"900000000000000213","type":"DYNAMIC_TYPE_DRAW","modules":{"module_dynamic":{"major":{"type":"MAJOR_TYPE_OPUS","opus":{"pics":{"url":"x"}}}}}}`: "900000000000000213",
		`{"id_str":900000000000000213,"type":"DYNAMIC_TYPE_WORD","modules":{}}`:                                                                                    "",
		`{"id_str":"900000000000000213","modules":{"module_dynamic":[]}}`:                                                                                          "900000000000000213",
		`"deleted"`: "",
	}
	for orig, id := range origs {
		feed := `{"items":[{"id_str":"900000000000000113","type":"DYNAMIC_TYPE_FORWARD","modules":{"module_author":{"mid":316381099,"pub_ts":1794030000},"module_dynamic":{"desc":{"rich_text_nodes":[{"type":"RICH_TEXT_NODE_TYPE_TEXT","text":"转发"}]}}},"orig":` + orig + `},
		 {"id_str":"900000000000000114","type":"DYNAMIC_TYPE_WORD","modules":{"module_author":{"mid":316381099,"pub_ts":1794030000},"module_dynamic":{"desc":{"rich_text_nodes":[{"type":"RICH_TEXT_NODE_TYPE_TEXT","text":"另一条"}]}}}}]}`
		page, err := ParseSpaceFeed(json.RawMessage(feed), DefaultAccount)
		if err != nil || len(page.Dynamics) != 2 {
			t.Fatalf("%s: page=%#v err=%v", orig, page, err)
		}
		d := page.Dynamics[0]
		want := &Quote{URL: d.URL(), Unavailable: true}
		if id != "" {
			want = &Quote{ID: PostPrefix + id, URL: "https://www.bilibili.com/opus/" + id, Unavailable: true}
		}
		if d.Text != "转发" || fmt.Sprint(*d.Quote) != fmt.Sprint(*want) {
			t.Fatalf("%s: forward=%#v quote=%#v", orig, d, d.Quote)
		}
	}
}

// An upload the listing can no longer show keeps the opus page, so the
// board shows the unavailable card rather than dropping it.
func TestAnUnavailableUploadQuoteLinksToItsOpusPage(t *testing.T) {
	feed := `{"items":[{"id_str":"900000000000000112","type":"DYNAMIC_TYPE_FORWARD","modules":{"module_author":{"mid":316381099,"pub_ts":1794030000},"module_dynamic":{"desc":{"rich_text_nodes":[{"type":"RICH_TEXT_NODE_TYPE_TEXT","text":"投稿"}]}}},
	 "orig":{"id_str":"900000000000000212","type":"DYNAMIC_TYPE_AV","modules":{"module_author":{"mid":42,"pub_ts":1794020000},"module_dynamic":{"major":{"type":"MAJOR_TYPE_ARCHIVE","archive":{"bvid":"BV1AAAAAAAAA","title":"","cover":"https://images.invalid/cover.jpg"}}}}}}]}`
	page, err := ParseSpaceFeed(json.RawMessage(feed), DefaultAccount)
	if err != nil || len(page.Dynamics) != 1 {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	if q := page.Dynamics[0].Quote; !q.Unavailable || q.ID != "bilibili-900000000000000212" || q.URL != "https://www.bilibili.com/opus/900000000000000212" {
		t.Fatalf("quote=%#v", q)
	}
}

// An old-form picture dynamic keeps its wording and its pictures.
func TestDrawDynamicsKeepTheirWordingAndPictures(t *testing.T) {
	feed := `{"items":[{"id_str":"900000000000000120","type":"DYNAMIC_TYPE_DRAW","modules":{"module_author":{"mid":316381099,"pub_ts":1794030000},"module_dynamic":{
	 "desc":{"text":"x","rich_text_nodes":[{"type":"RICH_TEXT_NODE_TYPE_TEXT","text":"今晚的照片"},{"type":"RICH_TEXT_NODE_TYPE_EMOJI","text":"[e]"},{"type":"RICH_TEXT_NODE_TYPE_TOPIC","text":"#话题#"}]},
	 "major":{"type":"MAJOR_TYPE_DRAW","draw":{"items":[{"src":"https://i0.hdslb.com/bfs/new_dyn/synthetic-e.jpg"},{"src":"https://i0.hdslb.com/bfs/new_dyn/synthetic-f.jpg"}]}}}}}]}`
	page, err := ParseSpaceFeed(json.RawMessage(feed), DefaultAccount)
	if err != nil || len(page.Dynamics) != 1 {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	d := page.Dynamics[0]
	if d.Text != "今晚的照片#话题#" || len(d.Images) != 2 || d.Images[1] != "https://i0.hdslb.com/bfs/new_dyn/synthetic-f.jpg" || d.Quote != nil {
		t.Fatalf("draw=%#v", d)
	}
}
