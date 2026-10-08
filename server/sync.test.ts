import assert from "node:assert/strict"
import crypto from "node:crypto"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"
import test from "node:test"

import {
  getSyncState,
  getVideoRecord,
  initializeDatabase,
  setLlmRouteProviders,
  setAppSetting,
  upsertLlmProvider,
  upsertAssets,
  upsertPosts,
} from "./database.ts"
import { encryptSecret } from "./secret-store.ts"

process.env.SYNC_REQUEST_DELAY_MS = "0"
const { mapTweet, runSync, snowflakeDate, syncX, syncYoutube } =
  await import("../scripts/sync.ts")

const twitterEpochMs = 1_288_834_974_657n

function snowflakeFor(date) {
  return ((BigInt(date.getTime()) - twitterEpochMs) << 22n).toString()
}

function response(body, options: Record<string, any> = {}) {
  return new Response(body, { status: 200, ...options })
}

for (const failure of ["budget", "503", "all-failed"]) {
  test(`X resumes durable pending IDs after ${failure}, even outside the profile and bootstrap window`, async (t) => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "kano-x-pending-"))
    const filename = path.join(directory, "snapshot.sqlite")
    let database = initializeDatabase({ seed: false, filename })
    const originalFetch = globalThis.fetch
    const originalEnvironment = Object.fromEntries(
      [
        "X_HANDLES",
        "X_MAX_STATUS_REQUESTS",
        "X_REFRESH_KNOWN",
        "X_BOOTSTRAP_DAYS",
        "X_SCHEDULE_REFRESH_LIMIT",
        "X_API_BEARER_TOKEN",
      ].map((key) => [key, process.env[key]]),
    )
    const fixedNow = Date.parse("2026-09-16T00:00:00Z")
    t.mock.method(Date, "now", () => fixedNow)
    const ids = [
      snowflakeFor(new Date(fixedNow - 3600_000)),
      snowflakeFor(new Date(fixedNow - 7200_000)),
    ]
    const oldId = snowflakeFor(new Date(fixedNow - 8 * 86400_000))
    let firstRun = true
    let profileIds = [...ids, oldId]
    const requested = []
    try {
      process.env.X_HANDLES = "kano_2525"
      process.env.X_MAX_STATUS_REQUESTS = failure === "budget" ? "1" : "2"
      process.env.X_REFRESH_KNOWN = "0"
      process.env.X_BOOTSTRAP_DAYS = "7"
      process.env.X_SCHEDULE_REFRESH_LIMIT = "0"
      delete process.env.X_API_BEARER_TOKEN
      globalThis.fetch = async (url) => {
        const value = String(url)
        if (value === "https://x.com/kano_2525")
          return response(
            profileIds
              .map((id) => `<a href="/kano_2525/status/${id}">post</a>`)
              .join(""),
          )
        const id = value.match(/\/status\/(\d+)$/u)?.[1]
        assert.ok(id)
        requested.push(id)
        if (
          firstRun &&
          (failure === "all-failed" || (failure === "503" && id === ids[1]))
        )
          return response("temporary", { status: 503 })
        return response(
          JSON.stringify({
            tweetID: id,
            text: "synthetic",
            date: snowflakeDate(id).toISOString(),
            author: { screenName: "kano_2525" },
          }),
        )
      }
      if (failure === "all-failed") await assert.rejects(syncX(database))
      else await syncX(database)
      assert.equal(
        database
          .prepare<unknown[], { count: number }>(
            "SELECT COUNT(*) AS count FROM posts",
          )
          .get().count,
        failure === "all-failed" ? 0 : 1,
      )
      assert.deepEqual(
        getSyncState(database, "x", "kano_2525").metadata.pendingStatusIds,
        failure === "all-failed" ? ids : [ids[1]],
      )
      const incrementalState = getSyncState(database, "x", "kano_2525")
      // A public recent window must not consume or rewrite normal incremental work.
      profileIds = [oldId]
      firstRun = false
      await syncX(database, { mode: "recent", days: 1 })
      assert.deepEqual(
        getSyncState(database, "x", "kano_2525"),
        incrementalState,
      )
      database.close()
      database = initializeDatabase({ seed: false, filename })
      requested.length = 0
      t.mock.method(Date, "now", () => fixedNow + 10 * 86400_000)
      profileIds = []
      await syncX(database)
      if (failure === "budget") assert.deepEqual(requested, [ids[1]])
      else
        assert.deepEqual(requested, failure === "all-failed" ? ids : [ids[1]])
      assert.equal(
        database
          .prepare<unknown[], { count: number }>(
            "SELECT COUNT(*) AS count FROM posts",
          )
          .get().count,
        2,
      )
      assert.deepEqual(
        getSyncState(database, "x", "kano_2525").metadata.pendingStatusIds,
        [],
      )
      assert.equal(getSyncState(database, "x", "kano_2525").cursorId, ids[0])
      assert.equal(getSyncState(database, "x", "_Kanotic"), null)
    } finally {
      globalThis.fetch = originalFetch
      for (const [key, value] of Object.entries(originalEnvironment)) {
        if (value == null) delete process.env[key]
        else process.env[key] = value
      }
      database.close()
      fs.rmSync(directory, { recursive: true, force: true })
    }
  })
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
      database
        .prepare<unknown[], Record<string, any>>(
          "SELECT COUNT(*) AS count FROM posts",
        )
        .get().count,
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
      database
        .prepare<unknown[], Record<string, any>>(
          "SELECT COUNT(*) AS count FROM posts",
        )
        .get().count,
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

