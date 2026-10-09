import type { Database as DatabaseConnection } from "better-sqlite3"

import crypto from "node:crypto"
import {
  normalizeFetchWindow,
  normalizeSourceLookback,
  sourceFetchStatus,
} from "./fetch-window.ts"
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
  appendLlmRouteProvider,
  deleteLlmModels,
  deleteWorkflow,
  getLlmModel,
  getScheduleProviderOrders,
  getLlmRouteTargets,
  getScheduleRouteTargets,
  listPostLlmStates,
  listScheduleAssetReviews,
  requestPostLlmReprocess,
  getWorkflow,
  listLlmModels,
  listWorkflows,
  LLM_MODEL_ID_PATTERN,
  LLM_MODEL_TAGS,
  normalizeLlmModelTags,
  setLlmRouteProviders,
  setLlmRouteTargets,
  upsertLlmModels,
  upsertLlmProvider,
  upsertWorkflow,
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
  updateScheduleAssetManualReview,
  updateManualEvent,
  resolveMediaReference,
} from "./database.ts"
import { createAdminAuth } from "./admin-auth.ts"
import { defaultScheduleModel } from "./schedule-extractor.ts"
import { decryptSecret, encryptSecret } from "./secret-store.ts"
import { fetchRemoteModels, inferModelTags } from "./llm-catalog.ts"
import { inferenceEndpoint } from "../src/lib/llm-endpoints.ts"
import { providerHeaders, readLlmJson, outputTokenLimit } from "./llm-http.ts"
import {
  WORKFLOW_MAX_INTERVAL_MINUTES,
  WORKFLOW_MIN_INTERVAL_MINUTES,
  WORKFLOW_STEPS,
  environmentSkippedSteps,
  normalizeWorkflowSteps,
} from "./workflow-catalog.ts"
import {
  avatarMediaDirectory,
  isAllowedMediaMimeType,
  legacyMediaCacheDirectory,
  resolveMediaCachePath,
  xMediaDirectory,
  youtubeMediaDirectory,
} from "./media-cache.ts"
import {
  discoverProfileMedia,
  downloadProfileMedia,
  isProfileSlot,
  isProfileSource,
  maxProfileImageBytes,
  saveUploadedProfileMedia,
} from "./profile-media.ts"

class AdminInputError extends Error {}

