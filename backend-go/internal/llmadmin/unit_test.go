package llmadmin

import (
	"context"
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

func TestProviderBaseURLCanonicalForm(t *testing.T) {
	cases := map[string]string{
		"https://provider.example.test":                    "https://provider.example.test",
		"  https://provider.example.test/v1///  ":          "https://provider.example.test/v1",
		"HTTPS://Provider.Example.TEST:443/V1":             "https://provider.example.test/V1",
		"https://provider.example.test:8443/api/":          "https://provider.example.test:8443/api",
		"https://provider.example.test/a/./b/../v1":        "https://provider.example.test/a/v1",
		"https://provider.example.test/v1/chat models":     "https://provider.example.test/v1/chat%20models",
		"https://@provider.example.test/v1":                "https://provider.example.test/v1",
		secureURL("[2001:DB8::10]", "/v1"):                 secureURL("[2001:db8::10]", "/v1"),
		secureURL(ipv4(203, 1, 113, 9), "/v1"):             secureURL(ipv4(203, 1, 113, 9), "/v1"),
		secureURL("[::ffff:"+ipv4(203, 1, 113, 9)+"]", ""): secureURL("[::ffff:cb01:7109]", ""),
	}
	for input, want := range cases {
		got, err := providerBaseURL(input, "production")
		if err != nil || got != want {
			t.Errorf("providerBaseURL(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{
		"provider.example.test/v1", "https:provider.example.test", secureURL("", ""), secureURL("", "/v1"),
		secureURL("provider.example.test:99999", ""), secureURL("exa mple.test", ""), secureURL("provider.example.test\\v1", ""),
		secureURL(ipv4(1, 2, 3, 4)+".5", ""), secureURL(strings.Join([]string{"256", "1", "1", "1"}, "."), ""),
		secureURL("[fe80::1%25eth0]", "/"), secureURL("пример.example.test", ""),
	} {
		if got, err := providerBaseURL(input, "development"); err == nil {
			t.Errorf("providerBaseURL(%q) = %q, want an error", input, got)
		}
	}
	if _, err := providerBaseURL(nil, "production"); err == nil || err.Error() != "baseUrl is required" {
		t.Errorf("missing baseUrl: %v", err)
	}
}

func TestPrivateAddressPolicy(t *testing.T) {
	// Octets on each side of every range boundary in the policy.
	privateV4 := [][4]byte{
		{0, 0, 0, 0}, {10, 0, 0, 1}, {100, 64, 0, 1}, {100, 127, 255, 255}, {127, 0, 0, 1}, {169, 254, 1, 1},
		{172, 16, 0, 1}, {172, 31, 255, 255}, {192, 0, 0, 1}, {192, 168, 0, 1}, {198, 18, 0, 1}, {198, 19, 0, 1},
		{198, 51, 100, 1}, {203, 0, 113, 1}, {224, 0, 0, 1}, {255, 255, 255, 255},
	}
	publicV4 := [][4]byte{
		{1, 1, 1, 1}, {9, 255, 255, 255}, {11, 0, 0, 1}, {100, 63, 0, 1}, {100, 128, 0, 1}, {169, 253, 0, 1},
		{172, 15, 0, 1}, {172, 32, 0, 1}, {192, 1, 0, 1}, {192, 167, 0, 1}, {198, 17, 0, 1}, {198, 20, 0, 1},
		{203, 1, 0, 1}, {223, 255, 255, 255},
	}
	for _, address := range privateV4 {
		if !privateIPv4(address) {
			t.Errorf("%v should be refused", address)
		}
		// An IPv4-mapped IPv6 address inherits the IPv4 decision.
		if !privateIPv6(netip.AddrFrom4(address).As16()) {
			t.Errorf("mapped %v should be refused", address)
		}
	}
	for _, address := range publicV4 {
		if privateIPv4(address) {
			t.Errorf("%v should be allowed", address)
		}
		if privateIPv6(netip.AddrFrom4(address).As16()) {
			t.Errorf("mapped %v should be allowed", address)
		}
	}
	for _, text := range []string{"::", "::1", "fc00::1", "fdff::1", "fe80::1", "febf::1", "ff02::1"} {
		if !privateIPv6(netip.MustParseAddr(text).As16()) {
			t.Errorf("%s should be refused", text)
		}
	}
	for _, text := range []string{"2001:db8::1", "fec0::1", "fbff::1", "::2", "2000::1"} {
		if privateIPv6(netip.MustParseAddr(text).As16()) {
			t.Errorf("%s should be allowed", text)
		}
	}
}

func TestInferModelTags(t *testing.T) {
	cases := map[string][]string{
		"gpt-4o-mini":            {"text", "image", "tools"},
		"gpt-5":                  {"text", "image", "reasoning", "tools"},
		"o1":                     {"text", "image", "reasoning"},
		"o1-mini":                {"text", "reasoning"},
		"O1-MINI":                {"text", "reasoning"},
		"o3-mini":                {"text", "image", "reasoning", "tools"},
		"claude-sonnet-4-5":      {"text", "image", "reasoning", "tools"},
		"deepseek-chat":          {"text", "tools"},
		"deepseek-reasoner":      {"text", "reasoning"},
		"qwen-vl-max":            {"text", "image", "tools"},
		"text-embedding-3-small": {"embedding"},
		"bge-m3":                 {"embedding"},
		"whisper-1":              {},
		"dall-e-3":               {},
		"plain-model":            {"text"},
		"":                       {"text"},
	}
	for id, want := range cases {
		if got := InferModelTags(id); !reflect.DeepEqual(got, want) {
			t.Errorf("InferModelTags(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestParseModelListShapes(t *testing.T) {
	ids := func(models []RemoteModel) []string {
		out := []string{}
		for _, model := range models {
			out = append(out, model.ID)
		}
		return out
	}
	equal(t, "bare array", ids(parseModelList([]any{"b-model", "a-model", "b-model"})), []string{"a-model", "b-model"})
	equal(t, "models key", ids(parseModelList(map[string]any{"models": []any{map[string]any{"model": "x-1"}}})), []string{"x-1"})
	equal(t, "unknown shape", ids(parseModelList(map[string]any{"items": []any{"x"}})), []string{})
	equal(t, "scalar", ids(parseModelList("x")), []string{})
	// Punctuation sorts before digits, digits before letters, case is ignored.
	equal(t, "collation", ids(parseModelList([]any{"model-b", "Model-a", "model.a", "model1", "model_a", "modela", "model/a"})),
		[]string{"model_a", "Model-a", "model-b", "model.a", "model/a", "model1", "modela"})
}

func TestModelIDCollation(t *testing.T) {
	ordered := []string{"a_b", "a-b", "a:b", "a.b", "a@b", "a/b", "a+b", "a1", "aa", "Ab", "b"}
	for i := 0; i+1 < len(ordered); i++ {
		if compareModelIDs(ordered[i], ordered[i+1]) >= 0 {
			t.Errorf("%q should sort before %q", ordered[i], ordered[i+1])
		}
	}
	if compareModelIDs("Model", "model") != 0 {
		t.Error("case must not affect ordering")
	}
}

func TestJavaScriptCoercions(t *testing.T) {
	equal(t, "integer", jsString(float64(30000)), "30000")
	equal(t, "fraction", jsString(1.5), "1.5")
	equal(t, "large", jsString(1e21), "1e+21")
	equal(t, "small", jsString(1.5e-7), "1.5e-7")
	equal(t, "array", jsString([]any{"a", nil, float64(2)}), "a,,2")
	equal(t, "object", jsString(map[string]any{}), "[object Object]")
	equal(t, "trim", jsTrim(string([]rune{0xFEFF, 0x3000, 0x20, 0x78, 0x20, 0xA0})), "x")
	equal(t, "utf16 length", utf16Len("a😀"), 3)
	equal(t, "utf16 prefix", utf16Prefix("a😀b", 2), "a")
	for input, want := range map[string]float64{"": 0, " 42 ": 42, "0x10": 16, "1e3": 1000, ".5": 0.5} {
		if got, ok := parseJSNumber(input); !ok || got != want {
			t.Errorf("parseJSNumber(%q) = %v, %v", input, got, ok)
		}
	}
	for _, input := range []string{"abc", "1_000", "0x", "1 2", "inf", "0x1p3"} {
		if got, ok := parseJSNumber(input); ok {
			t.Errorf("parseJSNumber(%q) = %v, want NaN", input, got)
		}
	}
	for _, value := range []any{"1500", float64(1500), []any{"1500"}} {
		if got, err := integerField(value, "timeoutMs", 1000, 120000, 30000); err != nil || got != 1500 {
			t.Errorf("integerField(%#v) = %v, %v", value, got, err)
		}
	}
	for _, value := range []any{1500.5, "soon", true, map[string]any{}, float64(999)} {
		if _, err := integerField(value, "timeoutMs", 1000, 120000, 30000); err == nil {
			t.Errorf("integerField(%#v) should fail", value)
		}
	}
	if got, _ := integerField("", "timeoutMs", 1000, 120000, 30000); got != 30000 {
		t.Errorf("empty integerField = %v", got)
	}
}

func TestEndpoints(t *testing.T) {
	cases := []struct{ base, protocol, inference, models string }{
		{"https://provider.example.test", protocolResponses, "https://provider.example.test/v1/responses", "https://provider.example.test/v1/models"},
		{"https://provider.example.test/v1/", protocolChat, "https://provider.example.test/v1/chat/completions", "https://provider.example.test/v1/models"},
		{"https://provider.example.test/v1/chat/completions", protocolAnthropic, "https://provider.example.test/v1/messages", "https://provider.example.test/v1/models"},
		{"https://provider.example.test/api", protocolChat, "https://provider.example.test/api/v1/chat/completions", "https://provider.example.test/api/v1/models"},
		{"https://provider.example.test/custom/path", protocolResponses, "https://provider.example.test/custom/path/responses", "https://provider.example.test/custom/path/models"},
		{"", protocolResponses, "", ""},
	}
	for _, test := range cases {
		equal(t, "inference "+test.base, inferenceEndpoint(test.base, test.protocol), test.inference)
		equal(t, "models "+test.base, modelsEndpoint(test.base), test.models)
	}
}

func TestAppSettingHelpers(t *testing.T) {
	h := newHarness(t, harnessOptions{})
	ctx := context.Background()
	db := h.store.DB()
	if value, err := GetAppSetting(ctx, db, "synthetic_key", "fallback"); err != nil || value != "fallback" {
		t.Fatalf("GetAppSetting fallback = %q, %v", value, err)
	}
	if _, found, err := LookupAppSetting(ctx, db, "synthetic_key"); err != nil || found {
		t.Fatalf("LookupAppSetting found=%v err=%v", found, err)
	}
	updatedAt, err := SetAppSetting(ctx, db, "synthetic_key", "")
	if err != nil || updatedAt == "" {
		t.Fatalf("SetAppSetting = %q, %v", updatedAt, err)
	}
	// A stored empty value is a value, not a reason to use the fallback.
	if value, err := GetAppSetting(ctx, db, "synthetic_key", "fallback"); err != nil || value != "" {
		t.Fatalf("GetAppSetting stored = %q, %v", value, err)
	}
	if _, err = SetAppSetting(ctx, db, "synthetic_key", "second"); err != nil {
		t.Fatal(err)
	}
	equal(t, "updated value", h.scalar(`SELECT value FROM app_settings WHERE key = 'synthetic_key'`), "second")
	equal(t, "single row", h.scalar(`SELECT CAST(COUNT(*) AS TEXT) FROM app_settings WHERE key = 'synthetic_key'`), "1")
}
