import "dotenv/config"

import path from "node:path"
import { fileURLToPath } from "node:url"
import { XMLParser } from "fast-xml-parser"

import {
  finishSyncRun,
  bumpDashboardRevision,
  getScheduleExtractionConfig,
  getKnownPostIds,
  getSyncState,
  getVideoRecord,
  initializeDatabase,
  listScheduleAssets,
  listScheduleCandidatePosts,
  listActiveVideoIds,
  startSyncRun,
  setAppSetting,
  upsertAssets,
  upsertEvents,
  upsertPosts,
  upsertProfile,
  upsertSyncState,
  upsertVideos,
} from "../server/database.js"
import { downloadPendingMedia } from "../server/media-downloader.js"
import { extractPendingSchedules } from "../server/schedule-extractor.js"

const DEFAULT_X_HANDLES = ["kano_2525", "_Kanotic"]
const DEFAULT_YOUTUBE_CHANNEL = "UCShXNLMXCfstmWKH_q86B8w"
const X_STATUS_HOSTS = new Set([
  "x.com",
  "www.x.com",
  "twitter.com",
  "www.twitter.com",
  "mobile.twitter.com",
])
const TWITTER_EPOCH_MS = 1_288_834_974_657n
const timeoutMs = boundedInteger(
  process.env.SYNC_TIMEOUT_MS,
  7000,
  1000,
  60_000,
)
const requestDelayMs = boundedInteger(
  process.env.SYNC_REQUEST_DELAY_MS,
  150,
  0,
  10_000,
)
const userAgent = "kano-status-board/0.1 (+local sync)"

const japanDateFormatter = new Intl.DateTimeFormat("en-CA", {
  timeZone: "Asia/Tokyo",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
})

function japanDateKey(value) {
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) return null
  const parts = japanDateFormatter
    .formatToParts(date)
    .reduce((result, part) => {
      result[part.type] = part.value
      return result
    }, {})
  return `${parts.year}-${parts.month}-${parts.day}`
}

function weekStartInJapan(value) {
  const key = japanDateKey(value)
  if (!key) return null
  const [year, month, day] = key.split("-").map(Number)
  const date = new Date(Date.UTC(year, month - 1, day))
  const daysSinceMonday = (date.getUTCDay() + 6) % 7
  date.setUTCDate(date.getUTCDate() - daysSinceMonday)
  return date.toISOString().slice(0, 10)
}

function boundedInteger(value, fallback, minimum, maximum) {
  const parsed = Number.parseInt(String(value ?? ""), 10)
  return Number.isInteger(parsed)
    ? Math.min(maximum, Math.max(minimum, parsed))
    : fallback
}

function normalizeXHandle(value) {
  const handle = String(value || "")
    .trim()
    .replace(/^@/u, "")
  return /^[A-Za-z0-9_]{1,30}$/u.test(handle) ? handle : null
}

function configuredXHandles() {
  const configured = process.env.X_HANDLES || process.env.X_HANDLE
  const values = configured
    ? String(configured).split(/[\n,，]+/u)
    : DEFAULT_X_HANDLES
  const handles = [...new Set(values.map(normalizeXHandle).filter(Boolean))]
  return handles.length ? handles : [...DEFAULT_X_HANDLES]
}

function matchesAnyKeyword(value, keywords = []) {
  const haystack = String(value || "").toLocaleLowerCase()
  return keywords.some((keyword) =>
    haystack.includes(String(keyword || "").toLocaleLowerCase()),
  )
}

function compareSnowflakeIds(left, right) {
  const leftValue = String(left || "")
  const rightValue = String(right || "")
  if (!/^\d+$/u.test(leftValue) || !/^\d+$/u.test(rightValue)) return null
  return BigInt(leftValue) === BigInt(rightValue)
    ? 0
    : BigInt(leftValue) > BigInt(rightValue)
      ? 1
      : -1
}

function delay(milliseconds) {
  return milliseconds > 0
    ? new Promise((resolve) => setTimeout(resolve, milliseconds))
    : Promise.resolve()
}

function asArray(value) {
  if (value == null) return []
  return Array.isArray(value) ? value : [value]
}

function parseDate(value) {
  if (value == null || value === "") return null
  const numeric = Number(value)
  if (Number.isFinite(numeric) && numeric > 0) {
    const milliseconds = numeric < 1e12 ? numeric * 1000 : numeric
    const date = new Date(milliseconds)
    if (!Number.isNaN(date.getTime())) return date.toISOString()
  }
  const date = new Date(String(value))
  return Number.isNaN(date.getTime()) ? null : date.toISOString()
}

