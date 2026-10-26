package youtube

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The Data API is optional. With a key, the recent step lists the uploads
// playlist and reads its detail candidates in one videos.list call before it
// requests any watch page; see docs/architecture/sources.md.

type apiThumbnails map[string]struct {
	URL string `json:"url"`
}

// best returns the largest thumbnail on YouTube's image CDN, or "".
func (t apiThumbnails) best() string {
	for _, size := range []string{"maxres", "standard", "high", "medium", "default"} {
		u, err := url.Parse(t[size].URL)
		if err == nil && u.Scheme == "https" && (u.Host == "ytimg.com" || strings.HasSuffix(u.Host, ".ytimg.com")) {
			return u.String()
		}
	}
	return ""
}

// ParseUploads reads a playlistItems.list answer for the uploads playlist.
// Private entries are skipped; each video keeps its publication time (the
// item's own time when the video's is missing) like a feed entry.
func ParseUploads(body []byte) ([]Video, error) {
	var page struct {
		Items *[]struct {
			Snippet struct {
				Title       string        `json:"title"`
				PublishedAt string        `json:"publishedAt"`
				Thumbnails  apiThumbnails `json:"thumbnails"`
			} `json:"snippet"`
			ContentDetails struct {
				VideoID          string `json:"videoId"`
				VideoPublishedAt string `json:"videoPublishedAt"`
			} `json:"contentDetails"`
			Status struct {
				Privacy string `json:"privacyStatus"`
			} `json:"status"`
		} `json:"items"`
	}
	if json.Unmarshal(body, &page) != nil || page.Items == nil {
		return nil, &Error{"youtube_api_invalid_response"}
	}
	out := []Video{}
	seen := map[string]bool{}
	for _, item := range *page.Items {
		id := item.ContentDetails.VideoID
		published := item.ContentDetails.VideoPublishedAt
		if published == "" {
			published = item.Snippet.PublishedAt
		}
		t, err := time.Parse(time.RFC3339Nano, published)
		title := strings.TrimSpace(item.Snippet.Title)
		if !videoID.MatchString(id) || err != nil || seen[id] || item.Status.Privacy == "private" || title == "" {
			continue
		}
		seen[id] = true
		thumbnail := item.Snippet.Thumbnails.best()
		if thumbnail == "" {
			thumbnail = "https://i.ytimg.com/vi/" + id + "/hqdefault.jpg"
		}
		out = append(out, Video{ID: id, Title: title, PublishedAt: t.UTC().Format(time.RFC3339Nano), URL: "https://www.youtube.com/watch?v=" + id, ThumbnailURL: thumbnail, Kind: Classify(title)})
	}
	if len(out) == 0 {
		return nil, &Error{"youtube_api_invalid_response"}
	}
	return out, nil
}

// apiVideo is one videos.list item.
type apiVideo struct {
	ID      string `json:"id"`
	Snippet struct {
		Title       string        `json:"title"`
		PublishedAt string        `json:"publishedAt"`
		Broadcast   string        `json:"liveBroadcastContent"`
		Thumbnails  apiThumbnails `json:"thumbnails"`
	} `json:"snippet"`
	ContentDetails struct {
		Duration string `json:"duration"`
	} `json:"contentDetails"`
	Live *struct {
		ScheduledStart string `json:"scheduledStartTime"`
		ActualStart    string `json:"actualStartTime"`
		ActualEnd      string `json:"actualEndTime"`
	} `json:"liveStreamingDetails"`
}

// ParseAPIVideos reads a videos.list answer by video ID. Videos the API does
// not return, such as private or removed ones, are simply absent.
func ParseAPIVideos(body []byte) (map[string]apiVideo, error) {
	var page struct {
		Items *[]apiVideo `json:"items"`
	}
	if json.Unmarshal(body, &page) != nil || page.Items == nil {
		return nil, &Error{"youtube_api_invalid_response"}
	}
	out := map[string]apiVideo{}
	for _, item := range *page.Items {
		if videoID.MatchString(item.ID) {
			out[item.ID] = item
		}
	}
	return out, nil
}

var isoDuration = regexp.MustCompile(`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`)