test("X bootstrap refreshes a recent schedule source outside profile discovery", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const originalFetch = globalThis.fetch
  const originalEnvironment = {
    X_BOOTSTRAP_DAYS: process.env.X_BOOTSTRAP_DAYS,
    X_HANDLES: process.env.X_HANDLES,
    X_MAX_STATUS_REQUESTS: process.env.X_MAX_STATUS_REQUESTS,
    X_REFRESH_KNOWN: process.env.X_REFRESH_KNOWN,
    X_SCHEDULE_REFRESH_LIMIT: process.env.X_SCHEDULE_REFRESH_LIMIT,
  }
  const now = Date.now()
  const scheduleId = snowflakeFor(new Date(now - 2 * 24 * 60 * 60 * 1000))
  const visibleId = snowflakeFor(new Date(now - 60 * 60 * 1000))
  const scheduleSourceUrl = `https://x.com/kano_2525/status/${scheduleId}`
  const imageUrl = "https://pbs.twimg.com/media/schedule.png"
  upsertAssets(database, [
    {
      id: "weekly-schedule",
      kind: "schedule",
      url: "",
      source_url: scheduleSourceUrl,
      week_start: "2026-08-24",
      source_account: "kano_2525",
    },
  ])
  const requestedIds = []
  try {
    process.env.X_BOOTSTRAP_DAYS = "7"
    process.env.X_HANDLES = "kano_2525"
    process.env.X_MAX_STATUS_REQUESTS = "2"
    process.env.X_REFRESH_KNOWN = "0"
    process.env.X_SCHEDULE_REFRESH_LIMIT = "1"
    globalThis.fetch = async (url) => {
      const value = String(url)
      if (value === "https://x.com/kano_2525") {
        return response(`<a href="/kano_2525/status/${visibleId}">post</a>`)
      }
      const id = value.match(/\/status\/(\d+)$/u)?.[1]
      assert.ok(id)
      requestedIds.push(id)
      const isSchedule = id === scheduleId
      return response(
        JSON.stringify({
          tweetID: id,
          text: isSchedule ? "今週のスケジュール" : "普通の投稿",
          date: snowflakeDate(id).toISOString(),
          author: { screenName: "kano_2525" },
          mediaURLs: isSchedule ? [imageUrl] : [],
        }),
        { headers: { "content-type": "application/json" } },
      )
    }

    const result = await syncX(database)
    assert.equal(result.scheduleAssets, 1)
    assert.equal(result.supplemented, 1)
    assert.equal(requestedIds[0], scheduleId)
    assert.equal(
      database
        .prepare<unknown[], Record<string, any>>(
          "SELECT url FROM assets WHERE id='weekly-schedule'",
        )
        .get().url,
      imageUrl,
    )
    assert.equal(
      database
        .prepare<unknown[], Record<string, any>>(
          "SELECT media_url AS mediaUrl FROM posts WHERE id=?",
        )
        .get(scheduleId).mediaUrl,
      imageUrl,
    )

    requestedIds.length = 0
    const incremental = await syncX(database)
    assert.equal(incremental.supplemented, 0)
    assert.equal(incremental.scheduleAssets, 0)
    assert.deepEqual(requestedIds, [])
  } finally {
    globalThis.fetch = originalFetch
    for (const [key, value] of Object.entries(originalEnvironment)) {
      if (value == null) delete process.env[key]
      else process.env[key] = value
    }
    database.close()
  }
})

