import fs from "node:fs"
import path from "node:path"
import express from "express"

import {
  createManualEvent,
  deleteManualEvent,
  getFeaturedVideoId,
  getAppSetting,
  getEvent,
  getScheduleExtractionConfig,
  listAdminVideos,
  listAdminEvents,
  listProfileMedia,
  getProfileMedia,
  selectProfileMedia,
  upsertProfileMediaCandidate,
  setAppSetting,
  setFeaturedVideoId,
  updateManualEvent,
} from "./database.js"
import { createAdminAuth } from "./admin-auth.js"
import { defaultScheduleModel } from "./schedule-extractor.js"
import {
  avatarMediaDirectory,
  isAllowedMediaMimeType,
  legacyMediaCacheDirectory,
  resolveMediaCachePath,
  xMediaDirectory,
  youtubeMediaDirectory,
} from "./media-cache.js"
import {
  discoverProfileMedia,
  downloadProfileMedia,
  isProfileSlot,
  isProfileSource,
  maxProfileImageBytes,
  saveUploadedProfileMedia,
} from "./profile-media.js"

class AdminInputError extends Error {}

function text(value, { name, required = false, max = 500 } = {}) {
  const normalized = value == null ? "" : String(value).trim()
  if (required && !normalized) throw new AdminInputError(`${name} is required`)
  if (normalized.length > max) throw new AdminInputError(`${name} is too long`)
  return normalized || null
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

const japanDateFormatter = new Intl.DateTimeFormat("en-CA", {
  timeZone: "Asia/Tokyo",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
})

function iso(value, { name, required = false } = {}) {
  if (value == null || value === "") {
    if (required) throw new AdminInputError(`${name} is required`)
    return null
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime()))
    throw new AdminInputError(`${name} is invalid`)
  return date.toISOString()
}

function publicUrl(value) {
  const normalized = text(value, { name: "url", max: 2000 })
  if (!normalized) return null
  try {
    const url = new URL(normalized)
    if (url.protocol !== "https:" && url.protocol !== "http:") {
      throw new AdminInputError("url must use HTTP or HTTPS")
    }
    return url.toString()
  } catch (error) {
    if (error instanceof AdminInputError) throw error
    throw new AdminInputError("url is invalid")
  }
}

function booleanValue(value, name) {
  if (typeof value === "boolean") return value
  const normalized = String(value ?? "")
    .trim()
    .toLowerCase()
  if (["1", "true", "yes", "on"].includes(normalized)) return true
  if (["0", "false", "no", "off"].includes(normalized)) return false
  throw new AdminInputError(`${name} must be a boolean`)
}

function scheduleKeywords(value) {
  const values = Array.isArray(value)
    ? value
    : String(value ?? "").split(/[\n,，]+/u)
  const normalized = [
    ...new Set(values.map((item) => String(item || "").trim()).filter(Boolean)),
  ]
  if (normalized.length > 30)
    throw new AdminInputError("scheduleKeywords contains too many entries")
  if (normalized.some((keyword) => keyword.length > 80))
    throw new AdminInputError(
      "scheduleKeywords contains an entry that is too long",
    )
  return normalized
}

function configPayload(database, openAiKeyConfigured, updatedAt = null) {
  const schedule = getScheduleExtractionConfig(database)
  return {
    llmModel: getAppSetting(
      database,
      "llm_model",
      process.env.OPENAI_MODEL || defaultScheduleModel,
    ),
    openAiKeyConfigured,
    scheduleExtractionEnabled: schedule.enabled,
    scheduleKeywords: schedule.keywords,
    featuredVideoId: getFeaturedVideoId(database),
    ...(updatedAt ? { updatedAt } : {}),
  }
}

function eventInput(body = {}) {
  const startsOn = text(body.startsOn ?? body.starts_on, {
    name: "startsOn",
    required: true,
    max: 10,
  })
  if (!validDate(startsOn)) throw new AdminInputError("startsOn is invalid")
  const startsAt = iso(body.startsAt ?? body.starts_at, { name: "startsAt" })
  if (startsAt && japanDateFormatter.format(new Date(startsAt)) !== startsOn) {
    throw new AdminInputError("startsAt must fall on startsOn in Asia/Tokyo")
  }
  const endsAt = iso(body.endsAt ?? body.ends_at, { name: "endsAt" })
  if (endsAt && !startsAt) {
    throw new AdminInputError("endsAt requires startsAt")
  }
  if (startsAt && endsAt && Date.parse(endsAt) < Date.parse(startsAt)) {
    throw new AdminInputError("endsAt must not be earlier than startsAt")
  }
  const eventType =
    text(body.eventType ?? body.event_type, {
      name: "eventType",
      max: 32,
    }) || "event"
  if (
    !["event", "stream", "member", "release", "appearance"].includes(eventType)
  ) {
    throw new AdminInputError("eventType is invalid")
  }
  const timePrecision = startsAt
    ? text(body.timePrecision ?? body.time_precision, {
        name: "timePrecision",
        max: 20,
      }) || "exact"
    : "unknown"
  if (!["exact", "approximate", "unknown"].includes(timePrecision)) {
    throw new AdminInputError("timePrecision is invalid")
  }
  return {
    title: text(body.title, { name: "title", required: true, max: 240 }),
    detail: text(body.detail, { name: "detail", max: 500 }),
    starts_on: startsOn,
    starts_at: startsAt,
    ends_at: endsAt,
    timezone: "Asia/Tokyo",
    time_precision: timePrecision,
    status: text(body.status, { name: "status", max: 80 }),
    event_type: eventType,
    url: publicUrl(body.url),
  }
}