// parseDuration reads an ISO 8601 length such as PT4M49S in seconds; zero
// for P0D, an unreadable value or more than a year.
func parseDuration(value string) int {
	m := isoDuration.FindStringSubmatch(value)
	if m == nil {
		return 0
	}
	total := 0
	for i, unit := range []int{86400, 3600, 60, 1} {
		n, _ := strconv.Atoi(m[i+1])
		if n > 366*86400/unit {
			return 0
		}
		total += n * unit
	}
	if total > 366*86400 {
		return 0
	}
	return total
}

// utcTime normalizes an RFC 3339 instant, or returns "".
func utcTime(value string) string {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// fromAPI turns a videos.list item into the same details a watch page gives,
// before completeDetails. ok is false when the item cannot say what the video
// is, and the watch page has to decide instead:
//
//   - The API has no isLiveContent. An upcoming or live premiere already has
//     its file's length, while a stream reports P0D, so the length tells them
//     apart until the broadcast ends.
//   - After that, only the stored verdict of an earlier check tells. The
//     kind cannot: a premiere keeps its title kind, which may be a live one.
func fromAPI(item apiVideo, old Video) (Video, bool) {
	v := Video{ID: item.ID, Title: strings.TrimSpace(item.Snippet.Title), URL: "https://www.youtube.com/watch?v=" + item.ID, PublishedAt: utcTime(item.Snippet.PublishedAt)}
	if v.Title == "" {
		return v, false
	}
	v.ThumbnailURL = item.Snippet.Thumbnails.best()
	// The API names a thumbnail without the watch page's ?v= version, so the
	// same image keeps its stored URL rather than reading as a replacement.
	if SameImage(old.ThumbnailURL, v.ThumbnailURL) {
		v.ThumbnailURL = old.ThumbnailURL
	}
	length := parseDuration(item.ContentDetails.Duration)
	if item.Live == nil {
		v.DurationSeconds = length
		return v, true
	}
	v.ScheduledAt = utcTime(item.Live.ActualStart)
	if v.ScheduledAt == "" {
		v.ScheduledAt = utcTime(item.Live.ScheduledStart)
	}
	v.EndedAt = utcTime(item.Live.ActualEnd)
	switch {
	case item.Snippet.Broadcast == "upcoming":
		v.IsUpcoming, v.LiveStatus = true, LiveStatusUpcoming
		v.Premiere, v.PremiereKnown = length > 0, true
		return v, v.ScheduledAt != ""
	case item.Snippet.Broadcast == "live" && v.EndedAt == "":
		v.LiveNow, v.LiveStatus = true, LiveStatusLive
		v.Premiere, v.PremiereKnown = length > 0, true
		return v, v.ScheduledAt != ""
	case v.EndedAt != "":
		v.LiveStatus = LiveStatusEnded
	}
	v.DurationSeconds = length
	if old.ScheduledAt == "" || !old.PremiereKnown {
		// The watch page decides. Should it fail, an ended broadcast with a
		// start still updates from this answer, unclassified.
		v.Unclassified = v.LiveStatus == LiveStatusEnded && v.ScheduledAt != ""
		return v, false
	}
	v.Premiere, v.PremiereKnown = old.Premiere, true
	return v, true
}

var thumbnailPath = regexp.MustCompile(`^/vi(?:_webp)?/([A-Za-z0-9_-]{11})/(?:default|mqdefault|hqdefault|sddefault|maxresdefault)(_live)?\.(?:jpg|webp)$`)

// SameImage reports whether two thumbnail URLs name the same image on the
// image CDN, whatever the numbered host. The sizes and formats YouTube renders
// of one video's thumbnail are one image, so the Data API's hqdefault and
// the watch page's og:image agree; a live placeholder is another image. A
// version query only tells images apart when both URLs carry one.
func SameImage(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	if errA != nil || errB != nil || a == "" || b == "" {
		return false
	}
	if va, vb := ua.Query().Get("v"), ub.Query().Get("v"); va != "" && vb != "" && va != vb {
		return false
	}
	ma, mb := thumbnailPath.FindStringSubmatch(ua.Path), thumbnailPath.FindStringSubmatch(ub.Path)
	if ma == nil || mb == nil {
		return ua.Path == ub.Path
	}
	return ma[1] == mb[1] && ma[2] == mb[2]
}
