package bilibili

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// dynamicsPage is one space feed page: an opus with two pictures, an upload
// published as a video dynamic, a forward of an opus, an entry of another
// account, a repeated ID and an empty entry.
const dynamicsPage = `{"items":[
 {"id_str":"1100000000000000001","type":"DYNAMIC_TYPE_DRAW","modules":{
   "module_author":{"pub_ts":1767258000,"name":"鹿乃","mid":316381099},
   "module_dynamic":{"desc":{"text":"今天的直播 &amp; 歌单"},"major":{"opus":{
     "title":null,"summary":{"text":"今週のスケジュールです"},
     "pics":[{"url":"http://i0.hdslb.com/bfs/new_dyn/one.jpg"},{"url":"https://i1.hdslb.com/bfs/new_dyn/two.jpg"}]}}}}},
 {"id_str":"1100000000000000002","type":"DYNAMIC_TYPE_AV","modules":{
   "module_author":{"pub_ts":1767258000,"name":"鹿乃","mid":316381099},
   "module_dynamic":{"major":{"archive":{"bvid":"BV1AAAAAAAAA","title":"翻唱"}}}}},
 {"id_str":"1100000000000000003","type":"DYNAMIC_TYPE_FORWARD","modules":{
   "module_author":{"pub_ts":1767258000,"name":"鹿乃","mid":316381099},
   "module_dynamic":{"desc":{"text":"转 发 &amp; 提醒"}}},
  "orig":{"id_str":"1100000000000000009","type":"DYNAMIC_TYPE_DRAW","modules":{
   "module_author":{"pub_ts":1767171600,"name":"别的 UP","mid":42},
   "module_dynamic":{"desc":{"text":"原动态正文"},"major":{"draw":{"items":[{"src":"https://i2.hdslb.com/bfs/album/orig.jpg"}]}}}}}},
 {"id_str":"1100000000000000004","type":"DYNAMIC_TYPE_WORD","modules":{
   "module_author":{"pub_ts":1767258000,"name":"别的 UP","mid":42},
   "module_dynamic":{"desc":{"text":"别人的动态"}}}},
 {"id_str":"1100000000000000001","type":"DYNAMIC_TYPE_WORD","modules":{
   "module_author":{"pub_ts":1767258000,"name":"鹿乃","mid":316381099},
   "module_dynamic":{"desc":{"text":"重复"}}}},
 {"id_str":"1100000000000000005","type":"DYNAMIC_TYPE_WORD","modules":{
   "module_author":{"pub_ts":1767258000,"name":"鹿乃","mid":316381099},
   "module_dynamic":{"desc":{"text":null}}}}
],"offset":"1100000000000000005","has_more":true}`

func TestParseDynamicsKeepsPostsRepostsAndPictures(t *testing.T) {
	items, err := ParseDynamics([]byte(dynamicsPage), "316381099")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items=%d, want 2: %#v", len(items), items)
	}
	post := items[0]
	if post.ID != "1100000000000000001" || post.Kind != KindDynamicPost || post.URL != "https://t.bilibili.com/1100000000000000001" {
		t.Fatalf("post=%#v", post)
	}
	// The opus summary wins over the desc, and the album keeps its order on
	// HTTPS only.
	if post.Text != "今週のスケジュールです" || len(post.Images) != 2 || post.Images[0] != "https://i0.hdslb.com/bfs/new_dyn/one.jpg" {
		t.Fatalf("text=%q images=%#v", post.Text, post.Images)
	}
	if post.Author != "鹿乃" || post.AuthorMid != 316381099 || post.PublishedAt != time.Unix(1767258000, 0).UTC().Format(time.RFC3339) {
		t.Fatalf("author=%q mid=%d at=%q", post.Author, post.AuthorMid, post.PublishedAt)
	}
	repost := items[1]
	if repost.Kind != KindDynamicRepost || repost.Text != "转 发 & 提醒" || repost.Original == nil {
		t.Fatalf("repost=%#v", repost)
	}
	if repost.Original.ID != "1100000000000000009" || repost.Original.Text != "原动态正文" || len(repost.Original.Images) != 1 {
		t.Fatalf("original=%#v", repost.Original)
	}
}

func TestParseDynamicsRejectsMalformedAnswers(t *testing.T) {
	for _, body := range []string{`{"code":0,"data":{}}`, `{"code":0}`, `not json`} {
		if _, err := ParseDynamics([]byte(body), "316381099"); ErrorCode(err) != "bilibili_invalid_response" {
			t.Fatalf("%s: err=%v", body, err)
		}
	}
}

func TestSignsSortedParametersWithATimestamp(t *testing.T) {
	at := time.Unix(1767258000, 0)
	query := map[string][]string{"host_mid": {"316381099"}, "features": {"itemOpusStyle,opusBigCover"}}
	signed := signWBI(query, "0123456789abcdef0123456789abcdef", at)
	if !strings.HasPrefix(signed, "features=itemOpusStyle%2CopusBigCover&host_mid=316381099&wts=1767258000&w_rid=") {
		t.Fatalf("signed=%s", signed)
	}
	if len(signed) != len("features=itemOpusStyle%2CopusBigCover&host_mid=316381099&wts=1767258000&w_rid=")+32 {
		t.Fatalf("signed=%s", signed)
	}
	// A different key signs differently, and the signing characters are gone.
	stripped := signWBI(params("a!(b)*c"), "0123456789abcdef0123456789abcdef", at)
	if !strings.HasPrefix(stripped, "a=abc&wts=") {
		t.Fatalf("stripped=%s", stripped)
	}
	if signWBI(params("a!(b)*c"), "fedcba9876543210fedcba9876543210", at) == stripped {
		t.Fatal("signature ignores the key")
	}
}

func params(value string) map[string][]string { return map[string][]string{"a": {value}} }

func TestDerivesTheSigningKeyFromTheImageNames(t *testing.T) {
	raw := "7cd084941338484aae1ad9425b84077c" + "4932caff0ff746eab6f01bf08b70ac45"
	if key := deriveMixinKey(raw); len(key) != 32 || key == raw[:32] {
		t.Fatalf("key=%q", key)
	}
	if deriveMixinKey("short") != "" {
		t.Fatal("a short key was accepted")
	}
	if imageStem("https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png") != "7cd084941338484aae1ad9425b84077c" {
		t.Fatal("image stem")
	}
}

func TestDynamicsNeedsTheOperatorsLogin(t *testing.T) {
	requests := 0
	c := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		return response(200, session), nil
	})})
	s := Session{Buvid3: "0A1B2C3D-0A1B-0A1B-0A1B-0A1B2C3D4E5F12345infoc", Buvid4: "synthetic-buvid4=="}
	if _, err := c.Dynamics(context.Background(), "316381099", 20, s); ErrorCode(err) != "bilibili_login_required" {
		t.Fatalf("err=%v", err)
	}
	if requests != 0 {
		t.Fatalf("requests=%d, want none", requests)
	}
}
