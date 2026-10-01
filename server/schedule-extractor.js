import crypto from "node:crypto"
import fs from "node:fs"

import {
  getAppSetting,
  getMediaAsset,
  getScheduleExtractionConfig,
  getScheduleExtraction,
  getLlmModel,
  getLlmProvider,
  getLlmProviderSecret,
  getScheduleRouteTargets,
  LLM_MAX_RETRIES,
  listMediaLinks,
  listScheduleCandidatePosts,
  normalizeLlmCapabilities,
  replaceAutomaticEventsForSource,
  SCHEDULE_BOARD_ROUTE,
  SCHEDULE_MESSAGE_ROUTE,
  SCHEDULE_VISION_ROUTE,
  updateLlmProviderStatus,
  upsertScheduleExtraction,
} from "./database.js"
import { resolveMediaCachePath } from "./media-cache.js"
import { decryptSecret } from "./secret-store.js"

export const scheduleExtractorVersion = "openai-schedule-v2"
export const defaultScheduleModel = "gpt-4o-mini"
const defaultOpenAiBaseUrl = "https://api.openai.com/v1"
const INPUT_MODES = new Set(["text", "image", "text_image"])
const DETECTION_TYPES = new Set(["board", "message"])
const CLASSIFICATIONS = new Set(["schedule", "not_schedule", "uncertain"])
const scheduleInstructions =
  "First classify whether this single Japanese X post is a concrete schedule notice. Return classification=not_schedule when it is ordinary conversation or has no concrete schedule, and classification=uncertain when the evidence is ambiguous. For classification=schedule, extract only entries explicitly supported by the post text or images. Interpret relative dates from the post publication time in Asia/Tokyo. Never invent a time. Use null time and timePrecision unknown when the date is known but the time is not. Return an empty events array for not_schedule."

const scheduleSchema = {
  type: "object",
  additionalProperties: false,
  properties: {
    classification: {
      type: "string",
      enum: ["schedule", "not_schedule", "uncertain"],
    },
    events: {
      type: "array",
      items: {
        type: "object",
        additionalProperties: false,
        properties: {
          title: { type: "string" },
          detail: { anyOf: [{ type: "string" }, { type: "null" }] },
          date: { type: "string" },
          time: { anyOf: [{ type: "string" }, { type: "null" }] },
          endTime: { anyOf: [{ type: "string" }, { type: "null" }] },
          timePrecision: {
            type: "string",
            enum: ["exact", "approximate", "unknown"],
          },
          status: { type: "string" },
          eventType: {
            type: "string",
            enum: ["event", "stream", "member", "release", "appearance"],
          },
          url: { anyOf: [{ type: "string" }, { type: "null" }] },
          confidence: { type: "number", minimum: 0, maximum: 1 },
          evidence: { type: "string" },
        },
        required: [
          "title",
          "detail",
          "date",
          "time",
          "endTime",
          "timePrecision",
          "status",
          "eventType",
          "url",
          "confidence",
          "evidence",
        ],
      },
    },
    confidence: { type: "number", minimum: 0, maximum: 1 },
    evidence: { type: "string" },
    action: {
      type: "string",
      enum: ["add", "update", "cancel", "none"],
    },
  },
  required: ["classification", "events", "confidence", "evidence", "action"],
}

function sha256(value) {
  return crypto.createHash("sha256").update(value).digest("hex")
}

function declaredPostMediaUrls(post) {
  const raw = post?.raw && typeof post.raw === "object" ? post.raw : null
  const values = [
    post?.mediaUrls,
    post?.media_urls,
    post?.mediaUrl,
    post?.media_url,
    raw?.mediaUrls,
    raw?.media_urls,
    raw?.mediaURLs,
  ]
  return [
    ...new Set(
      values
        .flatMap((value) => (Array.isArray(value) ? value : [value]))
        .filter((value) => typeof value === "string" && value.trim())
        .map((value) => value.trim()),
    ),
  ]
}

/**
 * Return the modality represented by the original post. Passing an images
 * array explicitly means that the caller wants to describe the supplied
 * payload; omitting it uses the post's declared media URLs instead.
 */
