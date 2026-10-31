package x

import (
	"context"
	"html"
	"regexp"
	"strings"
)

// APIQuery asks the official X API for one newest-first page of an
// account's own posts, newer than SinceID and older than UntilID when set,
// or published from StartTime. Max is 5 to 100; Token continues a read.
type APIQuery struct {
	Account          string
	SinceID, UntilID string
	StartTime        string
	Max              int
	Token            string
}

// APIPage is one answer: the account's own posts, newest first and mapped
// as the timeline and detail paths store them, the continuation, and Read,
// the posts the answer returned (X bills each one).
type APIPage struct {
	Posts []Post
	Next  string
	Read  int
}

// APIReader reads an account's posts through the official X API. It holds
// the credential; the adapter never sees it.
type APIReader interface {
	AccountPosts(ctx context.Context, query APIQuery) (APIPage, error)
}

// X API error codes the chain records.
const (
	// ErrAPIUnavailable: the chain enables the API but no credential is set.
	ErrAPIUnavailable ErrorCode = "x_api_unavailable"
	// ErrAPIGated: the timeline has not failed often enough yet.
	ErrAPIGated ErrorCode = "x_api_gated"
	// ErrAPIDailyCap: the day's posts are spent; the run is partial.
	ErrAPIDailyCap ErrorCode = "x_api_daily_cap"
	// ErrEmptyListing: an answer without any of the account's own statuses.
	ErrEmptyListing ErrorCode = "x_listing_empty"
)

// mediaLinkPattern is the t.co target of a post's own photo or video, which
// the timeline and detail answers leave out of the text.
var mediaLinkPattern = regexp.MustCompile(`^https?://(?:www\.)?(?:x|twitter)\.com/[^/]+/status/\d+/(?:photo|video)/\d+$`)

// APIPosts maps an X API v2 posts answer (data, includes, meta) onto the
// fields the timeline and detail paths store, so a post read through the API
// keeps its extraction fingerprint when a detail request refreshes it later:
// the full text of a long post with its links expanded and its own media
// links removed, the post's own images (a photo, or a video's or GIF's
// thumbnail) in post order, the quoted status's projection, the counters and
// raw_json.mediaURLs. Reposts are left out, and another author's status
// fails the answer. It returns the posts, newest first, how many posts the answer returned, and
// the continuation.
func APIPosts(payload map[string]any, account string) ([]Post, int, string, error) {
	data, isList := payload["data"].([]any)
	meta := object(payload["meta"])
	if (payload["data"] != nil && !isList) || meta == nil {
		return nil, 0, "", &Error{Code: ErrInvalidResponse}
	}
	includes := object(payload["includes"])
	users := map[string]map[string]any{}
	for _, item := range list(includes["users"]) {
		if user := object(item); stringValue(user["id"]) != "" {
			users[stringValue(user["id"])] = user
		}
	}
	media := map[string]map[string]any{}
	for _, item := range list(includes["media"]) {
		if m := object(item); stringValue(m["media_key"]) != "" {
			media[stringValue(m["media_key"])] = m
		}
	}
	referenced := map[string]map[string]any{}
	for _, key := range []string{"tweets", "posts"} {
		for _, item := range list(includes[key]) {
			if status := object(item); stringValue(status["id"]) != "" {
				referenced[stringValue(status["id"])] = status
			}
		}
	}
	read := len(data) + len(referenced)
	posts := []Post{}
	for _, item := range data {
		entry := object(item)
		id := stringValue(entry["id"])
		author := users[stringValue(entry["author_id"])]
		handle := stringValue(author["username"])
		// The read asks for the account's own posts; another author's is an
		// answer the board cannot trust.
		if handle != "" && !strings.EqualFold(handle, account) {
			return nil, read, "", &Error{Code: ErrInvalidResponse}
		}
		if !statusIDPattern.MatchString(id) || referenceOf(entry, "retweeted") != "" {
			continue
		}
		if handle == "" {
			handle = account
		}
		metrics := object(entry["public_metrics"])
		own := []any{}
		for _, u := range apiMedia(entry, media) {
			own = append(own, u)
		}
		raw := map[string]any{
			"id": id, "text": apiText(entry), "created_at": entry["created_at"],
			"url":       "https://x.com/" + handle + "/status/" + id,
			"author":    map[string]any{"screen_name": handle, "name": author["name"]},
			"mediaURLs": own,
			"likes":     metrics["like_count"], "retweets": first(metrics, "retweet_count", "repost_count"), "replies": metrics["reply_count"],
		}
		if quoted := referenceOf(entry, "quoted"); quoted != "" {
			if status, found := referenced[quoted]; found {
				raw["quote"] = apiQuote(status, users, media)
			} else {
				// Like vxtwitter's qrtURL without qrt: named but not carried.
				raw["qrtURL"] = "https://x.com/i/status/" + quoted
			}
		}
		post := Normalize(raw, id, account)
		if post == nil || post.ID != id {
			return nil, read, "", &Error{Code: ErrInvalidResponse}
		}
		posts = append(posts, *post)
	}
	return posts, read, stringValue(meta["next_token"]), nil
}