function snowflakeDate(id) {
  try {
    const milliseconds = (BigInt(id) >> 22n) + TWITTER_EPOCH_MS
    const date = new Date(Number(milliseconds))
    return Number.isNaN(date.getTime()) ? null : date
  } catch {
    return null
  }
}

function number(value) {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : 0
}

function decodeHtml(value = "") {
  return String(value)
    .replace(/&#(\d+);/g, (_match, code) => String.fromCodePoint(Number(code)))
    .replace(/&#x([\da-f]+);/gi, (_match, code) =>
      String.fromCodePoint(Number.parseInt(code, 16)),
    )
    .replace(/&quot;/g, '"')
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&#39;/g, "'")
}

async function fetchResponse(url, options = {}) {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)
  try {
    const response = await fetch(url, {
      ...options,
      signal: controller.signal,
      headers: {
        "user-agent": userAgent,
        accept:
          "text/html,application/xhtml+xml,application/xml,application/json;q=0.9,*/*;q=0.8",
        ...(options.headers || {}),
      },
    })
    if (!response.ok) {
      const retryAfter = response.headers.get("retry-after")
      throw new Error(
        `${response.status} ${response.statusText}${retryAfter ? `; retry after ${retryAfter}` : ""}`,
      )
    }
    return response
  } finally {
    clearTimeout(timer)
  }
}

async function fetchText(url, options = {}) {
  return fetchResponse(url, options).then((response) => response.text())
}

async function fetchJson(url, options = {}) {
  return fetchResponse(url, options).then((response) => response.json())
}

function inferPostType(text = "") {
  if (/schedule|スケジュール|配信予定|週間予定|今週の予定/i.test(text)) {
    return { type: "notice", label: "SCHEDULE / 日程" }
  }
  if (/お知らせ|公告|公開|official|所属|担当|発売|告知/i.test(text)) {
    return { type: "notice", label: "ANNOUNCEMENT" }
  }
  return { type: "daily", label: "DAILY / 近况" }
}

function getMediaUrls(payload) {
  return getMediaUrlsAtDepth(payload, 0)
}

function getQuotedPayload(payload) {
  return (
    [
      payload?.qrt,
      payload?.quotedTweet,
      payload?.quoted_tweet,
      payload?.quote,
    ].find((candidate) => candidate && typeof candidate === "object") || null
  )
}

function getTweetText(payload) {
  return payload?.text || payload?.tweetText || payload?.description || ""
}

function getMediaUrlsAtDepth(payload, depth) {
  if (!payload || typeof payload !== "object" || depth > 2) return []
  const candidates = [
    ...asArray(payload?.mediaURLs),
    ...asArray(payload?.media_urls),
    ...asArray(payload?.media_extended).map(
      (item) => item?.url || item?.media_url,
    ),
  ]
  const quoted = getQuotedPayload(payload)
  if (quoted) candidates.push(...getMediaUrlsAtDepth(quoted, depth + 1))
  return [
    ...new Set(
      candidates.filter(
        (value) => typeof value === "string" && value.length > 0,
      ),
    ),
  ]
}

function statusIdFromUrl(value) {
  if (typeof value !== "string" || value.trim() === "") return null
  try {
    const url = new URL(value)
    if (!X_STATUS_HOSTS.has(url.hostname.toLowerCase())) return null
    return url.pathname.match(/^\/[^/]+\/status\/(\d+)/u)?.[1] || null
  } catch {
    return null
  }
}

function handleFromStatusUrl(value) {
  if (typeof value !== "string" || value.trim() === "") return null
  try {
    const url = new URL(value)
    if (!X_STATUS_HOSTS.has(url.hostname.toLowerCase())) return null
    const match = url.pathname.match(/^\/([^/]+)\/status\/\d+/u)
    return match ? normalizeXHandle(decodeURIComponent(match[1])) : null
  } catch {
    return null
  }
}

function getTweetId(payload) {
  return String(payload?.tweetID || payload?.tweetId || payload?.id || "")
}

