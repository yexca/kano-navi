import fs from "node:fs"
import net from "node:net"
import path from "node:path"
import express from "express"

import {
  createManualEvent,
  deleteManualEvent,
  bumpDashboardRevision,
  getFeaturedVideoId,
  getAppSetting,
  getDetailedScheduleExtractionConfig,
  getEvent,
  getScheduleExtractionConfig,
  listAdminVideos,
  listAdminEvents,
  listAdminEventsPage,
  listLlmProviders,
  getLlmProvider,
  getLlmProviderSecret,
  getLlmRouteProviders,
  getScheduleProviderOrders,
  setLlmRouteProviders,
  upsertLlmProvider,
  updateLlmProviderStatus,
  deleteLlmProvider,
  listSyncRuns,
  getSyncRun,
  LLM_PROTOCOLS,
  LLM_CAPABILITIES,
  LLM_MAX_RETRIES,
  normalizeLlmCapabilities,
  SCHEDULE_MESSAGE_ROUTE,
  SCHEDULE_BOARD_ROUTE,
  SCHEDULE_ROUTES,
  SCHEDULE_VISION_ROUTE,
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
import { decryptSecret, encryptSecret } from "./secret-store.js"
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

function mutationOriginGuard(request, response, next) {
  const fetchSite = String(
    request.headers["sec-fetch-site"] || "",
  ).toLowerCase()
  if (fetchSite === "cross-site") {
    response.status(403).json({ error: "csrf_origin_mismatch" })
    return
  }
  const origin = String(request.headers.origin || "").trim()
  if (origin) {
    try {
      const expectedProtocol = String(
        request.headers["x-forwarded-proto"] || request.protocol,
      )
        .split(",", 1)[0]
        .trim()
        .toLowerCase()
      const expected = `${expectedProtocol}://${String(request.get("host"))}`
      if (new URL(origin).origin !== expected) throw new Error("origin")
    } catch {
      response.status(403).json({ error: "csrf_origin_mismatch" })
      return
    }
  }
  next()
}

function privateIpv4(hostname) {
  const parts = hostname.split(".").map((part) => Number(part))
  if (
    parts.length !== 4 ||
    parts.some((part) => !Number.isInteger(part) || part < 0 || part > 255)
  )
    return false
  const [a, b] = parts
  return (
    a === 0 ||
    a === 10 ||
    a === 127 ||
    (a === 100 && b >= 64 && b <= 127) ||
    (a === 169 && b === 254) ||
    (a === 172 && b >= 16 && b <= 31) ||
    (a === 192 && b === 0) ||
    (a === 192 && b === 168) ||
    (a === 198 && (b === 18 || b === 19)) ||
    (a === 198 && b === 51) ||
    (a === 203 && b === 0) ||
    a >= 224
  )
}

function ipv6BigInt(hostname) {
  let value = hostname.replace(/^\[|\]$/gu, "").toLowerCase()
  if (value.includes(".")) {
    const separator = value.lastIndexOf(":")
    if (separator < 0) return null
    const ipv4 = value.slice(separator + 1)
    if (!net.isIP(ipv4)) return null
    const parts = ipv4.split(".").map((part) => Number(part))
    if (
      parts.length !== 4 ||
      parts.some((part) => !Number.isInteger(part) || part < 0 || part > 255)
    )
      return null
    const high = ((parts[0] << 8) | parts[1]).toString(16)
    const low = ((parts[2] << 8) | parts[3]).toString(16)
    value = `${value.slice(0, separator)}:${high}:${low}`
  }
  if (net.isIP(value) !== 6) return null
  const halves = value.split("::")
  if (halves.length > 2) return null
  const left = halves[0] ? halves[0].split(":") : []
  const right = halves.length === 2 && halves[1] ? halves[1].split(":") : []
  const missing = 8 - left.length - right.length
  if (halves.length === 1 && missing !== 0) return null
  if (missing < 0 || (halves.length === 2 && missing < 1)) return null
  const groups =
    halves.length === 2
      ? [...left, ...Array(missing).fill("0"), ...right]
      : left
  if (
    groups.length !== 8 ||
    groups.some((group) => !/^[\da-f]{1,4}$/u.test(group))
  )
    return null
  let address = 0n
  for (const group of groups) address = (address << 16n) + BigInt(`0x${group}`)
  return address
}

function privateIpv6(hostname) {
  const address = ipv6BigInt(hostname)
  if (address == null) return false
  if (address === 0n || address === 1n) return true
  // Unspecified, loopback, unique-local, link-local, and multicast ranges.
  if (address >> 121n === 0x7en || address >> 121n === 0x7fn) return true // fc00::/7
  if (address >> 118n === 0x3fan) return true // fe80::/10
  if (address >> 120n === 0xffn) return true // ff00::/8
  // IPv4-mapped addresses inherit the IPv4 private/loopback policy.
  if (address >> 32n === 0xffffn) {
    const ipv4 = Number(address & 0xffffffffn)
    return privateIpv4(
      `${ipv4 >>> 24}.${(ipv4 >>> 16) & 255}.${(ipv4 >>> 8) & 255}.${ipv4 & 255}`,
    )
  }
  return false
}

function providerBaseUrl(value, { mode = "development" } = {}) {
  const normalized = text(value, { name: "baseUrl", required: true, max: 500 })
  let url
  try {
    url = new URL(normalized)
  } catch {
    throw new AdminInputError("baseUrl is invalid")
  }
  if (!["http:", "https:"].includes(url.protocol))
    throw new AdminInputError("baseUrl must use HTTP or HTTPS")
  if (url.username || url.password || url.search || url.hash)
    throw new AdminInputError(
      "baseUrl must not contain credentials, query, or fragment",
    )
  const hostname = url.hostname.toLowerCase().replace(/\.$/u, "")
  const ipVersion = net.isIP(hostname.replace(/^\[|\]$/gu, ""))
  if (
    hostname === "localhost" ||
    hostname.endsWith(".localhost") ||
    hostname.endsWith(".local") ||
    hostname.endsWith(".internal") ||
    (ipVersion === 4 && privateIpv4(hostname)) ||
    (ipVersion === 6 && privateIpv6(hostname))
  ) {
    throw new AdminInputError(
      "baseUrl must not target a private or loopback address",
    )
  }
  if (mode === "production" && url.protocol !== "https:")
    throw new AdminInputError("baseUrl must use HTTPS in production")
  return url.toString().replace(/\/+$/u, "")
}

function providerEndpoint(baseUrl, protocol) {
  const base = String(baseUrl).replace(/\/+$/u, "")
  if (/(?:\/responses|\/chat\/completions)$/u.test(base)) return base
  return protocol === "openai-chat-completions"
    ? `${base}/chat/completions`
    : `${base}/responses`
}

function providerModel(value) {
  const model = text(value, { name: "model", required: true, max: 160 })
  if (!/^[A-Za-z0-9][A-Za-z0-9._:/-]*$/u.test(model))
    throw new AdminInputError("model contains unsupported characters")
  return model
}

function providerCapabilities(value, existing = null) {
  if (value == null) {
    if (Array.isArray(existing?.capabilities)) return [...existing.capabilities]
    return existing?.visionCapable === false ? ["text"] : [...LLM_CAPABILITIES]
  }
  let values = value
  if (typeof value === "string") {
    try {
      const parsed = JSON.parse(value)
      values = Array.isArray(parsed) ? parsed : value.split(/[,\s]+/u)
    } catch {
      values = value.split(/[,\s]+/u)
    }
  }
  if (!Array.isArray(values)) values = []
  const capabilities = normalizeLlmCapabilities(values, false, { fallback: [] })
  if (!capabilities.length)
    throw new AdminInputError(
      "capabilities must include at least one of text or image",
    )
  if (capabilities.length !== values.length) {
    const normalizedValues = values.map((item) =>
      String(item || "")
        .trim()
        .toLowerCase() === "vision"
        ? "image"
        : String(item || "")
            .trim()
            .toLowerCase(),
    )
    if (
      normalizedValues.some((item) => item && !LLM_CAPABILITIES.includes(item))
    )
      throw new AdminInputError("capabilities contains an unsupported value")
  }
  return capabilities
}

function validProviderRoute(value, fallback = SCHEDULE_VISION_ROUTE) {
  const route = String(value || fallback)
  if (!SCHEDULE_ROUTES.includes(route))
    throw new AdminInputError("route is invalid")
  return route
}

function ensureProviderRoutes(database, id) {
  for (const route of [
    SCHEDULE_VISION_ROUTE,
    SCHEDULE_MESSAGE_ROUTE,
    SCHEDULE_BOARD_ROUTE,
  ]) {
    const current = getLlmRouteProviders(database, route)
    if (!current.includes(id)) {
      setLlmRouteProviders(database, route, [...current, id])
    }
  }
}

function integerField(value, name, minimum, maximum, fallback) {
  if (value == null || value === "") return fallback
  const number = Number(value)
  if (!Number.isInteger(number) || number < minimum || number > maximum)
    throw new AdminInputError(
      `${name} must be an integer between ${minimum} and ${maximum}`,
    )
  return number
}

function providerInput(
  body = {},
  existing = null,
  { mode = "development" } = {},
) {
  const id = text(body.id ?? existing?.id, {
    name: "id",
    required: true,
    max: 64,
  })
  if (!/^[a-z0-9][a-z0-9_-]{0,63}$/u.test(id))
    throw new AdminInputError("id contains unsupported characters")
  const protocol = String(
    body.protocol ?? existing?.protocol ?? "openai-responses",
  )
  if (!LLM_PROTOCOLS.includes(protocol))
    throw new AdminInputError("protocol is invalid")
  const baseUrl = providerBaseUrl(body.baseUrl ?? existing?.baseUrl, { mode })
  const model = providerModel(body.model ?? existing?.model)
  const name = text(body.name ?? existing?.name ?? id, {
    name: "name",
    required: true,
    max: 120,
  })
  const enabled =
    body.enabled == null
      ? existing?.enabled !== false
      : booleanValue(body.enabled, "enabled")
  const hasCapabilities = Object.prototype.hasOwnProperty.call(
    body,
    "capabilities",
  )
  const hasLegacyVision = Object.prototype.hasOwnProperty.call(
    body,
    "visionCapable",
  )
  const legacyVisionCapable = hasLegacyVision
    ? booleanValue(body.visionCapable, "visionCapable")
    : existing?.visionCapable !== false
  const capabilities = hasCapabilities
    ? providerCapabilities(body.capabilities, existing)
    : hasLegacyVision
      ? providerCapabilities(
          legacyVisionCapable ? ["text", "image"] : ["text"],
          existing,
        )
      : providerCapabilities(undefined, existing || { visionCapable: true })
  const visionCapable = capabilities.includes("image")
  const timeoutMs = integerField(
    body.timeoutMs,
    "timeoutMs",
    1000,
    120000,
    existing?.timeoutMs ?? 30000,
  )
  const maxRetries = integerField(
    body.maxRetries,
    "maxRetries",
    0,
    LLM_MAX_RETRIES,
    existing?.maxRetries ?? LLM_MAX_RETRIES,
  )
  let replaceApiKey = false
  let apiKeyCiphertext = null
  if (Object.prototype.hasOwnProperty.call(body, "apiKey")) {
    const apiKey = body.apiKey == null ? "" : String(body.apiKey).trim()
    if (apiKey.length > 500) throw new AdminInputError("apiKey is too long")
    replaceApiKey = true
    if (apiKey) {
      try {
        apiKeyCiphertext = encryptSecret(
          apiKey,
          process.env.LLM_SECRETS_KEY || "",
        )
      } catch (error) {
        throw new AdminInputError(error.message)
      }
    }
  }
  return {
    id,
    name,
    protocol,
    baseUrl,
    model,
    enabled,
    visionCapable,
    capabilities,
    timeoutMs,
    maxRetries,
    replaceApiKey,
    apiKeyCiphertext,
  }
}

function queryBoolean(value) {
  return ["1", "true", "yes", "on"].includes(
    String(value ?? "")
      .trim()
      .toLowerCase(),
  )
}

async function testProviderConnection(
  database,
  provider,
  { fetchImpl = fetch } = {},
) {
  const secret = getLlmProviderSecret(database, provider.id)
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
  if (!apiKey) throw new AdminInputError("provider API key is not configured")
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), provider.timeoutMs)
  try {
    const endpoint = providerEndpoint(provider.baseUrl, provider.protocol)
    const body =
      provider.protocol === "openai-chat-completions"
        ? {
            model: provider.model,
            messages: [{ role: "user", content: "ping" }],
            max_tokens: 1,
          }
        : {
            model: provider.model,
            store: false,
            input: "ping",
            max_output_tokens: 1,
          }
    const response = await fetchImpl(endpoint, {
      method: "POST",
      redirect: "error",
      signal: controller.signal,
      headers: {
        authorization: `Bearer ${apiKey}`,
        "content-type": "application/json",
      },
      body: JSON.stringify(body),
    })
    if (!response.ok)
      throw new Error(`provider returned HTTP ${response.status}`)
    updateLlmProviderStatus(database, provider.id, { status: "success" })
    return { ok: true, provider: getLlmProvider(database, provider.id) }
  } catch (error) {
    const message =
      error?.name === "AbortError"
        ? "provider request timed out"
        : "provider request failed"
    updateLlmProviderStatus(database, provider.id, {
      status: "failed",
      error: message,
    })
    throw new AdminInputError(message)
  } finally {
    clearTimeout(timer)
  }
}

