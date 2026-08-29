import assert from "node:assert/strict"
import test from "node:test"

import {
  getSyncState,
  getVideoRecord,
  initializeDatabase,
  setAppSetting,
  upsertPosts,
} from "./database.js"

process.env.SYNC_REQUEST_DELAY_MS = "0"
const { runSync, snowflakeDate, syncX, syncYoutube } =
  await import("../scripts/sync.mjs")

const twitterEpochMs = 1_288_834_974_657n

function snowflakeFor(date) {
  return ((BigInt(date.getTime()) - twitterEpochMs) << 22n).toString()
}

function response(body, options = {}) {
  return new Response(body, { status: 200, ...options })
}

test("X bootstrap stays within seven days and later requests only new posts", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const originalFetch = globalThis.fetch
  const originalEnvironment = {
    X_BOOTSTRAP_DAYS: process.env.X_BOOTSTRAP_DAYS,
    X_HANDLES: process.env.X_HANDLES,
    X_MAX_STATUS_REQUESTS: process.env.X_MAX_STATUS_REQUESTS,
    X_REFRESH_KNOWN: process.env.X_REFRESH_KNOWN,
  }
  const now = Date.now()
  const firstIds = [
    snowflakeFor(new Date(now - 60 * 60 * 1000)),
    snowflakeFor(new Date(now - 2 * 60 * 60 * 1000)),
    snowflakeFor(new Date(now - 8 * 24 * 60 * 60 * 1000)),
  ]
  const newId = snowflakeFor(new Date(now - 5 * 60 * 1000))
  const oldUnknownId = snowflakeFor(new Date(now - 9 * 24 * 60 * 60 * 1000))
  let profileIds = firstIds
  const requestedIds = []
  try {
    process.env.X_BOOTSTRAP_DAYS = "7"
    process.env.X_HANDLES = "kano_2525"
    process.env.X_MAX_STATUS_REQUESTS = "20"
    process.env.X_REFRESH_KNOWN = "0"
    globalThis.fetch = async (url) => {
      const value = String(url)
      if (value === "https://x.com/kano_2525") {
        return response(
          profileIds
            .map((id) => `<a href="/kano_2525/status/${id}">post</a>`)
            .join(""),
        )
      }
      const id = value.match(/\/status\/(\d+)$/u)?.[1]
      assert.ok(id)
      requestedIds.push(id)
      return response(
        JSON.stringify({
          tweetID: id,
          text: `post ${id}`,
          date: snowflakeDate(id).toISOString(),
          author: { screenName: "kano_2525" },
        }),
        { headers: { "content-type": "application/json" } },
      )
    }

    const bootstrap = await syncX(database)
    assert.equal(bootstrap.count, 2)
    assert.equal(bootstrap.requested, 2)
    assert.deepEqual(requestedIds.sort(), firstIds.slice(0, 2).sort())
    assert.equal(
      database.prepare("SELECT COUNT(*) AS count FROM posts").get().count,
      2,
    )
    assert.equal(
      getSyncState(database, "x", "kano_2525").metadata.bootstrap,
      true,
    )

    requestedIds.length = 0
    profileIds = [newId, ...firstIds.slice(0, 2), oldUnknownId]
    const incremental = await syncX(database)
    assert.equal(incremental.count, 1)
    assert.equal(incremental.requested, 1)
    assert.deepEqual(requestedIds, [newId])
    assert.equal(
      database.prepare("SELECT COUNT(*) AS count FROM posts").get().count,
      3,
    )
    assert.equal(
      getSyncState(database, "x", "kano_2525").metadata.bootstrap,
      false,
    )
  } finally {
    globalThis.fetch = originalFetch
    for (const [key, value] of Object.entries(originalEnvironment)) {
      if (value == null) delete process.env[key]
      else process.env[key] = value
    }
    database.close()
  }
})

