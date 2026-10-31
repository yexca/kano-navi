package x

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Quote is the sanitized projection of a status a post quotes. It is stored
// beside the quoting post and never becomes a post, a timeline entry or an
// extraction input of its own. Unavailable marks a quoted status the payload
// names but does not carry (deleted, withheld or protected): only its ID and
// URL are known then. MediaURLs are the quoted status's images, which the
// store links as quote-image media and never as schedule candidates.
type Quote struct {
	ID           string   `json:"id"`
	AuthorHandle string   `json:"authorHandle,omitempty"`
	AuthorName   string   `json:"authorName,omitempty"`
	Text         string   `json:"text,omitempty"`
	PublishedAt  string   `json:"publishedAt,omitempty"`
	URL          string   `json:"url"`
	Unavailable  bool     `json:"unavailable,omitempty"`
	MediaURLs    []string `json:"-"`
}

const (
	// maxQuoteText is X's longest post; maxQuoteName its longest display name.
	maxQuoteText  = 25000
	maxQuoteName  = 50
	maxQuoteMedia = 4
)

// quoteKeys name the quoted status: vxtwitter's detail answer calls it qrt,
// FxTwitter's timeline quote; the rest are older spellings.
var quoteKeys = []string{"qrt", "quote", "quotedTweet", "quoted_tweet"}

// QuoteOf reads the status a payload quotes. It is nil when the payload
// quotes none or names it without a usable status ID.
func QuoteOf(payload map[string]any) *Quote {
	for _, k := range quoteKeys {
		if m := object(payload[k]); len(m) > 0 {
			return quoteFrom(m)
		}
	}
	// vxtwitter keeps the URL of a quoted status it could not read.
	if id := statusIDIn(firstString(payload, "qrtURL", "qrt_url")); id != "" {
		return &Quote{ID: id, URL: quoteURL("", id), Unavailable: true}
	}
	return nil
}

func quoteFrom(m map[string]any) *Quote {
	id := firstString(m, "tweetID", "tweetId", "id", "id_str")
	if !statusIDPattern.MatchString(id) {
		id = statusIDIn(firstString(m, "tweetURL", "url"))
	}
	if id == "" {
		return nil
	}
	author := object(m["author"])
	if len(author) == 0 {
		author = object(m["user"])
	}
	handle := strings.TrimPrefix(firstString(author, "screenName", "screen_name"), "@")
	if handle == "" {
		handle = stringValue(m["user_screen_name"])
	}
	if !handlePattern.MatchString(handle) {
		handle = ""
	}
	q := &Quote{ID: id, URL: quoteURL(handle, id)}
	text := strings.TrimSpace(firstString(m, "text", "tweetText"))
	media := mediaURLs(m)
	// FxTwitter types a status it cannot show (a tombstone) as something
	// other than a status; without an author or content there is nothing to
	// show either.
	if kind := stringValue(m["type"]); (kind != "" && kind != "status") || handle == "" || (text == "" && len(media) == 0) {
		q.Unavailable = true
		return q
	}
	name := firstString(author, "name")
	if name == "" {
		name = stringValue(m["user_name"])
	}
	q.AuthorHandle, q.AuthorName, q.Text = handle, clip(strings.TrimSpace(name), maxQuoteName), clip(text, maxQuoteText)
	q.PublishedAt = parseDate(first(m, "date_epoch", "dateEpoch", "created_timestamp", "date", "created_at"))
	q.MediaURLs = media[:min(len(media), maxQuoteMedia)]
	return q
}

// OwnMediaURLs lists a stored payload's own images as Normalize reads them,
// without those of a status it quotes.
func OwnMediaURLs(payload map[string]any) []string { return mediaURLs(payload) }

// quoteURL is the quoted status's x.com address, built from its validated ID
// and handle rather than taken from the payload.
func quoteURL(handle, id string) string {
	if handle == "" {
		handle = "i"
	}
	return "https://x.com/" + handle + "/status/" + id
}

// quotedStatusPattern finds the status ID in a quoted status's URL.
var quotedStatusPattern = regexp.MustCompile(`/status/(\d{10,22})(?:[/?#]|$)`)

func statusIDIn(raw string) string {
	if m := quotedStatusPattern.FindStringSubmatch(raw); m != nil && statusIDPattern.MatchString(m[1]) {
		return m[1]
	}
	return ""
}

func clip(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit])
}
