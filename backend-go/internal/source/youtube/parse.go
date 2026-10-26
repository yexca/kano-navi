package youtube

import (
	"encoding/json"
	"encoding/xml"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Video struct {
	ID, Title, PublishedAt, ScheduledAt, URL, ThumbnailURL, Kind string
	// EndedAt is the broadcast end a watch page reports once a stream or
	// premiere finishes; it stays empty while unknown.
	EndedAt    string
	IsUpcoming bool
	// LiveNow is the watch page's own live signal: the player's isLive or the
	// broadcast details' isLiveNow, without a reported end.
	LiveNow bool
	// LiveStatus classifies a checked broadcast as LiveUpcoming, LiveNow or
	// LiveEnded. It stays empty for an ordinary video and for pages that do not
	// say, such as a feed entry that was never checked.
	LiveStatus string
	// Premiere marks a scheduled upload that is not live content.
	// PremiereKnown says whether a check could tell: a watch page naming
	// isLiveContent, or a Data API answer for an upcoming or live broadcast.
	// The store keeps a known verdict for the API answer of an ended
	// broadcast, which cannot tell.
	Premiere, PremiereKnown bool
	FeedOnly bool
	// DurationSeconds is the player's length; zero while unknown, as for a
	// feed entry, a waiting room or a broadcast still on air.
	DurationSeconds int
	// Short is the feed's format signal: true for an entry YouTube links to
	// its /shorts/ page, false for one it links to a watch page, and nil while
	// unknown, as for a watch-page read or an entry naming neither.
	Short *bool
}

// Broadcast states a watch page can report.
const (
	LiveStatusUpcoming = "upcoming"
	LiveStatusLive     = "live"
	LiveStatusEnded    = "ended"
)

func Classify(title string) string {
	for _, rule := range []struct{ pattern, kind string }{
		{`(?i)リズム天国|ゲーム|gameplay`, "GAMEPLAY"}, {`(?i)雑談|talk|おつかれ`, "TALK"},
		{`(?i)初配信|配信|live`, "LIVE ARCHIVE"}, {`(?i)歌枠|song|music|cover|歌唱`, "MUSIC"},
	} {
		if regexp.MustCompile(rule.pattern).MatchString(title) {
			return rule.kind
		}
	}
	return "VIDEO"
}

type feedLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
}

// feedShort reads an entry's alternate link. YouTube links a Short to its
// /shorts/ page and every other upload, stream or premiere to its watch page;
// any other link, or none, leaves the format unknown rather than guessed.
func feedShort(id string, links []feedLink) *bool {
	for _, link := range links {
		// Atom's default relation is alternate.
		if rel := strings.TrimSpace(link.Rel); rel != "" && rel != "alternate" {
			continue
		}
		u, err := url.Parse(strings.TrimSpace(link.Href))
		if err != nil || u.Scheme != "https" || u.Host != "www.youtube.com" && u.Host != "youtube.com" {
			continue
		}
		short := u.Path == "/shorts/"+id
		if short || u.Path == "/watch" && u.Query().Get("v") == id {
			return &short
		}
	}
	return nil
}

func ParseFeed(body []byte) ([]Video, error) {
	var feed struct {
		XMLName xml.Name `xml:"feed"`
		Entries []struct {
			ID        string     `xml:"videoId"`
			Title     string     `xml:"title"`
			Published string     `xml:"published"`
			Links     []feedLink `xml:"link"`
			Group     struct {
				Thumbnail struct {
					URL string `xml:"url,attr"`
				} `xml:"thumbnail"`
			} `xml:"group"`
		} `xml:"entry"`
	}
	if xml.Unmarshal(body, &feed) != nil || len(feed.Entries) == 0 {
		return nil, &Error{"youtube_invalid_feed"}
	}
	out := []Video{}
	seen := map[string]bool{}
	for _, entry := range feed.Entries {
		t, err := time.Parse(time.RFC3339Nano, entry.Published)
		if !videoID.MatchString(entry.ID) || err != nil || seen[entry.ID] {
			continue
		}
		seen[entry.ID] = true
		title := html.UnescapeString(strings.TrimSpace(entry.Title))
		if title == "" {
			title = "YouTube 更新"
		}
		thumbnail := entry.Group.Thumbnail.URL
		if thumbnail == "" {
			thumbnail = "https://i.ytimg.com/vi/" + entry.ID + "/hqdefault.jpg"
		}
		out = append(out, Video{ID: entry.ID, Title: title, PublishedAt: t.UTC().Format(time.RFC3339Nano), URL: "https://www.youtube.com/watch?v=" + entry.ID, ThumbnailURL: thumbnail, Kind: Classify(title), Short: feedShort(entry.ID, entry.Links)})
		if len(out) == 50 {
			break
		}
	}
	if len(out) == 0 {
		return nil, &Error{"youtube_invalid_feed"}
	}
	return out, nil
}