export function inputModeForPost(post, images) {
  const hasText = Boolean(String(post?.text || "").trim())
  const hasImages = Array.isArray(images)
    ? images.length > 0
    : declaredPostMediaUrls(post).length > 0
  if (hasText && hasImages) return "text_image"
  if (hasText) return "text"
  if (hasImages) return "image"
  return null
}

export function providerSupportsInput(provider, inputMode) {
  if (!INPUT_MODES.has(inputMode)) return false
  const capabilities = normalizeLlmCapabilities(
    provider?.capabilities,
    provider?.visionCapable !== false,
  )
  if (inputMode === "text_image") {
    return capabilities.includes("text") && capabilities.includes("image")
  }
  return capabilities.includes(inputMode)
}

function normalizeDetectionType(value) {
  const normalized = String(value || "board")
    .trim()
    .toLowerCase()
  return DETECTION_TYPES.has(normalized) ? normalized : "board"
}

function routeForDetectionType(detectionType) {
  return normalizeDetectionType(detectionType) === "message"
    ? SCHEDULE_MESSAGE_ROUTE
    : SCHEDULE_BOARD_ROUTE
}

function eventScopeForDetectionType(detectionType) {
  // Both detectors describe the same X source item. Keeping one source scope
  // lets a later not_schedule result retire an earlier automatic extraction
  // regardless of which route handled the post.
  normalizeDetectionType(detectionType)
  return "x"
}

function youtubeIdFromUrl(value) {
  if (!value) return null
  try {
    const url = new URL(value)
    if (url.hostname === "youtu.be") return url.pathname.slice(1) || null
    if (url.hostname.endsWith("youtube.com")) {
      return (
        url.searchParams.get("v") ||
        url.pathname.match(/\/(?:live|shorts)\/([^/]+)/u)?.[1] ||
        null
      )
    }
  } catch {
    return null
  }
  return null
}

function normalizePublicUrl(value, fallback) {
  if (!value) return fallback
  try {
    const url = new URL(value)
    return url.protocol === "https:" || url.protocol === "http:"
      ? url.toString()
      : fallback
  } catch {
    return fallback
  }
}

function validDate(value) {
  const match = String(value || "").match(/^(\d{4})-(\d{2})-(\d{2})$/u)
  if (!match) return false
  const date = new Date(
    Date.UTC(Number(match[1]), Number(match[2]) - 1, Number(match[3])),
  )
  return (
    date.getUTCFullYear() === Number(match[1]) &&
    date.getUTCMonth() === Number(match[2]) - 1 &&
    date.getUTCDate() === Number(match[3])
  )
}

function validTime(value) {
  return value == null || /^([01]\d|2[0-3]):[0-5]\d$/u.test(String(value))
}

function isoAtJapanTime(date, time) {
  if (!time) return null
  return new Date(`${date}T${time}:00+09:00`).toISOString()
}

function endIsoAtJapanTime(date, startTime, endTime) {
  if (!endTime) return null
  const start = startTime ? new Date(`${date}T${startTime}:00+09:00`) : null
  const end = new Date(`${date}T${endTime}:00+09:00`)
  if (start && end.getTime() < start.getTime())
    end.setUTCDate(end.getUTCDate() + 1)
  return end.toISOString()
}

function extractResponseText(payload) {
  if (typeof payload?.output_text === "string") return payload.output_text
  for (const output of payload?.output || []) {
    if (output.type !== "message") continue
    for (const content of output.content || []) {
      if (content.type === "refusal")
        throw new Error(content.refusal || "model refused")
      if (content.type === "output_text" && typeof content.text === "string") {
        return content.text
      }
    }
  }
  throw new Error("OpenAI response did not contain structured output")
}

function postImageState(database, post) {
  const supportedMimeTypes = new Set([
    "image/avif",
    "image/gif",
    "image/jpeg",
    "image/png",
    "image/webp",
  ])
  const links = listMediaLinks(database, {
    ownerType: "post",
    ownerId: String(post.id),
  })
    .filter((link) => link.role === "post-image")
    .sort((a, b) => a.position - b.position)
  const images = links
    .map((link) => {
      const asset = getMediaAsset(database, link.mediaId)
      const filePath = asset?.cachePath
        ? resolveMediaCachePath(asset.cachePath)
        : null
      if (
        asset?.status !== "ready" ||
        !asset.sha256 ||
        !supportedMimeTypes.has(asset.mimeType) ||
        !filePath ||
        !fs.existsSync(filePath)
      ) {
        return null
      }
      return { ...asset, filePath, position: link.position }
    })
    .filter(Boolean)
  return {
    hasImages: declaredPostMediaUrls(post).length > 0 || links.length > 0,
    images,
  }
}