function mapTweet(payload, fallbackId, handle = DEFAULT_X_HANDLES[0]) {
  const id = getTweetId(payload) || String(fallbackId)
  const text = getTweetText(payload)
  const publishedAt = parseDate(
    payload?.date_epoch ??
      payload?.dateEpoch ??
      payload?.date ??
      payload?.created_at,
  )
  const mediaUrls = getMediaUrls(payload)
  if (!id || (!text && !mediaUrls.length) || !publishedAt) return null
  const inferred = inferPostType(text)
  const quotedText = getTweetText(getQuotedPayload(payload))
  const searchText = [text, quotedText].filter(Boolean).join("\n")
  const author = payload?.author || payload?.user || {}
  const authorName =
    author?.name || author?.displayName || payload?.user_name || null
  const authorScreenName =
    author?.screenName ||
    author?.screen_name ||
    payload?.user_screen_name ||
    null
  const authorAvatar =
    author?.avatarURL ||
    author?.avatar_url ||
    author?.profileImageUrl ||
    payload?.user_profile_image_url ||
    null
  return {
    id,
    source: "x",
    account_handle: handle,
    type: inferred.type,
    label: inferred.label,
    text,
    search_text: searchText,
    published_at: publishedAt,
    url: (
      payload?.tweetURL ||
      payload?.url ||
      `https://x.com/${handle}/status/${id}`
    ).replace("twitter.com", "x.com"),
    likes: number(payload?.likes ?? payload?.likeCount),
    reposts: number(
      payload?.retweets ?? payload?.reposts ?? payload?.retweetCount,
    ),
    replies: number(payload?.replies ?? payload?.replyCount),
    media_url: mediaUrls[0] || null,
    media_urls: mediaUrls,
    media_alt: null,
    author_name: authorName,
    author_screen_name: authorScreenName,
    author_avatar: authorAvatar,
    raw: payload,
  }
}

function extractStatusIds(html) {
  const decoded = decodeHtml(html).replace(/\\u002F/g, "/")
  const ids = new Set()
  for (const match of decoded.matchAll(/(?:status%2F|status\/)(\d{10,})/gi)) {
    ids.add(match[1])
  }
  return [...ids].sort((a, b) => (BigInt(a) > BigInt(b) ? -1 : 1))
}

function collectScheduleRefreshCandidates(
  database,
  handle,
  scheduleKeywords,
  { includePostCandidates = false } = {},
) {
  const candidates = []
  const seen = new Set()
  let hasScheduleAssetSource = false
  const add = ({
    sourceUrl,
    sourceAccount = null,
    mediaUrl = null,
    weekStart = null,
    updatedAt = null,
    publishedAt = null,
  }) => {
    if (mediaUrl) return
    const id = statusIdFromUrl(sourceUrl)
    if (!id || seen.has(id)) return
    const sourceHandle =
      normalizeXHandle(sourceAccount) || handleFromStatusUrl(sourceUrl)
    if (
      sourceHandle &&
      sourceHandle.toLocaleLowerCase() !== handle.toLocaleLowerCase()
    ) {
      return
    }
    seen.add(id)
    candidates.push({
      id,
      weekStart,
      publishedAt: publishedAt || updatedAt || snowflakeDate(id)?.toISOString(),
    })
  }

  for (const asset of listScheduleAssets(database, { limit: 100 })) {
    const sourceHandle =
      normalizeXHandle(asset.sourceAccount) ||
      handleFromStatusUrl(asset.sourceUrl)
    if (
      sourceHandle &&
      sourceHandle.toLocaleLowerCase() === handle.toLocaleLowerCase()
    ) {
      hasScheduleAssetSource = true
    }
    add({
      sourceUrl: asset.sourceUrl,
      sourceAccount: asset.sourceAccount,
      mediaUrl: asset.url,
      weekStart: asset.weekStart,
      updatedAt: asset.updatedAt,
    })
  }
  if (includePostCandidates && !hasScheduleAssetSource) {
    for (const post of listScheduleCandidatePosts(database, {
      limit: 100,
      keywords: scheduleKeywords,
    })) {
      add({
        sourceUrl: post.url,
        sourceAccount: post.accountHandle,
        mediaUrl: post.mediaUrl,
        publishedAt: post.publishedAt,
      })
    }
  }

  return candidates.sort((left, right) => {
    const byId = compareSnowflakeIds(right.id, left.id)
    if (byId != null && byId !== 0) return byId
    return String(right.weekStart || right.publishedAt || "").localeCompare(
      String(left.weekStart || left.publishedAt || ""),
    )
  })
}

