package schedule

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The TypeScript extractor hashes and stores values produced by
// JSON.stringify, String(), slice() and trim(). The helpers in this file
// reproduce those JavaScript semantics so fingerprints and stored rows stay
// byte-compatible with what the original service wrote.
//
// JSON values are modelled as: nil (null), bool, float64, string, []any and
// *object (insertion ordered). A missing property is reported through the
// second return value of get, which stands in for JavaScript's undefined.

type member struct {
	key   string
	value any
}

type object struct {
	members []member
	index   map[string]int
}

func newObject() *object { return &object{index: map[string]int{}} }

// obj builds an ordered object from alternating key/value arguments.
func obj(pairs ...any) *object {
	o := newObject()
	for i := 0; i+1 < len(pairs); i += 2 {
		o.set(pairs[i].(string), pairs[i+1])
	}
	return o
}

// set keeps the first position of a repeated key, as JavaScript does.
func (o *object) set(key string, value any) {
	if i, ok := o.index[key]; ok {
		o.members[i].value = value
		return
	}
	o.index[key] = len(o.members)
	o.members = append(o.members, member{key, value})
}

func (o *object) get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	i, ok := o.index[key]
	if !ok {
		return nil, false
	}
	return o.members[i].value, true
}

// arrayIndex reports whether a property name is a canonical array index.
// JavaScript enumerates those before the other keys, in numeric order.
func arrayIndex(key string) (uint64, bool) {
	if key == "" || len(key) > 10 || (len(key) > 1 && key[0] == '0') {
		return 0, false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < '0' || key[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(key, 10, 64)
	if err != nil || n >= 4294967295 {
		return 0, false
	}
	return n, true
}

func (o *object) ordered() []member {
	numeric := false
	for _, m := range o.members {
		if _, ok := arrayIndex(m.key); ok {
			numeric = true
			break
		}
	}
	if !numeric {
		return o.members
	}
	type indexed struct {
		n uint64
		m member
	}
	var first []indexed
	var rest []member
	for _, m := range o.members {
		if n, ok := arrayIndex(m.key); ok {
			first = append(first, indexed{n, m})
		} else {
			rest = append(rest, m)
		}
	}
	sort.SliceStable(first, func(i, j int) bool { return first[i].n < first[j].n })
	out := make([]member, 0, len(o.members))
	for _, item := range first {
		out = append(out, item.m)
	}
	return append(out, rest...)
}

// rawJSON is emitted verbatim by stringify. It carries the fixed schemas.
type rawJSON string

var errInvalidJSON = errors.New("invalid JSON")

// parseJSON mirrors JSON.parse for well-formed UTF-8 input.
func parseJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	value, err := decodeJSONValue(dec)
	if err != nil {
		return nil, errInvalidJSON
	}
	if _, err = dec.Token(); err != io.EOF {
		return nil, errInvalidJSON
	}
	return value, nil
}

func decodeJSONValue(dec *json.Decoder) (any, error) {
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			out := newObject()
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, errInvalidJSON
				}
				item, err := decodeJSONValue(dec)
				if err != nil {
					return nil, err
				}
				out.set(key, item)
			}
			if _, err = dec.Token(); err != nil {
				return nil, err
			}
			return out, nil
		case '[':
			out := []any{}
			for dec.More() {
				item, err := decodeJSONValue(dec)
				if err != nil {
					return nil, err
				}
				out = append(out, item)
			}
			if _, err = dec.Token(); err != nil {
				return nil, err
			}
			return out, nil
		}
		return nil, errInvalidJSON
	case json.Number:
		number, err := strconv.ParseFloat(string(value), 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return nil, errInvalidJSON
		}
		return number, nil
	case string, bool, nil:
		return value, nil
	}
	return nil, errInvalidJSON
}

// stringify mirrors JSON.stringify without indentation.
func stringify(value any) string {
	return string(appendJSON(nil, value))
}

func appendJSON(b []byte, value any) []byte {
	switch v := value.(type) {
	case nil:
		return append(b, "null"...)
	case bool:
		if v {
			return append(b, "true"...)
		}
		return append(b, "false"...)
	case string:
		return appendQuoted(b, v)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return append(b, "null"...)
		}
		return append(b, formatNumber(v)...)
	case int:
		return strconv.AppendInt(b, int64(v), 10)
	case int64:
		return strconv.AppendInt(b, v, 10)
	case rawJSON:
		return append(b, v...)
	case *string:
		if v == nil {
			return append(b, "null"...)
		}
		return appendQuoted(b, *v)
	case []string:
		b = append(b, '[')
		for i, item := range v {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendQuoted(b, item)
		}
		return append(b, ']')
	case []any:
		b = append(b, '[')
		for i, item := range v {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendJSON(b, item)
		}
		return append(b, ']')
	case *object:
		if v == nil {
			return append(b, "null"...)
		}
		b = append(b, '{')
		for i, m := range v.ordered() {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendQuoted(b, m.key)
			b = append(b, ':')
			b = appendJSON(b, m.value)
		}
		return append(b, '}')
	}
	return append(b, "null"...)
}

const lowerHex = "0123456789abcdef"

