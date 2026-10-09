import assert from "node:assert/strict"
import crypto from "node:crypto"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"
import test from "node:test"
import Database from "better-sqlite3"
import { createApp } from "./app.ts"
import { listeningPort } from "./http-address.ts"
import {
  initializeDatabase,
  getDashboard,
  getCalendarPage,
  getEvent,
  getPostLlmState,
  getScheduleAssetReview,
  upsertPosts,
  upsertAssets,
  upsertEvents,
  upsertVideos,
  upsertMediaAsset,
  upsertLlmProvider,
  setLlmRouteProviders,
  updateScheduleAssetManualReview,
  updateScheduleAssetPeriod,
  requestPostLlmReprocess,
  listMediaLinks,
  setFeaturedVideoId,
  upsertScheduleAssetReview,
  upsertScheduleAssetsFromPosts,
} from "./database.ts"
import { resolveMediaCachePath, writeMediaFileAtomic } from "./media-cache.ts"
import {
  verifyScheduleAsset,
  extractSchedulePost,
} from "./schedule-extractor.ts"
import { schedulePeriod } from "./schedule-asset.ts"
import { deriveDashboardAt } from "../src/dashboard/derive-dashboard.ts"
import { encryptSecret } from "./secret-store.ts"

process.env.LLM_SECRETS_KEY = "synthetic-regression-master-key"
const now = new Date("2026-10-09T00:00:00Z")
function fixture(t) {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  t.after(() => database.close())
  return database
}
async function image(t, database, sourceUrl) {
  const written = await writeMediaFileAtomic({
    content: Buffer.concat([
      Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
      Buffer.from(crypto.randomUUID()),
    ]),
    extension: "png",
  })
  const file = resolveMediaCachePath(written.relativePath)
  t.after(() => fs.rmSync(file, { force: true }))
  upsertMediaAsset(database, {
    source: "x",
    sourceUrl,
    status: "ready",
    cachePath: written.relativePath,
    mimeType: "image/png",
    sha256: written.sha256,
    byteSize: written.byteSize,
  })
  return file
}
function vision(database) {
  upsertLlmProvider(database, {
    id: "synthetic-vision",
    name: "Synthetic vision",
    model: "synthetic-model",
    baseUrl: "https://provider.example.invalid/v1",
    capabilities: ["text", "image"],
    replaceApiKey: true,
    apiKeyCiphertext: encryptSecret(
      "not-a-real-api-key",
      process.env.LLM_SECRETS_KEY,
    ),
  })
  setLlmRouteProviders(database, "schedule_board", ["synthetic-vision"])
}
function resultResponse(result) {
  return new Response(JSON.stringify({ output_text: JSON.stringify(result) }), {
    status: 200,
  })
}
function deferred() {
  let resolve
  const promise = new Promise<any>((done) => {
    resolve = done
  })
  return { promise, resolve }
}
const verdict = {
  classification: "schedule",
  confidence: 0.95,
  evidence: "dated table",
  reason: "synthetic board",
}

for (const status of ["schedule", "not_schedule"])
  test(`in-flight image verification preserves manual ${status} and its reason`, async (t) => {
    const database = fixture(t),
      url = "https://pbs.twimg.com/media/concurrent.png"
    upsertAssets(database, [
      { id: "board", kind: "schedule", url, week_start: "2026-10-05" },
    ])
    await image(t, database, url)
    vision(database)
    const started = deferred(),
      release = deferred()
    const pending = verifyScheduleAsset(database, "board", {
      fetchImpl: async () => {
        started.resolve()
        await release.promise
        return resultResponse(verdict)
      },
    })
    await started.promise
    updateScheduleAssetManualReview(database, "board", {
      status,
      reason: "operator decision during response",
    })
    const checked = getScheduleAssetReview(database, "board").manualCheckedAt
    release.resolve()
    assert.equal((await pending).status, "success")
    const review = getScheduleAssetReview(database, "board")
    assert.equal(review.manualStatus, status)
    assert.equal(review.manualReason, "operator decision during response")
    assert.equal(review.manualCheckedAt, checked)
    assert.equal(review.approved, status === "schedule")
    assert.equal(
      getDashboard(database, { now }).assets.length,
      status === "schedule" ? 1 : 0,
    )
  })

