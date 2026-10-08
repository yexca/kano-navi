import { Brain, Eye, Layers, Type, Wrench } from "lucide-react"

export { inferenceEndpoint, modelsEndpoint } from "../lib/llm-endpoints.ts"

// Display metadata for catalog tags. `text` and `image` are the routing
// capabilities enforced by the server; the others are descriptive.
export const modelTags = [
  { id: "text", tone: "sky", icon: Type, routing: true },
  { id: "image", tone: "berry", icon: Eye, routing: true },
  { id: "reasoning", tone: "lavender", icon: Brain, routing: false },
  { id: "tools", tone: "honey", icon: Wrench, routing: false },
  { id: "embedding", tone: "leaf", icon: Layers, routing: false },
]

export const scheduleRoutes = [
  {
    id: "schedule_board",
    labelKey: "admin.route.board",
    hintKey: "admin.route.boardHint",
  },
  {
    id: "schedule_message",
    labelKey: "admin.route.message",
    hintKey: "admin.route.messageHint",
  },
  {
    id: "schedule_vision",
    labelKey: "admin.route.legacy",
    hintKey: "admin.route.legacyHint",
  },
]

/*
 * Provider presets. They only pre-fill the connection form; nothing
 * is requested until an operator saves a key and fetches models. Every host
 * here is documented in scripts/privacy-allowlist.json.
 */
export const providerPresets = [
  {
    id: "openai",
    name: "OpenAI",
    baseUrl: "https://api.openai.com/v1",
    protocol: "openai-responses",
  },
  {
    id: "anthropic",
    name: "Anthropic",
    baseUrl: "https://api.anthropic.com",
    protocol: "anthropic-messages",
  },
  {
    id: "deepseek",
    name: "DeepSeek",
    baseUrl: "https://api.deepseek.com/v1",
    protocol: "openai-chat-completions",
  },
  {
    id: "openrouter",
    name: "OpenRouter",
    baseUrl: "https://openrouter.ai/api/v1",
    protocol: "openai-chat-completions",
  },
  {
    id: "gemini",
    name: "Gemini",
    baseUrl: "https://generativelanguage.googleapis.com/v1beta/openai",
    protocol: "openai-chat-completions",
  },
  {
    id: "custom",
    name: "",
    baseUrl: "",
    protocol: "openai-chat-completions",
  },
]

export function providerSlug(name, existingIds = []) {
  const base =
    String(name || "")
      .toLowerCase()
      .normalize("NFKD")
      .replace(/[^a-z0-9]+/gu, "-")
      .replace(/^-+|-+$/gu, "")
      .slice(0, 48) || "provider"
  let candidate = base
  let suffix = 2
  while (existingIds.includes(candidate)) {
    candidate = `${base}-${suffix}`
    suffix += 1
  }
  return candidate
}

/**
 * Resolve a route target against the catalog for display: which model it
 * will call and whether it can actually be used right now.
 */
export function describeTarget(target, providers, models) {
  const provider = providers.find((item) => item.id === target.providerId)
  const modelId = target.modelId || provider?.model || ""
  const model = models.find(
    (item) => item.providerId === target.providerId && item.id === modelId,
  )
  const capabilities = target.modelId
    ? model?.capabilities || []
    : provider?.capabilities || []
  let issue = null
  if (!provider) issue = "providerMissing"
  else if (!provider.enabled) issue = "providerDisabled"
  else if (!provider.apiKeyConfigured) issue = "keyMissing"
  else if (!modelId) issue = "modelMissing"
  else if (target.modelId && !model) issue = "modelMissing"
  else if (model && !model.enabled) issue = "modelDisabled"
  return {
    provider,
    model,
    modelId,
    followsDefault: !target.modelId,
    capabilities,
    tags: model?.tags || capabilities,
    issue,
  }
}
