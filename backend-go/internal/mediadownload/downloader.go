package mediadownload

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yexca/kano-navi/backend-go/internal/mediaformat"
	"github.com/yexca/kano-navi/backend-go/internal/store/sqlite"
)

const MaxBytes = 10 * 1024 * 1024

type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }

type Download struct {
	Content                  []byte
	MIME, ETag, LastModified string
}
type Client struct{ http *http.Client }

func NewClient(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{}
	}
	copy := *client
	copy.Timeout = 10 * time.Second
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{&copy}
}
func AllowedURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if !(host == "pbs.twimg.com" || strings.HasSuffix(host, ".twimg.com") || strings.HasSuffix(host, ".ytimg.com")) {
		return false
	}
	for key := range u.Query() {
		key = strings.ToLower(key)
		for _, word := range []string{"password", "secret", "token", "api_key", "apikey", "auth"} {
			if strings.Contains(key, word) {
				return false
			}
		}
	}
	return true
}
func (c *Client) Fetch(ctx context.Context, raw string) (Download, error) {
	out := Download{}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for redirects := 0; redirects <= 3; redirects++ {
		if !AllowedURL(raw) {
			return out, &Error{"media_destination_rejected"}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return out, &Error{"media_destination_rejected"}
		}
		req.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg,image/gif")
		req.Header.Set("User-Agent", "kano-navi-go/media-cache")
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return out, &Error{"media_timeout"}
			}
			return out, &Error{"media_unavailable"}
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			resp.Body.Close()
			location := resp.Header.Get("Location")
			target, e := req.URL.Parse(location)
			if redirects == 3 || location == "" || e != nil {
				return out, &Error{"media_redirect_rejected"}
			}
			raw = target.String()
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode == 404 || resp.StatusCode == 410 {
			return out, &Error{"media_missing"}
		}
		if resp.StatusCode != 200 {
			return out, &Error{"media_http_error"}
		}
		if resp.ContentLength > MaxBytes {
			return out, &Error{"media_response_oversized"}
		}
		content, e := io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
		if e != nil {
			return out, &Error{"media_body_unavailable"}
		}
		if len(content) > MaxBytes {
			return out, &Error{"media_response_oversized"}
		}
		actual := mediaformat.Sniff(content)
		declared, _, e := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if actual == "" || (e == nil && mediaformat.Extension(declared) != "" && actual != declared) {
			return out, &Error{"media_invalid_image"}
		}
		return Download{content, actual, resp.Header.Get("ETag"), resp.Header.Get("Last-Modified")}, nil
	}
	return out, &Error{"media_redirect_rejected"}
}

type Summary struct{ Attempted, Ready, Missing, Failed int }

func (c *Client) DownloadPending(ctx context.Context, store *sqlite.Store, limit int) (Summary, error) {
	out := Summary{}
	assets, err := store.PendingMedia(ctx, limit)
	if err != nil {
		return out, err
	}
	for _, asset := range assets {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		d, err := c.Fetch(ctx, asset.SourceURL)
		status, code := "ready", ""
		if err != nil {
			status = "failed"
			code = "media_unavailable"
			var e *Error
			if errors.As(err, &e) {
				code = e.Code
			}
			if code == "media_missing" {
				status = "missing"
			}
		}
		if err = store.SaveMediaDownload(ctx, asset, d.Content, d.MIME, d.ETag, d.LastModified, status, code); err != nil {
			return out, err
		}
		out.Attempted++
		switch status {
		case "ready":
			out.Ready++
		case "missing":
			out.Missing++
		default:
			out.Failed++
		}
	}
	return out, nil
}