var idsPattern = regexp.MustCompile(`(?:watch\?v=|videoId["']?\s*[:=]\s*["']|/live/)([A-Za-z0-9_-]{11})`)

func DiscoverIDs(body []byte) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, m := range idsPattern.FindAllStringSubmatch(strings.ReplaceAll(string(body), `\u002F`, "/"), -1) {
		if !seen[m[1]] {
			out = append(out, m[1])
			seen[m[1]] = true
			if len(out) == 200 {
				break
			}
		}
	}
	return out
}

var playerPattern = regexp.MustCompile(`\bytInitialPlayerResponse(?:["']\])?\s*[:=]\s*\{`)
var encodedPlayerPattern = regexp.MustCompile(`"player_response"\s*:\s*("(?:\\.|[^"\\])*")`)

type playerResponse struct {
	VideoDetails struct {
		VideoID     string `json:"videoId"`
		Title       string `json:"title"`
		Upcoming    bool   `json:"isUpcoming"`
		LiveContent *bool  `json:"isLiveContent"`
		Live        bool   `json:"isLive"`
		// Length is a decimal string; "0" while a broadcast is on air.
		Length string `json:"lengthSeconds"`
	} `json:"videoDetails"`
	Playability struct {
		Status string `json:"status"`
	} `json:"playabilityStatus"`
	Microformat struct {
		Renderer struct {
			Broadcast *broadcastDetails `json:"liveBroadcastDetails"`
			// PublishDate is an RFC 3339 instant, or a bare date on older pages.
			PublishDate string `json:"publishDate"`
		} `json:"playerMicroformatRenderer"`
	} `json:"microformat"`
}

type broadcastDetails struct {
	LiveNow *bool  `json:"isLiveNow"`
	Start   string `json:"startTimestamp"`
	End     string `json:"endTimestamp"`
}