function route(handler) {
  return (request, response, next) => {
    try {
      return handler(request, response, next)
    } catch (error) {
      if (error instanceof AdminInputError) {
        response
          .status(400)
          .json({ error: "invalid_input", message: error.message })
        return
      }
      next(error)
    }
  }
}

function asyncRoute(handler) {
  return (request, response, next) => {
    Promise.resolve()
      .then(() => handler(request, response, next))
      .catch((error) => {
        if (error instanceof AdminInputError) {
          response
            .status(400)
            .json({ error: "invalid_input", message: error.message })
          return
        }
        next(error)
      })
  }
}

function profileMediaPayload(database) {
  const items = listProfileMedia(database).map((item) => ({
    ...item,
    previewUrl:
      item.status === "ready"
        ? `/api/admin/profile-media/${encodeURIComponent(item.id)}/preview`
        : null,
  }))
  return {
    items,
    active: {
      avatar:
        items.find((item) => item.slot === "avatar" && item.isActive) || null,
      banner:
        items.find((item) => item.slot === "banner" && item.isActive) || null,
    },
  }
}

function profileMediaPreview(database, request, response) {
  const item = getProfileMedia(database, request.params.id)
  if (!item || item.status !== "ready" || !item.cachePath) {
    response.status(404).json({ error: "profile_media_not_ready" })
    return
  }
  const filePath = resolveMediaCachePath(item.cachePath)
  if (!filePath) {
    response.status(404).json({ error: "profile_media_not_found" })
    return
  }
  try {
    const resolved = fs.realpathSync(filePath)
    const roots = [
      avatarMediaDirectory,
      xMediaDirectory,
      youtubeMediaDirectory,
      legacyMediaCacheDirectory,
    ].flatMap((root) => {
      try {
        return [fs.realpathSync(root)]
      } catch {
        return []
      }
    })
    if (
      !roots.some(
        (root) =>
          resolved === root || resolved.startsWith(`${root}${path.sep}`),
      )
    )
      throw new Error("outside profile media root")
    if (!fs.statSync(resolved).isFile()) throw new Error("not a file")
    if (!isAllowedMediaMimeType(item.mimeType))
      throw new Error("unsupported MIME")
    response.set("Content-Type", item.mimeType)
    response.set("Content-Security-Policy", "default-src 'none'; sandbox")
    response.set("X-Content-Type-Options", "nosniff")
    response.sendFile(resolved)
  } catch {
    response.status(404).json({ error: "profile_media_not_found" })
  }
}

