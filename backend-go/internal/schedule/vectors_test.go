package schedule

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// testdata/vectors.json was recorded from the original TypeScript sources and
// the Node runtime before that implementation was removed. It is a frozen
// reference that pins the JavaScript semantics this package re-implements: URL
// normalization, string handling, number formatting and the result
// normalizers.
type vectorData struct {
	Board        [][]any    `json:"board"`
	Numbers      [][]any    `json:"numbers"`
	Quoted       []string   `json:"quoted"`
	Reorder      []string   `json:"reorder"`
	Strings      [][]string `json:"strings"`
	Slices       [][]any    `json:"slices"`
	URLs         [][]any    `json:"urls"`
	OddURLs      [][]any    `json:"oddUrls"`
	Endpoints    [][]string `json:"endpoints"`
	Normalized   [][]any    `json:"normalized"`
	Verification [][]any    `json:"verification"`
	Capabilities [][]any    `json:"capabilities"`
	Tags         [][]any    `json:"tags"`
}

func loadVectors(t *testing.T) vectorData {
	t.Helper()
	raw, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var data vectorData
	if err = json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.URLs) == 0 || len(data.Normalized) == 0 {
		t.Fatal("empty vectors")
	}
	return data
}

func text(value any) string {
	out, _ := value.(string)
	return out
}

func stringList(value any) []string {
	out := []string{}
	for _, item := range value.([]any) {
		out = append(out, item.(string))
	}
	return out
}

func TestJavaScriptStringSemantics(t *testing.T) {
	data := loadVectors(t)
	for _, item := range data.Strings {
		if got := jsToLower(item[0]); got != item[1] {
			t.Errorf("toLowerCase(%q) = %q, want %q", item[0], got, item[1])
		}
		if got := jsTrim(item[0]); got != item[2] {
			t.Errorf("trim(%q) = %q, want %q", item[0], got, item[2])
		}
	}
	for _, item := range data.Slices {
		sliced := sliceUTF16(text(item[0]), int(item[1].(float64)))
		if got := sha256Hex(sliced); got != text(item[2]) {
			t.Errorf("hash of slice(%q, %v) = %s, want %s", item[0], item[1], got, item[2])
		}
		if got := utf16Length(sliced); got != int(item[3].(float64)) {
			t.Errorf("length of slice(%q, %v) = %d, want %v", item[0], item[1], got, item[3])
		}
	}
	for _, item := range data.Numbers {
		if got := formatNumber(item[0].(float64)); got != text(item[1]) {
			t.Errorf("String(%v) = %q, want %q", item[0], got, item[1])
		}
	}
	if got := stringify(data.Quoted[0]); got != data.Quoted[1] {
		t.Errorf("JSON.stringify string = %s, want %s", got, data.Quoted[1])
	}
	parsed, err := parseJSON([]byte(data.Reorder[0]))
	if err != nil {
		t.Fatal(err)
	}
	if got := stringify(parsed); got != data.Reorder[1] {
		t.Errorf("JSON round trip = %s, want %s", got, data.Reorder[1])
	}
	for _, invalid := range []string{"", "{", `{"a":1,}`, `[1 2]`, `{"a":1} x`, "\ufeff{}", "undefined"} {
		if _, err := parseJSON([]byte(invalid)); err == nil {
			t.Errorf("parseJSON(%q) succeeded", invalid)
		}
	}
}

