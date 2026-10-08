import assert from "node:assert/strict"
import crypto from "node:crypto"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"
import test from "node:test"
import { createApp } from "./app.ts"
import {
  initializeDatabase,
  getDashboard,
  getEvent,
  getLlmProvider,
  getLatestSync,
  getPostLlmState,
  upsertEvents,
  upsertPosts,
  upsertVideos,
  upsertAssets,
  upsertMediaAsset,
  upsertLlmProvider,
  upsertProfileMediaCandidate,
  setLlmRouteProviders,
  setAppSetting,
  startSyncRun,
  finishSyncRun,
  updateManualEvent,
  deleteManualEvent,
  requestPostLlmReprocess,
  getScheduleAssetReview,
} from "./database.ts"
import {
  extractSchedulePost,
  extractPendingSchedules,
  verifyPendingScheduleAssets,
  callOpenAiScheduleExtraction,
  callOpenAiScheduleAssetVerification,
} from "./schedule-extractor.ts"
import { writeMediaFileAtomic, resolveMediaCachePath } from "./media-cache.ts"
import { encryptSecret } from "./secret-store.ts"
import { createSyncJobManager } from "./sync-jobs.ts"
import { runSync } from "../scripts/sync.ts"
import { deriveDashboardAt } from "../src/dashboard/derive-dashboard.ts"
import { eventIsUpcoming, eventStatus } from "../src/dashboard/format.ts"
import { pickSpotlight } from "../src/dashboard/spotlight.ts"

const event = {
  title: "Synthetic stream",
  date: "2099-01-01",
  time: "20:00",
  endTime: null,
  timePrecision: "exact",
  status: "scheduled",
  eventType: "stream",
  url: null,
  confidence: 0.9,
  evidence: "synthetic evidence",
}
const post = {
  id: "regression-post",
  source: "x",
  text: "今週のスケジュール",
  published_at: "2098-12-30T00:00:00Z",
  url: "https://example.invalid/post",
}
const resultResponse = (result) =>
  Response.json({ output_text: JSON.stringify(result) })
function databaseFixture(t) {
  const db = initializeDatabase({ seed: false, filename: ":memory:" })
  t.after(() => db.close())
  return db
}
function providerFixture(t, db, capabilities = ["text", "image"]) {
  const savedEnvironment = { masterKey: process.env.LLM_SECRETS_KEY }
  process.env.LLM_SECRETS_KEY = crypto.randomBytes(32).toString("base64")
  t.after(() => {
    if (savedEnvironment.masterKey == null) delete process.env.LLM_SECRETS_KEY
    else process.env.LLM_SECRETS_KEY = savedEnvironment.masterKey
  })
  upsertLlmProvider(db, {
    id: "regression-provider",
    model: "synthetic-model",
    baseUrl: "https://provider.example.invalid/v1",
    capabilities,
    replaceApiKey: true,
    apiKeyCiphertext: encryptSecret(
      crypto.randomBytes(24).toString("hex"),
      process.env.LLM_SECRETS_KEY,
    ),
  })
  setLlmRouteProviders(db, "schedule_board", ["regression-provider"])
  setLlmRouteProviders(db, "schedule_message", ["regression-provider"])
}
async function appFixture(t, db, options = {}) {
  const server = createApp({ database: db, ...options }).listen(0, "127.0.0.1")
  await new Promise<void>((resolve) => server.once("listening", resolve))
  t.after(async () => {
    await new Promise<void>((resolve, reject) =>
      server.close((error) => (error ? reject(error) : resolve())),
    )
  })
  const address = server.address()
  assert.ok(address && typeof address === "object")
  return `http://127.0.0.1:${address.port}`
}
async function mcpRead(origin, name) {
  const response = await fetch(`${origin}/mcp`, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      accept: "application/json, text/event-stream",
    },
    body: JSON.stringify({
      jsonrpc: "2.0",
      id: 1,
      method: "tools/call",
      params: { name, arguments: {} },
    }),
  })
  assert.equal(response.status, 200)
  return JSON.parse((await response.json()).result.content[0].text)
}
async function syntheticImage(t, db, sourceUrl, marker) {
  const content = Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    Buffer.from(marker),
    crypto.randomBytes(16),
  ])
  const written = await writeMediaFileAtomic({
    content,
    extension: "png",
    source: "x",
  })
  t.after(() =>
    fs.rmSync(resolveMediaCachePath(written.relativePath), { force: true }),
  )
  upsertMediaAsset(db, {
    source: "x",
    sourceUrl,
    status: "ready",
    cachePath: written.relativePath,
    sha256: written.sha256,
    byteSize: written.byteSize,
    mimeType: "image/png",
  })
  return written
}
async function waitJob(manager, id) {
  for (let i = 0; i < 1000; i++) {
    const job = manager.get(id)
    if (["completed", "failed"].includes(job.status)) return job
    await new Promise((resolve) => setTimeout(resolve, 5))
  }
  throw new Error("synthetic job did not finish")
}