async function syncXAccount(
  database,
  handle,
  {
    discoveryLimit,
    requestLimit,
    bootstrapDays,
    refreshKnown,
    scheduleKeywords = [],
    scheduleRefreshLimit = 1,
    isPrimary = false,
  },
) {
  const profileUrl = `https://x.com/${handle}`
  const html = await fetchText(profileUrl)
  const discoveredIds = extractStatusIds(html).slice(0, discoveryLimit)
  const state = getSyncState(database, "x", handle)
  const cutoff = Date.now() - bootstrapDays * 24 * 60 * 60 * 1000
  const scheduleCandidates = collectScheduleRefreshCandidates(
    database,
    handle,
    scheduleKeywords,
    { includePostCandidates: !state },
  )
    .filter((candidate) => {
      const createdAt = snowflakeDate(candidate.id)
      return createdAt && createdAt.getTime() >= cutoff
    })
    .slice(0, scheduleRefreshLimit)
  const scheduleIds = scheduleCandidates.map((candidate) => candidate.id)
  const ids = [...new Set([...scheduleIds, ...discoveredIds])]
  if (!ids.length) throw new Error("无法从 X 公开页面找到状态 ID")
  if (requestLimit <= 0) {
    return {
      handle,
      count: 0,
      discovered: discoveredIds.length,
      supplemented: scheduleIds.length,
      requested: 0,
      scheduleAssets: 0,
      scheduleAsset: null,
      errors: ["X 状态请求预算已耗尽"],
    }
  }

  const knownIds = getKnownPostIds(database, ids)
  const priorityIds = new Set(scheduleIds)
  const cursorTime = Date.parse(state?.cursorTime || "")
  const isAfterCursor = (id) => {
    if (!state) return true
    const byId = compareSnowflakeIds(id, state.cursorId)
    if (byId != null) return byId > 0
    const createdAt = snowflakeDate(id)?.getTime()
    return Number.isFinite(createdAt) && Number.isFinite(cursorTime)
      ? createdAt > cursorTime
      : true
  }
  let knownRefreshes = 0
  const candidates = ids
    .filter((id) => {
      const createdAt = snowflakeDate(id)
      if (!state && createdAt && createdAt.getTime() < cutoff) return false
      if (priorityIds.has(id)) return true
      if (!knownIds.has(id)) return isAfterCursor(id)
      if (knownRefreshes < refreshKnown) {
        knownRefreshes += 1
        return true
      }
      return false
    })
    .slice(0, Math.max(0, requestLimit))

  const posts = []
  const errors = []
  let requestedCount = 0
  for (const [index, id] of candidates.entries()) {
    requestedCount += 1
    try {
      const payload = await fetchJson(
        `https://api.vxtwitter.com/${handle}/status/${id}`,
      )
      const post = mapTweet(payload, id, handle)
      const screenName = post?.author_screen_name?.replace(/^@/u, "")
      if (
        post &&
        (!screenName || screenName.toLowerCase() === handle.toLowerCase())
      ) {
        posts.push(post)
      }
    } catch (error) {
      errors.push(`${id}: ${error.message}`)
    }
    if (index < candidates.length - 1) await delay(requestDelayMs)
  }
  if (candidates.length && !posts.length) {
    const error = new Error(
      `X 状态接口没有返回可用动态${errors.length ? ` (${errors[0]})` : ""}`,
    )
    error.requested = requestedCount
    throw error
  }

  if (posts.length) upsertPosts(database, posts)
  const schedulePost = posts
    .filter(
      (post) =>
        matchesAnyKeyword(post.search_text || post.text, scheduleKeywords) &&
        post.media_url,
    )
    .sort((a, b) => Date.parse(b.published_at) - Date.parse(a.published_at))[0]
  const scheduleAsset = schedulePost
    ? {
        id: `schedule-${weekStartInJapan(schedulePost.published_at) || schedulePost.id}`,
        kind: "schedule",
        url: schedulePost.media_url,
        source_url: schedulePost.url,
        alt: schedulePost.media_alt || "Kano Mahoro weekly schedule",
        week_start: weekStartInJapan(schedulePost.published_at),
        source_account: handle,
        updated_at: schedulePost.published_at,
      }
    : null

  const newestPost = [...posts].sort(
    (left, right) =>
      Date.parse(right.published_at) - Date.parse(left.published_at),
  )[0]
  const newestDiscoveredId = discoveredIds[0] || ids[0]
  const nextCursorId = newestPost?.id || newestDiscoveredId
  const nextCursorTime =
    newestPost?.published_at || snowflakeDate(newestDiscoveredId)?.toISOString()
  const shouldAdvanceCursor =
    !state || isAfterCursor(nextCursorId) || !state.cursorId
  upsertSyncState(database, {
    source: "x",
    accountId: handle,
    cursorId: shouldAdvanceCursor ? nextCursorId : state.cursorId,
    cursorTime: shouldAdvanceCursor ? nextCursorTime : state.cursorTime,
    metadata: {
      handle,
      discovered: discoveredIds.length,
      requested: candidates.length,
      supplemented: scheduleIds.length,
      bootstrap: !state,
    },
  })
  return {
    handle,
    count: posts.length,
    discovered: discoveredIds.length,
    requested: candidates.length,
    supplemented: scheduleIds.length,
    scheduleCandidates: scheduleIds.length,
    scheduleAssets: scheduleAsset ? 1 : 0,
    scheduleAsset,
    errors,
  }
}

