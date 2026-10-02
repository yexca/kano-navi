import crypto from "node:crypto"
import fs from "node:fs"

import {
  getAppSetting,
  getMediaAsset,
  getScheduleAssetReview,
  getScheduleExtractionConfig,
  getScheduleExtraction,
  getLlmModel,
  getLlmProvider,
  getLlmProviderSecret,
  getScheduleRouteTargets,
  LLM_MAX_RETRIES,
  findCancellationTargets,
  getPostLlmState,
  listMediaLinks,
  listPostsRequestedForLlm,
  listScheduleCandidatePosts,
  normalizeLlmCapabilities,
  applyLlmCancellationJudgements,
  replaceAutomaticEventsForSource,
  SCHEDULE_BOARD_ROUTE,
  SCHEDULE_MESSAGE_ROUTE,
  SCHEDULE_VISION_ROUTE,
  updateLlmProviderStatus,
  upsertPostLlmState,
  listScheduleAssetReviews,
  upsertScheduleAssetReview,
  upsertScheduleExtraction,
} from "./database.js"
import { mediaIdForSourceUrl, resolveMediaCachePath } from "./media-cache.js"
import { decryptSecret } from "./secret-store.js"

export const scheduleExtractorVersion = "openai-schedule-v2"
export const defaultScheduleModel = "gpt-4o-mini"
const defaultOpenAiBaseUrl = "https://api.openai.com/v1"
const INPUT_MODES = new Set(["text", "image", "text_image"])
const DETECTION_TYPES = new Set(["board", "message"])
const CLASSIFICATIONS = new Set(["schedule", "not_schedule", "uncertain"])
const scheduleInstructions =
  "First classify whether this single Japanese X post is a concrete schedule notice. Return classification=not_schedule when it is ordinary conversation or has no concrete schedule, and classification=uncertain when the evidence is ambiguous. For classification=schedule, extract only entries explicitly supported by the post text or images. Interpret relative dates from the post publication time in Asia/Tokyo. Never invent a time. Use null time and timePrecision unknown when the date is known but the time is not. Set action=cancel only when the post is evidence that an existing schedule may have been cancelled; this is a judgement for review, never a deletion or definitive cancellation. Put the reason in the root reason field and cite the post wording in evidence. For cancellation, events should identify the affected date/title/time when possible. Return an empty events array for not_schedule."
const scheduleAssetVerificationInstructions =
  "Inspect the attached image only and decide whether it is a weekly or multi-day schedule board for the creator. Return schedule only when the image itself visibly contains a structured calendar or schedule table with days or dates and planned items. Return not_schedule for a stream thumbnail, promotional art, character art, a single video card, or any ordinary post image. Return uncertain when the image is unreadable or the evidence is insufficient. Do not infer a schedule from the source post wording. Cite visible image evidence briefly."

const scheduleAssetVerificationSchema = {
  type: "object",
  additionalProperties: false,
  properties: {
    classification: {
      type: "string",
      enum: ["schedule", "not_schedule", "uncertain"],
    },
    confidence: { type: "number", minimum: 0, maximum: 1 },
    evidence: { type: "string" },
    reason: { type: "string" },
  },
  required: ["classification", "confidence", "evidence", "reason"],
}

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
    reason: { anyOf: [{ type: "string" }, { type: "null" }] },
    action: {
      type: "string",
      enum: ["add", "update", "cancel", "none"],
    },
  },
  required: [
    "classification",
    "events",
    "confidence",
    "evidence",
    "reason",
    "action",
  ],
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
  // lets either route update the same automatic event identity.
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
    reason: String(result?.reason || "")
      .trim()
      .slice(0, 500),
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

function normalizeScheduleAssetVerification(result) {
  const classification = String(result?.classification || "")
    .trim()
    .toLowerCase()
  if (!CLASSIFICATIONS.has(classification)) {
    throw new Error(
      "schedule image verification returned an invalid classification",
    )
  }
  const confidence = Number(result?.confidence)
  if (!Number.isFinite(confidence) || confidence < 0 || confidence > 1) {
    throw new Error(
      "schedule image verification returned an invalid confidence",
    )
  }
  return {
    classification,
    confidence,
    evidence: String(result?.evidence || "")
      .trim()
      .slice(0, 1000),
    reason: String(result?.reason || "")
      .trim()
      .slice(0, 500),
  }
}