test("anonymous HTTP and MCP whitelist health/profile data while admin retains diagnostics", async (t) => {
  const db = databaseFixture(t)
  const authentication = { password: crypto.randomBytes(24).toString("hex") }
  const candidate = upsertProfileMediaCandidate(db, {
    slot: "avatar",
    source: "x",
    sourceUrl: "https://pbs.twimg.com/synthetic-private.jpg",
    sourceRef: "internal-source-reference",
  })
  db.prepare(
    "UPDATE profile_media SET is_active=1, cache_path=?, active_cache_path=?, last_error=? WHERE id=?",
  ).run(
    "x/internal-cache",
    "avatar/internal-cache",
    "raw media failure",
    candidate.id,
  )
  const runId = startSyncRun(db, "manual", {
    jobId: "internal-job",
    triggeredBy: "internal-trigger",
  })
  finishSyncRun(db, runId, {
    status: "partial",
    message: "raw diagnostic message",
    counts: { rawError: "raw upstream failure" },
  })
  const origin = await appFixture(t, db, {
    adminMode: "production",
    adminPassword: authentication.password,
    databaseLabel: "internal-database-path",
  })
  const health = await (await fetch(`${origin}/api/health`)).json()
  assert.deepEqual(Object.keys(health).sort(), [
    "lastSync",
    "now",
    "ok",
    "service",
  ])
  for (const payload of [
    health,
    await (await fetch(`${origin}/api/dashboard`)).json(),
    await mcpRead(origin, "dashboard_read"),
    await mcpRead(origin, "health_read"),
  ]) {
    assert.deepEqual(
      Object.keys(payload.lastSync || payload.meta.lastSync).sort(),
      ["finishedAt", "id", "message", "source", "startedAt", "status"],
    )
    assert.doesNotMatch(
      JSON.stringify(payload),
      /internal-job|internal-trigger|internal-cache|internal-source-reference|raw media failure|raw upstream failure|raw diagnostic message|internal-database-path/,
    )
    if (payload.profileMedia)
      assert.deepEqual(Object.keys(payload.profileMedia.avatar).sort(), [
        "id",
        "isActive",
        "publicUrl",
        "slot",
        "source",
        "sourceUrl",
        "status",
      ])
  }
  assert.equal((await fetch(`${origin}/api/admin/sync/runs`)).status, 401)
  const login = await fetch(`${origin}/api/admin/login`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(authentication),
  })
  assert.equal(login.status, 200)
  const cookie = login.headers.get("set-cookie").split(";", 1)[0]
  const runs = await (
    await fetch(`${origin}/api/admin/sync/runs`, { headers: { cookie } })
  ).json()
  assert.match(JSON.stringify(runs), /internal-job|raw upstream failure/)
  const media = await (
    await fetch(`${origin}/api/admin/profile-media`, { headers: { cookie } })
  ).json()
  assert.match(JSON.stringify(media), /internal-cache/)
})

