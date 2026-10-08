import assert from "node:assert/strict"
import fs from "node:fs"
import test from "node:test"

import {
  getDashboard,
  getEvent,
  getPostLlmState,
  getScheduleAssetReview,
  createManualEvent,
  initializeDatabase,
  requestPostLlmReprocess,
  setLlmRouteProviders,
  setAppSetting,
  upsertAssets,
  upsertLlmProvider,
  upsertMediaAsset,
  upsertPosts,
} from "./database.ts"
import { resolveMediaCachePath, writeMediaFileAtomic } from "./media-cache.ts"
import { encryptSecret } from "./secret-store.ts"
import {
  callOpenAiScheduleExtraction,
  extractSchedulePost,
  extractPendingSchedules,
  inputModeForPost,
  providerSupportsInput,
  verifyPendingScheduleAssets,
} from "./schedule-extractor.ts"

test("input modality maps to the required provider capabilities", () => {
  assert.equal(inputModeForPost({ text: "hello" }), "text")
  assert.equal(
    inputModeForPost({
      text: "hello",
      mediaUrls: ["https://example.invalid/a.jpg"],
    }),
    "text_image",
  )
  assert.equal(
    inputModeForPost({
      text: "",
      mediaUrls: ["https://example.invalid/a.jpg"],
    }),
    "image",
  )
  assert.equal(inputModeForPost({ text: "" }), null)
  assert.equal(providerSupportsInput({ capabilities: ["text"] }, "text"), true)
  assert.equal(
    providerSupportsInput({ capabilities: ["text"] }, "image"),
    false,
  )
  assert.equal(
    providerSupportsInput({ capabilities: ["text", "image"] }, "text_image"),
    true,
  )
})

test("OpenAI structured extraction uses cached vision input and caches results", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const sourceUrl = "https://pbs.twimg.com/media/schedule-extractor-test.png"
  const post = {
    id: "schedule-post-test",
    source: "x",
    type: "notice",
    label: "SCHEDULE / 日程",
    text: "9月6日のスケジュールです",
    publishedAt: "2026-08-28T01:00:00.000Z",
    published_at: "2026-08-28T01:00:00.000Z",
    url: "https://x.com/example/status/1234567890123456789",
    media_url: sourceUrl,
    media_urls: [sourceUrl],
  }
  const image = Buffer.from([
    0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x02,
  ])
  let cachedFile
  let calls = 0
  let requestBody
  const validResult = {
    events: [
      {
        title: "配信予定",
        detail: "画像に記載",
        date: "2026-09-06",
        time: null,
        endTime: null,
        timePrecision: "unknown",
        status: "待确认",
        eventType: "stream",
        url: null,
        confidence: 0.92,
        evidence: "9/6 配信予定",
      },
    ],
  }
  const fetchImpl = async (url, options) => {
    calls += 1
    assert.equal(url, "https://api.openai.com/v1/responses")
    assert.equal(options.method, "POST")
    assert.match(options.headers.authorization, /^Bearer /u)
    requestBody = JSON.parse(options.body)
    return new Response(
      JSON.stringify({ output_text: JSON.stringify(validResult) }),
      { status: 200, headers: { "content-type": "application/json" } },
    )
  }

  try {
    upsertPosts(database, [post])
    const written = await writeMediaFileAtomic({
      content: image,
      extension: "png",
    })
    cachedFile = resolveMediaCachePath(written.relativePath)
    upsertMediaAsset(database, {
      source: "x",
      sourceUrl,
      status: "ready",
      cachePath: written.relativePath,
      mimeType: "image/png",
      sha256: written.sha256,
      byteSize: written.byteSize,
    })

    const extracted = await extractSchedulePost(database, post, {
      apiKey: "not-a-real-api-key",
      model: "gpt-4o-mini",
      fetchImpl,
    })
    assert.equal(extracted.status, "success")
    assert.equal(calls, 1)
    assert.equal(requestBody.store, false)
    assert.equal(requestBody.text.format.type, "json_schema")
    assert.equal(requestBody.text.format.strict, true)
    const content = requestBody.input[0].content
    assert.equal(content[0].type, "input_text")
    assert.equal(content[1].type, "input_image")
    assert.match(content[1].image_url, /^data:image\/png;base64,/u)
    const event = getDashboard(database).events[0]
    assert.equal(event.startsOn, "2026-09-06")
    assert.equal(event.startsAt, null)
    assert.equal(event.timePrecision, "unknown")
    assert.equal(event.provenance, "automatic")

    const cached = await extractSchedulePost(database, post, {
      apiKey: "not-a-real-api-key",
      model: "gpt-4o-mini",
      fetchImpl,
    })
    assert.equal(cached.status, "cached")
    assert.equal(calls, 1)

    const changedModel = await extractSchedulePost(database, post, {
      apiKey: "not-a-real-api-key",
      model: "gpt-4.1-mini",
      fetchImpl,
    })
    assert.equal(changedModel.status, "success")
    assert.equal(calls, 2)
    assert.equal(requestBody.model, "gpt-4.1-mini")

    const invalid = await extractSchedulePost(
      database,
      { ...post, text: `${post.text} 更新` },
      {
        apiKey: "not-a-real-api-key",
        model: "gpt-4o-mini",
        fetchImpl: async () =>
          new Response(
            JSON.stringify({
              output_text: JSON.stringify({
                events: [{ ...validResult.events[0], date: "2026-02-30" }],
              }),
            }),
            { status: 200 },
          ),
      },
    )
    assert.equal(invalid.status, "failed")
    assert.match(invalid.error, /invalid event/u)
    assert.equal(getEvent(database, event.id).title, "配信予定")
  } finally {
    database.close()
    if (cachedFile) fs.rmSync(cachedFile, { force: true })
  }
})