async function callOpenAiScheduleAssetVerification(
  asset,
  image,
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
  const sourceText = `Candidate schedule image. Source URL: ${asset.sourceUrl || ""}`
  if (protocol === "openai-chat-completions") {
    const payload = await requestLlm(
      resolvedEndpoint,
      {
        model,
        messages: [
          { role: "system", content: scheduleAssetVerificationInstructions },
          {
            role: "user",
            content: [
              { type: "text", text: sourceText },
              {
                type: "image_url",
                image_url: { url: imageDataUrl(image), detail: "high" },
              },
            ],
          },
        ],
        response_format: {
          type: "json_schema",
          json_schema: {
            name: "kano_schedule_asset_verification",
            strict: true,
            schema: scheduleAssetVerificationSchema,
          },
        },
        max_output_tokens: 800,
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
      instructions: scheduleAssetVerificationInstructions,
      input: [
        {
          role: "user",
          content: [
            { type: "input_text", text: sourceText },
            {
              type: "input_image",
              image_url: imageDataUrl(image),
              detail: "high",
            },
          ],
        },
      ],
      text: {
        format: {
          type: "json_schema",
          name: "kano_schedule_asset_verification",
          strict: true,
          schema: scheduleAssetVerificationSchema,
        },
      },
      max_output_tokens: 800,
    },
    { apiKey, fetchImpl, timeoutMs },
  )
  return JSON.parse(extractResponseText(payload))
}

function scheduleAssetImage(database, asset) {
  const mediaId = mediaIdForSourceUrl(asset?.url)
  const media = mediaId ? getMediaAsset(database, mediaId) : null
  const filePath = media?.cachePath
    ? resolveMediaCachePath(media.cachePath)
    : null
  if (
    media?.status !== "ready" ||
    !media.sha256 ||
    !media.mimeType?.startsWith("image/") ||
    !filePath ||
    !fs.existsSync(filePath)
  ) {
    return null
  }
  return { ...media, filePath }
}

export async function verifyScheduleAsset(
  database,
  assetId,
  { fetchImpl = fetch, force = false } = {},
) {
  const review = getScheduleAssetReview(database, assetId)
  if (!review) return { status: "missing", assetId: String(assetId) }
  if (
    !force &&
    ["schedule", "not_schedule", "uncertain"].includes(review.llmStatus)
  ) {
    return {
      status: "cached",
      assetId: review.id,
      classification: review.llmStatus,
      confidence: review.llmConfidence,
    }
  }
  if (!review.sourceMatchesBoard) {
    // Older snapshots promoted any keyword post; such an image can only be
    // published by a manual label, so it is not worth an image-model call.
    upsertScheduleAssetReview(database, {
      assetId: review.id,
      llmStatus: "skipped",
      llmReason: "source_not_board",
      llmEvidence: null,
      llmModel: null,
      llmCheckedAt: null,
      manualStatus: review.manualStatus,
      manualReason: review.manualReason,
      manualCheckedAt: review.manualCheckedAt,
    })
    return { status: "skipped", assetId: review.id, reason: "source_not_board" }
  }
  const image = scheduleAssetImage(database, review)
  if (!image) {
    upsertScheduleAssetReview(database, {
      assetId: review.id,
      llmStatus: "skipped",
      llmReason: "media_pending",
      llmEvidence: null,
      llmModel: null,
      llmCheckedAt: null,
      manualStatus: review.manualStatus,
      manualReason: review.manualReason,
      manualCheckedAt: review.manualCheckedAt,
    })
    return { status: "skipped", assetId: review.id, reason: "media_pending" }
  }
  const configured = configuredProviders(database, {}, "image", "board")
  if (!configured.providers.length) {
    upsertScheduleAssetReview(database, {
      assetId: review.id,
      llmStatus: "skipped",
      llmReason: configured.reason,
      llmEvidence: null,
      llmModel: null,
      llmCheckedAt: null,
      manualStatus: review.manualStatus,
      manualReason: review.manualReason,
      manualCheckedAt: review.manualCheckedAt,
    })
    return { status: "skipped", assetId: review.id, reason: configured.reason }
  }
  upsertScheduleAssetReview(database, {
    assetId: review.id,
    llmStatus: "running",
    llmReason: null,
    llmEvidence: null,
    llmModel: null,
    llmCheckedAt: null,
    manualStatus: review.manualStatus,
    manualReason: review.manualReason,
    manualCheckedAt: review.manualCheckedAt,
  })
  let lastError = null
  for (const provider of configured.providers) {
    for (let attempt = 0; attempt <= LLM_MAX_RETRIES; attempt += 1) {
      try {
        const result = normalizeScheduleAssetVerification(
          await callOpenAiScheduleAssetVerification(review, image, {
            apiKey: provider.apiKey,
            model: provider.model,
            protocol: provider.protocol,
            baseUrl: provider.baseUrl,
            endpoint: provider.endpoint,
            fetchImpl,
            timeoutMs: provider.timeoutMs,
          }),
        )
        upsertScheduleAssetReview(database, {
          assetId: review.id,
          llmStatus: result.classification,
          llmConfidence: result.confidence,
          llmReason: result.reason,
          llmEvidence: result.evidence,
          llmModel: provider.model,
          llmCheckedAt: new Date().toISOString(),
          manualStatus: review.manualStatus,
          manualReason: review.manualReason,
          manualCheckedAt: review.manualCheckedAt,
        })
        if (provider.persisted)
          updateLlmProviderStatus(database, provider.id, { status: "success" })
        return {
          status: "success",
          assetId: review.id,
          classification: result.classification,
          confidence: result.confidence,
          providerId: provider.id,
        }
      } catch (error) {
        lastError = error
        if (attempt < LLM_MAX_RETRIES) await wait(retryDelay(attempt))
      }
    }
  }
  const message = lastError?.message || "all configured LLM providers failed"
  upsertScheduleAssetReview(database, {
    assetId: review.id,
    llmStatus: "failed",
    llmReason: message,
    llmEvidence: null,
    llmModel: null,
    llmCheckedAt: new Date().toISOString(),
    manualStatus: review.manualStatus,
    manualReason: review.manualReason,
    manualCheckedAt: review.manualCheckedAt,
  })
  return { status: "failed", assetId: review.id, error: message }
}

export async function verifyPendingScheduleAssets(
  database,
  { limit = 20, force = false, fetchImpl = fetch } = {},
) {
  const candidates = listScheduleAssetReviews(database, {
    limit: Math.min(200, Math.max(1, Number(limit) || 20)),
  }).filter(
    (asset) =>
      asset.manualStatus === "unreviewed" &&
      (force ||
        !["schedule", "not_schedule", "uncertain"].includes(asset.llmStatus)),
  )
  const unique = [
    ...new Map(
      candidates.map((asset) => [asset.url || asset.id, asset]),
    ).values(),
  ]
  const summary = {
    attempted: 0,
    success: 0,
    cached: 0,
    skipped: 0,
    failed: 0,
    uncertain: 0,
    schedule: 0,
    notSchedule: 0,
  }
  for (const asset of unique) {
    const result = await verifyScheduleAsset(database, asset.id, {
      fetchImpl,
      force,
    })
    summary.attempted += 1
    summary[result.status] = (summary[result.status] || 0) + 1
    if (result.classification === "uncertain") summary.uncertain += 1
    if (result.classification === "schedule") summary.schedule += 1
    if (result.classification === "not_schedule") summary.notSchedule += 1
    if (result.status === "success" || result.status === "cached") {
      const verified = getScheduleAssetReview(database, asset.id)
      for (const duplicate of candidates.filter(
        (candidate) =>
          candidate.id !== asset.id &&
          candidate.url &&
          candidate.url === asset.url,
      )) {
        const current = getScheduleAssetReview(database, duplicate.id)
        upsertScheduleAssetReview(database, {
          assetId: duplicate.id,
          llmStatus: result.classification,
          llmConfidence: result.confidence,
          llmReason: verified?.llmReason,
          llmEvidence: verified?.llmEvidence,
          llmModel: verified?.llmModel,
          llmCheckedAt: verified?.llmCheckedAt || new Date().toISOString(),
          manualStatus: current?.manualStatus,
          manualReason: current?.manualReason,
          manualCheckedAt: current?.manualCheckedAt,
        })
      }
    }
  }
  return summary
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

function markPostLlm(database, postId, state = {}) {
  if (!postId) return null
  const previous = getPostLlmState(database, postId)
  return upsertPostLlmState(database, {
    postId,
    ...state,
    reprocessRequested:
      state.reprocessRequested ?? previous?.reprocessRequested ?? false,
  })
}

async function extractWithProvider(
  database,
  post,
  images,
  contentFingerprint,
  provider,
  {
    fetchImpl = fetch,
    detectionType = "board",
    inputMode,
    eventScope,
    force = false,
    route,
  } = {},
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
    !force &&
    ["success", "uncertain"].includes(existing?.status) &&
    String(existing.model || "") === String(provider.model || "")
  ) {
    markPostLlm(database, post.id, {
      status: existing.status,
      route,
      lastProcessedAt: existing.updatedAt || new Date().toISOString(),
      lastExtractionId: existing.id,
      lastError: null,
      reprocessRequested: false,
    })
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
  markPostLlm(database, post.id, {
    status: "running",
    route,
    lastAttemptAt: new Date().toISOString(),
    lastError: null,
    reprocessRequested: false,
  })
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
        markPostLlm(database, post.id, {
          status: "uncertain",
          route,
          lastProcessedAt: new Date().toISOString(),
          lastExtractionId: running.id,
          lastError: null,
          reprocessRequested: false,
        })
        return {
          status: "uncertain",
          extractionId: running.id,
          providerId: provider.id,
          classification: normalized.classification,
          attempts: attempt + 1,
        }
      }
      let replacement = { upserted: 0, retired: 0 }
      let cancellation = null
      if (normalized.action === "cancel") {
        const targets = findCancellationTargets(database, normalized.events)
        cancellation = applyLlmCancellationJudgements(database, {
          sourceItemId: post.id,
          reason: normalized.reason || normalized.evidence,
          evidence: normalized.evidence,
          confidence: normalized.confidence,
          targets,
        })
      } else if (normalized.classification === "schedule") {
        replacement = replaceAutomaticEventsForSource(database, {
          source: normalizedEventScope,
          sourceItemId: post.id,
          extractionId: running.id,
          events: normalized.events,
        })
      }
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
      markPostLlm(database, post.id, {
        status: "success",
        route,
        lastProcessedAt: new Date().toISOString(),
        lastExtractionId: running.id,
        lastError: null,
        reprocessRequested: false,
      })
      return {
        status: "success",
        extractionId: running.id,
        providerId: provider.id,
        classification: normalized.classification,
        attempts: attempt + 1,
        ...(cancellation ? { cancellation } : {}),
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
    markPostLlm(database, post.id, {
      status: "failed",
      route,
      lastAttemptAt: new Date().toISOString(),
      lastExtractionId: running.id,
      lastError: error.message,
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
  const route = routeForDetectionType(detectionType)
  const previousState = getPostLlmState(database, post.id)
  const force = Boolean(options.force || previousState?.reprocessRequested)
  const media = postImageState(database, post)
  const modalityPost = {
    ...post,
    // Preserve the source declaration even while the corresponding cache
    // asset is pending. This prevents image posts from bypassing media_pending.
    mediaUrls: media.hasImages ? ["linked-image"] : [],
  }
  const inputMode = options.inputMode || inputModeForPost(modalityPost)
  if (!inputMode) {
    markPostLlm(database, post.id, {
      status: "skipped",
      route,
      lastError: "no_content",
      reprocessRequested: force,
    })
    return { status: "skipped", reason: "no_content" }
  }
  if (inputMode !== "text" && !media.images.length) {
    markPostLlm(database, post.id, {
      status: "skipped",
      route,
      lastError: "media_pending",
      reprocessRequested: force,
    })
    return { status: "skipped", reason: "media_pending" }
  }
  const configured = configuredProviders(
    database,
    options,
    inputMode,
    detectionType,
  )
  if (!configured.providers.length) {
    markPostLlm(database, post.id, {
      status: "skipped",
      route,
      lastError: configured.reason,
      reprocessRequested: force,
    })
    return { status: "skipped", reason: configured.reason }
  }
  markPostLlm(database, post.id, {
    status: "running",
    route,
    lastAttemptAt: new Date().toISOString(),
    lastError: null,
    reprocessRequested: false,
  })
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
        force,
        route,
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

/**
 * The route an operator asked for wins. Without one, a post that the keyword
 * paths also selected keeps that route, and any other post follows its own
 * modality: image posts go to the board route, text posts to the message one.
 */
function requestedDetectionType(post, keywordDetectionType) {
  if (post.llmRoute === SCHEDULE_MESSAGE_ROUTE) return "message"
  if (post.llmRoute === SCHEDULE_BOARD_ROUTE) return "board"
  if (keywordDetectionType) return keywordDetectionType
  return declaredPostMediaUrls(post).length ? "board" : "message"
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
  const limit = Math.min(100, Math.max(1, Number(options.limit) || 20))
  const requestedPosts = listPostsRequestedForLlm(database, { limit })
  if (!boardEnabled && !messageEnabled && !requestedPosts.length)
    return disabled
  // A post selected by the board path is not sent again through the message
  // path in the same scan.
  const keywordCandidates = new Map()
  for (const [enabled, detectionType] of [
    [boardEnabled, "board"],
    [messageEnabled, "message"],
  ]) {
    if (!enabled) continue
    for (const post of listScheduleCandidatePosts(database, {
      limit,
      keywords: options.keywords || scheduleConfig.keywords,
      detectionType,
    })) {
      const id = String(post.id)
      if (!keywordCandidates.has(id))
        keywordCandidates.set(id, { post, detectionType, force: false })
    }
  }
  // Operator reprocess requests run before keyword candidates so a full scan
  // limit cannot starve them, and they bypass the keyword heuristic.
  const requested = requestedPosts.map((post) => ({
    post,
    detectionType: requestedDetectionType(
      post,
      keywordCandidates.get(String(post.id))?.detectionType,
    ),
    force: true,
  }))
  const requestedIds = new Set(requested.map(({ post }) => String(post.id)))
  const publishedAt = ({ post }) =>
    Date.parse(post.publishedAt || post.published_at || 0)
  const uniqueCandidates = [
    ...requested,
    ...[...keywordCandidates.values()]
      .filter(({ post }) => !requestedIds.has(String(post.id)))
      .sort((left, right) => publishedAt(right) - publishedAt(left)),
  ]
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
  for (const { post, detectionType, force } of uniqueCandidates.slice(
    0,
    limit,
  )) {
    const result = await extractSchedulePost(database, post, {
      ...options,
      detectionType,
      force,
    })
    summary.attempted += 1
    summary[`${detectionType}Attempted`] += 1
    summary[result.status] = (summary[result.status] || 0) + 1
  }
  const assetSummary = boardEnabled
    ? await verifyPendingScheduleAssets(database, {
        limit: options.assetLimit || limit,
        force: Boolean(options.forceAssetVerification),
        fetchImpl: options.fetchImpl || fetch,
      })
    : null
  return assetSummary
    ? { ...summary, assetVerification: assetSummary }
    : summary
}

export {
  callOpenAiScheduleExtraction,
  callOpenAiScheduleAssetVerification,
  normalizeExtractedEvents,
  normalizeExtractionResult,
  normalizeScheduleAssetVerification,
  scheduleSchema,
  scheduleAssetVerificationSchema,
}