for (const status of [
  "cancelled",
  "canceled",
  "cancel",
  "取消",
  "已取消",
  "中止",
  "キャンセル",
  " CANCELLED ",
  "cancellation announced",
]) {
  for (const action of ["add", "update"]) {
    test(`model ${action}/${status} produces review evidence without cancelling, retiring or resurrecting schedules`, async (t) => {
      const db = databaseFixture(t)
      const options = {
        apiKey: "not-a-real-api-key",
        force: true,
        baseUrl: "https://provider.example.invalid/v1",
        fetchImpl: async () =>
          resultResponse({
            classification: "schedule",
            action,
            events: [event],
          }),
      }
      upsertPosts(db, [post])
      assert.equal(
        (await extractSchedulePost(db, post, options)).status,
        "success",
      )
      const id = getDashboard(db).events[0].id
      for (const kind of ["automatic", "manual", "confirmed", "deleted"]) {
        if (kind === "manual")
          updateManualEvent(db, id, {
            title: event.title,
            startsOn: event.date,
            startsAt: "2099-01-01T11:00:00Z",
            status: "scheduled",
            cancellationStatus: "none",
          })
        if (kind === "confirmed")
          db.prepare(
            "UPDATE events SET cancellation_status='manual_confirmed' WHERE id=?",
          ).run(id)
        if (kind === "deleted") deleteManualEvent(db, id)
        const before = getEvent(db, id)
        const output = {
          classification: "schedule",
          action,
          events: [
            { ...event, status },
            { ...event, title: "A still scheduled activity", time: "22:00" },
          ],
        }
        assert.equal(
          (
            await extractSchedulePost(db, post, {
              ...options,
              fetchImpl: async () => resultResponse(output),
            })
          ).status,
          "success",
        )
        const after = getEvent(db, id)
        assert.equal(after.status, before.status)
        assert.equal(after.startsAt, before.startsAt)
        assert.equal(after.deletedAt, before.deletedAt)
        assert.equal(after.manualLocked, before.manualLocked)
        assert.equal(
          after.cancellationStatus,
          ["confirmed", "deleted"].includes(kind)
            ? "manual_confirmed"
            : "llm_suspected",
        )
        assert.equal(
          getDashboard(db).events.some((item) =>
            /cancel|取消|中止|キャンセル/i.test(item.status || ""),
          ),
          false,
        )
      }
      const newDb = databaseFixture(t)
      assert.equal(
        (
          await extractSchedulePost(newDb, post, {
            ...options,
            fetchImpl: async () =>
              resultResponse({
                classification: "schedule",
                action,
                events: [{ ...event, status }],
              }),
          })
        ).status,
        "success",
      )
      assert.equal(getDashboard(newDb).events.length, 0)
    })
  }
}

for (const protocol of [
  "openai-responses",
  "openai-chat-completions",
  "anthropic-messages",
]) {
  test(`${protocol} uses the proper token field for connection, extraction and image verification`, async (t) => {
    const db = databaseFixture(t)
    providerFixture(t, db)
    upsertLlmProvider(db, {
      ...getLlmProvider(db, "regression-provider"),
      protocol,
    })
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "kano-protocol-"))
    t.after(() => {
      assert.ok(
        path.resolve(dir).startsWith(`${path.resolve(os.tmpdir())}${path.sep}`),
      )
      fs.rmSync(dir, { recursive: true, force: true })
    })
    const filePath = path.join(dir, "synthetic.png")
    fs.writeFileSync(
      filePath,
      Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    )
    const image = { filePath, mimeType: "image/png" }
    const expectedField = {
      "openai-responses": "max_output_tokens",
      "openai-chat-completions": "max_completion_tokens",
      "anthropic-messages": "max_tokens",
    }[protocol]
    const limits = []
    const fetchImpl = async (_url, options) => {
      const body = JSON.parse(options.body)
      limits.push(body[expectedField])
      for (const field of [
        "max_output_tokens",
        "max_completion_tokens",
        "max_tokens",
      ])
        assert.equal(Object.hasOwn(body, field), field === expectedField)
      const result =
        limits.length === 3
          ? {
              classification: "schedule",
              confidence: 0.9,
              reason: "synthetic",
              evidence: "synthetic",
            }
          : {
              classification: "not_schedule",
              events: [],
              confidence: 0.9,
              reason: "synthetic",
              evidence: "synthetic",
              action: "none",
            }
      return protocol === "anthropic-messages"
        ? Response.json({
            content: [
              { type: "tool_use", name: body.tools?.[0]?.name, input: result },
            ],
          })
        : protocol === "openai-chat-completions"
          ? Response.json({
              choices: [{ message: { content: JSON.stringify(result) } }],
            })
          : resultResponse(result)
    }
    const origin = await appFixture(t, db, { adminFetchImpl: fetchImpl })
    const connection = await fetch(
      `${origin}/api/admin/providers/regression-provider/test`,
      { method: "POST" },
    )
    assert.equal(connection.status, 200)
    const options = {
      apiKey: "not-a-real-api-key",
      protocol,
      model: "synthetic-model",
      fetchImpl,
      baseUrl: "https://provider.example.invalid/v1",
      endpoint: undefined,
    }
    await callOpenAiScheduleExtraction(post, [image], options)
    await callOpenAiScheduleAssetVerification(
      { sourceUrl: post.url },
      image,
      options,
    )
    assert.deepEqual(limits, [1, 4000, 800])
  })
}

