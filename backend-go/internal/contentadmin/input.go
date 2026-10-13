package contentadmin

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/yexca/kano-navi/backend-go/internal/httpx"
)

// jsString mirrors JavaScript String(value) for a decoded JSON value closely
// enough for operator text fields. A missing key and null are both "".
func jsString(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	switch trimmed[0] {
	case '"':
		var value string
		if json.Unmarshal(trimmed, &value) != nil {
			return ""
		}
		return value
	case '{':
		return "[object Object]"
	case '[':
		var items []json.RawMessage
		if json.Unmarshal(trimmed, &items) != nil {
			return ""
		}
		parts := make([]string, len(items))
		for i, item := range items {
			parts[i] = jsString(item)
		}
		return strings.Join(parts, ",")
	case 't', 'f':
		return string(trimmed)
	}
	number, err := strconv.ParseFloat(string(trimmed), 64)
	if err != nil {
		return string(trimmed)
	}
	if math.Abs(number) < 1e21 {
		return strconv.FormatFloat(number, 'f', -1, 64)
	}
	return strconv.FormatFloat(number, 'g', -1, 64)
}

// jsTrim removes the code points String.prototype.trim removes.
func jsTrim(value string) string {
	return strings.TrimFunc(value, func(r rune) bool {
		switch r {
		case '\t', '\n', '\v', '\f', '\r', ' ', 0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
			return true
		}
		return r >= 0x2000 && r <= 0x200a
	})
}

// jsLength is String.prototype.length: UTF-16 code units.
func jsLength(value string) int { return len(utf16.Encode([]rune(value))) }

// jsSlice is value.slice(0, limit) in UTF-16 code units.
func jsSlice(value string, limit int) string {
	units := utf16.Encode([]rune(value))
	if len(units) <= limit {
		return value
	}
	return string(utf16.Decode(units[:limit]))
}

// textValue is the admin `text` validator: trimmed, bounded, nil when empty.
func textValue(value, name string, required bool, max int) (*string, error) {
	normalized := jsTrim(value)
	if required && normalized == "" {
		return nil, httpx.Invalid("%s is required", name)
	}
	if jsLength(normalized) > max {
		return nil, httpx.Invalid("%s is too long", name)
	}
	if normalized == "" {
		return nil, nil
	}
	return &normalized, nil
}

func textField(body map[string]json.RawMessage, key string, required bool, max int) (*string, error) {
	return textValue(jsString(body[key]), key, required, max)
}

// stringField returns the value only when it is a JSON string.
func stringField(body map[string]json.RawMessage, key string) (string, bool) {
	var value string
	raw := bytes.TrimSpace(body[key])
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

var isoDatePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// calendarDate accepts a real YYYY-MM-DD calendar date. Years below 100 are
// rejected because Date.UTC remaps them, which the original check refuses.
func calendarDate(value string) (time.Time, bool) {
	if !isoDatePattern.MatchString(value) {
		return time.Time{}, false
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Year() < 100 {
		return time.Time{}, false
	}
	return parsed, true
}

// normalizeSourceURL follows the WHATWG serialization that `new URL()` gives
// for ordinary public URLs: lower-case scheme and host, no default port, "/"
// for an empty path and no fragment. Credentials are rejected outright.
func normalizeSourceURL(value string) (string, *url.URL) {
	trimmed := strings.TrimFunc(value, func(r rune) bool { return r <= ' ' })
	if trimmed == "" || strings.ContainsAny(trimmed, "\t\r\n") {
		return "", nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Opaque != "" || parsed.User != nil {
		return "", nil
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", nil
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return "", nil
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port := parsed.Port(); port != "" && !(scheme == "http" && port == "80") && !(scheme == "https" && port == "443") {
		host += ":" + port
	}
	parsed.Scheme = scheme
	parsed.Host = host
	parsed.Fragment = ""
	parsed.RawFragment = ""
	if parsed.Path == "" {
		parsed.Path = "/"
		parsed.RawPath = ""
	}
	return parsed.String(), parsed
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func safeHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// encodeURIComponent keeps the same unreserved set as the JavaScript builtin.
func encodeURIComponent(value string) string {
	const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.!~*'()"
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if strings.IndexByte(unreserved, c) >= 0 {
			out.WriteByte(c)
			continue
		}
		out.WriteString("%" + strings.ToUpper(hex.EncodeToString([]byte{c})))
	}
	return out.String()
}

func randomHex(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// randomUUID returns an RFC 4122 version 4 identifier.
func randomUUID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	buffer[6] = buffer[6]&0x0f | 0x40
	buffer[8] = buffer[8]&0x3f | 0x80
	encoded := hex.EncodeToString(buffer)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}

func nullString(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	return *value
}

func nullInt(value *int64) any {
	if value == nil || *value < 0 {
		return nil
	}
	return *value
}
