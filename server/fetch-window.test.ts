import { listeningPort } from "./http-address.ts"
import type { Database as DatabaseConnection } from "better-sqlite3"

import assert from "node:assert/strict"
import test from "node:test"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"
import { createApp } from "./app.ts"
import {
  getSyncState,
  getVideoRecord,
  getDashboard,
  getScheduleAssetReview,
  initializeDatabase,
  listScheduleAssets,
  listWorkflows,
  upsertPosts,
  upsertSyncState,
  upsertScheduleAssetReview,
  upsertMediaAsset,
  upsertAssets,
  upsertVideos,
  upsertWorkflow,
} from "./database.ts"
import {
  isInFetchWindow,
  normalizeFetchWindow,
  normalizeSourceLookback,
  oldestSourceTimestamp,
  resolveFetchWindow,
} from "./fetch-window.ts"
import { createSyncJobManager } from "./sync-jobs.ts"
import { createWorkflowScheduler } from "./workflow-scheduler.ts"
import { resolveMediaCachePath, writeMediaFileAtomic } from "./media-cache.ts"

process.env.SYNC_REQUEST_DELAY_MS = "0"
const { mapTweet, syncX, syncYoutube, runSync } =
  await import("../scripts/sync.ts")
const { fetchSourceWindow } = await import("../scripts/source-history.ts")
const account = "archive_test"
const channel = "synthetic-channel"
const credential = "synthetic-test-credential"
const localFetch = globalThis.fetch
const now = Date.parse("2026-10-08T03:00:00Z")
const window = { mode: "range", startDate: "2026-09-01", endDate: "2026-09-30" }
const post = (id, publishedAt, handle = account) => ({
  id,
  source: "x",
  account_handle: handle,
  text: "Archived post",
  published_at: publishedAt,
  url: `https://x.com/${handle}/status/${id}`,
})
const xEntry = (id, date) => ({ id, text: "Archived post", created_at: date })
const videoEntry = (id, date) => ({
  id: { videoId: id },
  snippet: { title: "Archived video", channelId: channel, publishedAt: date },
})
const json = (value, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: { "content-type": "application/json" },
  })

async function withSourceEnvironment(run) {
  const originalFetch = globalThis.fetch
  const keys = [
    "X_API_BEARER_TOKEN",
    "YOUTUBE_API_KEY",
    "X_HANDLES",
    "YOUTUBE_CHANNEL_ID",
    "X_MAX_STATUS_REQUESTS",
    "SOURCE_HISTORY_MAX_PAGES",
  ]
  const original = Object.fromEntries(
    keys.map((key) => [key, process.env[key]]),
  )
  const database = initializeDatabase({ filename: ":memory:", seed: false })
  try {
    globalThis.fetch = async () => {
      throw new Error("unexpected external request in offline test")
    }
    process.env.X_API_BEARER_TOKEN = "synthetic-test-credential"
    process.env.YOUTUBE_API_KEY = "synthetic-test-credential"
    process.env.X_HANDLES = account
    process.env.YOUTUBE_CHANNEL_ID = channel
    await run(database)
  } finally {
    globalThis.fetch = originalFetch
    for (const key of keys) {
      if (original[key] == null) delete process.env[key]
      else process.env[key] = original[key]
    }
    database.close()
  }
}

test("date windows include both Japan dates and reject invalid or unbounded inputs", () => {
  const selected = resolveFetchWindow({
    mode: "range",
    startDate: "2026-10-01",
    endDate: "2026-10-01",
  })
  assert.equal(selected.startTime, "2026-09-30T15:00:00.000Z")
  assert.equal(selected.endTime, "2026-10-01T15:00:00.000Z")
  assert.equal(isInFetchWindow(selected.startTime, selected), true)
  assert.equal(isInFetchWindow(selected.endTime, selected), false)
  for (const input of [
    { mode: "recent", days: 0 },
    { mode: "recent", days: "7" },
    { mode: "before", days: 366 },
    { mode: "range", startDate: "2026-02-30", endDate: "2026-03-01" },
    { mode: "range", startDate: "2026-10-02", endDate: "2026-10-01" },
    { mode: "range", startDate: "2025-01-01", endDate: "2026-10-01" },
  ])
    assert.throws(() => normalizeFetchWindow(input))
  assert.throws(() => resolveFetchWindow({ mode: "before", days: 30 }))
  assert.deepEqual(normalizeSourceLookback({ x: 30 }), { x: 30, youtube: 14 })
  assert.throws(() => normalizeSourceLookback({ x: 1.5 }))
})