for (const [name, text] of [
  ["a generic future plan", "今日は22時からゲームをする予定だよ！"],
  // inferPostType labels this `SCHEDULE / 日程`; the label must not count.
  ["a stream announcement", "今日は22時から配信予定です！"],
])
  test(`X does not promote ${name} image to a schedule asset`, async () => {
    const database = initializeDatabase({ seed: false, filename: ":memory:" })
    const originalFetch = globalThis.fetch
    const originalEnvironment = {
      X_BOOTSTRAP_DAYS: process.env.X_BOOTSTRAP_DAYS,
      X_HANDLES: process.env.X_HANDLES,
      X_MAX_STATUS_REQUESTS: process.env.X_MAX_STATUS_REQUESTS,
      X_REFRESH_KNOWN: process.env.X_REFRESH_KNOWN,
    }
    const postId = snowflakeFor(new Date(Date.now() - 60 * 60 * 1000))
    try {
      process.env.X_BOOTSTRAP_DAYS = "7"
      process.env.X_HANDLES = "kano_2525"
      process.env.X_MAX_STATUS_REQUESTS = "1"
      process.env.X_REFRESH_KNOWN = "0"
      globalThis.fetch = async (url) => {
        const value = String(url)
        if (value === "https://x.com/kano_2525") {
          return response(`<a href="/kano_2525/status/${postId}">post</a>`)
        }
        assert.equal(
          value,
          `https://api.vxtwitter.com/kano_2525/status/${postId}`,
        )
        return response(
          JSON.stringify({
            tweetID: postId,
            text,
            date: snowflakeDate(postId).toISOString(),
            author: { screenName: "kano_2525" },
            mediaURLs: ["https://pbs.twimg.com/media/stream-preview.png"],
          }),
          { headers: { "content-type": "application/json" } },
        )
      }

      const result = await syncX(database)
      assert.equal(result.scheduleAssets, 0)
      assert.equal(
        database
          .prepare<unknown[], Record<string, any>>(
            "SELECT COUNT(*) AS count FROM assets WHERE kind='schedule'",
          )
          .get().count,
        0,
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

test("X includes quoted tweet text and media when mapping a post", () => {
  const post = mapTweet(
    {
      tweetID: "2099999999999999999",
      text: "引用しました",
      date_epoch: Math.floor(Date.now() / 1000),
      author: { screenName: "_Kanotic" },
      qrt: {
        text: "今週のスケジュール",
        mediaURLs: ["https://pbs.twimg.com/media/quoted-schedule.png"],
      },
    },
    "2099999999999999999",
    "_Kanotic",
  )
  assert.equal(
    post.media_url,
    "https://pbs.twimg.com/media/quoted-schedule.png",
  )
  assert.match(post.search_text, /今週のスケジュール/u)
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
  let recovered = false
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
      if (handle === "kano_2525" && !recovered)
        return response("failed", { status: 503 })
      return response(
        JSON.stringify({
          tweetID: id,
          text: "WEEKLY schedule",
          date: snowflakeDate(id).toISOString(),
          author: { screenName: handle },
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
        .prepare<unknown[], Record<string, any>>(
          "SELECT account_handle AS accountHandle FROM posts",
        )
        .all(),
      [{ accountHandle: "_Kanotic" }],
    )
    assert.equal(getSyncState(database, "x", "kano_2525").cursorId, null)
    assert.equal(getSyncState(database, "x", "kano_2525").lastSuccessAt, null)
    assert.deepEqual(
      getSyncState(database, "x", "kano_2525").metadata.pendingStatusIds,
      primaryIds,
    )
    assert.equal(
      getSyncState(database, "x", "_Kanotic").metadata.bootstrap,
      true,
    )
    assert.equal(
      database
        .prepare<unknown[], Record<string, any>>(
          "SELECT url FROM assets WHERE id='weekly-schedule'",
        )
        .get().url,
      "https://pbs.twimg.com/media/weekly.png",
    )
    recovered = true
    detailRequests = 0
    const resumed = await syncX(database)
    assert.equal(resumed.requested, 3)
    assert.equal(detailRequests, 3)
    for (const [handle, ids] of Object.entries(profiles)) {
      assert.deepEqual(
        getSyncState(database, "x", handle).metadata.pendingStatusIds,
        [],
      )
      assert.equal(getSyncState(database, "x", handle).cursorId, ids[0])
      assert.deepEqual(
        database
          .prepare<unknown[], Record<string, any>>(
            "SELECT id FROM posts WHERE account_handle=? ORDER BY id DESC",
          )
          .all(handle)
          .map((post) => post.id),
        ids,
      )
    }
  } finally {
    globalThis.fetch = originalFetch
    for (const [key, value] of Object.entries(originalEnvironment)) {
      if (value == null) delete process.env[key]
      else process.env[key] = value
    }
    database.close()
  }
})

test("X does not advance an account cursor when its detail budget is zero", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const originalFetch = globalThis.fetch
  const originalEnvironment = {
    X_HANDLES: process.env.X_HANDLES,
    X_MAX_STATUS_REQUESTS: process.env.X_MAX_STATUS_REQUESTS,
    X_BOOTSTRAP_DAYS: process.env.X_BOOTSTRAP_DAYS,
    X_REFRESH_KNOWN: process.env.X_REFRESH_KNOWN,
  }
  const now = Date.now()
  const ids = {
    kano_2525: snowflakeFor(new Date(now - 60 * 60 * 1000)),
    _Kanotic: snowflakeFor(new Date(now - 30 * 60 * 1000)),
  }
  let detailRequests = 0
  try {
    process.env.X_HANDLES = "kano_2525,_Kanotic"
    process.env.X_MAX_STATUS_REQUESTS = "1"
    process.env.X_BOOTSTRAP_DAYS = "7"
    process.env.X_REFRESH_KNOWN = "0"
    globalThis.fetch = async (url) => {
      const value = String(url)
      const profileMatch = value.match(/^https:\/\/x\.com\/(.+)$/u)
      if (profileMatch) {
        const handle = profileMatch[1]
        return response(`<a href="/${handle}/status/${ids[handle]}">post</a>`)
      }
      detailRequests += 1
      const match = value.match(/\/status\/(\d+)$/u)
      assert.ok(match)
      return response(
        JSON.stringify({
          tweetID: match[1],
          text: `post ${match[1]}`,
          date: snowflakeDate(match[1]).toISOString(),
          author: { screenName: "kano_2525" },
        }),
        { headers: { "content-type": "application/json" } },
      )
    }

    const result = await syncX(database)
    assert.equal(detailRequests, 1)
    assert.equal(result.requested, 1)
    assert.equal(
      getSyncState(database, "x", "kano_2525")?.cursorId,
      ids.kano_2525,
    )
    assert.equal(getSyncState(database, "x", "_Kanotic"), null)
    assert.match(result.errors[0], /_Kanotic/u)
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
        .prepare<unknown[], Record<string, any>>(
          "SELECT COUNT(*) AS count FROM videos WHERE published_at IS NOT NULL",
        )
        .get().count,
      6,
    )
    assert.equal(getVideoRecord(database, videos[6].id), null)
    assert.equal(getVideoRecord(database, streamId).isUpcoming, 1)
    assert.equal(
      database
        .prepare<unknown[], Record<string, any>>(
          "SELECT COUNT(*) AS count FROM events WHERE id = ?",
        )
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
    "LLM_SECRETS_KEY",
  ]
  const originalEnvironment = Object.fromEntries(
    names.map((name) => [name, process.env[name]]),
  )
  try {
    process.env.SKIP_X = "1"
    process.env.SKIP_YOUTUBE = "1"
    process.env.SKIP_MEDIA = "1"
    delete process.env.SKIP_LLM
    process.env.LLM_SECRETS_KEY = crypto.randomBytes(32).toString("base64")
    upsertLlmProvider(database, {
      id: "sync-test-provider",
      name: "Sync test provider",
      baseUrl: "https://sync-test.example.invalid/v1",
      model: "sync-test-model",
      capabilities: ["text", "image"],
      replaceApiKey: true,
      apiKeyCiphertext: encryptSecret(
        "not-a-real-api-key",
        process.env.LLM_SECRETS_KEY,
      ),
    })
    setLlmRouteProviders(database, "schedule_board", ["sync-test-provider"])
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
