import type { Database as DatabaseConnection } from "better-sqlite3"

import crypto from "node:crypto"
import {
  getSyncState,
  getVideoRecord,
  upsertSyncState,
  upsertPosts,
  upsertScheduleAssetsFromPosts,
  upsertVideos,
} from "../server/database.ts"
import {
  isInFetchWindow,
  normalizeFetchWindow,
  oldestSourceTimestamp,
  resolveFetchWindow,
} from "../server/fetch-window.ts"

function bounded(value, fallback, maximum) {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed >= 1
    ? Math.min(parsed, maximum)
    : fallback
}

// At most 100 X posts or 50 YouTube entries per page, including expansions.
const MAX_SOURCE_API_BYTES = 4 * 1024 * 1024

async function readApiJson(response: Response) {
  const declaredSize = Number(response.headers.get("content-length"))
  if (declaredSize > MAX_SOURCE_API_BYTES) {
    await response.body?.cancel()
    throw new Error("source_api_response_too_large")
  }
  if (!response.body) throw new Error("source_api_invalid_response")
  const reader = response.body.getReader()
  const chunks: Buffer[] = []
  let size = 0
  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      size += value.byteLength
      if (size > MAX_SOURCE_API_BYTES) {
        await reader.cancel()
        throw new Error("source_api_response_too_large")
      }
      chunks.push(Buffer.from(value))
    }
  } finally {
    reader.releaseLock()
  }
  return JSON.parse(Buffer.concat(chunks, size).toString("utf8"))
}

// Credentials stay in headers. Response bodies and fetch exception messages
// can echo credentials, so only a controlled error goes into runs/logs.
async function apiJson(url, headers) {
  let controlledError = "source_api_request_failed"
  try {
    const response = await fetch(url, {
      headers: { accept: "application/json", ...headers },
      signal: AbortSignal.timeout(
        bounded(process.env.SYNC_TIMEOUT_MS, 7000, 60_000),
      ),
      redirect: "error",
    })
    if (!response.ok) {
      controlledError = `source_api_http_${response.status}`
      await response.body?.cancel()
      throw new Error(controlledError)
    }
    try {
      return await readApiJson(response)
    } catch (error) {
      if (error?.message === "source_api_response_too_large")
        controlledError = "source_api_response_too_large"
      throw error
    }
  } catch {
    throw new Error(controlledError)
  }
}

