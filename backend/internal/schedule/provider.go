package schedule

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/yexca/kano-navi/backend/internal/secretstore"
)

const (
	maxResponseBytes = 4 * 1024 * 1024
	// Cached media is capped at 25 MiB by the downloader; anything larger is
	// not a file this service wrote.
	maxImageBytes = 26 * 1024 * 1024
)

// provider is one usable route target: an enabled provider, the model the
// target names (or the provider default) and its decrypted credential. The
// key never leaves this struct except as a request header.
type provider struct {
	id           string
	protocol     string
	baseURL      string
	model        string
	timeoutMs    float64
	capabilities []string
	apiKey       string
}

// capabilityTokens mirrors the shared parsing of capabilities_json and
// tags_json: a JSON array, or a comma/whitespace separated list.
func capabilityTokens(raw string) []string {
	var values []string
	if parsed, err := parseJSON([]byte(raw)); err == nil {
		if list, ok := parsed.([]any); ok {
			for _, item := range list {
				values = append(values, stringOr(item, ""))
			}
			return normalizeTokens(values)
		}
	}
	values = strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || isJSSpace(r) })
	return normalizeTokens(values)
}

func normalizeTokens(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = jsToLower(jsTrim(value))
		if value == "vision" {
			value = "image"
		}
		out = append(out, value)
	}
	return out
}

// providerCapabilities mirrors normalizeLlmCapabilities for a provider row.
func providerCapabilities(raw string, legacyVision bool) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, token := range capabilityTokens(raw) {
		if (token == "text" || token == "image") && !seen[token] {
			seen[token] = true
			out = append(out, token)
		}
	}
	if len(out) > 0 {
		return out
	}
	if legacyVision {
		return []string{"text", "image"}
	}
	return []string{"text"}
}

// modelCapabilities keeps the routing tags of a catalog model in the fixed
// tag order. A model without routing tags is treated as text-only.
func modelCapabilities(raw string) []string {
	selected := map[string]bool{}
	for _, token := range capabilityTokens(raw) {
		selected[token] = true
	}
	out := []string{}
	for _, tag := range []string{"text", "image"} {
		if selected[tag] {
			out = append(out, tag)
		}
	}
	return out
}

func supportsInput(capabilities []string, inputMode string) bool {
	hasText, hasImage := len(capabilities) == 0, false
	for _, capability := range capabilities {
		switch capability {
		case "text":
			hasText = true
		case "image":
			hasImage = true
		}
	}
	switch inputMode {
	case "text_image":
		return hasText && hasImage
	case "text":
		return hasText
	case "image":
		return hasImage
	}
	return false
}

func routeForDetectionType(detectionType string) string {
	if detectionType == "message" {
		return routeMessage
	}
	return routeBoard
}

// configuredProviders returns the usable schedule queue targets for an input
// mode in priority order; targets whose capabilities do not fit are skipped.
// An empty list comes with the reason recorded on the skipped post or image.
func (e *Extractor) configuredProviders(ctx context.Context, inputMode string) ([]provider, string, error) {
	return e.routeProviders(ctx, routeQueue, inputMode)
}

// routeProviders resolves the targets of one queue: the schedule queue or the
// separate translation queue.
func (e *Extractor) routeProviders(ctx context.Context, route, inputMode string) ([]provider, string, error) {
	targets, err := queueTargets(ctx, e.db, route)
	if err != nil {
		return nil, "", err
	}
	if len(targets) == 0 {
		return []provider{}, "no_route_target", nil
	}
	providers := []provider{}
	apiKeys := map[string]string{}
	keyed := 0
	for _, target := range targets {
		row, err := llmProvider(ctx, e.db, target.providerID)
		if err != nil {
			return nil, "", err
		}
		if row == nil || !row.enabled {
			continue
		}
		// Every target names a catalog model; providers have no default.
		if target.modelID == "" {
			continue
		}
		catalog, err := llmModel(ctx, e.db, row.id, target.modelID)
		if err != nil {
			return nil, "", err
		}
		if catalog == nil || !catalog.enabled {
			continue
		}
		model, capabilities := catalog.id, catalog.capabilities
		apiKey, known := apiKeys[row.id]
		if !known {
			if row.ciphertext != "" {
				if plain, err := secretstore.Decrypt(row.ciphertext, e.secretsKey); err == nil {
					apiKey = plain
				}
			}
			apiKeys[row.id] = apiKey
		}
		if apiKey == "" {
			continue
		}
		keyed++
		if !supportsInput(capabilities, inputMode) {
			continue
		}
		providers = append(providers, provider{
			id:           row.id,
			protocol:     row.protocol,
			baseURL:      row.baseURL,
			model:        model,
			timeoutMs:    row.timeoutMs,
			capabilities: capabilities,
			apiKey:       apiKey,
		})
	}
	if len(providers) > 0 {
		return providers, "", nil
	}
	if keyed > 0 {
		return providers, "no_compatible_provider", nil
	}
	return providers, "missing_api_key", nil
}

func (p provider) endpoint() string {
	base := p.baseURL
	if base == "" {
		base = defaultOpenAIBaseURL
	}
	return inferenceEndpoint(base, p.protocol)
}