test("success caches do not consume scan budget, failures rotate, changed inputs and manual requests are processed", async (t) => {
  const db = databaseFixture(t)
  setAppSetting(db, "schedule_keyword_enabled", "0")
  setAppSetting(db, "schedule_vision_enabled", "0")
  const posts = Array.from({ length: 21 }, (_, i) => ({
    ...post,
    id: `queue-${i}`,
    text: `明日配信 ${i}`,
    published_at: new Date(Date.UTC(2098, 11, 30, 0, i)).toISOString(),
  }))
  upsertPosts(db, posts)
  let calls = 0
  const options = {
    apiKey: "not-a-real-api-key",
    baseUrl: "https://provider.example.invalid/v1",
    fetchImpl: async () => {
      calls++
      return resultResponse({ classification: "not_schedule", events: [] })
    },
  }
  assert.equal((await extractPendingSchedules(db, options)).attempted, 20)
  assert.equal((await extractPendingSchedules(db, options)).attempted, 1)
  assert.equal(calls, 21)
  assert.equal((await extractPendingSchedules(db, options)).attempted, 0)
  upsertPosts(db, [{ ...posts[0], text: "明日出演 changed" }])
  assert.equal(
    (await extractPendingSchedules(db, { ...options, limit: 1 })).attempted,
    1,
  )
  assert.equal(calls, 22)
  assert.equal(
    (
      await extractPendingSchedules(db, {
        ...options,
        model: "new-model",
        limit: 1,
      })
    ).attempted,
    1,
  )
  requestPostLlmReprocess(db, posts[5].id, "schedule_message")
  assert.equal(
    (await extractPendingSchedules(db, { ...options, limit: 1 })).attempted,
    1,
  )
  assert.equal(getPostLlmState(db, posts[5].id).reprocessRequested, false)
  const failures = databaseFixture(t)
  setAppSetting(failures, "schedule_keyword_enabled", "0")
  setAppSetting(failures, "schedule_vision_enabled", "0")
  upsertPosts(failures, posts.slice(0, 3))
  for (let i = 0; i < 3; i++)
    await extractPendingSchedules(failures, {
      ...options,
      limit: 1,
      fetchImpl: async () => new Response(null, { status: 503 }),
    })
  assert.equal(
    posts
      .slice(0, 3)
      .filter((item) => getPostLlmState(failures, item.id)?.status === "failed")
      .length,
    3,
  )
})