func ParseWatch(body []byte, id string) (Video, error) {
	text := string(body)
	v := Video{ID: id, URL: "https://www.youtube.com/watch?v=" + id}
	var player playerResponse
	found := false
	if m := playerPattern.FindStringIndex(text); m != nil {
		// Decode exactly the balanced JSON object; braces and escapes in strings
		// are handled by encoding/json, not an HTML-wide regular expression.
		if json.NewDecoder(strings.NewReader(text[m[1]-1:])).Decode(&player) != nil {
			return v, &Error{"youtube_watch_details_incomplete"}
		}
		found = true
	} else if m := encodedPlayerPattern.FindStringSubmatch(text); m != nil {
		var decoded string
		if json.Unmarshal([]byte(m[1]), &decoded) != nil || json.Unmarshal([]byte(decoded), &player) != nil {
			return v, &Error{"youtube_watch_details_incomplete"}
		}
		found = true
	}
	if found && player.VideoDetails.VideoID != "" && player.VideoDetails.VideoID != id {
		return v, &Error{"youtube_watch_details_incomplete"}
	}
	v.Title = strings.TrimSpace(player.VideoDetails.Title)
	if v.Title == "" {
		v.Title = meta(text, "og:title")
	}
	if v.Title == "" {
		v.Title = meta(text, "twitter:title")
	}
	raw := jsonField(text, "scheduledStartTime")
	if raw == "" {
		raw = jsonField(text, "startTimestamp")
	}
	if raw != "" {
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			if n, e := strconv.ParseInt(raw, 10, 64); e == nil && n > 0 {
				if n < 1e12 {
					n *= 1000
				}
				t = time.UnixMilli(n)
				err = nil
			}
		}
		if err != nil {
			return v, &Error{"youtube_watch_details_incomplete"}
		}
		v.ScheduledAt = t.UTC().Format(time.RFC3339Nano)
	}
	v.IsUpcoming = player.VideoDetails.Upcoming || (!found && regexp.MustCompile(`"isUpcoming"\s*:\s*true`).MatchString(text))
	if v.Title == "" || (v.IsUpcoming && v.ScheduledAt == "") || (v.ScheduledAt == "" && (!found || player.VideoDetails.VideoID != id || player.Playability.Status != "OK")) {
		return v, &Error{"youtube_watch_details_incomplete"}
	}
	// The end is optional detail read only from the flat broadcast object; an
	// unreadable one leaves it unknown.
	if i := strings.Index(text, `"liveBroadcastDetails"`); i >= 0 && v.ScheduledAt != "" {
		details := text[i:]
		if j := strings.IndexByte(details, '}'); j >= 0 {
			details = details[:j]
		}
		if t, err := time.Parse(time.RFC3339Nano, jsonField(details, "endTimestamp")); err == nil {
			v.EndedAt = t.UTC().Format(time.RFC3339Nano)
		}
	}
	live := player.VideoDetails.LiveContent
	v.PremiereKnown = found && v.ScheduledAt != "" && live != nil
	v.Premiere = v.PremiereKnown && !*live
	liveState(&v, player, found)
	if n, err := strconv.Atoi(player.VideoDetails.Length); err == nil && n > 0 && !v.IsUpcoming && !v.LiveNow {
		v.DurationSeconds = n
	}
	v.ThumbnailURL = meta(text, "og:image")
	v.PublishedAt = utcTime(player.Microformat.Renderer.PublishDate)
	if t, err := time.Parse("2006-01-02", player.Microformat.Renderer.PublishDate); err == nil {
		v.PublishedAt = t.Format(time.RFC3339Nano)
	}
	return v, nil
}

// liveState reads the broadcast signals of a page that already passed the
// completeness checks. An unreadable signal leaves the state unknown rather
// than guessing it from the start time.
func liveState(v *Video, player playerResponse, found bool) {
	details := player.Microformat.Renderer.Broadcast
	if !found || details == nil {
		if v.IsUpcoming {
			v.LiveStatus = LiveStatusUpcoming
		}
		return
	}
	if t, err := time.Parse(time.RFC3339Nano, details.End); err == nil {
		v.EndedAt = t.UTC().Format(time.RFC3339Nano)
	}
	now := player.VideoDetails.Live || details.LiveNow != nil && *details.LiveNow
	switch {
	case v.IsUpcoming:
		v.LiveStatus = LiveStatusUpcoming
		return
	case now && v.EndedAt == "":
		v.LiveNow, v.LiveStatus = true, LiveStatusLive
	case v.EndedAt != "" || details.LiveNow != nil:
		// The player answered isLiveNow: false for a broadcast that is
		// neither upcoming nor live, so it is over even without an end time.
		v.LiveStatus = LiveStatusEnded
	default:
		return
	}
	// A started broadcast reports its actual start, which replaces the
	// reservation time.
	if t, err := time.Parse(time.RFC3339Nano, details.Start); err == nil {
		v.ScheduledAt = t.UTC().Format(time.RFC3339Nano)
	}
}
func jsonField(text, field string) string {
	m := regexp.MustCompile(`"` + regexp.QuoteMeta(field) + `"\s*:\s*("(?:\\.|[^"\\])*")`).FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	var value string
	_ = json.Unmarshal([]byte(m[1]), &value)
	return value
}
func meta(text, name string) string {
	for _, tag := range regexp.MustCompile(`(?i)<meta\b[^>]*>`).FindAllString(text, -1) {
		attrs := map[string]string{}
		for _, m := range regexp.MustCompile(`(?i)(property|name|content)\s*=\s*["']([^"']*)["']`).FindAllStringSubmatch(tag, -1) {
			attrs[strings.ToLower(m[1])] = html.UnescapeString(m[2])
		}
		if attrs["property"] == name || attrs["name"] == name {
			return attrs["content"]
		}
	}
	return ""
}