test("schedule extraction skips safely when no API key is configured", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  try {
    const result = await extractSchedulePost(
      database,
      {
        id: "no-key-post",
        text: "スケジュール",
        publishedAt: "2026-08-28T01:00:00.000Z",
        url: "https://x.com/example/status/1234567890123456790",
      },
      { apiKey: "" },
    )
    assert.deepEqual(result, { status: "skipped", reason: "missing_api_key" })
  } finally {
    database.close()
  }
})

test("declared images remain pending until a ready cache asset exists", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const sourceUrl = "https://pbs.twimg.com/media/pending-schedule.jpg"
  const post = {
    id: "pending-image-post",
    source: "x",
    text: "",
    publishedAt: "2026-08-28T01:00:00.000Z",
    url: "https://x.com/example/status/pending-image-post",
    media_urls: [sourceUrl],
  }
  let calls = 0
  try {
    upsertPosts(database, [post])
    const result = await extractSchedulePost(database, post, {
      apiKey: "not-a-real-api-key",
      fetchImpl: async () => {
        calls += 1
        throw new Error("should not call a provider")
      },
    })
    assert.deepEqual(result, { status: "skipped", reason: "media_pending" })
    assert.equal(calls, 0)
  } finally {
    database.close()
  }
})

test("uncertain classifications are cached without creating events", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const post = {
    id: "uncertain-post",
    source: "x",
    text: "来週なにかあるかも",
    publishedAt: "2026-08-28T01:00:00.000Z",
    url: "https://x.com/example/status/uncertain-post",
  }
  let calls = 0
  const fetchImpl = async () => {
    calls += 1
    return new Response(
      JSON.stringify({
        output_text: JSON.stringify({
          classification: "uncertain",
          events: [],
          confidence: 0.42,
          evidence: "具体日期なし",
          action: "none",
        }),
      }),
      { status: 200, headers: { "content-type": "application/json" } },
    )
  }
  try {
    const first = await extractSchedulePost(database, post, {
      apiKey: "not-a-real-api-key",
      maxRetries: 0,
      fetchImpl,
    })
    assert.equal(first.status, "uncertain")
    assert.equal(first.classification, "uncertain")
    assert.equal(calls, 1)
    const second = await extractSchedulePost(database, post, {
      apiKey: "not-a-real-api-key",
      maxRetries: 0,
      fetchImpl,
    })
    assert.equal(second.status, "cached")
    assert.equal(second.classification, "uncertain")
    assert.equal(calls, 1)
    assert.equal(
      database
        .prepare<unknown[], Record<string, any>>(
          "SELECT COUNT(*) AS count FROM events",
        )
        .get().count,
      0,
    )
  } finally {
    database.close()
  }
})

