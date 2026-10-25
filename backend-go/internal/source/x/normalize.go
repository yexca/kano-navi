package x

import (
	"fmt"
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
	media := mediaURLs(payload, 0)
	if id == "" || published == "" || (text == "" && len(media) == 0) {
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
	p := &Post{ID: id, AccountHandle: account, Type: "daily", Label: "DAILY / 近况", Text: text, PublishedAt: published, URL: u, Likes: number(first(payload, "likes", "likeCount")), Reposts: number(first(payload, "retweets", "reposts", "retweetCount")), Replies: number(first(payload, "replies", "replyCount")), MediaURLs: media, AuthorScreenName: screen, Raw: payload}
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
func mediaURLs(payload map[string]any, depth int) []string {
	if depth > 2 {
		return nil
	}
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
	if vals, ok := payload["media_extended"].([]any); ok {
		for _, v := range vals {
			if m, ok := v.(map[string]any); ok {
				add(firstString(m, "url", "media_url"))
			}
		}
	}
	for _, u := range fxMediaURLs(object(payload["media"])) {
		add(u)
	}
	for _, k := range []string{"qrt", "quotedTweet", "quoted_tweet", "quote"} {
		if m := object(payload[k]); len(m) > 0 {
			out = append(out, mediaURLs(m, depth+1)...)
			break
		}
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
			out = append(out, stringValue(m["url"]))
		case "video", "gif":
			out = append(out, stringValue(m["thumbnail_url"]))
		}
	}
	return out
}