// extractorVersionFor is part of the stored extraction identity. The middle
// array element is the explicit endpoint override, which persisted providers
// never carry, so it is always null.
func extractorVersionFor(p provider, detectionType, inputMode string) string {
	return extractorVersion + ":" + detectionType + ":" + inputMode + ":" + p.id + ":" + p.protocol + ":" +
		sha256Hex(stringify([]any{p.baseURL, nil, p.model}))
}

func timeoutFor(p provider) time.Duration {
	ms := p.timeoutMs
	if !finite(ms) {
		ms = 30_000
	}
	ms = min(120_000, max(1, ms))
	return time.Duration(ms * float64(time.Millisecond))
}

// modelAttempts is how many calls one model gets for one input before the
// next model in the queue takes over: the operator's setting, or its
// default when the settings cannot be read.
func (e *Extractor) modelAttempts(ctx context.Context) int {
	settings, _ := e.store.SyncSettings(ctx)
	return settings.LLMModelAttempts
}

func retryDelay(attempt int) time.Duration {
	return min(1000, 100*time.Duration(1<<max(0, attempt))) * time.Millisecond
}

var errRedirect = errors.New("redirect rejected")

func newHTTPClient(base *http.Client) *http.Client {
	client := &http.Client{}
	if base != nil {
		copied := *base
		client = &copied
	}
	// A provider endpoint never legitimately redirects an inference request,
	// and following one would send the credential to another destination.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return errRedirect }
	return client
}

// requestLLM posts one request to the provider's inference endpoint. Every
// failure collapses to one of three fixed messages so no payload, credential
// or transport detail reaches stored state. A cancelled run returns ctx.Err().
func (e *Extractor) requestLLM(ctx context.Context, p provider, body string) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeoutFor(p))
	defer cancel()
	failure := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if requestCtx.Err() != nil {
			return fail("LLM request timed out")
		}
		return fail("LLM request failed")
	}
	target, ok := parseURL(p.endpoint())
	if !ok || !target.web() {
		return nil, failure()
	}
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, target.String(), strings.NewReader(body))
	if err != nil {
		return nil, failure()
	}
	request.Header.Set("Content-Type", "application/json")
	if p.protocol == protocolAnthropic {
		request.Header.Set("X-Api-Key", p.apiKey)
		request.Header.Set("Anthropic-Version", "2023-06-01")
	} else {
		request.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	response, err := e.client.Do(request)
	if err != nil {
		return nil, failure()
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		if ctx.Err() != nil || requestCtx.Err() != nil {
			return nil, failure()
		}
		return nil, fail(fmt.Sprintf("LLM request failed (HTTP %d)", response.StatusCode))
	}
	if response.ContentLength > maxResponseBytes {
		return nil, failure()
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
		return nil, failure()
	}
	payload, err := parseJSON(data)
	if err != nil {
		return nil, failure()
	}
	return payload, nil
}

// cachedImage is a ready, contained image of the runtime media cache.
type cachedImage struct {
	path     string
	mimeType string
	sha256   string
}

