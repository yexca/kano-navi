package youtube

import (
	"encoding/json"
	"encoding/xml"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Video struct {
	ID, Title, PublishedAt, ScheduledAt, URL, ThumbnailURL, Kind string
	IsUpcoming                                                   bool
	FeedOnly                                                     bool
}

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
func ParseFeed(body []byte) ([]Video, error) {
	var feed struct {
		XMLName xml.Name `xml:"feed"`
		Entries []struct {
			ID        string `xml:"videoId"`
			Title     string `xml:"title"`
			Published string `xml:"published"`
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
		out = append(out, Video{ID: entry.ID, Title: title, PublishedAt: t.UTC().Format(time.RFC3339Nano), URL: "https://www.youtube.com/watch?v=" + entry.ID, ThumbnailURL: thumbnail, Kind: Classify(title)})
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
		VideoID  string `json:"videoId"`
		Title    string `json:"title"`
		Upcoming bool   `json:"isUpcoming"`
	} `json:"videoDetails"`
	Playability struct {
		Status string `json:"status"`
	} `json:"playabilityStatus"`
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
	v.ThumbnailURL = meta(text, "og:image")
	return v, nil
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