function normalizeExtractedEvents(result, post) {
  if (!result || !Array.isArray(result.events)) {
    throw new Error("schedule extraction did not return an events array")
  }
  const timePrecisions = new Set(["exact", "approximate", "unknown"])
  const eventTypes = new Set([
    "event",
    "stream",
    "member",
    "release",
    "appearance",
  ])
  for (const event of result.events) {
    const confidence = Number(event?.confidence)
    const valid =
      event &&
      String(event.title || "").trim() &&
      validDate(event.date) &&
      validTime(event.time) &&
      validTime(event.endTime) &&
      !(event.endTime && !event.time) &&
      timePrecisions.has(event.timePrecision) &&
      eventTypes.has(event.eventType) &&
      Number.isFinite(confidence) &&
      confidence >= 0 &&
      confidence <= 1
    if (!valid) {
      throw new Error("schedule extraction contained an invalid event")
    }
  }
  const perDate = result.events.reduce((counts, event) => {
    counts.set(event.date, (counts.get(event.date) || 0) + 1)
    return counts
  }, new Map())

  return result.events.map((event) => {
    const title = String(event.title).trim().slice(0, 240)
    const time = event.time || null
    const sourceKey = `${event.date}T${time || "unknown"}:${sha256(title.toLowerCase()).slice(0, 10)}`
    const youtubeId = youtubeIdFromUrl(event.url)
    const dateId = String(event.date).replaceAll("-", "")
    const eventId = youtubeId
      ? `youtube-${youtubeId}`
      : perDate.get(event.date) === 1
        ? `x-schedule-${dateId}`
        : `x-schedule-${dateId}-${sha256(sourceKey).slice(0, 10)}`
    const startsAt = isoAtJapanTime(event.date, time)
    return {
      id: eventId,
      canonicalSource: youtubeId ? "youtube" : "x",
      canonicalSourceItemId: youtubeId || post.id,
      canonicalSourceKey: youtubeId ? "reservation" : sourceKey,
      source_key: sourceKey,
      title,
      detail: event.detail ? String(event.detail).trim().slice(0, 500) : null,
      starts_on: event.date,
      starts_at: startsAt,
      ends_at: endIsoAtJapanTime(event.date, time, event.endTime),
      timezone: "Asia/Tokyo",
      time_precision: time ? event.timePrecision : "unknown",
      status: String(event.status || "待确认")
        .trim()
        .slice(0, 80),
      event_type: event.eventType,
      url: normalizePublicUrl(event.url, post.url),
      is_upcoming: startsAt ? Date.parse(startsAt) >= Date.now() : true,
      confidence: Math.min(1, Math.max(0, Number(event.confidence) || 0)),
      raw: { evidence: String(event.evidence || "").slice(0, 1000) },
    }
  })
}

function inferredClassification(result) {
  if (typeof result?.classification === "string") {
    const normalized = result.classification.trim().toLowerCase()
    if (CLASSIFICATIONS.has(normalized)) return normalized
    throw new Error("schedule extraction returned an invalid classification")
  }
  return Array.isArray(result?.events) && result.events.length
    ? "schedule"
    : "not_schedule"
}

function normalizeExtractionResult(result, post) {
  const classification = inferredClassification(result)
  const events = normalizeExtractedEvents(
    { events: Array.isArray(result?.events) ? result.events : [] },
    post,
  )
  if (classification === "not_schedule" && events.length) {
    throw new Error("not_schedule extraction contained events")
  }
  if (classification === "uncertain" && events.length) {
    throw new Error("uncertain extraction contained events")
  }
  const confidenceValue =
    result?.confidence == null
      ? events.reduce(
          (maximum, event) => Math.max(maximum, event.confidence),
          0,
        )
      : Number(result.confidence)
  if (
    !Number.isFinite(confidenceValue) ||
    confidenceValue < 0 ||
    confidenceValue > 1
  ) {
    throw new Error("schedule extraction returned an invalid confidence")
  }
  const action = String(
    result?.action || (classification === "schedule" ? "add" : "none"),
  )
    .trim()
    .toLowerCase()
  if (!["add", "update", "cancel", "none"].includes(action)) {
    throw new Error("schedule extraction returned an invalid action")
  }
  return {
    classification,
    events,
    confidence: confidenceValue,
    evidence: String(result?.evidence || "")
      .trim()
      .slice(0, 1000),
    action,
  }
}