// apiQuote is a referenced status in the shape QuoteOf reads.
func apiQuote(status map[string]any, users, media map[string]map[string]any) map[string]any {
	author := users[stringValue(status["author_id"])]
	own := []any{}
	for _, u := range apiMedia(status, media) {
		own = append(own, u)
	}
	return map[string]any{
		"id": status["id"], "text": apiText(status), "created_at": status["created_at"],
		"author":    map[string]any{"screen_name": author["username"], "name": author["name"]},
		"mediaURLs": own,
	}
}

// referenceOf is the ID of the status a post references with kind
// (quoted, retweeted, replied_to), or "".
func referenceOf(entry map[string]any, kind string) string {
	for _, key := range []string{"referenced_tweets", "referenced_posts"} {
		for _, item := range list(entry[key]) {
			if ref := object(item); stringValue(ref["type"]) == kind && statusIDPattern.MatchString(stringValue(ref["id"])) {
				return stringValue(ref["id"])
			}
		}
	}
	return ""
}

// apiText is a post's full text as the timeline and detail answers give it:
// a long post's note text, short links expanded, the post's own media links
// removed and HTML entities decoded.
func apiText(entry map[string]any) string {
	text, entities := stringValue(entry["text"]), object(entry["entities"])
	for _, key := range []string{"note_tweet", "note_post"} {
		if note := object(entry[key]); stringValue(note["text"]) != "" {
			text, entities = stringValue(note["text"]), object(note["entities"])
			break
		}
	}
	for _, item := range list(entities["urls"]) {
		link := object(item)
		short, replacement := stringValue(link["url"]), stringValue(link["expanded_url"])
		switch {
		case short == "":
			continue
		case stringValue(link["media_key"]) != "" || mediaLinkPattern.MatchString(replacement):
			replacement = ""
		case replacement == "":
			continue
		}
		text = strings.ReplaceAll(text, short, replacement)
	}
	return strings.TrimSpace(html.UnescapeString(text))
}

// apiMedia lists a post's own images in attachment order: a photo's URL, or
// a video's or GIF's thumbnail.
func apiMedia(entry map[string]any, media map[string]map[string]any) []string {
	out := []string{}
	for _, key := range list(object(entry["attachments"])["media_keys"]) {
		// Only a photo has url; a video or GIF has its preview_image_url.
		m := media[stringValue(key)]
		u := detailPhotoURL(stringValue(m["url"]))
		if u == "" {
			u = stringValue(m["preview_image_url"])
		}
		if u != "" {
			out = append(out, u)
		}
	}
	return out
}

func list(v any) []any { l, _ := v.([]any); return l }
