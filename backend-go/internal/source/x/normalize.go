package x

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

func Normalize(payload map[string]any, fallbackID, account string) *Post {
	id := stringValue(payload["tweetID"])
	if id == "" {
		id = stringValue(payload["tweetId"])
	}
	if id == "" {
		id = stringValue(payload["id"])
	}
	if id == "" {
		id = fallbackID
	}
	text := stringValue(payload["text"])
	if text == "" {
		text = stringValue(payload["tweetText"])
	}
	if text == "" {
		text = stringValue(payload["description"])
	}
	// FxTwitter's created_at is the legacy Twitter format; its epoch comes first.
	published := parseDate(first(payload, "date_epoch", "dateEpoch", "created_timestamp", "date", "created_at"))
	// vxtwitter lists a video's or GIF's file in mediaURLs; the post keeps its
	// thumbnail, as the timeline does, and raw_json declares the same images.
	if own := extendedMediaURLs(payload); own != nil {
		payload["mediaURLs"] = own
	}
	// A quoted status's images stay out of the post's own media.
	media := mediaURLs(payload)
	quote := QuoteOf(payload)
	if id == "" || published == "" || (text == "" && len(media) == 0 && quote == nil) {
		return nil
	}
	author := object(payload["author"])
	if len(author) == 0 {
		author = object(payload["user"])
	}
	screen := firstString(author, "screenName", "screen_name")
	if screen == "" {
		screen = stringValue(payload["user_screen_name"])
	}
	u := stringValue(payload["tweetURL"])
	if u == "" {
		u = stringValue(payload["url"])
	}
	if u == "" {
		u = "https://x.com/" + account + "/status/" + id
	}
	u = strings.ReplaceAll(u, "twitter.com", "x.com")
	p := &Post{ID: id, AccountHandle: account, Type: "daily", Label: "DAILY / 近况", Text: text, PublishedAt: published, URL: u, Likes: number(first(payload, "likes", "likeCount")), Reposts: number(first(payload, "retweets", "reposts", "retweetCount")), Replies: number(first(payload, "replies", "replyCount")), MediaURLs: media, AuthorScreenName: screen, Quote: quote, Raw: payload}
	if strings.Contains(strings.ToLower(text), "schedule") || strings.Contains(text, "スケジュール") || strings.Contains(text, "配信予定") || strings.Contains(text, "週間予定") || strings.Contains(text, "今週の予定") {
		p.Type = "notice"
		p.Label = "SCHEDULE / 日程"
	} else if strings.Contains(text, "お知らせ") || strings.Contains(strings.ToLower(text), "official") || strings.Contains(text, "発売") || strings.Contains(text, "告知") {
		p.Type = "notice"
		p.Label = "ANNOUNCEMENT"
	}
	return p
}
func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
func first(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}
func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := stringValue(m[k]); s != "" {
			return s
		}
	}
	return ""
}
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func number(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		var i int
		_, _ = fmt.Sscanf(n, "%d", &i)
		return i
	}
	return 0
}

// mediaURLs lists a status's own images, never those of a status it quotes.
func mediaURLs(payload map[string]any) []string {
	out := []string{}
	add := func(v any) {
		if s, ok := v.(string); ok && s != "" {
			for _, x := range out {
				if x == s {
					return
				}
			}
			out = append(out, s)
		}
	}
	// vxtwitter builds mediaURLs from media_extended, which also names a
	// video's thumbnail; the list is read only without it.
	if own := extendedMediaURLs(payload); own != nil {
		for _, v := range own {
			add(v)
		}
	} else {
		for _, k := range []string{"mediaURLs", "media_urls"} {
			if vals, ok := payload[k].([]any); ok {
				for _, v := range vals {
					add(v)
				}
			}
			if s, ok := payload[k].(string); ok {
				add(s)
			}
		}
	}
	for _, u := range fxMediaURLs(object(payload["media"])) {
		add(u)
	}
	uniq := []string{}
	seen := map[string]bool{}
	for _, s := range out {
		if s != "" && !seen[s] {
			seen[s] = true
			uniq = append(uniq, s)
		}
	}
	return uniq
}

// extendedMediaURLs reads vxtwitter's media_extended in post order: an image's
// URL, or a video's or GIF's thumbnail. It is nil without such a list.
func extendedMediaURLs(payload map[string]any) []any {
	items, _ := payload["media_extended"].([]any)
	if len(items) == 0 {
		return nil
	}
	out := []any{}
	for _, item := range items {
		m := object(item)
		u := firstString(m, "url", "media_url")
		if t := stringValue(m["type"]); t == "video" || t == "gif" {
			u = stringValue(m["thumbnail_url"])
		}
		if u != "" {
			out = append(out, u)
		}
	}
	return out
}

// fxMediaURLs reads FxTwitter's media object in post order: a photo's image,
// or a video's or GIF's thumbnail, which is the image the board can show.
func fxMediaURLs(media map[string]any) []string {
	items, _ := media["all"].([]any)
	if items == nil {
		photos, _ := media["photos"].([]any)
		videos, _ := media["videos"].([]any)
		items = append(append([]any{}, photos...), videos...)
	}
	out := []string{}
	for _, item := range items {
		m := object(item)
		switch stringValue(m["type"]) {
		case "photo":
			out = append(out, detailPhotoURL(stringValue(m["url"])))
		case "video", "gif":
			out = append(out, stringValue(m["thumbnail_url"]))
		}
	}
	return out
}

// detailPhotoURL drops the size FxTwitter appends to a photo
// (".../media/<id>.jpg?name=orig"), which vxtwitter's detail answer leaves
// out, so both paths store the same image URL and extraction fingerprint.
func detailPhotoURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host != "pbs.twimg.com" || !strings.HasPrefix(u.Path, "/media/") || path.Ext(u.Path) == "" {
		return raw
	}
	query := u.Query()
	if len(query) != 1 || len(query["name"]) != 1 {
		return raw
	}
	u.RawQuery = ""
	return u.String()
}
