// Package llmadmin serves the operator's LLM configuration API: providers,
// their model catalogs, per-route model targets and the schedule detection
// settings. It mirrors the wire contract of server/admin-api.ts.
//
// Provider credentials are write-only. Neither the plaintext nor the stored
// ciphertext is ever placed in a response, an error or a log line.
package llmadmin

import (
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
)

const (
	RouteVision  = "schedule_vision"
	RouteMessage = "schedule_message"
	RouteBoard   = "schedule_board"

	// MaxRetries is the fixed three-request provider contract.
	MaxRetries = 2
	// DefaultScheduleModel is the fallback for the legacy llm_model setting.
	DefaultScheduleModel = "gpt-4o-mini"

	protocolResponses = "openai-responses"
	protocolChat      = "openai-chat-completions"
	protocolAnthropic = "anthropic-messages"

	maxRemoteModels   = 2000
	maxModelListBytes = 4 << 20
	maxInferenceBytes = 4 << 20
)

var (
	scheduleRoutes          = []string{RouteMessage, RouteBoard, RouteVision}
	llmCapabilities         = []string{"text", "image"}
	llmModelTags            = []string{"text", "image", "reasoning", "tools", "embedding"}
	llmProtocols            = []string{protocolResponses, protocolChat, protocolAnthropic}
	defaultScheduleKeywords = []string{"schedule", "スケジュール", "配信予定", "週間予定", "今週の予定", "予定"}

	modelIDPattern    = regexp.MustCompile(`^[A-Za-z0-9@][A-Za-z0-9._:/@+-]{0,199}$`)
	providerIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	llmModelPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)
)

// RouteMap keeps the three schedule routes in the order the TypeScript
// server emitted them, which a Go map would not.
type RouteMap[T any] struct {
	Message T `json:"schedule_message"`
	Board   T `json:"schedule_board"`
	Vision  T `json:"schedule_vision"`
}

func (m RouteMap[T]) Get(route string) T {
	switch route {
	case RouteMessage:
		return m.Message
	case RouteBoard:
		return m.Board
	default:
		return m.Vision
	}
}

// RemoteModel is one entry of a provider's own model list.
type RemoteModel struct {
	ID      string  `json:"id"`
	OwnedBy *string `json:"ownedBy"`
	Name    *string `json:"name"`
}

// Heuristics in the spirit of desktop LLM clients: they only pre-fill tags
// for newly discovered models, and the operator can always correct them.
var (
	nonChatPattern   = regexp.MustCompile(`(?i)(?:dall-e|gpt-image|imagen|flux|stable-diffusion|sdxl|midjourney|whisper|tts|speech|audio|transcribe|moderation|sora|veo|kling)`)
	embeddingPattern = regexp.MustCompile(`(?i)(?:embed|embedding|bge-|\be5-|gte-|jina-|rerank|text-similarity)`)
	// The source pattern also has `\bo1(?!-mini)`; RE2 has no lookahead, so
	// that alternative is checked by o1Vision below.
	imagePattern     = regexp.MustCompile(`(?i)(?:vision|[-_.]vl\b|[-_]vl[-_]|\bvl-|llava|pixtral|qvq|gpt-4o|gpt-4\.1|gpt-4-turbo|gpt-5|\bo3|\bo4|chatgpt-4o|gemini|gemma-3|claude-3|claude-(?:sonnet|opus|haiku|fable|mythos)(?:[-_]|$)|claude-[a-z]*-?[45](?:[-_.]|$)|glm-4(?:\.\d)?v|glm-4\.5v|minicpm-v|internvl|grok-(?:\d+-)?vision|grok-4|llama-4|llama-3\.2-(?:11|90)b|mistral-(?:small|medium)-3|kimi-(?:vl|latest)|doubao.*vision|step-1v|qwen.*-?omni|qwen-?vl|qwen2(?:\.5)?-vl|qwen3-vl)`)
	o1Pattern        = regexp.MustCompile(`(?i)\bo1`)
	reasoningPattern = regexp.MustCompile(`(?i)(?:\bo1|\bo3|\bo4|reason|thinking|think\b|\br1\b|-r1|qwq|qvq|deepseek-r|gpt-5|claude-[a-z]*-?(?:5|4|3-7|3\.7)|grok-[34]|gemini-(?:2\.5|3)|magistral|kimi-k2|glm-4\.[5-9]|qwen3)`)
	toolsPattern     = regexp.MustCompile(`(?i)(?:gpt-4|gpt-5|gpt-3\.5-turbo|\bo3|\bo4|claude|gemini|qwen|deepseek-(?:chat|v3|v4)|glm-4|mistral|mixtral|grok|llama-3\.[1-3]|llama-4|kimi|moonshot|command-r|doubao|hunyuan|ernie)`)
)