test("oldest boundaries use all stored records per account and chronological time", async () => {
  await withSourceEnvironment(async (database: DatabaseConnection) => {
    upsertPosts(database, [
      post("1", "2026-09-01T01:00:00+09:00"),
      post("2", "2026-09-01T00:00:00Z"),
      post("3", "2025-01-01T00:00:00Z", "other_test"),
    ])
    assert.equal(
      oldestSourceTimestamp(database, "x", account),
      "2026-09-01T01:00:00+09:00",
    )
    assert.equal(oldestSourceTimestamp(database, "x", "empty_test"), null)
    const selected = resolveFetchWindow(
      { mode: "before", days: 7 },
      { oldest: oldestSourceTimestamp(database, "x", account) },
    )
    assert.equal(selected.endTime, "2026-08-31T16:00:00.000Z")
  })
})

test("existing workflow schemas gain rolling defaults without resetting timers or settings", () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "kano-window-test-"))
  const filename = path.join(directory, "snapshot.sqlite")
  let database
  try {
    database = initializeDatabase({ filename })
    const workflow = upsertWorkflow(database, {
      id: "sources-only",
      name: "Preserved",
      scheduleEnabled: true,
      intervalMinutes: 120,
    })
    database.exec("ALTER TABLE workflows DROP COLUMN source_lookback_json")
    database.close()
    database = initializeDatabase({ filename })
    const upgraded = listWorkflows(database).find(
      (item) => item.id === workflow.id,
    )
    assert.equal(upgraded.name, "Preserved")
    assert.equal(upgraded.scheduleEnabled, true)
    assert.equal(upgraded.nextRunAt, workflow.nextRunAt)
    assert.deepEqual(upgraded.sourceLookbackDays, { x: 7, youtube: 14 })
  } finally {
    if (database?.open) database.close()
    fs.rmSync(directory, { recursive: true, force: true })
  }
})

test("public recent X fetches accept older unknown records within the window and report partial coverage", async () => {
  await withSourceEnvironment(async (database: DatabaseConnection) => {
    delete process.env.X_API_BEARER_TOKEN
    const liveNow = Date.now()
    const snowflake = (milliseconds) =>
      ((BigInt(milliseconds) - 1_288_834_974_657n) << 22n).toString()
    const older = snowflake(liveNow - 10 * 86_400_000)
    const outside = snowflake(liveNow - 40 * 86_400_000)
    const newest = snowflake(liveNow - 1000)
    upsertSyncState(database, {
      source: "x",
      accountId: account,
      cursorId: newest,
      cursorTime: new Date(liveNow - 1000).toISOString(),
    })
    let detailRequests = 0
    globalThis.fetch = async (value) => {
      const url = new URL(value)
      if (url.origin === "https://x.com")
        return new Response(
          `<a href="/${account}/status/${older}"></a><a href="/${account}/status/${outside}"></a>`,
        )
      assert.equal(url.pathname, `/${account}/status/${older}`)
      detailRequests += 1
      return json({
        tweetID: older,
        text: "Older unknown post",
        date: new Date(liveNow - 10 * 86_400_000).toISOString(),
      })
    }
    const progress = []
    const result = await runSync({
      database,
      closeDatabase: false,
      setExitCode: false,
      steps: ["x"],
      fetchWindows: { x: { mode: "recent", days: 30 } },
      onStep: (step, status) => progress.push([step, status]),
    })
    assert.equal(result.results.x.count, 1)
    assert.equal(result.status, "partial")
    assert.equal(detailRequests, 1)
    assert.deepEqual(progress, [
      ["x", "running"],
      ["x", "partial"],
    ])
    assert.equal(getSyncState(database, "x", account).cursorId, newest)
  })
})