test("image scan advances beyond reviewed rows and retries changed media/config while failures rotate", async (t) => {
  const db = databaseFixture(t)
  providerFixture(t, db, ["image"])
  const assets = []
  for (let i = 0; i < 22; i++) {
    const url = `https://pbs.twimg.com/media/regression-asset-${i}.png`
    const sourcePost = {
      ...post,
      id: `asset-post-${i}`,
      url: `https://example.invalid/asset-post-${i}`,
    }
    upsertPosts(db, [sourcePost])
    await syntheticImage(t, db, url, `regression-assets-${i}`)
    assets.push({
      id: `asset-${i}`,
      kind: "schedule",
      url,
      source_url: sourcePost.url,
      week_start: "2099-01-01",
    })
  }
  upsertAssets(db, assets)
  let calls = 0
  const fetchImpl = async () => {
    calls++
    return resultResponse({
      classification: "schedule",
      confidence: 0.9,
      evidence: "synthetic board",
      reason: "synthetic",
    })
  }
  assert.equal(
    (await verifyPendingScheduleAssets(db, { fetchImpl, limit: 20 })).attempted,
    20,
  )
  assert.equal(
    (await verifyPendingScheduleAssets(db, { fetchImpl, limit: 20 })).attempted,
    2,
  )
  assert.equal(calls, 22)
  assert.equal(
    (await verifyPendingScheduleAssets(db, { fetchImpl })).attempted,
    0,
  )
  await syntheticImage(t, db, assets[0].url, "regression-assets-new-content")
  assert.equal(
    (await verifyPendingScheduleAssets(db, { fetchImpl })).attempted,
    1,
  )
  upsertLlmProvider(db, {
    ...getLlmProvider(db, "regression-provider"),
    model: "changed-image-model",
  })
  for (let i = 0; i < 3; i++)
    await verifyPendingScheduleAssets(db, {
      limit: 1,
      fetchImpl: async () => new Response(null, { status: 503 }),
    })
  assert.equal(
    assets.filter(
      (asset) => getScheduleAssetReview(db, asset.id).llmStatus === "failed",
    ).length,
    3,
  )
})

test("image-only failure reaches workflow progress, sync persistence and standalone job outcome", async (t) => {
  const db = databaseFixture(t)
  providerFixture(t, db, ["image"])
  const sourceUrl = "https://pbs.twimg.com/media/regression-image-only.png"
  upsertPosts(db, [post])
  await syntheticImage(t, db, sourceUrl, "regression-image-only")
  upsertAssets(db, [
    {
      id: "image-only",
      kind: "schedule",
      url: sourceUrl,
      source_url: post.url,
    },
  ])
  const originalFetch = globalThis.fetch
  t.after(() => {
    globalThis.fetch = originalFetch
  })
  globalThis.fetch = async () => new Response(null, { status: 503 })
  const progress = []
  const result = await runSync({
    database: db,
    steps: ["schedule"],
    closeDatabase: false,
    setExitCode: false,
    onStep: (_step, status) => progress.push(status),
  })
  assert.equal(result.results.schedules.failed, 0)
  assert.equal(result.results.schedules.assetVerification.failed, 1)
  assert.equal(result.status, "failed")
  assert.deepEqual(progress, ["running", "failed"])
  assert.equal(getLatestSync(db).status, "failed")
  const manager = createSyncJobManager({
    database: db,
    scanImpl: async () => result.results.schedules,
  })
  const job = await waitJob(manager, manager.start("scan").job.id)
  assert.equal(job.status, "failed")
  assert.equal(job.run.status, "failed")
  const partialManager = createSyncJobManager({
    database: db,
    scanImpl: async () => ({
      success: 1,
      failed: 0,
      assetVerification: { failed: 1 },
    }),
  })
  const partial = await waitJob(
    partialManager,
    partialManager.start("scan").job.id,
  )
  assert.equal(partial.run.status, "partial")
})