for (const change of ["replacement", "aba", "content"])
  test(`image verification discards stale ${change} response`, async (t) => {
    const database = fixture(t),
      url = "https://pbs.twimg.com/media/stale.png"
    const asset = {
      id: "board",
      kind: "schedule",
      url,
      week_start: "2026-10-05",
    }
    upsertAssets(database, [asset])
    await image(t, database, url)
    vision(database)
    const started = deferred(),
      release = deferred()
    const pending = verifyScheduleAsset(database, "board", {
      fetchImpl: async () => {
        started.resolve()
        await release.promise
        return resultResponse(verdict)
      },
    })
    await started.promise
    if (change === "content") await image(t, database, url)
    else {
      upsertAssets(database, [
        { ...asset, url: "https://pbs.twimg.com/media/replacement.png" },
      ])
      if (change === "aba") upsertAssets(database, [asset])
    }
    updateScheduleAssetManualReview(database, "board", {
      status: "not_schedule",
      reason: "new image rejected",
    })
    release.resolve()
    assert.equal((await pending).status, "stale")
    assert.notEqual(
      getScheduleAssetReview(database, "board").llmStatus,
      "schedule",
    )
    assert.equal(
      getScheduleAssetReview(database, "board").manualReason,
      "new image rejected",
    )
    assert.equal(getDashboard(database, { now }).assets.length, 0)
  })

test("HTTP parser and unexpected errors use bounded logs without request or credential data", async (t) => {
  const database = fixture(t),
    logs = []
  t.mock.method(console, "error", (...args) => {
    logs.push(args)
  })
  const server = createApp({
    database,
    mcpControlToken: "",
    mcpEnabled: false,
  }).listen(0, "127.0.0.1")
  await new Promise<void>((resolve) => server.once("listening", resolve))
  t.after(() => new Promise<void>((resolve) => server.close(() => resolve())))
  const origin = `http://127.0.0.1:${listeningPort(server)}`
  const markers = [
    "not-a-real-api-key",
    "synthetic-password",
    "synthetic-ciphertext",
    "synthetic-authorization",
  ]
  for (const [body, status, code] of [
    [
      '{"apiKey":"not-a-real-api-key","password":"synthetic-password","ciphertext":"synthetic-ciphertext",',
      400,
      "invalid_json",
    ],
    [
      JSON.stringify({ apiKey: markers[0], padding: "x".repeat(40000) }),
      413,
      "request_too_large",
    ],
  ] as const) {
    const response = await fetch(`${origin}/api/admin/login`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        authorization: markers[3],
      },
      body,
    })
    assert.equal(response.status, status)
    assert.deepEqual(await response.json(), { error: code })
  }
  const error = Object.assign(new Error(markers.join(" ")), {
    body: markers,
    headers: { authorization: markers[3] },
  })
  t.mock.method(database, "prepare", () => {
    throw error
  })
  const response = await fetch(`${origin}/api/dashboard`)
  assert.equal(response.status, 500)
  assert.deepEqual(await response.json(), { error: "internal_error" })
  assert.equal(logs.length, 3)
  for (const marker of markers)
    assert.ok(!JSON.stringify(logs).includes(marker))
  for (const [entry] of logs)
    assert.deepEqual(Object.keys(entry).sort(), ["code", "event", "status"])
})

test("multi-image extraction waits for every declared file and preserves complete events and forced requests", async (t) => {
  const database = fixture(t)
  const urls = [
    "https://pbs.twimg.com/media/multi-a.png",
    "https://pbs.twimg.com/media/multi-b.png",
  ]
  let post = {
    id: "multi",
    source: "x",
    text: "今週の予定表",
    url: "https://x.com/example/status/multi",
    published_at: "2026-10-09T00:00:00Z",
    media_url: urls[0],
    media_urls: [urls[0]],
  }
  upsertPosts(database, [post])
  await image(t, database, urls[0])
  let calls = 0,
    inputImages = 0
  const options = {
    apiKey: "not-a-real-api-key",
    model: "synthetic-model",
    fetchImpl: async (_, options) => {
      calls++
      inputImages = JSON.parse(options.body).input[0].content.filter(
        (item) => item.type === "input_image",
      ).length
      return resultResponse({
        events: [
          {
            title: "Complete activity",
            date: "2026-10-10",
            time: null,
            endTime: null,
            timePrecision: "unknown",
            status: "待确认",
            eventType: "stream",
            confidence: 0.95,
            evidence: "synthetic",
          },
        ],
      })
    },
  }
  assert.equal(
    (await extractSchedulePost(database, post, options)).status,
    "success",
  )
  const id = getDashboard(database, { now }).events[0].id
  post = { ...post, media_urls: urls }
  upsertPosts(database, [post])
  requestPostLlmReprocess(database, post.id)
  assert.equal(
    (await extractSchedulePost(database, post, options)).reason,
    "media_pending",
  )
  assert.equal(calls, 1)
  assert.equal(getEvent(database, id).deletedAt, null)
  assert.equal(getPostLlmState(database, post.id).reprocessRequested, true)
  const secondFile = await image(t, database, urls[1])
  assert.equal(
    (
      await extractSchedulePost(database, post, {
        ...options,
        inputMode: "text",
      })
    ).status,
    "success",
  )
  assert.equal(calls, 2)
  assert.equal(inputImages, 2)
  assert.equal(getPostLlmState(database, post.id).reprocessRequested, false)
  fs.rmSync(secondFile)
  requestPostLlmReprocess(database, post.id)
  assert.equal(
    (
      await extractSchedulePost(database, post, {
        ...options,
        inputMode: "text",
      })
    ).reason,
    "media_pending",
  )
  assert.equal(calls, 2)
  assert.equal(getEvent(database, id).deletedAt, null)
})

