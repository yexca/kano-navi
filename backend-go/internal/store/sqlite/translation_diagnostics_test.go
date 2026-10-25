package sqlite

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTranslationDiagnosticsKeepOnlySafeUsageAndErrors(t *testing.T) {
	for message, want := range map[string]string{
		"translation result is missing en":               "missing_translation_en",
		"translation result is missing zh-CN":            "missing_translation_zh_cn",
		"LLM output was truncated":                       "output_truncated",
		"LLM response did not contain structured output": "no_structured_output",
		"LLM refused translation":                        "model_refused",
		"synthetic upstream response":                    "provider_error",
	} {
		if got := TranslationErrorCode(message); got != want {
			t.Fatalf("code=%s want=%s", got, want)
		}
	}
	clean := sanitizeCounts(`{"diagnostics":[{"maxTokens":1024,"inputTokens":100,"outputTokens":40,"stopReason":"max_tokens","apiToken":"synthetic credential","raw_json":"private"},{"inputTokens":"synthetic credential","outputTokens":-1,"maxTokens":0.5,"stopReason":"synthetic secret"}]}`)
	entries := clean["diagnostics"].([]any)
	first, second := entries[0].(map[string]any), entries[1].(map[string]any)
	if first["maxTokens"] != float64(1024) || first["inputTokens"] != float64(100) || first["outputTokens"] != float64(40) || first["stopReason"] != "max_tokens" {
		t.Fatalf("usage was lost: %v", first)
	}
	if second["inputTokens"] != nil || second["outputTokens"] != nil || second["maxTokens"] != nil {
		t.Fatalf("invalid usage admitted: %v", second)
	}
	data, err := json.Marshal(clean)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "synthetic credential") || strings.Contains(string(data), "private") || strings.Contains(string(data), "synthetic secret") {
		t.Fatalf("unsafe diagnostic=%s", data)
	}
}
