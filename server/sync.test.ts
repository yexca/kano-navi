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
  upsertSyncState,
} from "./database.ts"
import { encryptSecret } from "./secret-store.ts"

process.env.SYNC_REQUEST_DELAY_MS = "0"
process.env.SYNC_TIMEOUT_MS = "1000"
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

for (const failureStatus of [404, 503]) {
  test(`X rotates a persistent ${failureStatus} so a newer post gets the one-request budget`, async (t) => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "kano-x-retry-"))
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
        "SKIP_X",
      ].map((key) => [key, process.env[key]]),
    )
    const fixedNow = Date.parse("2026-09-16T00:00:00Z")
    t.mock.method(Date, "now", () => fixedNow)
    const failedId = snowflakeFor(new Date(fixedNow - 7200_000))
    const newerId = snowflakeFor(new Date(fixedNow - 3600_000))
    let profileIds = [failedId]
    let recovered = false
    const requested: string[] = []
    const run = () => runSync({ database, steps: ["x"], setExitCode: false })
    try {
      process.env.X_HANDLES = "kano_2525"
      process.env.X_MAX_STATUS_REQUESTS = "1"
      process.env.X_REFRESH_KNOWN = "0"
      process.env.X_BOOTSTRAP_DAYS = "7"
      process.env.X_SCHEDULE_REFRESH_LIMIT = "0"
      delete process.env.X_API_BEARER_TOKEN
      delete process.env.SKIP_X
      globalThis.fetch = async (url) => {
        const value = String(url)
        if (value === "https://x.com/kano_2525")
          return response(
            profileIds
              .map((id) => `<a href="/kano_2525/status/${id}">post</a>`)
              .join(""),
          )
        const id = value.match(
          /^https:\/\/api\.vxtwitter\.com\/kano_2525\/status\/(\d+)$/u,
        )?.[1]
        assert.ok(id)
        requested.push(id)
        if (id === failedId && !recovered)
          return response("unavailable", { status: failureStatus })
        return response(
          JSON.stringify({
            tweetID: id,
            text: "synthetic",
            date: snowflakeDate(id).toISOString(),
            author: { screenName: "kano_2525" },
          }),
        )
      }

      assert.equal((await run()).status, "failed")
      profileIds = [newerId]
      const statuses = []
      let successfulState: Record<string, any>
      for (let attempt = 0; attempt < 3; attempt += 1) {
        const before = requested.length
        if (attempt === 2 && failureStatus === 503) recovered = true
        statuses.push((await run()).status)
        assert.equal(requested.length - before, 1)
        if (attempt === 0) {
          const state = getSyncState(database, "x", "kano_2525")
          assert.equal(state.cursorId, null)
          assert.equal(state.cursorTime, null)
          assert.equal(state.lastSuccessAt, null)
          assert.deepEqual(state.metadata.pendingStatusIds, [newerId, failedId])
          database.close()
          database = initializeDatabase({ seed: false, filename })
          assert.deepEqual(getSyncState(database, "x", "kano_2525"), state)
        }
        if (attempt === 1) {
          assert.ok(
            database.prepare("SELECT id FROM posts WHERE id=?").get(newerId),
          )
          successfulState = getSyncState(database, "x", "kano_2525")
        }
      }
      assert.deepEqual(requested, [failedId, failedId, newerId, failedId])
      assert.deepEqual(statuses, [
        "failed",
        "success",
        failureStatus === 503 ? "success" : "failed",
      ])
      const state = getSyncState(database, "x", "kano_2525")
      assert.equal(state.cursorId, newerId)
      assert.equal(state.cursorTime, snowflakeDate(newerId).toISOString())
      if (failureStatus === 404)
        assert.equal(state.lastSuccessAt, successfulState.lastSuccessAt)
      assert.deepEqual(
        state.metadata.pendingStatusIds,
        failureStatus === 503 ? [] : [failedId],
      )
      assert.deepEqual(
        database
          .prepare<unknown[], { status: string }>(
            "SELECT status FROM sync_runs ORDER BY id",
          )
          .all()
          .map((row) => row.status),
        ["failed", ...statuses],
      )
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

for (const budget of [1, 2]) {
  test(`X retries every admitted ID fairly with a ${budget}-request budget and new arrivals`, async (t) => {
    const database = initializeDatabase({ seed: false, filename: ":memory:" })
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
    const ids = Array.from({ length: 4 }, (_, index) =>
      snowflakeFor(new Date(fixedNow - (10 + index) * 86400_000)),
    )
    const requested: string[] = []
    let profileId = ""
    // Existing metadata needs no migration; queue order survives an empty profile.
    upsertSyncState(database, {
      source: "x",
      accountId: "kano_2525",
      metadata: { pendingStatusIds: ids, retained: { value: "synthetic" } },
      recordSuccess: false,
    })
    try {
      process.env.X_HANDLES = "kano_2525"
      process.env.X_MAX_STATUS_REQUESTS = String(budget)
      process.env.X_REFRESH_KNOWN = "0"
      process.env.X_BOOTSTRAP_DAYS = "7"
      process.env.X_SCHEDULE_REFRESH_LIMIT = "0"
      delete process.env.X_API_BEARER_TOKEN
      globalThis.fetch = async (url) => {
        const value = String(url)
        if (value === "https://x.com/kano_2525")
          return response(
            profileId
              ? `<a href="/kano_2525/status/${profileId}">post</a>`
              : "",
          )
        const id = value.match(
          /^https:\/\/api\.vxtwitter\.com\/kano_2525\/status\/(\d+)$/u,
        )?.[1]
        assert.ok(id)
        requested.push(id)
        return response("unavailable", { status: 404 })
      }

      const admittedIds = [...ids]
      for (let attempt = 0; attempt < 8; attempt += 1) {
        // Newer profile IDs join at the tail without overtaking admitted work.
        profileId = snowflakeFor(new Date(fixedNow - (8 - attempt) * 3600_000))
        admittedIds.push(profileId)
        const before = requested.length
        await assert.rejects(syncX(database))
        assert.equal(requested.length - before, budget)
        assert.equal(new Set(requested.slice(before)).size, budget)
        const state = getSyncState(database, "x", "kano_2525")
        assert.deepEqual(
          [...state.metadata.pendingStatusIds].sort(),
          [...admittedIds].sort(),
        )
        assert.deepEqual(state.metadata.retained, { value: "synthetic" })
        assert.equal(state.cursorId, null)
        assert.equal(state.cursorTime, null)
        assert.equal(state.lastSuccessAt, null)
      }
      assert.deepEqual(requested.slice(0, ids.length), ids)
      assert.ok(requested.includes(admittedIds[4]))
      // Once arrivals stop, every queued ID receives exactly one request per round.
      profileId = ""
      requested.length = 0
      for (
        let attempt = 0;
        attempt < admittedIds.length / budget;
        attempt += 1
      ) {
        const before = requested.length
        await assert.rejects(syncX(database))
        assert.equal(requested.length - before, budget)
      }
      assert.deepEqual([...requested].sort(), [...admittedIds].sort())
    } finally {
      globalThis.fetch = originalFetch
      for (const [key, value] of Object.entries(originalEnvironment)) {
        if (value == null) delete process.env[key]
        else process.env[key] = value
      }
      database.close()
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

test("an entirely failed schedule stage marks an otherwise skipped sync as failed", async () => {
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

    const result = await runSync({
      database,
      closeDatabase: false,
      setExitCode: false,
    })
    assert.equal(result.status, "failed")
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

for (const failure of [
  "503",
  "incomplete 200",
  "metadata-only 200",
  "upcoming without schedule 200",
  "invalid schedule 200",
])
  test(`RSS/streams overlap recovers reservation details after ${failure} under budget and preserves snapshots`, async (t) => {
    const database = initializeDatabase({ seed: false, filename: ":memory:" })
    t.after(() => database.close())
    const originalFetch = globalThis.fetch
    const savedEnvironment = Object.fromEntries(
      ["YOUTUBE_MAX_DETAIL_REQUESTS", "YOUTUBE_CHANNEL_ID"].map((key) => [
        key,
        process.env[key],
      ]),
    )
    t.after(() => {
      globalThis.fetch = originalFetch
      for (const [key, value] of Object.entries(savedEnvironment)) {
        if (value == null) delete process.env[key]
        else process.env[key] = value
      }
    })
    process.env.YOUTUBE_MAX_DETAIL_REQUESTS = "1"
    process.env.YOUTUBE_CHANNEL_ID = "synthetic-channel"
    delete process.env.YOUTUBE_API_KEY
    const id = "overlap0001"
    let watchCalls = 0
    let fail = true
    let includeOverflow = true
    let overflowCalls = 0
    globalThis.fetch = async (url) => {
      const value = String(url)
      if (value.includes("/feeds/videos.xml"))
        return response(
          feedXml([
            {
              id,
              title: "RSS reservation",
              publishedAt: "2098-01-01T00:00:00Z",
            },
          ]),
        )
      if (value.endsWith("/streams"))
        return response(
          `<a href="/watch?v=${id}">reservation</a>${includeOverflow ? '<a href="/watch?v=overflow001">other</a>' : ""}`,
        )
      if (value === "https://www.youtube.com/watch?v=overflow001") {
        overflowCalls++
        return response(
          `<script>var ytInitialPlayerResponse = ${JSON.stringify({ videoDetails: { videoId: "overflow001", title: "Ordinary video", isUpcoming: false }, playabilityStatus: { status: "OK" } })};</script>`,
        )
      }
      assert.equal(value, `https://www.youtube.com/watch?v=${id}`)
      watchCalls++
      if (fail) {
        if (failure === "503") return response(null, { status: 503 })
        if (failure === "incomplete 200")
          return response("<html>temporarily incomplete</html>")
        if (failure === "metadata-only 200")
          return response('<meta property="og:title" content="Partial page"/>')
        if (failure === "upcoming without schedule 200")
          return response(
            `<script>var ytInitialPlayerResponse = ${JSON.stringify({ videoDetails: { videoId: id, title: "Reservation", isUpcoming: true } })};</script>`,
          )
        return response(
          '<meta property="og:title" content="Reservation"/><script>{"scheduledStartTime":"invalid","isUpcoming":true}</script>',
        )
      }
      return response(
        '<meta property="og:title" content="Detailed reservation"/><script>{"scheduledStartTime":"2099-01-01T11:00:00Z","isUpcoming":true}</script>',
      )
    }
    const first = await syncYoutube(database)
    assert.equal(first.inspected, 0)
    assert.equal(watchCalls, 1)
    assert.equal(getVideoRecord(database, id).scheduledAt, null)
    assert.deepEqual(
      getSyncState(database, "youtube", "synthetic-channel").metadata
        .streamDetailsCheckedIds,
      [],
    )
    assert.equal(first.errors.length, 1)
    if (failure !== "503")
      assert.match(first.errors[0], /youtube_watch_details_incomplete/u)
    assert.equal(
      getSyncState(database, "youtube", "synthetic-channel").metadata.inspected,
      0,
    )
    assert.equal(
      getSyncState(database, "youtube", "synthetic-channel").metadata
        .detailRequested,
      1,
    )
    const rssSnapshot = getVideoRecord(database, id)
    fail = false
    // An unattempted discovery gets a turn before the failed detail retries.
    const rotated = await syncYoutube(database)
    assert.equal(rotated.inspected, 1)
    assert.equal(overflowCalls, 1)
    assert.equal(watchCalls, 1)
    includeOverflow = false
    // The failed detail remains eligible despite its RSS row and cursor.
    const second = await syncYoutube(database)
    assert.equal(second.inspected, 1)
    assert.equal(watchCalls, 2)
    const known = getVideoRecord(database, id)
    assert.equal(known.isUpcoming, 1)
    assert.equal(known.scheduledAt, "2099-01-01T11:00:00.000Z")
    assert.equal(known.title, "Detailed reservation")
    assert.equal(known.publishedAt, rssSnapshot.publishedAt)
    assert.deepEqual(
      getSyncState(database, "youtube", "synthetic-channel").metadata
        .streamDetailsCheckedIds,
      [id],
    )
    assert.equal(
      database
        .prepare<unknown[], { count: number }>(
          "SELECT COUNT(*) AS count FROM events WHERE id=?",
        )
        .get(`youtube-${id}`).count,
      1,
    )
    const reservation = database
      .prepare<unknown[], Record<string, any>>(
        "SELECT * FROM events WHERE id=?",
      )
      .get(`youtube-${id}`)
    assert.equal(reservation.starts_at, known.scheduledAt)
    // A bounded recent window may refresh reservations but cannot rewrite the
    // normal incremental cursor or its successful-check metadata.
    const incrementalState = getSyncState(
      database,
      "youtube",
      "synthetic-channel",
    )
    includeOverflow = false
    await syncYoutube(database, { mode: "recent", days: 7 })
    assert.equal(watchCalls, 3)
    assert.deepEqual(
      getSyncState(database, "youtube", "synthetic-channel"),
      incrementalState,
    )
    const refreshedReservation = database
      .prepare<unknown[], Record<string, any>>(
        "SELECT * FROM events WHERE id=?",
      )
      .get(`youtube-${id}`)
    // Budget prioritizes unchecked discoveries before rechecking known reservations.
    globalThis.fetch = async (url) => {
      const value = String(url)
      if (value.includes("/feeds/videos.xml"))
        return response(
          feedXml([
            {
              id,
              title: "RSS reservation",
              publishedAt: "2098-01-01T00:00:00Z",
            },
          ]),
        )
      if (value.endsWith("/streams"))
        return response(`<a href="/watch?v=${id}">reservation</a>`)
      watchCalls++
      return response(
        '<html><meta property="og:title" content="Partial refresh"/></html>',
      )
    }
    await syncYoutube(database)
    assert.deepEqual(getVideoRecord(database, id), known)
    assert.deepEqual(
      database.prepare("SELECT * FROM events WHERE id=?").get(`youtube-${id}`),
      refreshedReservation,
    )
    assert.equal(watchCalls, 4)
  })

test("a valid ordinary YouTube player completes its detail check without a reservation", async (t) => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  t.after(() => database.close())
  const id = "ordinary001"
  let watchCalls = 0
  t.mock.method(globalThis, "fetch", async (url) => {
    const value = String(url)
    if (value.includes("/feeds/videos.xml"))
      return response(
        feedXml([
          { id, title: "Ordinary video", publishedAt: "2098-01-01T00:00:00Z" },
        ]),
      )
    if (value.endsWith("/streams"))
      return response(`<a href="/watch?v=${id}">video</a>`)
    assert.equal(value, `https://www.youtube.com/watch?v=${id}`)
    watchCalls++
    return response(
      `<script>var ytInitialPlayerResponse = ${JSON.stringify({ playabilityStatus: { status: "OK" }, videoDetails: { videoId: id, title: "Ordinary video", isUpcoming: false } })};</script>`,
    )
  })
  assert.equal((await syncYoutube(database)).inspected, 1)
  assert.equal((await syncYoutube(database)).inspected, 0)
  assert.equal(watchCalls, 1)
  assert.equal(getVideoRecord(database, id).scheduledAt, null)
  assert.equal(
    database
      .prepare<unknown[], { count: number }>(
        "SELECT count(*) AS count FROM events",
      )
      .get().count,
    0,
  )
})

test("an escaped YouTube player response completes a detail check", async (t) => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  t.after(() => database.close())
  const id = "escaped0001"
  const player = JSON.stringify({
    playabilityStatus: { status: "OK" },
    videoDetails: { videoId: id, title: "Escaped video", isUpcoming: false },
  })
  t.mock.method(globalThis, "fetch", async (url) => {
    const value = String(url)
    if (value.includes("/feeds/videos.xml"))
      return response(
        feedXml([
          { id, title: "Escaped video", publishedAt: "2098-01-01T00:00:00Z" },
        ]),
      )
    if (value.endsWith("/streams"))
      return response(`<a href="/watch?v=${id}">video</a>`)
    return response(
      `<script>var ytplayer = {"args":{"player_response":${JSON.stringify(player)}}};</script>`,
    )
  })
  assert.equal((await syncYoutube(database)).inspected, 1)
  assert.equal(getVideoRecord(database, id).title, "Escaped video")
})

test("source deadline covers delayed RSS bodies and failed jobs release the single-flight queue", async (t) => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  t.after(() => database.close())
  const originalFetch = globalThis.fetch
  t.after(() => {
    globalThis.fetch = originalFetch
  })
  let cancelled = false
  let signal: AbortSignal
  globalThis.fetch = async (_url, options) => {
    signal = options.signal
    return response(
      new ReadableStream({
        pull() {
          return new Promise(() => {})
        },
        cancel() {
          cancelled = true
        },
      }),
    )
  }
  const { createSyncJobManager } = await import("./sync-jobs.ts")
  const manager = createSyncJobManager({ database })
  const first = manager.start("workflow", "admin", { steps: ["youtube"] })
  const wait = async (id) => {
    for (let i = 0; i < 400; i++) {
      const job = manager.get(id)
      if (["failed", "completed"].includes(job.status)) return job
      await new Promise((resolve) => setTimeout(resolve, 10))
    }
    throw new Error("job failed to respect source timeout")
  }
  const failed = await wait(first.job.id)
  assert.equal(failed.run.status, "failed")
  assert.equal(signal.aborted, true)
  assert.equal(cancelled, true)
  globalThis.fetch = async (url) =>
    String(url).includes("feeds/videos.xml")
      ? response(
          feedXml([
            {
              id: "afterstall1",
              title: "Recovered",
              publishedAt: "2098-01-01T00:00:00Z",
            },
          ]),
        )
      : response("")
  const next = manager.start("workflow", "admin", { steps: ["youtube"] })
  assert.equal(next.accepted, true)
  const completed = await wait(next.job.id)
  assert.equal(completed.run.status, "success")
})

for (const source of ["x", "youtube"])
  test(`${source} rotates failing work fairly across rounds and a SQLite restart with budget one`, async (t) => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "kano-budget-")),
      filename = path.join(directory, "snapshot.sqlite")
    let database = initializeDatabase({ seed: false, filename })
    t.after(() => {
      database.close()
      fs.rmSync(directory, { recursive: true, force: true })
    })
    const saved = Object.fromEntries(
      [
        "X_HANDLES",
        "X_MAX_STATUS_REQUESTS",
        "X_REFRESH_KNOWN",
        "X_SCHEDULE_REFRESH_LIMIT",
        "X_API_BEARER_TOKEN",
        "YOUTUBE_CHANNEL_ID",
        "YOUTUBE_MAX_DETAIL_REQUESTS",
        "YOUTUBE_API_KEY",
      ].map((key) => [key, process.env[key]]),
    )
    t.after(() => {
      for (const [key, value] of Object.entries(saved)) {
        if (value == null) delete process.env[key]
        else process.env[key] = value
      }
    })
    process.env.X_HANDLES = "example,other_test"
    process.env.X_MAX_STATUS_REQUESTS = "1"
    process.env.X_REFRESH_KNOWN = "1"
    process.env.X_SCHEDULE_REFRESH_LIMIT = "0"
    process.env.YOUTUBE_CHANNEL_ID = "synthetic-budget-channel"
    process.env.YOUTUBE_MAX_DETAIL_REQUESTS = "1"
    delete process.env.X_API_BEARER_TOKEN
    delete process.env.YOUTUBE_API_KEY
    const fixedNow = Date.parse("2026-10-09T00:00:00Z")
    t.mock.method(Date, "now", () => fixedNow)
    const ids = [
      snowflakeFor(new Date(fixedNow - 3600000)),
      snowflakeFor(new Date(fixedNow - 7200000)),
    ]
    let recovered = false
    const attempts = []
    t.mock.method(globalThis, "fetch", async (value) => {
      const url = String(value)
      if (url.startsWith("https://x.com/")) {
        const handle = url.split("/").at(-1),
          id = handle === "example" ? ids[0] : ids[1]
        return response(`<a href="/${handle}/status/${id}">synthetic</a>`)
      }
      if (url.includes("api.vxtwitter.com")) {
        const handle = url.split("/")[3],
          id = url.split("/").at(-1)
        attempts.push(handle)
        if (handle === "example" && !recovered)
          return response("synthetic missing", { status: 404 })
        return response(
          JSON.stringify({
            tweetID: id,
            text: "synthetic",
            date: snowflakeDate(id).toISOString(),
            author: { screenName: handle },
          }),
        )
      }
      if (url.includes("/feeds/videos.xml"))
        return response(
          feedXml([
            {
              id: "rssbudget01",
              title: "Synthetic feed",
              publishedAt: "2026-10-08T00:00:00Z",
            },
          ]),
        )
      if (url.endsWith("/streams"))
        return response(
          '<a href="/watch?v=failed00001">first</a><a href="/watch?v=valid000001">second</a>',
        )
      const id = new URL(url).searchParams.get("v")
      attempts.push(id)
      if (id === "failed00001" && !recovered)
        return response("synthetic missing", { status: 404 })
      return response(
        `<meta property="og:title" content="Synthetic reservation"/><script>{"scheduledStartTime":"2099-01-01T11:00:00Z","isUpcoming":true}</script>`,
      )
    })
    const run = async () => {
      const before = attempts.length
      try {
        const result =
          source === "x" ? await syncX(database) : await syncYoutube(database)
        if (source === "x") assert.equal(result.requested, 1)
      } catch (error) {
        assert.equal(source, "x")
        assert.match(error.message, /同步失败/u)
      }
      assert.equal(attempts.length - before, 1)
    }
    await run()
    database.close()
    database = initializeDatabase({ seed: false, filename })
    await run()
    await run()
    assert.deepEqual(
      attempts,
      source === "x"
        ? ["example", "other_test", "example"]
        : ["failed00001", "valid000001", "failed00001"],
    )
    if (source === "x")
      assert.ok(
        database
          .prepare("SELECT id FROM posts WHERE account_handle='other_test'")
          .get(),
      )
    else assert.ok(getVideoRecord(database, "valid000001").scheduledAt)
    recovered = true
    await run()
    await run()
    if (source === "x")
      assert.ok(
        database
          .prepare("SELECT id FROM posts WHERE account_handle='example'")
          .get(),
      )
    else assert.ok(getVideoRecord(database, "failed00001").scheduledAt)
  })
