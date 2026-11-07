package bilibili

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The dynamics API answers only with a signed query (Bilibili's wbi scheme):
// the caller hashes its parameters together with a key derived from two image
// names that /x/web-interface/nav publishes, and sends the hash as w_rid.
// Bilibili rotates those images a few times a day, so the key is cached per
// client and fetched again once it is stale.

// mixinKeyEncTab permutes the concatenated image names into the signing key.
var mixinKeyEncTab = []int{
	46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35, 27, 43, 5,
	49, 33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13, 37, 48, 7, 16, 24, 55,
	40, 61, 26, 17, 0, 1, 60, 51, 30, 4, 22, 25, 54, 21, 56, 59, 6, 63, 57,
	62, 11, 36, 20, 34, 44, 52,
}

// wbiKeyTTL is how long a fetched signing key is reused.
const wbiKeyTTL = 6 * time.Hour

type wbiKey struct {
	mixin string
	at    time.Time
}

// mixinKey returns the signing key, fetching it when none is cached or the
// cached one is stale.
func (c *Client) mixinKey(ctx context.Context) (string, error) {
	now := time.Now()
	c.mu.Lock()
	key := c.wbi
	c.mu.Unlock()
	if key.mixin != "" && now.Sub(key.at) < wbiKeyTTL {
		return key.mixin, nil
	}
	data, err := c.get(ctx, "/x/web-interface/nav", nil, "https://www.bilibili.com/", nil)
	if err != nil {
		return "", err
	}
	var nav struct {
		WbiImg *struct {
			ImgURL string `json:"img_url"`
			SubURL string `json:"sub_url"`
		} `json:"wbi_img"`
	}
	if json.Unmarshal(data, &nav) != nil || nav.WbiImg == nil {
		return "", &Error{"bilibili_invalid_response"}
	}
	img, sub := imageStem(nav.WbiImg.ImgURL), imageStem(nav.WbiImg.SubURL)
	if img == "" || sub == "" {
		return "", &Error{"bilibili_invalid_response"}
	}
	mixin := deriveMixinKey(img + sub)
	if mixin == "" {
		return "", &Error{"bilibili_invalid_response"}
	}
	c.mu.Lock()
	c.wbi = wbiKey{mixin: mixin, at: now}
	c.mu.Unlock()
	return mixin, nil
}

// forgetMixinKey drops the cached key, so a refused signature asks for a new
// one instead of repeating the same mismatch for hours.
func (c *Client) forgetMixinKey() {
	c.mu.Lock()
	c.wbi = wbiKey{}
	c.mu.Unlock()
}

// keysStale reports whether the next signed read has to fetch a signing key
// first, which the caller counts as an extra request.
func (c *Client) keysStale() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wbi.mixin == "" || time.Since(c.wbi.at) >= wbiKeyTTL
}

// imageStem is the name part of a wbi image URL, such as 7cd084941338484a...
// from https://i0.hdslb.com/bfs/wbi/7cd084941338484a....png.
func imageStem(raw string) string {
	stem := raw
	if i := strings.LastIndex(raw, "/"); i >= 0 {
		stem = raw[i+1:]
	}
	if i := strings.LastIndex(stem, "."); i > 0 {
		stem = stem[:i]
	}
	if len(stem) < 32 {
		return ""
	}
	return stem
}

// deriveMixinKey takes the first 32 characters of the permuted image names.
func deriveMixinKey(raw string) string {
	if len(raw) < 64 {
		return ""
	}
	var b strings.Builder
	for _, i := range mixinKeyEncTab {
		b.WriteByte(raw[i])
	}
	return b.String()[:32]
}

// signWBI returns the signed query as Bilibili expects it: parameters sorted
// by name, their values stripped of characters the signature cannot carry,
// the timestamp included, and w_rid last.
func signWBI(query url.Values, mixinKey string, at time.Time) string {
	params := url.Values{}
	for name, values := range query {
		if len(values) == 0 {
			continue
		}
		params.Set(name, stripSigningCharacters(values[0]))
	}
	params.Set("wts", strconv.FormatInt(at.Unix(), 10))
	// Values.Encode sorts by name, which the signature requires.
	signed := params.Encode()
	sum := md5.Sum([]byte(signed + mixinKey))
	return signed + "&w_rid=" + hex.EncodeToString(sum[:])
}

// stripSigningCharacters removes the punctuation Bilibili's own signer
// removes before hashing.
func stripSigningCharacters(value string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune("!'()*", r) {
			return -1
		}
		return r
	}, value)
}
