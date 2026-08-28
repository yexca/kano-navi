import { XMLParser } from "fast-xml-parser"
import {
  finishSyncRun,
  initializeDatabase,
  startSyncRun,
  upsertAssets,
  upsertEvents,
  upsertPosts,
  upsertProfile,
  upsertVideos,
} from "../server/database.js"

const DEFAULT_X_HANDLE = "kano_2525"
const DEFAULT_YOUTUBE_CHANNEL = "UCShXNLMXCfstmWKH_q86B8w"
const timeoutMs = Number(process.env.SYNC_TIMEOUT_MS || 7000)
const userAgent = "kano-status-board/0.1 (+local sync)"

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
    if (!response.ok)
      throw new Error(`${response.status} ${response.statusText}`)
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
  if (/schedule|スケジュール|予定/i.test(text))
    return { type: "notice", label: "SCHEDULE / 日程" }
  if (/お知らせ|公告|公開|official|所属|担当|発売|告知/i.test(text))
    return { type: "notice", label: "ANNOUNCEMENT" }
  return { type: "daily", label: "DAILY / 近况" }
}

function getMediaUrls(payload) {
  const candidates = [
    ...(Array.isArray(payload?.mediaURLs) ? payload.mediaURLs : []),
    ...(Array.isArray(payload?.media_urls) ? payload.media_urls : []),
    ...(Array.isArray(payload?.media_extended)
      ? payload.media_extended.map((item) => item?.url || item?.media_url)
      : []),
  ]
  return [
    ...new Set(
      candidates.filter(
        (value) => typeof value === "string" && value.length > 0,
      ),
    ),
  ]
}

function getTweetId(payload) {
  return String(payload?.tweetID || payload?.tweetId || payload?.id || "")
}

function mapTweet(payload, fallbackId) {
  const id = getTweetId(payload) || String(fallbackId)
  const text = payload?.text || payload?.tweetText || payload?.description || ""
  const publishedAt = parseDate(
    payload?.date_epoch ??
      payload?.dateEpoch ??
      payload?.date ??
      payload?.created_at,
  )
  if (!id || !text || !publishedAt) return null
  const inferred = inferPostType(text)
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
  const mediaUrls = getMediaUrls(payload)
  return {
    id,
    source: "x",
    type: inferred.type,
    label: inferred.label,
    text,
    published_at: publishedAt,
    url: (
      payload?.tweetURL ||
      payload?.url ||
      `https://x.com/kano_2525/status/${id}`
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
  for (const match of decoded.matchAll(/(?:status%2F|status\/)(\d{10,})/gi))
    ids.add(match[1])
  return [...ids].sort((a, b) => (BigInt(a) > BigInt(b) ? -1 : 1))
}

async function syncX(database) {
  const handle = process.env.X_HANDLE || DEFAULT_X_HANDLE
  const profileUrl = `https://x.com/${handle}`
  const html = await fetchText(profileUrl)
  const ids = extractStatusIds(html).slice(0, 10)
  if (!ids.length) throw new Error("无法从 X 公开页面找到状态 ID")

  const posts = []
  const errors = []
  for (const id of ids) {
    try {
      const payload = await fetchJson(
        `https://api.vxtwitter.com/${handle}/status/${id}`,
      )
      const post = mapTweet(payload, id)
      const screenName = post?.author_screen_name
      if (
        post &&
        (!screenName || screenName.toLowerCase() === handle.toLowerCase())
      )
        posts.push(post)
    } catch (error) {
      errors.push(`${id}: ${error.message}`)
    }
    if (posts.length >= 8) break
  }
  if (!posts.length)
    throw new Error(
      `X 状态接口没有返回可用动态${errors.length ? ` (${errors[0]})` : ""}`,
    )

  upsertPosts(database, posts)
  const schedulePost = posts.find(
    (post) =>
      post.type === "notice" &&
      /^SCHEDULE\b/u.test(post.label || "") &&
      post.media_url,
  )
  if (schedulePost) {
    upsertAssets(database, [
      {
        id: "weekly-schedule",
        kind: "schedule",
        url: schedulePost.media_url,
        source_url: schedulePost.url,
        alt: schedulePost.media_alt || "Kano Mahoro weekly schedule",
        updated_at: schedulePost.published_at,
      },
    ])
  }
  const existing = database
    .prepare(`SELECT * FROM profiles ORDER BY id LIMIT 1`)
    .get()
  const author = posts.find((post) => post.author_name || post.author_avatar)
  if (existing && author) {
    upsertProfile(database, {
      id: existing.id,
      display_name: existing.display_name,
      romanized_name: existing.romanized_name,
      bio: existing.bio,
      avatar_url: existing.avatar_url?.startsWith("/assets/")
        ? existing.avatar_url
        : author.author_avatar || existing.avatar_url,
      banner_url: existing.banner_url,
      x_url: existing.x_url,
      youtube_url: existing.youtube_url,
      updated_at: new Date().toISOString(),
    })
  }
  return { count: posts.length, scheduleAssets: schedulePost ? 1 : 0, errors }
}

function entryValue(entry, key) {
  const value = entry?.[key]
  if (Array.isArray(value)) return value[0]
  return value
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
  ))
    ids.add(match[1])
  return [...ids]
}