test("recent RSS windows replace the six-entry bootstrap limit and retain incremental cursors", async () => {
  await withSourceEnvironment(async (database: DatabaseConnection) => {
    delete process.env.YOUTUBE_API_KEY
    const entries = [1, 5, 10, 15, 20, 25, 28, 40].map((days, index) => ({
      id: `samplevideo${index}`,
      date: new Date(Date.now() - days * 86_400_000).toISOString(),
    }))
    upsertSyncState(database, {
      source: "youtube",
      accountId: channel,
      cursorId: "newest",
    })
    globalThis.fetch = async (value) =>
      new URL(value).pathname === "/feeds/videos.xml"
        ? new Response(
            `<feed xmlns:yt="https://example.invalid/youtube-schema">${entries.map((entry) => `<entry><yt:videoId>${entry.id}</yt:videoId><title>Video</title><published>${entry.date}</published><link href="https://www.youtube.com/watch?v=${entry.id}" /></entry>`).join("")}</feed>`,
          )
        : new Response("")
    const result = await syncYoutube(database, { mode: "recent", days: 30 })
    assert.equal(result.count, 7)
    assert.equal(result.coverage, "rss_only")
    assert.equal(result.errors.length, 1)
    assert.equal(getSyncState(database, "youtube", channel).cursorId, "newest")
  })
})

test("X range pagination resumes without changing incremental cursors or losing a page", async () => {
  await withSourceEnvironment(async (database: DatabaseConnection) => {
    upsertSyncState(database, {
      source: "x",
      accountId: account,
      cursorId: "newest",
      cursorTime: "2026-10-01T00:00:00Z",
    })
    const requests = []
    globalThis.fetch = async (value, options) => {
      const url = new URL(value)
      assert.equal(url.origin, "https://api.x.com")
      assert.equal(options.headers.authorization, `Bearer ${credential}`)
      assert.ok(!url.href.includes(credential))
      assert.equal(
        url.searchParams.get("start_time"),
        "2026-08-31T15:00:00.000Z",
      )
      assert.equal(url.searchParams.get("end_time"), "2026-09-30T15:00:00.000Z")
      const token = url.searchParams.get("next_token")
      requests.push(token)
      return token
        ? json({ data: [xEntry("102", "2026-09-03T00:00:00Z")], meta: {} })
        : json({
            data: [
              xEntry("101", "2026-09-20T00:00:00Z"),
              xEntry("outside", "2026-10-01T00:00:00Z"),
            ],
            meta: { next_token: "synthetic-page-2" },
          })
    }
    const first = await fetchSourceWindow(database, {
      source: "x",
      account,
      window,
      mapTweet,
      pageLimit: 1,
      now,
    })
    assert.equal(first.hasMore, true)
    assert.equal(first.count, 1)
    const second = await fetchSourceWindow(database, {
      source: "x",
      account,
      window,
      mapTweet,
      pageLimit: 1,
      now,
    })
    assert.equal(second.hasMore, false)
    assert.deepEqual(requests, [null, "synthetic-page-2"])
    assert.equal(
      database
        .prepare<unknown[], Record<string, any>>(
          "SELECT COUNT(*) AS count FROM posts",
        )
        .get().count,
      2,
    )
    assert.equal(getSyncState(database, "x", account).cursorId, "newest")
  })
})