export function createAdminRouter({
  database,
  mode = "development",
  adminPassword = "",
  openAiKeyConfigured = Boolean(process.env.OPENAI_API_KEY),
} = {}) {
  const router = express.Router()
  const auth = createAdminAuth({ mode, adminPassword })
  router.use((_request, response, next) => {
    response.set("Cache-Control", "no-store")
    next()
  })

  router.get("/session", (request, response) => {
    response.json(auth.sessionPayload(request))
  })
  router.post("/login", auth.login)
  router.post("/logout", auth.logout)
  router.use(auth.requireAuth)

  router.get("/config", (_request, response) => {
    response.json(configPayload(database, openAiKeyConfigured))
  })
  router.put(
    "/config",
    route((request, response) => {
      const body = request.body || {}
      let updatedAt = null
      if (Object.prototype.hasOwnProperty.call(body, "llmModel")) {
        const model = text(body.llmModel, {
          name: "llmModel",
          required: true,
          max: 128,
        })
        if (!/^[A-Za-z0-9][A-Za-z0-9._:-]*$/u.test(model)) {
          throw new AdminInputError("llmModel contains unsupported characters")
        }
        updatedAt = setAppSetting(database, "llm_model", model).updatedAt
      }
      if (
        Object.prototype.hasOwnProperty.call(body, "scheduleExtractionEnabled")
      ) {
        const enabled = booleanValue(
          body.scheduleExtractionEnabled,
          "scheduleExtractionEnabled",
        )
        updatedAt = setAppSetting(
          database,
          "schedule_extraction_enabled",
          enabled ? "1" : "0",
        ).updatedAt
      }
      if (Object.prototype.hasOwnProperty.call(body, "scheduleKeywords")) {
        const keywords = scheduleKeywords(body.scheduleKeywords)
        updatedAt = setAppSetting(
          database,
          "schedule_keywords",
          JSON.stringify(keywords),
        ).updatedAt
      }
      if (Object.prototype.hasOwnProperty.call(body, "featuredVideoId")) {
        const featuredVideoId = text(body.featuredVideoId, {
          name: "featuredVideoId",
          max: 64,
        })
        try {
          setFeaturedVideoId(database, featuredVideoId)
          updatedAt = new Date().toISOString()
        } catch (error) {
          throw new AdminInputError(error.message)
        }
      }
      response.json(configPayload(database, openAiKeyConfigured, updatedAt))
    }),
  )

  router.get("/profile-media", (_request, response) => {
    response.json(profileMediaPayload(database))
  })
  router.get("/profile-media/:id/preview", (request, response) => {
    profileMediaPreview(database, request, response)
  })
  router.post(
    "/profile-media",
    route((request, response) => {
      const body = request.body || {}
      const slot = text(body.slot, { name: "slot", required: true, max: 16 })
      const source = text(body.source, {
        name: "source",
        required: true,
        max: 16,
      })
      if (
        !isProfileSlot(slot) ||
        !isProfileSource(source) ||
        source === "upload"
      )
        throw new AdminInputError("slot or source is invalid")
      const sourceUrl = publicUrl(body.sourceUrl)
      if (!sourceUrl) throw new AdminInputError("sourceUrl is required")
      const item = upsertProfileMediaCandidate(database, {
        slot,
        source,
        sourceRef: text(body.sourceRef, { name: "sourceRef", max: 2000 }),
        sourceUrl,
      })
      response.status(201).json({ item, ...profileMediaPayload(database) })
    }),
  )
  router.post(
    "/profile-media/discover",
    asyncRoute(async (request, response) => {
      const slot = text(request.body?.slot, {
        name: "slot",
        required: true,
        max: 16,
      })
      const source = text(request.body?.source, {
        name: "source",
        required: true,
        max: 16,
      })
      if (
        !isProfileSlot(slot) ||
        !isProfileSource(source) ||
        source === "upload"
      )
        throw new AdminInputError("slot or source is invalid")
      const result = await discoverProfileMedia(database, { slot, source })
      response.json({ ...result, ...profileMediaPayload(database) })
    }),
  )
  router.post(
    "/profile-media/upload/:slot",
    express.raw({
      type: (request) =>
        /^(?:image\/|application\/octet-stream(?:;|$))/iu.test(
          String(request.headers["content-type"] || ""),
        ),
      limit: maxProfileImageBytes,
    }),
    asyncRoute(async (request, response) => {
      const slot = text(request.params.slot, {
        name: "slot",
        required: true,
        max: 16,
      })
      if (!isProfileSlot(slot)) throw new AdminInputError("slot is invalid")
      if (!Buffer.isBuffer(request.body) || !request.body.length)
        throw new AdminInputError("image body is required")
      const item = await saveUploadedProfileMedia(
        database,
        slot,
        request.body,
        {
          mimeType: request.headers["content-type"],
        },
      )
      response.status(201).json({ item, ...profileMediaPayload(database) })
    }),
  )
  router.post(
    "/profile-media/:id/download",
    asyncRoute(async (request, response) => {
      const item = await downloadProfileMedia(database, request.params.id)
      response.json({ item, ...profileMediaPayload(database) })
    }),
  )
  router.post(
    "/profile-media/:id/select",
    route((request, response) => {
      const item = selectProfileMedia(database, request.params.id)
      response.json({ item, ...profileMediaPayload(database) })
    }),
  )

  router.get("/videos", (_request, response) => {
    response.json({ videos: listAdminVideos(database) })
  })

  router.get("/events", (request, response) => {
    response.json({
      events: listAdminEvents(database, {
        includeDeleted: request.query.includeDeleted === "1",
      }),
    })
  })
  router.post(
    "/events",
    route((request, response) => {
      const event = createManualEvent(database, eventInput(request.body))
      response.status(201).json({ event })
    }),
  )
  router.put(
    "/events/:id",
    route((request, response) => {
      const event = updateManualEvent(
        database,
        request.params.id,
        eventInput(request.body),
      )
      if (!event) {
        response.status(404).json({ error: "event_not_found" })
        return
      }
      response.json({ event })
    }),
  )
  router.post(
    "/events/:id/confirm",
    route((request, response) => {
      const existing = getEvent(database, request.params.id)
      if (!existing || existing.deletedAt) {
        response.status(404).json({ error: "event_not_found" })
        return
      }
      const event = updateManualEvent(
        database,
        request.params.id,
        eventInput(existing),
      )
      response.json({ event })
    }),
  )
  router.delete("/events/:id", (request, response) => {
    if (!deleteManualEvent(database, request.params.id)) {
      response.status(404).json({ error: "event_not_found" })
      return
    }
    response.status(204).end()
  })

  return router
}

export { AdminInputError, eventInput }
