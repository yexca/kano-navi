package youtube

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const MaxResponseBytes = 4 * 1024 * 1024

type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }

// DefaultFeedRetryDelays are the waits before the second and third feed
// requests. The public feed returns intermittent 5xx responses that clear
// within seconds, so a short bounded backoff avoids failing the whole step.
// Its 404 answers can last for hours and are not repeated.
var DefaultFeedRetryDelays = []time.Duration{500 * time.Millisecond, 1500 * time.Millisecond}

type Client struct {
	http *http.Client
	// FeedRetryDelays bounds the feed retries; each entry allows one more
	// request after an upstream 5xx or connection failure.
	FeedRetryDelays []time.Duration
}

func NewClient(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{}
	}
	copy := *client
	copy.Timeout = 10 * time.Second
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{http: &copy, FeedRetryDelays: DefaultFeedRetryDelays}
}

// get requests a public www.youtube.com page. Its error codes start with
// prefix: youtube_feed for the RSS feed, youtube for every other page.
func (c *Client) get(ctx context.Context, path string, query url.Values, prefix string) ([]byte, error) {
	u := url.URL{Scheme: "https", Host: "www.youtube.com", Path: path, RawQuery: query.Encode()}
	return c.do(ctx, u, "text/html,application/atom+xml", "", prefix)
}

// do requests one fixed destination. Error codes start with prefix; a 403 is
// reported only for the Data API, where it means a refused key or quota.
func (c *Client) do(ctx context.Context, u url.URL, accept, key, prefix string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, &Error{prefix + "_invalid_request"}
	}
	req.Header.Set("User-Agent", "kano-navi-go/source-sync")
	req.Header.Set("Accept", accept)
	if key != "" {
		req.Header.Set("X-Goog-Api-Key", key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, &Error{prefix + "_timeout"}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &Error{prefix + "_unavailable"}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, &Error{prefix + "_redirect_rejected"}
	}
	if resp.StatusCode == 429 {
		return nil, &Error{prefix + "_rate_limited"}
	}
	if resp.StatusCode == 403 && key != "" {
		return nil, &Error{prefix + "_forbidden"}
	}
	if resp.StatusCode >= 500 {
		return nil, &Error{prefix + "_upstream_5xx"}
	}
	if resp.StatusCode != 200 {
		return nil, &Error{prefix + "_http_error"}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return nil, &Error{prefix + "_body_unavailable"}
	}
	if len(body) > MaxResponseBytes {
		return nil, &Error{prefix + "_response_oversized"}
	}
	return body, nil
}

var videoID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
var channelID = regexp.MustCompile(`^UC[A-Za-z0-9_-]{22}$`)