export async function syncX(database) {
  const handles = configuredXHandles()
  const scheduleKeywords = getScheduleExtractionConfig(database).keywords
  const discoveryLimit = boundedInteger(
    process.env.X_DISCOVERY_LIMIT,
    50,
    1,
    200,
  )
  const requestLimit = boundedInteger(
    process.env.X_MAX_STATUS_REQUESTS,
    12,
    1,
    50,
  )
  const bootstrapDays = boundedInteger(process.env.X_BOOTSTRAP_DAYS, 7, 1, 30)
  const refreshKnown = boundedInteger(process.env.X_REFRESH_KNOWN, 1, 0, 5)
  const scheduleRefreshLimit = boundedInteger(
    process.env.X_SCHEDULE_REFRESH_LIMIT,
    1,
    0,
    5,
  )
  const accounts = []
  const errors = []
  let remainingRequests = requestLimit
  let requestedTotal = 0
  for (const [index, handle] of handles.entries()) {
    const remainingAccounts = handles.length - index
    const accountLimit =
      remainingRequests > 0
        ? Math.max(1, Math.ceil(remainingRequests / remainingAccounts))
        : 0
    try {
      const result = await syncXAccount(database, handle, {
        discoveryLimit,
        requestLimit: accountLimit,
        bootstrapDays,
        refreshKnown,
        scheduleKeywords,
        scheduleRefreshLimit,
        isPrimary: index === 0,
      })
      accounts.push(result)
      requestedTotal += result.requested
      remainingRequests = Math.max(0, remainingRequests - result.requested)
    } catch (error) {
      const consumed = Math.max(0, Number(error.requested) || 0)
      requestedTotal += consumed
      remainingRequests = Math.max(0, remainingRequests - consumed)
      errors.push(`${handle}: ${error.message}`)
    }
  }
  if (!accounts.length) {
    throw new Error(
      `X 所有账号同步失败${errors.length ? ` (${errors[0]})` : ""}`,
    )
  }
  const scheduleAssets = accounts
    .map((account) => account.scheduleAsset)
    .filter(Boolean)
  const scheduleAsset = [...scheduleAssets].sort(
    (a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at),
  )[0]
  if (scheduleAssets.length) upsertAssets(database, scheduleAssets)
  // Keep the old well-known ID as a compatibility alias for older snapshots;
  // the dashboard prefers the week-qualified records when both exist.
  if (scheduleAsset) {
    upsertAssets(database, [{ ...scheduleAsset, id: "weekly-schedule" }])
  }
  setAppSetting(database, "x_accounts", JSON.stringify(handles))
  return {
    count: accounts.reduce((sum, account) => sum + account.count, 0),
    discovered: accounts.reduce((sum, account) => sum + account.discovered, 0),
    supplemented: accounts.reduce(
      (sum, account) => sum + (account.supplemented || 0),
      0,
    ),
    requested: requestedTotal,
    scheduleAssets: scheduleAssets.length,
    accounts: accounts.map(
      ({ scheduleAsset: _scheduleAsset, ...account }) => account,
    ),
    errors: [
      ...errors,
      ...accounts.flatMap((account) =>
        account.errors.map((message) => `${account.handle}: ${message}`),
      ),
    ],
  }
}

