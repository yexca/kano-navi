package llmadmin

import (
	"fmt"
	"math"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/yexca/kano-navi/backend/internal/httpx"
	"github.com/yexca/kano-navi/backend/internal/secretstore"
)

// text trims an optional field. "" stands for the JavaScript null result.
func text(value any, name string, required bool, max int) (string, error) {
	normalized := ""
	if value != nil {
		normalized = jsTrim(jsString(value))
	}
	if required && normalized == "" {
		return "", httpx.Invalid("%s is required", name)
	}
	if utf16Len(normalized) > max {
		return "", httpx.Invalid("%s is too long", name)
	}
	return normalized, nil
}

func booleanValue(value any, name string) (bool, error) {
	if flag, ok := value.(bool); ok {
		return flag, nil
	}
	switch strings.ToLower(jsTrim(stringOrEmpty(value))) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	}
	return false, httpx.Invalid("%s must be a boolean", name)
}

func isKeywordSeparator(r rune) bool { return r == '\n' || r == ',' || r == '，' }

func scheduleKeywords(value any) ([]string, error) {
	var items []string
	if array, ok := value.([]any); ok {
		for _, item := range array {
			items = append(items, truthyString(item))
		}
	} else {
		items = strings.FieldsFunc(stringOrEmpty(value), isKeywordSeparator)
	}
	trimmed := make([]string, 0, len(items))
	for _, item := range items {
		if keyword := jsTrim(item); keyword != "" {
			trimmed = append(trimmed, keyword)
		}
	}
	normalized := uniqueStrings(trimmed)
	if len(normalized) > 30 {
		return nil, httpx.Invalid("keywords contains too many entries")
	}
	for _, keyword := range normalized {
		if utf16Len(keyword) > 80 {
			return nil, httpx.Invalid("keywords contains an entry that is too long")
		}
	}
	return normalized, nil
}

func privateIPv4(address [4]byte) bool {
	a, b := address[0], address[1]
	return a == 0 ||
		a == 10 ||
		a == 127 ||
		(a == 100 && b >= 64 && b <= 127) ||
		(a == 169 && b == 254) ||
		(a == 172 && b >= 16 && b <= 31) ||
		(a == 192 && b == 0) ||
		(a == 192 && b == 168) ||
		(a == 198 && (b == 18 || b == 19)) ||
		(a == 198 && b == 51) ||
		(a == 203 && b == 0) ||
		a >= 224
}

// privateIPv6 covers the unspecified, loopback, unique-local, link-local and
// multicast ranges. IPv4-mapped addresses inherit the IPv4 policy.
func privateIPv6(address [16]byte) bool {
	zeroPrefix := func(n int) bool {
		for _, b := range address[:n] {
			if b != 0 {
				return false
			}
		}
		return true
	}
	if zeroPrefix(15) && address[15] <= 1 {
		return true
	}
	if address[0]&0xfe == 0xfc { // fc00::/7
		return true
	}
	if address[0] == 0xfe && address[1]&0xc0 == 0x80 { // fe80::/10
		return true
	}
	if address[0] == 0xff { // ff00::/8
		return true
	}
	if zeroPrefix(10) && address[10] == 0xff && address[11] == 0xff {
		return privateIPv4([4]byte(address[12:16]))
	}
	return false
}

// urlIPv4 applies the URL Standard's IPv4 host parser, which also accepts
// shortened, octal and hexadecimal spellings such as 0x7f.1 or 2130706433.
// isIPv4 reports whether the host must be read as an address at all; ok is
// false when it must but is malformed.
func urlIPv4(host string) (address [4]byte, isIPv4, ok bool) {
	parts := strings.Split(host, ".")
	if len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	parse := func(part string) (uint64, bool) {
		if part == "" {
			return 0, false
		}
		base := 10
		switch {
		case len(part) >= 2 && (part[:2] == "0x" || part[:2] == "0X"):
			part, base = part[2:], 16
		case len(part) >= 2 && part[0] == '0':
			part, base = part[1:], 8
		}
		if part == "" {
			return 0, true
		}
		n, err := strconv.ParseUint(part, base, 64)
		if err != nil && n != math.MaxUint64 {
			return 0, false
		}
		return n, true
	}
	last := parts[len(parts)-1]
	numeric := last != "" && strings.Trim(last, "0123456789") == ""
	if !numeric {
		if _, hex := parse(last); !hex || len(last) < 2 || (last[:2] != "0x" && last[:2] != "0X") {
			return address, false, false
		}
	}
	if len(parts) > 4 {
		return address, true, false
	}
	var total uint64
	for i, part := range parts {
		n, valid := parse(part)
		if !valid {
			return address, true, false
		}
		if i < len(parts)-1 {
			if n > 255 {
				return address, true, false
			}
			total |= n << (8 * (3 - i))
			continue
		}
		if n >= uint64(1)<<(8*(5-len(parts))) {
			return address, true, false
		}
		total |= n
	}
	return [4]byte{byte(total >> 24), byte(total >> 16), byte(total >> 8), byte(total)}, true, true
}