func o1Vision(id string) bool {
	for _, match := range o1Pattern.FindAllStringIndex(id, -1) {
		rest := id[match[1]:]
		if len(rest) < 5 || !strings.EqualFold(rest[:5], "-mini") {
			return true
		}
	}
	return false
}

// InferModelTags suggests catalog tags for a model ID. Only text and image
// drive routing.
func InferModelTags(id string) []string {
	if embeddingPattern.MatchString(id) {
		return []string{"embedding"}
	}
	if nonChatPattern.MatchString(id) {
		return []string{}
	}
	tags := []string{"text"}
	if imagePattern.MatchString(id) || o1Vision(id) {
		tags = append(tags, "image")
	}
	if reasoningPattern.MatchString(id) {
		tags = append(tags, "reasoning")
	}
	if toolsPattern.MatchString(id) {
		tags = append(tags, "tools")
	}
	return tags
}

// collationWeight approximates the primary-strength English collation used
// by localeCompare(..., {sensitivity: "base"}) for the model ID alphabet:
// punctuation, then digits, then letters without case.
func collationWeight(r rune) int {
	switch {
	case r >= '0' && r <= '9':
		return 20 + int(r-'0')
	case r >= 'a' && r <= 'z':
		return 40 + int(r-'a')
	case r >= 'A' && r <= 'Z':
		return 40 + int(r-'A')
	}
	if index := strings.IndexRune("_-:.@/+", r); index >= 0 {
		return 1 + index
	}
	return 100 + int(r)
}

func compareModelIDs(left, right string) int {
	a, b := []rune(left), []rune(right)
	for i := 0; i < len(a) && i < len(b); i++ {
		if d := collationWeight(a[i]) - collationWeight(b[i]); d != 0 {
			return d
		}
	}
	return len(a) - len(b)
}

// finishModelList is the dedupe, cap and sort tail of parseModelList.
func finishModelList(models []RemoteModel) []RemoteModel {
	seen := make(map[string]bool, len(models))
	out := make([]RemoteModel, 0, len(models))
	for _, model := range models {
		if seen[model.ID] {
			continue
		}
		seen[model.ID] = true
		out = append(out, model)
		if len(out) >= maxRemoteModels {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return compareModelIDs(out[i].ID, out[j].ID) < 0 })
	return out
}

// parseModelList reads the common model-list payloads: OpenAI-style
// {data:[{id}]}, {models:[{id|name}]} or a bare array. Unknown or unsafe IDs
// are dropped so only values accepted by the catalog are returned.
func parseModelList(payload any) []RemoteModel {
	var items []any
	switch value := payload.(type) {
	case []any:
		items = value
	case map[string]any:
		if data, ok := value["data"].([]any); ok {
			items = data
		} else if models, ok := value["models"].([]any); ok {
			items = models
		}
	}
	models := make([]RemoteModel, 0, len(items))
	for _, item := range items {
		object, _ := item.(map[string]any)
		var rawID any
		if text, ok := item.(string); ok {
			rawID = text
		} else if object != nil {
			for _, key := range []string{"id", "name", "model"} {
				if object[key] != nil {
					rawID = object[key]
					break
				}
			}
		}
		id := strings.TrimPrefix(jsTrim(truthyString(rawID)), "models/")
		if !modelIDPattern.MatchString(id) {
			continue
		}
		model := RemoteModel{ID: id}
		if object != nil {
			var owner any = ""
			for _, key := range []string{"owned_by", "ownedBy", "owner"} {
				if object[key] != nil {
					owner = object[key]
					break
				}
			}
			model.OwnedBy = nullableString(utf16Prefix(jsTrim(jsString(owner)), 120))
			display := object["display_name"]
			if display == nil && jsTruthy(object["id"]) {
				display = object["name"]
			}
			if text, ok := display.(string); ok {
				model.Name = nullableString(utf16Prefix(jsTrim(text), 120))
			}
		}
		models = append(models, model)
	}
	return finishModelList(models)
}