test("cancellation extraction keeps the event and records review evidence", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const post = {
    id: "cancel-post",
    source: "x",
    text: "本日の夜配信は中止になりました",
    publishedAt: "2026-09-04T01:00:00.000Z",
    url: "https://x.com/example/status/cancel-post",
  }
  let calls = 0
  const fetchImpl = async () => {
    calls += 1
    return new Response(
      JSON.stringify({
        output_text: JSON.stringify({
          classification: "schedule",
          events: [
            {
              title: "夜配信",
              detail: null,
              date: "2026-09-04",
              time: "21:00",
              endTime: null,
              timePrecision: "exact",
              status: "中止",
              eventType: "stream",
              url: null,
              confidence: 0.9,
              evidence: "中止になりました",
            },
          ],
          confidence: 0.91,
          evidence: "本日の夜配信は中止になりました",
          reason: "本人公告と一致するため取消の可能性",
          action: "cancel",
        }),
      }),
      { status: 200, headers: { "content-type": "application/json" } },
    )
  }
  try {
    upsertPosts(database, [post])
    createManualEvent(database, {
      id: "manual-stream",
      title: "夜配信",
      startsOn: "2026-09-04",
      startsAt: "2026-09-04T12:00:00.000Z",
      eventType: "stream",
      status: "予定",
    })
    const result = await extractSchedulePost(database, post, {
      apiKey: "not-a-real-api-key",
      model: "gpt-4o-mini",
      fetchImpl,
    })
    assert.equal(result.status, "success")
    assert.deepEqual(result.cancellation, { applied: 1, candidates: 1 })
    const event = getEvent(database, "manual-stream")
    assert.equal(event.title, "夜配信")
    assert.equal(event.deletedAt, null)
    assert.equal(event.cancellationStatus, "llm_suspected")
    assert.equal(event.cancellationReason, "本人公告と一致するため取消の可能性")
    assert.equal(getPostLlmState(database, post.id).status, "success")

    const cached = await extractSchedulePost(database, post, {
      apiKey: "not-a-real-api-key",
      model: "gpt-4o-mini",
      fetchImpl,
    })
    assert.equal(cached.status, "cached")
    requestPostLlmReprocess(database, post.id, "schedule_message")
    const forced = await extractSchedulePost(database, post, {
      apiKey: "not-a-real-api-key",
      model: "gpt-4o-mini",
      fetchImpl,
    })
    assert.equal(forced.status, "success")
    assert.equal(calls, 2)
  } finally {
    database.close()
  }
})

test("schedule images need an image verdict and a board-like source post", async () => {
  const previousSecretsKey = process.env.LLM_SECRETS_KEY
  process.env.LLM_SECRETS_KEY = ["test", "asset", "secret", "key"].join("-")
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const boardUrl = "https://pbs.twimg.com/media/asset-verify-board.png"
  const streamUrl = "https://pbs.twimg.com/media/asset-verify-stream.png"
  const cachedFiles = []
  const requests = []
  const fetchImpl = async (url, options) => {
    requests.push({ url, body: JSON.parse(options.body) })
    return new Response(
      JSON.stringify({
        output_text: JSON.stringify({
          classification: "schedule",
          confidence: 0.97,
          evidence: "seven dated rows with stream titles",
          reason: "visible weekly schedule table",
        }),
      }),
      { status: 200, headers: { "content-type": "application/json" } },
    )
  }
  try {
    upsertLlmProvider(database, {
      id: "vision-provider",
      name: "vision-provider",
      baseUrl: "https://vision-provider.example.invalid/v1",
      model: "vision-model",
      capabilities: ["image"],
      replaceApiKey: true,
      apiKeyCiphertext: encryptSecret(
        "vision-key",
        process.env.LLM_SECRETS_KEY,
      ),
    })
    setLlmRouteProviders(database, "schedule_board", ["vision-provider"])
    upsertPosts(database, [
      {
        id: "asset-board-post",
        source: "x",
        text: "今週のスケジュール",
        published_at: "2026-08-24T01:00:00.000Z",
        url: "https://x.com/example/status/asset-board-post",
        media_url: boardUrl,
      },
      {
        // Written by an older sync: the derived label alone must not count.
        id: "asset-stream-post",
        source: "x",
        type: "notice",
        label: "SCHEDULE / 日程",
        text: "今日は22時から配信予定です",
        published_at: "2026-08-25T01:00:00.000Z",
        url: "https://x.com/example/status/asset-stream-post",
        media_url: streamUrl,
      },
    ])
    for (const [index, sourceUrl] of [boardUrl, streamUrl].entries()) {
      const written = await writeMediaFileAtomic({
        content: Buffer.from([
          0x89,
          0x50,
          0x4e,
          0x47,
          0x0d,
          0x0a,
          0x1a,
          0x0a,
          0x10 + index,
        ]),
        extension: "png",
      })
      cachedFiles.push(resolveMediaCachePath(written.relativePath))
      upsertMediaAsset(database, {
        source: "x",
        sourceUrl,
        status: "ready",
        cachePath: written.relativePath,
        mimeType: "image/png",
        sha256: written.sha256,
        byteSize: written.byteSize,
      })
    }
    upsertAssets(database, [
      {
        id: "schedule-2026-08-24",
        kind: "schedule",
        url: boardUrl,
        source_url: "https://x.com/example/status/asset-board-post",
        week_start: "2026-08-24",
      },
      {
        id: "schedule-2026-08-17",
        kind: "schedule",
        url: streamUrl,
        source_url: "https://x.com/example/status/asset-stream-post",
        week_start: "2026-08-17",
      },
    ])

    const summary = await verifyPendingScheduleAssets(database, { fetchImpl })
    assert.equal(summary.schedule, 1)
    assert.equal(summary.skipped, 1)
    assert.equal(requests.length, 1)
    assert.match(requests[0].url, /vision-provider/u)
    const content = requests[0].body.input[0].content
    assert.equal(content[1].type, "input_image")
    assert.match(content[1].image_url, /^data:image\/png;base64,/u)

    const board = getScheduleAssetReview(database, "schedule-2026-08-24")
    assert.equal(board.llmStatus, "schedule")
    assert.equal(board.llmModel, "vision-model")
    assert.equal(board.approved, true)
    const stream = getScheduleAssetReview(database, "schedule-2026-08-17")
    assert.equal(stream.sourceMatchesBoard, false)
    assert.equal(stream.llmStatus, "skipped")
    assert.equal(stream.llmReason, "source_not_board")
    assert.equal(stream.approved, false)
    assert.deepEqual(
      getDashboard(database, {
        now: new Date("2026-08-26T00:00:00Z"),
      }).scheduleImages.map((asset) => asset.id),
      ["schedule-2026-08-24"],
    )

    const rerun = await verifyPendingScheduleAssets(database, { fetchImpl })
    assert.equal(rerun.attempted, 1)
    assert.equal(requests.length, 1)
  } finally {
    database.close()
    for (const file of cachedFiles) fs.rmSync(file, { force: true })
    if (previousSecretsKey == null) delete process.env.LLM_SECRETS_KEY
    else process.env["LLM_SECRETS_KEY"] = previousSecretsKey
  }
})