var hostnamePattern = regexp.MustCompile(`^[a-z0-9_-]+(?:\.[a-z0-9_-]*)*$`)

func removeDotSegments(path string) string {
	segments := strings.Split(path, "/")
	out := make([]string, 0, len(segments))
	for i, segment := range segments {
		last := i == len(segments)-1
		switch strings.ToLower(segment) {
		case ".", "%2e":
			if last {
				out = append(out, "")
			}
		case "..", ".%2e", "%2e.", "%2e%2e":
			if len(out) > 1 {
				out = out[:len(out)-1]
			}
			if last {
				out = append(out, "")
			}
		default:
			out = append(out, segment)
		}
	}
	return strings.Join(out, "/")
}

// providerBaseURL validates and canonicalizes a provider address. Loopback,
// private, link-local and internal-only names are refused, and production
// accepts HTTPS only.
func providerBaseURL(value any, mode string) (string, error) {
	normalized, err := text(value, "baseUrl", true, 500)
	if err != nil {
		return "", err
	}
	invalid := httpx.Invalid("baseUrl is invalid")
	if strings.Contains(normalized, `\`) {
		return "", invalid
	}
	parsed, parseErr := url.Parse(normalized)
	if parseErr != nil || parsed.Scheme == "" {
		return "", invalid
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", httpx.Invalid("baseUrl must use HTTP or HTTPS")
	}
	if parsed.Opaque != "" || parsed.Host == "" {
		return "", invalid
	}
	credentials := false
	if parsed.User != nil {
		password, _ := parsed.User.Password()
		credentials = parsed.User.Username() != "" || password != ""
	}
	if credentials || strings.ContainsAny(normalized, "?#") {
		return "", httpx.Invalid("baseUrl must not contain credentials, query, or fragment")
	}

	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" || strings.Contains(hostname, "%") {
		return "", invalid
	}
	private := false
	serialized := hostname
	if strings.HasPrefix(parsed.Host, "[") {
		address, addrErr := netip.ParseAddr(hostname)
		if addrErr != nil || !address.Is6() {
			return "", invalid
		}
		bytes := address.As16()
		private = privateIPv6(bytes)
		serialized = "[" + address.String() + "]"
		if address.Is4In6() {
			// URL serialization keeps mapped addresses in hexadecimal groups.
			serialized = fmt.Sprintf("[::ffff:%x:%x]", uint16(bytes[12])<<8|uint16(bytes[13]), uint16(bytes[14])<<8|uint16(bytes[15]))
		}
	} else {
		for _, r := range hostname {
			if r > 0x7f {
				return "", invalid
			}
		}
		if !hostnamePattern.MatchString(hostname) {
			return "", invalid
		}
		if address, isIPv4, ok := urlIPv4(hostname); isIPv4 {
			if !ok {
				return "", invalid
			}
			private = privateIPv4(address)
			serialized = netip.AddrFrom4(address).String()
		} else {
			name := strings.TrimSuffix(hostname, ".")
			private = name == "localhost" ||
				strings.HasSuffix(name, ".localhost") ||
				strings.HasSuffix(name, ".local") ||
				strings.HasSuffix(name, ".internal")
		}
	}
	if private {
		return "", httpx.Invalid("baseUrl must not target a private or loopback address")
	}
	if mode == "production" && scheme != "https" {
		return "", httpx.Invalid("baseUrl must use HTTPS in production")
	}

	if port := parsed.Port(); port != "" {
		number, portErr := strconv.Atoi(port)
		if portErr != nil || number < 0 || number > 65535 {
			return "", invalid
		}
		if (scheme == "http" && number != 80) || (scheme == "https" && number != 443) {
			serialized += ":" + strconv.Itoa(number)
		}
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	return strings.TrimRight(scheme+"://"+serialized+removeDotSegments(path), "/"), nil
}

func modelID(value any, name string, required bool) (string, error) {
	model, err := text(value, name, required, 200)
	if err != nil || model == "" {
		return "", err
	}
	if !modelIDPattern.MatchString(model) {
		return "", httpx.Invalid("%s contains unsupported characters", name)
	}
	return model, nil
}

func providerCapabilities(value any, existing *Provider) ([]string, error) {
	if value == nil {
		if existing != nil {
			return slices.Clone(existing.Capabilities), nil
		}
		return slices.Clone(llmCapabilities), nil
	}
	values, _ := listValues(value)
	capabilities := normalizeCapabilities(values)
	if len(capabilities) == 0 {
		return nil, httpx.Invalid("capabilities must include at least one of text or image")
	}
	if len(capabilities) != len(values) {
		for _, item := range values {
			if word := vocabularyWord(item); word != "" && !slices.Contains(llmCapabilities, word) {
				return nil, httpx.Invalid("capabilities contains an unsupported value")
			}
		}
	}
	return capabilities, nil
}

func integerField(value any, name string, minimum, maximum, fallback int64) (int64, error) {
	if value == nil || value == "" {
		return fallback, nil
	}
	number, ok := jsNumber(value)
	if !ok || math.IsInf(number, 0) || number != math.Trunc(number) || number < float64(minimum) || number > float64(maximum) {
		return 0, httpx.Invalid("%s must be an integer between %d and %d", name, minimum, maximum)
	}
	return int64(number), nil
}

// providerWrite is a validated provider row. The ciphertext only travels from
// here into the INSERT statement.
type providerWrite struct {
	ID               string
	Name             string
	Protocol         string
	BaseURL          string
	Enabled          bool
	Capabilities     []string
	TimeoutMs        int64
	ReplaceAPIKey    bool
	APIKeyCiphertext string
}

func coalesce(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func (m *Module) providerInput(body map[string]any, existing *Provider) (providerWrite, error) {
	var input providerWrite
	var err error
	// existingValue keeps "no existing provider" distinct from a present field.
	existingValue := func(pick func(*Provider) any) any {
		if existing == nil {
			return nil
		}
		return pick(existing)
	}

	if input.ID, err = text(coalesce(body["id"], existingValue(func(p *Provider) any { return p.ID })), "id", true, 64); err != nil {
		return input, err
	}
	if !providerIDPattern.MatchString(input.ID) {
		return input, httpx.Invalid("id contains unsupported characters")
	}
	input.Protocol = jsString(coalesce(body["protocol"], existingValue(func(p *Provider) any { return p.Protocol }), protocolResponses))
	if !slices.Contains(llmProtocols, input.Protocol) {
		return input, httpx.Invalid("protocol is invalid")
	}
	if input.BaseURL, err = providerBaseURL(coalesce(body["baseUrl"], existingValue(func(p *Provider) any { return p.BaseURL })), m.mode); err != nil {
		return input, err
	}
	// Providers have no default model: queue targets and tests name a
	// catalog model from llm_models. A "model" field from an older client is
	// ignored.
	if input.Name, err = text(coalesce(body["name"], existingValue(func(p *Provider) any { return p.Name }), input.ID), "name", true, 120); err != nil {
		return input, err
	}
	if body["enabled"] == nil {
		input.Enabled = existing == nil || existing.Enabled
	} else if input.Enabled, err = booleanValue(body["enabled"], "enabled"); err != nil {
		return input, err
	}

	rawCapabilities, hasCapabilities := body["capabilities"]
	rawVision, hasLegacyVision := body["visionCapable"]
	legacyVision := true
	if hasLegacyVision {
		if legacyVision, err = booleanValue(rawVision, "visionCapable"); err != nil {
			return input, err
		}
	}
	switch {
	case hasCapabilities:
		input.Capabilities, err = providerCapabilities(rawCapabilities, existing)
	case hasLegacyVision && legacyVision:
		input.Capabilities, err = providerCapabilities([]any{"text", "image"}, existing)
	case hasLegacyVision:
		input.Capabilities, err = providerCapabilities([]any{"text"}, existing)
	default:
		input.Capabilities, err = providerCapabilities(nil, existing)
	}
	if err != nil {
		return input, err
	}

	timeoutFallback := int64(30000)
	if existing != nil {
		timeoutFallback = existing.TimeoutMs
	}
	if input.TimeoutMs, err = integerField(body["timeoutMs"], "timeoutMs", 1000, 120000, timeoutFallback); err != nil {
		return input, err
	}
	// Still validated for older clients; every stored row uses MaxRetries.
	if _, err = integerField(body["maxRetries"], "maxRetries", 0, MaxRetries, MaxRetries); err != nil {
		return input, err
	}

	if rawKey, hasKey := body["apiKey"]; hasKey {
		apiKey := jsTrim(stringOrEmpty(rawKey))
		if utf16Len(apiKey) > 500 {
			return input, httpx.Invalid("apiKey is too long")
		}
		input.ReplaceAPIKey = true
		if apiKey != "" {
			sealed, encryptErr := secretstore.Encrypt(apiKey, m.key)
			if encryptErr != nil {
				// A fixed message: the underlying error must never carry input.
				return input, httpx.Invalid("%s", secretstore.ErrKeyRequired.Error())
			}
			input.APIKeyCiphertext = sealed
		}
	}
	return input, nil
}

// modelWrite carries only the fields a client actually sent; an unset field
// keeps the stored value.
type modelWrite struct {
	ID         string
	Name       *string
	HasName    bool
	Tags       []string
	HasTags    bool
	Enabled    bool
	HasEnabled bool
	OwnedBy    *string
	HasOwnedBy bool
	Origin     string
}

func modelTags(value any) ([]string, error) {
	values, isArray := value.([]any)
	if !isArray {
		for _, part := range strings.Split(stringOrEmpty(value), ",") {
			values = append(values, part)
		}
	}
	for _, item := range values {
		word := strings.ToLower(jsTrim(truthyString(item)))
		if word == "vision" {
			word = "image"
		}
		if word != "" && !slices.Contains(llmModelTags, word) {
			return nil, httpx.Invalid("tags contains an unsupported value")
		}
	}
	return normalizeModelTags(values), nil
}

func modelInput(value any) (modelWrite, error) {
	object, _ := value.(map[string]any)
	var input modelWrite
	var err error
	if input.ID, err = modelID(object["id"], "model id", true); err != nil {
		return input, err
	}
	if raw, ok := object["name"]; ok {
		name, nameErr := text(raw, "model name", false, 120)
		if nameErr != nil {
			return input, nameErr
		}
		input.Name, input.HasName = nullableString(name), true
	}
	if raw, ok := object["tags"]; ok {
		if input.Tags, err = modelTags(raw); err != nil {
			return input, err
		}
		input.HasTags = true
	}
	if raw, ok := object["enabled"]; ok {
		if input.Enabled, err = booleanValue(raw, "enabled"); err != nil {
			return input, err
		}
		input.HasEnabled = true
	}
	if raw, ok := object["ownedBy"]; ok {
		owner, ownerErr := text(raw, "ownedBy", false, 120)
		if ownerErr != nil {
			return input, ownerErr
		}
		input.OwnedBy, input.HasOwnedBy = nullableString(owner), true
	}
	return input, nil
}

func routeTargetsInput(value any) ([]RouteTarget, error) {
	items, ok := value.([]any)
	if !ok || len(items) > 100 {
		return nil, httpx.Invalid("targets must be an array")
	}
	targets := make([]RouteTarget, 0, len(items))
	for _, item := range items {
		object, _ := item.(map[string]any)
		providerID, err := text(object["providerId"], "providerId", true, 64)
		if err != nil {
			return nil, err
		}
		model, err := modelID(object["modelId"], "modelId", true)
		if err != nil {
			return nil, err
		}
		targets = append(targets, RouteTarget{ProviderID: providerID, ModelID: model})
	}
	return targets, nil
}
