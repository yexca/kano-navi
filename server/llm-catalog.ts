import { LLM_MODEL_ID_PATTERN, normalizeLlmModelTags } from "./database.ts"
import { modelsEndpoint } from "../src/lib/llm-endpoints.ts"
import { providerHeaders, readLlmJson } from "./llm-http.ts"

export { modelsEndpoint } from "../src/lib/llm-endpoints.ts"

const MAX_REMOTE_MODELS = 2000
const MAX_MODEL_LIST_BYTES = 4 * 1024 * 1024

// Heuristics in the spirit of desktop LLM clients: they only pre-fill tags
// for newly discovered models, and the operator can always correct them.
const nonChatPattern =
  /(?:dall-e|gpt-image|imagen|flux|stable-diffusion|sdxl|midjourney|whisper|tts|speech|audio|transcribe|moderation|sora|veo|kling)/iu
const embeddingPattern =
  /(?:embed|embedding|bge-|\be5-|gte-|jina-|rerank|text-similarity)/iu
const imagePattern =
  /(?:vision|[-_.]vl\b|[-_]vl[-_]|\bvl-|llava|pixtral|qvq|gpt-4o|gpt-4\.1|gpt-4-turbo|gpt-5|\bo1(?!-mini)|\bo3|\bo4|chatgpt-4o|gemini|gemma-3|claude-3|claude-(?:sonnet|opus|haiku|fable|mythos)(?:[-_]|$)|claude-[a-z]*-?[45](?:[-_.]|$)|glm-4(?:\.\d)?v|glm-4\.5v|minicpm-v|internvl|grok-(?:\d+-)?vision|grok-4|llama-4|llama-3\.2-(?:11|90)b|mistral-(?:small|medium)-3|kimi-(?:vl|latest)|doubao.*vision|step-1v|qwen.*-?omni|qwen-?vl|qwen2(?:\.5)?-vl|qwen3-vl)/iu
const reasoningPattern =
  /(?:\bo1|\bo3|\bo4|reason|thinking|think\b|\br1\b|-r1|qwq|qvq|deepseek-r|gpt-5|claude-[a-z]*-?(?:5|4|3-7|3\.7)|grok-[34]|gemini-(?:2\.5|3)|magistral|kimi-k2|glm-4\.[5-9]|qwen3)/iu
const toolsPattern =
  /(?:gpt-4|gpt-5|gpt-3\.5-turbo|\bo3|\bo4|claude|gemini|qwen|deepseek-(?:chat|v3|v4)|glm-4|mistral|mixtral|grok|llama-3\.[1-3]|llama-4|kimi|moonshot|command-r|doubao|hunyuan|ernie)/iu

/** Suggest catalog tags for a model ID. `text` and `image` drive routing. */
export function inferModelTags(modelId) {
  const id = String(modelId || "")
  if (embeddingPattern.test(id)) return ["embedding"]
  if (nonChatPattern.test(id)) return []
  const tags = ["text"]
  if (imagePattern.test(id)) tags.push("image")
  if (reasoningPattern.test(id)) tags.push("reasoning")
  if (toolsPattern.test(id)) tags.push("tools")
  return normalizeLlmModelTags(tags)
}

/**
 * Parse common model-list payloads: OpenAI-style `{ data: [{ id }] }`,
 * `{ models: [{ id | name }] }`, or a bare array. Unknown or unsafe IDs are
 * dropped so that only values accepted by the catalog are returned.
 */
export function parseModelList(payload) {
  const items = Array.isArray(payload)
    ? payload
    : Array.isArray(payload?.data)
      ? payload.data
      : Array.isArray(payload?.models)
        ? payload.models
        : []
  const seen = new Set()
  const models = []
  for (const item of items) {
    const rawId =
      typeof item === "string" ? item : (item?.id ?? item?.name ?? item?.model)
    const id = String(rawId || "")
      .trim()
      .replace(/^models\//u, "")
    if (!LLM_MODEL_ID_PATTERN.test(id) || seen.has(id)) continue
    seen.add(id)
    const ownedBy =
      typeof item === "object" && item
        ? String(item.owned_by ?? item.ownedBy ?? item.owner ?? "")
            .trim()
            .slice(0, 120) || null
        : null
    const displayName =
      typeof item === "object" &&
      item &&
      typeof (item.display_name ?? (item.id && item.name)) === "string"
        ? (item.display_name ?? item.name).trim().slice(0, 120) || null
        : null
    models.push({ id, ownedBy, name: displayName })
    if (models.length >= MAX_REMOTE_MODELS) break
  }
  return models.sort((left, right) =>
    left.id.localeCompare(right.id, "en", { sensitivity: "base" }),
  )
}

/**
 * Fetch a provider's model list. Errors are reduced to fixed messages so an
 * upstream response body never reaches the API output or logs.
 */
export async function fetchRemoteModels(
  {
    baseUrl,
    apiKey,
    timeoutMs = 15_000,
    protocol = "openai-responses",
  }: { baseUrl: any; apiKey: any; timeoutMs?: number; protocol?: string },
  { fetchImpl = fetch }: { fetchImpl?: any } = {},
) {
  const controller = new AbortController()
  const timer = setTimeout(
    () => controller.abort(),
    Math.min(60_000, Math.max(1000, Number(timeoutMs) || 15_000)),
  )
  try {
    const collected = []
    const cursors = new Set<string>()
    for (let page = 0; page < 20; page += 1) {
      const url = new URL(modelsEndpoint(baseUrl))
      if (protocol === "anthropic-messages") {
        url.searchParams.set("limit", "100")
        const cursor = [...cursors].at(-1)
        if (cursor) url.searchParams.set("after_id", cursor)
      }
      const response = await fetchImpl(url.toString(), {
        method: "GET",
        redirect: "error",
        signal: controller.signal,
        headers: {
          accept: "application/json",
          ...providerHeaders(protocol, apiKey),
        },
      })
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const payload = await readLlmJson(response, MAX_MODEL_LIST_BYTES)
      collected.push(...parseModelList(payload))
      if (protocol !== "anthropic-messages" || !payload.has_more) {
        return parseModelList(collected)
      }
      const cursor = String(payload.last_id || "")
      if (!LLM_MODEL_ID_PATTERN.test(cursor) || cursors.has(cursor)) {
        throw new Error("invalid model list cursor")
      }
      cursors.add(cursor)
    }
    throw new Error("model list pagination limit exceeded")
  } catch (error) {
    const wrapped = new Error(
      controller.signal.aborted
        ? "model list request timed out"
        : "model list request failed",
    )
    wrapped.cause = error
    throw wrapped
  } finally {
    clearTimeout(timer)
  }
}