func TestURLNormalizationMatchesWHATWG(t *testing.T) {
	data := loadVectors(t)
	for _, item := range data.URLs {
		input := text(item[0])
		parsed, ok := parseURL(input)
		if want, valid := item[1].(string); valid != ok {
			t.Errorf("parseURL(%q) ok = %v, want %v", input, ok, valid)
		} else if ok && parsed.special && parsed.String() != want {
			t.Errorf("parseURL(%q) = %q, want %q", input, parsed.String(), want)
		}
		if got := youtubeIDFromURL(input); got != text(item[2]) {
			t.Errorf("youtubeIDFromURL(%q) = %q, want %q", input, got, text(item[2]))
		}
		if got := mediaIDForSourceURL(input); got != text(item[3]) {
			t.Errorf("mediaIDForSourceURL(%q) = %q, want %q", input, got, text(item[3]))
		}
	}
	// Malformed and userinfo-bearing inputs are stored as parts and joined here.
	if len(data.OddURLs) == 0 {
		t.Fatal("missing odd URL vectors")
	}
	for _, item := range data.OddURLs {
		input := strings.Join(stringList(item[0]), "://")
		parsed, ok := parseURL(input)
		if item[1] == nil {
			if ok {
				t.Errorf("parseURL(%q) succeeded", input)
			}
		} else if want := strings.Join(stringList(item[1]), "://"); !ok || parsed.String() != want {
			t.Errorf("parseURL(%q) = %v, want %q", input, parsed, want)
		}
		if got := youtubeIDFromURL(input); got != text(item[2]) {
			t.Errorf("youtubeIDFromURL(%q) = %q, want %q", input, got, text(item[2]))
		}
		if got := mediaIDForSourceURL(input); got != text(item[3]) {
			t.Errorf("mediaIDForSourceURL(%q) = %q, want %q", input, got, text(item[3]))
		}
	}
	// The original host test is a suffix match without a label boundary.
	if got := youtubeIDOf(&parsedURL{special: true, host: "notyoutube.com", path: "/shorts/abc"}); got != "abc" {
		t.Errorf("suffix host id = %q", got)
	}
	if got := normalizePublicURL("javascript:alert(1)", "https://x.com/fallback"); got != "https://x.com/fallback" {
		t.Errorf("non-http link was kept: %q", got)
	}
	for _, item := range data.Endpoints {
		if got := providerAPIRoot(item[0]); got != item[2] {
			t.Errorf("providerAPIRoot(%q) = %q, want %q", item[0], got, item[2])
		}
		if got := inferenceEndpoint(item[0], item[1]); got != item[3] {
			t.Errorf("inferenceEndpoint(%q, %q) = %q, want %q", item[0], item[1], got, item[3])
		}
	}
}

