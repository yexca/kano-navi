// Package niconico looks up the thumbnail address of a niconico video for the
// Works page's covers. It reads only the public getthumbinfo API, one request
// per video, and never downloads the image itself: the media step does.
package niconico

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// APIHost answers getthumbinfo.
const APIHost = "ext.nicovideo.jp"

// ThumbnailHost serves the thumbnails getthumbinfo names.
const ThumbnailHost = "nicovideo.cdn.nimg.jp"

// MaxResponseBytes bounds one answer; it carries a video's description, tags
// and counters, a few kilobytes.
const MaxResponseBytes = 256 * 1024

// Timeout bounds one lookup.
const Timeout = 10 * time.Second

type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }

// Lookup failure codes. NotFound is final: the video was deleted, made
// private or never existed.
const (
	CodeInvalidVideo  = "niconico_invalid_video"
	CodeNotFound      = "niconico_not_found"
	CodeTimeout       = "niconico_timeout"
	CodeUnavailable   = "niconico_unavailable"
	CodeRateLimited   = "niconico_rate_limited"
	CodeRedirect      = "niconico_redirect_rejected"
	CodeHTTP          = "niconico_http_error"
	CodeOversized     = "niconico_response_oversized"
	CodeInvalidAnswer = "niconico_invalid_response"
)

var (
	videoID = regexp.MustCompile(`^(?:sm|nm|so)[0-9]{1,10}$`)
	// thumbnailPath is a thumbnail of a video's number, with the hash newer
	// videos carry; ParseThumbnail requires the requested video's number.
	thumbnailPath = regexp.MustCompile(`^/thumbnails/([0-9]+)/([0-9]+)(?:\.[0-9]+)?$`)
)

type Client struct{ http *http.Client }

// NewClient copies client (the operator's media proxy route) with the lookup
// timeout and no redirects.
func NewClient(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{}
	}
	copy := *client
	copy.Timeout = Timeout
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{http: &copy}
}

type thumbResponse struct {
	Status string `xml:"status,attr"`
	Thumb  struct {
		VideoID   string `xml:"video_id"`
		Thumbnail string `xml:"thumbnail_url"`
	} `xml:"thumb"`
	Error struct {
		Code string `xml:"code"`
	} `xml:"error"`
}

// Thumbnail returns the address of the video's large thumbnail. Only newer
// videos have the large size; the media step falls back to the plain one.
func (c *Client) Thumbnail(ctx context.Context, id string) (string, error) {
	if !videoID.MatchString(id) {
		return "", &Error{CodeInvalidVideo}
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	u := url.URL{Scheme: "https", Host: APIHost, Path: "/api/getthumbinfo/" + id}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", &Error{CodeInvalidVideo}
	}
	req.Header.Set("User-Agent", "kano-navi-go/works-covers")
	// getthumbinfo's CDN answers 406 to an XML Accept together with the gzip
	// Accept-Encoding Go's transport adds, so the request accepts any type.
	req.Header.Set("Accept", "*/*")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", requestError(ctx, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return "", &Error{CodeRedirect}
	case resp.StatusCode == http.StatusTooManyRequests:
		return "", &Error{CodeRateLimited}
	case resp.StatusCode >= 500:
		return "", &Error{CodeUnavailable}
	case resp.StatusCode != http.StatusOK:
		return "", &Error{CodeHTTP}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return "", requestError(ctx, err)
	}
	if len(body) > MaxResponseBytes {
		return "", &Error{CodeOversized}
	}
	return ParseThumbnail(body, id)
}

// requestError classifies a failed request or body read: a deadline, before
// or after the headers, is a timeout, and a cancelled lookup returns the
// context's error.
func requestError(ctx context.Context, err error) error {
	var netErr net.Error
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &netErr) && netErr.Timeout() {
		return &Error{CodeTimeout}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return &Error{CodeUnavailable}
}

// ParseThumbnail reads a getthumbinfo answer for id.
func ParseThumbnail(body []byte, id string) (string, error) {
	if !videoID.MatchString(id) {
		return "", &Error{CodeInvalidVideo}
	}
	var out thumbResponse
	if xml.Unmarshal(body, &out) != nil {
		return "", &Error{CodeInvalidAnswer}
	}
	switch out.Status {
	case "ok":
	case "fail":
		switch strings.TrimSpace(out.Error.Code) {
		case "DELETED", "NOT_FOUND", "COMMUNITY":
			return "", &Error{CodeNotFound}
		}
		return "", &Error{CodeUnavailable}
	default:
		return "", &Error{CodeInvalidAnswer}
	}
	if strings.TrimSpace(out.Thumb.VideoID) != id {
		return "", &Error{CodeInvalidAnswer}
	}
	thumb, err := url.Parse(strings.TrimSpace(out.Thumb.Thumbnail))
	if err != nil || thumb.Scheme != "https" || strings.ToLower(thumb.Host) != ThumbnailHost || thumb.RawQuery != "" || thumb.User != nil {
		return "", &Error{CodeInvalidAnswer}
	}
	// Both numbers in the path are the requested video's.
	m := thumbnailPath.FindStringSubmatch(thumb.Path)
	if number := id[2:]; m == nil || m[1] != number || m[2] != number {
		return "", &Error{CodeInvalidAnswer}
	}
	return "https://" + ThumbnailHost + thumb.Path + ".L", nil
}

// ErrorCode is the fixed code of a lookup error.
func ErrorCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return CodeUnavailable
}