export async function fetchSourceWindow(
  database: DatabaseConnection,
  {
    source,
    account,
    window: input,
    mapTweet,
    classifyVideo = () => "VIDEO",
    pageLimit = null,
    now = Date.now(),
  }: {
    source: any
    account: any
    window: any
    mapTweet?: any
    classifyVideo?: any
    pageLimit?: any
    now?: any
  },
) {
  const token =
    source === "x"
      ? process.env.X_API_BEARER_TOKEN?.trim()
      : process.env.YOUTUBE_API_KEY?.trim()
  if (!token)
    throw new Error(
      source === "x"
        ? "X_API_BEARER_TOKEN is required for history search"
        : "YOUTUBE_API_KEY is required for history search",
    )
  const backfillState = getSyncState(database, `${source}-backfill`, account)
  let oldest = oldestSourceTimestamp(database, source, account)
  if (
    oldest &&
    backfillState?.cursorTime &&
    Date.parse(backfillState.cursorTime) < Date.parse(oldest)
  )
    oldest = backfillState.cursorTime
  let window = resolveFetchWindow(input, { oldest, now })
  // Finish an interrupted backfill interval before opening another interval.
  if (input.mode === "before" && backfillState?.metadata?.pendingWindow)
    window = backfillState.metadata.pendingWindow
  const key = `${account}:${crypto
    .createHash("sha256")
    .update(
      input.mode === "before"
        ? "before"
        : JSON.stringify(normalizeFetchWindow(input)),
    )
    .digest("hex")
    .slice(0, 24)}`
  const checkpoint = getSyncState(database, `${source}-window`, key)
  let nextToken = checkpoint?.cursorId || null
  if (nextToken && checkpoint.metadata?.window) {
    const saved = checkpoint.metadata.window
    if (
      input.mode !== "recent" ||
      Date.parse(saved.endTime) > Date.parse(window.startTime)
    )
      window = saved
    else nextToken = null
  }
  // X requires an end time at least ten seconds behind the present. Keep it
  // stable over every page (including a resumed page).
  if (source === "x" && !nextToken)
    window = {
      ...window,
      endTime: new Date(
        Math.min(Date.parse(window.endTime), now - 10_000),
      ).toISOString(),
    }
  if (Date.parse(window.startTime) >= Date.parse(window.endTime))
    throw new Error("fetch window must begin in the past")
  const maxPages =
    pageLimit ?? bounded(process.env.SOURCE_HISTORY_MAX_PAGES, 4, 20)
  let requested = 0
  let count = 0
  let discovered = 0
  let scheduleAssets = 0
  const previousDiscovered = nextToken ? checkpoint.metadata.discovered || 0 : 0
  const errors = []
  do {
    const url = new URL(
      source === "x"
        ? "https://api.x.com/2/tweets/search/all"
        : "https://www.googleapis.com/youtube/v3/search",
    )
    if (source === "x") {
      Object.entries({
        query: `from:${account}`,
        start_time: window.startTime,
        end_time: window.endTime,
        max_results: "100",
        sort_order: "recency",
        "tweet.fields": "created_at,public_metrics,attachments,author_id",
        expansions: "attachments.media_keys,author_id",
        "media.fields": "url,preview_image_url,alt_text",
        "user.fields": "username,name,profile_image_url",
      }).forEach(([key, value]) => url.searchParams.set(key, value))
      if (nextToken) url.searchParams.set("next_token", nextToken)
    } else {
      Object.entries({
        part: "snippet",
        channelId: account,
        type: "video",
        order: "date",
        publishedAfter: new Date(
          Date.parse(window.startTime) - 1000,
        ).toISOString(),
        publishedBefore: window.endTime,
        maxResults: "50",
      }).forEach(([key, value]) => url.searchParams.set(key, value))
      if (nextToken) url.searchParams.set("pageToken", nextToken)
    }
    requested += 1
    let payload
    try {
      payload = await apiJson(
        url,
        source === "x"
          ? { authorization: `Bearer ${token}` }
          : { "X-Goog-Api-Key": token },
      )
      if (
        !payload ||
        typeof payload !== "object" ||
        payload.errors?.length ||
        payload.error ||
        (source === "x"
          ? (payload.data != null && !Array.isArray(payload.data)) ||
            !payload.meta
          : !Array.isArray(payload.items))
      )
        throw new Error("source_api_invalid_response")
    } catch (error) {
      if (requested === 1) {
        ;(error as Error & { requested: number }).requested = requested
        throw error
      }
      errors.push(error.message)
      break
    }
    const entries = source === "x" ? payload.data || [] : payload.items
    discovered += entries.length
    const mappedRecords = entries.map((entry) => {
      if (source === "x") {
        const author = payload.includes?.users?.find(
          (user) => user.id === entry.author_id,
        )
        if (
          author?.username &&
          author.username.toLowerCase() !== account.toLowerCase()
        )
          return null
        const media = (payload.includes?.media || []).filter((item) =>
          entry.attachments?.media_keys?.includes(item.media_key),
        )
        return mapTweet(
          {
            ...entry,
            author: {
              screenName: account,
              name: author?.name,
              avatarURL: author?.profile_image_url,
            },
            mediaURLs: media
              .map((item) => item.url || item.preview_image_url)
              .filter(Boolean),
            likes: entry.public_metrics?.like_count,
            retweets: entry.public_metrics?.retweet_count,
            replies: entry.public_metrics?.reply_count,
          },
          entry.id,
          account,
        )
      }
      const snippet = entry.snippet
      const id = entry.id?.videoId
      if (!id || !snippet?.title || snippet.channelId !== account) return null
      const thumbnail =
        snippet.thumbnails?.high?.url ||
        snippet.thumbnails?.default?.url ||
        null
      const existing = getVideoRecord(database, id)
      return {
        id,
        source,
        title: snippet.title,
        published_at: snippet.publishedAt,
        url: `https://www.youtube.com/watch?v=${id}`,
        thumbnail_url: thumbnail,
        thumbnail_urls: thumbnail ? [thumbnail] : [],
        kind:
          existing?.kind ||
          (snippet.liveBroadcastContent === "upcoming"
            ? "UPCOMING LIVE"
            : classifyVideo(snippet.title)),
        is_upcoming:
          existing?.isUpcoming ?? snippet.liveBroadcastContent === "upcoming",
        raw: entry,
      }
    })
    if (
      mappedRecords.some(
        (record) =>
          !record || !Number.isFinite(Date.parse(record.published_at)),
      )
    ) {
      if (requested === 1) {
        const error = new Error("source_api_invalid_records")
        ;(error as Error & { requested: number }).requested = requested
        throw error
      }
      errors.push("source_api_invalid_records")
      break
    }
    const records = mappedRecords.filter((record) =>
      isInFetchWindow(record.published_at, window),
    )
    const next =
      source === "x" ? payload.meta?.next_token : payload.nextPageToken
    if (next && next === nextToken) {
      errors.push("source_api_repeated_page")
      break
    }
    // Persist the page and continuation together, so retries cannot skip it.
    database.transaction(() => {
      if (source === "x") {
        upsertPosts(database, records)
        scheduleAssets += upsertScheduleAssetsFromPosts(
          database,
          records,
          account,
        ).length
      } else upsertVideos(database, records)
      upsertSyncState(database, {
        source: `${source}-window`,
        accountId: key,
        cursorId: next || null,
        metadata: { window, discovered: previousDiscovered + discovered },
      })
      if (input.mode === "before")
        upsertSyncState(database, {
          source: `${source}-backfill`,
          accountId: account,
          cursorTime: next
            ? window.endTime
            : source === "youtube" && previousDiscovered + discovered >= 500
              ? oldestSourceTimestamp(database, source, account) ||
                window.endTime
              : window.startTime,
          metadata: { pendingWindow: next ? window : null },
        })
    })()
    count += records.length
    nextToken = next || null
    if (nextToken && requested < maxPages) {
      await new Promise((resolve) =>
        setTimeout(
          resolve,
          Math.max(
            0,
            Math.min(10_000, Number(process.env.SYNC_REQUEST_DELAY_MS) || 0),
          ),
        ),
      )
    }
  } while (nextToken && requested < maxPages)
  if (nextToken && !errors.length)
    errors.push("history_page_budget_reached; repeat this fetch to continue")
  if (source === "youtube" && previousDiscovered + discovered >= 500)
    errors.push("youtube_search_limit; use a narrower date range")
  return {
    count,
    discovered,
    requested,
    ...(source === "x" ? { scheduleAssets } : {}),
    window,
    hasMore: Boolean(nextToken),
    errors,
  }
}