func (image cachedImage) base64() (string, error) {
	file, err := os.Open(image.path)
	if err != nil {
		return "", fail("schedule image could not be read")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxImageBytes+1))
	if err != nil || len(data) > maxImageBytes {
		return "", fail("schedule image could not be read")
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// modelCall describes one structured-output request independent of protocol.
type modelCall struct {
	instructions string
	name         string
	schema       rawJSON
	maxTokens    int
	text         string
	includeText  bool
	images       []cachedImage
	// JSON output is used by translation, which returns data rather than a
	// tool action. Schedule extraction keeps its existing tool protocol.
	jsonOutput  bool
	diagnostics *modelDiagnostics
}

func property(value any, key string) any {
	root, _ := value.(*object)
	out, _ := root.get(key)
	return out
}

const noStructuredOutput = "LLM response did not contain structured output"

// anthropicToolInstruction asks for the result tool by name. Newer Claude
// models reject a forced tool_choice ("tool" or "any") with HTTP 400, so the
// request uses "auto" and steers from the system prompt instead.
func anthropicToolInstruction(instructions, name string) string {
	return instructions + "\n\nReturn the result only by calling the " + name + " tool exactly once."
}

// callModel sends the request in the provider's protocol and returns the
// structured result the model produced.
func (e *Extractor) callModel(ctx context.Context, p provider, call modelCall) (any, error) {
	encoded := make([]string, len(call.images))
	for i, image := range call.images {
		data, err := image.base64()
		if err != nil {
			return nil, err
		}
		encoded[i] = data
	}
	switch p.protocol {
	case protocolAnthropic:
		content := []any{}
		for i, image := range call.images {
			content = append(content, obj("type", "image", "source", obj(
				"type", "base64", "media_type", image.mimeType, "data", encoded[i])))
		}
		if call.includeText {
			content = append(content, obj("type", "text", "text", call.text))
		}
		request := obj(
			"model", p.model,
			"max_tokens", min(4000, max(1, call.maxTokens)),
			"system", call.instructions,
			"messages", []any{obj("role", "user", "content", content)},
		)
		if call.jsonOutput {
			request.set("system", call.instructions)
			output := obj("format", obj("type", "json_schema", "schema", call.schema))
			// Haiku 5.5 thinks by default, sharing max_tokens with its reply.
			// Simple translations do not need that extra budget. Keep these
			// model-specific options away from other configured models.
			if p.model == "claude-haiku-5-5" || strings.HasPrefix(p.model, "claude-haiku-5-5-") {
				request.set("thinking", obj("type", "disabled"))
				output.set("effort", "low")
			}
			request.set("output_config", output)
		} else {
			request.set("system", anthropicToolInstruction(call.instructions, call.name))
			request.set("tools", []any{obj(
				"name", call.name,
				"description", "Return the structured classification result.",
				"input_schema", call.schema)})
			request.set("tool_choice", obj("type", "auto", "disable_parallel_tool_use", true))
		}
		payload, err := e.requestLLM(ctx, p, stringify(request))
		if err != nil {
			return nil, err
		}
		if err = call.checkResponse(payload, p.protocol); err != nil {
			return nil, err
		}
		parts, _ := property(payload, "content").([]any)
		if call.jsonOutput {
			return parseTextParts(parts, "text")
		}
		for _, part := range parts {
			if property(part, "type") != "tool_use" || property(part, "name") != call.name {
				continue
			}
			// Only the first matching tool call counts, as Array#find did.
			switch input := property(part, "input").(type) {
			case *object, []any:
				return input, nil
			}
			return nil, fail(noStructuredOutput)
		}
		return nil, fail(noStructuredOutput)
	case protocolChat:
		content := []any{}
		if call.includeText {
			content = append(content, obj("type", "text", "text", call.text))
		}
		for i, image := range call.images {
			content = append(content, obj("type", "image_url", "image_url", obj(
				"url", "data:"+image.mimeType+";base64,"+encoded[i], "detail", "high")))
		}
		payload, err := e.requestLLM(ctx, p, stringify(obj(
			"model", p.model,
			"messages", []any{
				obj("role", "system", "content", call.instructions),
				obj("role", "user", "content", content),
			},
			"response_format", obj("type", "json_schema", "json_schema", obj(
				"name", call.name, "strict", true, "schema", call.schema)),
			"max_completion_tokens", call.maxTokens,
		)))
		if err != nil {
			return nil, err
		}
		if err = call.checkResponse(payload, p.protocol); err != nil {
			return nil, err
		}
		choices, _ := property(payload, "choices").([]any)
		var message any
		if len(choices) > 0 {
			message = property(property(choices[0], "message"), "content")
		}
		switch value := message.(type) {
		case string:
			return parseModelText(value)
		case []any:
			var texts []string
			for _, part := range value {
				if text, ok := property(part, "text").(string); ok && property(part, "type") == "text" {
					texts = append(texts, text)
				}
			}
			if joined := strings.Join(texts, "\n"); joined != "" {
				return parseModelText(joined)
			}
		}
		return nil, fail(noStructuredOutput)
	}
	content := []any{}
	if call.includeText {
		content = append(content, obj("type", "input_text", "text", call.text))
	}
	for i, image := range call.images {
		content = append(content, obj(
			"type", "input_image",
			"image_url", "data:"+image.mimeType+";base64,"+encoded[i],
			"detail", "high"))
	}
	payload, err := e.requestLLM(ctx, p, stringify(obj(
		"model", p.model,
		"store", false,
		"instructions", call.instructions,
		"input", []any{obj("role", "user", "content", content)},
		"text", obj("format", obj(
			"type", "json_schema", "name", call.name, "strict", true, "schema", call.schema)),
		"max_output_tokens", call.maxTokens,
	)))
	if err != nil {
		return nil, err
	}
	if err = call.checkResponse(payload, p.protocol); err != nil {
		return nil, err
	}
	if text, ok := property(payload, "output_text").(string); ok {
		return parseModelText(text)
	}
	outputs, _ := property(payload, "output").([]any)
	for _, output := range outputs {
		if property(output, "type") != "message" {
			continue
		}
		parts, _ := property(output, "content").([]any)
		for _, part := range parts {
			if property(part, "type") == "refusal" {
				// The refusal wording is the stored reason, as before; it is
				// bounded because it is provider-controlled text.
				return nil, fail(sliceUTF16(stringOr(property(part, "refusal"), "model refused"), 500))
			}
			if text, ok := property(part, "text").(string); ok && property(part, "type") == "output_text" {
				return parseModelText(text)
			}
		}
	}
	return nil, fail("OpenAI response did not contain structured output")
}

func parseTextParts(parts []any, kind string) (any, error) {
	var texts []string
	for _, part := range parts {
		if text, ok := property(part, "text").(string); ok && property(part, "type") == kind {
			texts = append(texts, text)
		}
	}
	if len(texts) == 0 {
		return nil, fail(noStructuredOutput)
	}
	return parseModelText(strings.Join(texts, "\n"))
}

func parseModelText(text string) (any, error) {
	value, err := parseJSON([]byte(text))
	if err != nil {
		return nil, fail("LLM response was not valid JSON")
	}
	return value, nil
}