test("complete post media updates reconcile decrease, replacement, order and empty; omitted/failed reads retain snapshots", (t) => {
  const database = fixture(t),
    urls = [
      "https://pbs.twimg.com/media/a.png",
      "https://pbs.twimg.com/media/b.png",
      "https://pbs.twimg.com/media/c.png",
    ]
  const post = {
    id: "post",
    source: "x",
    text: "synthetic",
    published_at: now.toISOString(),
    url: "https://x.com/example/status/post",
  }
  const check = (expected) => {
    const media = getDashboard(database, { now }).posts[0].media
    assert.deepEqual(
      media.map((item) => item.sourceUrl),
      expected,
    )
    assert.equal(
      listMediaLinks(database, { ownerType: "post", ownerId: post.id }).length,
      expected.length,
    )
  }
  for (const selected of [
    [urls[0], urls[1]],
    [urls[1]],
    [urls[2], urls[0]],
    [urls[0], urls[2]],
    [],
  ]) {
    upsertPosts(database, [
      { ...post, media_urls: selected, media_url: selected[0] ?? null },
    ])
    check(selected)
    upsertPosts(database, [
      { ...post, text: "updated text without media fields" },
    ])
    check(selected)
    upsertPosts(database, [])
    check(selected)
  }
  upsertPosts(database, [{ ...post, media_urls: urls }])
  check(urls)
  assert.throws(() =>
    upsertPosts(database, [
      { ...post, media_urls: [] },
      { ...post, id: null, published_at: { invalid: true } },
    ]),
  )
  check(urls) // Domain write and links roll back together.
})

for (const [text, published, start, end, basis] of [
  [
    "来週の予定表 10/12–10/18",
    "2026-10-11T12:00:00Z",
    "2026-10-12",
    "2026-10-18",
    "explicit",
  ],
  [
    "来週の予定表",
    "2026-10-11T14:59:00Z",
    "2026-10-12",
    "2026-10-18",
    "relative",
  ],
  [
    "今週の予定表",
    "2026-10-11T14:59:00Z",
    "2026-10-05",
    "2026-10-11",
    "relative",
  ],
  [
    "next week schedule",
    "2026-12-27T00:00:00Z",
    "2026-12-28",
    "2027-01-03",
    "relative",
  ],
  [
    "予定表 12/28–1/3",
    "2026-12-27T00:00:00Z",
    "2026-12-28",
    "2027-01-03",
    "explicit",
  ],
  [
    "予定表 2027年1月4日〜10日",
    "2026-12-30T00:00:00Z",
    "2027-01-04",
    "2027-01-10",
    "explicit",
  ],
  [
    "予定表 10/12–10/25",
    "2026-10-11T00:00:00Z",
    "2026-10-12",
    "2026-10-25",
    "explicit",
  ],
  ["新しいスケジュール", "2026-10-11T00:00:00Z", null, null, "unknown"],
  [
    "10/11投稿 来週の予定表 10/12(月)〜10/18(日)",
    "2026-10-11T00:00:00Z",
    "2026-10-12",
    "2026-10-18",
    "explicit",
  ],
  [
    "予定表 10/12、10/18 のお知らせ",
    "2026-10-11T00:00:00Z",
    null,
    null,
    "unknown",
  ],
  [
    "予定表 10/12–10/18 と 10/19–10/25",
    "2026-10-11T00:00:00Z",
    null,
    null,
    "unknown",
  ],
  ["予定表 2/30–3/2", "2026-02-28T00:00:00Z", null, null, "unknown"],
])
  test(`schedule period follows source wording: ${text}`, () => {
    const period = schedulePeriod(text, published)
    assert.equal(period.period_start, start)
    assert.equal(period.period_end, end)
    assert.equal(period.period_basis, basis)
  })

