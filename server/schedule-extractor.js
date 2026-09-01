import crypto from "node:crypto"
import fs from "node:fs"

import {
  getAppSetting,
  getMediaAsset,
  getScheduleExtractionConfig,
  getScheduleExtraction,
  getLlmProvider,
  getLlmProviderSecret,
  getLlmRouteProviders,
  listMediaLinks,
  listScheduleCandidatePosts,
  replaceAutomaticEventsForSource,
  updateLlmProviderStatus,
  upsertScheduleExtraction,
} from "./database.js"
import { resolveMediaCachePath } from "./media-cache.js"
import { decryptSecret } from "./secret-store.js"

export const scheduleExtractorVersion = "openai-schedule-v1"
export const defaultScheduleModel = "gpt-4o-mini"
const defaultOpenAiBaseUrl = "https://api.openai.com/v1"
const scheduleInstructions =
  "Extract only schedule entries explicitly supported by the Japanese post text or images. Interpret relative dates from the post publication time in Asia/Tokyo. Never invent a time. Use null time and timePrecision unknown when the date is known but the time is not. Return an empty events array when there is no concrete schedule."

const scheduleSchema = {
  type: "object",
  additionalProperties: false,
  properties: {
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
  },
  required: ["events"],
}

function sha256(value) {
  return crypto.createHash("sha256").update(value).digest("hex")
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

function readyPostImages(database, postId) {
  const supportedMimeTypes = new Set([
    "image/gif",
    "image/jpeg",
    "image/png",
    "image/webp",
  ])
  return listMediaLinks(database, {
    ownerType: "post",
    ownerId: String(postId),
  })
    .filter((link) => link.role === "post-image")
    .sort((a, b) => a.position - b.position)
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
  },
) {
  const provider = { protocol, baseUrl: baseUrl || defaultOpenAiBaseUrl }
  const resolvedEndpoint = endpoint || providerEndpoint(provider)
  const content = [
    {
      type: "input_text",
      text: `Source post published at ${post.publishedAt}. Source URL: ${post.url}\n\n${post.text}`,
    },
  ]
  for (const image of images) {
    content.push({
      type: "input_image",
      image_url: imageDataUrl(image),
      detail: "high",
    })
  }
  if (protocol === "openai-chat-completions") {
    const chatContent = [
      {
        type: "text",
        text: `Source post published at ${post.publishedAt}. Source URL: ${post.url}\n\n${post.text}`,
      },
      ...images.map((image) => ({
        type: "image_url",
        image_url: { url: imageDataUrl(image), detail: "high" },
      })),
    ]
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
  const apiKey = options.apiKey ?? process.env.OPENAI_API_KEY ?? ""
  if (!apiKey) return null
  return {
    id: "legacy-openai",
    name: "Legacy OpenAI",
    protocol: options.protocol || "openai-responses",
    baseUrl:
      options.baseUrl || process.env.OPENAI_BASE_URL || defaultOpenAiBaseUrl,
    endpoint: options.endpoint,
    model:
      options.model ||
      getAppSetting(
        database,
        "llm_model",
        process.env.OPENAI_MODEL || defaultScheduleModel,
      ),
    timeoutMs:
      options.timeoutMs ?? Number(process.env.OPENAI_TIMEOUT_MS || 30_000),
    maxRetries: 0,
    apiKey,
    persisted: false,
  }
}

function configuredProviders(database, options) {
  const hasExplicitProvider =
    options.apiKey != null ||
    options.endpoint != null ||
    options.model != null ||
    options.baseUrl != null ||
    options.protocol != null
  if (hasExplicitProvider) {
    const provider = legacyProvider(database, options)
    return provider ? [provider] : []
  }
  const config = getScheduleExtractionConfig(database)
  const ids = config.providerOrder.length
    ? config.providerOrder
    : getLlmRouteProviders(database, "schedule_vision")
  const providers = []
  for (const id of ids) {
    const row = getLlmProvider(database, id)
    if (!row || !row.enabled || !row.visionCapable) continue
    const secret = getLlmProviderSecret(database, id)
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
    } else if (id === "openai-default") {
      apiKey = process.env.OPENAI_API_KEY || null
    }
    if (!apiKey) continue
    providers.push({ ...row, apiKey, persisted: true })
  }
  if (!providers.length) {
    const fallback = legacyProvider(database, options)
    if (fallback) providers.push(fallback)
  }
  return providers
}