test("backfill resumes its pending interval and advances even over empty intervals", async () => {
  await withSourceEnvironment(async (database: DatabaseConnection) => {
    upsertPosts(database, [post("initial", "2026-10-01T00:00:00Z")])
    const requests = []
    globalThis.fetch = async (value) => {
      const url = new URL(value)
      requests.push([
        url.searchParams.get("start_time"),
        url.searchParams.get("end_time"),
        url.searchParams.get("next_token"),
      ])
      if (requests.length === 1)
        return json({
          data: [xEntry("older", "2026-09-25T00:00:00Z")],
          meta: { next_token: "synthetic-continuation" },
        })
      return json({ data: [], meta: {} })
    }
    const input = {
      source: "x",
      account,
      window: { mode: "before", days: 30 },
      mapTweet,
      pageLimit: 1,
      now,
    }
    await fetchSourceWindow(database, input)
    await fetchSourceWindow(database, input)
    assert.deepEqual(requests[0].slice(0, 2), requests[1].slice(0, 2))
    assert.equal(requests[1][2], "synthetic-continuation")
    assert.equal(
      getSyncState(database, "x-backfill", account).cursorTime,
      "2026-09-01T00:00:00.000Z",
    )
    await fetchSourceWindow(database, input)
    assert.equal(requests[2][1], "2026-09-01T00:00:00.000Z")
    assert.equal(
      getSyncState(database, "x-backfill", account).cursorTime,
      "2026-08-02T00:00:00.000Z",
    )
  })
})

test("source failures retain records and checkpoints without disclosing credential-bearing errors", async () => {
  await withSourceEnvironment(async (database: DatabaseConnection) => {
    let requests = 0
    globalThis.fetch = async () => {
      requests += 1
      if (requests === 1)
        return json({
          data: [xEntry("kept", "2026-09-10T00:00:00Z")],
          meta: { next_token: "synthetic-retry-page" },
        })
      throw new Error(`failed URL and key: ${credential}`)
    }
    const result = await fetchSourceWindow(database, {
      source: "x",
      account,
      window,
      mapTweet,
      pageLimit: 2,
      now,
    })
    assert.equal(result.count, 1)
    assert.equal(result.hasMore, true)
    assert.deepEqual(result.errors, ["source_api_request_failed"])
    assert.ok(!JSON.stringify(result).includes(credential))
    assert.equal(
      database
        .prepare<unknown[], Record<string, any>>(
          "SELECT COUNT(*) AS count FROM posts",
        )
        .get().count,
      1,
    )
    globalThis.fetch = async (value) => {
      assert.equal(
        new URL(value).searchParams.get("next_token"),
        "synthetic-retry-page",
      )
      return json({ data: [], meta: {} })
    }
    await fetchSourceWindow(database, {
      source: "x",
      account,
      window,
      mapTweet,
      now,
    })
  })
})

test("YouTube range pages use header credentials and filter publication dates", async () => {
  await withSourceEnvironment(async (database: DatabaseConnection) => {
    upsertVideos(database, [
      {
        id: "first-video",
        title: "Scheduled video",
        kind: "UPCOMING LIVE",
        published_at: "2026-09-10T00:00:00Z",
        scheduled_at: "2026-12-01T00:00:00Z",
        is_upcoming: true,
      },
    ])
    upsertSyncState(database, {
      source: "youtube",
      accountId: channel,
      cursorId: "live-cursor",
    })
    let requests = 0
    globalThis.fetch = async (value, options) => {
      const url = new URL(value)
      assert.equal(url.origin, "https://www.googleapis.com")
      assert.equal(options.headers["X-Goog-Api-Key"], credential)
      assert.equal(url.searchParams.has("key"), false)
      assert.equal(url.searchParams.get("channelId"), channel)
      requests += 1
      return json({
        items: [
          videoEntry(
            requests === 1 ? "first-video" : "next-video",
            "2026-09-10T00:00:00Z",
          ),
          videoEntry("outside-video", "2026-10-01T00:00:00Z"),
        ],
        ...(requests === 1 ? { nextPageToken: "synthetic-second-page" } : {}),
      })
    }
    const result = await syncYoutube(database, window)
    assert.equal(result.count, 2)
    assert.equal(result.hasMore, false)
    assert.equal(getVideoRecord(database, "first-video").kind, "UPCOMING LIVE")
    assert.equal(
      Boolean(getVideoRecord(database, "first-video").isUpcoming),
      true,
    )
    assert.equal(
      getSyncState(database, "youtube", channel).cursorId,
      "live-cursor",
    )
    assert.equal(
      database
        .prepare<unknown[], Record<string, any>>(
          "SELECT COUNT(*) AS count FROM videos",
        )
        .get().count,
      2,
    )
  })
})

