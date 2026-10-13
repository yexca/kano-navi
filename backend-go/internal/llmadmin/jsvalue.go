package llmadmin

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// The admin console was written against a JavaScript server, so request
// fields are coerced the way that server coerced them (String(), Number(),
// truthiness, trim). Values are the result of decoding JSON into `any`.

func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00A0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

func jsTrim(value string) string { return strings.TrimFunc(value, isJSSpace) }

// utf16Len is String.prototype.length.
func utf16Len(value string) int {
	n := 0
	for _, r := range value {
		n++
		if r > 0xFFFF {
			n++
		}
	}
	return n
}

// utf16Prefix is value.slice(0, max) without splitting a surrogate pair.
func utf16Prefix(value string, max int) string {
	n := 0
	for i, r := range value {
		width := 1
		if r > 0xFFFF {
			width = 2
		}
		if n+width > max {
			return value[:i]
		}
		n += width
	}
	return value
}

func jsNumberString(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0:
		return "0"
	}
	abs := math.Abs(f)
	if abs >= 1e21 || abs < 1e-6 {
		text := strconv.FormatFloat(f, 'e', -1, 64)
		mantissa, exponent, _ := strings.Cut(text, "e")
		sign := exponent[:1]
		digits := strings.TrimLeft(exponent[1:], "0")
		return mantissa + "e" + sign + digits
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// jsString is String(value) for a JSON-decoded value.
func jsString(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		return jsNumberString(v)
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			if item != nil {
				parts[i] = jsString(item)
			}
		}
		return strings.Join(parts, ",")
	default:
		return "[object Object]"
	}
}

func jsTruthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case float64:
		return v != 0 && !math.IsNaN(v)
	case string:
		return v != ""
	default:
		return true
	}
}

// stringOrEmpty is String(value ?? "").
func stringOrEmpty(value any) string {
	if value == nil {
		return ""
	}
	return jsString(value)
}

// truthyString is String(value || "").
func truthyString(value any) string {
	if !jsTruthy(value) {
		return ""
	}
	return jsString(value)
}

var jsDecimalPattern = regexp.MustCompile(`^[+-]?(?:\d+\.?\d*(?:[eE][+-]?\d+)?|\.\d+(?:[eE][+-]?\d+)?)$`)

func parseJSNumber(value string) (float64, bool) {
	text := jsTrim(value)
	if text == "" {
		return 0, true
	}
	switch text {
	case "Infinity", "+Infinity":
		return math.Inf(1), true
	case "-Infinity":
		return math.Inf(-1), true
	}
	if len(text) > 2 && text[0] == '0' {
		base := 0
		switch text[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			n, err := strconv.ParseUint(text[2:], base, 64)
			if err != nil {
				return 0, false
			}
			return float64(n), true
		}
	}
	if !jsDecimalPattern.MatchString(text) {
		return 0, false
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil && !math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// jsNumber is Number(value); ok is false for NaN.
func jsNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case nil:
		return 0, true
	case float64:
		return v, !math.IsNaN(v)
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	case string:
		return parseJSNumber(v)
	case []any:
		return parseJSNumber(jsString(v))
	default:
		return 0, false
	}
}

// compactJSON is JSON.stringify for values stored in SQLite: no HTML escaping
// so rows stay byte-compatible with the ones the TypeScript server wrote.
func compactJSON(value any) string {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "[]"
	}
	return strings.TrimSuffix(buffer.String(), "\n")
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
