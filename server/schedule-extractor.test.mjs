import assert from "node:assert/strict"
import fs from "node:fs"
import test from "node:test"

import {
  getEvent,
  initializeDatabase,
  upsertMediaAsset,
  upsertPosts,
} from "./database.js"
import { resolveMediaCachePath, writeMediaFileAtomic } from "./media-cache.js"
import {
  callOpenAiScheduleExtraction,
  extractSchedulePost,
} from "./schedule-extractor.js"

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
    const event = getEvent(database, "x-schedule-20260906")
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
    assert.equal(getEvent(database, "x-schedule-20260906").title, "配信予定")
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
    /OpenAI request timed out/u,
  )
})
