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
  initializeDatabase,
  listWorkflows,
  upsertPosts,
  upsertSyncState,
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
