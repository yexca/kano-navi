package mediadownload

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
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
	// NotModified reports a 304 answer to a revalidation; Content is empty.
	NotModified bool
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
	// X media, YouTube thumbnails and Bilibili covers (i0.hdslb.com and its
	// numbered siblings); each needs a real subdomain of the CDN.
	if !(host == "pbs.twimg.com" || strings.HasSuffix(host, ".twimg.com") || strings.HasSuffix(host, ".ytimg.com") || strings.HasSuffix(host, ".hdslb.com")) {
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
	return c.fetch(ctx, raw, "", "")
}

// fetch sends the stored validators so an unchanged file costs a 304.
func (c *Client) fetch(ctx context.Context, raw, etag, lastModified string) (Download, error) {
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
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		if lastModified != "" {
			req.Header.Set("If-Modified-Since", lastModified)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return out, &Error{"media_timeout"}
			}
			return out, &Error{"media_unavailable"}
		}
		if resp.StatusCode == http.StatusNotModified && (etag != "" || lastModified != "") {
			resp.Body.Close()
			return Download{NotModified: true}, nil
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
		if resp.StatusCode == http.StatusTooManyRequests {
			return out, &Error{"media_rate_limited"}
		}
		if resp.StatusCode >= 500 {
			return out, &Error{"media_unavailable"}
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
		return Download{Content: content, MIME: actual, ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified")}, nil
	}
	return out, &Error{"media_redirect_rejected"}
}

// thumbnailPath matches YouTube's fixed-size thumbnail files.
var thumbnailPath = regexp.MustCompile(`^/(vi|vi_webp)/([A-Za-z0-9_-]{11})/(maxresdefault|sddefault|hqdefault)(_live)?\.(jpg|webp)$`)

// thumbnailSizes is YouTube's size order; hqdefault exists for every video,
// while the larger files are generated only for some.
var thumbnailSizes = []string{"maxresdefault", "sddefault", "hqdefault"}

// ThumbnailFallbacks lists the smaller variants of a YouTube thumbnail URL to
// try, in order, when that size was never generated. A live variant ends with
// the plain hqdefault.jpg, which outlives the stream. Other URLs have none.
func ThumbnailFallbacks(raw string) []string {
	u, err := url.Parse(raw)
	if err != nil || !strings.HasSuffix(strings.ToLower(u.Hostname()), ".ytimg.com") {
		return nil
	}
	m := thumbnailPath.FindStringSubmatch(u.Path)
	if m == nil {
		return nil
	}
	dir, id, size, live, ext := m[1], m[2], m[3], m[4], m[5]
	base := "https://" + u.Host + "/"
	out := []string{}
	smaller := false
	for _, candidate := range thumbnailSizes {
		if smaller {
			out = append(out, base+dir+"/"+id+"/"+candidate+live+"."+ext)
		}
		smaller = smaller || candidate == size
	}
	if live != "" {
		out = append(out, base+"vi/"+id+"/hqdefault.jpg")
	}
	return out
}

// fetchAsset requests the asset and, while YouTube reports a size as missing,
// the next smaller one. The bytes stay under the original URL, so a later
// revalidation picks up the larger size once YouTube generates it.
func (c *Client) fetchAsset(ctx context.Context, asset sqlite.PendingMedia) (Download, error) {
	d, err := c.fetch(ctx, asset.SourceURL, asset.ETag, asset.LastModified)
	for _, next := range ThumbnailFallbacks(asset.SourceURL) {
		var e *Error
		if !errors.As(err, &e) || e.Code != "media_missing" {
			break
		}
		d, err = c.fetch(ctx, next, asset.ETag, asset.LastModified)
	}
	return d, err
}

// Measured counts ready files cached before dimension probing whose width and
// height were filled by this run.
type Summary struct{ Attempted, Ready, Unchanged, Missing, Failed, Measured int }

// measureBatch bounds how many older files one media step measures.
const measureBatch = 50

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
		d, err := c.fetchAsset(ctx, asset)
		if err == nil && d.NotModified {
			if err = store.MarkMediaUnchanged(ctx, asset); err != nil {
				return out, err
			}
			out.Attempted++
			out.Unchanged++
			continue
		}
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
	measured, err := store.MeasureMediaDimensions(ctx, measureBatch)
	if err != nil {
		return out, err
	}
	out.Measured = measured
	return out, nil
}