function providerEndpoint(provider) {
  const base = String(provider.baseUrl || defaultOpenAiBaseUrl).replace(
    /\/+$/u,
    "",
  )
  if (/(?:\/responses|\/chat\/completions)$/u.test(base)) return base
  return provider.protocol === "openai-chat-completions"
    ? `${base}/chat/completions`
    : `${base}/responses`
}

function imageDataUrl(image) {
  const body = fs.readFileSync(image.filePath)
  return `data:${image.mimeType};base64,${body.toString("base64")}`
}

function responseTextFromChat(payload) {
  const content = payload?.choices?.[0]?.message?.content
  if (typeof content === "string") return content
  if (Array.isArray(content)) {
    const text = content
      .filter((part) => part?.type === "text" && typeof part.text === "string")
      .map((part) => part.text)
      .join("\n")
    if (text) return text
  }
  throw new Error("LLM response did not contain structured output")
}

function timeoutValue(value) {
  return Number.isFinite(Number(value))
    ? Math.min(120_000, Math.max(1, Number(value)))
    : 30_000
}

function retryDelay(attempt) {
  return Math.min(1000, 100 * 2 ** Math.max(0, attempt))
}

function wait(milliseconds) {
  return milliseconds > 0
    ? new Promise((resolve) => setTimeout(resolve, milliseconds))
    : Promise.resolve()
}

async function requestLlm(
  endpoint,
  body,
  { apiKey, fetchImpl = fetch, timeoutMs },
) {
  const timeout = timeoutValue(timeoutMs)
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeout)
  let response
  try {
    response = await fetchImpl(endpoint, {
      method: "POST",
      signal: controller.signal,
      headers: {
        authorization: `Bearer ${apiKey}`,
        "content-type": "application/json",
      },
      body: JSON.stringify(body),
    })
  } catch (error) {
    if (controller.signal.aborted) throw new Error("OpenAI request timed out")
    throw error
  } finally {
    clearTimeout(timer)
  }
  if (!response.ok) {
    // Do not persist or log an upstream response body: providers sometimes
    // echo authorization material or other sensitive request fields.
    throw new Error(`LLM request failed (HTTP ${response.status})`)
  }
  return response.json()
}

async function callOpenAiScheduleExtraction(
  post,
  images,
  {
    apiKey,
    model,
    protocol = "openai-responses",
    fetchImpl = fetch,
    endpoint,
    baseUrl,
    timeoutMs = 30_000,
    inputMode,
  },
) {
  const normalizedImages = Array.isArray(images) ? images : []
  const resolvedInputMode =
    inputMode || inputModeForPost(post, normalizedImages)
  if (!INPUT_MODES.has(resolvedInputMode)) {
    throw new Error("schedule extraction requires text or image input")
  }
  const includeText = resolvedInputMode !== "image"
  const includeImages = resolvedInputMode !== "text"
  const provider = { protocol, baseUrl: baseUrl || defaultOpenAiBaseUrl }
  const resolvedEndpoint = endpoint || providerEndpoint(provider)
  const sourceText = `Source post published at ${post.publishedAt}. Source URL: ${post.url || ""}\n\n${post.text || ""}`
  const content = []
  if (includeText) {
    content.push({
      type: "input_text",
      text: sourceText,
    })
  }
  if (includeImages) {
    for (const image of normalizedImages) {
      content.push({
        type: "input_image",
        image_url: imageDataUrl(image),
        detail: "high",
      })
    }
  }
  if (protocol === "openai-chat-completions") {
    const chatContent = []
    if (includeText) chatContent.push({ type: "text", text: sourceText })
    if (includeImages) {
      chatContent.push(
        ...normalizedImages.map((image) => ({
          type: "image_url",
          image_url: { url: imageDataUrl(image), detail: "high" },
        })),
      )
    }
    const payload = await requestLlm(
      resolvedEndpoint,
      {
        model,
        messages: [
          { role: "system", content: scheduleInstructions },
          { role: "user", content: chatContent },
        ],
        response_format: {
          type: "json_schema",
          json_schema: {
            name: "kano_schedule_extraction",
            strict: true,
            schema: scheduleSchema,
          },
        },
        max_output_tokens: 4000,
      },
      { apiKey, fetchImpl, timeoutMs },
    )
    return JSON.parse(responseTextFromChat(payload))
  }
  const payload = await requestLlm(
    resolvedEndpoint,
    {
      model,
      store: false,
      instructions: scheduleInstructions,
      input: [{ role: "user", content }],
      text: {
        format: {
          type: "json_schema",
          name: "kano_schedule_extraction",
          strict: true,
          schema: scheduleSchema,
        },
      },
      max_output_tokens: 4000,
    },
    { apiKey, fetchImpl, timeoutMs },
  )
  return JSON.parse(extractResponseText(payload))
}