func (c *Client) Feed(ctx context.Context, channel string) ([]byte, error) {
	if !channelID.MatchString(channel) {
		return nil, &Error{"youtube_invalid_channel"}
	}
	query := url.Values{"channel_id": {channel}}
	for attempt := 0; ; attempt++ {
		body, err := c.get(ctx, "/feeds/videos.xml", query, "youtube_feed")
		if err == nil || attempt >= len(c.FeedRetryDelays) || !transient(err) {
			return body, err
		}
		timer := time.NewTimer(c.FeedRetryDelays[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// transient reports feed failures worth repeating: upstream 5xx and
// connection errors. Rate limits, other 4xx, timeouts, redirects and body
// limits are not.
func transient(err error) bool {
	var e *Error
	return errors.As(err, &e) && (e.Code == "youtube_feed_upstream_5xx" || e.Code == "youtube_feed_unavailable")
}
func (c *Client) Streams(ctx context.Context, channel string) ([]byte, error) {
	if !channelID.MatchString(channel) {
		return nil, &Error{"youtube_invalid_channel"}
	}
	return c.get(ctx, "/channel/"+channel+"/streams", nil, "youtube")
}

// VideosPage is the channel's uploads tab, newest first. It lists IDs
// without publication times and stands in for the feed when that fails.
func (c *Client) VideosPage(ctx context.Context, channel string) ([]byte, error) {
	if !channelID.MatchString(channel) {
		return nil, &Error{"youtube_invalid_channel"}
	}
	return c.get(ctx, "/channel/"+channel+"/videos", nil, "youtube")
}
func (c *Client) Watch(ctx context.Context, id string) ([]byte, error) {
	if !videoID.MatchString(id) {
		return nil, &Error{"youtube_invalid_video"}
	}
	return c.get(ctx, "/watch", url.Values{"v": {id}}, "youtube")
}

// MaxAPIVideos is the most IDs one videos.list call accepts.
const MaxAPIVideos = 50

// PlaylistItems lists the newest entries of the channel's uploads playlist
// ("UU" and the channel ID after "UC") through the YouTube Data API: one
// quota unit. The key travels only in the X-Goog-Api-Key header.
func (c *Client) PlaylistItems(ctx context.Context, key, channel string) ([]byte, error) {
	return c.PlaylistPage(ctx, key, channel, "")
}

var pageToken = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// PlaylistPage lists the uploads playlist page that an earlier answer's
// nextPageToken names, or the newest page for an empty token: one quota unit.
func (c *Client) PlaylistPage(ctx context.Context, key, channel, token string) ([]byte, error) {
	if !channelID.MatchString(channel) {
		return nil, &Error{"youtube_invalid_channel"}
	}
	return c.playlist(ctx, key, "UU"+channel[2:], token)
}

// ShortsPlaylist lists the newest page of the channel's Shorts playlist
// ("UUSH" and the channel ID after "UC"): one quota unit. YouTube does not
// document this playlist, so it only stands in for the feed's Shorts
// verdicts; a channel without one answers 404.
func (c *Client) ShortsPlaylist(ctx context.Context, key, channel string) ([]byte, error) {
	if !channelID.MatchString(channel) {
		return nil, &Error{"youtube_invalid_channel"}
	}
	return c.playlist(ctx, key, "UUSH"+channel[2:], "")
}

// LivePlaylist lists the newest page of the channel's live playlist ("UULV"
// and the channel ID after "UC"), the streams of its Live tab: one quota
// unit. Like the Shorts playlist it is undocumented.
func (c *Client) LivePlaylist(ctx context.Context, key, channel string) ([]byte, error) {
	if !channelID.MatchString(channel) {
		return nil, &Error{"youtube_invalid_channel"}
	}
	return c.playlist(ctx, key, "UULV"+channel[2:], "")
}

// VideosPlaylist lists the newest page of the channel's long-form playlist
// ("UULF" and the channel ID after "UC"), the uploads of its Videos tab,
// premieres included and streams left out: one quota unit. It is
// undocumented too.
func (c *Client) VideosPlaylist(ctx context.Context, key, channel string) ([]byte, error) {
	if !channelID.MatchString(channel) {
		return nil, &Error{"youtube_invalid_channel"}
	}
	return c.playlist(ctx, key, "UULF"+channel[2:], "")
}
func (c *Client) playlist(ctx context.Context, key, playlist, token string) ([]byte, error) {
	query := url.Values{"part": {"snippet,contentDetails,status"}, "playlistId": {playlist}, "maxResults": {"50"}}
	if token != "" {
		if !pageToken.MatchString(token) {
			return nil, &Error{"youtube_api_invalid_response"}
		}
		query.Set("pageToken", token)
	}
	return c.api(ctx, "playlistItems", query, key)
}

// Videos reads up to MaxAPIVideos videos through the Data API's videos.list:
// one quota unit for the whole batch, whatever parts it names. The
// localizations part carries the titles the creator localized.
func (c *Client) Videos(ctx context.Context, key string, ids []string) ([]byte, error) {
	if len(ids) == 0 || len(ids) > MaxAPIVideos {
		return nil, &Error{"youtube_api_invalid_request"}
	}
	for _, id := range ids {
		if !videoID.MatchString(id) {
			return nil, &Error{"youtube_invalid_video"}
		}
	}
	query := url.Values{"part": {"snippet,contentDetails,liveStreamingDetails,localizations"}, "id": {strings.Join(ids, ",")}, "maxResults": {"50"}}
	return c.api(ctx, "videos", query, key)
}
func (c *Client) api(ctx context.Context, resource string, query url.Values, key string) ([]byte, error) {
	if strings.TrimSpace(key) == "" {
		return nil, &Error{"youtube_api_key_missing"}
	}
	u := url.URL{Scheme: "https", Host: "www.googleapis.com", Path: "/youtube/v3/" + resource, RawQuery: query.Encode()}
	return c.do(ctx, u, "application/json", strings.TrimSpace(key), "youtube_api")
}

type Options struct {
	ChannelID                          string
	BootstrapVideos, MaxDetailRequests int
	// APIKey is the optional YOUTUBE_API_KEY. When set, the recent step lists
	// uploads and reads details through the Data API before the public pages.
	APIKey string
}

func OptionsFromEnv() Options {
	channel := strings.TrimSpace(os.Getenv("YOUTUBE_CHANNEL_ID"))
	if channel == "" {
		channel = "UCShXNLMXCfstmWKH_q86B8w"
	}
	bounded := func(key string, fallback, min, max int) int {
		n, err := strconv.Atoi(os.Getenv(key))
		if err != nil || n < min || n > max {
			return fallback
		}
		return n
	}
	return Options{channel, bounded("YOUTUBE_BOOTSTRAP_VIDEOS", 6, 1, 30), bounded("YOUTUBE_MAX_DETAIL_REQUESTS", 12, 1, 50), strings.TrimSpace(os.Getenv("YOUTUBE_API_KEY"))}
}