test("queued posts run before keyword candidates and pick a text route", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const bodies = []
  const fetchImpl = async (url, options) => {
    bodies.push(options.body)
    return new Response(
      JSON.stringify({
        output_text: JSON.stringify({
          classification: "not_schedule",
          events: [],
          confidence: 0.9,
          evidence: "ordinary update",
          reason: null,
          action: "none",
        }),
      }),
      { status: 200, headers: { "content-type": "application/json" } },
    )
  }
  const options = {
    apiKey: "not-a-real-api-key",
    model: "gpt-4o-mini",
    fetchImpl,
    limit: 1,
  }
  try {
    upsertPosts(database, [
      {
        id: "fresh-keyword-post",
        source: "x",
        text: "9月6日 20時から配信予定です",
        published_at: "2026-09-05T01:00:00.000Z",
        url: "https://x.com/example/status/fresh-keyword-post",
      },
      {
        id: "old-queued-post",
        source: "x",
        text: "ちょっとした近況です",
        published_at: "2026-08-01T01:00:00.000Z",
        url: "https://x.com/example/status/old-queued-post",
      },
    ])
    setAppSetting(database, "schedule_message_enabled", "1")
    requestPostLlmReprocess(database, "old-queued-post")

    const first = await extractPendingSchedules(database, options)
    assert.equal(first.attempted, 1)
    assert.equal(first.messageAttempted, 1)
    assert.equal(bodies.length, 1)
    assert.match(bodies[0], /ちょっとした近況/u)
    const state = getPostLlmState(database, "old-queued-post")
    assert.equal(state.status, "success")
    assert.equal(state.route, "schedule_message")
    assert.equal(state.reprocessRequested, false)

    await extractPendingSchedules(database, options)
    assert.equal(bodies.length, 2)
    assert.match(bodies[1], /配信予定/u)
  } finally {
    database.close()
  }
})

