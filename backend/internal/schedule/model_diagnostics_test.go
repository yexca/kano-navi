package schedule

import (
	"net/http"
	"strings"
	"testing"
)

func TestTranslationRefusalDoesNotRetryOrStoreProviderWording(t *testing.T) {
	for _, protocol := range []string{protocolAnthropic, protocolChat, protocolResponses} {
		t.Run(protocol, func(t *testing.T) {
			f, translator := newTranslatorFixture(t)
			f.addProvider("main", protocol, "https://translate.example.test", "synthetic-model", `["text"]`)
			f.queueTranslation("main")
			f.addPost("1", "", "おはよう", recent(1), "")
			f.provider.respond = func(recordedRequest) (int, string) {
				switch protocol {
				case protocolAnthropic:
					return http.StatusOK, `{"stop_reason":"refusal","content":[{"type":"text","text":"synthetic private refusal"}]}`
				case protocolChat:
					return http.StatusOK, `{"choices":[{"finish_reason":"stop","message":{"refusal":"synthetic private refusal"}}]}`
				default:
					return http.StatusOK, `{"status":"completed","output":[{"type":"message","content":[{"type":"refusal","refusal":"synthetic private refusal"}]}]}`
				}
			}
			result := runTranslator(t, translator)
			if result.Status != "failed" || f.provider.count() != 1 || f.text(`SELECT last_error FROM translation_states`) != modelRefused {
				t.Fatalf("result=%v requests=%d", result, f.provider.count())
			}
			if strings.Contains(stringify(result.Counts), "private refusal") {
				t.Fatal("provider refusal wording leaked")
			}
		})
	}
}

func TestModelDiagnosticsIgnoreUntrustedAndInvalidMetadata(t *testing.T) {
	d := modelDiagnostics{}
	call := modelCall{jsonOutput: true, diagnostics: &d}
	payload, err := parseJSON([]byte(`{"stop_reason":"synthetic private response","usage":{"input_tokens":"synthetic credential","output_tokens":-1}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err = call.checkResponse(payload, protocolAnthropic); err != nil || d.stopReason != "unknown" || d.inputTokens != nil || d.outputTokens != nil {
		t.Fatalf("diagnostics=%+v err=%v", d, err)
	}
	for _, value := range []float64{0.5, 1_000_000_001} {
		if usageCount(value) != nil {
			t.Fatalf("invalid usage admitted: %v", value)
		}
	}
}