function legacyProvider(database, options) {
  // Explicit options are kept for isolated callers and tests. Normal sync
  // uses persisted providers and never reads a model credential from env.
  const apiKey = options.apiKey ?? ""
  if (!apiKey) return null
  const capabilities = normalizeLlmCapabilities(
    options.capabilities,
    options.visionCapable !== false,
  )
  return {
    id: "legacy-openai",
    name: "Legacy OpenAI",
    protocol: options.protocol || "openai-responses",
    baseUrl: options.baseUrl || defaultOpenAiBaseUrl,
    endpoint: options.endpoint,
    model:
      options.model ||
      getAppSetting(database, "llm_model", defaultScheduleModel),
    timeoutMs: options.timeoutMs ?? 30_000,
    maxRetries: options.maxRetries ?? 2,
    capabilities,
    visionCapable: capabilities.includes("image"),
    apiKey,
    persisted: false,
  }
}

function configuredProviders(database, options, inputMode, detectionType) {
  const hasExplicitProvider =
    options.apiKey != null ||
    options.endpoint != null ||
    options.model != null ||
    options.baseUrl != null ||
    options.protocol != null ||
    options.capabilities != null
  if (hasExplicitProvider) {
    const provider = legacyProvider(database, options)
    if (!provider) return { providers: [], reason: "missing_api_key" }
    if (!providerSupportsInput(provider, inputMode)) {
      return { providers: [], reason: "no_compatible_provider" }
    }
    return { providers: [provider], reason: null }
  }
  const route = routeForDetectionType(detectionType)
  const routeTargets = getScheduleRouteTargets(database)
  const targets =
    routeTargets[route]?.length > 0
      ? routeTargets[route]
      : routeTargets[SCHEDULE_VISION_ROUTE] || []
  const providers = []
  const apiKeys = new Map()
  let enabledProviderCount = 0
  let keyedProviderCount = 0
  for (const target of targets) {
    const row = getLlmProvider(database, target.providerId)
    if (!row || !row.enabled) continue
    // A target names a catalog model; an empty model follows the provider's
    // default model and its provider-level capabilities.
    let model = row.model
    let capabilities = row.capabilities
    if (target.modelId) {
      const catalogModel = getLlmModel(database, row.id, target.modelId)
      if (!catalogModel || !catalogModel.enabled) continue
      model = catalogModel.id
      capabilities = catalogModel.capabilities
    }
    if (!model) continue
    enabledProviderCount += 1
    if (!apiKeys.has(row.id)) {
      const secret = getLlmProviderSecret(database, row.id)
      let apiKey = null
      if (secret?.apiKeyCiphertext) {
        try {
          apiKey = decryptSecret(
            secret.apiKeyCiphertext,
            process.env.LLM_SECRETS_KEY || "",
          )
        } catch {
          apiKey = null
        }
      }
      apiKeys.set(row.id, apiKey)
    }
    const apiKey = apiKeys.get(row.id)
    if (!apiKey) continue
    keyedProviderCount += 1
    const candidate = {
      ...row,
      model,
      capabilities,
      visionCapable: capabilities.includes("image"),
      apiKey,
      persisted: true,
    }
    if (!providerSupportsInput(candidate, inputMode)) continue
    providers.push(candidate)
  }
  if (!providers.length && !enabledProviderCount) {
    const fallback = legacyProvider(database, options)
    if (fallback && providerSupportsInput(fallback, inputMode)) {
      return { providers: [fallback], reason: null }
    }
  }
  if (providers.length) return { providers, reason: null }
  return {
    providers: [],
    reason:
      keyedProviderCount > 0 ? "no_compatible_provider" : "missing_api_key",
  }
}