var endpointSuffixPattern = regexp.MustCompile(`/(?:responses|chat/completions|messages|models)$`)

var providerAPIRoots = map[string]string{
	"openrouter.ai":                     "/api/v1",
	"generativelanguage.googleapis.com": "/v1beta/openai",
	"dashscope.aliyuncs.com":            "/compatible-mode/v1",
}

// providerAPIRoot mirrors src/lib/llm-endpoints.ts so the admin preview and
// the request use the same address. It contains no credentials.
func providerAPIRoot(baseURL string) string {
	base := strings.TrimRight(jsTrim(baseURL), "/")
	base = endpointSuffixPattern.ReplaceAllString(base, "")
	if base == "" {
		return ""
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || parsed.Opaque != "" {
		return base
	}
	path := strings.TrimRight(parsed.EscapedPath(), "/")
	if path == "" || path == "/api" || path == "/compatible-mode" {
		root, known := providerAPIRoots[strings.ToLower(parsed.Hostname())]
		if !known {
			root = path + "/v1"
		}
		return strings.TrimRight(parsed.Scheme+"://"+parsed.Host+root, "/")
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
	default:
		return base + "/responses"
	}
}

func modelsEndpoint(baseURL string) string {
	base := providerAPIRoot(baseURL)
	if base == "" {
		return ""
	}
	return base + "/models"
}

// listValues is the shared "JSON array, array, or delimited string" reader
// behind capability and tag normalization. ok is false when the value is
// neither a string nor an array.
func listValues(value any) (values []any, ok bool) {
	switch v := value.(type) {
	case []any:
		return v, true
	case string:
		var parsed any
		if json.Unmarshal([]byte(v), &parsed) == nil {
			if array, isArray := parsed.([]any); isArray {
				return array, true
			}
		}
		parts := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || isJSSpace(r) })
		values = make([]any, len(parts))
		for i, part := range parts {
			values[i] = part
		}
		return values, true
	default:
		return nil, false
	}
}

func vocabularyWord(item any) string {
	word := strings.ToLower(jsTrim(truthyString(item)))
	if word == "vision" {
		return "image"
	}
	return word
}

// normalizeCapabilities keeps the supported capabilities in first-seen order.
func normalizeCapabilities(values []any) []string {
	out := []string{}
	for _, item := range values {
		word := vocabularyWord(item)
		if slices.Contains(llmCapabilities, word) && !slices.Contains(out, word) {
			out = append(out, word)
		}
	}
	return out
}

// storedCapabilities reads capabilities_json with the legacy vision flag as
// the fallback for rows that predate the explicit set.
func storedCapabilities(raw string, legacyVision bool) []string {
	values, _ := listValues(raw)
	if out := normalizeCapabilities(values); len(out) > 0 {
		return out
	}
	if legacyVision {
		return slices.Clone(llmCapabilities)
	}
	return []string{"text"}
}

// normalizeModelTags keeps the known tags in catalog order.
func normalizeModelTags(value any) []string {
	values, ok := listValues(value)
	out := []string{}
	if !ok {
		return out
	}
	selected := make(map[string]bool, len(values))
	for _, item := range values {
		selected[vocabularyWord(item)] = true
	}
	for _, tag := range llmModelTags {
		if selected[tag] {
			out = append(out, tag)
		}
	}
	return out
}

func routingCapabilities(tags []string) []string {
	out := []string{}
	for _, tag := range tags {
		if slices.Contains(llmCapabilities, tag) {
			out = append(out, tag)
		}
	}
	return out
}
