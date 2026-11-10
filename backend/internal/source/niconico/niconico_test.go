package niconico

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func answer(status, inner string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?><nicovideo_thumb_response status="` + status + `">` + inner + `</nicovideo_thumb_response>`)
}

func TestParseThumbnailAsksForTheLargeSizeOnItsHost(t *testing.T) {
	for name, tc := range map[string]struct {
		body []byte
		want string
		code string
	}{
		"old":       {answer("ok", `<thumb><video_id>sm9</video_id><thumbnail_url>https://nicovideo.cdn.nimg.jp/thumbnails/9/9</thumbnail_url></thumb>`), "https://nicovideo.cdn.nimg.jp/thumbnails/9/9.L", ""},
		"new":       {answer("ok", `<thumb><video_id>sm40</video_id><thumbnail_url>https://nicovideo.cdn.nimg.jp/thumbnails/40/40.123</thumbnail_url></thumb>`), "https://nicovideo.cdn.nimg.jp/thumbnails/40/40.123.L", ""},
		"deleted":   {answer("fail", `<error><code>DELETED</code></error>`), "", CodeNotFound},
		"missing":   {answer("fail", `<error><code>NOT_FOUND</code></error>`), "", CodeNotFound},
		"busy":      {answer("fail", `<error><code>BUSY</code></error>`), "", CodeUnavailable},
		"other one": {answer("ok", `<thumb><video_id>sm9</video_id><thumbnail_url>https://nicovideo.cdn.nimg.jp/thumbnails/8/8</thumbnail_url></thumb>`), "", CodeInvalidAnswer},
		"other id":  {answer("ok", `<thumb><video_id>sm8</video_id><thumbnail_url>https://nicovideo.cdn.nimg.jp/thumbnails/9/9</thumbnail_url></thumb>`), "", CodeInvalidAnswer},
		"elsewhere": {answer("ok", `<thumb><video_id>sm9</video_id><thumbnail_url>https://example.com/thumbnails/9/9</thumbnail_url></thumb>`), "", CodeInvalidAnswer},
		"plain":     {answer("ok", `<thumb><video_id>sm9</video_id><thumbnail_url>http://nicovideo.cdn.nimg.jp/thumbnails/9/9</thumbnail_url></thumb>`), "", CodeInvalidAnswer},
		"query":     {answer("ok", `<thumb><video_id>sm9</video_id><thumbnail_url>https://nicovideo.cdn.nimg.jp/thumbnails/9/9?x=1</thumbnail_url></thumb>`), "", CodeInvalidAnswer},
		"html":      {[]byte("<html>"), "", CodeInvalidAnswer},
	} {
		got, err := ParseThumbnail(tc.body, map[bool]string{true: "sm40", false: "sm9"}[name == "new"])
		if got != tc.want || (tc.code == "") != (err == nil) || (err != nil && ErrorCode(err) != tc.code) {
			t.Fatalf("%s: got %q err %v", name, got, err)
		}
	}
}

func TestThumbnailBoundsTheRequest(t *testing.T) {
	var asked string
	client := NewClient(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		asked = r.URL.String()
		// An XML Accept with gzip makes niconico's CDN answer 406.
		if r.Header.Get("Accept") != "*/*" {
			return &http.Response{StatusCode: 406, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		switch r.URL.Path {
		case "/api/getthumbinfo/sm1":
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://example.com/"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		case "/api/getthumbinfo/sm2":
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", MaxResponseBytes+1)))}, nil
		}
		return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})})
	for id, code := range map[string]string{"sm1": CodeRedirect, "sm2": CodeOversized, "sm3": CodeUnavailable, "watch?v=1": CodeInvalidVideo} {
		asked = ""
		if _, err := client.Thumbnail(context.Background(), id); ErrorCode(err) != code {
			t.Fatalf("%s: err %v", id, err)
		}
		if code != CodeInvalidVideo && asked != "https://"+APIHost+"/api/getthumbinfo/"+id {
			t.Fatalf("%s asked %q", id, asked)
		}
	}
}

// stalledBody sends its first bytes and then waits until the request ends.
type stalledBody struct {
	ctx  context.Context
	sent bool
}

func (b *stalledBody) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		return copy(p, "<a>"), nil
	}
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *stalledBody) Close() error { return nil }

func TestThumbnailBodyStalledPastTheDeadlineIsATimeout(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: &stalledBody{ctx: r.Context()}}, nil
	})})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := client.Thumbnail(ctx, "sm9"); ErrorCode(err) != CodeTimeout {
		t.Fatalf("err %v", err)
	}
}
