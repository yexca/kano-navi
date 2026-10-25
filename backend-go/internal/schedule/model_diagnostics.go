package schedule

// Only fixed stop reasons and bounded numeric usage leave the provider
// boundary. Response text, thinking, refusal wording and headers never do.
const (
	outputTruncated = "LLM output was truncated"
	modelRefused    = "LLM refused translation"
)

type modelDiagnostics struct {
	stopReason                string
	inputTokens, outputTokens *int
}

func usageCount(value any) *int {
	n, ok := value.(float64)
	if !ok || !finite(n) || n < 0 || n > 1_000_000_000 || n != float64(int(n)) {
		return nil
	}
	count := int(n)
	return &count
}

func (call modelCall) checkResponse(payload any, protocol string) error {
	if !call.jsonOutput {
		return nil
	}
	d := modelDiagnostics{}
	usage := property(payload, "usage")
	var reason any
	refused := false
	switch protocol {
	case protocolAnthropic:
		reason = property(payload, "stop_reason")
		d.inputTokens, d.outputTokens = usageCount(property(usage, "input_tokens")), usageCount(property(usage, "output_tokens"))
	case protocolChat:
		choices, _ := property(payload, "choices").([]any)
		if len(choices) > 0 {
			reason = property(choices[0], "finish_reason")
			refusal := property(property(choices[0], "message"), "refusal")
			refused = refusal != nil && refusal != ""
		}
		d.inputTokens, d.outputTokens = usageCount(property(usage, "prompt_tokens")), usageCount(property(usage, "completion_tokens"))
	default:
		reason = property(payload, "status")
		if reason == "incomplete" {
			reason = property(property(payload, "incomplete_details"), "reason")
		}
		outputs, _ := property(payload, "output").([]any)
		for _, output := range outputs {
			parts, _ := property(output, "content").([]any)
			for _, part := range parts {
				refused = refused || property(part, "type") == "refusal"
			}
		}
		d.inputTokens, d.outputTokens = usageCount(property(usage, "input_tokens")), usageCount(property(usage, "output_tokens"))
	}
	// Keep the original API code when it is one of the known safe values.
	switch reason {
	case "end_turn", "tool_use", "stop_sequence", "stop", "completed", "max_tokens", "max_output_tokens", "length", "refusal", "content_filter", "incomplete", "failed":
		d.stopReason = reason.(string)
	default:
		d.stopReason = "unknown"
	}
	if refused {
		d.stopReason = "refusal"
	}
	if call.diagnostics != nil {
		*call.diagnostics = d
	}
	switch d.stopReason {
	case "max_tokens", "max_output_tokens", "length":
		return fail(outputTruncated)
	case "refusal", "content_filter":
		return fail(modelRefused)
	}
	if protocol == protocolResponses && (property(payload, "status") == "incomplete" || property(payload, "status") == "failed") {
		return fail(noStructuredOutput)
	}
	return nil
}