test("the unchanged snapshot advances focus, videos and date-only events at Japan time boundaries", (t) => {
  const db = databaseFixture(t)
  upsertEvents(db, [
    {
      id: "first",
      source: "manual",
      title: "First activity",
      starts_on: "2099-01-01",
      starts_at: "2099-01-01T10:00:00Z",
      event_type: "event",
    },
    {
      id: "second",
      source: "manual",
      title: "Second activity",
      starts_on: "2099-01-01",
      starts_at: "2099-01-01T12:00:00Z",
      event_type: "event",
    },
    {
      id: "date-only",
      source: "manual",
      title: "Date only",
      starts_on: "2099-01-01",
      time_precision: "unknown",
    },
    {
      id: "tomorrow",
      source: "manual",
      title: "Tomorrow",
      starts_on: "2099-01-02",
      time_precision: "unknown",
    },
  ])
  upsertVideos(db, [
    {
      id: "video",
      source: "youtube",
      title: "Stream",
      scheduled_at: "2099-01-01T06:00:00Z",
      is_upcoming: true,
      url: "https://example.invalid/video",
    },
  ])
  const snapshot = getDashboard(db, {
    now: new Date("2099-01-01T05:00:00Z"),
  }) as any
  const before = deriveDashboardAt(snapshot, Date.parse("2099-01-01T05:30:00Z"))
  assert.equal(before.summary.nextStream.id, "video")
  const first = deriveDashboardAt(snapshot, Date.parse("2099-01-01T09:30:00Z"))
  assert.equal(
    pickSpotlight(first.summary, Date.parse("2099-01-01T09:30:00Z")).event.id,
    "first",
  )
  assert.equal(first.videos[0].isUpcoming, false)
  const second = deriveDashboardAt(snapshot, Date.parse("2099-01-01T10:01:00Z"))
  assert.equal(
    pickSpotlight(second.summary, Date.parse("2099-01-01T10:01:00Z")).event.id,
    "second",
  )
  const midnight = Date.parse("2099-01-01T15:00:00Z")
  const after = deriveDashboardAt(snapshot, midnight)
  const dayOnly = after.events.find((item) => item.id === "date-only")
  assert.equal(dayOnly.startsAt, null)
  assert.equal(dayOnly.isUpcoming, false)
  assert.equal(eventIsUpcoming(dayOnly, midnight), false)
  assert.equal(eventStatus(dayOnly, midnight), "pending")
  assert.equal(after.summary.nextEvent.id, "tomorrow")
  assert.equal(pickSpotlight(after.summary, midnight).dateOnly, "2099-01-02")
  assert.equal(
    snapshot.events.find((item) => item.id === "date-only").isUpcoming,
    true,
  )
  const live = deriveDashboardAt(snapshot, Date.parse("2099-01-01T08:59:00Z"))
  assert.equal(live.summary.nextStream.id, "video")
})