function configPayload(database, updatedAt = null) {
  const schedule = getDetailedScheduleExtractionConfig(database)
  const providerOrders =
    schedule.providerOrders || getScheduleProviderOrders(database)
  const providers = publicProviders(database)
  const openAiKeyConfigured = Boolean(
    providers.find((provider) => provider.id === "openai-default")
      ?.apiKeyConfigured,
  )
  return {
    llmModel: getAppSetting(database, "llm_model", defaultScheduleModel),
    openAiKeyConfigured,
    scheduleExtractionEnabled: schedule.enabled,
    scheduleKeywordEnabled: schedule.keywordEnabled,
    scheduleVisionEnabled: schedule.visionEnabled,
    scheduleMessageEnabled: schedule.messageEnabled,
    scheduleKeywords: schedule.keywords,
    providerOrder: schedule.providerOrder,
    providerOrders,
    routes: providerOrders,
    providers,
    featuredVideoId: getFeaturedVideoId(database),
    ...(updatedAt ? { updatedAt } : {}),
  }
}

function publicProviders(database) {
  return listLlmProviders(database)
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
  jobs = null,
  fetchImpl = fetch,
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
  router.use(mutationOriginGuard)

  router.get("/config", (_request, response) => {
    response.json(configPayload(database))
  })
  router.put(
    "/config",
    route((request, response) => {
      const body = request.body || {}
      let updatedAt = null
      let dashboardChanged = false
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
        setAppSetting(database, "schedule_keyword_enabled", enabled ? "1" : "0")
        setAppSetting(database, "schedule_vision_enabled", enabled ? "1" : "0")
        setAppSetting(database, "schedule_message_enabled", enabled ? "1" : "0")
      }
      if (
        Object.prototype.hasOwnProperty.call(body, "scheduleKeywordEnabled")
      ) {
        const enabled = booleanValue(
          body.scheduleKeywordEnabled,
          "scheduleKeywordEnabled",
        )
        updatedAt = setAppSetting(
          database,
          "schedule_keyword_enabled",
          enabled ? "1" : "0",
        ).updatedAt
      }
      if (Object.prototype.hasOwnProperty.call(body, "scheduleVisionEnabled")) {
        const enabled = booleanValue(
          body.scheduleVisionEnabled,
          "scheduleVisionEnabled",
        )
        updatedAt = setAppSetting(
          database,
          "schedule_vision_enabled",
          enabled ? "1" : "0",
        ).updatedAt
      }
      if (
        Object.prototype.hasOwnProperty.call(body, "scheduleMessageEnabled")
      ) {
        const enabled = booleanValue(
          body.scheduleMessageEnabled,
          "scheduleMessageEnabled",
        )
        updatedAt = setAppSetting(
          database,
          "schedule_message_enabled",
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
          const previousFeaturedVideoId = getFeaturedVideoId(database)
          setFeaturedVideoId(database, featuredVideoId)
          dashboardChanged =
            previousFeaturedVideoId !== getFeaturedVideoId(database)
          updatedAt = new Date().toISOString()
        } catch (error) {
          throw new AdminInputError(error.message)
        }
      }
      if (dashboardChanged) bumpDashboardRevision(database)
      response.json(configPayload(database, updatedAt))
    }),
  )

  router.get("/providers", (request, response) => {
    const route = validProviderRoute(request.query?.route)
    const providerOrders = getScheduleProviderOrders(database)
    response.json({
      providers: publicProviders(database),
      route,
      providerOrder: providerOrders[route],
      providerOrders,
      routes: providerOrders,
    })
  })
  router.post(
    "/providers",
    route((request, response) => {
      let input
      try {
        input = providerInput(request.body || {}, null, { mode })
        const provider = upsertLlmProvider(database, input)
        ensureProviderRoutes(database, input.id)
        response.status(201).json({
          provider,
        })
      } catch (error) {
        if (error instanceof AdminInputError) throw error
        throw new AdminInputError(error.message)
      }
    }),
  )
  router.put(
    "/providers/order",
    route((request, response) => {
      const route = validProviderRoute(request.body?.route)
      const ids = request.body?.providerOrder ?? request.body?.ids
      if (!Array.isArray(ids) || ids.length > 100)
        throw new AdminInputError("providerOrder must be an array")
      try {
        const providerOrder = setLlmRouteProviders(database, route, ids)
        response.json({
          route,
          providerOrder,
          providerOrders: getScheduleProviderOrders(database),
          routes: getScheduleProviderOrders(database),
          providers: publicProviders(database),
        })
      } catch (error) {
        throw new AdminInputError(error.message)
      }
    }),
  )
  router.put(
    "/providers/:id",
    route((request, response) => {
      const existing = getLlmProvider(database, request.params.id)
      if (!existing) {
        response.status(404).json({ error: "llm_provider_not_found" })
        return
      }
      try {
        const input = providerInput(
          { ...(request.body || {}), id: request.params.id },
          existing,
          { mode },
        )
        const provider = upsertLlmProvider(database, input)
        ensureProviderRoutes(database, input.id)
        response.json({
          provider,
        })
      } catch (error) {
        if (error instanceof AdminInputError) throw error
        throw new AdminInputError(error.message)
      }
    }),
  )
  router.delete(
    "/providers/:id",
    route((request, response) => {
      if (!deleteLlmProvider(database, request.params.id)) {
        response.status(404).json({ error: "llm_provider_not_found" })
        return
      }
      response.status(204).end()
    }),
  )
  const providerTestRoute = asyncRoute(async (request, response) => {
    const provider = getLlmProvider(database, request.params.id)
    if (!provider) {
      response.status(404).json({ error: "llm_provider_not_found" })
      return
    }
    const result = await testProviderConnection(database, provider, {
      fetchImpl,
    })
    response.json({
      ...result,
      provider: result.provider,
    })
  })
  router.put("/providers/:id/test", providerTestRoute)
  router.post("/providers/:id/test", providerTestRoute)

  router.get("/sync/runs", (request, response) => {
    response.json({
      runs: listSyncRuns(database, {
        limit: request.query.limit,
        jobId: request.query.jobId,
        status: request.query.status,
      }),
    })
  })
  router.get("/sync/jobs", (request, response) => {
    response.json({ jobs: jobs?.list(request.query.limit) || [] })
  })
  router.get("/sync/jobs/:id", (request, response) => {
    const job =
      jobs?.get(request.params.id) || getSyncRun(database, request.params.id)
    if (!job) {
      response.status(404).json({ error: "sync_job_not_found" })
      return
    }
    response.json({ job })
  })
  router.post(
    "/sync",
    route((request, response) => {
      if (!jobs) {
        response.status(503).json({ error: "sync_jobs_unavailable" })
        return
      }
      const result = jobs.start("sync", "admin")
      response.status(result.accepted ? 202 : 409).json(result)
    }),
  )
  router.post(
    "/sync/start",
    route((request, response) => {
      if (!jobs) {
        response.status(503).json({ error: "sync_jobs_unavailable" })
        return
      }
      const result = jobs.start("sync", "admin")
      response.status(result.accepted ? 202 : 409).json(result)
    }),
  )
  router.post(
    "/scan/automatic",
    route((request, response) => {
      if (!jobs) {
        response.status(503).json({ error: "sync_jobs_unavailable" })
        return
      }
      const result = jobs.start("scan", "admin")
      response.status(result.accepted ? 202 : 409).json(result)
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
      bumpDashboardRevision(database)
      response.json({ item, ...profileMediaPayload(database) })
    }),
  )

  router.get("/videos", (_request, response) => {
    response.json({ videos: listAdminVideos(database) })
  })

  router.get("/events", (request, response) => {
    const hasPagination =
      request.query.page != null ||
      request.query.pageSize != null ||
      request.query.search != null ||
      request.query.provenance != null ||
      request.query.source != null ||
      request.query.from != null ||
      request.query.to != null
    if (!hasPagination) {
      response.json({
        events: listAdminEvents(database, {
          includeDeleted: queryBoolean(request.query.includeDeleted),
        }),
      })
      return
    }
    response.json(
      listAdminEventsPage(database, {
        page: request.query.page,
        pageSize: request.query.pageSize,
        includeDeleted: queryBoolean(request.query.includeDeleted),
        search: request.query.search,
        provenance: request.query.provenance,
        source: request.query.source,
        from: request.query.from,
        to: request.query.to,
      }),
    )
  })
  router.post(
    "/events",
    route((request, response) => {
      const event = createManualEvent(database, eventInput(request.body))
      bumpDashboardRevision(database)
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
      bumpDashboardRevision(database)
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
      bumpDashboardRevision(database)
      response.json({ event })
    }),
  )
  router.delete("/events/:id", (request, response) => {
    if (!deleteManualEvent(database, request.params.id)) {
      response.status(404).json({ error: "event_not_found" })
      return
    }
    bumpDashboardRevision(database)
    response.status(204).end()
  })

  return router
}

export { AdminInputError, eventInput }