async function syncYoutube(database) {
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
  const videos = entries.map(mapFeedEntry).filter(Boolean)
  if (!videos.length) throw new Error("YouTube RSS 没有返回视频")
  upsertVideos(database, videos)

  let streamHtml = ""
  const streamErrors = []
  try {
    streamHtml = await fetchText(
      `https://www.youtube.com/channel/${channelId}/streams`,
    )
  } catch (error) {
    streamErrors.push(`streams: ${error.message}`)
  }

  const streamIds = extractYoutubeIds(streamHtml).slice(0, 12)
  const upcomingVideos = []
  const upcomingEvents = []
  const now = Date.now()
  for (const id of streamIds) {
    try {
      const html = await fetchText(`https://www.youtube.com/watch?v=${id}`)
      const scheduledAt = parseDate(
        extractJsonField(html, "scheduledStartTime") ||
          extractJsonField(html, "startTimestamp"),
      )
      const isUpcomingFlag = /["']isUpcoming["']\s*:\s*true/i.test(html)
      if (!scheduledAt && !isUpcomingFlag) continue
      const title =
        extractMeta(html, "og:title") ||
        extractMeta(html, "twitter:title") ||
        `YouTube Live ${id}`
      const thumbnail =
        extractMeta(html, "og:image") ||
        `https://i.ytimg.com/vi/${id}/hqdefault.jpg`
      const upcoming =
        Boolean(scheduledAt && Date.parse(scheduledAt) >= now) || isUpcomingFlag
      const video = {
        id,
        source: "youtube",
        title,
        published_at: null,
        scheduled_at: scheduledAt,
        url: `https://www.youtube.com/watch?v=${id}`,
        thumbnail_url: thumbnail,
        thumbnail_urls: [thumbnail],
        kind: upcoming ? "UPCOMING LIVE" : "LIVE ARCHIVE",
        is_upcoming: upcoming,
      }
      upcomingVideos.push(video)
      if (upcoming && scheduledAt) {
        const memberOnly = /メンバー|membership|限定/i.test(title)
        upcomingEvents.push({
          id: `youtube-${id}`,
          source: "youtube",
          title,
          detail: memberOnly
            ? "メンバーシップ限定 / YouTube"
            : "YouTube Live / 预约",
          starts_at: scheduledAt,
          status: memberOnly ? "会员限定" : "已预约",
          event_type: memberOnly ? "member" : "stream",
          url: `https://www.youtube.com/watch?v=${id}`,
          is_upcoming: true,
        })
      }
    } catch (error) {
      streamErrors.push(`${id}: ${error.message}`)
    }
  }
  if (upcomingVideos.length) upsertVideos(database, upcomingVideos)
  if (upcomingEvents.length) upsertEvents(database, upcomingEvents)
  return {
    count: videos.length + upcomingVideos.length,
    upcoming: upcomingEvents.length,
    errors: streamErrors,
  }
}

const database = initializeDatabase()
const startedAt = new Date().toISOString()
const runId = startSyncRun(database, "manual", startedAt)
const results = {}
let successCount = 0

try {
  if (process.env.SKIP_X !== "1") {
    try {
      results.x = await syncX(database)
      successCount += 1
      console.log(`X: ${results.x.count} 条动态`)
    } catch (error) {
      results.x = { error: error.message }
      console.error(`X 同步失败: ${error.message}`)
    }
  }

  if (process.env.SKIP_YOUTUBE !== "1") {
    try {
      results.youtube = await syncYoutube(database)
      successCount += 1
      console.log(
        `YouTube: ${results.youtube.count} 条视频，${results.youtube.upcoming} 条预约`,
      )
    } catch (error) {
      results.youtube = { error: error.message }
      console.error(`YouTube 同步失败: ${error.message}`)
    }
  }

  const attempted = ["x", "youtube"].filter(
    (source) => results[source] !== undefined,
  ).length
  const hasWarnings = Object.values(results).some(
    (result) => Array.isArray(result?.errors) && result.errors.length > 0,
  )
  const status =
    successCount === attempted && successCount > 0
      ? hasWarnings
        ? "partial"
        : "success"
      : successCount > 0
        ? "partial"
        : "failed"
  finishSyncRun(database, runId, {
    status,
    message:
      status === "success"
        ? "同步完成"
        : "部分数据源不可用或存在警告，保留已有快照",
    counts: results,
  })
  if (status === "failed") process.exitCode = 1
} finally {
  database.close()
}