// appendQuoted follows ECMAScript QuoteJSONString: only quotes, backslashes
// and C0 controls are escaped. HTML characters and U+2028/9 stay literal.
func appendQuoted(b []byte, s string) []byte {
	b = append(b, '"')
	for _, r := range s {
		switch r {
		case '"':
			b = append(b, '\\', '"')
		case '\\':
			b = append(b, '\\', '\\')
		case '\b':
			b = append(b, '\\', 'b')
		case '\f':
			b = append(b, '\\', 'f')
		case '\n':
			b = append(b, '\\', 'n')
		case '\r':
			b = append(b, '\\', 'r')
		case '\t':
			b = append(b, '\\', 't')
		default:
			if r < 0x20 {
				b = append(b, '\\', 'u', '0', '0', lowerHex[r>>4], lowerHex[r&0xf])
			} else {
				b = utf8.AppendRune(b, r)
			}
		}
	}
	return append(b, '"')
}

// formatNumber mirrors Number.prototype.toString for finite values.
func formatNumber(f float64) string {
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
	if abs >= 1e-6 && abs < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	text := strconv.FormatFloat(f, 'e', -1, 64)
	mantissa, exponent, _ := strings.Cut(text, "e")
	sign := exponent[:1]
	digits := strings.TrimLeft(exponent[1:], "0")
	return mantissa + "e" + sign + digits
}

// jsString mirrors String(value) for JSON values.
func jsString(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		return formatNumber(v)
	case string:
		return v
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			if item != nil {
				parts[i] = jsString(item)
			}
		}
		return strings.Join(parts, ",")
	case *object:
		return "[object Object]"
	}
	return ""
}

func truthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case float64:
		return v != 0 && !math.IsNaN(v)
	case string:
		return v != ""
	}
	return true
}

// stringOr mirrors String(value || fallback).
func stringOr(value any, fallback string) string {
	if truthy(value) {
		return jsString(value)
	}
	return fallback
}

var decimalLiteral = regexp.MustCompile(`^[+-]?(?:\d+\.?\d*(?:[eE][+-]?\d+)?|\.\d+(?:[eE][+-]?\d+)?)$`)

// jsNumber mirrors Number(value); defined=false stands for undefined.
func jsNumber(value any, defined bool) float64 {
	if !defined {
		return math.NaN()
	}
	switch v := value.(type) {
	case nil:
		return 0
	case bool:
		if v {
			return 1
		}
		return 0
	case float64:
		return v
	case string:
		text := jsTrim(v)
		switch text {
		case "":
			return 0
		case "Infinity", "+Infinity":
			return math.Inf(1)
		case "-Infinity":
			return math.Inf(-1)
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
					return math.NaN()
				}
				return float64(n)
			}
		}
		if !decimalLiteral.MatchString(text) {
			return math.NaN()
		}
		n, err := strconv.ParseFloat(text, 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return math.NaN()
		}
		return n
	case []any:
		return jsNumber(jsString(v), true)
	}
	return math.NaN()
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// jsTrim mirrors String.prototype.trim, whose whitespace set differs from
// strings.TrimSpace (U+FEFF is trimmed, U+0085 is not).
func jsTrim(s string) string { return strings.TrimFunc(s, isJSSpace) }

// utf16Length mirrors String.prototype.length.
func utf16Length(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xffff {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// sliceUTF16 mirrors value.slice(0, limit), which counts UTF-16 code units. A
// surrogate pair cut in half becomes U+FFFD, matching how the lone surrogate
// was encoded when Node hashed or stored the string as UTF-8.
func sliceUTF16(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	units := 0
	for i, r := range s {
		width := 1
		if r > 0xffff {
			width = 2
		}
		if units+width > limit {
			if units+1 == limit && width == 2 {
				return s[:i] + "�"
			}
			return s[:i]
		}
		units += width
	}
	return s
}

func isCased(r rune) bool {
	return unicode.IsUpper(r) || unicode.IsLower(r) || unicode.IsTitle(r)
}

func isCaseIgnorable(r rune) bool {
	switch r {
	case '\'', '.', ':', '^', '`', 0x00a8, 0x00ad, 0x00af, 0x00b4, 0x00b7, 0x00b8, 0x2018, 0x2019, 0x2024, 0x2027, 0xfe13, 0xfe52, 0xfe55, 0xff07, 0xff0e, 0xff1a:
		return true
	}
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Lm, unicode.Sk)
}

// jsToLower mirrors String.prototype.toLowerCase, including the two
// SpecialCasing rules that simple per-rune mapping misses.
func jsToLower(s string) string {
	special := false
	for _, r := range s {
		if r == 0x0130 || r == 0x03a3 {
			special = true
			break
		}
	}
	if !special {
		return strings.Map(unicode.ToLower, s)
	}
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		switch r {
		case 0x0130:
			b.WriteString("i̇")
		case 0x03a3:
			before := false
			for j := i - 1; j >= 0; j-- {
				if isCaseIgnorable(runes[j]) {
					continue
				}
				before = isCased(runes[j])
				break
			}
			after := false
			for j := i + 1; j < len(runes); j++ {
				if isCaseIgnorable(runes[j]) {
					continue
				}
				after = isCased(runes[j])
				break
			}
			if before && !after {
				b.WriteRune(0x03c2)
			} else {
				b.WriteRune(0x03c3)
			}
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