for (const [linkForm, eventUrl, videoUrl] of [
  [
    "ordinary URL",
    "https://example.invalid/linked-stream",
    "https://example.invalid/linked-stream",
  ],
  [
    "watch URL",
    "https://www.youtube.com/watch?v=synth000001",
    "https://www.youtube.com/watch?v=synth000001",
  ],
  [
    "short URL",
    "https://youtu.be/synth000001",
    "https://www.youtube.com/watch?v=synth000001",
  ],
  [
    "live URL",
    "https://www.youtube.com/live/synth000001",
    "https://www.youtube.com/watch?v=synth000001",
  ],
  [
    "share query",
    "https://www.youtube.com/watch?v=synth000001&feature=share",
    "https://www.youtube.com/watch?v=synth000001",
  ],
  [
    "shorts URL",
    "https://www.youtube.com/shorts/synth000001",
    "https://www.youtube.com/watch?v=synth000001",
  ],
  [
    "mobile watch URL",
    "https://m.youtube.com/watch?feature=share&v=synth000001",
    "https://www.youtube.com/watch?v=synth000001",
  ],
  [
    "short video URL",
    "https://www.youtube.com/watch?v=synth000001",
    "https://youtu.be/synth000001?feature=share",
  ],
])
  for (const explicitEnd of [true, false])
    test(`snapshot-to-spotlight ${linkForm} respects ${explicitEnd ? "the linked stream end" : "the three-hour live grace boundary"}`, async (t) => {
      const db = databaseFixture(t)
      upsertEvents(db, [
        {
          id: "linked-stream",
          source: "manual",
          title: "Linked stream",
          starts_on: "2099-01-01",
          starts_at: "2099-01-01T09:00:00Z",
          ends_at: explicitEnd ? "2099-01-01T09:30:00Z" : null,
          event_type: "stream",
          url: eventUrl,
        },
        {
          id: "later",
          source: "manual",
          title: "Later activity",
          starts_on: "2099-01-01",
          starts_at: explicitEnd
            ? "2099-01-01T10:00:00Z"
            : "2099-01-01T13:00:00Z",
          event_type: "event",
          url: "https://example.invalid/later",
        },
      ])
      upsertVideos(db, [
        {
          id: "linked-video",
          source: "youtube",
          title: "Linked stream",
          scheduled_at: "2099-01-01T09:00:00Z",
          is_upcoming: true,
          url: videoUrl,
        },
      ])
      const origin = await appFixture(t, db)
      const snapshot = await (await fetch(`${origin}/api/dashboard`)).json()
      assert.ok(snapshot.events.every((event) => !("sourceItemId" in event)))
      const original = structuredClone(snapshot)
      const boundary = Date.parse(
        explicitEnd ? "2099-01-01T09:30:00Z" : "2099-01-01T12:00:00Z",
      )
      for (const now of [
        Date.parse("2099-01-01T08:59:00Z"),
        boundary - 1,
        boundary,
        boundary + 1,
        boundary + 15 * 60_000,
      ]) {
        const derived = deriveDashboardAt(snapshot, now)
        const spotlight = pickSpotlight(derived.summary, now)
        assert.equal(derived.meta.revision, snapshot.meta.revision)
        if (now < boundary) {
          assert.equal(derived.summary.nextEvent.id, "linked-stream")
          assert.equal(derived.summary.nextStream.id, "linked-video")
          assert.equal(spotlight.video.id, "linked-video")
        } else {
          assert.equal(derived.summary.nextEvent.id, "later")
          assert.equal(derived.summary.nextStream, null)
          assert.equal(spotlight.event.id, "later")
        }
      }
      // A summary derived just before the boundary must also expire at pick time.
      const before = deriveDashboardAt(snapshot, boundary - 1)
      assert.equal(pickSpotlight(before.summary, boundary).mode, "latest")
      assert.deepEqual(snapshot, original)
    })

for (const eventUrl of [
  "https://youtu.be/synth000002",
  "https://www.youtube.com/live/synth000002",
  "https://www.youtube.com/watch?v=synth000002&feature=share",
  "https://notyoutube.example.invalid/watch?v=synth000001",
  "https://youtube.com.example.invalid/watch?v=synth000001",
  "https://example.invalid/linked-stream?feature=share",
])
  test(`snapshot focus does not inherit an unrelated end from ${eventUrl}`, (t) => {
    const db = databaseFixture(t)
    upsertEvents(db, [
      {
        id: "other-stream",
        source: "manual",
        title: "Other stream",
        starts_on: "2099-01-01",
        starts_at: "2099-01-01T09:00:00Z",
        ends_at: "2099-01-01T09:30:00Z",
        event_type: "stream",
        url: eventUrl,
      },
    ])
    upsertVideos(db, [
      {
        id: "linked-video",
        source: "youtube",
        title: "Linked stream",
        scheduled_at: "2099-01-01T09:00:00Z",
        is_upcoming: true,
        url: eventUrl.startsWith("https://example.invalid/")
          ? "https://example.invalid/linked-stream"
          : "https://www.youtube.com/watch?v=synth000001",
      },
    ])
    const snapshot = getDashboard(db) as any
    const live = deriveDashboardAt(snapshot, Date.parse("2099-01-01T09:45:00Z"))
    assert.equal(live.summary.nextEvent, null)
    assert.equal(live.summary.nextStream.endsAt, null)
    assert.equal(
      pickSpotlight(live.summary, Date.parse("2099-01-01T09:45:00Z")).video.id,
      "linked-video",
    )
    assert.equal(
      deriveDashboardAt(snapshot, Date.parse("2099-01-01T12:00:00Z")).summary
        .nextStream,
      null,
    )
  })