test("official X fetches share the request budget even when an account fails", async () => {
  await withSourceEnvironment(async (database: DatabaseConnection) => {
    process.env.X_HANDLES = "first_test,second_test,third_test"
    process.env.X_MAX_STATUS_REQUESTS = "1"
    let requests = 0
    globalThis.fetch = async () => {
      requests += 1
      return json({ error: credential }, 429)
    }
    await assert.rejects(syncX(database, window))
    assert.equal(requests, 1)
  })
})

test("official recent X pages register weekly boards, exclude stream notices and retain same-image reviews", async (t) => {
  t.mock.method(Date, "now", () => now)
  await withSourceEnvironment(async (database: DatabaseConnection) => {
    upsertSyncState(database, {
      source: "x",
      accountId: account,
      cursorId: "incremental-kept",
      metadata: { pendingStatusIds: ["unfinished"] },
    })
    const incremental = getSyncState(database, "x", account)
    let imageUrl = "https://media.example.invalid/weekly.png"
    globalThis.fetch = async (value, options) => {
      assert.equal(new URL(value).origin, "https://api.x.com")
      assert.equal(options.redirect, "error")
      assert.ok(options.signal instanceof AbortSignal)
      return json({
        data: [
          {
            ...xEntry("101", "2026-10-05T00:00:00Z"),
            text: "This week schedule board",
            attachments: { media_keys: ["board"] },
          },
          {
            ...xEntry("102", "2026-10-06T00:00:00Z"),
            text: "今日は22時から配信予定です",
            attachments: { media_keys: ["stream"] },
          },
        ],
        includes: {
          media: [
            { media_key: "board", url: imageUrl },
            {
              media_key: "stream",
              url: "https://media.example.invalid/stream.png",
            },
          ],
        },
        meta: {},
      })
    }
    const input = { mode: "recent", days: 7 }
    assert.equal((await syncX(database, input)).scheduleAssets, 1)
    const assets = listScheduleAssets(database)
    assert.deepEqual(getDashboard(database).scheduleImages, [])
    assert.deepEqual(assets.map((asset) => asset.id).sort(), [
      "schedule-2026-10-05",
      "weekly-schedule",
    ])
    for (const asset of assets) {
      assert.equal(asset.sourceAccount, account)
      assert.equal(asset.weekStart, "2026-10-05")
      assert.equal(asset.sourceUrl, `https://x.com/${account}/status/101`)
      assert.equal(
        getScheduleAssetReview(database, asset.id).llmStatus,
        "pending",
      )
      upsertScheduleAssetReview(database, {
        assetId: asset.id,
        llmStatus: "schedule",
        llmConfidence: 0.9,
        manualStatus: "schedule",
        manualReason: "Synthetic manual review",
      })
    }
    // Even an approved candidate cannot expose an uncached remote image.
    assert.ok(
      getDashboard(database).assets.every((asset) => asset.url === null),
    )
    const cached = await writeMediaFileAtomic({
      source: "x",
      content: Buffer.concat([
        Buffer.from(
          "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aN4sAAAAASUVORK5CYII=",
          "base64",
        ),
        Buffer.from("synthetic-official-board-cache"),
      ]),
      extension: "png",
    })
    t.after(() =>
      fs.rmSync(resolveMediaCachePath(cached.relativePath), { force: true }),
    )
    upsertMediaAsset(database, {
      source: "x",
      sourceUrl: imageUrl,
      status: "ready",
      cachePath: cached.relativePath,
      mimeType: "image/png",
      sha256: cached.sha256,
      byteSize: cached.byteSize,
    })
    assert.deepEqual(
      getDashboard(database)
        .scheduleImages.map((asset) => asset.id)
        .sort(),
      assets.map((asset) => asset.id).sort(),
    )
    assert.ok(
      getDashboard(database).scheduleImages.every((asset) =>
        asset.url.startsWith("/media/"),
      ),
    )
    const reviews = assets.map((asset) =>
      getScheduleAssetReview(database, asset.id),
    )
    await syncX(database, input)
    assert.deepEqual(listScheduleAssets(database), assets)
    assert.deepEqual(
      assets.map((asset) => getScheduleAssetReview(database, asset.id)),
      reviews,
    )
    imageUrl = "https://media.example.invalid/replacement.png"
    await syncX(database, input)
    for (const asset of assets) {
      const review = getScheduleAssetReview(database, asset.id)
      assert.equal(review.llmStatus, "pending")
      assert.equal(review.manualStatus, "unreviewed")
      assert.equal(review.manualReason, null)
    }
    assert.deepEqual(getDashboard(database).scheduleImages, [])
    assert.deepEqual(getSyncState(database, "x", account), incremental)
    assert.equal(
      database
        .prepare<unknown[], { count: number }>(
          "SELECT COUNT(*) AS count FROM schedule_extractions",
        )
        .get().count,
      0,
    )
    assert.equal(
      database
        .prepare<unknown[], { count: number }>(
          "SELECT COUNT(*) AS count FROM posts",
        )
        .get().count,
      2,
    )
    assert.ok(
      database
        .prepare("SELECT id FROM media_assets WHERE source_url = ?")
        .get(imageUrl),
    )
  })
})