function text(
  value,
  {
    name,
    required = false,
    max = 500,
  }: { name?: any; required?: boolean; max?: number } = {},
) {
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

function iso(
  value,
  { name, required = false }: { name?: any; required?: boolean } = {},
) {
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
  if (["GET", "HEAD", "OPTIONS"].includes(request.method)) {
    next()
    return
  }
  const fetchSite = String(
    request.headers["sec-fetch-site"] || "",
  ).toLowerCase()
  if (fetchSite === "cross-site") {
    response.status(403).json({ error: "csrf_origin_mismatch" })
    return
  }
  // Browsers compute this against the public URL before a reverse proxy can
  // rewrite Host or terminate HTTPS. It cannot be set by page JavaScript.
  if (fetchSite === "same-origin") {
    next()
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

function providerBaseUrl(
  value,
  { mode = "development" }: { mode?: string } = {},
) {
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

function modelId(
  value,
  {
    name = "model",
    required = true,
  }: { name?: string; required?: boolean } = {},
) {
  const model = text(value, { name, required, max: 200 })
  if (!model) return ""
  if (!LLM_MODEL_ID_PATTERN.test(model))
    throw new AdminInputError(`${name} contains unsupported characters`)
  return model
}

// The provider model is now an optional default (connection test and legacy
// routes); catalog models live in llm_models.
function providerModel(value) {
  return modelId(value, { name: "model", required: false })
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

// Legacy clients create a provider with one model and expect it to join
// every route. Catalog-managed providers without a default model are routed
// explicitly from the detection settings instead.
function ensureProviderRoutes(database: DatabaseConnection, provider) {
  if (!provider?.model) return
  for (const route of [
    SCHEDULE_VISION_ROUTE,
    SCHEDULE_MESSAGE_ROUTE,
    SCHEDULE_BOARD_ROUTE,
  ]) {
    appendLlmRouteProvider(database, route, provider.id)
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
  body: Record<string, any> = {},
  existing = null,
  { mode = "development" }: { mode?: string } = {},
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

function providerApiKey(database: DatabaseConnection, provider) {
  const secret = getLlmProviderSecret(database, provider.id)
  if (!secret?.apiKeyCiphertext) return null
  try {
    return decryptSecret(
      secret.apiKeyCiphertext,
      process.env.LLM_SECRETS_KEY || "",
    )
  } catch {
    return null
  }
}

async function discoverProviderModels(
  database: DatabaseConnection,
  provider,
  { fetchImpl = fetch }: { fetchImpl?: any } = {},
) {
  const apiKey = providerApiKey(database, provider)
  if (!apiKey) throw new AdminInputError("provider API key is not configured")
  try {
    const remote = await fetchRemoteModels(
      {
        baseUrl: provider.baseUrl,
        apiKey,
        timeoutMs: provider.timeoutMs,
        protocol: provider.protocol,
      },
      { fetchImpl },
    )
    updateLlmProviderStatus(database, provider.id, { status: "success" })
    return remote
  } catch (error) {
    updateLlmProviderStatus(database, provider.id, {
      status: "failed",
      error: error.message,
    })
    throw new AdminInputError(error.message)
  }
}

async function testProviderConnection(
  database: DatabaseConnection,
  provider,
  { fetchImpl = fetch, model = "" }: { fetchImpl?: any; model?: string } = {},
) {
  const apiKey = providerApiKey(database, provider)
  if (!apiKey) throw new AdminInputError("provider API key is not configured")
  const testModel = model || provider.model
  // Without any model, listing models is the cheapest authenticated check.
  if (!testModel) {
    const remote = await discoverProviderModels(database, provider, {
      fetchImpl,
    })
    return {
      ok: true,
      method: "models",
      remoteModelCount: remote.length,
      provider: getLlmProvider(database, provider.id),
    }
  }
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), provider.timeoutMs)
  try {
    const endpoint = inferenceEndpoint(provider.baseUrl, provider.protocol)
    const body =
      provider.protocol !== "openai-responses"
        ? {
            model: testModel,
            messages: [{ role: "user", content: "ping" }],
            ...outputTokenLimit(provider.protocol, 1),
          }
        : {
            model: testModel,
            store: false,
            input: "ping",
            ...outputTokenLimit(provider.protocol, 1),
          }
    const response = await fetchImpl(endpoint, {
      method: "POST",
      redirect: "error",
      signal: controller.signal,
      headers: providerHeaders(provider.protocol, apiKey),
      body: JSON.stringify(body),
    })
    if (!response.ok)
      throw new Error(`provider returned HTTP ${response.status}`)
    await readLlmJson(response)
    updateLlmProviderStatus(database, provider.id, { status: "success" })
    return {
      ok: true,
      method: "inference",
      model: testModel,
      provider: getLlmProvider(database, provider.id),
    }
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

function configPayload(database: DatabaseConnection, updatedAt = null) {
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
    models: listLlmModels(database),
    modelTags: LLM_MODEL_TAGS,
    routeTargets: getScheduleRouteTargets(database),
    configuredRouteTargets: Object.fromEntries(
      SCHEDULE_ROUTES.map((route) => [
        route,
        getLlmRouteTargets(database, route),
      ]),
    ),
    featuredVideoId: getFeaturedVideoId(database),
    ...(updatedAt ? { updatedAt } : {}),
  }
}

function modelInput(value) {
  const id = modelId(value?.id, { name: "model id" })
  const hasTags = value && Object.prototype.hasOwnProperty.call(value, "tags")
  return {
    id,
    ...(value && Object.prototype.hasOwnProperty.call(value, "name")
      ? { name: text(value.name, { name: "model name", max: 120 }) }
      : {}),
    ...(hasTags ? { tags: modelTags(value.tags) } : {}),
    ...(value && Object.prototype.hasOwnProperty.call(value, "enabled")
      ? { enabled: booleanValue(value.enabled, "enabled") }
      : {}),
    ...(value && Object.prototype.hasOwnProperty.call(value, "ownedBy")
      ? { ownedBy: text(value.ownedBy, { name: "ownedBy", max: 120 }) }
      : {}),
  }
}

function modelTags(value) {
  const values = Array.isArray(value) ? value : String(value ?? "").split(",")
  const tags = normalizeLlmModelTags(values)
  const unknown = values
    .map((item) =>
      String(item || "")
        .trim()
        .toLowerCase(),
    )
    .filter(Boolean)
    .map((item) => (item === "vision" ? "image" : item))
    .filter((item) => !LLM_MODEL_TAGS.includes(item))
  if (unknown.length)
    throw new AdminInputError("tags contains an unsupported value")
  return tags
}

function routeTargetsInput(value) {
  if (!Array.isArray(value) || value.length > 100)
    throw new AdminInputError("targets must be an array")
  return value.map((target) => ({
    providerId: text(target?.providerId, {
      name: "providerId",
      required: true,
      max: 64,
    }),
    modelId: modelId(target?.modelId, { name: "modelId", required: false }),
  }))
}

function workflowInput(body: Record<string, any> = {}, existing = null) {
  const name = text(body.name ?? existing?.name, {
    name: "name",
    required: true,
    max: 80,
  })
  const rawSteps = body.steps ?? existing?.steps
  if (!Array.isArray(rawSteps))
    throw new AdminInputError("steps must be an array")
  const steps = normalizeWorkflowSteps(rawSteps)
  if (steps.length !== new Set(rawSteps.map(String)).size)
    throw new AdminInputError("steps contains an unsupported value")
  if (!steps.length) throw new AdminInputError("steps must not be empty")
  const scheduleEnabled =
    body.scheduleEnabled == null
      ? Boolean(existing?.scheduleEnabled)
      : booleanValue(body.scheduleEnabled, "scheduleEnabled")
  const intervalMinutes = integerField(
    body.intervalMinutes,
    "intervalMinutes",
    WORKFLOW_MIN_INTERVAL_MINUTES,
    WORKFLOW_MAX_INTERVAL_MINUTES,
    existing?.intervalMinutes ?? 60,
  )
  let sourceLookbackDays
  try {
    sourceLookbackDays = normalizeSourceLookback(
      body.sourceLookbackDays ?? existing?.sourceLookbackDays,
    )
  } catch (error) {
    throw new AdminInputError(error.message)
  }
  return { name, steps, scheduleEnabled, intervalMinutes, sourceLookbackDays }
}

function workflowId(name) {
  const slug = String(name || "")
    .toLowerCase()
    .normalize("NFKD")
    .replace(/[^a-z0-9]+/gu, "-")
    .replace(/^-+|-+$/gu, "")
    .slice(0, 40)
  return `${slug || "workflow"}-${crypto.randomBytes(3).toString("hex")}`
}

function workflowsPayload(
  database: DatabaseConnection,
  workflows = null,
  jobs = null,
) {
  return {
    workflows: listWorkflows(database),
    steps: WORKFLOW_STEPS.map(({ id, group }) => ({ id, group })),
    skippedByEnvironment: environmentSkippedSteps(),
    limits: {
      minIntervalMinutes: WORKFLOW_MIN_INTERVAL_MINUTES,
      maxIntervalMinutes: WORKFLOW_MAX_INTERVAL_MINUTES,
    },
    scheduler: workflows?.status?.() || { running: false, tickSeconds: null },
    activeJob: jobs?.active?.() || null,
  }
}

function publicProviders(database: DatabaseConnection) {
  return listLlmProviders(database)
}

function eventInput(body: Record<string, any> = {}) {
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
  const cancellationStatus =
    text(body.cancellationStatus ?? body.cancellation_status, {
      name: "cancellationStatus",
      max: 32,
    }) || "none"
  // `llm_suspected` only means "keep the current LLM overlay"; the database
  // ignores it unless the stored event already carries that overlay.
  if (
    !["none", "llm_suspected", "manual_confirmed"].includes(cancellationStatus)
  ) {
    throw new AdminInputError("cancellationStatus is invalid")
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
    cancellation_status: cancellationStatus,
    cancellation_reason:
      cancellationStatus === "manual_confirmed"
        ? text(body.cancellationReason ?? body.cancellation_reason, {
            name: "cancellationReason",
            required: true,
            max: 500,
          })
        : null,
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

function profileMediaPayload(database: DatabaseConnection) {
  const items = listProfileMedia(database).map((item): Record<string, any> => ({
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

function scheduleAssetPayload(database: DatabaseConnection, item) {
  const media = resolveMediaReference(database, item.url)
  return {
    ...item,
    previewUrl: media.publicUrl,
    mediaId: media.id,
    mediaStatus: media.status,
    mediaSourceUrl: media.sourceUrl,
  }
}

function profileMediaPreview(database: DatabaseConnection, request, response) {
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
    response.sendFile(path.basename(resolved), { root: path.dirname(resolved) })
  } catch {
    response.status(404).json({ error: "profile_media_not_found" })
  }
}

export function createAdminRouter({
  database,
  mode = "development",
  adminPassword = "",
  jobs = null,
  workflows = null,
  fetchImpl = fetch,
}: {
  database?: DatabaseConnection
  mode?: string
  adminPassword?: string
  jobs?: any
  workflows?: any
  fetchImpl?: any
} = {}) {
  const router = express.Router()
  const auth = createAdminAuth({ mode, adminPassword })
  router.use((_request, response, next) => {
    response.set("Cache-Control", "no-store, no-transform")
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
        ensureProviderRoutes(database, provider)
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
        // Edits never re-add a provider that an operator removed from a route.
        const provider = upsertLlmProvider(database, input)
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
    const model = modelId(request.body?.model, {
      name: "model",
      required: false,
    })
    if (model && !getLlmModel(database, provider.id, model))
      throw new AdminInputError("model is not in the provider catalog")
    const result = await testProviderConnection(database, provider, {
      fetchImpl,
      model,
    })
    response.json({
      ...result,
      provider: result.provider,
    })
  })
  router.put("/providers/:id/test", providerTestRoute)
  router.post("/providers/:id/test", providerTestRoute)

  router.get("/models", (request, response) => {
    response.json({
      models: listLlmModels(database, {
        providerId: request.query.providerId || null,
      }),
      modelTags: LLM_MODEL_TAGS,
    })
  })
  router.post(
    "/providers/:id/models/discover",
    asyncRoute(async (request, response) => {
      const provider = getLlmProvider(database, request.params.id)
      if (!provider) {
        response.status(404).json({ error: "llm_provider_not_found" })
        return
      }
      const remote = await discoverProviderModels(database, provider, {
        fetchImpl,
      })
      const added = new Map(
        listLlmModels(database, { providerId: provider.id }).map((model) => [
          model.id,
          model,
        ]),
      )
      response.json({
        provider: getLlmProvider(database, provider.id),
        models: remote.map((model) => ({
          ...model,
          suggestedTags: inferModelTags(model.id),
          added: added.has(model.id),
          tags: added.get(model.id)?.tags || null,
        })),
      })
    }),
  )
  router.post(
    "/providers/:id/models",
    route((request, response) => {
      const provider = getLlmProvider(database, request.params.id)
      if (!provider) {
        response.status(404).json({ error: "llm_provider_not_found" })
        return
      }
      const values = request.body?.models
      if (!Array.isArray(values) || !values.length || values.length > 500)
        throw new AdminInputError("models must be a non-empty array")
      const inputs = values.map((value) => {
        const input = modelInput(value)
        const existing = getLlmModel(database, provider.id, input.id)
        return {
          ...input,
          ...(input.tags || existing ? {} : { tags: inferModelTags(input.id) }),
          origin:
            existing?.origin ||
            (request.body?.origin === "remote" ? "remote" : "manual"),
        }
      })
      try {
        const models = upsertLlmModels(database, provider.id, inputs)
        if (!provider.model) {
          // The first catalog model becomes the default test model.
          upsertLlmProvider(database, {
            ...provider,
            model: inputs[0].id,
            capabilities: models.find((model) => model.id === inputs[0].id)
              ?.capabilities?.length
              ? models.find((model) => model.id === inputs[0].id).capabilities
              : provider.capabilities,
            replaceApiKey: false,
          })
        }
        response.status(201).json({
          provider: getLlmProvider(database, provider.id),
          models: listLlmModels(database, { providerId: provider.id }),
        })
      } catch (error) {
        throw new AdminInputError(error.message)
      }
    }),
  )
  router.put(
    "/providers/:id/models",
    route((request, response) => {
      const provider = getLlmProvider(database, request.params.id)
      if (!provider) {
        response.status(404).json({ error: "llm_provider_not_found" })
        return
      }
      const input = modelInput(request.body || {})
      if (!getLlmModel(database, provider.id, input.id)) {
        response.status(404).json({ error: "llm_model_not_found" })
        return
      }
      try {
        upsertLlmModels(database, provider.id, [input])
      } catch (error) {
        throw new AdminInputError(error.message)
      }
      response.json({
        provider: getLlmProvider(database, provider.id),
        model: getLlmModel(database, provider.id, input.id),
        models: listLlmModels(database, { providerId: provider.id }),
      })
    }),
  )
  router.post(
    "/providers/:id/models/remove",
    route((request, response) => {
      const provider = getLlmProvider(database, request.params.id)
      if (!provider) {
        response.status(404).json({ error: "llm_provider_not_found" })
        return
      }
      const ids = request.body?.ids
      if (!Array.isArray(ids) || !ids.length || ids.length > 500)
        throw new AdminInputError("ids must be a non-empty array")
      const removed = deleteLlmModels(
        database,
        provider.id,
        ids.map((id) => modelId(id, { name: "model id" })),
      )
      response.json({
        removed,
        provider: getLlmProvider(database, provider.id),
        models: listLlmModels(database, { providerId: provider.id }),
      })
    }),
  )

  router.get("/routes", (_request, response) => {
    response.json({
      routes: getScheduleRouteTargets(database),
      configured: Object.fromEntries(
        SCHEDULE_ROUTES.map((name) => [
          name,
          getLlmRouteTargets(database, name),
        ]),
      ),
    })
  })
  router.put(
    "/routes/:route",
    route((request, response) => {
      const routeName = validProviderRoute(request.params.route)
      const targets = routeTargetsInput(request.body?.targets)
      try {
        setLlmRouteTargets(database, routeName, targets)
      } catch (error) {
        throw new AdminInputError(error.message)
      }
      response.json(configPayload(database, new Date().toISOString()))
    }),
  )

  router.get("/workflows", (_request, response) => {
    response.json(workflowsPayload(database, workflows, jobs))
  })
  router.post(
    "/workflows",
    route((request, response) => {
      const input = workflowInput(request.body || {})
      let id = workflowId(input.name)
      while (getWorkflow(database, id)) id = workflowId(input.name)
      const workflow = upsertWorkflow(database, { ...input, id })
      response
        .status(201)
        .json({ workflow, ...workflowsPayload(database, workflows, jobs) })
    }),
  )
  router.put(
    "/workflows/:id",
    route((request, response) => {
      const existing = getWorkflow(database, request.params.id)
      if (!existing) {
        response.status(404).json({ error: "workflow_not_found" })
        return
      }
      const workflow = upsertWorkflow(database, {
        ...workflowInput(request.body || {}, existing),
        id: existing.id,
      })
      response.json({
        workflow,
        ...workflowsPayload(database, workflows, jobs),
      })
    }),
  )
  router.delete(
    "/workflows/:id",
    route((request, response) => {
      if (!deleteWorkflow(database, request.params.id)) {
        response.status(404).json({ error: "workflow_not_found" })
        return
      }
      response.json(workflowsPayload(database, workflows, jobs))
    }),
  )
  router.post(
    "/workflows/:id/run",
    route((request, response) => {
      if (!workflows) {
        response.status(503).json({ error: "sync_jobs_unavailable" })
        return
      }
      const result = workflows.run(request.params.id, "admin")
      if (result.reason === "workflow_not_found") {
        response.status(404).json({ error: "workflow_not_found" })
        return
      }
      response.status(result.accepted ? 202 : 409).json({
        ...result,
        ...workflowsPayload(database, workflows, jobs),
      })
    }),
  )

  router.get("/sources/fetch", (_request, response) => {
    response.json(sourceFetchStatus(database))
  })
  router.post(
    "/sources/:source/fetch",
    route((request, response) => {
      const source = request.params.source
      if (!["x", "youtube"].includes(source))
        throw new AdminInputError("unsupported source")
      let window
      try {
        window = normalizeFetchWindow(request.body?.window)
      } catch (error) {
        throw new AdminInputError(error.message)
      }
      const status = sourceFetchStatus(database)[source]
      if (window.mode !== "recent" && !status.historyAvailable)
        throw new AdminInputError(
          source === "x"
            ? "X_API_BEARER_TOKEN is required for history search"
            : "YOUTUBE_API_KEY is required for history search",
        )
      if (
        window.mode === "before" &&
        !(source === "x"
          ? status.accounts.some((account) => account.oldest)
          : status.oldest)
      )
        throw new AdminInputError(
          "no stored records to backfill; select a date range first",
        )
      if (!jobs)
        return response.status(503).json({ error: "sync_jobs_unavailable" })
      const result = jobs.start("workflow", "admin", {
        steps: [source, "media"],
        fetchWindows: { [source]: window },
      })
      response.status(result.accepted ? 202 : 409).json(result)
    }),
  )

  router.get("/sync/runs", (request, response) => {
    response.json({
      runs: listSyncRuns(database, {
        limit: request.query.limit as string,
        jobId: request.query.jobId,
        status: request.query.status as string,
      }),
    })
  })
  router.get("/sync/jobs", (request, response) => {
    response.json({ jobs: jobs?.list(request.query.limit as string) || [] })
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

  router.get("/schedule-assets", (request, response) => {
    response.json({
      assets: listScheduleAssetReviews(database, {
        limit: request.query.limit as string,
      }).map((item) => scheduleAssetPayload(database, item)),
    })
  })
  router.post(
    "/schedule-assets/:id/manual-review",
    route((request, response) => {
      const status = text(request.body?.status, {
        name: "status",
        required: true,
        max: 24,
      })
      if (!["unreviewed", "schedule", "not_schedule"].includes(status)) {
        throw new AdminInputError("status is invalid")
      }
      const reason =
        status === "unreviewed"
          ? null
          : text(request.body?.reason, {
              name: "reason",
              required: true,
              max: 500,
            })
      const item = updateScheduleAssetManualReview(
        database,
        request.params.id,
        { status, reason },
      )
      if (!item) {
        response.status(404).json({ error: "schedule_asset_not_found" })
        return
      }
      bumpDashboardRevision(database)
      response.json({ asset: scheduleAssetPayload(database, item) })
    }),
  )

  router.get("/posts/llm", (request, response) => {
    response.json({
      posts: listPostLlmStates(database, {
        limit: request.query.limit as string,
        status: request.query.status as string,
        reprocessRequested: queryBoolean(request.query.reprocessRequested),
      }),
    })
  })
  router.post(
    "/posts/:id/llm/reprocess",
    route((request, response) => {
      const state = requestPostLlmReprocess(
        database,
        request.params.id,
        request.body?.route,
      )
      if (!state) {
        response.status(404).json({ error: "post_not_found" })
        return
      }
      response.json({ state })
    }),
  )

  router.get("/events", (request, response) => {
    const hasPagination =
      (request.query.pageSize as string) != null ||
      (request.query.page as string) != null ||
      (request.query.search as string) != null ||
      (request.query.provenance as string) != null ||
      (request.query.source as string) != null ||
      (request.query.from as string) != null ||
      (request.query.to as string) != null
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
        page: request.query.page as string,
        pageSize: request.query.pageSize as string,
        includeDeleted: queryBoolean(request.query.includeDeleted),
        search: request.query.search as string,
        provenance: request.query.provenance as string,
        source: request.query.source as string,
        from: request.query.from as string,
        to: request.query.to as string,
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