function entryValue(entry, key) {
  const value = entry?.[key]
  return Array.isArray(value) ? value[0] : value
}

function classifyVideo(title = "") {
  if (/リズム天国|ゲーム|gameplay/i.test(title)) return "GAMEPLAY"
  if (/雑談|talk|おつかれ/i.test(title)) return "TALK"
  if (/初配信|配信|live/i.test(title)) return "LIVE ARCHIVE"
  if (/歌枠|song|music|cover|歌唱/i.test(title)) return "MUSIC"
  return "VIDEO"
}

function mapFeedEntry(entry) {
  const id = entryValue(entry, "yt:videoId") || entryValue(entry, "videoId")
  if (!id) return null
  const title = decodeHtml(entryValue(entry, "title") || "")
  const publishedAt = parseDate(
    entryValue(entry, "published") || entryValue(entry, "updated"),
  )
  const group = entry?.["media:group"] || {}
  const thumbnail =
    group?.["media:thumbnail"]?.["@_url"] ||
    group?.["media:thumbnail"]?.url ||
    null
  const link = asArray(entry?.link).find(
    (item) => item?.["@_rel"] === "alternate",
  )
  return {
    id: String(id),
    source: "youtube",
    title: title || "YouTube 更新",
    published_at: publishedAt,
    scheduled_at: null,
    url: link?.["@_href"] || `https://www.youtube.com/watch?v=${id}`,
    thumbnail_url: thumbnail || `https://i.ytimg.com/vi/${id}/hqdefault.jpg`,
    thumbnail_urls: [thumbnail || `https://i.ytimg.com/vi/${id}/hqdefault.jpg`],
    kind: classifyVideo(title),
    is_upcoming: false,
  }
}

function extractMeta(html, name) {
  const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
  const patterns = [
    new RegExp(
      `<meta[^>]+(?:property|name)=["']${escaped}["'][^>]+content=["']([^"']+)["']`,
      "i",
    ),
    new RegExp(
      `<meta[^>]+content=["']([^"']+)["'][^>]+(?:property|name)=["']${escaped}["']`,
      "i",
    ),
  ]
  for (const pattern of patterns) {
    const match = html.match(pattern)
    if (match) return decodeHtml(match[1])
  }
  return null
}