test("historical X boards retain Japan weeks across pages and accounts without regressing the alias", async () => {
  await withSourceEnvironment(async (database: DatabaseConnection) => {
    process.env.X_HANDLES = `${account},other_test`
    let page = 0
    globalThis.fetch = async (value) => {
      const url = new URL(value)
      const otherAccount = url.searchParams.get("query") === "from:other_test"
      const continued = Boolean(url.searchParams.get("next_token"))
      page += 1
      const date = otherAccount
        ? "2026-09-20T20:00:00Z"
        : continued
          ? "2026-09-20T18:00:00Z"
          : "2026-09-20T23:00:00Z"
      return json({
        data: [
          {
            ...xEntry(String(page), date),
            text: "今週の予定表",
            attachments: { media_keys: ["board"] },
          },
          ...(!otherAccount && !continued
            ? [
                {
                  ...xEntry("older-week", "2026-09-13T20:00:00Z"),
                  text: "Weekly schedule 9/14–9/20",
                  attachments: { media_keys: ["older"] },
                },
              ]
            : []),
        ],
        includes: {
          media: [
            {
              media_key: "board",
              url: `https://media.example.invalid/board-${page}.png`,
            },
            {
              media_key: "older",
              url: "https://media.example.invalid/older.png",
            },
          ],
        },
        meta:
          !otherAccount && !continued
            ? { next_token: "synthetic-second-page" }
            : {},
      })
    }
    await syncX(database, window)
    const assets = listScheduleAssets(database)
    assert.deepEqual(assets.map((asset) => asset.id).sort(), [
      "schedule-2026-09-14",
      "schedule-2026-09-21",
      "weekly-schedule",
    ])
    const latest = assets.find((asset) => asset.id === "schedule-2026-09-21")
    assert.equal(latest.sourceAccount, account)
    assert.equal(latest.url, "https://media.example.invalid/board-1.png")
    assert.equal(
      assets.find((asset) => asset.id === "weekly-schedule").url,
      latest.url,
    )
    assert.equal(getSyncState(database, "x", account), null)
    assert.equal(getSyncState(database, "x", "other_test"), null)
  })
})

test("official X refresh fills a same-source schedule placeholder even with a newer metadata timestamp", async () => {
  await withSourceEnvironment(async (database: DatabaseConnection) => {
    upsertAssets(database, [
      {
        id: "weekly-schedule",
        kind: "schedule",
        url: null,
        source_url: `https://x.com/${account}/status/101`,
        updated_at: "2026-10-01T00:00:00Z",
      },
    ])
    globalThis.fetch = async () =>
      json({
        data: [
          {
            ...xEntry("101", "2026-09-20T00:00:00Z"),
            text: "Weekly schedule board",
            attachments: { media_keys: ["board"] },
          },
        ],
        includes: {
          media: [
            {
              media_key: "board",
              url: "https://media.example.invalid/board.png",
            },
          ],
        },
        meta: {},
      })
    await fetchSourceWindow(database, {
      source: "x",
      account,
      window,
      mapTweet,
      now,
    })
    assert.equal(
      listScheduleAssets(database).find(
        (asset) => asset.id === "weekly-schedule",
      ).url,
      "https://media.example.invalid/board.png",
    )
  })
})