test("X sync aggregates accounts and keeps failed detail requests within the shared budget", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const originalFetch = globalThis.fetch
  const originalEnvironment = {
    X_HANDLES: process.env.X_HANDLES,
    X_MAX_STATUS_REQUESTS: process.env.X_MAX_STATUS_REQUESTS,
    X_REFRESH_KNOWN: process.env.X_REFRESH_KNOWN,
  }
  const now = Date.now()
  const primaryIds = [
    snowflakeFor(new Date(now - 60 * 60 * 1000)),
    snowflakeFor(new Date(now - 2 * 60 * 60 * 1000)),
  ]
  const secondaryIds = [
    snowflakeFor(new Date(now - 30 * 60 * 1000)),
    snowflakeFor(new Date(now - 90 * 60 * 1000)),
  ]
  const profiles = {
    kano_2525: primaryIds,
    _Kanotic: secondaryIds,
  }
  let detailRequests = 0
  try {
    process.env.X_HANDLES = "kano_2525,_Kanotic"
    process.env.X_MAX_STATUS_REQUESTS = "3"
    process.env.X_REFRESH_KNOWN = "0"
    setAppSetting(database, "schedule_keywords", JSON.stringify(["WEEKLY"]))
    globalThis.fetch = async (url) => {
      const value = String(url)
      const profileMatch = value.match(/^https:\/\/x\.com\/(.+)$/u)
      if (profileMatch) {
        const handle = profileMatch[1]
        return response(
          profiles[handle]
            .map((id) => `<a href="/${handle}/status/${id}">post</a>`)
            .join(""),
        )
      }
      const statusMatch = value.match(
        /^https:\/\/api\.vxtwitter\.com\/([^/]+)\/status\/(\d+)$/u,
      )
      assert.ok(statusMatch)
      detailRequests += 1
      const [, handle, id] = statusMatch
      if (handle === "kano_2525") return response("failed", { status: 503 })
      return response(
        JSON.stringify({
          tweetID: id,
          text: "WEEKLY schedule",
          date: snowflakeDate(id).toISOString(),
          author: { screenName: "_Kanotic" },
          mediaURLs: ["https://pbs.twimg.com/media/weekly.png"],
        }),
        { headers: { "content-type": "application/json" } },
      )
    }

    const result = await syncX(database)
    assert.equal(detailRequests, 3)
    assert.equal(result.requested, 3)
    assert.equal(result.count, 1)
    assert.equal(result.errors.length, 1)
    assert.deepEqual(
      database
        .prepare("SELECT account_handle AS accountHandle FROM posts")
        .all(),
      [{ accountHandle: "_Kanotic" }],
    )
    assert.equal(getSyncState(database, "x", "kano_2525"), null)
    assert.equal(
      getSyncState(database, "x", "_Kanotic").metadata.bootstrap,
      true,
    )
    assert.equal(
      database
        .prepare("SELECT url FROM assets WHERE id='weekly-schedule'")
        .get().url,
      "https://pbs.twimg.com/media/weekly.png",
    )
  } finally {
    globalThis.fetch = originalFetch
    for (const [key, value] of Object.entries(originalEnvironment)) {
      if (value == null) delete process.env[key]
      else process.env[key] = value
    }
    database.close()
  }
})

function feedXml(videos) {
  const entries = videos
    .map(
      (video) => `
        <entry>
          <yt:videoId>${video.id}</yt:videoId>
          <title>${video.title}</title>
          <published>${video.publishedAt}</published>
          <link rel="alternate" href="https://www.youtube.com/watch?v=${video.id}" />
          <media:group>
            <media:thumbnail url="https://i.ytimg.com/vi/${video.id}/hqdefault.jpg" />
          </media:group>
        </entry>`,
    )
    .join("")
  return `<?xml version="1.0" encoding="UTF-8"?>
    <feed xmlns:yt="https://example.invalid/youtube-schema"
      xmlns:media="https://example.invalid/media-schema">${entries}</feed>`
}