function providerExtractorVersion(provider, { detectionType, inputMode } = {}) {
  return `${scheduleExtractorVersion}:${normalizeDetectionType(detectionType)}:${inputMode}:${provider.id}:${provider.protocol}`
}

async function extractWithProvider(
  database,
  post,
  images,
  contentFingerprint,
  provider,
  { fetchImpl = fetch, detectionType = "board", inputMode, eventScope } = {},
) {
  const normalizedDetectionType = normalizeDetectionType(detectionType)
  const normalizedEventScope =
    eventScope || eventScopeForDetectionType(normalizedDetectionType)
  const extractorVersion = providerExtractorVersion(provider, {
    detectionType: normalizedDetectionType,
    inputMode,
  })
  const existing = getScheduleExtraction(database, {
    source: "x",
    sourceItemId: post.id,
    contentFingerprint,
    extractorVersion,
  })
  if (
    ["success", "uncertain"].includes(existing?.status) &&
    String(existing.model || "") === String(provider.model || "")
  ) {
    return {
      status: "cached",
      extractionId: existing.id,
      providerId: provider.id,
      classification: (() => {
        try {
          const cached = JSON.parse(existing.resultJson || "null")
          return inferredClassification(cached)
        } catch {
          return "schedule"
        }
      })(),
    }
  }
  const running = upsertScheduleExtraction(database, {
    source: "x",
    sourceItemId: post.id,
    contentFingerprint,
    extractorVersion,
    model: provider.model,
    status: "running",
  })
  let lastError = null
  // `maxRetries` is retained for compatibility, but every provider gets one
  // initial request plus two retries before priority failover.
  const maxRetries = LLM_MAX_RETRIES
  const maxAttempts = maxRetries + 1
  for (let attempt = 0; attempt < maxAttempts; attempt += 1) {
    try {
      const result = await callOpenAiScheduleExtraction(post, images, {
        apiKey: provider.apiKey,
        model: provider.model,
        protocol: provider.protocol,
        baseUrl: provider.baseUrl,
        endpoint: provider.endpoint,
        fetchImpl,
        timeoutMs: provider.timeoutMs,
        inputMode,
      })
      const normalized = normalizeExtractionResult(result, post)
      if (normalized.classification === "uncertain") {
        upsertScheduleExtraction(database, {
          source: "x",
          sourceItemId: post.id,
          contentFingerprint,
          extractorVersion,
          model: provider.model,
          status: "uncertain",
          result,
        })
        if (provider.persisted)
          updateLlmProviderStatus(database, provider.id, { status: "success" })
        return {
          status: "uncertain",
          extractionId: running.id,
          providerId: provider.id,
          classification: normalized.classification,
          attempts: attempt + 1,
        }
      }
      const replacement = replaceAutomaticEventsForSource(database, {
        source: normalizedEventScope,
        sourceItemId: post.id,
        extractionId: running.id,
        events: normalized.events,
      })
      upsertScheduleExtraction(database, {
        source: "x",
        sourceItemId: post.id,
        contentFingerprint,
        extractorVersion,
        model: provider.model,
        status: "success",
        result,
      })
      if (provider.persisted)
        updateLlmProviderStatus(database, provider.id, { status: "success" })
      return {
        status: "success",
        extractionId: running.id,
        providerId: provider.id,
        classification: normalized.classification,
        attempts: attempt + 1,
        ...replacement,
      }
    } catch (error) {
      lastError = error
      if (attempt < maxAttempts - 1) await wait(retryDelay(attempt))
    }
  }
  {
    const error = lastError || new Error("provider request failed")
    upsertScheduleExtraction(database, {
      source: "x",
      sourceItemId: post.id,
      contentFingerprint,
      extractorVersion,
      model: provider.model,
      status: "failed",
      error: error.message,
    })
    if (provider.persisted)
      updateLlmProviderStatus(database, provider.id, {
        status: "failed",
        error: error.message,
      })
    return {
      status: "failed",
      extractionId: running.id,
      providerId: provider.id,
      error: error.message,
      attempts: maxAttempts,
    }
  }
}

