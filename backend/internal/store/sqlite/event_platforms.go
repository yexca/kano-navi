package sqlite

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
)

// Event platforms. An entry's announced platforms (events.platforms, a JSON
// array) are where the wording, the source or an operator says it happens;
// events.platforms_inferred is 1 when they came from a source default rather
// than from the wording or an operator. events.platform is the first
// announced platform, or unknown, for readers of the single value.
const (
	PlatformYouTube  = "youtube"
	PlatformBilibili = "bilibili"
	PlatformOther    = "other"
	PlatformUnknown  = "unknown"
)

// EventPlatforms are the announced platform values.
var EventPlatforms = []string{PlatformYouTube, PlatformBilibili, PlatformOther}

// NormalizePlatforms keeps the known values of platforms once each, in their
// announced order, so the first stays the entry's own platform; anything
// else is dropped.
func NormalizePlatforms(platforms []string) []string {
	out := []string{}
	for _, value := range platforms {
		value = strings.ToLower(strings.TrimSpace(value))
		if slices.Contains(EventPlatforms, value) && !slices.Contains(out, value) {
			out = append(out, value)
		}
	}
	return out
}

// PrimaryPlatform is the events.platform value of an announced set.
func PrimaryPlatform(platforms []string) string {
	if len(platforms) == 0 {
		return PlatformUnknown
	}
	return platforms[0]
}

// EncodePlatforms is the events.platforms value of an announced set.
func EncodePlatforms(platforms []string) string {
	encoded, err := json.Marshal(NormalizePlatforms(platforms))
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

// storedPlatforms reads the announced platforms of a row: its array, or, when
// the array is null, youtube for a row whose single platform is youtube (the
// schema upgrade sets only that one on the YouTube writers' events).
func storedPlatforms(platforms, platform sql.NullString) []string {
	if platforms.Valid {
		var values []string
		if json.Unmarshal([]byte(platforms.String), &values) == nil {
			return NormalizePlatforms(values)
		}
	}
	if platform.Valid && platform.String == PlatformYouTube {
		return []string{PlatformYouTube}
	}
	return []string{}
}

// IsBilibiliRoomURL reports a Bilibili live-room link. The room is the same
// for every broadcast, so it is never an entry's identity link.
func IsBilibiliRoomURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "live.bilibili.com" || strings.HasSuffix(host, ".live.bilibili.com")
}

// readPlatforms is the announced platforms of a stored row as readers see
// it. A live-room link as the row's url, unless the entry is a
// collaboration in another room (other), reads as platform bilibili and is
// no identity link: room reports the link so the caller can drop it.
func readPlatforms(platforms, platform sql.NullString, link string) (announced []string, room bool) {
	announced = storedPlatforms(platforms, platform)
	if link == "" || !IsBilibiliRoomURL(link) || slices.Contains(announced, PlatformOther) {
		return announced, false
	}
	return NormalizePlatforms(append(announced, PlatformBilibili)), true
}

// hasPlatform reports whether platform is in platforms.
func hasPlatform(platforms []string, platform string) bool {
	return slices.Contains(platforms, platform)
}

// orderPlatforms puts lead first and keeps the rest in their order.
func orderPlatforms(platforms []string, lead string) []string {
	out := NormalizePlatforms(platforms)
	if index := slices.Index(out, lead); index > 0 {
		out = append([]string{lead}, slices.Delete(out, index, index+1)...)
	}
	return out
}