function providerExtractorVersion(provider) {
  return `${scheduleExtractorVersion}:${provider.id}:${provider.protocol}`
}

async function extractWithProvider(
  database,
  post,
  images,
  contentFingerprint,
  provider,
  { fetchImpl = fetch } = {},
) {
  const extractorVersion = providerExtractorVersion(provider)
  const existing = getScheduleExtraction(database, {
    source: "x",
    sourceItemId: post.id,
    contentFingerprint,
    extractorVersion,
  })
  if (
    existing?.status === "success" &&
    String(existing.model || "") === String(provider.model || "")
  ) {
    return {
      status: "cached",
      extractionId: existing.id,
      providerId: provider.id,
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
  const maxRetries = Math.min(3, Math.max(0, Number(provider.maxRetries) || 0))
  for (let attempt = 0; attempt <= maxRetries; attempt += 1) {
    try {
      const result = await callOpenAiScheduleExtraction(post, images, {
        apiKey: provider.apiKey,
        model: provider.model,
        protocol: provider.protocol,
        baseUrl: provider.baseUrl,
        endpoint: provider.endpoint,
        fetchImpl,
        timeoutMs: provider.timeoutMs,
      })
      const events = normalizeExtractedEvents(result, post)
      const replacement = replaceAutomaticEventsForSource(database, {
        source: "x",
        sourceItemId: post.id,
        extractionId: running.id,
        events,
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
        attempts: attempt + 1,
        ...replacement,
      }
    } catch (error) {
      lastError = error
      if (attempt < maxRetries) await wait(retryDelay(attempt))
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
      attempts: maxRetries + 1,
    }
  }
}

export async function extractSchedulePost(database, post, options = {}) {
  const providers = configuredProviders(database, options)
  if (!providers.length) return { status: "skipped", reason: "missing_api_key" }
  const images = readyPostImages(database, post.id)
  const contentFingerprint = sha256(
    JSON.stringify({
      text: post.text,
      images: images.map((image) => image.sha256),
    }),
  )
  const attempts = []
  for (const provider of providers) {
    const result = await extractWithProvider(
      database,
      post,
      images,
      contentFingerprint,
      provider,
      options,
    )
    attempts.push(result)
    if (result.status === "success" || result.status === "cached")
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
  if (!scheduleConfig.keywordEnabled) {
    return {
      attempted: 0,
      success: 0,
      cached: 0,
      skipped: 0,
      failed: 0,
      disabled: true,
    }
  }
  if (!scheduleConfig.visionEnabled) {
    return {
      attempted: 0,
      success: 0,
      cached: 0,
      skipped: 0,
      failed: 0,
      disabled: true,
    }
  }
  const posts = listScheduleCandidatePosts(database, {
    limit: options.limit || 20,
    keywords: options.keywords || scheduleConfig.keywords,
  })
  const summary = {
    attempted: 0,
    success: 0,
    cached: 0,
    skipped: 0,
    failed: 0,
    keywords: scheduleConfig.keywords,
    providers: scheduleConfig.providerOrder,
  }
  for (const post of posts) {
    const result = await extractSchedulePost(database, post, options)
    summary.attempted += 1
    summary[result.status] = (summary[result.status] || 0) + 1
    if (result.reason === "missing_api_key") break
  }
  return summary
}

export {
  callOpenAiScheduleExtraction,
  normalizeExtractedEvents,
  scheduleSchema,
}
