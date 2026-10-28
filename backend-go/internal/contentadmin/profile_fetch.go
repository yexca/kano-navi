package contentadmin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	fetchTimeout        = 10 * time.Second
	maxFetchRedirects   = 3
	maxProfilePageBytes = 4 * 1024 * 1024
	profileUserAgent    = "kano-status-board/0.1 (+local profile media)"
	defaultXProfilePage = "https://x.com/kano_2525"
	defaultYouTubePage  = "https://www.youtube.com/channel/UCShXNLMXCfstmWKH_q86B8w"
)

// These host lists are an SSRF boundary and are deliberately kept in code.
// The privacy allowlist is not a download permission list.
var (
	profilePageHosts = map[string]bool{
		"x.com": true, "www.x.com": true, "twitter.com": true, "www.twitter.com": true,
		"youtube.com": true, "www.youtube.com": true,
	}
	profileImageHosts = map[string]bool{
		"pbs.twimg.com": true, "abs.twimg.com": true,
		"i.ytimg.com": true, "i1.ytimg.com": true, "i2.ytimg.com": true, "i3.ytimg.com": true, "i4.ytimg.com": true,
		"yt3.ggpht.com": true, "yt3.googleusercontent.com": true, "lh3.googleusercontent.com": true,
	}
	profileImageHostSuffixes = []string{".twimg.com", ".ytimg.com"}
	sensitiveQueryWords      = []string{"password", "secret", "token", "api_key", "apikey", "auth"}
)

// outboundURL applies the rules shared by every destination: HTTPS on the
// default port, no credentials, and an explicitly listed host.
func outboundURL(raw string, hosts map[string]bool, suffixes []string) (string, bool) {
	normalized, parsed := normalizeSourceURL(raw)
	if parsed == nil || parsed.Scheme != "https" || parsed.Port() != "" {
		return "", false
	}
	return normalized, hostAllowed(parsed.Hostname(), hosts, suffixes)
}

// hostAllowed matches an exact host or a listed suffix. A real subdomain
// boundary is required, so the bare parent and lookalike names never match.
func hostAllowed(host string, hosts map[string]bool, suffixes []string) bool {
	host = strings.ToLower(host)
	if hosts[host] {
		return true
	}
	for _, suffix := range suffixes {
		if len(host) > len(suffix) && strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

func profilePageURL(raw string) (string, bool) {
	return outboundURL(raw, profilePageHosts, nil)
}

func profileImageURL(raw string) (string, bool) {
	normalized, ok := outboundURL(raw, profileImageHosts, profileImageHostSuffixes)
	if !ok {
		return "", false
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return "", false
	}
	for key := range parsed.Query() {
		key = strings.ToLower(key)
		for _, word := range sensitiveQueryWords {
			if strings.Contains(key, word) {
				return "", false
			}
		}
	}
	return normalized, true
}

type fetchKind struct {
	label    string // used in the fixed failure messages
	accept   string
	maxBytes int64
	validate func(string) (string, bool)
}

var (
	pageFetch = fetchKind{label: "profile page", accept: "text/html,application/xhtml+xml", maxBytes: maxProfilePageBytes, validate: profilePageURL}
	// No Accept header was sent for images originally; advertising the cacheable
	// formats matches the source media downloader.
	imageFetch = fetchKind{label: "profile image", accept: "image/avif,image/webp,image/png,image/jpeg,image/gif", maxBytes: maxProfileImageBytes, validate: profileImageURL}
)

type fetched struct {
	url         string
	status      int
	contentType string
	body        []byte
}

func (f *fetched) ok() bool { return f.status >= 200 && f.status <= 299 }

func isRedirectStatus(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}
	return false
}

// fetchAllowed performs one bounded GET. A single deadline covers every
// redirect hop, the headers and the complete body; each hop is revalidated
// against the destination allowlist before any request is made.
func (m *Module) fetchAllowed(ctx context.Context, start string, kind fetchKind) (*fetched, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	// Transport errors can embed proxy or resolver detail, so only a fixed
	// description is kept.
	transportFailure := func(err error) error {
		var timeout interface{ Timeout() bool }
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
			return failure("request timed out")
		}
		return failure(kind.label + " request failed")
	}
	current, ok := kind.validate(start)
	if !ok {
		return nil, failure(kind.label + " host is not allowed")
	}
	for redirect := 0; redirect <= maxFetchRedirects; redirect++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			return nil, failure(kind.label + " host is not allowed")
		}
		request.Header.Set("User-Agent", profileUserAgent)
		request.Header.Set("Accept", kind.accept)
		response, err := m.client.Do(request)
		if err != nil {
			return nil, transportFailure(err)
		}
		if isRedirectStatus(response.StatusCode) {
			location := response.Header.Get("Location")
			response.Body.Close()
			if location == "" || redirect == maxFetchRedirects {
				return nil, failure(kind.label + " redirect limit exceeded")
			}
			target, parseErr := request.URL.Parse(location)
			if parseErr != nil {
				return nil, failure(kind.label + " host is not allowed")
			}
			if current, ok = kind.validate(target.String()); !ok {
				return nil, failure(kind.label + " host is not allowed")
			}
			continue
		}
		result := &fetched{url: current, status: response.StatusCode, contentType: response.Header.Get("Content-Type")}
		if !result.ok() {
			response.Body.Close()
			return result, nil
		}
		if response.ContentLength > kind.maxBytes {
			response.Body.Close()
			return nil, failure("response exceeds the size limit")
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, kind.maxBytes+1))
		response.Body.Close()
		if readErr != nil {
			return nil, transportFailure(readErr)
		}
		if int64(len(body)) > kind.maxBytes {
			return nil, failure("response exceeds the size limit")
		}
		result.body = body
		return result, nil
	}
	return nil, failure(kind.label + " redirect limit exceeded")
}

func httpStatusFailure(kind fetchKind, status int) error {
	return failure(kind.label + " returned HTTP " + strconv.Itoa(status))
}

var (
	metaImagePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)<meta[^>]+(?:property|name)=["'](?:og:image|twitter:image)["'][^>]+content=["']([^"']+)["']`),
		regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+(?:property|name)=["'](?:og:image|twitter:image)["']`),
	}
	keyedImagePatterns = map[string]*regexp.Regexp{
		"avatar": keyedImagePattern("profile_image_url|profileImageUrl|avatar_url|avatarUrl|channelAvatar"),
		"banner": keyedImagePattern("profile_banner_url|profileBannerUrl|banner_url|bannerUrl|channelBanner"),
	}
)

// decodeEscapedURL undoes the JSON and HTML escaping seen in profile pages,
// in a fixed order.
func decodeEscapedURL(value string) string {
	value = strings.ReplaceAll(value, `\/`, "/")
	value = strings.ReplaceAll(value, `\`+"u0026", "&")
	return strings.ReplaceAll(value, "&amp;", "&")
}

func keyedImagePattern(keys string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(?:` + keys + `)[^\n]{0,120}?[=:]\s*["'](https?:\\?/\\?/[^"']+)["']`)
}

// discoveredImageURLs lists image references found in a profile page, keyed
// script values first and social meta images second, in document order.
func discoveredImageURLs(html, slot string) []string {
	urls := []string{}
	if pattern := keyedImagePatterns[slot]; pattern != nil {
		for _, match := range pattern.FindAllStringSubmatch(html, -1) {
			urls = append(urls, decodeEscapedURL(match[1]))
		}
	}
	for _, pattern := range metaImagePatterns {
		for _, match := range pattern.FindAllStringSubmatch(html, -1) {
			urls = append(urls, decodeEscapedURL(match[1]))
		}
	}
	return urls
}