test("manual period correction survives automatic registration, preserves review, and covers both weeks", (t) => {
  const database = fixture(t),
    post = {
      id: "period-post",
      text: "来週の予定表 10/12–10/25",
      published_at: "2026-10-11T00:00:00Z",
      url: "https://x.com/example/status/period",
      media_url: "https://pbs.twimg.com/media/period.png",
    }
  upsertPosts(database, [post])
  upsertScheduleAssetsFromPosts(database, [post], "example")
  const id = "schedule-2026-10-12"
  updateScheduleAssetManualReview(database, id, {
    status: "schedule",
    reason: "board approved",
  })
  updateScheduleAssetPeriod(database, id, {
    start: "2026-10-19",
    end: "2026-11-01",
    reason: "operator checked dates",
  })
  upsertScheduleAssetsFromPosts(database, [post], "example")
  const review = getScheduleAssetReview(database, id)
  assert.equal(review.periodBasis, "manual")
  assert.equal(review.periodStart, "2026-10-19")
  assert.equal(review.manualReason, "board approved")
  assert.equal(
    getCalendarPage(database, { from: "2026-10-12", to: "2026-10-18" })
      .scheduleImages.length,
    0,
  )
  for (const from of ["2026-10-19", "2026-10-26"])
    assert.equal(
      getCalendarPage(database, {
        from,
        to: new Date(Date.parse(from) + 6 * 86400000)
          .toISOString()
          .slice(0, 10),
      }).scheduleImages[0].id,
      id,
    )
})

test("period correction API requires authentication and validates dates without altering manual review", async (t) => {
  const database = fixture(t)
  upsertAssets(database, [
    {
      id: "api-period",
      kind: "schedule",
      url: "https://pbs.twimg.com/media/api-period.png",
      week_start: "2026-10-05",
      source_url: "https://x.com/example/status/api-period",
    },
  ])
  updateScheduleAssetManualReview(database, "api-period", {
    status: "not_schedule",
    reason: "operator rejection",
  })
  const servers = []
  const originFor = async (appMode) => {
    const server = createApp({
      database,
      adminMode: appMode,
      adminPassword: "synthetic-test-password",
      mcpControlToken: "",
      mcpEnabled: false,
    }).listen(0, "127.0.0.1")
    servers.push(server)
    await new Promise<void>((resolve) => server.once("listening", resolve))
    return `http://127.0.0.1:${listeningPort(server)}`
  }
  t.after(async () => {
    for (const server of servers)
      await new Promise<void>((resolve) => server.close(() => resolve()))
  })
  const production = await originFor("production"),
    development = await originFor("development")
  const change = (origin, body) =>
    fetch(`${origin}/api/admin/schedule-assets/api-period/period`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(body),
    })
  const valid = {
    start: "2026-10-12",
    end: "2026-10-18",
    reason: "synthetic date correction",
  }
  assert.equal((await change(production, valid)).status, 401)
  for (const invalid of [
    { ...valid, start: "2026-02-30" },
    { ...valid, end: "2026-10-01" },
    { ...valid, reason: " " },
  ])
    assert.equal((await change(development, invalid)).status, 400)
  assert.equal(
    getScheduleAssetReview(database, "api-period").periodStart,
    "2026-10-05",
  )
  const response = await change(development, valid)
  assert.equal(response.status, 200)
  const { asset } = await response.json()
  assert.equal(asset.periodStart, valid.start)
  assert.equal(asset.periodEnd, valid.end)
  assert.equal(asset.manualStatus, "not_schedule")
  assert.equal(asset.manualReason, "operator rejection")
  assert.equal(asset.sourceUrl, "https://x.com/example/status/api-period")
})

test("additive period migration preserves legacy images and review metadata", (t) => {
  const directory = fs.mkdtempSync(
      path.join(os.tmpdir(), "kano-period-migration-"),
    ),
    filename = path.join(directory, "snapshot.sqlite")
  const legacy = new Database(filename)
  legacy.exec(
    "CREATE TABLE assets (id TEXT PRIMARY KEY, kind TEXT, url TEXT, source_url TEXT, alt TEXT, week_start TEXT, source_account TEXT, updated_at TEXT, raw_json TEXT); INSERT INTO assets(id,kind,url,week_start) VALUES ('old','schedule','https://example.invalid/old.png','2026-10-05')",
  )
  legacy.exec(`CREATE TABLE schedule_asset_reviews (
    asset_id TEXT PRIMARY KEY, llm_status TEXT, llm_confidence REAL, llm_reason TEXT,
    llm_evidence TEXT, llm_model TEXT, llm_checked_at TEXT, manual_status TEXT,
    manual_reason TEXT, manual_checked_at TEXT, updated_at TEXT);
    INSERT INTO schedule_asset_reviews(asset_id,llm_status,llm_reason,manual_status,manual_reason,manual_checked_at,updated_at)
    VALUES ('old','schedule','synthetic model review','not_schedule','legacy manual rejection','2026-10-06T00:00:00Z','2026-10-06T00:00:00Z')`)
  legacy.close()
  const database = initializeDatabase({ seed: false, filename })
  t.after(() => {
    database.close()
    fs.rmSync(directory, { recursive: true, force: true })
  })
  const review = getScheduleAssetReview(database, "old")
  assert.equal(review.periodStart, "2026-10-05")
  assert.equal(review.periodEnd, "2026-10-11")
  assert.equal(review.periodBasis, "legacy")
  assert.equal(review.url, "https://example.invalid/old.png")
  assert.equal(review.manualStatus, "not_schedule")
  assert.equal(review.manualReason, "legacy manual rejection")
  assert.equal(review.manualCheckedAt, "2026-10-06T00:00:00Z")
  assert.equal(review.llmReason, "synthetic model review")
  assert.equal(review.approved, false)
})