test("YouTube bootstraps six videos, then adds only newer RSS entries and rechecks reservations", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const originalFetch = globalThis.fetch
  const originalBootstrap = process.env.YOUTUBE_BOOTSTRAP_VIDEOS
  const videos = Array.from({ length: 8 }, (_, index) => ({
    id: `v${String(index + 1).padStart(10, "0")}`,
    title: `Video ${index + 1}`,
    publishedAt: new Date(Date.UTC(2026, 7, 28 - index)).toISOString(),
  }))
  const streamId = "stream00001"
  let currentFeed = videos
  let watchRequests = 0
  try {
    process.env.YOUTUBE_BOOTSTRAP_VIDEOS = "6"
    globalThis.fetch = async (url) => {
      const value = String(url)
      if (value.includes("/feeds/videos.xml"))
        return response(feedXml(currentFeed))
      if (value.endsWith("/streams")) {
        return response(`<a href="/watch?v=${streamId}">upcoming</a>`)
      }
      if (value.endsWith(`watch?v=${streamId}`)) {
        watchRequests += 1
        return response(`
          <meta property="og:title" content="Upcoming live" />
          <meta property="og:image" content="https://i.ytimg.com/vi/${streamId}/hqdefault.jpg" />
          <script>{"scheduledStartTime":"2027-09-01T11:00:00Z","isUpcoming":true}</script>
        `)
      }
      throw new Error(`unexpected URL ${value}`)
    }

    const bootstrap = await syncYoutube(database)
    assert.equal(bootstrap.count, 6)
    assert.equal(bootstrap.discovered, 8)
    assert.equal(bootstrap.inspected, 1)
    assert.equal(
      database
        .prepare(
          "SELECT COUNT(*) AS count FROM videos WHERE published_at IS NOT NULL",
        )
        .get().count,
      6,
    )
    assert.equal(getVideoRecord(database, videos[6].id), null)
    assert.equal(getVideoRecord(database, streamId).isUpcoming, 1)
    assert.equal(
      database
        .prepare("SELECT COUNT(*) AS count FROM events WHERE id = ?")
        .get(`youtube-${streamId}`).count,
      1,
    )

    const newest = {
      id: "v9999999999",
      title: "New video",
      publishedAt: "2026-08-29T00:00:00.000Z",
    }
    currentFeed = [newest, ...videos]
    const incremental = await syncYoutube(database)
    assert.equal(incremental.count, 1)
    assert.equal(incremental.discovered, 9)
    assert.equal(incremental.inspected, 1)
    assert.ok(getVideoRecord(database, newest.id))
    assert.equal(getVideoRecord(database, videos[6].id), null)
    assert.equal(getVideoRecord(database, videos[7].id), null)
    assert.equal(watchRequests, 2)
  } finally {
    globalThis.fetch = originalFetch
    if (originalBootstrap == null) delete process.env.YOUTUBE_BOOTSTRAP_VIDEOS
    else process.env.YOUTUBE_BOOTSTRAP_VIDEOS = originalBootstrap
    database.close()
  }
})

test("a failed OpenAI stage marks an otherwise skipped sync as partial", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const originalFetch = globalThis.fetch
  const names = [
    "SKIP_X",
    "SKIP_YOUTUBE",
    "SKIP_MEDIA",
    "SKIP_LLM",
    "OPENAI_API_KEY",
  ]
  const originalEnvironment = Object.fromEntries(
    names.map((name) => [name, process.env[name]]),
  )
  try {
    process.env.SKIP_X = "1"
    process.env.SKIP_YOUTUBE = "1"
    process.env.SKIP_MEDIA = "1"
    delete process.env.SKIP_LLM
    process.env.OPENAI_API_KEY = "not-a-real-api-key"
    upsertPosts(database, [
      {
        id: "schedule-sync-failure",
        source: "x",
        type: "notice",
        label: "SCHEDULE / 日程",
        text: "今週の予定",
        published_at: "2026-08-29T00:00:00.000Z",
        url: "https://x.com/example/status/1234567890123456792",
      },
    ])
    globalThis.fetch = async () =>
      new Response("synthetic failure", { status: 500 })

    const result = await runSync({ database, closeDatabase: false })
    assert.equal(result.status, "partial")
    assert.equal(result.results.schedules.failed, 1)
  } finally {
    globalThis.fetch = originalFetch
    for (const [key, value] of Object.entries(originalEnvironment)) {
      if (value == null) delete process.env[key]
      else process.env[key] = value
    }
    database.close()
  }
})