func TestNormalizersMatchTypeScript(t *testing.T) {
	data := loadVectors(t)
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	for _, item := range data.Normalized {
		name := text(item[0])
		input, err := parseJSON([]byte(text(item[1])))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		result, err := normalizeExtractionResult(input, "post-1", "https://x.com/synthetic_handle/status/1", now)
		if wantError := text(item[3]); wantError != "" {
			if err == nil || err.Error() != wantError {
				t.Errorf("%s: error = %v, want %q", name, err, wantError)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error %v", name, err)
			continue
		}
		events := make([]any, len(result.events))
		for i, event := range result.events {
			events[i] = event.rawObject()
		}
		got := stringify(obj(
			"classification", result.classification, "events", events, "confidence", result.confidence,
			"evidence", result.evidence, "reason", result.reason, "action", result.action))
		if got != text(item[2]) {
			t.Errorf("%s:\n got  %s\n want %s", name, got, text(item[2]))
		}
	}
	for _, item := range data.Verification {
		name := text(item[0])
		input, err := parseJSON([]byte(text(item[1])))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		result, err := normalizeAssetVerification(input)
		if wantError := text(item[3]); wantError != "" {
			if err == nil || err.Error() != wantError {
				t.Errorf("%s: error = %v, want %q", name, err, wantError)
			}
			continue
		}
		got := stringify(obj("classification", result.classification, "confidence", result.confidence,
			"evidence", result.evidence, "reason", result.reason))
		if err != nil || got != text(item[2]) {
			t.Errorf("%s: got %s (%v), want %s", name, got, err, text(item[2]))
		}
	}
}

func TestSourceGateAndCapabilitiesMatchTypeScript(t *testing.T) {
	data := loadVectors(t)
	for _, item := range data.Board {
		if got := isLikelyScheduleBoardText(text(item[0])); got != item[1].(bool) {
			t.Errorf("isLikelyScheduleBoardText(%q) = %v", item[0], got)
		}
	}
	for _, item := range data.Capabilities {
		if got := providerCapabilities(text(item[0]), item[1].(bool)); !reflect.DeepEqual(got, stringList(item[2])) {
			t.Errorf("providerCapabilities(%q, %v) = %v, want %v", item[0], item[1], got, item[2])
		}
	}
	for _, item := range data.Tags {
		if got := modelCapabilities(text(item[0])); !reflect.DeepEqual(got, stringList(item[1])) {
			t.Errorf("modelCapabilities(%q) = %v, want %v", item[0], got, item[1])
		}
	}
	for _, check := range []struct {
		capabilities []string
		mode         string
		want         bool
	}{
		{[]string{"text"}, "text", true},
		{[]string{"text"}, "image", false},
		{[]string{"text"}, "text_image", false},
		{[]string{"text", "image"}, "text_image", true},
		{[]string{"image"}, "image", true},
		{[]string{"image"}, "text", false},
		{[]string{}, "text", true},
		{[]string{}, "image", false},
		{[]string{"text", "image"}, "audio", false},
	} {
		if got := supportsInput(check.capabilities, check.mode); got != check.want {
			t.Errorf("supportsInput(%v, %q) = %v", check.capabilities, check.mode, got)
		}
	}
}

func TestScanStatusMatchesScanOutcome(t *testing.T) {
	for _, check := range []struct {
		counts map[string]any
		want   string
	}{
		{map[string]any{"attempted": 0, "disabled": true}, "success"},
		{map[string]any{"success": 2, "failed": 0}, "success"},
		{map[string]any{"success": 1, "failed": 1}, "partial"},
		{map[string]any{"cached": 1, "failed": 1}, "partial"},
		{map[string]any{"uncertain": 1, "failed": 1}, "partial"},
		{map[string]any{"skipped": 3, "failed": 1}, "failed"},
		{map[string]any{"success": 0, "failed": 2, "assetVerification": map[string]any{"success": 1}}, "partial"},
		{map[string]any{"success": 1, "assetVerification": map[string]any{"failed": 1}}, "partial"},
		{map[string]any{"assetVerification": map[string]any{"failed": 1, "skipped": 2}}, "failed"},
	} {
		if got := scanStatus(check.counts); got != check.want {
			t.Errorf("scanStatus(%v) = %q, want %q", check.counts, got, check.want)
		}
	}
}

func TestKeywordAndToggleParsing(t *testing.T) {
	if got := boundedKeywords(parseListText(`["a,b"," c ","c",1,""]`)); !reflect.DeepEqual(got, []string{"a,b", "c", "1"}) {
		t.Errorf("json keywords = %v", got)
	}
	if got := boundedKeywords(parseListText("予定，schedule\n 予定 ,,")); !reflect.DeepEqual(got, []string{"予定", "schedule"}) {
		t.Errorf("split keywords = %v", got)
	}
	if got := boundedKeywords(parseListValues(defaultScheduleKeywords)); !reflect.DeepEqual(got, defaultScheduleKeywords) {
		t.Errorf("default keywords = %v", got)
	}
	if got := boundedKeywords(parseListValues([]string{})); len(got) != 0 {
		t.Errorf("empty keywords = %v", got)
	}
	for value, want := range map[string]bool{"1": true, " TRUE ": true, "on": true, "yes": true, "0": false, "off": false, "No": false, "false": false} {
		if settingBoolean(value, !want) != want {
			t.Errorf("settingBoolean(%q) != %v", value, want)
		}
	}
	if !settingBoolean("", true) || settingBoolean("maybe", false) {
		t.Error("settingBoolean fallback is wrong")
	}
}