test("dashboard stays bounded with 10000 historical events/posts/media; calendar pagination retains every event", async (t) => {
  const database = fixture(t)
  const records = Array.from({ length: 10000 }, (_, index) => ({
    id: `old-${index}`,
    source: "x",
    title: "Synthetic historical activity",
    starts_on: "2020-01-06",
    url: "https://example.invalid/activity",
  }))
  upsertEvents(database, records)
  upsertPosts(
    database,
    records.map((record) => ({
      ...record,
      text: record.title,
      published_at: "2020-01-06T00:00:00Z",
      media_urls: [`https://pbs.twimg.com/media/history-${record.id}.png`],
    })),
  )
  upsertEvents(database, [
    {
      id: "next",
      source: "x",
      title: "Next activity",
      starts_on: "2026-12-01",
    },
  ])
  upsertVideos(
    database,
    Array.from({ length: 100 }, (_, index) => ({
      id: `video-${index}`,
      source: "youtube",
      title: "Synthetic",
      published_at: "2020-01-01T00:00:00Z",
      scheduled_at: new Date(
        Date.parse("2099-01-01T00:00:00Z") + index * 86400000,
      ).toISOString(),
      is_upcoming: true,
      url: `https://www.youtube.com/watch?v=video-${index}`,
    })),
  )
  upsertVideos(database, [
    {
      id: "featured-old",
      title: "Featured",
      published_at: "2010-01-01T00:00:00Z",
      url: "https://www.youtube.com/watch?v=featured-old",
    },
  ])
  setFeaturedVideoId(database, "featured-old")
  const snapshot = getDashboard(database, { now, days: 3 })
  assert.ok(Buffer.byteLength(JSON.stringify(snapshot)) < 100000)
  assert.equal(snapshot.events.length, 1)
  assert.equal(snapshot.summary.nextEvent.id, "next")
  assert.equal(snapshot.summary.counts.events, 10001)
  assert.equal(snapshot.summary.latestPost.id.startsWith("old-"), true)
  assert.equal(snapshot.posts.length, 0)
  assert.equal(
    snapshot.videos.some((video) => video.id === "featured-old"),
    true,
  )
  assert.equal(snapshot.summary.nextStream.id, "video-0")
  for (let index = 0; index < 40; index++)
    upsertEvents(database, [
      {
        id: `youtube-video-${index}`,
        source: "youtube",
        source_item_id: `video-${index}`,
        source_key: "reservation",
        title: "Cancelled synthetic reservation",
        starts_on: "2099-01-01",
        cancellation_status: "manual_confirmed",
        cancellation_reason: "synthetic cancellation",
      },
    ])
  assert.equal(
    getDashboard(database, { now }).summary.nextStream.id,
    "video-40",
  )
  const ids = new Set()
  for (let page = 1; page <= 100; page++) {
    const result = getCalendarPage(database, {
      from: "2020-01-06",
      to: "2020-01-12",
      page,
    })
    assert.equal(result.items.length, 100)
    assert.equal(result.dayCounts["2020-01-06"], 10000)
    for (const event of result.items) ids.add(event.id)
  }
  assert.equal(ids.size, 10000)
  const current = getCalendarPage(database, {
    from: "2026-10-05",
    to: "2026-10-11",
  })
  assert.deepEqual(current.adjacent, {
    previous: "2020-01-06",
    next: "2026-12-01",
  })
  const server = createApp({
    database,
    mcpControlToken: "",
    mcpEnabled: false,
  }).listen(0, "127.0.0.1")
  await new Promise<void>((resolve) => server.once("listening", resolve))
  t.after(() => new Promise<void>((resolve) => server.close(() => resolve())))
  const origin = `http://127.0.0.1:${listeningPort(server)}`
  const response = await fetch(
    `${origin}/api/calendar?from=2020-01-06&to=2020-01-12&page=100`,
  )
  const dashboardResponse = await fetch(`${origin}/api/dashboard?days=3`)
  assert.equal(dashboardResponse.status, 200)
  assert.ok(Buffer.byteLength(await dashboardResponse.text()) < 100000)
  const payload = await response.json()
  assert.equal(response.status, 200)
  assert.equal(payload.events.length, 100)
  for (const event of payload.events) {
    assert.ok(!("sourceItemId" in event))
    assert.ok(!("extractionId" in event))
    assert.ok(!("raw_json" in event))
  }
  assert.equal(
    (await fetch(`${origin}/api/calendar?from=2020-01-01&to=2026-12-31`))
      .status,
    400,
  )
})