test("historical X posts, board candidates, media and checkpoint roll back together", async () => {
  await withSourceEnvironment(async (database: DatabaseConnection) => {
    globalThis.fetch = async () =>
      json({
        data: [
          {
            ...xEntry("101", "2026-09-20T00:00:00Z"),
            text: "Weekly schedule board",
            attachments: { media_keys: ["board"] },
          },
        ],
        includes: {
          media: [
            {
              media_key: "board",
              url: "https://media.example.invalid/board.png",
            },
          ],
        },
        meta: {},
      })
    database.exec(
      "CREATE TRIGGER reject_checkpoint BEFORE INSERT ON sync_state BEGIN SELECT RAISE(ABORT, 'synthetic checkpoint failure'); END",
    )
    await assert.rejects(
      fetchSourceWindow(database, {
        source: "x",
        account,
        window,
        mapTweet,
        now,
      }),
    )
    for (const table of [
      "posts",
      "assets",
      "media_assets",
      "media_links",
      "schedule_asset_reviews",
      "sync_state",
    ])
      assert.equal(
        database
          .prepare<unknown[], { count: number }>(
            `SELECT COUNT(*) AS count FROM ${table}`,
          )
          .get().count,
        0,
      )
  })
})

for (const source of ["x", "youtube"]) {
  for (const sizeHeader of ["declared", "missing", "understated"]) {
    test(`${source} history cancels ${sizeHeader} oversized responses and preserves snapshots and continuation`, async () => {
      await withSourceEnvironment(async (database: DatabaseConnection) => {
        const input = {
          source,
          account: source === "x" ? account : channel,
          window,
          mapTweet,
          now,
          pageLimit: 1,
        }
        const normal =
          source === "x"
            ? {
                data: [xEntry("kept", "2026-09-10T00:00:00Z")],
                meta: { next_token: "synthetic-kept-page" },
              }
            : {
                items: [videoEntry("kept", "2026-09-10T00:00:00Z")],
                nextPageToken: "synthetic-kept-page",
              }
        globalThis.fetch = async () => json(normal)
        await fetchSourceWindow(database, input)
        const savedState = database.prepare("SELECT * FROM sync_state").all()
        const savedRecords = database
          .prepare(`SELECT * FROM ${source === "x" ? "posts" : "videos"}`)
          .all()
        let cancelled = false
        let pulls = 0
        // UTF-8 byte count exceeds the cap while the character count does not.
        const chunk = Buffer.from("界".repeat(256 * 1024))
        const oversized = () =>
          new Response(
            new ReadableStream({
              pull(controller) {
                pulls += 1
                if (pulls === 1) controller.enqueue(Buffer.from('{"unused":"'))
                else if (pulls < 16) controller.enqueue(chunk)
                else {
                  controller.enqueue(Buffer.from('"}'))
                  controller.close()
                }
              },
              cancel() {
                cancelled = true
              },
            }),
            {
              headers:
                sizeHeader === "missing"
                  ? {}
                  : {
                      "content-length":
                        sizeHeader === "declared"
                          ? String(8 * 1024 * 1024 + 1)
                          : "1",
                    },
            },
          )
        globalThis.fetch = async (value, options) => {
          const url = new URL(value)
          assert.equal(
            url.searchParams.get(source === "x" ? "next_token" : "pageToken"),
            "synthetic-kept-page",
          )
          assert.equal(options.redirect, "error")
          assert.ok(options.signal instanceof AbortSignal)
          return oversized()
        }
        await assert.rejects(fetchSourceWindow(database, input), {
          message: "source_api_response_too_large",
          requested: 1,
        })
        assert.equal(cancelled, true)
        assert.ok(pulls < 16)
        if (sizeHeader === "declared") assert.ok(pulls <= 1)
        assert.deepEqual(
          database.prepare("SELECT * FROM sync_state").all(),
          savedState,
        )
        assert.deepEqual(
          database
            .prepare(`SELECT * FROM ${source === "x" ? "posts" : "videos"}`)
            .all(),
          savedRecords,
        )
        globalThis.fetch = async () =>
          json(source === "x" ? { data: [], meta: {} } : { items: [] })
        assert.equal((await fetchSourceWindow(database, input)).hasMore, false)
      })
    })
  }
}