test("post cache invalidates on publication time and cached image changes", async (t) => {
  const db = databaseFixture(t)
  const sourceUrl = "https://pbs.twimg.com/media/regression-post-cache.png"
  const imagePost = { ...post, media_url: sourceUrl }
  upsertPosts(db, [imagePost])
  await syntheticImage(t, db, sourceUrl, "regression-post-cache-old")
  let calls = 0
  const options = {
    apiKey: "not-a-real-api-key",
    baseUrl: "https://provider.example.invalid/v1",
    fetchImpl: async () => {
      calls++
      return resultResponse({ classification: "not_schedule", events: [] })
    },
  }
  assert.equal(
    (await extractSchedulePost(db, imagePost, options)).status,
    "success",
  )
  assert.equal(
    (await extractSchedulePost(db, imagePost, options)).status,
    "cached",
  )
  await syntheticImage(t, db, sourceUrl, "regression-post-cache-new")
  assert.equal(
    (await extractSchedulePost(db, imagePost, options)).status,
    "success",
  )
  const changed = { ...imagePost, published_at: "2098-12-31T00:00:00Z" }
  assert.equal(
    (await extractSchedulePost(db, changed, options)).status,
    "success",
  )
  assert.equal(calls, 3)
})

test("priority failover work still consumes budget when a later provider has a cached result", async (t) => {
  const db = databaseFixture(t)
  providerFixture(t, db)
  setAppSetting(db, "schedule_keyword_enabled", "0")
  setAppSetting(db, "schedule_vision_enabled", "0")
  const posts = [post, { ...post, id: "regression-second-cached" }]
  upsertPosts(db, posts)
  const successFetch = async () =>
    resultResponse({ classification: "not_schedule", events: [] })
  for (const item of posts)
    await extractSchedulePost(db, item, {
      detectionType: "message",
      fetchImpl: successFetch,
    })
  upsertLlmProvider(db, {
    id: "regression-failing-provider",
    model: "synthetic-model",
    baseUrl: "https://failed-provider.example.invalid/v1",
    capabilities: ["text"],
    replaceApiKey: true,
    apiKeyCiphertext: encryptSecret(
      crypto.randomBytes(24).toString("hex"),
      process.env.LLM_SECRETS_KEY,
    ),
  })
  setLlmRouteProviders(db, "schedule_message", [
    "regression-failing-provider",
    "regression-provider",
  ])
  let calls = 0
  const summary = await extractPendingSchedules(db, {
    limit: 1,
    fetchImpl: async () => {
      calls++
      return new Response(null, { status: 503 })
    },
  })
  assert.equal(summary.attempted, 1)
  assert.equal(summary.cached, 1)
  assert.equal(calls, 3)
})

test("legacy image-review migration retains source assets and manual approval", () => {
  const directory = fs.mkdtempSync(
    path.join(os.tmpdir(), "kano-review-upgrade-"),
  )
  const filename = path.join(directory, "fixture.sqlite")
  let db = initializeDatabase({ seed: false, filename })
  try {
    upsertAssets(db, [
      {
        id: "review-upgrade",
        kind: "schedule",
        url: "https://pbs.twimg.com/media/synthetic-review-upgrade.png",
        source_url: post.url,
      },
    ])
    db.prepare(
      "UPDATE schedule_asset_reviews SET manual_status='schedule', manual_reason='synthetic human approval' WHERE asset_id='review-upgrade'",
    ).run()
    db.exec(
      "ALTER TABLE schedule_asset_reviews DROP COLUMN llm_input_fingerprint",
    )
    db.close()
    db = initializeDatabase({ seed: false, filename })
    const review = getScheduleAssetReview(db, "review-upgrade")
    assert.equal(review.manualStatus, "schedule")
    assert.equal(review.manualReason, "synthetic human approval")
    assert.equal(review.llmInputFingerprint, null)
    assert.equal(
      db
        .prepare<unknown[], { count: number }>(
          "SELECT COUNT(*) AS count FROM assets",
        )
        .get().count,
      1,
    )
  } finally {
    db.close()
    assert.ok(
      path
        .resolve(directory)
        .startsWith(`${path.resolve(os.tmpdir())}${path.sep}`),
    )
    fs.rmSync(directory, { recursive: true, force: true })
  }
})