test("bounded video candidates skip ended streams and retain long active streams", (t) => {
  const database = fixture(t)
  const future = Array.from({ length: 60 }, (_, index) => ({
    id: `future-${index}`,
    title: "Synthetic future stream",
    scheduled_at: new Date(
      now.getTime() + (index + 1) * 86400000,
    ).toISOString(),
    is_upcoming: true,
    url: `https://www.youtube.com/watch?v=future-${index}`,
  }))
  const ended = Array.from({ length: 40 }, (_, index) => ({
    id: `ended-${index}`,
    title: "Synthetic ended stream",
    scheduled_at: "2026-10-08T23:00:00Z",
    is_upcoming: true,
    url: `https://www.youtube.com/watch?v=ended-${index}`,
  }))
  upsertVideos(database, [...future, ...ended])
  upsertEvents(
    database,
    ended.map((video) => ({
      id: `youtube-${video.id}`,
      source: "youtube",
      source_item_id: video.id,
      title: video.title,
      starts_on: "2026-10-09",
      starts_at: video.scheduled_at,
      ends_at: "2026-10-08T23:45:00Z",
      event_type: "stream",
    })),
  )
  assert.equal(
    deriveDashboardAt(getDashboard(database, { now }) as any, now.getTime())
      .summary.nextStream.id,
    "future-0",
  )
  upsertVideos(database, [
    {
      id: "long-active",
      title: "Synthetic active stream",
      scheduled_at: "2026-10-08T20:00:00Z",
      is_upcoming: true,
      url: "https://www.youtube.com/watch?v=long-active",
    },
  ])
  upsertEvents(database, [
    {
      id: "youtube-long-active",
      source: "youtube",
      source_item_id: "long-active",
      title: "Synthetic active stream",
      starts_on: "2026-10-09",
      starts_at: "2026-10-08T20:00:00Z",
      ends_at: "2026-10-09T02:00:00Z",
      event_type: "stream",
    },
  ])
  assert.equal(
    deriveDashboardAt(getDashboard(database, { now }) as any, now.getTime())
      .summary.nextStream.id,
    "long-active",
  )
})

test("bounded focus retains a video end even when its event falls beyond week and live previews", (t) => {
  const database = fixture(t)
  const rows = Array.from({ length: 140 }, (_, index) => ({
    id: `focus-noise-${index}`,
    source: "x",
    title: "Synthetic focus noise",
    starts_on: "2026-10-08",
    starts_at: "2026-10-08T00:00:00Z",
    ends_at: "2026-10-10T00:00:00Z",
    event_type: "stream",
  }))
  upsertEvents(database, rows)
  upsertEvents(
    database,
    Array.from({ length: 40 }, (_, index) => ({
      id: `recent-noise-${index}`,
      source: "x",
      title: "Recent noise",
      starts_on: "2026-10-09",
      starts_at: "2026-10-08T23:30:00Z",
      ends_at: "2026-10-10T00:00:00Z",
      event_type: "stream",
    })),
  )
  upsertEvents(database, [
    {
      id: "linked-end",
      source: "x",
      title: "Ended linked stream",
      starts_on: "2026-10-09",
      starts_at: "2026-10-08T23:00:00Z",
      ends_at: "2026-10-08T23:45:00Z",
      url: "https://youtu.be/synth000001",
      event_type: "stream",
    },
  ])
  upsertVideos(database, [
    {
      id: "synth000001",
      title: "Ended linked stream",
      scheduled_at: "2026-10-08T23:00:00Z",
      is_upcoming: true,
      url: "https://www.youtube.com/watch?v=synth000001",
    },
  ])
  const snapshot = getDashboard(database, { now })
  assert.ok(snapshot.events.some((event) => event.id === "linked-end"))
  const derived = deriveDashboardAt(snapshot as any, now.getTime())
  assert.equal(derived.videos[0].endsAt, "2026-10-08T23:45:00Z")
  assert.equal(derived.summary.nextStream, null)
  assert.ok(snapshot.events.length < 181)
})

