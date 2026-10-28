package schedule

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
)

// Links are normalized with WHATWG URL semantics before they are hashed or
// stored. net/url serializes several inputs differently, so
// this file implements the subset that matters for http(s) links: scheme and
// host lowercasing, default ports, dot segments and the percent-encode sets.
// Non-ASCII hosts are lowercased but not converted to punycode, and unusual
// IPv4 spellings are not canonicalized.

type parsedURL struct {
	scheme   string
	special  bool // a WHATWG special scheme; other schemes keep only their scheme
	username string
	password string
	host     string
	port     string
	path     string
	query    *string
	fragment *string
}

var schemePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)

// specialSchemes maps the hierarchical WHATWG schemes to their default port.
var specialSchemes = map[string]uint64{"http": 80, "https": 443, "ftp": 21, "ws": 80, "wss": 443}

// web reports whether the URL is a public http(s) link.
func (u *parsedURL) web() bool { return u.scheme == "http" || u.scheme == "https" }

func percentEncode(s string, inSet func(byte) bool) string {
	clean := true
	for i := 0; i < len(s); i++ {
		if c := s[i]; c <= 0x1f || c > 0x7e || inSet(c) {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	const upperHex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c <= 0x1f || c > 0x7e || inSet(c) {
			b.WriteByte('%')
			b.WriteByte(upperHex[c>>4])
			b.WriteByte(upperHex[c&0xf])
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func inQuerySet(c byte) bool {
	return c == ' ' || c == '"' || c == '#' || c == '<' || c == '>' || c == '\''
}

func inFragmentSet(c byte) bool {
	return c == ' ' || c == '"' || c == '<' || c == '>' || c == '`'
}

func inPathSet(c byte) bool {
	return c == ' ' || c == '"' || c == '#' || c == '<' || c == '>' || c == '?' || c == '^' || c == '`' || c == '{' || c == '}'
}

func inUserinfoSet(c byte) bool {
	return inPathSet(c) || c == '/' || c == ':' || c == ';' || c == '=' || c == '@' || c == '[' || c == '\\' || c == ']' || c == '|'
}

func percentDecode(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				b.WriteByte(byte(n))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isSingleDot(segment string) bool {
	return segment == "." || strings.EqualFold(segment, "%2e")
}

func isDoubleDot(segment string) bool {
	switch strings.ToLower(segment) {
	case "..", ".%2e", "%2e.", "%2e%2e":
		return true
	}
	return false
}

func normalizePath(raw string) string {
	raw = strings.ReplaceAll(raw, `\`, "/")
	raw = strings.TrimPrefix(raw, "/")
	parts := strings.Split(raw, "/")
	segments := make([]string, 0, len(parts))
	for i, part := range parts {
		last := i == len(parts)-1
		switch {
		case isDoubleDot(part):
			if len(segments) > 0 {
				segments = segments[:len(segments)-1]
			}
			if last {
				segments = append(segments, "")
			}
		case isSingleDot(part):
			if last {
				segments = append(segments, "")
			}
		default:
			segments = append(segments, percentEncode(part, inPathSet))
		}
	}
	return "/" + strings.Join(segments, "/")
}

// parseURL mirrors `new URL(value)` without a base. ok=false stands for the
// TypeError the constructor throws.
func parseURL(input string) (*parsedURL, bool) {
	input = strings.TrimFunc(input, func(r rune) bool { return r <= 0x20 })
	input = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, input)
	prefix := schemePattern.FindString(input)
	if prefix == "" {
		return nil, false
	}
	u := &parsedURL{scheme: strings.ToLower(prefix[:len(prefix)-1])}
	defaultPort, special := specialSchemes[u.scheme]
	if !special {
		return u, true
	}
	u.special = true
	rest := strings.TrimLeft(input[len(prefix):], `/\`)
	end := strings.IndexAny(rest, `/\?#`)
	authority := rest
	if end >= 0 {
		authority, rest = rest[:end], rest[end:]
	} else {
		rest = ""
	}
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		credentials := authority[:at]
		authority = authority[at+1:]
		name, password, _ := strings.Cut(credentials, ":")
		u.username = percentEncode(name, inUserinfoSet)
		u.password = percentEncode(password, inUserinfoSet)
	}
	host, port := authority, ""
	if strings.HasPrefix(authority, "[") {
		closing := strings.Index(authority, "]")
		if closing < 0 {
			return nil, false
		}
		host, port = authority[:closing+1], authority[closing+1:]
		if port != "" && !strings.HasPrefix(port, ":") {
			return nil, false
		}
		port = strings.TrimPrefix(port, ":")
		host = strings.ToLower(host)
	} else {
		if colon := strings.Index(authority, ":"); colon >= 0 {
			host, port = authority[:colon], authority[colon+1:]
		}
		host = strings.ToLower(percentDecode(host))
		if host == "" {
			return nil, false
		}
		for i := 0; i < len(host); i++ {
			c := host[i]
			if c <= 0x20 || c == 0x7f || strings.IndexByte(`#/:<>?@[\]^|%`, c) >= 0 {
				return nil, false
			}
		}
	}
	if port != "" {
		for i := 0; i < len(port); i++ {
			if port[i] < '0' || port[i] > '9' {
				return nil, false
			}
		}
		number, err := strconv.ParseUint(port, 10, 32)
		if err != nil || number > 65535 {
			return nil, false
		}
		if number == defaultPort {
			port = ""
		} else {
			port = strconv.FormatUint(number, 10)
		}
	}
	u.host, u.port = host, port
	pathEnd := strings.IndexAny(rest, "?#")
	rawPath := rest
	if pathEnd >= 0 {
		rawPath, rest = rest[:pathEnd], rest[pathEnd:]
	} else {
		rest = ""
	}
	u.path = normalizePath(rawPath)
	if strings.HasPrefix(rest, "?") {
		query := rest[1:]
		if hash := strings.Index(query, "#"); hash >= 0 {
			query, rest = query[:hash], query[hash:]
		} else {
			rest = ""
		}
		query = percentEncode(query, inQuerySet)
		u.query = &query
	}
	if strings.HasPrefix(rest, "#") {
		fragment := percentEncode(rest[1:], inFragmentSet)
		u.fragment = &fragment
	}
	return u, true
}

// String mirrors URL.prototype.toString for http(s) URLs.
func (u *parsedURL) String() string {
	var b strings.Builder
	b.WriteString(u.scheme)
	b.WriteString("://")
	if u.username != "" || u.password != "" {
		b.WriteString(u.username)
		if u.password != "" {
			b.WriteByte(':')
			b.WriteString(u.password)
		}
		b.WriteByte('@')
	}
	b.WriteString(u.host)
	if u.port != "" {
		b.WriteByte(':')
		b.WriteString(u.port)
	}
	b.WriteString(u.path)
	if u.query != nil {
		b.WriteByte('?')
		b.WriteString(*u.query)
	}
	if u.fragment != nil {
		b.WriteByte('#')
		b.WriteString(*u.fragment)
	}
	return b.String()
}

// searchParam mirrors url.searchParams.get(name).
func (u *parsedURL) searchParam(name string) (string, bool) {
	if u.query == nil {
		return "", false
	}
	for _, pair := range strings.Split(*u.query, "&") {
		if pair == "" {
			continue
		}
		key, value, _ := strings.Cut(pair, "=")
		if formDecode(key) == name {
			return formDecode(value), true
		}
	}
	return "", false
}

func formDecode(s string) string {
	return strings.ToValidUTF8(percentDecode(strings.ReplaceAll(s, "+", " ")), "�")
}

var (
	youtubePathPattern    = regexp.MustCompile(`/(?:live|shorts)/([^/]+)`)
	youtubeVideoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
)

func youtubeIDFromURL(value string) string {
	if value == "" {
		return ""
	}
	u, ok := parseURL(value)
	if !ok || !u.web() {
		return ""
	}
	return youtubeIDOf(u)
}

func validYouTubeID(value string) string {
	if youtubeVideoIDPattern.MatchString(value) {
		return value
	}
	return ""
}

// youtubeIDOf accepts only YouTube's real host boundary and video ID shape.
func youtubeIDOf(u *parsedURL) string {
	if u.host == "youtu.be" {
		return validYouTubeID(strings.TrimPrefix(u.path, "/"))
	}
	if u.host == "youtube.com" || strings.HasSuffix(u.host, ".youtube.com") {
		if id, _ := u.searchParam("v"); id != "" {
			return validYouTubeID(id)
		}
		if match := youtubePathPattern.FindStringSubmatch(u.path); match != nil {
			return validYouTubeID(match[1])
		}
	}
	return ""
}

func normalizePublicURL(value, fallback string) string {
	if value == "" {
		return fallback
	}
	u, ok := parseURL(value)
	if !ok || !u.web() {
		return fallback
	}
	return u.String()
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// mediaIDForSourceURL mirrors server/media-cache.ts: the opaque media
// identity is the hash of the normalized, fragment-free source URL.
func mediaIDForSourceURL(value string) string {
	value = jsTrim(value)
	if value == "" {
		return ""
	}
	u, ok := parseURL(value)
	if !ok || !u.web() {
		return ""
	}
	u.fragment = nil
	return sha256Hex(u.String())
}

var mediaIDPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

var (
	trailingSlashes   = regexp.MustCompile(`/+$`)
	endpointSuffix    = regexp.MustCompile(`/(?:responses|chat/completions|messages|models)$`)
	providerRootPaths = map[string]string{
		"openrouter.ai":                     "/api/v1",
		"generativelanguage.googleapis.com": "/v1beta/openai",
		"dashscope.aliyuncs.com":            "/compatible-mode/v1",
	}
)

// providerAPIRoot mirrors src/lib/llm-endpoints.ts.
func providerAPIRoot(baseURL string) string {
	base := jsTrim(baseURL)
	base = trailingSlashes.ReplaceAllString(base, "")
	base = endpointSuffix.ReplaceAllString(base, "")
	if base == "" {
		return ""
	}
	u, ok := parseURL(base)
	if !ok || !u.special {
		return base
	}
	path := trailingSlashes.ReplaceAllString(u.path, "")
	if path == "" || path == "/api" || path == "/compatible-mode" {
		if root, found := providerRootPaths[u.host]; found {
			u.path = root
		} else {
			u.path = path + "/v1"
		}
		return trailingSlashes.ReplaceAllString(u.String(), "")
	}
	return base
}

func inferenceEndpoint(baseURL, protocol string) string {
	base := providerAPIRoot(baseURL)
	if base == "" {
		return ""
	}
	switch protocol {
	case protocolAnthropic:
		return base + "/messages"
	case protocolChat:
		return base + "/chat/completions"
	}
	return base + "/responses"
}
