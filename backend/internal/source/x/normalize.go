package x

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/yexca/kano-navi/backend/internal/urlguard"
)

// Post labels, stored in posts.label and projected as the public `label`.
// Like every value synchronization stores, they are English base text, never
// a localized display string. Bilibili dynamics carry LabelDaily as well.
const (
	LabelDaily        = "DAILY"
	LabelSchedule     = "SCHEDULE"
	LabelAnnouncement = "ANNOUNCEMENT"
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
	items := mediaItems(payload)
	media := itemURLs(items)
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
		u = fallbackPostURL(account, id)
	}
	u = normalizePostURL(u, account, id)
	avatarURL := extractAuthorAvatarURL(author, payload)
	p := &Post{ID: id, AccountHandle: account, Type: "daily", Label: LabelDaily, Text: text, PublishedAt: published, URL: u, Likes: number(first(payload, "likes", "likeCount")), Reposts: number(first(payload, "retweets", "reposts", "retweetCount")), Replies: number(first(payload, "replies", "replyCount")), MediaURLs: media, MediaKinds: itemKinds(items), AuthorScreenName: screen, AuthorAvatarURL: avatarURL, Quote: quote, Raw: payload}
	if strings.Contains(strings.ToLower(text), "schedule") || strings.Contains(text, "スケジュール") || strings.Contains(text, "配信予定") || strings.Contains(text, "週間予定") || strings.Contains(text, "今週の予定") {
		p.Type = "notice"
		p.Label = LabelSchedule
	} else if strings.Contains(text, "お知らせ") || strings.Contains(strings.ToLower(text), "official") || strings.Contains(text, "発売") || strings.Contains(text, "告知") {
		p.Type = "notice"
		p.Label = LabelAnnouncement
	}
	return p
}

// NormalizeAvatarURL normalizes an X profile avatar URL to HTTPS without
// fragments and upgrades Twitter's default "_normal" thumbnail suffix to
// "_400x400" when present.
func NormalizeAvatarURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || urlguard.ContainsCredentials(trimmed) {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return ""
	}
	parsed.Fragment = ""
	if ext := path.Ext(parsed.Path); ext != "" {
		stem := strings.TrimSuffix(parsed.Path, ext)
		if strings.HasSuffix(stem, "_normal") {
			parsed.Path = strings.TrimSuffix(stem, "_normal") + "_400x400" + ext
		}
	}
	return parsed.String()
}

func extractAuthorAvatarURL(author, payload map[string]any) string {
	if raw := firstString(author, "avatar_url", "avatarUrl", "profile_image_url_https", "profile_image_url"); raw != "" {
		return NormalizeAvatarURL(raw)
	}
	if payload != nil {
		if raw := firstString(payload, "user_profile_image_url", "user_profile_image_url_https", "avatar_url"); raw != "" {
			return NormalizeAvatarURL(raw)
		}
	}
	return ""
}

func recordDiscoveredAvatar(avatars map[string]string, handle, rawURL string) {
	if avatars == nil {
		return
	}
	key := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(handle), "@"))
	if key == "" {
		return
	}
	if _, exists := avatars[key]; exists {
		return
	}
	if normalized := NormalizeAvatarURL(rawURL); normalized != "" {
		avatars[key] = normalized
	}
}

func fallbackPostURL(account, id string) string {
	return "https://x.com/" + account + "/status/" + id
}