export async function extractSchedulePost(database, post, options = {}) {
  const detectionType = normalizeDetectionType(options.detectionType)
  const media = postImageState(database, post)
  const modalityPost = {
    ...post,
    // Preserve the source declaration even while the corresponding cache
    // asset is pending. This prevents image posts from bypassing media_pending.
    mediaUrls: media.hasImages ? ["linked-image"] : [],
  }
  const inputMode = options.inputMode || inputModeForPost(modalityPost)
  if (!inputMode) return { status: "skipped", reason: "no_content" }
  if (inputMode !== "text" && !media.images.length) {
    return { status: "skipped", reason: "media_pending" }
  }
  const configured = configuredProviders(
    database,
    options,
    inputMode,
    detectionType,
  )
  if (!configured.providers.length) {
    return { status: "skipped", reason: configured.reason }
  }
  const modelPost = {
    ...post,
    publishedAt: post.publishedAt || post.published_at,
    text: post.text || "",
  }
  const contentFingerprint = sha256(
    JSON.stringify({
      detectionType,
      inputMode,
      text: modelPost.text,
      images: media.images.map((image) => image.sha256),
    }),
  )
  const attempts = []
  for (const provider of configured.providers) {
    const result = await extractWithProvider(
      database,
      modelPost,
      media.images,
      contentFingerprint,
      provider,
      {
        ...options,
        detectionType,
        inputMode,
        eventScope: eventScopeForDetectionType(detectionType),
      },
    )
    attempts.push(result)
    if (
      result.status === "success" ||
      result.status === "cached" ||
      result.status === "uncertain"
    )
      return { ...result, attempts }
  }
  const last = attempts.at(-1)
  return {
    status: "failed",
    error: last?.error || "all configured LLM providers failed",
    attempts,
  }
}

export async function extractPendingSchedules(database, options = {}) {
  const scheduleConfig = getScheduleExtractionConfig(database)
  const disabled = {
    attempted: 0,
    success: 0,
    cached: 0,
    skipped: 0,
    failed: 0,
    disabled: true,
  }
  const boardEnabled =
    scheduleConfig.keywordEnabled && scheduleConfig.visionEnabled
  const messageEnabled = scheduleConfig.messageEnabled
  if (!boardEnabled && !messageEnabled) return disabled
  const limit = Math.min(100, Math.max(1, Number(options.limit) || 20))
  const candidates = []
  if (boardEnabled) {
    for (const post of listScheduleCandidatePosts(database, {
      limit,
      keywords: options.keywords || scheduleConfig.keywords,
      detectionType: "board",
    })) {
      candidates.push({ post, detectionType: "board" })
    }
  }
  if (messageEnabled) {
    for (const post of listScheduleCandidatePosts(database, {
      limit,
      keywords: options.keywords || scheduleConfig.keywords,
      detectionType: "message",
    })) {
      candidates.push({ post, detectionType: "message" })
    }
  }
  const seen = new Set()
  const uniqueCandidates = candidates
    .filter(({ post }) => {
      const id = String(post.id)
      if (seen.has(id)) return false
      seen.add(id)
      return true
    })
    .sort(
      (left, right) =>
        Date.parse(right.post.publishedAt || right.post.published_at || 0) -
        Date.parse(left.post.publishedAt || left.post.published_at || 0),
    )
  const summary = {
    attempted: 0,
    success: 0,
    cached: 0,
    skipped: 0,
    failed: 0,
    uncertain: 0,
    boardAttempted: 0,
    messageAttempted: 0,
    keywords: scheduleConfig.keywords,
    providers: scheduleConfig.providerOrder,
    providerOrders: scheduleConfig.providerOrders,
  }
  for (const { post, detectionType } of uniqueCandidates.slice(0, limit)) {
    const result = await extractSchedulePost(database, post, {
      ...options,
      detectionType,
    })
    summary.attempted += 1
    summary[`${detectionType}Attempted`] += 1
    summary[result.status] = (summary[result.status] || 0) + 1
  }
  return summary
}

export {
  callOpenAiScheduleExtraction,
  normalizeExtractedEvents,
  normalizeExtractionResult,
  scheduleSchema,
}