function extractJsonField(html, field) {
  const escapedField = field.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
  const normalized = html.replace(/\\\\"/g, '"').replace(/\\\\\//g, "/")
  const patterns = [
    new RegExp(`"${escapedField}"\\s*:\\s*"([^"]+)"`),
    new RegExp(`${escapedField}\\s*[:=]\\s*["']([^"']+)["']`),
  ]
  for (const pattern of patterns) {
    const match = normalized.match(pattern)
    if (match) return match[1].replace(/\\u0026/g, "&").replace(/\\\//g, "/")
  }
  return null
}

function extractYoutubeIds(html) {
  const ids = new Set()
  const decoded = html.replace(/\\u002F/g, "/")
  for (const match of decoded.matchAll(
    /(?:watch\?v=|videoId["']?\s*[:=]\s*["']|\/live\/)([A-Za-z0-9_-]{11})/g,
  )) {
    ids.add(match[1])
  }
  return [...ids]
}

export async function syncYoutube(database) {
  const channelId = process.env.YOUTUBE_CHANNEL_ID || DEFAULT_YOUTUBE_CHANNEL
  const rssUrl = `https://www.youtube.com/feeds/videos.xml?channel_id=${channelId}`
  const feedXml = await fetchText(rssUrl)
  const parser = new XMLParser({
    ignoreAttributes: false,
    attributeNamePrefix: "@_",
    trimValues: true,
  })
  const parsed = parser.parse(feedXml)
  const entries = asArray(parsed?.feed?.entry)
  const feedVideos = entries.map(mapFeedEntry).filter(Boolean)
  if (!feedVideos.length) throw new Error("YouTube RSS 没有返回视频")
  const state = getSyncState(database, "youtube", channelId)
  const bootstrapLimit = boundedInteger(
    process.env.YOUTUBE_BOOTSTRAP_VIDEOS,
    6,
    1,
    30,
  )
  const cursorIndex = state?.cursorId
    ? feedVideos.findIndex((video) => video.id === state.cursorId)
    : -1
  const cursorTime = Date.parse(state?.cursorTime || "")
  const incrementalVideos = state
    ? cursorIndex >= 0
      ? feedVideos.slice(0, cursorIndex)
      : feedVideos.filter((video) => {
          const publishedAt = Date.parse(video.published_at || "")
          return (
            !Number.isFinite(cursorTime) ||
            (Number.isFinite(publishedAt) && publishedAt > cursorTime)
          )
        })
    : []
  const videos = state
    ? incrementalVideos.filter((video) => !getVideoRecord(database, video.id))
    : feedVideos.slice(0, bootstrapLimit)
  if (videos.length) upsertVideos(database, videos)

  let streamHtml = ""
  const streamErrors = []
  try {
    streamHtml = await fetchText(
      `https://www.youtube.com/channel/${channelId}/streams`,
    )
  } catch (error) {
    streamErrors.push(`streams: ${error.message}`)
  }

  const now = Date.now()
  const streamIds = extractYoutubeIds(streamHtml)
  const activeIds = listActiveVideoIds(database, { limit: 30 }).filter((id) => {
    const existing = getVideoRecord(database, id)
    if (!existing) return false
    const scheduled = Date.parse(existing.scheduledAt || "")
    return (
      Boolean(existing.isUpcoming) ||
      (Number.isFinite(scheduled) && scheduled >= now - 86_400_000)
    )
  })
  const detailLimit = boundedInteger(
    process.env.YOUTUBE_MAX_DETAIL_REQUESTS,
    12,
    1,
    50,
  )
  const detailIds = [...new Set([...streamIds, ...activeIds])]
    .filter((id) => {
      const existing = getVideoRecord(database, id)
      if (!existing) return true
      const scheduled = Date.parse(existing.scheduledAt || "")
      return (
        Boolean(existing.isUpcoming) ||
        (Number.isFinite(scheduled) && scheduled >= now - 86_400_000)
      )
    })
    .slice(0, detailLimit)

  const inspectedVideos = []
  const reservationEvents = []
  for (const [index, id] of detailIds.entries()) {
    try {
      const html = await fetchText(`https://www.youtube.com/watch?v=${id}`)
      const existing = getVideoRecord(database, id)
      const scheduledAt = parseDate(
        extractJsonField(html, "scheduledStartTime") ||
          extractJsonField(html, "startTimestamp"),
      )
      const isUpcomingFlag = /["']isUpcoming["']\s*:\s*true/i.test(html)
      const title =
        extractMeta(html, "og:title") ||
        extractMeta(html, "twitter:title") ||
        existing?.title ||
        `YouTube Live ${id}`
      const thumbnail =
        extractMeta(html, "og:image") ||
        existing?.thumbnailUrl ||
        `https://i.ytimg.com/vi/${id}/hqdefault.jpg`
      const effectiveScheduledAt = scheduledAt || existing?.scheduledAt || null
      const upcoming =
        isUpcomingFlag ||
        Boolean(
          effectiveScheduledAt &&
          Date.parse(effectiveScheduledAt) >= Date.now(),
        )
      const video = {
        id,
        source: "youtube",
        title,
        published_at: existing?.publishedAt || null,
        scheduled_at: effectiveScheduledAt,
        url: `https://www.youtube.com/watch?v=${id}`,
        thumbnail_url: thumbnail,
        thumbnail_urls: [thumbnail],
        kind: upcoming ? "UPCOMING LIVE" : "LIVE ARCHIVE",
        is_upcoming: upcoming,
      }
      inspectedVideos.push(video)
      if (effectiveScheduledAt) {
        const memberOnly = /メンバー|membership|限定/i.test(title)
        reservationEvents.push({
          id: `youtube-${id}`,
          source: "youtube",
          source_item_id: id,
          source_key: "reservation",
          title,
          detail: memberOnly
            ? "メンバーシップ限定 / YouTube"
            : "YouTube Live / 预约",
          starts_on: japanDateFormatter.format(new Date(effectiveScheduledAt)),
          starts_at: effectiveScheduledAt,
          status: upcoming ? (memberOnly ? "会员限定" : "已预约") : "已完成",
          event_type: memberOnly ? "member" : "stream",
          url: `https://www.youtube.com/watch?v=${id}`,
          is_upcoming: upcoming,
          provenance: "automatic",
        })
      }
    } catch (error) {
      streamErrors.push(`${id}: ${error.message}`)
    }
    if (index < detailIds.length - 1) await delay(requestDelayMs)
  }
  if (inspectedVideos.length) upsertVideos(database, inspectedVideos)
  if (reservationEvents.length) upsertEvents(database, reservationEvents)

  const newest = feedVideos
    .filter((video) => video.published_at)
    .sort((a, b) => Date.parse(b.published_at) - Date.parse(a.published_at))[0]
  upsertSyncState(database, {
    source: "youtube",
    accountId: channelId,
    cursorId: newest?.id || feedVideos[0].id,
    cursorTime: newest?.published_at || null,
    metadata: {
      feedEntries: feedVideos.length,
      inserted: videos.length,
      bootstrap: !state,
      streamIds: streamIds.length,
      inspected: detailIds.length,
    },
  })
  return {
    count: videos.length,
    discovered: feedVideos.length,
    inspected: inspectedVideos.length,
    upcoming: reservationEvents.filter((event) => event.is_upcoming).length,
    errors: streamErrors,
  }
}

export async function runSync({
  database = null,
  closeDatabase = null,
  triggeredBy = "cli",
  jobId = null,
  setExitCode = true,
} = {}) {
  const ownsDatabase = !database
  const activeDatabase = database || initializeDatabase()
  const shouldClose = closeDatabase ?? ownsDatabase
  const runId = startSyncRun(activeDatabase, "manual", {
    triggeredBy,
    jobId,
  })
  const results = {}
  let successCount = 0
  let attempted = 0

  try {
    if (process.env.SKIP_X !== "1") {
      attempted += 1
      try {
        results.x = await syncX(activeDatabase)
        successCount += 1
        console.log(
          `X: ${results.x.count} 条更新，${results.x.requested} 次状态请求`,
        )
      } catch (error) {
        results.x = { error: error.message }
        console.error(`X 同步失败: ${error.message}`)
      }
    }

    if (process.env.SKIP_YOUTUBE !== "1") {
      attempted += 1
      try {
        results.youtube = await syncYoutube(activeDatabase)
        successCount += 1
        console.log(
          `YouTube: RSS ${results.youtube.count} 条，检查 ${results.youtube.inspected} 条预约`,
        )
      } catch (error) {
        results.youtube = { error: error.message }
        console.error(`YouTube 同步失败: ${error.message}`)
      }
    }

    if (process.env.SKIP_MEDIA !== "1") {
      results.media = await downloadPendingMedia(activeDatabase, {
        limit: boundedInteger(process.env.MEDIA_DOWNLOAD_LIMIT, 20, 0, 100),
        timeoutMs: boundedInteger(
          process.env.MEDIA_DOWNLOAD_TIMEOUT_MS,
          10_000,
          1000,
          60_000,
        ),
        maxBytes: boundedInteger(
          process.env.MEDIA_MAX_BYTES,
          10 * 1024 * 1024,
          1024,
          25 * 1024 * 1024,
        ),
      })
    }

    if (process.env.SKIP_LLM !== "1") {
      results.schedules = await extractPendingSchedules(activeDatabase)
    }

    const hasWarnings =
      Object.values(results).some(
        (result) => Array.isArray(result?.errors) && result.errors.length > 0,
      ) ||
      Number(results.media?.failed || 0) > 0 ||
      Number(results.schedules?.failed || 0) > 0
    const status =
      attempted === 0 || successCount === attempted
        ? hasWarnings
          ? "partial"
          : "success"
        : successCount > 0
          ? "partial"
          : "failed"
    finishSyncRun(activeDatabase, runId, {
      status,
      message:
        status === "success"
          ? "同步完成"
          : "部分数据源不可用或存在警告，保留已有快照",
      counts: results,
    })
    bumpDashboardRevision(activeDatabase)
    if (status === "failed" && setExitCode) process.exitCode = 1
    return { status, results }
  } catch (error) {
    finishSyncRun(activeDatabase, runId, {
      status: "failed",
      message: "同步异常终止，保留已有快照",
      counts: { ...results, fatal: { error: error.message } },
    })
    bumpDashboardRevision(activeDatabase)
    throw error
  } finally {
    if (shouldClose) activeDatabase.close()
  }
}

const isMain =
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)

if (isMain) await runSync()

export {
  extractJsonField,
  extractMeta,
  extractStatusIds,
  extractYoutubeIds,
  mapFeedEntry,
  mapTweet,
  parseDate,
  snowflakeDate,
}