func normalizePostURL(raw, account, id string) string {
	fallback := fallbackPostURL(account, id)
	raw = strings.ReplaceAll(strings.TrimSpace(raw), "twitter.com", "x.com")
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || urlguard.ContainsCredentials(raw) {
		return fallback
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String()
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

// Media kinds: what one of a status's images shows. A video's or GIF's image
// is its thumbnail; the file itself is never read.
const (
	MediaPhoto = "photo"
	MediaVideo = "video"
	MediaGIF   = "gif"
)

// mediaKind maps a source's media type (vxtwitter image, video or gif,
// FxTwitter photo, video or gif, X API photo, video or animated_gif) to a
// kind, or "" for a type it does not name.
func mediaKind(kind string) string {
	switch strings.ToLower(kind) {
	case "photo", "image":
		return MediaPhoto
	case "video":
		return MediaVideo
	case "gif", "animated_gif":
		return MediaGIF
	}
	return ""
}

// mediaItem is one of a status's images and its kind.
type mediaItem struct{ url, kind string }

func itemURLs(items []mediaItem) []string {
	out := make([]string, 0, len(items))
	for _, m := range items {
		out = append(out, m.url)
	}
	return out
}

func itemKinds(items []mediaItem) []string {
	out := make([]string, 0, len(items))
	for _, m := range items {
		out = append(out, m.kind)
	}
	return out
}

// mediaItems lists a status's own images with their kinds, never those of a
// status it quotes. An image listed twice keeps its first place and the
// first kind a source names; an image no source types is a photo.
func mediaItems(payload map[string]any) []mediaItem {
	out := []mediaItem{}
	index := map[string]int{}
	add := func(u, kind string) {
		if u == "" {
			return
		}
		if i, ok := index[u]; ok {
			if out[i].kind == "" {
				out[i].kind = kind
			}
			return
		}
		index[u] = len(out)
		out = append(out, mediaItem{url: u, kind: kind})
	}
	// vxtwitter builds mediaURLs from media_extended, which also names a
	// video's thumbnail; the list is read only without it, with the kinds a
	// stored payload lists beside it in mediaKinds.
	if own := extendedMedia(payload); own != nil {
		for _, m := range own {
			add(m.url, m.kind)
		}
	} else {
		kinds := list(payload["mediaKinds"])
		for _, k := range []string{"mediaURLs", "media_urls"} {
			if vals, ok := payload[k].([]any); ok {
				for i, v := range vals {
					kind := ""
					if i < len(kinds) {
						kind = mediaKind(stringValue(kinds[i]))
					}
					add(stringValue(v), kind)
				}
			}
			if s, ok := payload[k].(string); ok {
				add(s, "")
			}
		}
	}
	for _, m := range fxMedia(object(payload["media"])) {
		add(m.url, m.kind)
	}
	for i := range out {
		if out[i].kind == "" {
			out[i].kind = MediaPhoto
		}
	}
	return out
}

// mediaURLs lists a status's own images, never those of a status it quotes.
func mediaURLs(payload map[string]any) []string { return itemURLs(mediaItems(payload)) }

// extendedMedia reads vxtwitter's media_extended in post order: an image's
// URL, or a video's or GIF's thumbnail, with its kind. It is nil without
// such a list.
func extendedMedia(payload map[string]any) []mediaItem {
	items, _ := payload["media_extended"].([]any)
	if len(items) == 0 {
		return nil
	}
	out := []mediaItem{}
	for _, item := range items {
		m := object(item)
		kind := mediaKind(stringValue(m["type"]))
		u := firstString(m, "url", "media_url")
		if kind == MediaVideo || kind == MediaGIF {
			u = stringValue(m["thumbnail_url"])
		}
		if u != "" {
			out = append(out, mediaItem{url: u, kind: kind})
		}
	}
	return out
}

// extendedMediaURLs is extendedMedia's URLs, as raw_json.mediaURLs lists
// them; nil without media_extended.
func extendedMediaURLs(payload map[string]any) []any {
	items := extendedMedia(payload)
	if items == nil {
		return nil
	}
	out := []any{}
	for _, m := range items {
		out = append(out, m.url)
	}
	return out
}

// fxMedia reads FxTwitter's media object in post order: a photo's image, or
// a video's or GIF's thumbnail, which is the image the board can show, with
// its kind.
func fxMedia(media map[string]any) []mediaItem {
	items, _ := media["all"].([]any)
	if items == nil {
		photos, _ := media["photos"].([]any)
		videos, _ := media["videos"].([]any)
		items = append(append([]any{}, photos...), videos...)
	}
	out := []mediaItem{}
	for _, item := range items {
		m := object(item)
		switch kind := mediaKind(stringValue(m["type"])); kind {
		case MediaPhoto:
			out = append(out, mediaItem{url: detailPhotoURL(stringValue(m["url"])), kind: kind})
		case MediaVideo, MediaGIF:
			out = append(out, mediaItem{url: stringValue(m["thumbnail_url"]), kind: kind})
		}
	}
	return out
}

// fxMediaURLs is fxMedia's URLs.
func fxMediaURLs(media map[string]any) []string { return itemURLs(fxMedia(media)) }

// OwnMedia lists a stored payload's own images and their kinds as Normalize
// reads them, without those of a status it quotes.
func OwnMedia(payload map[string]any) (urls, kinds []string) {
	items := mediaItems(payload)
	return itemURLs(items), itemKinds(items)
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