test("an oversized later history page reports a controlled error and retains the committed first page", async () => {
  await withSourceEnvironment(async (database: DatabaseConnection) => {
    let calls = 0
    globalThis.fetch = async () =>
      ++calls === 1
        ? json({
            data: [xEntry("kept", "2026-09-10T00:00:00Z")],
            meta: { next_token: "synthetic-kept-page" },
          })
        : new Response(credential, {
            headers: { "content-length": String(8 * 1024 * 1024 + 1) },
          })
    const result = await fetchSourceWindow(database, {
      source: "x",
      account,
      window,
      mapTweet,
      now,
    })
    assert.equal(result.count, 1)
    assert.equal(result.hasMore, true)
    assert.deepEqual(result.errors, ["source_api_response_too_large"])
    assert.ok(!JSON.stringify(result).includes(credential))
    assert.equal(
      database
        .prepare<unknown[], { cursor_id: string }>(
          "SELECT cursor_id FROM sync_state WHERE source = 'x-window'",
        )
        .get().cursor_id,
      "synthetic-kept-page",
    )
  })
})

test("saved rolling periods reach scheduled jobs and admin fetch endpoints validate scope", async () => {
  await withSourceEnvironment(async (database: DatabaseConnection) => {
    const calls = []
    const jobs = createSyncJobManager({
      database,
      runSyncImpl: async (options) => {
        calls.push(options)
        return { status: "success", results: {} }
      },
    })
    const workflow = upsertWorkflow(database, {
      id: "rolling",
      name: "Rolling",
      steps: ["x", "youtube"],
      sourceLookbackDays: { x: 30, youtube: 60 },
    })
    const scheduler = createWorkflowScheduler({ database, jobs })
    scheduler.run(workflow.id, "scheduler")
    await new Promise((resolve) => setTimeout(resolve, 5))
    assert.deepEqual(calls[0].fetchWindows, {
      x: { mode: "recent", days: 30 },
      youtube: { mode: "recent", days: 60 },
    })
    upsertWorkflow(database, { id: "rolling", name: "Renamed" })
    assert.deepEqual(listWorkflows(database)[0].sourceLookbackDays, {
      x: 30,
      youtube: 60,
    })
    const server = createApp({
      database,
      syncJobs: jobs,
      adminMode: "development",
    }).listen(0, "127.0.0.1")
    await new Promise((resolve) => server.once("listening", resolve))
    const origin = `http://127.0.0.1:${listeningPort(server)}`
    try {
      const submit = (source, body) =>
        localFetch(`${origin}/api/admin/sources/${source}/fetch`, {
          method: "POST",
          headers: { "content-type": "application/json", origin },
          body: JSON.stringify(body),
        })
      const status = await (
        await localFetch(`${origin}/api/admin/sources/fetch`)
      ).json()
      assert.equal(status.x.historyAvailable, true)
      assert.ok(!JSON.stringify(status).includes(credential))
      assert.equal((await submit("other", { window })).status, 400)
      assert.equal(
        (await submit("x", { window: { mode: "before", days: 7 } })).status,
        400,
      )
      assert.equal(
        (await submit("x", { window: { mode: "recent", days: 0 } })).status,
        400,
      )
      const accepted = await submit("youtube", { window })
      assert.equal(accepted.status, 202)
      await new Promise((resolve) => setTimeout(resolve, 5))
      assert.deepEqual(calls[1].steps, ["youtube", "media"])
      assert.deepEqual(calls[1].fetchWindows, { youtube: window })
      delete process.env.X_API_BEARER_TOKEN
      assert.equal((await submit("x", { window })).status, 400)
    } finally {
      await new Promise((resolve) => server.close(resolve))
      scheduler.stop()
    }
  })
})
