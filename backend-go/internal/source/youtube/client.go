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

type Client struct{ http *http.Client }

func NewClient(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{}
	}
	copy := *client
	copy.Timeout = 10 * time.Second
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{http: &copy}
}
func (c *Client) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	u := url.URL{Scheme: "https", Host: "www.youtube.com", Path: path, RawQuery: query.Encode()}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, &Error{"youtube_invalid_request"}
	}
	req.Header.Set("User-Agent", "kano-navi-go/source-sync")
	req.Header.Set("Accept", "text/html,application/atom+xml")
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, &Error{"youtube_timeout"}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &Error{"youtube_unavailable"}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, &Error{"youtube_redirect_rejected"}
	}
	if resp.StatusCode == 429 {
		return nil, &Error{"youtube_rate_limited"}
	}
	if resp.StatusCode != 200 {
		return nil, &Error{"youtube_http_error"}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return nil, &Error{"youtube_body_unavailable"}
	}
	if len(body) > MaxResponseBytes {
		return nil, &Error{"youtube_response_oversized"}
	}
	return body, nil
}

var videoID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
var channelID = regexp.MustCompile(`^UC[A-Za-z0-9_-]{22}$`)

func (c *Client) Feed(ctx context.Context, channel string) ([]byte, error) {
	if !channelID.MatchString(channel) {
		return nil, &Error{"youtube_invalid_channel"}
	}
	return c.get(ctx, "/feeds/videos.xml", url.Values{"channel_id": {channel}})
}
func (c *Client) Streams(ctx context.Context, channel string) ([]byte, error) {
	if !channelID.MatchString(channel) {
		return nil, &Error{"youtube_invalid_channel"}
	}
	return c.get(ctx, "/channel/"+channel+"/streams", nil)
}
func (c *Client) Watch(ctx context.Context, id string) ([]byte, error) {
	if !videoID.MatchString(id) {
		return nil, &Error{"youtube_invalid_video"}
	}
	return c.get(ctx, "/watch", url.Values{"v": {id}})
}

type Options struct {
	ChannelID                          string
	BootstrapVideos, MaxDetailRequests int
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
	return Options{channel, bounded("YOUTUBE_BOOTSTRAP_VIDEOS", 6, 1, 30), bounded("YOUTUBE_MAX_DETAIL_REQUESTS", 12, 1, 50)}
}