test("first dashboard query uses indexed video associations with 10000 historical events and videos", (t) => {
  const database = fixture(t)
  const records = Array.from({ length: 10000 }, (_, index) => ({
    id: `history-${index}`,
    title: "Synthetic historical stream",
    source: "x",
    starts_on: "2020-01-06",
    starts_at: "2020-01-06T00:00:00Z",
    url: `https://youtu.be/history-${index}`,
  }))
  upsertEvents(database, records)
  upsertVideos(
    database,
    records.map((record) => ({
      ...record,
      published_at: record.starts_at,
      scheduled_at: record.starts_at,
      url: `https://www.youtube.com/watch?v=${record.id}`,
    })),
  )
  setFeaturedVideoId(database, "history-9999")
  const prepare = database.prepare.bind(database)
  const plans: string[] = []
  database.prepare = ((sql) => {
    const statement = prepare(sql)
    if (sql.includes("FROM events e") && sql.includes("UNION SELECT")) {
      const explain = prepare(`EXPLAIN QUERY PLAN ${sql}`)
      const all = statement.all.bind(statement)
      statement.all = (...args) => {
        plans.push(...explain.all(...args).map((row: any) => row.detail))
        return all(...args)
      }
      const get = statement.get.bind(statement)
      statement.get = (...args) => {
        plans.push(...explain.all(...args).map((row: any) => row.detail))
        return get(...args)
      }
    }
    return statement
  }) as typeof database.prepare
  const start = performance.now()
  const snapshot = getDashboard(database, { now })
  t.diagnostic(`First snapshot: ${(performance.now() - start).toFixed(1)} ms`)
  assert.ok(plans.length > 0)
  assert.ok(plans.some((plan) => plan.includes("events_stream_identity_idx")))
  assert.ok(plans.some((plan) => plan.includes("events_source_item_idx")))
  assert.ok(plans.some((plan) => plan.includes("event_sources_identity_idx")))
  assert.ok(
    !plans.some((plan) => /^SCAN (?:e|events|event_sources)\b/u.test(plan)),
  )
  assert.ok(snapshot.videos.some((video) => video.id === "history-9999"))
  assert.equal(snapshot.summary.counts.events, 10000)
  assert.equal(snapshot.summary.nextStream, null)
  assert.ok(Buffer.byteLength(JSON.stringify(snapshot)) < 100000)
})

test("video grace eligibility precedes truncation at the three-hour boundary", (t) => {
  const database = fixture(t)
  upsertVideos(database, [
    ...Array.from({ length: 35 }, (_, index) => ({
      id: `boundary-${index}`,
      title: "Synthetic grace-boundary stream",
      scheduled_at: "2026-10-08T21:00:00Z",
      url: `https://www.youtube.com/watch?v=boundary-${index}`,
    })),
    ...Array.from({ length: 100 }, (_, index) => ({
      id: `upcoming-${index}`,
      title: "Synthetic upcoming stream",
      scheduled_at: new Date(
        now.getTime() + (index + 1) * 86400000,
      ).toISOString(),
      url: `https://www.youtube.com/watch?v=upcoming-${index}`,
    })),
  ])
  for (const offset of [-1, 0, 1]) {
    const instant = new Date(now.getTime() + offset)
    const snapshot = getDashboard(database, { now: instant })
    const derived = deriveDashboardAt(snapshot as any, instant.getTime())
    assert.equal(
      derived.summary.nextStream.id,
      offset < 0 ? "boundary-0" : "upcoming-0",
    )
    assert.ok(snapshot.videos.length <= 61)
  }
})

test("additive association indexes preserve existing locks, tombstones and Featured settings", (t) => {
  const database = fixture(t)
  upsertEvents(database, [
    {
      id: "locked",
      source: "x",
      title: "Synthetic locked stream",
      starts_on: "2026-10-09",
      manual_locked: true,
      provenance: "manual",
      cancellation_status: "llm_suspected",
      cancellation_evidence: "Synthetic review evidence",
    },
    {
      id: "tombstone",
      source: "x",
      title: "Synthetic deleted stream",
      starts_on: "2026-10-09",
      manual_locked: true,
      deleted_at: now.toISOString(),
    },
  ])
  upsertVideos(database, [
    {
      id: "retained-featured",
      title: "Synthetic Featured",
      url: "https://www.youtube.com/watch?v=retained-featured",
    },
  ])
  setFeaturedVideoId(database, "retained-featured")
  const originalEvents = database
    .prepare("SELECT * FROM events ORDER BY id")
    .all()
  const originalSettings = database
    .prepare("SELECT * FROM app_settings ORDER BY key")
    .all()
  database.exec(
    "DROP INDEX events_source_item_idx; DROP INDEX events_stream_identity_idx",
  )
  const directory = fs.mkdtempSync(
    path.join(os.tmpdir(), "kano-index-upgrade-"),
  )
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }))
  const filename = path.join(directory, "snapshot.sqlite")
  fs.writeFileSync(filename, database.serialize())
  for (let startup = 0; startup < 2; startup++) {
    const upgraded = initializeDatabase({ seed: false, filename })
    try {
      assert.deepEqual(
        upgraded.prepare("SELECT * FROM events ORDER BY id").all(),
        originalEvents,
      )
      assert.deepEqual(
        upgraded.prepare("SELECT * FROM app_settings ORDER BY key").all(),
        originalSettings,
      )
      const indexes = upgraded
        .prepare(
          "SELECT name FROM sqlite_master WHERE type='index' AND name IN ('events_source_item_idx', 'events_stream_identity_idx')",
        )
        .all()
      assert.equal(indexes.length, 2)
      const snapshot = getDashboard(upgraded, { now })
      assert.equal(snapshot.meta.featuredVideoId, "retained-featured")
      assert.ok(!snapshot.events.some((event) => event.id === "tombstone"))
    } finally {
      upgraded.close()
    }
  }
})

