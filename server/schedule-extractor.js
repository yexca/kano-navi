import crypto from "node:crypto"
import fs from "node:fs"

import {
  getAppSetting,
  getMediaAsset,
  getScheduleExtractionConfig,
  getScheduleExtraction,
  listMediaLinks,
  listScheduleCandidatePosts,
  replaceAutomaticEventsForSource,
  upsertScheduleExtraction,
} from "./database.js"
import { resolveMediaCachePath } from "./media-cache.js"

export const scheduleExtractorVersion = "openai-schedule-v1"
export const defaultScheduleModel = "gpt-4o-mini"

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

async function callOpenAiScheduleExtraction(
  post,
  images,
  {
    apiKey,
    model,
    fetchImpl = fetch,
    endpoint = "https://api.openai.com/v1/responses",
    timeoutMs = 30_000,
  },
) {
  const content = [
    {
      type: "input_text",
      text: `Source post published at ${post.publishedAt}. Source URL: ${post.url}\n\n${post.text}`,
    },
  ]
  for (const image of images) {
    const body = fs.readFileSync(image.filePath)
    content.push({
      type: "input_image",
      image_url: `data:${image.mimeType};base64,${body.toString("base64")}`,
      detail: "high",
    })
  }
  const timeout = Number.isFinite(Number(timeoutMs))
    ? Math.min(120_000, Math.max(1, Number(timeoutMs)))
    : 30_000
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
      body: JSON.stringify({
        model,
        store: false,
        instructions:
          "Extract only schedule entries explicitly supported by the Japanese post text or images. Interpret relative dates from the post publication time in Asia/Tokyo. Never invent a time. Use null time and timePrecision unknown when the date is known but the time is not. Return an empty events array when there is no concrete schedule.",
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
      }),
    })
  } catch (error) {
    if (controller.signal.aborted) throw new Error("OpenAI request timed out")
    throw error
  } finally {
    clearTimeout(timer)
  }
  if (!response.ok) {
    const message = String(await response.text()).slice(0, 500)
    throw new Error(
      `OpenAI ${response.status}: ${message || response.statusText}`,
    )
  }
  const payload = await response.json()
  return JSON.parse(extractResponseText(payload))
}

export async function extractSchedulePost(
  database,
  post,
  {
    apiKey = process.env.OPENAI_API_KEY || "",
    model = getAppSetting(
      database,
      "llm_model",
      process.env.OPENAI_MODEL || defaultScheduleModel,
    ),
    fetchImpl = fetch,
    endpoint,
    timeoutMs = Number(process.env.OPENAI_TIMEOUT_MS || 30_000),
  } = {},
) {
  if (!apiKey) return { status: "skipped", reason: "missing_api_key" }
  const images = readyPostImages(database, post.id)
  const contentFingerprint = sha256(
    JSON.stringify({
      text: post.text,
      images: images.map((image) => image.sha256),
    }),
  )
  const existing = getScheduleExtraction(database, {
    source: "x",
    sourceItemId: post.id,
    contentFingerprint,
    extractorVersion: scheduleExtractorVersion,
  })
  if (
    existing?.status === "success" &&
    String(existing.model || "") === String(model || "")
  ) {
    return { status: "cached", extractionId: existing.id }
  }

  const running = upsertScheduleExtraction(database, {
    source: "x",
    sourceItemId: post.id,
    contentFingerprint,
    extractorVersion: scheduleExtractorVersion,
    model,
    status: "running",
  })
  try {
    const result = await callOpenAiScheduleExtraction(post, images, {
      apiKey,
      model,
      fetchImpl,
      endpoint,
      timeoutMs,
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
      extractorVersion: scheduleExtractorVersion,
      model,
      status: "success",
      result,
    })
    return { status: "success", extractionId: running.id, ...replacement }
  } catch (error) {
    upsertScheduleExtraction(database, {
      source: "x",
      sourceItemId: post.id,
      contentFingerprint,
      extractorVersion: scheduleExtractorVersion,
      model,
      status: "failed",
      error: error.message,
    })
    return { status: "failed", extractionId: running.id, error: error.message }
  }
}

export async function extractPendingSchedules(database, options = {}) {
  const scheduleConfig = getScheduleExtractionConfig(database)
  if (!scheduleConfig.enabled) {
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