test("a provider is retried three times before priority failover", async () => {
  const previousSecretsKey = process.env.LLM_SECRETS_KEY
  process.env.LLM_SECRETS_KEY = ["test", "schedule", "secret", "key"].join("-")
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  const makeProvider = (id, apiKey) =>
    upsertLlmProvider(database, {
      id,
      name: id,
      baseUrl: `https://${id}.example.invalid/v1`,
      model: `${id}-model`,
      capabilities: ["text"],
      maxRetries: 2,
      replaceApiKey: true,
      apiKeyCiphertext: encryptSecret(apiKey, process.env.LLM_SECRETS_KEY),
    })
  let calls = []
  try {
    makeProvider("first-provider", "first-key")
    makeProvider("second-provider", "second-key")
    // Simulate the legacy database default. Runtime extraction must still
    // honor the three-request policy even before a database reopen migrates it.
    database
      .prepare<unknown[], Record<string, any>>(
        "UPDATE llm_providers SET max_retries = 0 WHERE id = ?",
      )
      .run("first-provider")
    setLlmRouteProviders(database, "schedule_message", [
      "first-provider",
      "second-provider",
    ])
    const post = {
      id: "failover-post",
      source: "x",
      text: "9月6日に配信予定",
      publishedAt: "2026-08-28T01:00:00.000Z",
      url: "https://x.com/example/status/failover-post",
    }
    const result = await extractSchedulePost(database, post, {
      detectionType: "message",
      fetchImpl: async (url) => {
        calls.push(url)
        if (calls.length <= 3) return new Response("failure", { status: 502 })
        return new Response(
          JSON.stringify({
            output_text: JSON.stringify({
              classification: "not_schedule",
              events: [],
              confidence: 0.9,
              evidence: "not a concrete notice",
              action: "none",
            }),
          }),
          { status: 200, headers: { "content-type": "application/json" } },
        )
      },
    })
    assert.equal(result.status, "success")
    assert.equal(result.classification, "not_schedule")
    assert.equal(calls.length, 4)
    assert.equal(
      calls.filter((url) => url.includes("first-provider")).length,
      3,
    )
    assert.equal(
      calls.filter((url) => url.includes("second-provider")).length,
      1,
    )
  } finally {
    database.close()
    if (previousSecretsKey == null) delete process.env.LLM_SECRETS_KEY
    else process.env["LLM_SECRETS_KEY"] = previousSecretsKey
  }
})

test("schedule extraction honors its independent enable flag and keywords", async () => {
  const database = initializeDatabase({ seed: false, filename: ":memory:" })
  try {
    upsertPosts(database, [
      {
        id: "custom-keyword-post",
        source: "x",
        text: "WEEKLY_BOARD",
        published_at: "2026-08-28T01:00:00.000Z",
        url: "https://x.com/example/status/1234567890123456793",
      },
      {
        id: "default-keyword-post",
        source: "x",
        label: "SCHEDULE / 日程",
        text: "通常内容",
        published_at: "2026-08-28T02:00:00.000Z",
        url: "https://x.com/example/status/1234567890123456794",
      },
    ])
    setAppSetting(
      database,
      "schedule_keywords",
      JSON.stringify(["WEEKLY_BOARD"]),
    )
    setAppSetting(database, "schedule_extraction_enabled", "0")
    setAppSetting(database, "schedule_message_enabled", "1")
    const legacyDisabledButMessageEnabled = await extractPendingSchedules(
      database,
      { apiKey: "" },
    )
    assert.equal(legacyDisabledButMessageEnabled.attempted, 1)
    assert.equal(legacyDisabledButMessageEnabled.skipped, 1)
    assert.equal(legacyDisabledButMessageEnabled.disabled, undefined)

    setAppSetting(database, "schedule_extraction_enabled", "1")
    const result = await extractPendingSchedules(database, { apiKey: "" })
    assert.equal(result.attempted, 1)
    assert.equal(result.skipped, 1)
    assert.deepEqual(result.keywords, ["WEEKLY_BOARD"])
  } finally {
    database.close()
  }
})

test("OpenAI extraction aborts after its configured timeout", async () => {
  await assert.rejects(
    callOpenAiScheduleExtraction(
      {
        text: "スケジュール",
        publishedAt: "2026-08-28T01:00:00.000Z",
        url: "https://x.com/example/status/1234567890123456791",
      },
      [],
      {
        apiKey: "not-a-real-api-key",
        model: "gpt-4o-mini",
        timeoutMs: 5,
        fetchImpl: async (_url, options) =>
          new Promise((_resolve, reject) => {
            options.signal.addEventListener(
              "abort",
              () => {
                const error = new Error("aborted")
                error.name = "AbortError"
                reject(error)
              },
              { once: true },
            )
          }),
      },
    ),
    /LLM request timed out/u,
  )
})