test("live event eligibility precedes truncation and retains the earliest cross-week focus", (t) => {
  const database = fixture(t)
  const noise = [
    { status: " Cancelled " },
    { cancellation_status: "manual_confirmed" },
    { cancellation_status: "llm_suspected" },
    { ends_at: "2026-10-08T23:45:00Z" },
    { ends_at: now.toISOString() },
    { event_type: "event" },
    { event_type: "release" },
    { event_type: "video" },
    { deleted_at: "2026-10-08T23:45:00Z" },
  ].flatMap((override, kind) =>
    Array.from({ length: 35 }, (_, index) => ({
      id: `ineligible-${kind}-${index}`,
      source: "x",
      title: "Synthetic ineligible focus",
      starts_on: "2026-10-09",
      starts_at: "2026-10-08T23:00:00Z",
      ends_at: "2026-10-09T03:00:00Z",
      event_type: "stream",
      ...override,
    })),
  )
  upsertEvents(database, noise)
  upsertEvents(
    database,
    [
      { status: " Cancelled " },
      { cancellation_status: "manual_confirmed" },
      { cancellation_status: "llm_suspected" },
      { event_type: "event" },
    ].flatMap((override, kind) =>
      Array.from({ length: 35 }, (_, index) => ({
        id: `old-ineligible-${kind}-${index}`,
        source: "x",
        title: "Synthetic old ineligible focus",
        starts_on: "2026-10-03",
        starts_at: "2026-10-03T00:00:00Z",
        ends_at: "2026-10-10T00:00:00Z",
        event_type: "stream",
        ...override,
      })),
    ),
  )
  upsertEvents(
    database,
    Array.from({ length: 40 }, (_, index) => ({
      id: `later-live-${index}`,
      source: "x",
      title: "Synthetic later live stream",
      starts_on: "2026-10-08",
      starts_at: "2026-10-08T00:00:00Z",
      ends_at: "2026-10-10T00:00:00Z",
      event_type: "member",
    })),
  )
  upsertEvents(database, [
    {
      id: "cross-week-live",
      source: "x",
      title: "Synthetic cross-week stream",
      starts_on: "2026-10-04",
      starts_at: "2026-10-04T00:00:00Z",
      ends_at: "2026-10-09T01:00:00Z",
      event_type: "stream",
    },
    ...["20:59:59.999", "21:00:00", "21:00:00.001"].map((time, index) => ({
      id: `grace-${index}`,
      source: "youtube",
      title: "Synthetic grace boundary",
      starts_on: "2026-10-08",
      starts_at: `2026-10-08T${time}Z`,
      event_type: "event",
    })),
  ])
  const snapshot = getDashboard(database, { now })
  const derived = deriveDashboardAt(snapshot as any, now.getTime())
  assert.equal(derived.summary.nextEvent.id, "cross-week-live")
  assert.ok(snapshot.events.length <= 130)
  // Current-week previews still show cancellation evidence, while the focus
  // shortlist includes only the earliest 30 eligible streams before limiting.
  assert.ok(
    snapshot.events.some(
      (event) => event.cancellationStatus === "manual_confirmed",
    ),
  )
  assert.ok(
    !snapshot.events.some((event) => event.id.startsWith("old-ineligible-")),
  )
  const afterEnd = new Date("2026-10-09T01:00:00Z")
  assert.equal(
    deriveDashboardAt(
      getDashboard(database, { now: afterEnd }) as any,
      afterEnd.getTime(),
    ).summary.nextEvent.id,
    "later-live-0",
  )
  const boundaryWeek = getCalendarPage(database, {
    from: "2026-10-05",
    to: "2026-10-11",
    pageSize: 100,
  })
  assert.ok(boundaryWeek.total > 100)
  for (const [id, live] of [
    ["grace-0", false],
    ["grace-1", false],
    ["grace-2", true],
  ]) {
    const event = getEvent(database, id)
    const single = deriveDashboardAt(
      { ...snapshot, events: [event] } as any,
      now.getTime(),
    )
    assert.equal(Boolean(single.summary.nextEvent), live)
  }
})
